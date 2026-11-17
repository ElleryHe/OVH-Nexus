package catalog

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/ovh/go-ovh/ovh"

	"ovh-nexus/server/internal/logger"
	"ovh-nexus/server/internal/ovhclient"
	"ovh-nexus/server/internal/store"
	"ovh-nexus/server/internal/vault"
)

func newTestCatalog(t *testing.T) (*Service, *store.Store, string, chi.Router) {
	t.Helper()
	dataDir := t.TempDir()
	v, err := vault.New("test-master-encryption-key-123456")
	if err != nil {
		t.Fatalf("vault.New failed: %v", err)
	}
	st, err := store.Open(dataDir, v)
	if err != nil {
		t.Fatalf("store.Open failed: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	var buf bytes.Buffer
	hub := logger.NewHub(st, logger.Options{Level: "DEBUG", Format: "text", Output: &buf})
	pool := ovhclient.NewPool(st, hub)
	svc := NewService(st, hub, pool, dataDir)

	r := chi.NewRouter()
	r.Route("/api", func(api chi.Router) {
		svc.MountRoutes(api)
	})
	return svc, st, dataDir, r
}

func TestHardwareParsersAndPlanInference(t *testing.T) {
	m1 := ParseMemoryCode("ram-32g-noecc-2133", nil)
	if got := FormatMemory(m1, "zh-CN"); got != "32 GB DDR4 · 非 ECC · 2133 MHz" {
		t.Fatalf("unexpected zh-CN memory format: %q", got)
	}
	if got := FormatMemory(m1, "en"); got != "32 GB DDR4 · Non-ECC · 2133 MHz" {
		t.Fatalf("unexpected en memory format: %q", got)
	}
	if got := FormatMemory(m1, "fr"); got != "32 Go DDR4 · Sans ECC · 2133 MHz" {
		t.Fatalf("unexpected fr memory format: %q", got)
	}

	s1 := ParseStorageCode("softraid-1x480ssd", false)
	if got := FormatStorage(s1, "zh-CN"); got != "1x 480 GB SSD · 软 RAID" {
		t.Fatalf("unexpected storage format: %q", got)
	}
	sSys := ParseStorageCode("softraid-2x480ssd-sata-system", true)
	if got := FormatStorage(sSys, "en"); got != "2x 480 GB SATA SSD" {
		t.Fatalf("unexpected systemStorage format: %q", got)
	}
	sNone := ParseStorageCode("noraid-0", false)
	if got := FormatStorage(sNone, "en"); got != "No Data Drive" {
		t.Fatalf("unexpected noraid-0 format: %q", got)
	}

	g1 := ParseGPUCode("gpu-4xnvidia-l40s-48g")
	if got := FormatGPU(g1, "en"); got != "4x NVIDIA L40S 48 GB" {
		t.Fatalf("unexpected GPU format: %q", got)
	}

	// Deterministic inference across product lines
	cases := map[string]string{
		"27skle01-v1-sgp": "KS-LE-1",
		"24ska01":         "KS-A",
		"25sysle02":       "SYS-LE-2",
		"25rise01":        "RISE-1",
		"25adv01":         "ADVANCE-1",
		"25scaleamd03":    "SCALE-a3",
		"25hgr01":         "HGR-HGR01",
	}
	for code, wantName := range cases {
		meta := InferPlanFromCode(code)
		if meta.Name != wantName {
			t.Errorf("InferPlanFromCode(%q) = %q, want %q", code, meta.Name, wantName)
		}
	}

	if got := StandardizeConfig("ram-64g-ecc-3200-24rise01"); got != "ram-64g" {
		t.Fatalf("StandardizeConfig unexpected: %q", got)
	}

	// Toronto normalization, cart region inference, generation inference, and tri-lingual datacenter labels
	if got := NormalizeDisplayDatacenter("ca-east-tor-a"); got != "tor" {
		t.Fatalf("NormalizeDisplayDatacenter(ca-east-tor-a) = %q, want \"tor\"", got)
	}
	if got := NormalizeDisplayDatacenter("ca-east-tor"); got != "tor" {
		t.Fatalf("NormalizeDisplayDatacenter(ca-east-tor) = %q, want \"tor\"", got)
	}
	if got := InferCartRegion("ca-east-tor-a"); got != "canada" {
		t.Fatalf("InferCartRegion(ca-east-tor-a) = %q, want \"canada\"", got)
	}
	if got := InferCartRegion("tor"); got != "canada" {
		t.Fatalf("InferCartRegion(tor) = %q, want \"canada\"", got)
	}
	if got := InferCartRegion("eu-south-mrs"); got != "europe" {
		t.Fatalf("InferCartRegion(eu-south-mrs) = %q, want \"europe\"", got)
	}
	if got := InferPlanGeneration("24ska01"); got != 2024 {
		t.Fatalf("InferPlanGeneration(24ska01) = %d, want 2024", got)
	}
	if got := InferPlanGeneration("27skle01-v1"); got != 2027 {
		t.Fatalf("InferPlanGeneration(27skle01-v1) = %d, want 2027", got)
	}
	if got := InferPlanGeneration("vps-2025-model1"); got != 0 {
		t.Fatalf("InferPlanGeneration(vps-2025-model1) = %d, want 0", got)
	}
	if got := FormatDatacenterLabel("bhs", "zh-CN"); got != "BHS（博阿努瓦，加拿大 🇨🇦）" {
		t.Fatalf("FormatDatacenterLabel(bhs, zh-CN) = %q", got)
	}
	if got := FormatDatacenterLabel("bhs", "en"); got != "BHS (Beauharnois, Canada 🇨🇦)" {
		t.Fatalf("FormatDatacenterLabel(bhs, en) = %q", got)
	}
	if got := FormatDatacenterLabel("fra", "fr"); got != "FRA (Francfort, Allemagne 🇩🇪)" {
		t.Fatalf("FormatDatacenterLabel(fra, fr) = %q", got)
	}
	if got := FormatDatacenterLabel("ca-east-tor-a", "zh-CN"); got != "TOR（多伦多，加拿大 🇨🇦）" {
		t.Fatalf("FormatDatacenterLabel(ca-east-tor-a, zh-CN) = %q", got)
	}
	if got := DatacenterFlag("tor"); got != "🇨🇦" {
		t.Fatalf("DatacenterFlag(tor) = %q, want 🇨🇦", got)
	}
}

func TestCatalogFetchingSingleflightDiskCacheAndRoutes(t *testing.T) {
	svc, _, dataDir, router := newTestCatalog(t)

	var ecoCalls atomic.Int32
	var bmCalls atomic.Int32
	var availCalls atomic.Int32

	mockOVH := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/order/catalog/public/eco":
			ecoCalls.Add(1)
			time.Sleep(30 * time.Millisecond)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"locale": map[string]any{"currencyCode": "EUR"},
				"plans": []map[string]any{
					{
						"planCode":    "24ska01",
						"invoiceName": "KS-A | Intel i7-6700k",
						"product":     "24ska01",
						"blobs":       map[string]any{"commercial": map[string]any{"name": "KS-A"}},
						"pricings": []map[string]any{
							{"mode": "default", "capacities": []string{"renew"}, "price": 5499000000},
						},
						"addonFamilies": []map[string]any{
							{"name": "memory", "default": "ram-32g-noecc-2133-24ska01", "addons": []string{"ram-32g-noecc-2133-24ska01", "ram-64g-noecc-2133-24ska01"}},
							{"name": "storage", "default": "softraid-2x480ssd-24ska01", "addons": []string{"softraid-2x480ssd-24ska01"}},
							{"name": "bandwidth", "default": "bandwidth-500-24ska01", "addons": []string{"bandwidth-500-24ska01"}},
						},
					},
				},
				"addons": []map[string]any{
					{
						"planCode": "ram-64g-noecc-2133-24ska01",
						"product":  "ram-64g-noecc-2133",
						"pricings": []map[string]any{{"mode": "default", "capacities": []string{"renew"}, "price": 1000000000}},
					},
				},
				"products": []map[string]any{
					{
						"name": "24ska01",
						"blobs": map[string]any{
							"technical": map[string]any{
								"server": map[string]any{
									"range": "kimsufi",
									"cpu": map[string]any{
										"brand": "Intel", "model": "i7-6700k", "cores": 4, "threads": 8, "frequency": 4, "boost": 4.2,
									},
								},
							},
						},
					},
				},
			})
		case "/order/catalog/public/baremetalServers":
			bmCalls.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"locale": map[string]any{"currencyCode": "EUR"},
				"plans": []map[string]any{
					{
						"planCode":    "25adv01",
						"invoiceName": "ADVANCE-1 | AMD EPYC 4244P",
						"product":     "25adv01",
						"blobs":       map[string]any{"commercial": map[string]any{"name": "ADVANCE-1"}},
						"pricings": []map[string]any{
							{"mode": "default", "capacities": []string{"renew"}, "price": 7900000000},
						},
						"addonFamilies": []map[string]any{
							{"name": "vrack", "default": "vrack-bandwidth-1000-25adv01", "addons": []string{"vrack-bandwidth-1000-25adv01"}},
						},
					},
				},
			})
		case "/order/catalog/public/vps":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"locale": map[string]any{"currencyCode": "EUR"},
				"plans": []map[string]any{
					{"planCode": "vps-2025-model1", "invoiceName": "VPS-1", "pricings": []map[string]any{{"mode": "default", "capacities": []string{"renew"}, "price": 500000000}}},
				},
			})
		case "/dedicated/server/datacenter/availabilities":
			availCalls.Add(1)
			pc := r.URL.Query().Get("planCode")
			items := []map[string]any{
				{
					"fqn":      "24ska01.ram-32g-noecc-2133.softraid-2x480ssd",
					"planCode": "24ska01",
					"server":   "24ska01",
					"memory":   "ram-32g-noecc-2133",
					"storage":  "softraid-2x480ssd",
					"datacenters": []map[string]any{
						{"datacenter": "gra", "availability": "1H-high"},
						{"datacenter": "ynm", "availability": "unavailable"},
					},
				},
				{
					"fqn":      "24ska01.ram-64g-noecc-2133.softraid-2x480ssd",
					"planCode": "24ska01",
					"server":   "24ska01",
					"memory":   "ram-64g-noecc-2133",
					"storage":  "softraid-2x480ssd",
					"datacenters": []map[string]any{
						{"datacenter": "gra", "availability": "unavailable"},
						{"datacenter": "rbx", "availability": "72H"},
					},
				},
			}
			if pc != "" {
				var filtered []map[string]any
				for _, it := range items {
					if it["planCode"] == pc {
						filtered = append(filtered, it)
					}
				}
				_ = json.NewEncoder(w).Encode(filtered)
				return
			}
			_ = json.NewEncoder(w).Encode(items)
		default:
			http.NotFound(w, r)
		}
	}))
	defer mockOVH.Close()
	svc.SetPublicAPIBaseURL(mockOVH.URL)

	// 1. Singleflight test: 8 concurrent ListServers calls must only hit upstream once.
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			resp, err := svc.ListServers(context.Background(), ListServersOptions{Endpoint: "ovh-eu", Zone: "IE"})
			if err != nil {
				t.Errorf("concurrent ListServers failed: %v", err)
				return
			}
			if len(resp.Servers) != 2 {
				t.Errorf("expected 2 servers, got %d", len(resp.Servers))
			}
		}()
	}
	wg.Wait()

	if ecoCalls.Load() != 1 || bmCalls.Load() != 1 || availCalls.Load() != 1 {
		t.Fatalf("expected singleflight to deduplicate upstream calls (got eco=%d bm=%d avail=%d)", ecoCalls.Load(), bmCalls.Load(), availCalls.Load())
	}

	// Verify disk JSON cache files were written under <dataDir>/cache
	cacheFiles, _ := filepath.Glob(filepath.Join(dataDir, "cache", "*.json"))
	if len(cacheFiles) < 2 {
		t.Fatalf("expected disk JSON cache files in %s/cache, got %v", dataDir, cacheFiles)
	}

	// 2. GET /api/servers should serve from memory cache without calling upstream again.
	req := httptest.NewRequest(http.MethodGet, "/api/servers", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/servers failed: %d", rec.Code)
	}
	if ecoCalls.Load() != 1 {
		t.Fatalf("expected GET /api/servers to hit cache, ecoCalls=%d", ecoCalls.Load())
	}

	var listResp ServerListResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &listResp); err != nil {
		t.Fatalf("decode /api/servers: %v", err)
	}
	if len(listResp.Servers) != 2 || listResp.Servers[0].Name != "KS-A" || !listResp.CacheInfo.Cached {
		t.Fatalf("unexpected /api/servers payload: %+v", listResp)
	}

	// Verify ynm -> mum display mapping
	foundMum := false
	for _, dc := range listResp.Servers[0].Datacenters {
		if dc.Datacenter == "mum" {
			foundMum = true
		}
	}
	if !foundMum {
		t.Fatalf("expected ynm datacenter mapped to mum in server plan datacenters: %+v", listResp.Servers[0].Datacenters)
	}

	// 3. POST /api/servers/refresh forces upstream refresh
	req = httptest.NewRequest(http.MethodPost, "/api/servers/refresh", nil)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || ecoCalls.Load() != 2 {
		t.Fatalf("POST /api/servers/refresh failed: code=%d ecoCalls=%d", rec.Code, ecoCalls.Load())
	}

	// 4. GET /api/catalog/dictionary + ETag 304
	req = httptest.NewRequest(http.MethodGet, "/api/catalog/dictionary", nil)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/catalog/dictionary failed: %d", rec.Code)
	}
	etag := rec.Header().Get("ETag")
	if etag == "" {
		t.Fatalf("expected ETag header on /api/catalog/dictionary")
	}
	req304 := httptest.NewRequest(http.MethodGet, "/api/catalog/dictionary", nil)
	req304.Header.Set("If-None-Match", etag)
	rec304 := httptest.NewRecorder()
	router.ServeHTTP(rec304, req304)
	if rec304.Code != http.StatusNotModified {
		t.Fatalf("expected 304 Not Modified for matching ETag, got %d", rec304.Code)
	}

	// 5. GET /api/availability & POST /api/availability/{planCode}
	req = httptest.NewRequest(http.MethodGet, "/api/availability", nil)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/availability failed: %d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/availability/24ska01", strings.NewReader(`{"options":["ram-64g-noecc-2133-24ska01","softraid-2x480ssd-24ska01"]}`))
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /api/availability/24ska01 failed: %d %s", rec.Code, rec.Body.String())
	}
	var availMap map[string]string
	_ = json.Unmarshal(rec.Body.Bytes(), &availMap)
	if availMap["rbx"] != "72H" || availMap["gra"] != "unavailable" {
		t.Fatalf("unexpected option-matched availability: %+v", availMap)
	}

	// 6. GET /api/cache/info & POST /api/cache/clear
	req = httptest.NewRequest(http.MethodGet, "/api/cache/info", nil)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/cache/info failed: %d", rec.Code)
	}
	var cacheInfo CacheInfoResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &cacheInfo)
	if !cacheInfo.Backend.HasCachedData || !cacheInfo.Backend.CacheValid || cacheInfo.Backend.ServerCount != 2 {
		t.Fatalf("unexpected cache info: %+v", cacheInfo)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/cache/clear", strings.NewReader(`{"type":"all"}`))
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /api/cache/clear failed: %d", rec.Code)
	}
	entries, _ := os.ReadDir(filepath.Join(dataDir, "cache"))
	if len(entries) != 0 {
		t.Fatalf("expected cache directory to be cleared, got %d files", len(entries))
	}
}

func TestGetServerPriceCartLifecycle(t *testing.T) {
	svc, _, _, router := newTestCatalog(t)

	// Missing credentials returns 400 OVH_CREDENTIALS_MISSING
	req := httptest.NewRequest(http.MethodPost, "/api/servers/24ska01/price", strings.NewReader(`{"datacenter":"gra"}`))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "OVH_CREDENTIALS_MISSING") {
		t.Fatalf("expected 400 OVH_CREDENTIALS_MISSING, got %d %s", rec.Code, rec.Body.String())
	}

	var deletedCart bool
	var setDC string
	mockOVH := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/auth/time":
			_ = json.NewEncoder(w).Encode(time.Now().Unix())
		case r.Method == http.MethodPost && r.URL.Path == "/order/cart":
			_ = json.NewEncoder(w).Encode(map[string]any{"cartId": "cart-123"})
		case r.Method == http.MethodPost && r.URL.Path == "/order/cart/cart-123/assign":
			_ = json.NewEncoder(w).Encode(map[string]any{})
		case r.Method == http.MethodPost && r.URL.Path == "/order/cart/cart-123/eco":
			_ = json.NewEncoder(w).Encode(map[string]any{"itemId": 77})
		case r.Method == http.MethodPost && r.URL.Path == "/order/cart/cart-123/item/77/configuration":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["label"] == "dedicated_datacenter" {
				setDC = fmt.Sprint(body["value"])
			}
			_ = json.NewEncoder(w).Encode(map[string]any{})
		case r.Method == http.MethodGet && r.URL.Path == "/order/cart/cart-123/eco/options":
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{"planCode": "ram-64g-noecc-2133-24ska01", "duration": "P1M", "pricingMode": "default"},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/order/cart/cart-123/eco/options":
			_ = json.NewEncoder(w).Encode(map[string]any{})
		case r.Method == http.MethodGet && r.URL.Path == "/order/cart/cart-123/summary":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"prices": map[string]any{
					"withTax":      map[string]any{"value": 11.99, "currencyCode": "EUR"},
					"withoutTax":   map[string]any{"value": 9.99, "currencyCode": "EUR"},
					"tax":          map[string]any{"value": 2.00, "currencyCode": "EUR"},
					"currencyCode": "EUR",
				},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/order/cart/cart-123":
			_ = json.NewEncoder(w).Encode(map[string]any{"items": []any{}})
		case r.Method == http.MethodDelete && r.URL.Path == "/order/cart/cart-123":
			deletedCart = true
			_ = json.NewEncoder(w).Encode(map[string]any{})
		default:
			http.NotFound(w, r)
		}
	}))
	defer mockOVH.Close()

	svc.SetPublicAPIBaseURL(mockOVH.URL)
	svc.SetAccountClientFunc(func(_ context.Context, _ string) (*ovh.Client, *store.OVHAccount, error) {
		c, err := ovh.NewClient(mockOVH.URL, "ak-test", "as-test", "ck-test")
		if err != nil {
			return nil, nil, err
		}
		return c, &store.OVHAccount{ID: "acc-1", Endpoint: "ovh-eu", Zone: "IE"}, nil
	})

	// Passing display dc "mum" must map to API dc "ynm"
	req = httptest.NewRequest(http.MethodPost, "/api/servers/24ska01/price", strings.NewReader(`{"datacenter":"mum","options":["ram-64g-noecc-2133-24ska01"]}`))
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /api/servers/24ska01/price failed: %d %s", rec.Code, rec.Body.String())
	}
	var priceResp ServerPriceResult
	if err := json.Unmarshal(rec.Body.Bytes(), &priceResp); err != nil {
		t.Fatalf("decode price resp: %v", err)
	}
	if !priceResp.Success || priceResp.Price == nil || priceResp.Price.Prices.WithTax == nil || *priceResp.Price.Prices.WithTax != 11.99 {
		t.Fatalf("unexpected price response: %+v", priceResp)
	}
	if setDC != "ynm" {
		t.Fatalf("expected mum to normalize to ynm in cart configuration, got %q", setDC)
	}
	if !deletedCart {
		t.Fatalf("expected temporary quote cart to be deleted")
	}
}

func TestPresaleSKUSynthesisInListServersAndConfigAvailabilities(t *testing.T) {
	svc, _, _, _ := newTestCatalog(t)

	mockOVH := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/order/catalog/public/eco":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"locale": map[string]any{"currencyCode": "EUR"},
				"plans": []map[string]any{
					{
						"planCode":    "24ska01",
						"invoiceName": "KS-A | Intel i7-6700k",
						"product":     "24ska01",
						"blobs":       map[string]any{"commercial": map[string]any{"name": "KS-A"}},
						"pricings": []map[string]any{
							{"mode": "default", "capacities": []string{"renew"}, "price": 5499000000},
						},
					},
				},
			})
		case "/order/catalog/public/baremetalServers", "/order/catalog/public/vps":
			_ = json.NewEncoder(w).Encode(map[string]any{"locale": map[string]any{"currencyCode": "EUR"}, "plans": []any{}})
		case "/dedicated/server/datacenter/availabilities":
			pc := r.URL.Query().Get("planCode")
			items := []map[string]any{
				{
					"fqn":      "24ska01.ram-32g-noecc-2133.softraid-2x480ssd",
					"planCode": "24ska01",
					"server":   "24ska01",
					"memory":   "ram-32g-noecc-2133",
					"storage":  "softraid-2x480ssd",
					"datacenters": []map[string]any{
						{"datacenter": "gra", "availability": "1H-high"},
					},
				},
				{
					"fqn":      "27skle01-v1.ram-64g-ecc-3200.softraid-2x960nvme",
					"planCode": "27skle01-v1",
					"server":   "27skle01",
					"memory":   "ram-64g-ecc-3200",
					"storage":  "softraid-2x960nvme",
					"datacenters": []map[string]any{
						{"datacenter": "ca-east-tor-a", "availability": "72H"},
						{"datacenter": "bhs", "availability": "unavailable"},
					},
				},
			}
			if pc != "" {
				var filtered []map[string]any
				for _, it := range items {
					if it["planCode"] == pc {
						filtered = append(filtered, it)
					}
				}
				_ = json.NewEncoder(w).Encode(filtered)
				return
			}
			_ = json.NewEncoder(w).Encode(items)
		default:
			http.NotFound(w, r)
		}
	}))
	defer mockOVH.Close()
	svc.SetPublicAPIBaseURL(mockOVH.URL)

	resp, err := svc.ListServers(context.Background(), ListServersOptions{Endpoint: "ovh-eu", Zone: "IE", ForceRefresh: true})
	if err != nil {
		t.Fatalf("ListServers failed: %v", err)
	}
	if len(resp.Servers) != 2 {
		t.Fatalf("expected 2 servers (1 listed + 1 presale), got %d", len(resp.Servers))
	}

	listed := resp.Servers[0]
	if listed.PlanCode != "24ska01" || listed.ListingState != "listed" || !listed.Orderable || listed.Generation != 2024 {
		t.Fatalf("unexpected listed ServerPlan: %+v", listed)
	}

	presale := resp.Servers[1]
	if presale.PlanCode != "27skle01-v1" || presale.ListingState != "presale" || presale.Orderable || presale.Generation != 2027 || presale.MonthlyPrice != nil {
		t.Fatalf("unexpected presale ServerPlan: %+v", presale)
	}
	if presale.Name != "KS-LE-1" || presale.CatalogSource != "presale" {
		t.Fatalf("expected inferred presale name KS-LE-1 and source presale, got %+v", presale)
	}
	foundTor := false
	for _, dc := range presale.Datacenters {
		if dc.Datacenter == "tor" && dc.Availability == "72H" && dc.Flag == "🇨🇦" {
			foundTor = true
		}
	}
	if !foundTor {
		t.Fatalf("expected ca-east-tor-a normalized to tor with 🇨🇦 flag on presale plan: %+v", presale.Datacenters)
	}

	cfgs, err := svc.GetConfigLevelAvailabilities(context.Background(), "ovh-eu", "IE", "27skle01-v1")
	if err != nil {
		t.Fatalf("GetConfigLevelAvailabilities for presale plan failed: %v", err)
	}
	if len(cfgs) != 1 {
		t.Fatalf("expected 1 synthesized ConfigAvailability for presale plan, got %d", len(cfgs))
	}
	for _, cfg := range cfgs {
		if cfg.ListingState != "presale" || cfg.Orderable || cfg.Generation != 2027 {
			t.Fatalf("unexpected presale ConfigAvailability metadata: %+v", cfg)
		}
		if cfg.Datacenters["tor"] != "72H" || cfg.Datacenters["ca-east-tor-a"] != "72H" {
			t.Fatalf("expected both raw and normalized Toronto availability in ConfigAvailability: %+v", cfg.Datacenters)
		}
	}
}

func TestRealPlansAliasExclusionAnnouncedSynthesisAndPlanConfigs(t *testing.T) {
	svc, _, _, _ := newTestCatalog(t)
	svc.SetListedAtResolver(func(planCode string) string {
		if planCode == "27risele01-v1" {
			return "2026-11-17T08:00:00Z"
		}
		return ""
	})

	vpsHitCount := 0
	mockOVH := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/order/catalog/public/eco":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"locale": map[string]any{"currencyCode": "EUR"},
				"plans": []map[string]any{
					{
						"planCode":    "24sys02-v3",
						"invoiceName": "SYS-2 | Intel Xeon-D 2141I",
						"product":     "24sys02-1g",
						"blobs":       map[string]any{"commercial": map[string]any{"name": "SYS-2"}},
						"pricings": []map[string]any{
							{"mode": "default", "capacities": []string{"renew"}, "price": 3999000000},
						},
						"addonFamilies": []map[string]any{
							{"name": "memory", "default": "ram-32g-ecc-2133-24sys02-1g", "addons": []string{"ram-32g-ecc-2133-24sys02-1g", "ram-64g-ecc-2133-24sys02-1g"}},
							{"name": "storage", "default": "softraid-2x512nvme-24sys02-1g", "addons": []string{"softraid-2x512nvme-24sys02-1g", "softraid-2x2000sa-24sys02-1g"}},
						},
					},
					{
						"planCode":    "27risele01-v1",
						"invoiceName": "RISE-LE-1 | AMD EPYC 4244P",
						"product":     "27risele01-v1",
						"blobs":       map[string]any{"commercial": map[string]any{"name": "RISE-LE-1"}},
						"pricings": []map[string]any{
							{"mode": "default", "capacities": []string{"renew"}, "price": 4599000000},
						},
						"addonFamilies": []map[string]any{
							{"name": "memory", "default": "ram-32g-ecc-5200-27risele01-v1", "addons": []string{"ram-32g-ecc-5200-27risele01-v1"}},
							{"name": "storage", "default": "softraid-2x512nvme-27risele01-v1", "addons": []string{"softraid-2x512nvme-27risele01-v1"}},
						},
					},
					{
						// Addon plan listed in cat.Plans without memory+storage families -> must NOT enter RealPlans
						"planCode":    "ram-64g-ecc-2133-24sys02-1g",
						"invoiceName": "64GB DDR4 ECC 2133MHz",
						"product":     "ram-64g-ecc-2133",
						"pricings": []map[string]any{
							{"mode": "default", "capacities": []string{"renew"}, "price": 1200000000},
						},
						"addonFamilies": []map[string]any{
							{"name": "Sub-Addon", "addons": []string{"dummy"}},
						},
					},
				},
				"addons": []map[string]any{
					{
						"planCode":    "ram-32g-ecc-2133-24sys02-1g",
						"invoiceName": "32GB DDR4 ECC 2133MHz",
						"product":     "ram-32g-ecc-2133",
						"pricings": []map[string]any{
							{"mode": "default", "capacities": []string{"renew"}, "price": 0},
						},
					},
					{
						"planCode":    "softraid-2x512nvme-24sys02-1g",
						"invoiceName": "2x512GB NVMe",
						"product":     "softraid-2x512nvme",
						"pricings": []map[string]any{
							{"mode": "default", "capacities": []string{"renew"}, "price": 0},
						},
					},
					{
						"planCode":    "softraid-2x2000sa-24sys02-1g",
						"invoiceName": "2x2TB SATA",
						"product":     "softraid-2x2000sa",
						"pricings": []map[string]any{
							{"mode": "default", "capacities": []string{"renew"}, "price": 500000000},
						},
					},
				},
			})
		case "/order/catalog/public/baremetalServers":
			_ = json.NewEncoder(w).Encode(map[string]any{"locale": map[string]any{"currencyCode": "EUR"}, "plans": []any{}})
		case "/order/catalog/public/vps":
			vpsHitCount++
			_ = json.NewEncoder(w).Encode(map[string]any{
				"locale": map[string]any{"currencyCode": "EUR"},
				"plans": []map[string]any{
					{
						"planCode":    "vps-2025-model1",
						"invoiceName": "VPS-1",
						"product":     "vps-2025-model1",
						"pricings": []map[string]any{
							{"mode": "default", "capacities": []string{"renew"}, "price": 420000000},
						},
					},
				},
			})
		case "/dedicated/server/datacenter/availabilities":
			items := []map[string]any{
				{
					"fqn":      "24sys02-v3.ram-32g-ecc-2133.softraid-2x512nvme",
					"planCode": "24sys02-v3",
					"server":   "24sys02-1g",
					"memory":   "ram-32g-ecc-2133",
					"storage":  "softraid-2x512nvme",
					"datacenters": []map[string]any{
						{"datacenter": "gra", "availability": "1H-high"},
					},
				},
				{
					"fqn":      "24sys02-v3.ram-64g-ecc-2133.softraid-2x2000sa",
					"planCode": "24sys02-v3",
					"server":   "24sys02-1g",
					"memory":   "ram-64g-ecc-2133",
					"storage":  "softraid-2x2000sa",
					"datacenters": []map[string]any{
						{"datacenter": "rbx", "availability": "24H"},
					},
				},
			}
			_ = json.NewEncoder(w).Encode(items)
		default:
			http.NotFound(w, r)
		}
	}))
	defer mockOVH.Close()
	svc.SetPublicAPIBaseURL(mockOVH.URL)

	// 1. Verify GetBaremetalDictionary skips VPS and populates RealPlans without alias pollution
	bmDict, err := svc.GetBaremetalDictionary(context.Background(), "ovh-eu", "IE", true)
	if err != nil {
		t.Fatalf("GetBaremetalDictionary failed: %v", err)
	}
	if vpsHitCount != 0 {
		t.Fatalf("expected GetBaremetalDictionary to skip VPS catalog, got %d VPS requests", vpsHitCount)
	}
	if len(bmDict.RealPlans) != 2 {
		t.Fatalf("expected exactly 2 RealPlans (24sys02-v3, 27risele01-v1), got %d: %+v", len(bmDict.RealPlans), bmDict.RealPlans)
	}
	if !IsPlanListedInDict("24sys02-v3", bmDict) || !IsPlanListedInDict("27risele01-v1", bmDict) || !IsPlanListedInDict("27risele01-v1-sgp", bmDict) {
		t.Fatalf("expected real planCodes and regional variants to be listed in RealPlans")
	}
	if IsPlanListedInDict("24sys02-1g", bmDict) || IsPlanListedInDict("24sys02", bmDict) || IsPlanListedInDict("27risele01", bmDict) || IsPlanListedInDict("vps-2025-model1", bmDict) {
		t.Fatalf("expected product alias 24sys02-1g, version-stripped 24sys02/27risele01, and vps plan to NOT be in RealPlans")
	}

	// 2. Verify GetCatalogAvailabilities synthesizes announced 27risele01-v1 and computes config pricing
	availItems, err := svc.GetCatalogAvailabilities(context.Background(), "ovh-eu", "IE", "")
	if err != nil {
		t.Fatalf("GetCatalogAvailabilities failed: %v", err)
	}
	if len(availItems) != 3 {
		t.Fatalf("expected 3 AvailabilityItems (2 for 24sys02-v3 + 1 announced 27risele01-v1), got %d", len(availItems))
	}
	var foundAnnounced *AvailabilityItem
	var foundUpgraded *AvailabilityItem
	for i := range availItems {
		it := &availItems[i]
		if it.PlanCode == "27risele01-v1" {
			foundAnnounced = it
		}
		if it.PlanCode == "24sys02-v3" && it.ConfigKey == "ram-64g-ecc-2133+softraid-2x2000sa" {
			foundUpgraded = it
		}
	}
	if foundAnnounced == nil || foundAnnounced.ListingState != "announced" || foundAnnounced.Price != 45.99 || foundAnnounced.ListedAt != "2026-11-17T08:00:00Z" || !foundAnnounced.IsDefault {
		t.Fatalf("unexpected announced AvailabilityItem: %+v", foundAnnounced)
	}
	if foundUpgraded == nil || foundUpgraded.EstimatedMonthlyPrice != 56.99 || foundUpgraded.IsDefault {
		t.Fatalf("expected upgraded config price 39.99 + 12.00 + 5.00 = 56.99, got %+v", foundUpgraded)
	}

	// 3. Verify GET /api/catalog/plans/{planCode}/configs via HTTP router
	r := chi.NewRouter()
	svc.MountRoutes(r)

	req := httptest.NewRequest(http.MethodGet, "/catalog/plans/24sys02-v3/configs?subsidiary=IE", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GET /catalog/plans/24sys02-v3/configs status=%d body=%s", w.Code, w.Body.String())
	}
	var cfgResp PlanConfigsResponse
	if err := json.NewDecoder(w.Body).Decode(&cfgResp); err != nil {
		t.Fatalf("failed to decode PlanConfigsResponse: %v", err)
	}
	if cfgResp.PlanCode != "24sys02-v3" || cfgResp.ListingState != "listed" || cfgResp.BasePrice != 39.99 || len(cfgResp.Configs) != 2 {
		t.Fatalf("unexpected PlanConfigsResponse: %+v", cfgResp)
	}
	if !cfgResp.Configs[0].IsDefault || cfgResp.Configs[0].EstimatedMonthlyPrice != 39.99 {
		t.Fatalf("expected first config to be default at 39.99, got %+v", cfgResp.Configs[0])
	}
	if cfgResp.Configs[1].EstimatedMonthlyPrice != 56.99 || cfgResp.Configs[1].InStockCount != 1 {
		t.Fatalf("expected second config at 56.99 with inStockCount=1, got %+v", cfgResp.Configs[1])
	}
}

func TestBlackFridayPromotionsSetupFeesPricingModesAndCartHelpers(t *testing.T) {
	svc, _, _, router := newTestCatalog(t)

	var cartDurationsTried []string
	var optionModeChosen, optionDurChosen string
	var appliedCoupon string
	rejectCoupon := false

	mockOVH := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/auth/time":
			_ = json.NewEncoder(w).Encode(time.Now().Unix())
		case r.URL.Path == "/order/catalog/public/eco":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"locale": map[string]any{"currencyCode": "EUR"},
				"plans": []map[string]any{
					{
						"planCode":    "24sys02-v3",
						"invoiceName": "SYS-2 | Intel Xeon-D 2141I",
						"product":     "24sys02-1g",
						"blobs":       map[string]any{"commercial": map[string]any{"name": "SYS-2"}},
						"pricings": []map[string]any{
							{
								"mode":       "default",
								"capacities": []string{"installation"},
								"price":      3499000000,
							},
							{
								"mode":       "default",
								"capacities": []string{"renew"},
								"interval":   1,
								"commitment": 0,
								"price":      3799000000,
								"promotions": []map[string]any{
									{
										"name":        "BF26_ECO_20OFF",
										"description": "Black Friday 20% off",
										"value":       20,
										"type":        "percentage",
										"duration":    0,
										"endDate":     "2026-12-03T13:00:00Z",
										"total":       map[string]any{"value": 3039000000},
									},
								},
							},
							{
								"mode":       "upfront12",
								"capacities": []string{"installation"},
								"price":      0,
							},
							{
								"mode":       "upfront12",
								"capacities": []string{"renew"},
								"interval":   12,
								"commitment": 12,
								"price":      43309000000,
								"promotions": []map[string]any{
									{
										"name":        "BF26_ECO_20OFF",
										"description": "Black Friday 20% off",
										"value":       20,
										"type":        "percentage",
										"duration":    1,
										"endDate":     "2026-12-03T13:00:00Z",
										"total":       map[string]any{"value": 34647000000},
									},
								},
							},
						},
						"addonFamilies": []map[string]any{
							{"name": "memory", "default": "ram-32g-ecc-2133-24sys02-1g", "addons": []string{"ram-32g-ecc-2133-24sys02-1g", "ram-64g-ecc-2133-24sys02-1g"}},
							{"name": "storage", "default": "softraid-2x512nvme-24sys02-1g", "addons": []string{"softraid-2x512nvme-24sys02-1g"}},
						},
					},
				},
				"addons": []map[string]any{
					{
						"planCode": "ram-32g-ecc-2133-24sys02-1g",
						"product":  "ram-32g-ecc-2133",
						"pricings": []map[string]any{
							{"mode": "default", "capacities": []string{"renew"}, "price": 0},
							{"mode": "upfront12", "capacities": []string{"renew"}, "interval": 12, "price": 0},
						},
					},
					{
						"planCode": "ram-64g-ecc-2133-24sys02-1g",
						"product":  "ram-64g-ecc-2133",
						"pricings": []map[string]any{
							{"mode": "default", "capacities": []string{"renew"}, "price": 1200000000},
							{"mode": "upfront12", "capacities": []string{"renew"}, "interval": 12, "price": 13680000000},
						},
					},
					{
						"planCode": "softraid-2x512nvme-24sys02-1g",
						"product":  "softraid-2x512nvme",
						"pricings": []map[string]any{
							{"mode": "default", "capacities": []string{"renew"}, "price": 0},
							{"mode": "upfront12", "capacities": []string{"renew"}, "interval": 12, "price": 0},
						},
					},
				},
			})
		case r.URL.Path == "/order/catalog/public/baremetalServers":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"locale": map[string]any{"currencyCode": "EUR"},
				"plans": []map[string]any{
					{
						"planCode":    "24adv05-v3",
						"invoiceName": "ADVANCE-5 | AMD EPYC",
						"product":     "24adv05-v3",
						"blobs":       map[string]any{"commercial": map[string]any{"name": "ADVANCE-5"}},
						"pricings": []map[string]any{
							{
								"mode":       "default",
								"capacities": []string{"installation"},
								"price":      39499000000,
								"promotions": []map[string]any{
									{
										"name":        "BF26_OVH_FREESETUP",
										"description": "Free installation fee",
										"value":       100,
										"type":        "percentage",
										"total":       map[string]any{"value": 0},
									},
								},
							},
							{
								"mode":       "default",
								"capacities": []string{"renew"},
								"interval":   1,
								"commitment": 0,
								"price":      15099000000,
							},
						},
					},
				},
			})
		case r.URL.Path == "/order/catalog/public/vps":
			_ = json.NewEncoder(w).Encode(map[string]any{"locale": map[string]any{"currencyCode": "EUR"}, "plans": []any{}})
		case r.URL.Path == "/dedicated/server/datacenter/availabilities":
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{
					"fqn":      "24sys02-v3.ram-32g-ecc-2133.softraid-2x512nvme",
					"planCode": "24sys02-v3",
					"server":   "24sys02-1g",
					"memory":   "ram-32g-ecc-2133",
					"storage":  "softraid-2x512nvme",
					"datacenters": []map[string]any{
						{"datacenter": "gra", "availability": "1H-high"},
					},
				},
				{
					"fqn":      "24sys02-v3.ram-64g-ecc-2133.softraid-2x512nvme",
					"planCode": "24sys02-v3",
					"server":   "24sys02-1g",
					"memory":   "ram-64g-ecc-2133",
					"storage":  "softraid-2x512nvme",
					"datacenters": []map[string]any{
						{"datacenter": "gra", "availability": "1H-high"},
					},
				},
				{
					"fqn":      "24adv05-v3.default.default",
					"planCode": "24adv05-v3",
					"server":   "24adv05-v3",
					"datacenters": []map[string]any{
						{"datacenter": "rbx", "availability": "72H"},
					},
				},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/order/cart":
			_ = json.NewEncoder(w).Encode(map[string]any{"cartId": "cart-promo"})
		case r.Method == http.MethodPost && r.URL.Path == "/order/cart/cart-promo/assign":
			_ = json.NewEncoder(w).Encode(map[string]any{})
		case r.Method == http.MethodPost && r.URL.Path == "/order/cart/cart-promo/eco":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			dur := fmt.Sprint(body["duration"])
			cartDurationsTried = append(cartDurationsTried, dur)
			if dur == "P12M" {
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]any{"message": "Duration P12M not accepted, use P1Y"})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"itemId": 88})
		case r.Method == http.MethodPost && r.URL.Path == "/order/cart/cart-promo/item/88/configuration":
			_ = json.NewEncoder(w).Encode(map[string]any{})
		case r.Method == http.MethodGet && r.URL.Path == "/order/cart/cart-promo/eco/options":
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{"planCode": "ram-64g-ecc-2133-24sys02-1g", "duration": "P1M", "pricingMode": "default"},
				{"planCode": "ram-64g-ecc-2133-24sys02-1g", "duration": "P12M", "pricingMode": "upfront12"},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/order/cart/cart-promo/eco/options":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			optionModeChosen = fmt.Sprint(body["pricingMode"])
			optionDurChosen = fmt.Sprint(body["duration"])
			_ = json.NewEncoder(w).Encode(map[string]any{})
		case r.Method == http.MethodPost && r.URL.Path == "/order/cart/cart-promo/coupons":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			appliedCoupon = fmt.Sprint(body["coupon"])
			if rejectCoupon {
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]any{"message": "Invalid coupon"})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{})
		case r.Method == http.MethodGet && r.URL.Path == "/order/cart/cart-promo/summary":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"prices": map[string]any{
					"withTax":      map[string]any{"value": 415.76, "currencyCode": "EUR"},
					"withoutTax":   map[string]any{"value": 346.47, "currencyCode": "EUR"},
					"tax":          map[string]any{"value": 69.29, "currencyCode": "EUR"},
					"currencyCode": "EUR",
				},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/order/cart/cart-promo":
			_ = json.NewEncoder(w).Encode(map[string]any{"items": []any{}})
		case r.Method == http.MethodDelete && r.URL.Path == "/order/cart/cart-promo":
			_ = json.NewEncoder(w).Encode(map[string]any{})
		default:
			http.NotFound(w, r)
		}
	}))
	defer mockOVH.Close()
	svc.SetPublicAPIBaseURL(mockOVH.URL)
	svc.SetAccountClientFunc(func(_ context.Context, _ string) (*ovh.Client, *store.OVHAccount, error) {
		c, err := ovh.NewClient(mockOVH.URL, "ak-test", "as-test", "ck-test")
		if err != nil {
			return nil, nil, err
		}
		return c, &store.OVHAccount{ID: "acc-1", Endpoint: "ovh-eu", Zone: "IE"}, nil
	})

	// 1. Verify ServerPlans parsing for 24sys02-v3 (-20% recurring promo) and 24adv05-v3 (FREE SETUP promo)
	listResp, err := svc.ListServers(context.Background(), ListServersOptions{Endpoint: "ovh-eu", Zone: "IE", ForceRefresh: true})
	if err != nil {
		t.Fatalf("ListServers failed: %v", err)
	}
	if len(listResp.Servers) != 2 {
		t.Fatalf("expected 2 servers, got %d", len(listResp.Servers))
	}
	var sys2, adv5 *ServerPlan
	for i := range listResp.Servers {
		switch listResp.Servers[i].PlanCode {
		case "24sys02-v3":
			sys2 = &listResp.Servers[i]
		case "24adv05-v3":
			adv5 = &listResp.Servers[i]
		}
	}
	if sys2 == nil || !sys2.HasPromo || sys2.StartingPrice != 30.39 || sys2.OriginalMonthlyPrice != 37.99 || sys2.SetupFee != 34.99 || sys2.FreeSetup || sys2.PromoBadge != "-20%" || sys2.PromoCode != "BF26_ECO_20OFF" {
		t.Fatalf("unexpected 24sys02-v3 ServerPlan promo/setup fields: %+v", sys2)
	}
	if len(sys2.PricingModes) != 2 || sys2.PricingModes[0].Mode != "default" || sys2.PricingModes[1].Mode != "upfront12" {
		t.Fatalf("expected [default, upfront12] PricingModes on 24sys02-v3, got %+v", sys2.PricingModes)
	}
	up12 := sys2.PricingModes[1]
	if up12.PeriodPrice != 346.47 || up12.OriginalPeriodPrice != 433.09 || up12.MonthlyEquivalentPrice != 28.87 || up12.OriginalMonthlyPrice != 36.09 || !up12.FreeSetup || up12.SetupFee != 0 {
		t.Fatalf("unexpected upfront12 mode on 24sys02-v3: %+v", up12)
	}
	if adv5 == nil || !adv5.HasPromo || adv5.SetupFee != 0 || adv5.OriginalSetupFee != 394.99 || !adv5.FreeSetup || adv5.PromoBadge != "FREE SETUP" || adv5.PromoCode != "BF26_OVH_FREESETUP" {
		t.Fatalf("unexpected 24adv05-v3 FREE SETUP promo fields: %+v", adv5)
	}

	// 2. Verify PlanConfigs multi-mode addon calculation for upgraded 64G RAM config
	cfgResp, err := svc.GetPlanConfigs(context.Background(), "ovh-eu", "IE", "24sys02-v3")
	if err != nil {
		t.Fatalf("GetPlanConfigs failed: %v", err)
	}
	if len(cfgResp.Configs) != 2 || !cfgResp.HasPromo || cfgResp.OriginalMonthlyPrice != 37.99 {
		t.Fatalf("unexpected PlanConfigsResponse: %+v", cfgResp)
	}
	upgradedCfg := cfgResp.Configs[1]
	if upgradedCfg.EstimatedMonthlyPrice != 42.39 || upgradedCfg.OriginalMonthlyPrice != 49.99 {
		t.Fatalf("expected upgraded default monthly 30.39+12=42.39 (orig 37.99+12=49.99), got %+v", upgradedCfg)
	}
	if len(upgradedCfg.PricingModes) != 2 || upgradedCfg.PricingModes[1].PeriodPrice != 483.27 || upgradedCfg.PricingModes[1].MonthlyEquivalentPrice != 40.27 {
		t.Fatalf("expected upgraded upfront12 periodPrice 346.47+136.80=483.27 (monthly 40.27), got %+v", upgradedCfg.PricingModes)
	}

	// 3. Verify POST /api/servers/24sys02-v3/price with upfront12 (P12M -> P1Y fallback) and valid coupon
	req := httptest.NewRequest(http.MethodPost, "/api/servers/24sys02-v3/price", strings.NewReader(`{"datacenter":"gra","options":["ram-64g-ecc-2133-24sys02-1g"],"pricingMode":"upfront12","coupon":"BF2026"}`))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /api/servers/24sys02-v3/price failed: %d %s", rec.Code, rec.Body.String())
	}
	var priceRes ServerPriceResult
	_ = json.Unmarshal(rec.Body.Bytes(), &priceRes)
	if !priceRes.Success || priceRes.PricingMode != "upfront12" || priceRes.Coupon != "BF2026" || priceRes.Price == nil || priceRes.Price.MonthlyEquivalentWithoutTax == nil || *priceRes.Price.MonthlyEquivalentWithoutTax != 28.87 {
		t.Fatalf("unexpected priceRes for upfront12: %+v (price=%+v)", priceRes, priceRes.Price)
	}
	if len(cartDurationsTried) < 2 || cartDurationsTried[0] != "P12M" || cartDurationsTried[1] != "P1Y" {
		t.Fatalf("expected AddCartServerItemWithMode to retry P12M -> P1Y, got %v", cartDurationsTried)
	}
	if optionModeChosen != "upfront12" || optionDurChosen != "P12M" {
		t.Fatalf("expected AddCartOptionsWithMode to prefer upfront12/P12M option, got mode=%q dur=%q", optionModeChosen, optionDurChosen)
	}
	if appliedCoupon != "BF2026" {
		t.Fatalf("expected ApplyCartCoupon to post BF2026, got %q", appliedCoupon)
	}

	// 4. Verify rejected coupon returns 400 INVALID_COUPON
	rejectCoupon = true
	reqBad := httptest.NewRequest(http.MethodPost, "/api/servers/24sys02-v3/price", strings.NewReader(`{"datacenter":"gra","coupon":"EXPIRED_CODE"}`))
	recBad := httptest.NewRecorder()
	router.ServeHTTP(recBad, reqBad)
	if recBad.Code != http.StatusBadRequest || !strings.Contains(recBad.Body.String(), "INVALID_COUPON") {
		t.Fatalf("expected 400 INVALID_COUPON on rejected coupon, got %d %s", recBad.Code, recBad.Body.String())
	}
}


