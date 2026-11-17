package procurement

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
