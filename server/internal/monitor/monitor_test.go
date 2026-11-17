package monitor

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/ovh/go-ovh/ovh"

	"ovh-nexus/server/internal/catalog"
	"ovh-nexus/server/internal/logger"
	"ovh-nexus/server/internal/notify"
	"ovh-nexus/server/internal/ovhclient"
	"ovh-nexus/server/internal/procurement"
	"ovh-nexus/server/internal/store"
	"ovh-nexus/server/internal/vault"
)

type capturedTelegram struct {
	mu       sync.Mutex
	messages []string
}

func (c *capturedTelegram) record(text string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.messages = append(c.messages, text)
}

func (c *capturedTelegram) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.messages)
}

func (c *capturedTelegram) slice() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]string, len(c.messages))
	copy(out, c.messages)
	return out
}

type monitorTestEnv struct {
	svc    *Service
	proc   *procurement.Service
	cat    *catalog.Service
	disp   *notify.Dispatcher
	st     *store.Store
	router chi.Router
	tg     *capturedTelegram
}

func newMonitorTestEnv(t *testing.T, mockOVH *httptest.Server) *monitorTestEnv {
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

	var logBuf bytes.Buffer
	hub := logger.NewHub(st, logger.Options{Level: "DEBUG", Format: "text", Output: &logBuf})
	pool := ovhclient.NewPool(st, hub)

	tgCap := &capturedTelegram{}
	tgServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if txt, ok := body["text"].(string); ok {
			tgCap.record(txt)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": map[string]any{"message_id": 1}})
	}))
	t.Cleanup(tgServer.Close)

	disp := notify.NewDispatcher(st, v, hub, pool)
	disp.SetTelegramBaseURL(tgServer.URL)
	if _, err := st.UpsertChannel(context.Background(), store.NotificationChannel{
		ID:      "tg-mon",
		Type:    "telegram",
		Name:    "Monitor Alerts",
		Enabled: true,
		Events: []string{
			notify.EventStockAvailable,
			notify.EventStockUnavailable,
			notify.EventSKUChanged,
			notify.EventOrderDelivered,
			notify.EventServiceExpiring,
		},
		Config: map[string]any{
			"token":  "123:test-token",
			"chatId": "-100123",
		},
	}); err != nil {
		t.Fatalf("seed notification channel: %v", err)
	}

	cat := catalog.NewService(st, hub, pool, dataDir)
	proc := procurement.NewService(st, hub, pool, cat, disp)
	svc := NewService(st, hub, pool, cat, proc, disp)
	svc.SetStockInterval(time.Hour) // prevent unintended ticker fires during manual tests

	if mockOVH != nil {
		cat.SetPublicAPIBaseURL(mockOVH.URL)
		clientFn := func(_ context.Context, _ string) (*ovh.Client, *store.OVHAccount, error) {
			c, err := ovh.NewClient(mockOVH.URL, "ak-test", "as-test", "ck-test")
			if err != nil {
				return nil, nil, err
			}
			return c, &store.OVHAccount{ID: "acc-1", Endpoint: "ovh-eu", Zone: "IE"}, nil
		}
		cat.SetAccountClientFunc(clientFn)
		proc.SetAccountClientFunc(clientFn)
		svc.SetAccountClientFunc(clientFn)
		_, _ = st.UpsertAccount(context.Background(), store.OVHAccount{
			ID:          "acc-1",
			Alias:       "Primary EU",
			Endpoint:    "ovh-eu",
			Zone:        "IE",
			AppKey:      "ak-test",
			AppSecret:   "as-test",
			ConsumerKey: "ck-test",
			IsDefault:   true,
		})
	}

	t.Cleanup(func() {
		svc.StopStockMonitor()
		svc.StopVPSMonitor()
		svc.StopSKUWatcher()
		svc.StopLifecycleWatcher()
	})

	r := chi.NewRouter()
	r.Route("/api", func(api chi.Router) {
		svc.MountRoutes(api)
		proc.MountRoutes(api)
	})

	return &monitorTestEnv{
		svc:    svc,
		proc:   proc,
		cat:    cat,
		disp:   disp,
		st:     st,
		router: r,
		tg:     tgCap,
	}
}

func TestStockMonitorTransitionsAutoOrderAndRoutes(t *testing.T) {
	var graStatus atomic.Value
	graStatus.Store("unavailable")

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
						"planCode":    "24ska01",
						"invoiceName": "KS-A | Intel i7-6700k",
						"blobs":       map[string]any{"commercial": map[string]any{"name": "KS-A"}},
						"pricings":    []map[string]any{{"mode": "default", "capacities": []string{"renew"}, "price": 5499000000}},
						"addonFamilies": []map[string]any{
							{"name": "memory", "default": "ram-32g-noecc-2133-24ska01", "addons": []string{"ram-32g-noecc-2133-24ska01"}},
							{"name": "storage", "default": "softraid-2x480ssd-24ska01", "addons": []string{"softraid-2x480ssd-24ska01"}},
						},
					},
				},
			})
		case r.URL.Path == "/order/catalog/public/baremetalServers" || r.URL.Path == "/order/catalog/public/vps":
			_ = json.NewEncoder(w).Encode(map[string]any{"locale": map[string]any{"currencyCode": "EUR"}, "plans": []any{}})
		case r.URL.Path == "/dedicated/server/datacenter/availabilities":
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{
					"fqn":      "24ska01.ram-32g-noecc-2133.softraid-2x480ssd",
					"planCode": "24ska01",
					"memory":   "ram-32g-noecc-2133",
					"storage":  "softraid-2x480ssd",
					"datacenters": []map[string]any{
						{"datacenter": "gra", "availability": graStatus.Load().(string)},
						{"datacenter": "rbx", "availability": "unavailable"},
					},
				},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/order/cart":
			_ = json.NewEncoder(w).Encode(map[string]any{"cartId": "stock-quote-1"})
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/assign"):
			_ = json.NewEncoder(w).Encode(map[string]any{})
		case r.Method == http.MethodPost && r.URL.Path == "/order/cart/stock-quote-1/eco":
			_ = json.NewEncoder(w).Encode(map[string]any{"itemId": 7})
		case r.Method == http.MethodPost && r.URL.Path == "/order/cart/stock-quote-1/item/7/configuration":
			_ = json.NewEncoder(w).Encode(map[string]any{})
		case r.Method == http.MethodGet && r.URL.Path == "/order/cart/stock-quote-1/eco/options":
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{"planCode": "ram-32g-noecc-2133-24ska01", "duration": "P1M", "pricingMode": "default"},
				{"planCode": "softraid-2x480ssd-24ska01", "duration": "P1M", "pricingMode": "default"},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/order/cart/stock-quote-1/eco/options":
			_ = json.NewEncoder(w).Encode(map[string]any{})
		case r.Method == http.MethodGet && r.URL.Path == "/order/cart/stock-quote-1/summary":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"prices": map[string]any{
					"withTax":      map[string]any{"value": 15.99, "currencyCode": "EUR"},
					"withoutTax":   map[string]any{"value": 13.33, "currencyCode": "EUR"},
					"tax":          map[string]any{"value": 2.66, "currencyCode": "EUR"},
					"currencyCode": "EUR",
				},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/order/cart/stock-quote-1":
			_ = json.NewEncoder(w).Encode(map[string]any{"items": []any{}})
		case r.Method == http.MethodDelete && r.URL.Path == "/order/cart/stock-quote-1":
			_ = json.NewEncoder(w).Encode(map[string]any{})
		default:
			http.NotFound(w, r)
		}
	}))
	defer mockOVH.Close()

	env := newMonitorTestEnv(t, mockOVH)

	// 1. Adding subscription with autoOrder=true and no explicit config must return 400 CONFIG_REQUIRED_FOR_AUTO_ORDER.
	badReq := httptest.NewRequest(http.MethodPost, "/api/monitor/subscriptions", strings.NewReader(`{
		"planCode": "24ska01",
		"datacenters": ["gra"],
		"notifyAvailable": true,
		"autoOrder": true
	}`))
	badRec := httptest.NewRecorder()
	env.router.ServeHTTP(badRec, badReq)
	if badRec.Code != http.StatusBadRequest || !strings.Contains(badRec.Body.String(), "CONFIG_REQUIRED_FOR_AUTO_ORDER") {
		t.Fatalf("expected 400 CONFIG_REQUIRED_FOR_AUTO_ORDER when autoOrder=true without targets, got %d %s", badRec.Code, badRec.Body.String())
	}

	// Add subscription with explicit target config (auto-starts stock monitor).
	req := httptest.NewRequest(http.MethodPost, "/api/monitor/subscriptions", strings.NewReader(`{
		"planCode": "24ska01",
		"datacenters": ["gra"],
		"targets": [
			{
				"id": "tgt-1",
				"configKey": "ram-32g-noecc-2133+softraid-2x480ssd",
				"options": ["ram-32g-noecc-2133-24ska01", "softraid-2x480ssd-24ska01"],
				"label": "32 GB DDR4 + 2x480 GB SSD",
				"datacenters": ["gra"],
				"notifyAvailable": true,
				"notifyUnavailable": true,
				"autoOrder": true,
				"quantity": 1,
				"maxMonthlyPrice": 20.00
			}
		]
	}`))
	rec := httptest.NewRecorder()
	env.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /api/monitor/subscriptions failed: %d %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/monitor/status", nil)
	rec = httptest.NewRecorder()
	env.router.ServeHTTP(rec, req)
	var stBody map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &stBody)
	if stBody["running"] != true {
		t.Fatalf("expected monitor to auto-start on subscription add, got %+v", stBody)
	}

	// 2. Baseline check (unavailable -> silent baseline, 0 alerts).
	req = httptest.NewRequest(http.MethodPost, "/api/monitor/check-now", nil)
	rec = httptest.NewRecorder()
	env.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /api/monitor/check-now failed: %d", rec.Code)
	}
	if env.tg.count() != 0 {
		t.Fatalf("expected 0 alerts on initial baseline, got %d", env.tg.count())
	}

	// 3. Flip GRA to 1H-high -> triggers stock.available + unpaid auto-order queue task.
	graStatus.Store("1H-high")
	req = httptest.NewRequest(http.MethodPost, "/api/monitor/check-now", nil)
	rec = httptest.NewRecorder()
	env.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("check-now failed: %d", rec.Code)
	}
	if env.tg.count() != 1 {
		t.Fatalf("expected 1 stock.available alert, got %d", env.tg.count())
	}
	qItems, _ := env.st.ListQueue(context.Background(), "")
	if len(qItems) != 1 || qItems[0].PlanCode != "24ska01" || qItems[0].AutoPay || qItems[0].WaiveRetractationPeriod || qItems[0].Source != "stock-monitor" || qItems[0].SourceRef != "24ska01" {
		t.Fatalf("expected 1 safe unpaid auto-order queue task with source=stock-monitor, got %+v", qItems)
	}

	// Repeated check while still available must not duplicate alert.
	_, _ = env.svc.CheckStockNow(context.Background())
	if env.tg.count() != 1 {
		t.Fatalf("expected no duplicate alert when stock remains available, got %d", env.tg.count())
	}

	// 4. Flip GRA back to unavailable -> triggers stock.unavailable with duration & updates newest-first history.
	graStatus.Store("unavailable")
	_, _ = env.svc.CheckStockNow(context.Background())
	if env.tg.count() != 2 {
		t.Fatalf("expected 2 alerts total (available + unavailable), got %d", env.tg.count())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/monitor/subscriptions/24ska01/history", nil)
	rec = httptest.NewRecorder()
	env.router.ServeHTTP(rec, req)
	var histResp struct {
		PlanCode string                    `json:"planCode"`
		History  []store.StockHistoryEntry `json:"history"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &histResp)
	if len(histResp.History) != 2 || histResp.History[0].ChangeType != "unavailable" || histResp.History[1].ChangeType != "available" {
		t.Fatalf("expected newest-first stock history [unavailable, available], got %+v", histResp.History)
	}

	// 5. batch-add-all, delete, clear, stop.
	req = httptest.NewRequest(http.MethodPost, "/api/monitor/subscriptions/batch-add-all", strings.NewReader(`{}`))
	rec = httptest.NewRecorder()
	env.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("batch-add-all failed: %d %s", rec.Code, rec.Body.String())
	}
	var batchResp map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &batchResp)
	if _, ok := batchResp["skipped"]; !ok {
		t.Fatalf("expected skipped count in batch-add-all response, got %+v", batchResp)
	}
	req = httptest.NewRequest(http.MethodDelete, "/api/monitor/subscriptions/clear", nil)
	rec = httptest.NewRecorder()
	env.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("clear failed: %d", rec.Code)
	}
	var clearResp map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &clearResp)
	if clearResp["count"] != float64(1) {
		t.Fatalf("expected count=1 in stock clear response, got %+v", clearResp)
	}
	req = httptest.NewRequest(http.MethodPost, "/api/monitor/stop", nil)
	rec = httptest.NewRecorder()
	env.router.ServeHTTP(rec, req)
	if env.svc.IsStockRunning() {
		t.Fatalf("expected stock monitor to stop")
	}
}

func TestVPSMonitorTransitionsAndLifecycle(t *testing.T) {
	var vpsStatus atomic.Value
	vpsStatus.Store("outOfStock")

	mockOVH := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/vps/order/rule/datacenter" {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"datacenters": []map[string]any{
					{"datacenter": "Gravelines", "code": "GRA", "status": vpsStatus.Load().(string), "daysBeforeDelivery": 0},
					{"datacenter": "Beauharnois", "code": "BHS", "status": "outOfStock", "daysBeforeDelivery": 0},
				},
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer mockOVH.Close()

	env := newMonitorTestEnv(t, mockOVH)

	// Interval validation (< 60 -> 400).
	req := httptest.NewRequest(http.MethodPut, "/api/vps-monitor/interval", strings.NewReader(`{"interval":30}`))
	rec := httptest.NewRecorder()
	env.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for interval < 60, got %d", rec.Code)
	}
	req = httptest.NewRequest(http.MethodPut, "/api/vps-monitor/interval", strings.NewReader(`{"interval":120}`))
	rec = httptest.NewRecorder()
	env.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for interval=120, got %d", rec.Code)
	}

	// Add VPS sub -> auto-starts VPS monitor.
	req = httptest.NewRequest(http.MethodPost, "/api/vps-monitor/subscriptions", strings.NewReader(`{
		"planCode": "vps-2025-model1",
		"ovhSubsidiary": "IE",
		"datacenters": ["GRA"],
		"notifyAvailable": true,
		"notifyUnavailable": true
	}`))
	rec = httptest.NewRecorder()
	env.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /api/vps-monitor/subscriptions failed: %d %s", rec.Code, rec.Body.String())
	}
	var addResp struct {
		Status       string                `json:"status"`
		Subscription store.VPSSubscription `json:"subscription"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &addResp)
	if !env.svc.IsVPSRunning() || addResp.Subscription.ID == "" {
		t.Fatalf("expected VPS monitor running with created sub, running=%v sub=%+v", env.svc.IsVPSRunning(), addResp.Subscription)
	}

	// Baseline -> available -> outOfStock transitions.
	_, _ = env.svc.CheckVPSOnce(context.Background(), "")
	if env.tg.count() != 0 {
		t.Fatalf("expected silent baseline, got %d alerts", env.tg.count())
	}

	vpsStatus.Store("available")
	req = httptest.NewRequest(http.MethodPost, "/api/vps-monitor/check/vps-2025-model1", nil)
	rec = httptest.NewRecorder()
	env.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || env.tg.count() != 1 {
		t.Fatalf("expected available transition + 1 alert, code=%d tg=%d", rec.Code, env.tg.count())
	}

	vpsStatus.Store("outOfStock")
	req = httptest.NewRequest(http.MethodPost, "/api/vps-monitor/check-now", nil)
	rec = httptest.NewRecorder()
	env.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || env.tg.count() != 2 {
		t.Fatalf("expected unavailable transition + 2 alerts total, code=%d tg=%d", rec.Code, env.tg.count())
	}

	// Verify history newest-first.
	req = httptest.NewRequest(http.MethodGet, "/api/vps-monitor/subscriptions/"+addResp.Subscription.ID+"/history", nil)
	rec = httptest.NewRecorder()
	env.router.ServeHTTP(rec, req)
	var vpsHistResp struct {
		ID      string                  `json:"id"`
		History []store.VPSHistoryEntry `json:"history"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &vpsHistResp)
	if len(vpsHistResp.History) != 2 || vpsHistResp.History[0].ChangeType != "unavailable" || vpsHistResp.History[1].ChangeType != "available" {
		t.Fatalf("unexpected VPS history order/content: %+v", vpsHistResp)
	}

	// Deleting last sub auto-stops VPS monitor.
	req = httptest.NewRequest(http.MethodDelete, "/api/vps-monitor/subscriptions/"+addResp.Subscription.ID, nil)
	rec = httptest.NewRecorder()
	env.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || env.svc.IsVPSRunning() {
		t.Fatalf("expected VPS monitor auto-stop when 0 subs remain, code=%d running=%v", rec.Code, env.svc.IsVPSRunning())
	}

	// Clear endpoint returns count.
	req = httptest.NewRequest(http.MethodDelete, "/api/vps-monitor/subscriptions/clear", nil)
	rec = httptest.NewRecorder()
	env.router.ServeHTTP(rec, req)
	var vpsClear map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &vpsClear)
	if rec.Code != http.StatusOK || vpsClear["count"] != float64(0) {
		t.Fatalf("expected VPS clear count=0, got %d %+v", rec.Code, vpsClear)
	}
}

func TestSKUWatcherAntiFalsePositiveAndConfigDiff(t *testing.T) {
	var plansMu sync.Mutex
	buildPlans := func(codes []string) []map[string]any {
		out := make([]map[string]any, 0, len(codes))
		for _, c := range codes {
			out = append(out, map[string]any{
				"planCode":    c,
				"invoiceName": "KS-" + c,
				"blobs":       map[string]any{"commercial": map[string]any{"name": "KS-" + c}},
				"pricings":    []map[string]any{{"mode": "default", "capacities": []string{"renew"}, "price": 500000000}},
			})
		}
		return out
	}

	base10 := []string{
		"24sk01", "24sk01-sgp", "24sk02", "24sk03", "24sk04", "24sk05", "24sk06", "24sk07", "24sk08", "24sk09", "24sk10",
	}
	currentPlans := buildPlans(base10)
	currentAvails := []map[string]any{
		{"fqn": "24sk01.ram-32g.ssd", "planCode": "24sk01", "memory": "ram-32g-noecc-2133", "storage": "softraid-2x480ssd", "datacenters": []map[string]any{{"datacenter": "gra", "availability": "1H"}}},
	}

	mockOVH := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		plansMu.Lock()
		defer plansMu.Unlock()
		switch r.URL.Path {
		case "/order/catalog/public/eco":
			_ = json.NewEncoder(w).Encode(map[string]any{"locale": map[string]any{"currencyCode": "EUR"}, "plans": currentPlans})
		case "/order/catalog/public/baremetalServers", "/order/catalog/public/vps":
			_ = json.NewEncoder(w).Encode(map[string]any{"locale": map[string]any{"currencyCode": "EUR"}, "plans": []any{}})
		case "/dedicated/server/datacenter/availabilities":
			_ = json.NewEncoder(w).Encode(currentAvails)
		default:
			http.NotFound(w, r)
		}
	}))
	defer mockOVH.Close()

	env := newMonitorTestEnv(t, mockOVH)
	if _, err := env.st.SaveNotificationSettings(context.Background(), store.NotificationSettings{
		Locale: "en",
		Rules: store.NotificationRules{
			SKUEnabled:       true,
			SKUInterval:      600,
			SKUTrackConfigs:  true,
			SKURanges:        []string{"KS"},
			SKUMergeRegional: true,
			ExpiryDays:       7,
		},
	}); err != nil {
		t.Fatalf("SaveNotificationSettings failed: %v", err)
	}

	// Step 1: Initial baseline (merges 24sk01 + 24sk01-sgp => 10 logical SKUs), 0 alerts, and recentFirstSeen MUST be empty!
	res1, err := env.svc.CheckSKUOnce(context.Background(), true)
	if err != nil {
		t.Fatalf("CheckSKUOnce #1 failed: %v", err)
	}
	if res1.Status != "baseline_initialized" || res1.TotalSKUs != 10 || res1.TrackedCount != 10 || env.tg.count() != 0 {
		t.Fatalf("unexpected step 1 baseline result: %+v tg=%d", res1, env.tg.count())
	}
	initStatus, _, err := env.svc.GetSKUStatusAndChanges(context.Background())
	if err != nil {
		t.Fatalf("GetSKUStatusAndChanges after baseline failed: %v", err)
	}
	if len(initStatus.RecentFirstSeen) != 0 {
		t.Fatalf("expected RecentFirstSeen to be empty on initial silent baseline, got %+v", initStatus.RecentFirstSeen)
	}

	// Step 2: Sudden >30% drop (10 -> 4) MUST trigger glitch protection!
	plansMu.Lock()
	currentPlans = buildPlans([]string{"24sk01", "24sk02", "24sk03", "24sk04"})
	plansMu.Unlock()
	res2, err := env.svc.CheckSKUOnce(context.Background(), true)
	if err != nil {
		t.Fatalf("CheckSKUOnce #2 failed: %v", err)
	}
	if res2.Status != "skipped_glitch" || env.tg.count() != 0 {
		t.Fatalf("expected skipped_glitch without alert, got %+v tg=%d", res2, env.tg.count())
	}

	// Step 3: 1 SKU missing for the first time (24sk10 absent) -> NOT yet confirmed removed!
	plansMu.Lock()
	currentPlans = buildPlans([]string{"24sk01", "24sk01-sgp", "24sk02", "24sk03", "24sk04", "24sk05", "24sk06", "24sk07", "24sk08", "24sk09"})
	plansMu.Unlock()
	res3, err := env.svc.CheckSKUOnce(context.Background(), true)
	if err != nil {
		t.Fatalf("CheckSKUOnce #3 failed: %v", err)
	}
	if res3.RemovedCount != 0 || res3.TrackedCount != 10 || env.tg.count() != 0 {
		t.Fatalf("expected 1st-miss grace retention (no removal alert yet), got %+v tg=%d", res3, env.tg.count())
	}

	// Step 4: 24sk10 missing a 2nd consecutive check + 24sk11 added + 24sk01 gains 64G config -> emits sku.changed!
	plansMu.Lock()
	currentPlans = buildPlans([]string{"24sk01", "24sk01-sgp", "24sk02", "24sk03", "24sk04", "24sk05", "24sk06", "24sk07", "24sk08", "24sk09", "24sk11"})
	currentAvails = []map[string]any{
		{"fqn": "24sk01.ram-32g.ssd", "planCode": "24sk01", "memory": "ram-32g-noecc-2133", "storage": "softraid-2x480ssd", "datacenters": []map[string]any{{"datacenter": "gra", "availability": "1H"}}},
		{"fqn": "24sk01.ram-64g.nvme", "planCode": "24sk01", "memory": "ram-64g-noecc-2133", "storage": "softraid-2x960nvme", "datacenters": []map[string]any{{"datacenter": "gra", "availability": "1H"}}},
	}
	plansMu.Unlock()

	req := httptest.NewRequest(http.MethodPost, "/api/sku/check", nil)
	rec := httptest.NewRecorder()
	env.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /api/sku/check failed: %d %s", rec.Code, rec.Body.String())
	}
	var checkHTTPResp struct {
		OK      bool              `json:"ok"`
		Status  SKUWatcherStatus  `json:"status"`
		Result  SKUCheckResult    `json:"result"`
		Changes []SKUChangeRecord `json:"changes"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &checkHTTPResp); err != nil {
		t.Fatalf("decode POST /api/sku/check failed: %v", err)
	}
	if !checkHTTPResp.OK || checkHTTPResp.Status.Endpoint != "ovh-eu" || checkHTTPResp.Status.TotalSKUs != 10 {
		t.Fatalf("unexpected POST /api/sku/check status object: %+v", checkHTTPResp.Status)
	}
	if checkHTTPResp.Result.Status != "ok" || checkHTTPResp.Result.TotalSKUs != 10 || len(checkHTTPResp.Result.Added) != 1 || len(checkHTTPResp.Result.Removed) != 1 || checkHTTPResp.Result.ConfigChanges != 1 || env.tg.count() != 1 {
		t.Fatalf("expected 1 added, 1 confirmed removed, 1 config change + 1 TG alert, got %+v tg=%d", checkHTTPResp.Result, env.tg.count())
	}
	if len(checkHTTPResp.Changes) != 3 {
		t.Fatalf("expected 3 top-level changes in /sku/check response, got %d", len(checkHTTPResp.Changes))
	}

	// Verify /api/sku/status and /api/sku/changes: only post-baseline 24sk11 is in RecentFirstSeen, NOT baseline 24sk01-sgp!
	req = httptest.NewRequest(http.MethodGet, "/api/sku/status", nil)
	rec = httptest.NewRecorder()
	env.router.ServeHTTP(rec, req)
	var skuStatus SKUWatcherStatus
	if err := json.Unmarshal(rec.Body.Bytes(), &skuStatus); err != nil {
		t.Fatalf("decode /api/sku/status failed: %v", err)
	}
	if !skuStatus.Initialized || skuStatus.TotalSKUs != 10 || skuStatus.RecentFirstSeen["24sk11"] == "" || skuStatus.RecentFirstSeen["24sk01-sgp"] != "" {
		t.Fatalf("unexpected /api/sku/status: %+v", skuStatus)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/sku/changes?limit=200", nil)
	rec = httptest.NewRecorder()
	env.router.ServeHTTP(rec, req)
	var changesResp struct {
		Changes []SKUChangeRecord `json:"changes"`
		Total   int               `json:"total"`
		Status  SKUWatcherStatus  `json:"status"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &changesResp); err != nil {
		t.Fatalf("decode /api/sku/changes failed: %v", err)
	}
	if changesResp.Total != 3 || len(changesResp.Changes) != 3 || changesResp.Status.TotalSKUs != 10 || changesResp.Status.RecentFirstSeen["24sk11"] == "" {
		t.Fatalf("expected 3 SKU changes + status map, got %+v", changesResp)
	}
	var sawCfgChanged bool
	for _, ch := range changesResp.Changes {
		if ch.SKUKey == "" {
			t.Fatalf("expected non-empty skuKey on SKUChangeRecord: %+v", ch)
		}
		if ch.Type == "config_changed" && len(ch.AddedConfigs) > 0 && ch.StockState == "in_stock" {
			sawCfgChanged = true
		}
	}
	if !sawCfgChanged {
		t.Fatalf("expected config_changed record with AddedConfigs and StockState, got %+v", changesResp.Changes)
	}

	// Step 5: Presale SKU appears in availabilities only (27skle01) -> tracked in PresaleSKUs, NOT in RecentFirstSeen.
	plansMu.Lock()
	currentAvails = append(currentAvails, map[string]any{
		"fqn":         "27skle01.ram-64g.nvme",
		"planCode":    "27skle01",
		"memory":      "ram-64g-ecc-3200",
		"storage":     "softraid-2x960nvme",
		"datacenters": []map[string]any{{"datacenter": "ca-east-tor-a", "availability": "72H"}},
	})
	plansMu.Unlock()

	res5, err := env.svc.CheckSKUOnce(context.Background(), true)
	if err != nil {
		t.Fatalf("CheckSKUOnce #5 (presale added) failed: %v", err)
	}
	if res5.AddedCount != 1 || res5.Added[0].ListingState != "presale" || res5.Added[0].Generation != 2027 {
		t.Fatalf("expected presale added record with generation 2027, got %+v", res5)
	}
	st5, _, _ := env.svc.GetSKUStatusAndChanges(context.Background())
	if st5.PresaleSKUs["27skle01"] == "" || st5.RecentFirstSeen["27skle01"] != "" {
		t.Fatalf("expected 27skle01 in PresaleSKUs and NOT in RecentFirstSeen, got presale=%+v recent=%+v", st5.PresaleSKUs, st5.RecentFirstSeen)
	}

	// Step 6: 27skle01 transitions from presale -> listed in eco catalog -> enters RecentFirstSeen and emits listed change!
	plansMu.Lock()
	currentPlans = buildPlans([]string{"24sk01", "24sk01-sgp", "24sk02", "24sk03", "24sk04", "24sk05", "24sk06", "24sk07", "24sk08", "24sk09", "24sk11", "27skle01"})
	plansMu.Unlock()

	res6, err := env.svc.CheckSKUOnce(context.Background(), true)
	if err != nil {
		t.Fatalf("CheckSKUOnce #6 (presale -> listed transition) failed: %v", err)
	}
	if res6.AddedCount != 1 || res6.Added[0].ListingState != "listed" || res6.Added[0].PlanCode != "27skle01" {
		t.Fatalf("expected presale -> listed transition to emit added/listed record, got %+v", res6)
	}
	st6, _, _ := env.svc.GetSKUStatusAndChanges(context.Background())
	if st6.RecentFirstSeen["27skle01"] == "" || st6.PresaleSKUs["27skle01"] != "" {
		t.Fatalf("expected 27skle01 in RecentFirstSeen and removed from PresaleSKUs after listing, got presale=%+v recent=%+v", st6.PresaleSKUs, st6.RecentFirstSeen)
	}
}

func TestLifecycleWatcherDeliveredAndExpiringDeduplication(t *testing.T) {
	soonDate := time.Now().Add(3 * 24 * time.Hour).Format("2006-01-02T15:04:05Z07:00")
	laterDate := time.Now().Add(25 * 24 * time.Hour).Format("2006-01-02T15:04:05Z07:00")

	mockOVH := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/auth/time":
			_ = json.NewEncoder(w).Encode(time.Now().Unix())
		case "/me/order/order-555/status":
			_ = json.NewEncoder(w).Encode("delivered")
		case "/dedicated/server":
			_ = json.NewEncoder(w).Encode([]string{"ns123.ip-1-2-3.eu"})
		case "/dedicated/server/ns123.ip-1-2-3.eu/serviceInfos":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"expiration": soonDate,
				"status":     "ok",
				"renew":      map[string]any{"automatic": false},
			})
		case "/vps":
			_ = json.NewEncoder(w).Encode([]string{"vps-later.ovh.net"})
		case "/vps/vps-later.ovh.net/serviceInfos":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"expiration": laterDate,
				"status":     "ok",
				"renew":      map[string]any{"automatic": true},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer mockOVH.Close()

	env := newMonitorTestEnv(t, mockOVH)
	_, err := env.st.AddHistory(context.Background(), store.PurchaseHistory{
		AccountID:  "acc-1",
		PlanCode:   "24ska01",
		Datacenter: "gra",
		Status:     "success",
		OrderID:    "order-555",
	})
	if err != nil {
		t.Fatalf("seed purchase history failed: %v", err)
	}

	// First lifecycle scan: 1 delivered + 1 expiring (2 alerts; vps-later > 7 days ignored).
	if err := env.svc.CheckLifecycleOnce(context.Background()); err != nil {
		t.Fatalf("CheckLifecycleOnce #1 failed: %v", err)
	}
	if env.tg.count() != 2 {
		t.Fatalf("expected 2 lifecycle alerts (1 delivered + 1 expiring), got %d: %v", env.tg.count(), env.tg.slice())
	}

	// Verify DeliveredNotified persisted in SQLite.
	hist, _ := env.st.ListHistory(context.Background(), "acc-1")
	if len(hist) != 1 || !hist[0].DeliveredNotified {
		t.Fatalf("expected DeliveredNotified=true, got %+v", hist)
	}

	// Second lifecycle scan MUST emit 0 duplicate alerts.
	if err := env.svc.CheckLifecycleOnce(context.Background()); err != nil {
		t.Fatalf("CheckLifecycleOnce #2 failed: %v", err)
	}
	if env.tg.count() != 2 {
		t.Fatalf("expected deduplicated lifecycle scan (still 2 alerts), got %d", env.tg.count())
	}
}

func TestStockMultiTargetMigrationAndConfigSpecificAutoOrder(t *testing.T) {
	var cfg32Status, cfg64Status atomic.Value
	cfg32Status.Store("unavailable")
	cfg64Status.Store("unavailable")

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
						"planCode":    "24ska01",
						"invoiceName": "KS-A | Intel i7-6700k",
						"blobs":       map[string]any{"commercial": map[string]any{"name": "KS-A"}},
						"pricings":    []map[string]any{{"mode": "default", "capacities": []string{"renew"}, "price": 2000000000}},
						"addonFamilies": []map[string]any{
							{
								"name":    "memory",
								"default": "ram-32g-noecc-2133-24ska01",
								"addons":  []string{"ram-32g-noecc-2133-24ska01", "ram-64g-noecc-2133-24ska01"},
							},
							{
								"name":    "storage",
								"default": "softraid-2x480ssd-24ska01",
								"addons":  []string{"softraid-2x480ssd-24ska01", "softraid-2x960nvme-24ska01"},
							},
						},
					},
				},
			})
		case r.URL.Path == "/order/catalog/public/baremetalServers" || r.URL.Path == "/order/catalog/public/vps":
			_ = json.NewEncoder(w).Encode(map[string]any{"locale": map[string]any{"currencyCode": "EUR"}, "plans": []any{}})
		case r.URL.Path == "/dedicated/server/datacenter/availabilities":
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{
					"fqn":      "24ska01.ram-32g-noecc-2133.softraid-2x480ssd",
					"planCode": "24ska01",
					"memory":   "ram-32g-noecc-2133",
					"storage":  "softraid-2x480ssd",
					"datacenters": []map[string]any{
						{"datacenter": "gra", "availability": cfg32Status.Load().(string)},
					},
				},
				{
					"fqn":      "24ska01.ram-64g-noecc-2133.softraid-2x960nvme",
					"planCode": "24ska01",
					"memory":   "ram-64g-noecc-2133",
					"storage":  "softraid-2x960nvme",
					"datacenters": []map[string]any{
						{"datacenter": "gra", "availability": cfg64Status.Load().(string)},
					},
				},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/order/cart":
			_ = json.NewEncoder(w).Encode(map[string]any{"cartId": "stock-quote-mt"})
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/assign"):
			_ = json.NewEncoder(w).Encode(map[string]any{})
		case r.Method == http.MethodPost && r.URL.Path == "/order/cart/stock-quote-mt/eco":
			_ = json.NewEncoder(w).Encode(map[string]any{"itemId": 7})
		case r.Method == http.MethodPost && r.URL.Path == "/order/cart/stock-quote-mt/item/7/configuration":
			_ = json.NewEncoder(w).Encode(map[string]any{})
		case r.Method == http.MethodGet && r.URL.Path == "/order/cart/stock-quote-mt/eco/options":
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{"planCode": "ram-32g-noecc-2133-24ska01", "duration": "P1M", "pricingMode": "default"},
				{"planCode": "softraid-2x480ssd-24ska01", "duration": "P1M", "pricingMode": "default"},
				{"planCode": "ram-64g-noecc-2133-24ska01", "duration": "P1M", "pricingMode": "default"},
				{"planCode": "softraid-2x960nvme-24ska01", "duration": "P1M", "pricingMode": "default"},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/order/cart/stock-quote-mt/eco/options":
			_ = json.NewEncoder(w).Encode(map[string]any{})
		case r.Method == http.MethodGet && r.URL.Path == "/order/cart/stock-quote-mt/summary":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"prices": map[string]any{
					"withTax":      map[string]any{"value": 42.00, "currencyCode": "EUR"},
					"withoutTax":   map[string]any{"value": 35.00, "currencyCode": "EUR"},
					"tax":          map[string]any{"value": 7.00, "currencyCode": "EUR"},
					"currencyCode": "EUR",
				},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/order/cart/stock-quote-mt":
			_ = json.NewEncoder(w).Encode(map[string]any{"items": []any{}})
		case r.Method == http.MethodDelete && r.URL.Path == "/order/cart/stock-quote-mt":
			_ = json.NewEncoder(w).Encode(map[string]any{})
		default:
			http.NotFound(w, r)
		}
	}))
	defer mockOVH.Close()

	env := newMonitorTestEnv(t, mockOVH)

	// 1. Legacy subscription migration: seed a legacy subscription with empty Targets and AutoOrder=true.
	_, err := env.st.UpsertStockSub(context.Background(), store.StockSubscription{
		AccountID:         "acc-1",
		PlanCode:          "24ska01",
		ServerName:        "KS-A",
		Datacenters:       []string{"gra"},
		NotifyAvailable:   true,
		NotifyUnavailable: false,
		AutoOrder:         true,
		AutoOrderQuantity: 2,
		LastStatus:        map[string]string{"gra": "unavailable"},
	})
	if err != nil {
		t.Fatalf("seed legacy stock sub failed: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/monitor/subscriptions", nil)
	rec := httptest.NewRecorder()
	env.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/monitor/subscriptions failed: %d %s", rec.Code, rec.Body.String())
	}
	var listedSubs []store.StockSubscription
	if err := json.Unmarshal(rec.Body.Bytes(), &listedSubs); err != nil {
		t.Fatalf("decode subscriptions failed: %v", err)
	}
	if len(listedSubs) != 1 {
		t.Fatalf("expected 1 migrated subscription, got %d", len(listedSubs))
	}
	mig := listedSubs[0]
	if mig.AutoOrder || !mig.NeedsConfigSelection || len(mig.Targets) != 1 {
		t.Fatalf("expected legacy auto-order paused with needsConfigSelection=true and 1 default target, got %+v", mig)
	}
	if mig.Targets[0].ID != "default" || mig.Targets[0].ConfigKey != "" || mig.Targets[0].AutoOrder || mig.Targets[0].Quantity != 2 {
		t.Fatalf("unexpected migrated target: %+v", mig.Targets[0])
	}

	// Verify migration INFO log was persisted.
	logs, _ := env.st.QueryLogs(context.Background(), "INFO", "stock-monitor", "migrated legacy stock subscription", 10)
	if len(logs) == 0 {
		t.Fatalf("expected INFO log entry for legacy subscription migration")
	}

	// 2. Validation: target with autoOrder=true but missing options or configKey -> 400 CONFIG_REQUIRED_FOR_AUTO_ORDER.
	for _, badPayload := range []string{
		`{"planCode":"24ska01","targets":[{"configKey":"","options":["ram-32g"],"autoOrder":true}]}`,
		`{"planCode":"24ska01","targets":[{"configKey":"ram-32g+softraid-2x480ssd","options":[],"autoOrder":true}]}`,
	} {
		req = httptest.NewRequest(http.MethodPost, "/api/monitor/subscriptions", strings.NewReader(badPayload))
		rec = httptest.NewRecorder()
		env.router.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 for payload %s, got %d %s", badPayload, rec.Code, rec.Body.String())
		}
		var errBody map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &errBody)
		if errBody["code"] != "CONFIG_REQUIRED_FOR_AUTO_ORDER" {
			t.Fatalf("expected code=CONFIG_REQUIRED_FOR_AUTO_ORDER, got %+v", errBody)
		}
	}

	// 3. Save multi-target subscription:
	//    - Target 1: 32G config (notify-only, autoOrder=false)
	//    - Target 2: 64G config (notify + autoOrder=true, quantity=3, maxMonthlyPrice=45.00)
	req = httptest.NewRequest(http.MethodPost, "/api/monitor/subscriptions", strings.NewReader(`{
		"planCode": "24ska01",
		"targets": [
			{
				"id": "tgt-32g",
				"configKey": "ram-32g-noecc-2133+softraid-2x480ssd",
				"options": ["ram-32g-noecc-2133-24ska01", "softraid-2x480ssd-24ska01"],
				"label": "32GB + 2x480GB SSD",
				"datacenters": ["gra"],
				"notifyAvailable": true,
				"autoOrder": false,
				"quantity": 1
			},
			{
				"id": "tgt-64g",
				"configKey": "ram-64g-noecc-2133+softraid-2x960nvme",
				"options": ["ram-64g-noecc-2133-24ska01", "softraid-2x960nvme-24ska01"],
				"label": "64GB + 2x960GB NVMe",
				"datacenters": ["gra"],
				"notifyAvailable": true,
				"autoOrder": true,
				"quantity": 3,
				"maxMonthlyPrice": 45.00,
				"currency": "EUR"
			}
		]
	}`))
	rec = httptest.NewRecorder()
	env.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST multi-target subscription failed: %d %s", rec.Code, rec.Body.String())
	}
	var saveResp struct {
		Subscription store.StockSubscription `json:"subscription"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &saveResp)
	if saveResp.Subscription.NeedsConfigSelection || !saveResp.Subscription.AutoOrder || len(saveResp.Subscription.Targets) != 2 {
		t.Fatalf("expected NeedsConfigSelection=false, AutoOrder=true, 2 targets, got %+v", saveResp.Subscription)
	}

	// Baseline tick: both configs unavailable.
	if _, err := env.svc.CheckStockNow(context.Background()); err != nil {
		t.Fatalf("baseline CheckStockNow failed: %v", err)
	}

	// 4. Only 32G becomes available -> Target 1 notifies, Target 2 (64G) does NOT auto-order!
	cfg32Status.Store("1H-high")
	if _, err := env.svc.CheckStockNow(context.Background()); err != nil {
		t.Fatalf("32G transition CheckStockNow failed: %v", err)
	}
	if env.tg.count() != 1 {
		t.Fatalf("expected 1 notification for 32G target, got %d", env.tg.count())
	}
	if !strings.Contains(env.tg.slice()[0], "32GB + 2x480GB SSD") {
		t.Fatalf("expected notification to mention target label '32GB + 2x480GB SSD', got %s", env.tg.slice()[0])
	}
	qItems, _ := env.st.ListQueue(context.Background(), "")
	if len(qItems) != 0 {
		t.Fatalf("expected 0 auto-order queue tasks when only notify-only 32G target is in stock, got %+v", qItems)
	}

	// 5. Now 64G becomes available -> Target 2 notifies AND triggers auto-order with options, quantity=3, maxMonthlyPrice=45.00!
	cfg64Status.Store("1H-high")
	if _, err := env.svc.CheckStockNow(context.Background()); err != nil {
		t.Fatalf("64G transition CheckStockNow failed: %v", err)
	}
	if env.tg.count() != 2 {
		t.Fatalf("expected 2 notifications total after 64G transition, got %d", env.tg.count())
	}
	qItems, _ = env.st.ListQueue(context.Background(), "")
	if len(qItems) != 1 {
		t.Fatalf("expected 1 auto-order queue task for 64G target, got %+v", qItems)
	}
	gotQ := qItems[0]
	if gotQ.Quantity != 3 || gotQ.MaxMonthlyPrice != 45.00 || gotQ.ConfigKey != "ram-64g-noecc-2133+softraid-2x960nvme" {
		t.Fatalf("expected queue task with Quantity=3, MaxMonthlyPrice=45.00, ConfigKey=ram-64g-noecc-2133+softraid-2x960nvme, got %+v", gotQ)
	}
	if len(gotQ.Options) != 2 || gotQ.Options[0] != "ram-64g-noecc-2133-24ska01" || gotQ.Options[1] != "softraid-2x960nvme-24ska01" {
		t.Fatalf("unexpected auto-order options: %+v", gotQ.Options)
	}
}

func TestStockTargetPassesPricingModeDurationAndCouponToAutoOrder(t *testing.T) {
	var dcStatus atomic.Value
	dcStatus.Store("unavailable")

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
						"invoiceName": "SYS-2",
						"pricings": []map[string]any{
							{"mode": "default", "capacities": []string{"renew"}, "interval": 1, "price": 3799000000},
							{"mode": "upfront12", "capacities": []string{"renew"}, "interval": 12, "commitment": 12, "price": 34647000000},
						},
					},
				},
			})
		case r.URL.Path == "/order/catalog/public/baremetalServers" || r.URL.Path == "/order/catalog/public/vps":
			_ = json.NewEncoder(w).Encode(map[string]any{"locale": map[string]any{"currencyCode": "EUR"}, "plans": []any{}})
		case r.URL.Path == "/dedicated/server/datacenter/availabilities":
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{
					"fqn":      "24sys02-v3.ram-32g-ecc-2133.softraid-2x450nvme",
					"planCode": "24sys02-v3",
					"memory":   "ram-32g-ecc-2133",
					"storage":  "softraid-2x450nvme",
					"datacenters": []map[string]any{
						{"datacenter": "gra", "availability": dcStatus.Load().(string)},
					},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer mockOVH.Close()

	env := newMonitorTestEnv(t, mockOVH)

	var capturedMu sync.Mutex
	var capturedReqs []AutoOrderRequest
	env.svc.SetAutoOrderFunc(func(_ context.Context, req AutoOrderRequest) (*procurement.QuickOrderResult, int, error) {
		capturedMu.Lock()
		capturedReqs = append(capturedReqs, req)
		capturedMu.Unlock()
		return &procurement.QuickOrderResult{
			Success:      true,
			ID:           "mock-queue-1",
			PricingMode:  req.PricingMode,
			Duration:     req.Duration,
			Coupon:       req.Coupon,
			MonthlyPrice: 28.87,
			PeriodPrice:  346.47,
		}, http.StatusOK, nil
	})

	req := httptest.NewRequest(http.MethodPost, "/api/monitor/subscriptions", strings.NewReader(`{
		"planCode": "24sys02-v3",
		"targets": [
			{
				"id": "tgt-upfront12",
				"configKey": "ram-32g-ecc-2133+softraid-2x450nvme",
				"options": ["ram-32g-ecc-2133-24sys02-v3", "softraid-2x450nvme-24sys02-v3"],
				"label": "32GB ECC + 2x450GB NVMe",
				"datacenters": ["gra"],
				"notifyAvailable": false,
				"autoOrder": true,
				"quantity": 1,
				"maxMonthlyPrice": 30.00,
				"pricingMode": "upfront12",
				"coupon": "BF26"
			}
		]
	}`))
	rec := httptest.NewRecorder()
	env.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /api/monitor/subscriptions failed: %d %s", rec.Code, rec.Body.String())
	}

	// Baseline tick (unavailable).
	if _, err := env.svc.CheckStockNow(context.Background()); err != nil {
		t.Fatalf("baseline CheckStockNow failed: %v", err)
	}

	// Transition unavailable -> 1H-high -> must invoke autoOrderFn with PricingMode="upfront12", Duration="P12M", Coupon="BF26".
	dcStatus.Store("1H-high")
	if _, err := env.svc.CheckStockNow(context.Background()); err != nil {
		t.Fatalf("transition CheckStockNow failed: %v", err)
	}

	capturedMu.Lock()
	defer capturedMu.Unlock()
	if len(capturedReqs) != 1 {
		t.Fatalf("expected 1 autoOrderFn call, got %d", len(capturedReqs))
	}
	got := capturedReqs[0]
	if got.PricingMode != "upfront12" || got.Duration != "P12M" || got.Coupon != "BF26" || got.MaxMonthlyPrice != 30.00 {
		t.Fatalf("expected autoOrderFn to receive PricingMode=upfront12, Duration=P12M, Coupon=BF26, MaxMonthlyPrice=30.00; got %+v", got)
	}
}
