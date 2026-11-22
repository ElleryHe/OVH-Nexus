package procurement

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	"ovh-nexus/server/internal/store"
	"ovh-nexus/server/internal/vault"
)

type testEnv struct {
	svc     *Service
	cat     *catalog.Service
	notify  *notify.Dispatcher
	st      *store.Store
	router  chi.Router
	tgPosts *atomic.Int32
	lastTG  *string
	tgMu    *sync.Mutex
}

func newTestProcurementEnv(t *testing.T, mockOVH *httptest.Server) *testEnv {
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

	var tgPosts atomic.Int32
	var tgMu sync.Mutex
	var lastTG string
	tgServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		tgMu.Lock()
		lastTG, _ = body["text"].(string)
		tgMu.Unlock()
		tgPosts.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": map[string]any{"message_id": 1}})
	}))
	t.Cleanup(tgServer.Close)

	disp := notify.NewDispatcher(st, v, hub, pool)
	disp.SetTelegramBaseURL(tgServer.URL)
	if _, err := st.UpsertChannel(context.Background(), store.NotificationChannel{
		ID:      "tg-test",
		Type:    "telegram",
		Name:    "Test TG",
		Enabled: true,
		Events:  []string{notify.EventOrderSuccess, notify.EventOrderFailed},
		Config: map[string]any{
			"token":  "123:test-token",
			"chatId": "-100999",
		},
	}); err != nil {
		t.Fatalf("seed notification channel: %v", err)
	}

	cat := catalog.NewService(st, hub, pool, dataDir)
	svc := NewService(st, hub, pool, cat, disp)

	if mockOVH != nil {
		cat.SetPublicAPIBaseURL(mockOVH.URL)
		cat.SetAccountClientFunc(func(_ context.Context, _ string) (*ovh.Client, *store.OVHAccount, error) {
			c, err := ovh.NewClient(mockOVH.URL, "ak-test", "as-test", "ck-test")
			if err != nil {
				return nil, nil, err
			}
			return c, &store.OVHAccount{ID: "acc-1", Endpoint: "ovh-eu", Zone: "IE"}, nil
		})
		svc.SetAccountClientFunc(func(_ context.Context, _ string) (*ovh.Client, *store.OVHAccount, error) {
			c, err := ovh.NewClient(mockOVH.URL, "ak-test", "as-test", "ck-test")
			if err != nil {
				return nil, nil, err
			}
			return c, &store.OVHAccount{ID: "acc-1", Endpoint: "ovh-eu", Zone: "IE"}, nil
		})
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

	r := chi.NewRouter()
	r.Route("/api", func(api chi.Router) {
		svc.MountRoutes(api)
	})

	return &testEnv{
		svc:     svc,
		cat:     cat,
		notify:  disp,
		st:      st,
		router:  r,
		tgPosts: &tgPosts,
		lastTG:  &lastTG,
		tgMu:    &tgMu,
	}
}

func TestQueueCRUDAndSecurityDefaults(t *testing.T) {
	env := newTestProcurementEnv(t, nil)

	// 1. POST /api/queue omitting auto_pay and waive_retractation_period MUST default both to false.
	req := httptest.NewRequest(http.MethodPost, "/api/queue", strings.NewReader(`{
		"planCode":"24ska01",
		"datacenters":["gra","rbx"],
		"options":["ram-32g-noecc-2133-24ska01"],
		"quantity":2,
		"retryInterval":15
	}`))
	rec := httptest.NewRecorder()
	env.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /api/queue failed: %d %s", rec.Code, rec.Body.String())
	}
	var createResp struct {
		Status string          `json:"status"`
		ID     string          `json:"id"`
		Item   store.QueueItem `json:"item"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &createResp); err != nil {
		t.Fatalf("decode create resp: %v", err)
	}
	if createResp.Item.AutoPay || createResp.Item.WaiveRetractationPeriod {
		t.Fatalf("expected auto_pay=false and waive_retractation_period=false by default, got %+v", createResp.Item)
	}
	if createResp.Item.Quantity != 2 || createResp.Item.RetryInterval != 15 || createResp.Item.Status != "running" {
		t.Fatalf("unexpected created queue item: %+v", createResp.Item)
	}

	// 2. PUT /api/queue/{id} partial update omitting flags keeps them false.
	req = httptest.NewRequest(http.MethodPut, "/api/queue/"+createResp.ID, strings.NewReader(`{
		"retryInterval": 25
	}`))
	rec = httptest.NewRecorder()
	env.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT /api/queue/{id} failed: %d %s", rec.Code, rec.Body.String())
	}
	var updateResp struct {
		Status string          `json:"status"`
		Item   store.QueueItem `json:"item"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &updateResp)
	if updateResp.Item.AutoPay || updateResp.Item.WaiveRetractationPeriod || updateResp.Item.RetryInterval != 25 {
		t.Fatalf("unexpected partial update result: %+v", updateResp.Item)
	}

	// 3. Explicit true on both POST and PUT is honored when caller asks for it.
	req = httptest.NewRequest(http.MethodPost, "/api/queue", strings.NewReader(`{
		"planCode":"25adv01",
		"datacenters":["rbx"],
		"auto_pay":true,
		"waive_retractation_period":true
	}`))
	rec = httptest.NewRecorder()
	env.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /api/queue explicit flags failed: %d %s", rec.Code, rec.Body.String())
	}
	var explicitResp struct {
		Item store.QueueItem `json:"item"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &explicitResp)
	if !explicitResp.Item.AutoPay || !explicitResp.Item.WaiveRetractationPeriod {
		t.Fatalf("expected explicit true flags to be preserved, got %+v", explicitResp.Item)
	}

	// Toggle status via PUT /api/queue/{id}/status and verify GET /api/queue and GET /api/queue/all.
	req = httptest.NewRequest(http.MethodPut, "/api/queue/"+createResp.ID+"/status", strings.NewReader(`{"status":"paused"}`))
	rec = httptest.NewRecorder()
	env.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT /api/queue/{id}/status failed: %d %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/queue/all", nil)
	rec = httptest.NewRecorder()
	env.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/queue/all failed: %d", rec.Code)
	}
	var items []store.QueueItem
	_ = json.Unmarshal(rec.Body.Bytes(), &items)
	if len(items) != 2 {
		t.Fatalf("expected 2 queue items, got %d", len(items))
	}

	// Delete one & clear remainder.
	req = httptest.NewRequest(http.MethodDelete, "/api/queue/"+createResp.ID, nil)
	rec = httptest.NewRecorder()
	env.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("DELETE /api/queue/{id} failed: %d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/queue/clear", nil)
	rec = httptest.NewRecorder()
	env.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /api/queue/clear failed: %d", rec.Code)
	}
}

func TestQuickOrderNoPricedConfigAndCooldown(t *testing.T) {
	var availItems []map[string]any
	availItems = []map[string]any{
		{
			"fqn":      "24ska01.ram-32g-noecc-2133.softraid-2x480ssd",
			"planCode": "24ska01",
			"memory":   "ram-32g-noecc-2133",
			"storage":  "softraid-2x480ssd",
			"datacenters": []map[string]any{
				{"datacenter": "gra", "availability": "1H-high"},
				{"datacenter": "rbx", "availability": "unavailable"},
			},
		},
	}

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
						"invoiceName": "KS-A",
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
			_ = json.NewEncoder(w).Encode(availItems)
		case r.Method == http.MethodPost && r.URL.Path == "/order/cart":
			_ = json.NewEncoder(w).Encode(map[string]any{"cartId": "quote-cart-1"})
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/assign"):
			_ = json.NewEncoder(w).Encode(map[string]any{})
		case r.Method == http.MethodPost && r.URL.Path == "/order/cart/quote-cart-1/eco":
			_ = json.NewEncoder(w).Encode(map[string]any{"itemId": 11})
		case r.Method == http.MethodPost && r.URL.Path == "/order/cart/quote-cart-1/item/11/configuration":
			_ = json.NewEncoder(w).Encode(map[string]any{})
		case r.Method == http.MethodGet && r.URL.Path == "/order/cart/quote-cart-1/eco/options":
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{"planCode": "ram-32g-noecc-2133-24ska01", "duration": "P1M", "pricingMode": "default"},
				{"planCode": "softraid-2x480ssd-24ska01", "duration": "P1M", "pricingMode": "default"},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/order/cart/quote-cart-1/eco/options":
			_ = json.NewEncoder(w).Encode(map[string]any{})
		case r.Method == http.MethodGet && r.URL.Path == "/order/cart/quote-cart-1/summary":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"prices": map[string]any{
					"withTax":      map[string]any{"value": 15.99, "currencyCode": "EUR"},
					"withoutTax":   map[string]any{"value": 13.33, "currencyCode": "EUR"},
					"tax":          map[string]any{"value": 2.66, "currencyCode": "EUR"},
					"currencyCode": "EUR",
				},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/order/cart/quote-cart-1":
			_ = json.NewEncoder(w).Encode(map[string]any{"items": []any{}})
		case r.Method == http.MethodDelete && r.URL.Path == "/order/cart/quote-cart-1":
			_ = json.NewEncoder(w).Encode(map[string]any{})
		default:
			http.NotFound(w, r)
		}
	}))
	defer mockOVH.Close()

	env := newTestProcurementEnv(t, mockOVH)

	// 1. Requesting quick-order in unavailable DC (rbx) MUST return 400 NO_PRICED_CONFIG.
	req := httptest.NewRequest(http.MethodPost, "/api/orders/quick-order", strings.NewReader(`{
		"planCode":"24ska01",
		"datacenter":"rbx"
	}`))
	rec := httptest.NewRecorder()
	env.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 NO_PRICED_CONFIG, got %d %s", rec.Code, rec.Body.String())
	}
	var errResp map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &errResp)
	if errResp["success"] != false || errResp["code"] != "NO_PRICED_CONFIG" {
		t.Fatalf("unexpected error payload: %+v", errResp)
	}

	// 2. Requesting quick-order in available DC (gra) enqueues with auto_pay=false & waive=false.
	req = httptest.NewRequest(http.MethodPost, "/api/orders/quick-order", strings.NewReader(`{
		"planCode":"24ska01",
		"datacenter":"gra"
	}`))
	rec = httptest.NewRecorder()
	env.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 quick-order, got %d %s", rec.Code, rec.Body.String())
	}
	var okResp QuickOrderResult
	_ = json.Unmarshal(rec.Body.Bytes(), &okResp)
	if !okResp.Success || okResp.Item == nil || okResp.Item.AutoPay || okResp.Item.WaiveRetractationPeriod || okResp.Item.Source != "manual" {
		t.Fatalf("unexpected quick-order response: %+v", okResp)
	}

	// 3. Duplicate quick-order hits 429 DUPLICATE_QUEUE_TASK.
	req = httptest.NewRequest(http.MethodPost, "/api/orders/quick-order", strings.NewReader(`{
		"planCode":"24ska01",
		"datacenter":"gra"
	}`))
	rec = httptest.NewRecorder()
	env.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 duplicate guard, got %d %s", rec.Code, rec.Body.String())
	}

	// 4. Direct QuickOrder with Source="stock-monitor" without options MUST be rejected with CONFIG_REQUIRED_FOR_AUTO_ORDER.
	noOptRes, noOptStatus, noOptErr := env.svc.QuickOrder(context.Background(), QuickOrderRequest{
		AccountID:          "acc-1",
		PlanCode:           "24ska01",
		Datacenter:         "gra",
		SkipDuplicateCheck: true,
		Source:             "stock-monitor",
		SourceRef:          "24ska01",
	})
	if noOptErr == nil || noOptStatus != http.StatusBadRequest || noOptRes == nil || noOptRes.Code != "CONFIG_REQUIRED_FOR_AUTO_ORDER" || noOptRes.ErrorCode != "CONFIG_REQUIRED_FOR_AUTO_ORDER" {
		t.Fatalf("expected stock-monitor QuickOrder without options to fail with 400 CONFIG_REQUIRED_FOR_AUTO_ORDER, got status=%d err=%v res=%+v", noOptStatus, noOptErr, noOptRes)
	}

	// 5. Direct QuickOrder with Source="stock-monitor", SourceRef="24ska01", and explicit Options persists source linkage.
	monRes, monStatus, monErr := env.svc.QuickOrder(context.Background(), QuickOrderRequest{
		AccountID:          "acc-1",
		PlanCode:           "24ska01",
		Datacenter:         "gra",
		Options:            []string{"ram-32g-noecc-2133-24ska01", "softraid-2x480ssd-24ska01"},
		SkipDuplicateCheck: true,
		Source:             "stock-monitor",
		SourceRef:          "24ska01",
	})
	if monErr != nil || monStatus != http.StatusOK || monRes.Item == nil || monRes.Item.Source != "stock-monitor" || monRes.Item.SourceRef != "24ska01" {
		t.Fatalf("expected stock-monitor source linkage on QuickOrder, got status=%d err=%v item=%+v", monStatus, monErr, monRes.Item)
	}
}

func TestQuoteCreateUnpaidOrderAndQueueWorkerExecution(t *testing.T) {
	var checkoutMu sync.Mutex
	var checkoutPayloads []map[string]any
	var shouldFailCheckout atomic.Bool

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
						{"datacenter": "gra", "availability": "1H-high"},
					},
				},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/order/cart":
			_ = json.NewEncoder(w).Encode(map[string]any{"cartId": "cart-buy-1"})
		case r.Method == http.MethodPost && r.URL.Path == "/order/cart/cart-buy-1/assign":
			_ = json.NewEncoder(w).Encode(map[string]any{})
		case r.Method == http.MethodPost && r.URL.Path == "/order/cart/cart-buy-1/eco":
			_ = json.NewEncoder(w).Encode(map[string]any{"itemId": 42})
		case r.Method == http.MethodPost && r.URL.Path == "/order/cart/cart-buy-1/item/42/configuration":
			_ = json.NewEncoder(w).Encode(map[string]any{})
		case r.Method == http.MethodGet && r.URL.Path == "/order/cart/cart-buy-1/eco/options":
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{"planCode": "ram-32g-noecc-2133-24ska01", "duration": "P1M", "pricingMode": "default"},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/order/cart/cart-buy-1/eco/options":
			_ = json.NewEncoder(w).Encode(map[string]any{})
		case r.Method == http.MethodGet && r.URL.Path == "/order/cart/cart-buy-1/summary":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"prices": map[string]any{
					"withTax":      map[string]any{"value": 12.00, "currencyCode": "EUR"},
					"withoutTax":   map[string]any{"value": 10.00, "currencyCode": "EUR"},
					"tax":          map[string]any{"value": 2.00, "currencyCode": "EUR"},
					"currencyCode": "EUR",
				},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/order/cart/cart-buy-1":
			_ = json.NewEncoder(w).Encode(map[string]any{"items": []any{}})
		case r.Method == http.MethodDelete && r.URL.Path == "/order/cart/cart-buy-1":
			_ = json.NewEncoder(w).Encode(map[string]any{})
		case r.Method == http.MethodGet && r.URL.Path == "/order/cart/cart-buy-1/checkout":
			_ = json.NewEncoder(w).Encode(map[string]any{"contracts": []any{}})
		case r.Method == http.MethodPost && r.URL.Path == "/order/cart/cart-buy-1/checkout":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			checkoutMu.Lock()
			checkoutPayloads = append(checkoutPayloads, body)
			checkoutMu.Unlock()
			if shouldFailCheckout.Load() {
				w.WriteHeader(http.StatusInternalServerError)
				_ = json.NewEncoder(w).Encode(map[string]any{"message": "payment gateway temporarily down"})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"orderId": 987654321,
				"url":     "https://www.ovh.com/order/bill/987654321",
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer mockOVH.Close()

	env := newTestProcurementEnv(t, mockOVH)
	env.svc.SetMonitorRunningFunc(func() bool { return true })

	// 1. QuoteOrder checks monthly pricing without ordering.
	quote, err := env.svc.QuoteOrder(context.Background(), QuoteRequest{
		AccountID:  "acc-1",
		PlanCode:   "24ska01",
		Datacenter: "gra",
		Options:    []string{"ram-32g-noecc-2133-24ska01"},
	})
	if err != nil {
		t.Fatalf("QuoteOrder failed: %v", err)
	}
	if quote.ModelName != "KS-A" || quote.Price == nil || quote.Price.Prices.WithTax == nil || *quote.Price.Prices.WithTax != 12.00 {
		t.Fatalf("unexpected QuoteOrder result: %+v", quote)
	}

	// 2. CreateUnpaidOrder hardcodes autoPay=false, waiveRetractationPeriod=false, quantity=1.
	orderRes, err := env.svc.CreateUnpaidOrder(context.Background(), UnpaidOrderRequest{
		AccountID:  "acc-1",
		PlanCode:   "24ska01",
		Datacenter: "gra",
		Options:    []string{"ram-32g-noecc-2133-24ska01"},
	})
	if err != nil {
		t.Fatalf("CreateUnpaidOrder failed: %v", err)
	}
	if orderRes.AutoPay || orderRes.WaiveRetractationPeriod || orderRes.Quantity != 1 || orderRes.OrderID != "987654321" {
		t.Fatalf("unexpected CreateUnpaidOrder result: %+v", orderRes)
	}

	// Verify OVH checkout POST payload strictly sent false/false.
	checkoutMu.Lock()
	if len(checkoutPayloads) != 1 {
		checkoutMu.Unlock()
		t.Fatalf("expected 1 checkout payload, got %d", len(checkoutPayloads))
	}
	cp := checkoutPayloads[0]
	checkoutMu.Unlock()
	if cp["autoPayWithPreferredPaymentMethod"] != false || cp["waiveRetractationPeriod"] != false {
		t.Fatalf("SECURITY VIOLATION: expected checkout autoPay=false & waive=false, got %+v", cp)
	}

	// 3. Background worker executes a queued task via ProcessDueTasksOnce.
	task, err := env.svc.CreateQueueTask(context.Background(), CreateQueueRequest{
		AccountID:   "acc-1",
		PlanCode:    "24ska01",
		Datacenters: []string{"gra"},
		Options:     []string{"ram-32g-noecc-2133-24ska01"},
		Quantity:    1,
	})
	if err != nil {
		t.Fatalf("CreateQueueTask failed: %v", err)
	}
	env.svc.ProcessDueTasksOnce(context.Background())

	updatedTask, err := env.st.GetQueueItem(context.Background(), task.ID)
	if err != nil {
		t.Fatalf("GetQueueItem failed: %v", err)
	}
	if updatedTask.Status != "completed" || updatedTask.Purchased != 1 {
		t.Fatalf("expected completed task, got %+v", updatedTask)
	}

	// 4. Verify failed checkout emits order.failed and records failure history.
	shouldFailCheckout.Store(true)
	failTask, err := env.svc.CreateQueueTask(context.Background(), CreateQueueRequest{
		AccountID:     "acc-1",
		PlanCode:      "24ska01",
		Datacenters:   []string{"gra"},
		Options:       []string{"ram-32g-noecc-2133-24ska01"},
		MaxRetryCount: 1,
	})
	if err != nil {
		t.Fatalf("CreateQueueTask failed: %v", err)
	}
	env.svc.ProcessDueTasksOnce(context.Background())
	failedTaskState, _ := env.st.GetQueueItem(context.Background(), failTask.ID)
	if failedTaskState.Status != "failed" || failedTaskState.FailureCount != 1 {
		t.Fatalf("expected failed task after 1 max retry, got %+v", failedTaskState)
	}

	// Telegram should have received 2 order.success + 1 order.failed = 3 messages.
	if env.tgPosts.Load() != 3 {
		t.Fatalf("expected 3 telegram order alerts, got %d", env.tgPosts.Load())
	}

	// 5. Verify /api/history, /api/purchase-history, /api/stats, /api/stats/accounts, and DELETE /api/history.
	for _, path := range []string{"/api/history", "/api/purchase-history"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		env.router.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s failed: %d", path, rec.Code)
		}
		var hist []store.PurchaseHistory
		_ = json.Unmarshal(rec.Body.Bytes(), &hist)
		if len(hist) != 3 {
			t.Fatalf("expected 3 history records on %s, got %d", path, len(hist))
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/api/stats", nil)
	rec := httptest.NewRecorder()
	env.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/stats failed: %d", rec.Code)
	}
	var stResp store.Stats
	_ = json.Unmarshal(rec.Body.Bytes(), &stResp)
	if !stResp.MonitorRunning || stResp.PurchaseSuccess != 2 || stResp.PurchaseFailed != 1 {
		t.Fatalf("unexpected /api/stats payload: %+v", stResp)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/stats/accounts", nil)
	rec = httptest.NewRecorder()
	env.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "acc-1") {
		t.Fatalf("unexpected /api/stats/accounts payload: %d %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodDelete, "/api/history", nil)
	rec = httptest.NewRecorder()
	env.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("DELETE /api/history failed: %d", rec.Code)
	}
}

func TestQuickOrderMaxMonthlyPriceSetupFeeAndQuantity(t *testing.T) {
	var checkoutCount atomic.Int32

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
						"pricings":    []map[string]any{{"mode": "default", "capacities": []string{"renew"}, "price": 3500000000}},
						"addonFamilies": []map[string]any{
							{
								"name":    "memory",
								"default": "ram-32g-noecc-2133-24ska01",
								"addons":  []string{"ram-32g-noecc-2133-24ska01", "ram-128g-ecc-2933-24ska01"},
							},
							{
								"name":    "storage",
								"default": "softraid-2x480ssd-24ska01",
								"addons":  []string{"softraid-2x480ssd-24ska01", "softraid-4x3840nvme-24ska01"},
							},
						},
					},
				},
			})
		case r.URL.Path == "/order/catalog/public/baremetalServers" || r.URL.Path == "/order/catalog/public/vps":
			_ = json.NewEncoder(w).Encode(map[string]any{"locale": map[string]any{"currencyCode": "EUR"}, "plans": []any{}})
		case r.URL.Path == "/dedicated/server/datacenter/availabilities":
			// In sbg: default 32G config is unavailable, only expensive 128G config is available.
			// In gra: default 32G config is available.
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{
					"fqn":      "24ska01.ram-32g-noecc-2133.softraid-2x480ssd",
					"planCode": "24ska01",
					"memory":   "ram-32g-noecc-2133",
					"storage":  "softraid-2x480ssd",
					"datacenters": []map[string]any{
						{"datacenter": "gra", "availability": "1H-high"},
						{"datacenter": "sbg", "availability": "unavailable"},
					},
				},
				{
					"fqn":      "24ska01.ram-128g-ecc-2933.softraid-4x3840nvme",
					"planCode": "24ska01",
					"memory":   "ram-128g-ecc-2933",
					"storage":  "softraid-4x3840nvme",
					"datacenters": []map[string]any{
						{"datacenter": "gra", "availability": "1H-high"},
						{"datacenter": "sbg", "availability": "1H-high"},
					},
				},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/order/cart":
			_ = json.NewEncoder(w).Encode(map[string]any{"cartId": "cart-guard-1"})
		case r.Method == http.MethodPost && r.URL.Path == "/order/cart/cart-guard-1/assign":
			_ = json.NewEncoder(w).Encode(map[string]any{})
		case r.Method == http.MethodPost && r.URL.Path == "/order/cart/cart-guard-1/eco":
			_ = json.NewEncoder(w).Encode(map[string]any{"itemId": 77})
		case r.Method == http.MethodPost && r.URL.Path == "/order/cart/cart-guard-1/item/77/configuration":
			_ = json.NewEncoder(w).Encode(map[string]any{})
		case r.Method == http.MethodGet && r.URL.Path == "/order/cart/cart-guard-1/eco/options":
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{"planCode": "ram-32g-noecc-2133-24ska01", "duration": "P1M", "pricingMode": "default"},
				{"planCode": "softraid-2x480ssd-24ska01", "duration": "P1M", "pricingMode": "default"},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/order/cart/cart-guard-1/eco/options":
			_ = json.NewEncoder(w).Encode(map[string]any{})
		case r.Method == http.MethodGet && r.URL.Path == "/order/cart/cart-guard-1/summary":
			// Live cart quote:
			// - Monthly recurring ex-VAT: 35.00 EUR (server) + 10.00 EUR (memory) = 45.00 EUR
			// - One-off installation/setup fee ex-VAT: 50.00 EUR
			// - Total upfront ex-VAT: 95.00 EUR (114.00 EUR with tax)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"prices": map[string]any{
					"withTax":      map[string]any{"value": 114.00, "currencyCode": "EUR"},
					"withoutTax":   map[string]any{"value": 95.00, "currencyCode": "EUR"},
					"tax":          map[string]any{"value": 19.00, "currencyCode": "EUR"},
					"currencyCode": "EUR",
				},
				"details": []map[string]any{
					{
						"detailType":  "DURATION",
						"planCode":    "24ska01",
						"description": "KS-A Server",
						"totalPrice":  map[string]any{"value": 35.00, "currencyCode": "EUR"},
					},
					{
						"detailType":  "DURATION",
						"planCode":    "ram-32g-noecc-2133-24ska01",
						"description": "32GB RAM",
						"totalPrice":  map[string]any{"value": 10.00, "currencyCode": "EUR"},
					},
					{
						"detailType":  "INSTALLATION",
						"planCode":    "installation",
						"description": "Installation fee",
						"totalPrice":  map[string]any{"value": 50.00, "currencyCode": "EUR"},
					},
				},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/order/cart/cart-guard-1":
			_ = json.NewEncoder(w).Encode(map[string]any{"items": []any{}})
		case r.Method == http.MethodDelete && r.URL.Path == "/order/cart/cart-guard-1":
			_ = json.NewEncoder(w).Encode(map[string]any{})
		case r.Method == http.MethodGet && r.URL.Path == "/order/cart/cart-guard-1/checkout":
			_ = json.NewEncoder(w).Encode(map[string]any{"contracts": []any{}})
		case r.Method == http.MethodPost && r.URL.Path == "/order/cart/cart-guard-1/checkout":
			seq := checkoutCount.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"orderId": 1000 + seq,
				"url":     "https://www.ovh.com/order/bill/1000",
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer mockOVH.Close()

	env := newTestProcurementEnv(t, mockOVH)

	// 1. Manual QuickOrder without options in SBG (where default config is unavailable and only expensive 128G is available)
	//    MUST NOT auto-pick the expensive non-default config!
	sbgRes, sbgStatus, _ := env.svc.QuickOrder(context.Background(), QuickOrderRequest{
		AccountID:  "acc-1",
		PlanCode:   "24ska01",
		Datacenter: "sbg",
		Source:     "manual",
	})
	if sbgStatus != http.StatusBadRequest || sbgRes == nil || sbgRes.Code != "NO_PRICED_CONFIG" {
		t.Fatalf("expected manual QuickOrder without options to reject non-default expensive config in sbg, got status=%d res=%+v", sbgStatus, sbgRes)
	}

	// 2. MaxMonthlyPrice exceeded (45.00 EUR monthly ex-VAT > 40.00 EUR cap):
	//    Must abort before checkout, return PRICE_ABOVE_LIMIT, log WARNING, and emit order.failed notification!
	capRes, capStatus, capErr := env.svc.QuickOrder(context.Background(), QuickOrderRequest{
		AccountID:          "acc-1",
		PlanCode:           "24ska01",
		Datacenter:         "gra",
		Options:            []string{"ram-32g-noecc-2133-24ska01", "softraid-2x480ssd-24ska01"},
		ConfigKey:          "ram-32g-noecc-2133+softraid-2x480ssd",
		ConfigLabel:        "32GB + 2x480GB SSD",
		MaxMonthlyPrice:    40.00,
		Quantity:           2,
		SkipDuplicateCheck: true,
		Source:             "stock-monitor",
	})
	if capErr == nil || !strings.Contains(capErr.Error(), "PRICE_ABOVE_LIMIT") {
		t.Fatalf("expected error containing PRICE_ABOVE_LIMIT, got %v", capErr)
	}
	if capStatus != http.StatusBadRequest || capRes == nil || capRes.ErrorCode != "PRICE_ABOVE_LIMIT" || capRes.Code != "PRICE_ABOVE_LIMIT" {
		t.Fatalf("expected 400 QuickOrderResult with ErrorCode=PRICE_ABOVE_LIMIT, got status=%d res=%+v", capStatus, capRes)
	}
	if checkoutCount.Load() != 0 {
		t.Fatalf("expected 0 checkouts when MaxMonthlyPrice exceeded, got %d", checkoutCount.Load())
	}
	if env.tgPosts.Load() != 1 {
		t.Fatalf("expected 1 order.failed notification on PRICE_ABOVE_LIMIT, got %d", env.tgPosts.Load())
	}
	env.tgMu.Lock()
	tgMsg := strings.ReplaceAll(*env.lastTG, `\_`, `_`)
	env.tgMu.Unlock()
	if !strings.Contains(tgMsg, "PRICE_ABOVE_LIMIT") || !strings.Contains(tgMsg, "32GB + 2x480GB SSD") {
		t.Fatalf("expected order.failed notification to mention PRICE_ABOVE_LIMIT and ConfigLabel, got %q", tgMsg)
	}
	warnLogs, _ := env.st.QueryLogs(context.Background(), "WARNING", "queue", "live cart monthly price exceeds configured maxMonthlyPrice", 10)
	if len(warnLogs) == 0 {
		t.Fatalf("expected WARNING log when live cart monthly price exceeds maxMonthlyPrice")
	}

	// 3. MaxMonthlyPrice respected when setup fee is ignored (45.00 EUR monthly ex-VAT <= 50.00 EUR cap, despite 50.00 EUR setup fee = 95.00 EUR upfront),
	//    AND Quantity=3 is honored across checkout & PurchaseHistory!
	okRes, okStatus, okErr := env.svc.QuickOrder(context.Background(), QuickOrderRequest{
		AccountID:          "acc-1",
		PlanCode:           "24ska01",
		Datacenter:         "gra",
		Options:            []string{"ram-32g-noecc-2133-24ska01", "softraid-2x480ssd-24ska01"},
		ConfigKey:          "ram-32g-noecc-2133+softraid-2x480ssd",
		ConfigLabel:        "32GB + 2x480GB SSD",
		MaxMonthlyPrice:    50.00,
		Quantity:           3,
		SkipDuplicateCheck: true,
		Source:             "stock-monitor",
	})
	if okErr != nil || okStatus != http.StatusOK || okRes == nil || !okRes.Success {
		t.Fatalf("expected QuickOrder to succeed when monthly ex-VAT (45.00) <= MaxMonthlyPrice (50.00) ignoring setup fee (50.00), got status=%d err=%v res=%+v", okStatus, okErr, okRes)
	}
	if okRes.MonthlyPrice != 45.00 || okRes.SetupFee != 50.00 || okRes.Quantity != 3 || okRes.Item.Quantity != 3 {
		t.Fatalf("expected MonthlyPrice=45.00, SetupFee=50.00, Quantity=3, got %+v", okRes)
	}

	// Execute queue worker -> must loop 3 times to fulfill Quantity=3!
	env.svc.ProcessDueTasksOnce(context.Background())

	if checkoutCount.Load() != 3 {
		t.Fatalf("expected 3 checkouts for Quantity=3, got %d", checkoutCount.Load())
	}
	completedTask, err := env.st.GetQueueItem(context.Background(), okRes.ID)
	if err != nil || completedTask.Status != "completed" || completedTask.Purchased != 3 || completedTask.Quantity != 3 {
		t.Fatalf("expected completed queue task with Purchased=3/Quantity=3, got %+v err=%v", completedTask, err)
	}

	hist, _ := env.st.ListHistory(context.Background(), "acc-1")
	successCount := 0
	for _, h := range hist {
		if h.Status == "success" {
			successCount++
			if h.Quantity != 3 || h.SetupFee != 50.00 {
				t.Fatalf("expected PurchaseHistory Quantity=3 and SetupFee=50.00, got %+v", h)
			}
		}
	}
	if successCount != 3 {
		t.Fatalf("expected 3 successful PurchaseHistory entries for Quantity=3, got %d", successCount)
	}
}

func TestUpfront12MonthlyEquivalentGuardAndFailFastCouponValidation(t *testing.T) {
	var (
		mu              sync.Mutex
		serverPayloads  []map[string]any
		optionPayloads  []map[string]any
		couponsPosted   []string
		rejectCoupon    atomic.Bool
		summaryCalls    atomic.Int32
		checkoutCalls   atomic.Int32
		deletedCarts    atomic.Int32
		cartCounter     atomic.Int32
	)

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
						"invoiceName": "SYS-2 | Intel Xeon-E 2136",
						"blobs":       map[string]any{"commercial": map[string]any{"name": "SYS-2"}},
						"pricings": []map[string]any{
							{"mode": "default", "capacities": []string{"renew"}, "interval": 1, "price": 3799000000},
							{"mode": "upfront12", "capacities": []string{"renew"}, "interval": 12, "commitment": 12, "price": 34647000000},
						},
						"addonFamilies": []map[string]any{
							{"name": "memory", "default": "ram-32g-ecc-2133-24sys02-v3", "addons": []string{"ram-32g-ecc-2133-24sys02-v3"}},
							{"name": "storage", "default": "softraid-2x450nvme-24sys02-v3", "addons": []string{"softraid-2x450nvme-24sys02-v3"}},
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
						{"datacenter": "gra", "availability": "1H-high"},
					},
				},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/order/cart":
			id := cartCounter.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{"cartId": fmt.Sprintf("cart-promo-%d", id)})
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/assign"):
			_ = json.NewEncoder(w).Encode(map[string]any{})
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/eco"):
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			mu.Lock()
			serverPayloads = append(serverPayloads, body)
			mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]any{"itemId": 101})
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/item/101/configuration"):
			_ = json.NewEncoder(w).Encode(map[string]any{})
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/eco/options"):
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{"planCode": "ram-32g-ecc-2133-24sys02-v3", "duration": "P1M", "pricingMode": "default"},
				{"planCode": "ram-32g-ecc-2133-24sys02-v3", "duration": "P12M", "pricingMode": "upfront12"},
				{"planCode": "softraid-2x450nvme-24sys02-v3", "duration": "P1M", "pricingMode": "default"},
				{"planCode": "softraid-2x450nvme-24sys02-v3", "duration": "P12M", "pricingMode": "upfront12"},
			})
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/eco/options"):
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			mu.Lock()
			optionPayloads = append(optionPayloads, body)
			mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]any{})
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/coupons"):
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			c, _ := body["coupon"].(string)
			mu.Lock()
			couponsPosted = append(couponsPosted, c)
			mu.Unlock()
			if rejectCoupon.Load() {
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]any{"message": "Invalid or expired coupon code"})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{})
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/summary"):
			summaryCalls.Add(1)
			// 12-month upfront discounted quote: €346.47 ex-VAT (€28.87/mo equivalent), free setup (€0.00).
			_ = json.NewEncoder(w).Encode(map[string]any{
				"prices": map[string]any{
					"withTax":      map[string]any{"value": 415.76, "currencyCode": "EUR"},
					"withoutTax":   map[string]any{"value": 346.47, "currencyCode": "EUR"},
					"tax":          map[string]any{"value": 69.29, "currencyCode": "EUR"},
					"currencyCode": "EUR",
				},
				"details": []map[string]any{
					{
						"detailType":  "DURATION",
						"planCode":    "24sys02-v3",
						"description": "SYS-2 12 months upfront",
						"totalPrice":  map[string]any{"value": 433.09, "currencyCode": "EUR"},
					},
					{
						"detailType":  "PROMOTION",
						"planCode":    "BF26_ECO_20OFF",
						"description": "Black Friday -20%",
						"totalPrice":  map[string]any{"value": -86.62, "currencyCode": "EUR"},
					},
				},
			})
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/order/cart/") && !strings.HasSuffix(r.URL.Path, "/checkout"):
			_ = json.NewEncoder(w).Encode(map[string]any{"items": []any{}})
		case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/order/cart/"):
			deletedCarts.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{})
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/checkout"):
			_ = json.NewEncoder(w).Encode(map[string]any{"contracts": []any{}})
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/checkout"):
			seq := checkoutCalls.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"orderId": 5000 + seq,
				"url":     "https://www.ovh.com/order/bill/5000",
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer mockOVH.Close()

	env := newTestProcurementEnv(t, mockOVH)
	opts := []string{"ram-32g-ecc-2133-24sys02-v3", "softraid-2x450nvme-24sys02-v3"}

	// 1. Upfront12 with MaxMonthlyPrice=25.00 MUST block with 400 PRICE_ABOVE_LIMIT (€346.47 / 12 = €28.87 > €25.00).
	lowCapRes, lowCapStatus, lowCapErr := env.svc.QuickOrder(context.Background(), QuickOrderRequest{
		AccountID:          "acc-1",
		PlanCode:           "24sys02-v3",
		Datacenter:         "gra",
		Options:            opts,
		PricingMode:        "upfront12",
		MaxMonthlyPrice:    25.00,
		SkipDuplicateCheck: true,
	})
	if lowCapErr == nil || !errors.Is(lowCapErr, ErrPriceAboveLimit) || lowCapStatus != http.StatusBadRequest {
		t.Fatalf("expected upfront12 with MaxMonthlyPrice=25.00 to fail with 400 ErrPriceAboveLimit, got status=%d err=%v res=%+v", lowCapStatus, lowCapErr, lowCapRes)
	}
	if lowCapRes.Code != "PRICE_ABOVE_LIMIT" || lowCapRes.ErrorCode != "PRICE_ABOVE_LIMIT" || lowCapRes.PeriodPrice != 346.47 || lowCapRes.MonthlyPrice != 28.87 || lowCapRes.PricingMode != "upfront12" || lowCapRes.Duration != "P12M" {
		t.Fatalf("unexpected PRICE_ABOVE_LIMIT result fields: %+v", lowCapRes)
	}

	// 2. Seed an active "default" queue item with identical options, then verify QuickOrder with pricingMode="upfront12"
	//    is NOT treated as a duplicate AND succeeds when MaxMonthlyPrice=30.00 (€28.87 <= €30.00) + valid coupon "BF26"!
	defaultQueue, err := env.svc.CreateQueueTask(context.Background(), CreateQueueRequest{
		AccountID:   "acc-1",
		PlanCode:    "24sys02-v3",
		Datacenters: []string{"gra"},
		Options:     opts,
		PricingMode: "default",
	})
	if err != nil {
		t.Fatalf("seed default queue item: %v", err)
	}
	defaultQueue.Status = "paused"
	_, _ = env.st.UpdateQueueItem(context.Background(), *defaultQueue)

	okRes, okStatus, okErr := env.svc.QuickOrder(context.Background(), QuickOrderRequest{
		AccountID:       "acc-1",
		PlanCode:        "24sys02-v3",
		Datacenter:      "gra",
		Options:         opts,
		PricingMode:     "upfront12",
		Coupon:          "BF26",
		MaxMonthlyPrice: 30.00,
	})
	if okErr != nil || okStatus != http.StatusOK || okRes == nil || !okRes.Success {
		t.Fatalf("expected upfront12 QuickOrder (28.87 <= 30.00) to succeed, got status=%d err=%v res=%+v", okStatus, okErr, okRes)
	}
	if okRes.PeriodPrice != 346.47 || okRes.MonthlyPrice != 28.87 || okRes.SetupFee != 0 || okRes.PricingMode != "upfront12" || okRes.Duration != "P12M" || okRes.Coupon != "BF26" {
		t.Fatalf("unexpected upfront12 QuickOrderResult fields: %+v", okRes)
	}
	if okRes.Item == nil || okRes.Item.PricingMode != "upfront12" || okRes.Item.Duration != "P12M" || okRes.Item.Coupon != "BF26" {
		t.Fatalf("expected QueueItem to persist upfront12/P12M/BF26, got %+v", okRes.Item)
	}

	// Verify cart creation & option payloads used pricingMode="upfront12" and duration="P12M", and posted coupon "BF26".
	mu.Lock()
	if len(serverPayloads) == 0 || serverPayloads[len(serverPayloads)-1]["pricingMode"] != "upfront12" || serverPayloads[len(serverPayloads)-1]["duration"] != "P12M" {
		t.Fatalf("expected server cart payload with pricingMode=upfront12 & duration=P12M, got %+v", serverPayloads)
	}
	if len(optionPayloads) == 0 || optionPayloads[len(optionPayloads)-1]["pricingMode"] != "upfront12" || optionPayloads[len(optionPayloads)-1]["duration"] != "P12M" {
		t.Fatalf("expected option cart payload with pricingMode=upfront12 & duration=P12M, got %+v", optionPayloads)
	}
	if len(couponsPosted) == 0 || couponsPosted[len(couponsPosted)-1] != "BF26" {
		t.Fatalf("expected POST /coupons with BF26, got %+v", couponsPosted)
	}
	mu.Unlock()

	// Duplicate upfront12 QuickOrder (same coupon) while active MUST return 429 DUPLICATE_QUEUE_TASK.
	dupRes, dupStatus, _ := env.svc.QuickOrder(context.Background(), QuickOrderRequest{
		AccountID:      "acc-1",
		PlanCode:       "24sys02-v3",
		Datacenter:     "gra",
		Options:        opts,
		PricingMode:    "upfront12",
		Coupon:         "BF26",
		SkipPriceCheck: true,
	})
	if dupStatus != http.StatusTooManyRequests || dupRes == nil || dupRes.Code != "DUPLICATE_QUEUE_TASK" {
		t.Fatalf("expected duplicate upfront12 QuickOrder to return 429 DUPLICATE_QUEUE_TASK, got status=%d res=%+v", dupStatus, dupRes)
	}

	// Execute queued upfront12 task -> checkout succeeds and records upfront12 history!
	env.svc.ProcessDueTasksOnce(context.Background())
	if checkoutCalls.Load() != 1 {
		t.Fatalf("expected 1 checkout for upfront12 task, got %d", checkoutCalls.Load())
	}
	doneTask, _ := env.st.GetQueueItem(context.Background(), okRes.ID)
	if doneTask == nil || doneTask.Status != "completed" || doneTask.Purchased != 1 {
		t.Fatalf("expected upfront12 queue task completed, got %+v", doneTask)
	}

	// 3. Strict Fail-Fast Coupon Validation: when OVH returns 400 on POST /order/cart/{cartId}/coupons,
	//    BOTH POST /api/orders/quick-order (QuickOrder) AND purchaseServer MUST immediately delete the cart
	//    and abort with 400 INVALID_COUPON without calling /summary or /checkout!
	rejectCoupon.Store(true)
	beforeSummaries := summaryCalls.Load()
	beforeCheckouts := checkoutCalls.Load()
	beforeDeletes := deletedCarts.Load()
	beforeAlerts := env.tgPosts.Load()

	httpReq := httptest.NewRequest(http.MethodPost, "/api/orders/quick-order", strings.NewReader(`{
		"planCode": "24sys02-v3",
		"datacenter": "gra",
		"options": ["ram-32g-ecc-2133-24sys02-v3", "softraid-2x450nvme-24sys02-v3"],
		"pricingMode": "upfront12",
		"coupon": "BAD_COUPON",
		"skipDuplicateCheck": true
	}`))
	httpRec := httptest.NewRecorder()
	env.router.ServeHTTP(httpRec, httpReq)
	if httpRec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 INVALID_COUPON on POST /api/orders/quick-order, got %d %s", httpRec.Code, httpRec.Body.String())
	}
	var badBody map[string]any
	_ = json.Unmarshal(httpRec.Body.Bytes(), &badBody)
	if badBody["code"] != "INVALID_COUPON" || badBody["errorCode"] != "INVALID_COUPON" {
		t.Fatalf("expected code=INVALID_COUPON & errorCode=INVALID_COUPON, got %+v", badBody)
	}
	if summaryCalls.Load() != beforeSummaries {
		t.Fatalf("expected QuickOrder NOT to call /summary after rejected coupon (before=%d after=%d)", beforeSummaries, summaryCalls.Load())
	}
	if checkoutCalls.Load() != beforeCheckouts {
		t.Fatalf("expected QuickOrder NOT to call /checkout after rejected coupon (before=%d after=%d)", beforeCheckouts, checkoutCalls.Load())
	}
	if deletedCarts.Load() <= beforeDeletes {
		t.Fatalf("expected cart to be deleted immediately on rejected coupon")
	}
	if env.tgPosts.Load() <= beforeAlerts {
		t.Fatalf("expected order.failed notification on rejected coupon")
	}

	// Also verify purchaseServer / ProcessDueTasksOnce fail-fast on rejected coupon without calling /checkout!
	badQueueReq := httptest.NewRequest(http.MethodPost, "/api/queue", strings.NewReader(`{
		"planCode": "24sys02-v3",
		"datacenters": ["gra"],
		"options": ["ram-32g-ecc-2133-24sys02-v3", "softraid-2x450nvme-24sys02-v3"],
		"pricingMode": "upfront12",
		"coupon": "EXPIRED_BF",
		"maxRetryCount": 10
	}`))
	badQueueRec := httptest.NewRecorder()
	env.router.ServeHTTP(badQueueRec, badQueueReq)
	if badQueueRec.Code != http.StatusOK {
		t.Fatalf("POST /api/queue failed: %d %s", badQueueRec.Code, badQueueRec.Body.String())
	}
	var createdBadQ struct {
		ID   string          `json:"id"`
		Item store.QueueItem `json:"item"`
	}
	_ = json.Unmarshal(badQueueRec.Body.Bytes(), &createdBadQ)
	if createdBadQ.Item.PricingMode != "upfront12" || createdBadQ.Item.Duration != "P12M" || createdBadQ.Item.Coupon != "EXPIRED_BF" {
		t.Fatalf("expected POST /api/queue to persist upfront12/P12M/EXPIRED_BF, got %+v", createdBadQ.Item)
	}

	beforeSummaries = summaryCalls.Load()
	beforeCheckouts = checkoutCalls.Load()
	env.svc.ProcessDueTasksOnce(context.Background())
	if summaryCalls.Load() != beforeSummaries || checkoutCalls.Load() != beforeCheckouts {
		t.Fatalf("expected purchaseServer NOT to call /summary or /checkout on rejected coupon (summaries %d->%d, checkouts %d->%d)",
			beforeSummaries, summaryCalls.Load(), beforeCheckouts, checkoutCalls.Load())
	}
	failedBadQ, _ := env.st.GetQueueItem(context.Background(), createdBadQ.ID)
	if failedBadQ == nil || failedBadQ.Status != "failed" || !strings.Contains(failedBadQ.LastError, "INVALID_COUPON") {
		t.Fatalf("expected queue task with rejected coupon to fail-fast with INVALID_COUPON, got %+v", failedBadQ)
	}

	couponWarns, _ := env.st.QueryLogs(context.Background(), "WARNING", "queue", "rejected cart coupon", 10)
	if len(couponWarns) < 2 {
		t.Fatalf("expected WARNING logs for rejected coupons in both QuickOrder and purchaseServer, got %d", len(couponWarns))
	}
}

func TestSecurityHotfixTransientCouponInflightLockWildcardAndQuotePriceCap(t *testing.T) {
	env := newTestProcurementEnv(t, nil)
	ctx := context.Background()

	var couponStatus atomic.Int32
	var summaryMonthlyCents atomic.Int64
	var summarySetupCents atomic.Int64
	couponStatus.Store(http.StatusTooManyRequests)
	summaryMonthlyCents.Store(3000) // €30.00
	summarySetupCents.Store(0)      // €0.00

	ovhServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/auth/time"):
			_, _ = w.Write([]byte("1763200000"))
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/dedicated/server/datacenter/availabilities"):
			_, _ = w.Write([]byte(`[{"fqn":"24ska01","planCode":"24ska01","memory":"ram-32g","storage":"softraid-2x480ssd","datacenters":[{"datacenter":"gra","availability":"1H-high"}]}]`))
		case r.Method == http.MethodPost && r.URL.Path == "/1.0/order/cart":
			_, _ = w.Write([]byte(`{"cartId":"cart-sec-1"}`))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/assign"):
			_, _ = w.Write([]byte(`null`))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/eco"):
			_, _ = w.Write([]byte(`{"itemId":101}`))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/configuration"):
			_, _ = w.Write([]byte(`{}`))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/coupons"):
			code := int(couponStatus.Load())
			if code != http.StatusOK {
				w.WriteHeader(code)
				_, _ = w.Write([]byte(`{"message":"temporary rate limit"}`))
				return
			}
			_, _ = w.Write([]byte(`{}`))
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/summary"):
			mVal := float64(summaryMonthlyCents.Load()) / 100.0
			sVal := float64(summarySetupCents.Load()) / 100.0
			_ = json.NewEncoder(w).Encode(map[string]any{
				"prices": map[string]any{
					"withoutTax": map[string]any{"value": mVal + sVal, "currencyCode": "EUR"},
					"withTax":    map[string]any{"value": (mVal + sVal) * 1.2, "currencyCode": "EUR"},
				},
				"details": []map[string]any{
					{"detailType": "DURATION", "totalPrice": map[string]any{"value": mVal, "currencyCode": "EUR"}},
					{"detailType": "INSTALLATION", "totalPrice": map[string]any{"value": sVal, "currencyCode": "EUR"}},
				},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/1.0/order/cart/cart-sec-1":
			_, _ = w.Write([]byte(`{"cartId":"cart-sec-1","items":[]}`))
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/checkout"):
			_, _ = w.Write([]byte(`{}`))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/checkout"):
			_, _ = w.Write([]byte(`{"orderId":778899,"url":"https://www.ovh.com/order/778899"}`))
		case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/1.0/order/cart/"):
			_, _ = w.Write([]byte(`null`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer ovhServer.Close()

	client, err := ovh.NewClient(ovhServer.URL+"/1.0", "ak", "as", "ck")
	if err != nil {
		t.Fatalf("ovh.NewClient: %v", err)
	}
	client.Client = ovhServer.Client()
	env.svc.SetAccountClientFunc(func(context.Context, string) (*ovh.Client, *store.OVHAccount, error) {
		return client, &store.OVHAccount{ID: "acc-1", Endpoint: "ovh-eu", Zone: "IE"}, nil
	})
	env.cat.SetPublicAPIBaseURL(ovhServer.URL + "/1.0")

	// 1. P0-08: Transient HTTP 429 on ApplyCartCoupon keeps queue item running (does NOT fail-fast as INVALID_COUPON).
	task, err := env.svc.CreateQueueTask(ctx, CreateQueueRequest{
		AccountID:     "acc-1",
		PlanCode:      "24ska01",
		Datacenters:   []string{"gra"},
		Options:       []string{"ram-32g"},
		Coupon:        "VIP_429_TEST",
		MaxRetryCount: 10,
	})
	if err != nil {
		t.Fatalf("CreateQueueTask: %v", err)
	}
	env.svc.ProcessDueTasksOnce(ctx)
	after429, _ := env.st.GetQueueItem(ctx, task.ID)
	if after429 == nil || after429.Status != "running" || after429.NextAttemptAt == 0 {
		t.Fatalf("expected transient 429 on coupon to keep task running with backoff, got %+v", after429)
	}
	_ = env.st.DeleteQueueItem(ctx, task.ID)
	couponStatus.Store(http.StatusOK)

	// 2. P0-05: CreateUnpaidOrder with EnforceQuoteCap aborts with ErrPriceChangedSinceQuote if monthly OR setup increases!
	summaryMonthlyCents.Store(3500) // jumped from €30 to €35
	_, err = env.svc.CreateUnpaidOrder(ctx, UnpaidOrderRequest{
		AccountID:       "acc-1",
		PlanCode:        "24ska01",
		Datacenter:      "gra",
		MaxMonthlyPrice: 30.00,
		MaxSetupFee:     0,
		EnforceQuoteCap: true,
	})
	if !errors.Is(err, ErrPriceChangedSinceQuote) {
		t.Fatalf("expected ErrPriceChangedSinceQuote on monthly jump, got %v", err)
	}
	summaryMonthlyCents.Store(3000)
	summarySetupCents.Store(5000) // setup jumped from €0 to €50!
	_, err = env.svc.CreateUnpaidOrder(ctx, UnpaidOrderRequest{
		AccountID:       "acc-1",
		PlanCode:        "24ska01",
		Datacenter:      "gra",
		MaxMonthlyPrice: 30.00,
		MaxSetupFee:     0,
		EnforceQuoteCap: true,
	})
	if !errors.Is(err, ErrPriceChangedSinceQuote) {
		t.Fatalf("expected ErrPriceChangedSinceQuote when free setup jumped to €50, got %v", err)
	}
	summarySetupCents.Store(0)

	// 3. P0-09: Concurrent QuickOrder serialization via inflightLocks + "any" datacenter wildcard dedup.
	var okCount, dupCount atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, stCode, _ := env.svc.QuickOrder(ctx, QuickOrderRequest{
				AccountID:      "acc-1",
				PlanCode:       "24ska01",
				Datacenter:     "gra",
				Options:        []string{"ram-32g"},
				SkipPriceCheck: true,
			})
			if stCode == http.StatusOK {
				okCount.Add(1)
			} else if stCode == http.StatusTooManyRequests {
				dupCount.Add(1)
			}
		}()
	}
	wg.Wait()
	if okCount.Load() != 1 || dupCount.Load() != 5 {
		t.Fatalf("expected exactly 1 QuickOrder success and 5 duplicate rejections, got ok=%d dup=%d", okCount.Load(), dupCount.Load())
	}

	// "any" wildcard matches existing "gra" queue task!
	_, wildStatus, _ := env.svc.QuickOrder(ctx, QuickOrderRequest{
		AccountID:      "acc-1",
		PlanCode:       "24ska01",
		Datacenter:     "any",
		Options:        []string{"ram-32g"},
		SkipPriceCheck: true,
	})
	if wildStatus != http.StatusTooManyRequests {
		t.Fatalf("expected datacenter='any' to match active 'gra' task as 429 duplicate, got %d", wildStatus)
	}
}

func TestSecurityHotfixFollowUpDeferUnlockSummaryFailClosedAndScopedAutoCoupon(t *testing.T) {
	env := newTestProcurementEnv(t, nil)
	ctx := context.Background()

	var summaryFailRemaining atomic.Int32
	var checkoutHits atomic.Int32
	validEnd := time.Now().Add(48 * time.Hour).UTC().Format(time.RFC3339)
	ecoCatalogJSON := fmt.Sprintf(`{
		"locale":{"currencyCode":"EUR"},
		"plans":[
			{
				"planCode":"24ska01",
				"invoiceName":"KS-A",
				"pricings":[
					{
						"mode":"default","capacities":["renew"],"price":3000000000,"interval":1,
						"promotions":[{"name":"BF26_ECO_20OFF","context":"standard","type":"percentage","value":20,"endDate":%q}]
					},
					{
						"mode":"upfront24","capacities":["renew"],"price":72000000000,"interval":24
					}
				]
			},
			{
				"planCode":"26adv01",
				"invoiceName":"ADV-1",
				"pricings":[
					{"mode":"default","capacities":["renew"],"price":8000000000,"interval":1}
				]
			}
		]
	}`, validEnd)

	ovhServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/auth/time"):
			_, _ = w.Write([]byte("1763200000"))
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/order/catalog/public/eco"):
			_, _ = w.Write([]byte(ecoCatalogJSON))
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/order/catalog/public/"):
			_, _ = w.Write([]byte(`{"locale":{"currencyCode":"EUR"},"plans":[],"products":[],"addons":[]}`))
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/dedicated/server/datacenter/availabilities"):
			_, _ = w.Write([]byte(`[{"fqn":"24ska01","planCode":"24ska01","memory":"ram-32g","storage":"softraid-2x480ssd","datacenters":[{"datacenter":"gra","availability":"1H-high"}]}]`))
		case r.Method == http.MethodPost && r.URL.Path == "/1.0/order/cart":
			_, _ = w.Write([]byte(`{"cartId":"cart-fu-1"}`))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/assign"):
			_, _ = w.Write([]byte(`null`))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/eco"):
			_, _ = w.Write([]byte(`{"itemId":101}`))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/configuration"):
			_, _ = w.Write([]byte(`{}`))
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/summary"):
			if rem := summaryFailRemaining.Add(-1); rem >= 0 {
				w.WriteHeader(http.StatusServiceUnavailable)
				_, _ = w.Write([]byte(`{"message":"temporary cart summary outage"}`))
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"prices": map[string]any{
					"withoutTax": map[string]any{"value": 30.00, "currencyCode": "EUR"},
					"withTax":    map[string]any{"value": 36.00, "currencyCode": "EUR"},
				},
				"details": []map[string]any{
					{"detailType": "DURATION", "totalPrice": map[string]any{"value": 30.00, "currencyCode": "EUR"}},
				},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/1.0/order/cart/cart-fu-1":
			_, _ = w.Write([]byte(`{"cartId":"cart-fu-1","items":[]}`))
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/checkout"):
			_, _ = w.Write([]byte(`{}`))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/checkout"):
			checkoutHits.Add(1)
			_, _ = w.Write([]byte(`{"orderId":889900,"url":"https://www.ovh.com/order/889900"}`))
		case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/1.0/order/cart/"):
			_, _ = w.Write([]byte(`null`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer ovhServer.Close()

	client, err := ovh.NewClient(ovhServer.URL+"/1.0", "ak", "as", "ck")
	if err != nil {
		t.Fatalf("ovh.NewClient: %v", err)
	}
	client.Client = ovhServer.Client()
	env.svc.SetAccountClientFunc(func(context.Context, string) (*ovh.Client, *store.OVHAccount, error) {
		return client, &store.OVHAccount{ID: "acc-1", Endpoint: "ovh-eu", Zone: "IE"}, nil
	})
	env.cat.SetPublicAPIBaseURL(ovhServer.URL + "/1.0")

	// 1. Scoped auto-applied coupon stripping: strips ONLY matching (planCode + pricingMode), preserves cross-model & cross-mode.
	qMatch, err := env.svc.CreateQueueTask(ctx, CreateQueueRequest{
		AccountID: "acc-1", PlanCode: "24ska01", Datacenters: []string{"gra"}, PricingMode: "default", Coupon: "bf26_eco_20off",
	})
	if err != nil || qMatch.Coupon != "" {
		t.Fatalf("expected matching (24ska01 + default) auto coupon stripped, got err=%v coupon=%q", err, qMatch.Coupon)
	}
	_ = env.st.DeleteQueueItem(ctx, qMatch.ID)

	qWrongMode, err := env.svc.CreateQueueTask(ctx, CreateQueueRequest{
		AccountID: "acc-1", PlanCode: "24ska01", Datacenters: []string{"gra"}, PricingMode: "upfront24", Coupon: "BF26_ECO_20OFF",
	})
	if err != nil || qWrongMode.Coupon != "BF26_ECO_20OFF" {
		t.Fatalf("expected cross-mode coupon (upfront24) NOT stripped, got err=%v coupon=%q", err, qWrongMode.Coupon)
	}
	_ = env.st.DeleteQueueItem(ctx, qWrongMode.ID)

	qWrongPlan, err := env.svc.CreateQueueTask(ctx, CreateQueueRequest{
		AccountID: "acc-1", PlanCode: "26adv01", Datacenters: []string{"gra"}, PricingMode: "default", Coupon: "BF26_ECO_20OFF",
	})
	if err != nil || qWrongPlan.Coupon != "BF26_ECO_20OFF" {
		t.Fatalf("expected cross-model coupon (26adv01) NOT stripped, got err=%v coupon=%q", err, qWrongPlan.Coupon)
	}
	_ = env.st.DeleteQueueItem(ctx, qWrongPlan.ID)

	// 2. Capped queue task fails-closed when /summary fails (no blind checkout) AND remains running for retry!
	summaryFailRemaining.Store(1)
	cappedTask, err := env.svc.CreateQueueTask(ctx, CreateQueueRequest{
		AccountID:       "acc-1",
		PlanCode:        "24ska01",
		Datacenters:     []string{"gra"},
		MaxMonthlyPrice: 40.00,
		MaxRetryCount:   10,
	})
	if err != nil {
		t.Fatalf("CreateQueueTask capped: %v", err)
	}
	env.svc.ProcessDueTasksOnce(ctx)
	if checkoutHits.Load() != 0 {
		t.Fatalf("expected capped queue task to fail-closed without checkout when /summary fails, got checkoutHits=%d", checkoutHits.Load())
	}
	afterSummaryErr, _ := env.st.GetQueueItem(ctx, cappedTask.ID)
	if afterSummaryErr == nil || afterSummaryErr.Status != "running" || afterSummaryErr.NextAttemptAt == 0 {
		t.Fatalf("expected capped queue task to stay running with retry backoff on /summary failure, got %+v", afterSummaryErr)
	}
	_ = env.st.DeleteQueueItem(ctx, cappedTask.ID)

	// 3. MCP EnforceQuoteCap: single transient 503 on /summary retries and succeeds; persistent 503 returns ErrPriceCheckFailed (not ErrPriceChangedSinceQuote).
	summaryFailRemaining.Store(1)
	res, err := env.svc.CreateUnpaidOrder(ctx, UnpaidOrderRequest{
		AccountID: "acc-1", PlanCode: "24ska01", Datacenter: "gra", MaxMonthlyPrice: 30.00, MaxSetupFee: 0, EnforceQuoteCap: true,
	})
	if err != nil || res == nil || res.OrderID != "889900" {
		t.Fatalf("expected transient single /summary 503 to recover on retry, got err=%v res=%+v", err, res)
	}
	checkoutBefore := checkoutHits.Load()
	summaryFailRemaining.Store(5)
	_, err = env.svc.CreateUnpaidOrder(ctx, UnpaidOrderRequest{
		AccountID: "acc-1", PlanCode: "24ska01", Datacenter: "gra", MaxMonthlyPrice: 30.00, MaxSetupFee: 0, EnforceQuoteCap: true,
	})
	if !errors.Is(err, ErrPriceCheckFailed) || errors.Is(err, ErrPriceChangedSinceQuote) {
		t.Fatalf("expected persistent /summary 503 to return ErrPriceCheckFailed (not ErrPriceChangedSinceQuote), got %v", err)
	}
	if checkoutHits.Load() != checkoutBefore {
		t.Fatalf("expected no checkout on persistent /summary failure")
	}

	// 4. executeQueueTask releases inflightLocks via defer even if purchaseServer panics.
	panicTask, err := env.svc.CreateQueueTask(ctx, CreateQueueRequest{
		AccountID: "acc-1", PlanCode: "24ska01", Datacenters: []string{"gra"}, Options: []string{"ram-panic"},
	})
	if err != nil {
		t.Fatalf("CreateQueueTask panic: %v", err)
	}
	env.svc.SetAccountClientFunc(func(context.Context, string) (*ovh.Client, *store.OVHAccount, error) {
		panic("simulated client lookup panic")
	})
	panicked := false
	func() {
		defer func() {
			if recover() != nil {
				panicked = true
			}
		}()
		env.svc.executeQueueTask(ctx, panicTask.ID)
	}()
	if !panicked {
		t.Fatalf("expected simulated panic during executeQueueTask")
	}
	locked := make(chan struct{})
	go func() {
		unlock := env.svc.lockInflightConfig("acc-1", "24ska01", []string{"ram-panic"}, "default", "")
		unlock()
		close(locked)
	}()
	select {
	case <-locked:
	case <-time.After(2 * time.Second):
		t.Fatalf("inflightLocks deadlocked after panic in executeQueueTask (missing defer unlock)")
	}
}
