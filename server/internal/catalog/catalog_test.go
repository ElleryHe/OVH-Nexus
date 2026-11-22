package catalog

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
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

	// Verify compressed disk .json.gz cache files were written under <dataDir>/cache
	cacheFiles, _ := filepath.Glob(filepath.Join(dataDir, "cache", "*.json.gz"))
	if len(cacheFiles) < 2 {
		t.Fatalf("expected compressed .json.gz disk cache files in %s/cache, got %v", dataDir, cacheFiles)
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

func TestZeroSetupFeeWithoutPromoAndMultiConfigIsolation(t *testing.T) {
	svc, _, _, _ := newTestCatalog(t)

	mockOVH := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/order/catalog/public/baremetalServers":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"locale": map[string]any{"currencyCode": "EUR"},
				"plans": []map[string]any{
					{
						"planCode":    "21hcisap01-v1",
						"invoiceName": "HGR-SAP-1 | Dual Intel Xeon Gold 6226R",
						"product":     "21hcisap01",
						"blobs":       map[string]any{"commercial": map[string]any{"name": "HGR-SAP-1"}},
						"pricings": []map[string]any{
							{
								"mode":       "default",
								"capacities": []string{"installation"},
								"price":      0,
							},
							{
								"mode":       "default",
								"capacities": []string{"renew"},
								"interval":   1,
								"commitment": 0,
								"price":      160799000000,
							},
						},
						"addonFamilies": []map[string]any{
							{
								"name":    "memory",
								"default": "ram-384g-ecc-2933-21hcisap01",
								"addons":  []string{"ram-384g-ecc-2933-21hcisap01", "ram-768g-ecc-2933-21hcisap01"},
							},
							{
								"name":    "storage",
								"default": "softraid-2x960nvme-21hcisap01",
								"addons":  []string{"softraid-2x960nvme-21hcisap01", "softraid-4x960nvme-21hcisap01"},
							},
						},
					},
					{
						// Another plan whose addon shares product "softraid-2x960nvme" and has a promo
						"planCode":    "24adv01-promo",
						"invoiceName": "ADVANCE-1",
						"product":     "24adv01",
						"pricings": []map[string]any{
							{"mode": "default", "capacities": []string{"renew"}, "interval": 1, "price": 9900000000},
						},
						"addonFamilies": []map[string]any{
							{"name": "memory", "default": "ram-32g-24adv01", "addons": []string{"ram-32g-24adv01"}},
							{"name": "storage", "default": "softraid-2x960nvme-24adv01", "addons": []string{"softraid-2x960nvme-24adv01"}},
						},
					},
				},
				"addons": []map[string]any{
					{
						"planCode": "ram-384g-ecc-2933-21hcisap01",
						"product":  "ram-384g-ecc-2933",
						"pricings": []map[string]any{{"mode": "default", "capacities": []string{"renew"}, "price": 0}},
					},
					{
						"planCode": "ram-768g-ecc-2933-21hcisap01",
						"product":  "ram-768g-ecc-2933",
						"pricings": []map[string]any{{"mode": "default", "capacities": []string{"renew"}, "price": 35000000000}},
					},
					{
						"planCode": "softraid-2x960nvme-21hcisap01",
						"product":  "softraid-2x960nvme",
						"pricings": []map[string]any{{"mode": "default", "capacities": []string{"renew"}, "price": 0}},
					},
					{
						"planCode": "softraid-4x960nvme-21hcisap01",
						"product":  "softraid-4x960nvme",
						"pricings": []map[string]any{{"mode": "default", "capacities": []string{"renew"}, "price": 12000000000}},
					},
					{
						"planCode": "softraid-2x960nvme-24adv01",
						"product":  "softraid-2x960nvme",
						"pricings": []map[string]any{
							{
								"mode":       "default",
								"capacities": []string{"renew"},
								"price":      5000000000,
								"promotions": []map[string]any{
									{"name": "BF26_STORAGE", "value": 20, "type": "percentage", "total": map[string]any{"value": 4000000000}},
								},
							},
						},
					},
				},
			})
		case "/order/catalog/public/eco", "/order/catalog/public/vps":
			_ = json.NewEncoder(w).Encode(map[string]any{"locale": map[string]any{"currencyCode": "EUR"}, "plans": []any{}})
		case "/dedicated/server/datacenter/availabilities":
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{
					"fqn":         "21hcisap01-v1.ram-768g-ecc-2933.softraid-4x960nvme",
					"planCode":    "21hcisap01-v1",
					"server":      "21hcisap01",
					"memory":      "ram-768g-ecc-2933",
					"storage":     "softraid-4x960nvme",
					"datacenters": []map[string]any{{"datacenter": "gra", "availability": "1H-low"}},
				},
				{
					"fqn":         "21hcisap01-v1.ram-384g-ecc-2933.softraid-2x960nvme",
					"planCode":    "21hcisap01-v1",
					"server":      "21hcisap01",
					"memory":      "ram-384g-ecc-2933",
					"storage":     "softraid-2x960nvme",
					"datacenters": []map[string]any{{"datacenter": "sbg", "availability": "1H-low"}},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer mockOVH.Close()
	svc.SetPublicAPIBaseURL(mockOVH.URL)

	listResp, err := svc.ListServers(context.Background(), ListServersOptions{Endpoint: "ovh-eu", Zone: "IE", ForceRefresh: true})
	if err != nil {
		t.Fatalf("ListServers failed: %v", err)
	}
	var sap *ServerPlan
	for i := range listResp.Servers {
		if listResp.Servers[i].PlanCode == "21hcisap01-v1" {
			sap = &listResp.Servers[i]
			break
		}
	}
	if sap == nil {
		t.Fatalf("expected 21hcisap01-v1 in ListServers")
	}
	if sap.HasPromo {
		t.Fatalf("expected 21hcisap01-v1 (originalSetupFee=0, no promotions) to have HasPromo=false, got %+v", sap)
	}
	if !sap.FreeSetup || sap.SetupFee != 0 || sap.OriginalSetupFee != 0 {
		t.Fatalf("expected FreeSetup=true with SetupFee=0, OriginalSetupFee=0, got %+v", sap)
	}
	if sap.StartingPrice != 1607.99 || sap.OriginalMonthlyPrice != 1607.99 {
		t.Fatalf("expected StartingPrice == OriginalMonthlyPrice == 1607.99 from same config, got start=%v orig=%v", sap.StartingPrice, sap.OriginalMonthlyPrice)
	}

	availItems, err := svc.GetCatalogAvailabilities(context.Background(), "ovh-eu", "IE", "21hcisap01-v1")
	if err != nil {
		t.Fatalf("GetCatalogAvailabilities failed: %v", err)
	}
	if len(availItems) != 2 {
		t.Fatalf("expected 2 availability items for 21hcisap01-v1, got %d", len(availItems))
	}
	for _, it := range availItems {
		if it.HasPromo {
			t.Fatalf("expected AvailabilityItem %s to have HasPromo=false, got %+v", it.ConfigKey, it)
		}
		if it.EstimatedMonthlyPrice != it.OriginalMonthlyPrice {
			t.Fatalf("expected EstimatedMonthlyPrice == OriginalMonthlyPrice on non-promo config %s, got %v vs %v", it.ConfigKey, it.EstimatedMonthlyPrice, it.OriginalMonthlyPrice)
		}
	}
}

func TestV140CacheSlimmingSHA256ShortCircuitPromotionsRadarAndAutoPromoGuard(t *testing.T) {
	dataDir := t.TempDir()
	cacheDir := filepath.Join(dataDir, "cache")
	if err := os.MkdirAll(cacheDir, 0o700); err != nil {
		t.Fatalf("mkdir cacheDir: %v", err)
	}
	// Seed legacy v1/v2/v3/vps files that NewService must automatically prune on startup.
	legacyFiles := []string{
		"catalog_dict_ovh-eu_IE.json",
		"catalog_dict_v2_ovh-eu_IE.json",
		"catalog_dict_v3_ovh-eu_IE.json",
		"servers_v3_ovh-eu_IE.json",
		"vps_catalog_ovh-eu_IE.json",
		"catalog_dict_v4_ovh-eu_IE.json.gz.tmp",
	}
	for _, lf := range legacyFiles {
		if err := os.WriteFile(filepath.Join(cacheDir, lf), []byte(`{"stale":true}`), 0o600); err != nil {
			t.Fatalf("seed legacy file %s: %v", lf, err)
		}
	}

	svc := NewService(nil, nil, nil, dataDir)
	for _, lf := range legacyFiles {
		if _, err := os.Stat(filepath.Join(cacheDir, lf)); !os.IsNotExist(err) {
			t.Fatalf("expected legacy cache file %s to be pruned on NewService startup", lf)
		}
	}

	// Build mock OVH public API serving dual-channel Black Friday promos + heavy software license junk.
	mockOVH := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/order/catalog/public/eco":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"locale": map[string]any{"currencyCode": "EUR"},
				"addons": []map[string]any{
					{
						"planCode": "ram-64g-ecc-2133-24sys02-v3",
						"product":  "ram-64g-ecc-2133",
						"pricings": []map[string]any{
							{"mode": "default", "capacities": []string{"renew"}, "price": 1200000000, "interval": 1},
						},
					},
					{
						"planCode": "windows-2022-std-24sys02-v3",
						"product":  "windows-2022-std",
						"pricings": []map[string]any{
							{"mode": "default", "capacities": []string{"renew"}, "price": 2500000000, "interval": 1},
						},
					},
				},
				"plans": []map[string]any{
					{
						"planCode":    "24sys02-v3",
						"product":     "24sys02-1g",
						"invoiceName": "SYS-2 | Intel Xeon E-2386G",
						"pricings": []map[string]any{
							{
								"mode":       "default",
								"capacities": []string{"renew"},
								"price":      3799000000,
								"interval":   1,
								"promotions": []map[string]any{
									{
										"name":            "BF26_ECO_20OFF",
										"description":     "Black Friday : 20% off for 12 months on SYS-2 server.",
										"context":         "standard",
										"type":            "percentage",
										"value":           20,
										"duration":        12,
										"minimumDuration": 2,
										"startDate":       "2026-11-17T09:00:00+01:00",
										"endDate":         "2026-12-01T14:00:00+01:00",
										"discount":        map[string]any{"value": 759800000},
										"total":           map[string]any{"value": 3039200000},
									},
								},
							},
							{
								"mode":       "upfront24",
								"capacities": []string{"renew"},
								"price":      82000000000,
								"interval":   24,
								"commitment": 24,
							},
						},
						"addonFamilies": []map[string]any{
							{"name": "memory", "default": "ram-32g-ecc-2133-24sys02-v3", "addons": []string{"ram-32g-ecc-2133-24sys02-v3", "ram-64g-ecc-2133-24sys02-v3"}},
							{"name": "storage", "default": "softraid-2x512nvme-24sys02-v3", "addons": []string{"softraid-2x512nvme-24sys02-v3"}},
							{"name": "application-license", "addons": []string{"windows-2022-std-24sys02-v3", "cpanel-pro-100"}},
							{"name": "distribution-license", "addons": []string{"cloudlinux-os-24sys02-v3"}},
						},
					},
				},
			})
		case "/order/catalog/public/baremetalServers":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"locale": map[string]any{"currencyCode": "EUR"},
				"plans": []map[string]any{
					{
						"planCode":    "26game01-v4",
						"product":     "26game01",
						"invoiceName": "GAME-1 | AMD Ryzen 5 5600X",
						"pricings": []map[string]any{
							{
								"mode":       "default",
								"capacities": []string{"renew"},
								"price":      8000000000,
								"interval":   1,
								"promotions": []map[string]any{
									{
										"name":            "BF26_OVH_GAME_15OFF",
										"description":     "Black Friday : 15% off for 12 months on GAME-1.",
										"context":         "standard",
										"type":            "percentage",
										"value":           15,
										"duration":        12,
										"minimumDuration": 2,
										"endDate":         "2026-12-01T14:00:00+01:00",
									},
								},
							},
							{
								"mode":       "default",
								"capacities": []string{"installation"},
								"price":      10000000000,
								"promotions": []map[string]any{
									{
										"name":        "BF26_OVH_GAME_15OFF_SETUP",
										"description": "Black Friday : 15% off on GAME-1 setup fees.",
										"context":     "standard",
										"type":        "percentage",
										"value":       15,
										"duration":    1,
										"endDate":     "2026-12-01T14:00:00+01:00",
									},
								},
							},
						},
						"addonFamilies": []map[string]any{
							{"name": "memory", "default": "ram-32g-ecc-3200-26game01-v4", "addons": []string{"ram-32g-ecc-3200-26game01-v4"}},
							{"name": "storage", "default": "softraid-2x960nvme-26game01-v4", "addons": []string{"softraid-2x960nvme-26game01-v4"}},
						},
					},
				},
			})
		case "/dedicated/server/datacenter/availabilities":
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{
					"fqn":      "24sys02-v3.ram-32g.softraid-2x512nvme",
					"planCode": "24sys02-v3",
					"memory":   "ram-32g-ecc-2133",
					"storage":  "softraid-2x512nvme",
					"datacenters": []map[string]any{
						{"datacenter": "gra", "availability": "1H"},
					},
				},
				{
					"fqn":      "26game01-v4.ram-32g.softraid-2x960nvme",
					"planCode": "26game01-v4",
					"memory":   "ram-32g-ecc-3200",
					"storage":  "softraid-2x960nvme",
					"datacenters": []map[string]any{
						{"datacenter": "rbx", "availability": "unavailable"},
					},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer mockOVH.Close()
	svc.SetPublicAPIBaseURL(mockOVH.URL)

	// 1. First baremetal refresh populates deduplicated RealPlans + PlanAliases and strips software licenses.
	dict1, err := svc.GetBaremetalDictionary(context.Background(), "ovh-eu", "IE", true)
	if err != nil {
		t.Fatalf("first GetBaremetalDictionary failed: %v", err)
	}
	if len(dict1.RealPlans) != 2 {
		t.Fatalf("expected 2 RealPlans, got %d", len(dict1.RealPlans))
	}
	if len(dict1.Plans) != 0 {
		t.Fatalf("expected 0 duplicate entries in Plans (aliases belong in PlanAliases), got %+v", dict1.Plans)
	}
	if dict1.PlanAliases["24sys02-1g"] != "24sys02-v3" || dict1.PlanAliases["24sys02"] != "24sys02-v3" {
		t.Fatalf("expected PlanAliases to map 24sys02-1g and 24sys02 -> 24sys02-v3, got %+v", dict1.PlanAliases)
	}
	if _, leaked := dict1.AddonPrices["windows-2022-std-24sys02-v3"]; leaked {
		t.Fatalf("expected software license addon to be filtered out from AddonPrices")
	}
	sys2 := dict1.RealPlans["24sys02-v3"]
	for _, opt := range sys2.AvailableOptions {
		if strings.Contains(opt.Family, "license") || strings.Contains(opt.Value, "windows") || strings.Contains(opt.Value, "cpanel") || strings.Contains(opt.Value, "cloudlinux") {
			t.Fatalf("expected software license option to be filtered from AvailableOptions, got %+v", opt)
		}
	}

	// Verify baremetal dictionary writes to its isolated compressed .json.gz file (never clobbering full dict).
	dictPath := svc.bmDictCachePath("ovh-eu", "IE")
	stat1, err := os.Stat(dictPath)
	if err != nil {
		t.Fatalf("expected compressed baremetal dict cache file %s: %v", dictPath, err)
	}

	// 2. Second forceRefresh with unchanged upstream JSON must short-circuit via SHA-256 without rewriting disk
	// and return an immutable shallow copy (preventing concurrent mutation races on UpdatedAt).
	time.Sleep(20 * time.Millisecond)
	dict2, err := svc.GetBaremetalDictionary(context.Background(), "ovh-eu", "IE", true)
	if err != nil {
		t.Fatalf("second GetBaremetalDictionary failed: %v", err)
	}
	if dict2 == dict1 || len(dict2.RealPlans) != len(dict1.RealPlans) {
		t.Fatalf("expected SHA-256 short-circuit to publish immutable shallow copy sharing RealPlans")
	}
	stat2, err := os.Stat(dictPath)
	if err != nil {
		t.Fatalf("stat dictPath after short-circuit: %v", err)
	}
	if !stat2.ModTime().Equal(stat1.ModTime()) {
		t.Fatalf("expected SHA-256 short-circuit to skip disk write (mtime changed %v -> %v)", stat1.ModTime(), stat2.ModTime())
	}

	// 3. Verify GET /api/catalog/dictionary returns wire-slimmed projection (no AvailableOptions) and GET /api/catalog/promotions returns dual-channel campaigns.
	r := chi.NewRouter()
	svc.MountRoutes(r)

	wDict := httptest.NewRecorder()
	r.ServeHTTP(wDict, httptest.NewRequest(http.MethodGet, "/catalog/dictionary?subsidiary=IE", nil))
	if wDict.Code != http.StatusOK {
		t.Fatalf("GET /catalog/dictionary failed: %d %s", wDict.Code, wDict.Body.String())
	}
	if strings.Contains(wDict.Body.String(), `"availableOptions"`) {
		t.Fatalf("expected GET /catalog/dictionary wire projection to omit availableOptions")
	}

	wPromo := httptest.NewRecorder()
	r.ServeHTTP(wPromo, httptest.NewRequest(http.MethodGet, "/catalog/promotions?subsidiary=IE", nil))
	if wPromo.Code != http.StatusOK {
		t.Fatalf("GET /catalog/promotions failed: %d %s", wPromo.Code, wPromo.Body.String())
	}
	var promoResp CatalogPromotionsResponse
	if err := json.Unmarshal(wPromo.Body.Bytes(), &promoResp); err != nil {
		t.Fatalf("decode /catalog/promotions: %v", err)
	}
	// Must capture all 3 campaigns including BOTH BF26_OVH_GAME_15OFF and BF26_OVH_GAME_15OFF_SETUP on 26game01-v4!
	if promoResp.TotalCampaigns != 3 || promoResp.TotalPromotedPlans != 2 || promoResp.InStockPlans != 1 {
		t.Fatalf("unexpected promotions summary: campaigns=%d plans=%d inStock=%d (%+v)", promoResp.TotalCampaigns, promoResp.TotalPromotedPlans, promoResp.InStockPlans, promoResp)
	}
	if len(promoResp.AutoAppliedCodes) != 3 {
		t.Fatalf("expected 3 autoAppliedCodes, got %+v", promoResp.AutoAppliedCodes)
	}
	var sys2Promo, game1Promo *CatalogPromoPlan
	for i := range promoResp.Plans {
		switch promoResp.Plans[i].PlanCode {
		case "24sys02-v3":
			sys2Promo = &promoResp.Plans[i]
		case "26game01-v4":
			game1Promo = &promoResp.Plans[i]
		}
	}
	if sys2Promo == nil || !slices.Contains(sys2Promo.SupportedPromoModes, "default") || !slices.Contains(sys2Promo.MissedPromoModes, "upfront24") || sys2Promo.AddonsDiscounted {
		t.Fatalf("unexpected 24sys02-v3 promo plan breakdown: %+v", sys2Promo)
	}
	if game1Promo == nil || !game1Promo.HasRecurringPromo || !game1Promo.HasSetupPromo || len(game1Promo.PromoCodes) != 2 {
		t.Fatalf("expected 26game01-v4 dual-channel promo (renew + setup), got %+v", game1Promo)
	}
	if game1Promo.PricingModes[0].RenewPromo == nil || game1Promo.PricingModes[0].SetupPromo == nil {
		t.Fatalf("expected both RenewPromo and SetupPromo populated on 26game01-v4 PricingModes[0], got %+v", game1Promo.PricingModes[0])
	}

	// Verify LookupAutoAppliedPromo detects standard auto-applied campaign codes.
	camp, isAuto := svc.LookupAutoAppliedPromo(context.Background(), "ovh-eu", "IE", "bf26_eco_20off")
	if !isAuto || camp == nil || camp.PromoCode != "BF26_ECO_20OFF" {
		t.Fatalf("expected LookupAutoAppliedPromo to detect BF26_ECO_20OFF, got isAuto=%v camp=%+v", isAuto, camp)
	}

	// Verify LookupAutoAppliedPromoForPlan matches strictly by (planCode, pricingMode) and never cross-strips.
	planCamp, planAuto := svc.LookupAutoAppliedPromoForPlan(context.Background(), "ovh-eu", "IE", "24sys02-v3", "default", "bf26_eco_20off")
	if !planAuto || planCamp == nil || planCamp.PromoCode != "BF26_ECO_20OFF" {
		t.Fatalf("expected LookupAutoAppliedPromoForPlan to match 24sys02-v3+default, got isAuto=%v camp=%+v", planAuto, planCamp)
	}
	if _, wrongModeAuto := svc.LookupAutoAppliedPromoForPlan(context.Background(), "ovh-eu", "IE", "24sys02-v3", "upfront24", "BF26_ECO_20OFF"); wrongModeAuto {
		t.Fatalf("expected LookupAutoAppliedPromoForPlan NOT to match unsupported pricingMode upfront24 on 24sys02-v3")
	}
	if _, wrongPlanAuto := svc.LookupAutoAppliedPromoForPlan(context.Background(), "ovh-eu", "IE", "26game01-v4", "default", "BF26_ECO_20OFF"); wrongPlanAuto {
		t.Fatalf("expected LookupAutoAppliedPromoForPlan NOT to match cross-model planCode 26game01-v4")
	}
}

func TestSecurityHotfixGzipPartialOutageConcurrentWritePromosAndThrottle(t *testing.T) {
	svc, st, dataDir, router := newTestCatalog(t)
	ctx := context.Background()

	var failBM atomic.Bool
	writeGzip := func(w http.ResponseWriter, body string) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Encoding", "gzip")
		gz := gzip.NewWriter(w)
		_, _ = gz.Write([]byte(body))
		_ = gz.Close()
	}

	pastEnd := time.Now().Add(-2 * time.Hour).UTC().Format(time.RFC3339)
	futureStart := time.Now().Add(48 * time.Hour).UTC().Format(time.RFC3339)
	validEnd := time.Now().Add(72 * time.Hour).UTC().Format(time.RFC3339)

	ecoJSON := fmt.Sprintf(`{
		"locale":{"currencyCode":"EUR"},
		"plans":[{
			"planCode":"24eco-commit-v1",
			"product":"24eco-commit",
			"invoiceName":"Eco Commit",
			"pricings":[
				{
					"mode":"default","capacities":["renew"],"price":5000000000,"interval":1,"commitment":0,
					"promotions":[
						{"name":"EXPIRED_PROMO","context":"standard","type":"percentage","value":50,"endDate":%q},
						{"name":"FUTURE_PROMO","context":"standard","type":"percentage","value":40,"startDate":%q,"endDate":%q},
						{"name":"SOLDOUT_PROMO","context":"standard","type":"percentage","value":30,"isGlobalQuantityLimited":true,"quantity":0,"endDate":%q},
						{"name":"VIP_VOUCHER","context":"voucher","type":"percentage","value":25,"endDate":%q}
					]
				},
				{
					"mode":"upfront12","capacities":["renew"],"price":60000000000,"interval":12,"commitment":12,
					"promotions":[
						{"name":"COMMIT_12M_20OFF","context":"standard","type":"percentage","value":20,"endDate":%q}
					]
				}
			]
		}]
	}`, pastEnd, futureStart, validEnd, validEnd, validEnd, validEnd)
	bmJSON := `{"locale":{"currencyCode":"EUR"},"plans":[{"planCode":"24rise01","product":"24rise01","invoiceName":"RISE-1","pricings":[{"mode":"default","capacities":["renew"],"price":9000000000,"interval":1}]}]}`
	vpsJSON := `{"locale":{"currencyCode":"EUR"},"plans":[{"planCode":"vps-starter","product":"vps-starter","invoiceName":"VPS Starter","pricings":[{"mode":"default","capacities":["renew"],"price":500000000,"interval":1}]}]}`
	availJSON := `[{"fqn":"24eco-commit-v1","planCode":"24eco-commit-v1","server":"24eco-commit","memory":"ram-32g","storage":"softraid-2x480ssd","datacenters":[{"datacenter":"gra","availability":"1H-high"}]}]`

	mockOVH := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/order/catalog/public/eco"):
			writeGzip(w, ecoJSON)
		case strings.Contains(r.URL.Path, "/order/catalog/public/baremetalServers"):
			if failBM.Load() {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			writeGzip(w, bmJSON)
		case strings.Contains(r.URL.Path, "/order/catalog/public/vps"):
			writeGzip(w, vpsJSON)
		case strings.Contains(r.URL.Path, "/dedicated/server/datacenter/availabilities"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(availJSON))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer mockOVH.Close()
	svc.SetPublicAPIBaseURL(mockOVH.URL)

	// 1. Gzip fetch + isolated full vs bm caches: VPS plan in full dict survives baremetal refresh!
	fullDict, err := svc.GetDictionary(ctx, "ovh-eu", "IE", true)
	if err != nil || len(fullDict.RealPlans) != 2 || len(fullDict.Plans) != 1 {
		t.Fatalf("expected 2 baremetal + 1 vps plan in full dict, got err=%v real=%d extra=%d", err, len(fullDict.RealPlans), len(fullDict.Plans))
	}
	bmDict, err := svc.GetBaremetalDictionary(ctx, "ovh-eu", "IE", true)
	if err != nil || len(bmDict.RealPlans) != 2 {
		t.Fatalf("expected 2 real plans in bm dict, got err=%v real=%d", err, len(bmDict.RealPlans))
	}
	fullAfter, _ := svc.GetDictionary(ctx, "ovh-eu", "IE", false)
	if _, ok := fullAfter.Plans["vps-starter"]; !ok {
		t.Fatalf("expected full dictionary to preserve vps-starter independently of bm dictionary")
	}

	// 2. Promo filtering + voucher separation + commitment-only savings + monthly-normalized MaxSavingsAmount.
	ecoPlan := bmDict.RealPlans["24eco-commit-v1"]
	if ecoPlan.PricingModes[0].Mode != "default" || ecoPlan.PricingModes[0].PeriodPrice != 50 || ecoPlan.PricingModes[0].HasPromo {
		t.Fatalf("expected expired/future/soldout/voucher promos NOT to discount automatic default price, got %+v", ecoPlan.PricingModes[0])
	}
	if len(ecoPlan.PricingModes[0].VoucherPromos) != 1 || ecoPlan.PricingModes[0].VoucherPromos[0].Code != "VIP_VOUCHER" {
		t.Fatalf("expected VIP_VOUCHER recorded in VoucherPromos without auto-applying, got %+v", ecoPlan.PricingModes[0].VoucherPromos)
	}
	promoResp, err := svc.GetPromotions(ctx, PromotionsQueryOptions{Endpoint: "ovh-eu", Zone: "IE"})
	if err != nil || len(promoResp.Plans) != 1 {
		t.Fatalf("unexpected promotions response: err=%v resp=%+v", err, promoResp)
	}
	p0 := promoResp.Plans[0]
	if p0.MonthlySavings != 10 || p0.BestPromoMode != "upfront12" {
		t.Fatalf("expected commitment-only upfront12 promo to report MonthlySavings=10 & BestPromoMode=upfront12, got %+v", p0)
	}
	for _, c := range promoResp.Campaigns {
		if c.PromoCode == "COMMIT_12M_20OFF" && c.MaxSavingsAmount != 10 {
			t.Fatalf("expected 12m campaign MaxSavingsAmount normalized to monthly equivalent 10, got %.2f", c.MaxSavingsAmount)
		}
		if c.PromoCode == "EXPIRED_PROMO" || c.PromoCode == "FUTURE_PROMO" || c.PromoCode == "SOLDOUT_PROMO" {
			t.Fatalf("inactive promo %s leaked into campaigns", c.PromoCode)
		}
	}

	// 3. Cold-start disk digest recovery + partial outage guard in BOTH GetBaremetalDictionary AND buildServerPlans!
	svcRestart := NewService(st, nil, ovhclient.NewPool(st, nil), dataDir)
	svcRestart.SetPublicAPIBaseURL(mockOVH.URL)
	failBM.Store(true)
	bmSafe, err := svcRestart.GetBaremetalDictionary(ctx, "ovh-eu", "IE", true)
	if err != nil || len(bmSafe.RealPlans) != 2 {
		t.Fatalf("expected cold-start partial outage guard to preserve both plans from disk digest, got err=%v len=%d", err, len(bmSafe.RealPlans))
	}
	srvList, err := svcRestart.ListServers(ctx, ListServersOptions{Endpoint: "ovh-eu", Zone: "IE", ForceRefresh: true})
	if err != nil || len(srvList.Servers) < 2 {
		t.Fatalf("expected buildServerPlans partial outage guard to preserve complete dictionary, got err=%v count=%d", err, len(srvList.Servers))
	}
	failBM.Store(false)

	// 4. Concurrent writeGzipJSONFile safety (P0-03) under -race.
	concurrentPath := filepath.Join(dataDir, "cache", "concurrent_atomic.json.gz")
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			_ = writeGzipJSONFile(concurrentPath, map[string]int{"seq": idx})
		}(i)
	}
	wg.Wait()
	if got, ok := readGzipJSONFile[map[string]int](concurrentPath); !ok || got == nil {
		t.Fatalf("expected valid readable gzip JSON after concurrent writes")
	}
	entries, _ := os.ReadDir(filepath.Join(dataDir, "cache"))
	for _, ent := range entries {
		if strings.HasSuffix(ent.Name(), ".tmp") {
			t.Fatalf("unexpected leftover temp file: %s", ent.Name())
		}
	}

	// 5. GET /servers/{planCode}/price removed (405), /cache/info omits host absolute path, rapid refresh throttled (429).
	recGetPrice := httptest.NewRecorder()
	router.ServeHTTP(recGetPrice, httptest.NewRequest(http.MethodGet, "/api/servers/24eco-commit-v1/price", nil))
	if recGetPrice.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405 Method Not Allowed for GET /servers/{planCode}/price, got %d", recGetPrice.Code)
	}
	recInfo := httptest.NewRecorder()
	router.ServeHTTP(recInfo, httptest.NewRequest(http.MethodGet, "/api/cache/info", nil))
	if strings.Contains(recInfo.Body.String(), dataDir) {
		t.Fatalf("expected /api/cache/info NOT to leak host absolute path %q: %s", dataDir, recInfo.Body.String())
	}
	recRef1 := httptest.NewRecorder()
	router.ServeHTTP(recRef1, httptest.NewRequest(http.MethodPost, "/api/servers/refresh", nil))
	if recRef1.Code != http.StatusOK {
		t.Fatalf("first POST /api/servers/refresh failed: %d", recRef1.Code)
	}
	recRef2 := httptest.NewRecorder()
	router.ServeHTTP(recRef2, httptest.NewRequest(http.MethodPost, "/api/servers/refresh", nil))
	if recRef2.Code != http.StatusTooManyRequests || recRef2.Header().Get("Retry-After") == "" {
		t.Fatalf("expected second rapid POST /api/servers/refresh to return 429 with Retry-After, got %d hdr=%v", recRef2.Code, recRef2.Header())
	}
	// Per-endpoint/zone bucketing: /catalog/promotions?refresh=1 and another zone (FR) must NOT collide with servers:ovh-eu:IE!
	recPromoRef := httptest.NewRecorder()
	router.ServeHTTP(recPromoRef, httptest.NewRequest(http.MethodGet, "/api/catalog/promotions?refresh=1", nil))
	if recPromoRef.Code != http.StatusOK {
		t.Fatalf("expected GET /api/catalog/promotions?refresh=1 to use its own throttle bucket (200), got %d body=%s", recPromoRef.Code, recPromoRef.Body.String())
	}
	recPromoRef2 := httptest.NewRecorder()
	router.ServeHTTP(recPromoRef2, httptest.NewRequest(http.MethodGet, "/api/catalog/promotions?refresh=1", nil))
	if recPromoRef2.Code != http.StatusTooManyRequests {
		t.Fatalf("expected second rapid GET /api/catalog/promotions?refresh=1 to return 429, got %d", recPromoRef2.Code)
	}
	recRefFR := httptest.NewRecorder()
	router.ServeHTTP(recRefFR, httptest.NewRequest(http.MethodPost, "/api/servers/refresh?zone=FR", nil))
	if recRefFR.Code != http.StatusOK {
		t.Fatalf("expected POST /api/servers/refresh?zone=FR to use an independent zone bucket (200), got %d", recRefFR.Code)
	}
}

func TestGetServerPriceWithModeDeductsSetupFeeFromMonthlyEquivalent(t *testing.T) {
	svc, _, _, _ := newTestCatalog(t)
	var freeFirstMonth atomic.Bool
	mockOVH := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/auth/time":
			_, _ = w.Write([]byte("1763200000"))
		case r.Method == http.MethodPost && r.URL.Path == "/order/cart":
			_, _ = w.Write([]byte(`{"cartId":"cart-setup-1"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/order/cart/cart-setup-1/assign":
			_, _ = w.Write([]byte(`{}`))
		case r.Method == http.MethodPost && (r.URL.Path == "/order/cart/cart-setup-1/baremetalServers" || r.URL.Path == "/order/cart/cart-setup-1/eco"):
			_, _ = w.Write([]byte(`{"itemId":1}`))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/configuration"):
			_, _ = w.Write([]byte(`{}`))
		case r.Method == http.MethodGet && r.URL.Path == "/order/cart/cart-setup-1/summary":
			if freeFirstMonth.Load() {
				// 0.00 EUR recurring + 50.00 EUR setup = 50.00 EUR total ex-VAT (no DURATION line).
				_ = json.NewEncoder(w).Encode(map[string]any{
					"prices": map[string]any{
						"withoutTax":   map[string]any{"value": 50.00, "currencyCode": "EUR"},
						"withTax":      map[string]any{"value": 60.00, "currencyCode": "EUR"},
						"currencyCode": "EUR",
					},
					"details": []map[string]any{
						{"detailType": "INSTALLATION", "description": "Setup", "totalPrice": map[string]any{"value": 50.00, "currencyCode": "EUR"}},
					},
				})
				return
			}
			// 30.00 EUR monthly recurring + 50.00 EUR setup = 80.00 EUR total ex-VAT (96.00 EUR with tax).
			_ = json.NewEncoder(w).Encode(map[string]any{
				"prices": map[string]any{
					"withoutTax":   map[string]any{"value": 80.00, "currencyCode": "EUR"},
					"withTax":      map[string]any{"value": 96.00, "currencyCode": "EUR"},
					"tax":          map[string]any{"value": 16.00, "currencyCode": "EUR"},
					"currencyCode": "EUR",
				},
				"details": []map[string]any{
					{"detailType": "DURATION", "description": "Server", "totalPrice": map[string]any{"value": 30.00, "currencyCode": "EUR"}},
					{"detailType": "INSTALLATION", "description": "Setup", "totalPrice": map[string]any{"value": 50.00, "currencyCode": "EUR"}},
				},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/order/cart/cart-setup-1":
			_, _ = w.Write([]byte(`{"items":[]}`))
		case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/order/cart/"):
			_, _ = w.Write([]byte(`{}`))
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

	res, err := svc.GetServerPriceWithMode(context.Background(), "acc-1", "24ska01", "gra", nil, "default", "P1M", "")
	if err != nil || res == nil || !res.Success || res.Price == nil {
		t.Fatalf("GetServerPriceWithMode failed: err=%v res=%+v", err, res)
	}
	if res.Price.MonthlyEquivalentWithoutTax == nil || *res.Price.MonthlyEquivalentWithoutTax != 30.00 {
		t.Fatalf("expected MonthlyEquivalentWithoutTax=30.00 (excluding 50.00 setup fee), got %+v", res.Price.MonthlyEquivalentWithoutTax)
	}
	if res.Price.SetupFeeWithoutTax == nil || *res.Price.SetupFeeWithoutTax != 50.00 {
		t.Fatalf("expected SetupFeeWithoutTax=50.00, got %+v", res.Price.SetupFeeWithoutTax)
	}

	// Setup-only total (totalEx == setupExVAT) MUST NOT report setup fee as MonthlyEquivalentWithoutTax.
	freeFirstMonth.Store(true)
	resSetupOnly, err := svc.GetServerPriceWithMode(context.Background(), "acc-1", "24ska01", "gra", nil, "default", "P1M", "")
	if err != nil || resSetupOnly == nil || !resSetupOnly.Success || resSetupOnly.Price == nil {
		t.Fatalf("GetServerPriceWithMode setup-only failed: err=%v res=%+v", err, resSetupOnly)
	}
	if resSetupOnly.Price.MonthlyEquivalentWithoutTax != nil {
		t.Fatalf("expected nil MonthlyEquivalentWithoutTax when totalEx == setupFeeWithoutTax, got %v", *resSetupOnly.Price.MonthlyEquivalentWithoutTax)
	}
}



