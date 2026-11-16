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
