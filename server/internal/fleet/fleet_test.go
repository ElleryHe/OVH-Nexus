package fleet

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/ovh/go-ovh/ovh"

	"ovh-nexus/server/internal/auth"
	"ovh-nexus/server/internal/config"
	"ovh-nexus/server/internal/logger"
	"ovh-nexus/server/internal/ovhclient"
	"ovh-nexus/server/internal/store"
	"ovh-nexus/server/internal/vault"
)

const testPanelKey = "TestPanelSecretKey!123456"

type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

type ovhCall struct {
	Method string
	Path   string
	Body   any
}

type mockOVHClient struct {
	mu      sync.Mutex
	calls   []ovhCall
	handler func(method, path string, reqBody any) (any, error)
}

func (m *mockOVHClient) record(method, path string, body any) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, ovhCall{Method: method, Path: path, Body: body})
}

func (m *mockOVHClient) callsCopy() []ovhCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]ovhCall, len(m.calls))
	copy(out, m.calls)
	return out
}

func (m *mockOVHClient) call(method, path string, reqBody, resType any) error {
	m.record(method, path, reqBody)
	if m.handler == nil {
		return nil
	}
	resp, err := m.handler(method, path, reqBody)
	if err != nil {
		return err
	}
	if resType == nil || resp == nil {
		return nil
	}
	raw, err := json.Marshal(resp)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, resType)
}

func (m *mockOVHClient) GetWithContext(_ context.Context, path string, resType any) error {
	return m.call(http.MethodGet, path, nil, resType)
}

func (m *mockOVHClient) PostWithContext(_ context.Context, path string, reqBody, resType any) error {
	return m.call(http.MethodPost, path, reqBody, resType)
}

func (m *mockOVHClient) PutWithContext(_ context.Context, path string, reqBody, resType any) error {
	return m.call(http.MethodPut, path, reqBody, resType)
}

func (m *mockOVHClient) DeleteWithContext(_ context.Context, path string, resType any) error {
	return m.call(http.MethodDelete, path, nil, resType)
}

type fleetTestEnv struct {
	cfg     *config.Config
	store   *store.Store
	ovhPool *ovhclient.Pool
	authSvc *auth.Service
	logHub  *logger.Hub
	logBuf  *syncBuffer
	svc     *Service
	router  chi.Router
	mockOVH *mockOVHClient
}

func setupFleetTest(t *testing.T, withAuthGate bool) *fleetTestEnv {
	t.Helper()
	dir := t.TempDir()
	envMap := map[string]string{
		"API_SECRET_KEY":      testPanelKey,
		"DATA_ENCRYPTION_KEY": "TestDataEncryptionKey!1234567890",
		"DATA_DIR":            dir,
	}
	cfg, err := config.LoadFromEnv(func(k string) (string, bool) {
		v, ok := envMap[k]
		return v, ok
	})
	if err != nil {
		t.Fatalf("LoadFromEnv failed: %v", err)
	}

	v, err := vault.New(cfg.DataEncryptionKey)
	if err != nil {
		t.Fatalf("vault.New failed: %v", err)
	}
	st, err := store.Open(cfg.DataDir, v)
	if err != nil {
		t.Fatalf("store.Open failed: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	logBuf := &syncBuffer{}
	logHub := logger.NewHub(st, logger.Options{
		Level:      "debug",
		Format:     "json",
		Output:     logBuf,
		BufferSize: 200,
	})
	ovhPool := ovhclient.NewPool(st, logHub)
	authSvc, err := auth.NewService(context.Background(), cfg, st, logHub)
	if err != nil {
		t.Fatalf("auth.NewService failed: %v", err)
	}

	svc := NewService(cfg, st, ovhPool, authSvc, logHub)
	svc.ipmiPollInterval = time.Millisecond

	mock := &mockOVHClient{}
	svc.newCredClientFn = func(endpoint, appKey, appSecret, consumerKey string) (OVHClient, error) {
		return mock, nil
	}

	r := chi.NewRouter()
	r.Route("/api", func(api chi.Router) {
		if withAuthGate {
			api.Use(authSvc.RequireAuthAndTOTPGate)
		}
		svc.MountRoutes(api)
	})

	return &fleetTestEnv{
		cfg:     cfg,
		store:   st,
		ovhPool: ovhPool,
		authSvc: authSvc,
		logHub:  logHub,
		logBuf:  logBuf,
		svc:     svc,
		router:  r,
		mockOVH: mock,
	}
}

func (e *fleetTestEnv) attachAccountClient(accID string) {
	e.svc.clientForAccountFn = func(ctx context.Context, requestedID string) (OVHClient, *store.OVHAccount, error) {
		hint := strings.TrimSpace(requestedID)
		if hint == "" {
			hint = accID
		}
		acc, err := e.store.GetActiveAccount(ctx, hint)
		if err != nil {
			return nil, nil, ovhclient.ErrCredentialsMissing
		}
		return e.mockOVH, acc, nil
	}
	e.svc.clientForRequestFn = func(r *http.Request) (OVHClient, *store.OVHAccount, error) {
		hint := ""
		if r != nil {
			hint = strings.TrimSpace(r.Header.Get("X-OVH-Account"))
		}
		return e.svc.clientForAccountFn(r.Context(), hint)
	}
}

func doReq(t *testing.T, h http.Handler, method, target string, body any, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal request body: %v", err)
		}
		reader = bytes.NewReader(raw)
	}
	req := httptest.NewRequest(method, target, reader)
	req.RemoteAddr = "127.0.0.1:12345"
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func decodeMap(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode JSON (%d %s): %v", rec.Code, rec.Body.String(), err)
	}
	return out
}

func TestAccountsCRUDMaskingAndPoolInvalidation(t *testing.T) {
	env := setupFleetTest(t, false)

	// Empty list
	rec := doReq(t, env.router, http.MethodGet, "/api/accounts", nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/accounts status=%d body=%s", rec.Code, rec.Body.String())
	}
	res := decodeMap(t, rec)
	if accts, _ := res["accounts"].([]any); len(accts) != 0 {
		t.Fatalf("expected 0 accounts, got %v", accts)
	}
	if res["defaultAccountId"] != nil {
		t.Fatalf("expected nil defaultAccountId, got %v", res["defaultAccountId"])
	}

	const rawAK = "ak_test_secret_9012"
	const rawAS = "as_very_secret_9999"
	const rawCK = "ck_test_secret_3456"

	env.mockOVH.handler = func(method, path string, _ any) (any, error) {
		if path == "/me" {
			return map[string]any{
				"customerCode": "xx1234-ovh",
				"nichandle":    "xx1234-ovh",
				"email":        "ops@example.com",
			}, nil
		}
		return nil, &ovh.APIError{Code: 404, Message: "Not found", QueryID: "Q-TEST-404"}
	}

	var deletedCallbackID string
	env.svc.SetOnAccountDeleted(func(accountID string) {
		deletedCallbackID = accountID
	})

	// Create first account with auto /me id discovery
	rec = doReq(t, env.router, http.MethodPost, "/api/accounts", map[string]any{
		"alias":       "Main EU",
		"endpoint":    "ovh-eu",
		"zone":        "EU",
		"appKey":      rawAK,
		"appSecret":   rawAS,
		"consumerKey": rawCK,
	}, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /api/accounts status=%d body=%s", rec.Code, rec.Body.String())
	}
	if bodyStr := rec.Body.String(); strings.Contains(bodyStr, rawAK) || strings.Contains(bodyStr, rawAS) || strings.Contains(bodyStr, rawCK) {
		t.Fatalf("POST /api/accounts leaked plaintext credential: %s", bodyStr)
	}
	res = decodeMap(t, rec)
	if res["defaultAccountId"] != "xx1234-ovh" {
		t.Fatalf("expected defaultAccountId=xx1234-ovh, got %v", res["defaultAccountId"])
	}
	accObj := res["account"].(map[string]any)
	if accObj["appSecret"] != "****" {
		t.Fatalf("expected masked appSecret ****, got %v", accObj["appSecret"])
	}
	if accObj["hasAppKey"] != true || accObj["hasAppSecret"] != true || accObj["hasConsumerKey"] != true {
		t.Fatalf("expected has* booleans true, got %+v", accObj)
	}

	// Verify RegisterAccountSecrets + HandleUpsertAccount never expose secrets in logs
	if err := env.svc.RegisterAccountSecrets(context.Background()); err != nil {
		t.Fatalf("RegisterAccountSecrets error: %v", err)
	}
	env.logHub.Info(logger.SourceFleet, "Checking secret redaction", "ak", rawAK, "as", rawAS, "ck", rawCK)
	logs := env.logBuf.String()
	for _, secret := range []string{rawAK, rawAS, rawCK} {
		if strings.Contains(logs, secret) {
			t.Fatalf("plaintext secret %q leaked into logger output: %s", secret, logs)
		}
	}

	// GET /api/accounts must remain strictly masked
	rec = doReq(t, env.router, http.MethodGet, "/api/accounts", nil, nil)
	if bodyStr := rec.Body.String(); strings.Contains(bodyStr, rawAK) || strings.Contains(bodyStr, rawAS) || strings.Contains(bodyStr, rawCK) {
		t.Fatalf("GET /api/accounts leaked plaintext secret: %s", bodyStr)
	}

	// Edit existing account with omitted/masked secrets -> preserves stored credentials
	rec = doReq(t, env.router, http.MethodPost, "/api/accounts", map[string]any{
		"id":          "xx1234-ovh",
		"alias":       "Main EU Renamed",
		"endpoint":    "ovh-eu",
		"appKey":      accObj["appKey"],
		"appSecret":   "****",
		"consumerKey": "",
	}, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("edit POST /api/accounts status=%d body=%s", rec.Code, rec.Body.String())
	}
	stored, err := env.store.GetAccount(context.Background(), "xx1234-ovh")
	if err != nil {
		t.Fatalf("GetAccount error: %v", err)
	}
	if stored.Alias != "Main EU Renamed" || stored.AppKey != rawAK || stored.AppSecret != rawAS || stored.ConsumerKey != rawCK {
		t.Fatalf("stored credentials were not preserved on edit: %+v", stored)
	}

	// Mismatched OVH /me account must be rejected
	env.mockOVH.handler = func(method, path string, _ any) (any, error) {
		if path == "/me" {
			return map[string]any{
				"customerCode": "other-ovh",
				"nichandle":    "other-ovh",
				"email":        "other@example.com",
			}, nil
		}
		return nil, nil
	}
	rec = doReq(t, env.router, http.MethodPost, "/api/accounts", map[string]any{
		"id":          "xx1234-ovh",
		"appKey":      "ak_new_12345678",
		"appSecret":   "as_new_12345678",
		"consumerKey": "ck_new_12345678",
	}, nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 on mismatched account /me, got %d body=%s", rec.Code, rec.Body.String())
	}

	// Resolve info success + error with OVH-Query-ID
	rec = doReq(t, env.router, http.MethodPost, "/api/accounts/resolve-info", map[string]any{
		"appKey":      "ak_resolve_1234",
		"appSecret":   "as_resolve_1234",
		"consumerKey": "ck_resolve_1234",
		"endpoint":    "ovh-ca",
	}, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("resolve-info failed: %d %s", rec.Code, rec.Body.String())
	}
	rMap := decodeMap(t, rec)
	if rMap["customerCode"] != "other-ovh" || rMap["email"] != "other@example.com" {
		t.Fatalf("unexpected resolve-info payload: %+v", rMap)
	}

	env.mockOVH.handler = func(method, path string, _ any) (any, error) {
		return nil, &ovh.APIError{Code: 403, Message: "Invalid signature", QueryID: "Q-RESOLVE-99"}
	}
	rec = doReq(t, env.router, http.MethodPost, "/api/accounts/resolve-info", map[string]any{
		"appKey":      "ak_resolve_1234",
		"appSecret":   "as_resolve_1234",
		"consumerKey": "ck_resolve_1234",
	}, nil)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "Q-RESOLVE-99") {
		t.Fatalf("expected 400 with OVH-Query-ID Q-RESOLVE-99, got %d %s", rec.Code, rec.Body.String())
	}

	// Add second account directly, switch default, seed queue+subs, then delete it
	env.mockOVH.handler = func(method, path string, _ any) (any, error) {
		return map[string]any{"customerCode": "ca9999-ovh", "nichandle": "ca9999-ovh", "email": "ca@example.com"}, nil
	}
	rec = doReq(t, env.router, http.MethodPost, "/api/accounts", map[string]any{
		"alias":       "CA Account",
		"endpoint":    "ovh-ca",
		"zone":        "CA",
		"appKey":      "ak_ca_12345678",
		"appSecret":   "as_ca_12345678",
		"consumerKey": "ck_ca_12345678",
	}, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST second account status=%d body=%s", rec.Code, rec.Body.String())
	}

	rec = doReq(t, env.router, http.MethodPut, "/api/accounts/default", map[string]any{"id": "ca9999-ovh"}, nil)
	if rec.Code != http.StatusOK || decodeMap(t, rec)["defaultAccountId"] != "ca9999-ovh" {
		t.Fatalf("PUT /api/accounts/default failed: %d %s", rec.Code, rec.Body.String())
	}

	if _, err := env.store.CreateQueueItem(context.Background(), store.QueueItem{
		ID:        "q-1",
		AccountID: "ca9999-ovh",
		PlanCode:  "24ska01",
		Quantity:  1,
		Status:    "pending",
	}); err != nil {
		t.Fatalf("CreateQueueItem error: %v", err)
	}
	if _, err := env.store.UpsertStockSub(context.Background(), store.StockSubscription{
		AccountID: "ca9999-ovh",
		PlanCode:  "24ska01",
	}); err != nil {
		t.Fatalf("UpsertStockSub error: %v", err)
	}

	rec = doReq(t, env.router, http.MethodDelete, "/api/accounts/ca9999-ovh", nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("DELETE /api/accounts/ca9999-ovh failed: %d %s", rec.Code, rec.Body.String())
	}
	if deletedCallbackID != "ca9999-ovh" {
		t.Fatalf("expected onAccountDeleted(ca9999-ovh), got %q", deletedCallbackID)
	}
	if qItems, _ := env.store.ListQueue(context.Background(), "ca9999-ovh"); len(qItems) != 0 {
		t.Fatalf("expected queue cleared on account delete, remaining=%d", len(qItems))
	}
	if subs, _ := env.store.ListStockSubs(context.Background(), "ca9999-ovh"); len(subs) != 0 {
		t.Fatalf("expected stock subs cleared on account delete, remaining=%d", len(subs))
	}
	if decodeMap(t, rec)["defaultAccountId"] != "xx1234-ovh" {
		t.Fatalf("expected fallback defaultAccountId=xx1234-ovh, got %s", rec.Body.String())
	}
}

func TestSettingsVerifyAuthDebugAndEndpointConfig(t *testing.T) {
	env := setupFleetTest(t, false)

	// Legacy webhook routes must return 404
	for _, legacyPath := range []string{"/api/telegram/webhook", "/api/telegram/set-webhook"} {
		rec := doReq(t, env.router, http.MethodPost, legacyPath, map[string]any{}, nil)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("legacy route %s should return 404, got %d", legacyPath, rec.Code)
		}
	}

	// Verify default settings uses ovh-nexus-<zone> (ovh-eu defaults to IE -> ovh-nexus-ie)
	rec := doReq(t, env.router, http.MethodGet, "/api/settings", nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/settings status=%d", rec.Code)
	}
	settings := decodeMap(t, rec)
	if settings["iam"] != "ovh-nexus-ie" {
		t.Fatalf("expected default iam=ovh-nexus-ie, got %v", settings["iam"])
	}
	if settings["hasTgToken"] != false || settings["tgToken"] != "" {
		t.Fatalf("unexpected legacy telegram token indicators: %+v", settings)
	}

	// Verify auth returns valid:false when no credentials exist
	rec = doReq(t, env.router, http.MethodGet, "/api/verify-auth", nil, nil)
	if rec.Code != http.StatusOK || decodeMap(t, rec)["valid"] != false {
		t.Fatalf("expected valid:false when unconfigured, got %d %s", rec.Code, rec.Body.String())
	}

	// Save settings with CA credentials & check dynamic iam = ovh-nexus-ca
	const caAK = "ak_settings_1234"
	const caAS = "as_settings_1234"
	const caCK = "ck_settings_1234"
	env.mockOVH.handler = func(method, path string, _ any) (any, error) {
		if path == "/me" {
			return map[string]any{
				"customerCode": "ca5555-ovh",
				"nichandle":    "ca5555-ovh",
				"email":        "ca-ops@example.com",
			}, nil
		}
		return nil, nil
	}

	rec = doReq(t, env.router, http.MethodPost, "/api/settings", map[string]any{
		"endpoint":    "ovh-ca",
		"zone":        "CA",
		"appKey":      caAK,
		"appSecret":   caAS,
		"consumerKey": caCK,
	}, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /api/settings failed: %d %s", rec.Code, rec.Body.String())
	}

	rec = doReq(t, env.router, http.MethodGet, "/api/settings", nil, nil)
	settings = decodeMap(t, rec)
	if settings["iam"] != "ovh-nexus-ca" || settings["zone"] != "CA" {
		t.Fatalf("expected iam=ovh-nexus-ca zone=CA, got %+v", settings)
	}
	if bodyStr := rec.Body.String(); strings.Contains(bodyStr, caAK) || strings.Contains(bodyStr, caAS) || strings.Contains(bodyStr, caCK) {
		t.Fatalf("GET /api/settings leaked plaintext secret: %s", bodyStr)
	}
	if settings["hasAppKey"] != true || settings["hasAppSecret"] != true || settings["hasConsumerKey"] != true {
		t.Fatalf("expected has* booleans true, got %+v", settings)
	}

	// Endpoint config reflects active account
	rec = doReq(t, env.router, http.MethodGet, "/api/endpoint-config", nil, nil)
	epCfg := decodeMap(t, rec)
	if epCfg["endpoint"] != "ovh-ca" || epCfg["zone"] != "CA" {
		t.Fatalf("unexpected /api/endpoint-config payload: %+v", epCfg)
	}

	// Debug account exposes only safe metadata + booleans
	rec = doReq(t, env.router, http.MethodGet, "/api/debug/account", nil, map[string]string{"X-OVH-Account": "ca5555-ovh"})
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/debug/account status=%d", rec.Code)
	}
	if bodyStr := rec.Body.String(); strings.Contains(bodyStr, caAK) || strings.Contains(bodyStr, caAS) || strings.Contains(bodyStr, caCK) {
		t.Fatalf("GET /api/debug/account leaked secret: %s", bodyStr)
	}
	dbg := decodeMap(t, rec)
	if dbg["exists"] != true || dbg["hasAppKey"] != true || dbg["hasAppSecret"] != true || dbg["hasConsumerKey"] != true {
		t.Fatalf("unexpected debug account payload: %+v", dbg)
	}

	// Verify-auth via attached account client succeeds
	env.attachAccountClient("ca5555-ovh")
	rec = doReq(t, env.router, http.MethodGet, "/api/verify-auth", nil, nil)
	vMap := decodeMap(t, rec)
	if vMap["valid"] != true {
		t.Fatalf("expected verify-auth true, got %+v", vMap)
	}
	rec = doReq(t, env.router, http.MethodPost, "/api/verify-auth", map[string]any{
		"appKey":      "ak_inline_1234",
		"appSecret":   "as_inline_1234",
		"consumerKey": "ck_inline_1234",
		"endpoint":    "ovh-us",
	}, nil)
	if decodeMap(t, rec)["valid"] != true {
		t.Fatalf("expected POST /api/verify-auth valid:true, got %s", rec.Body.String())
	}

	// Verify clearCredentials wipes credentials cleanly
	rec = doReq(t, env.router, http.MethodPost, "/api/settings", map[string]any{
		"clearCredentials": true,
	}, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("clearCredentials failed: %d %s", rec.Code, rec.Body.String())
	}
	rec = doReq(t, env.router, http.MethodGet, "/api/settings", nil, nil)
	settings = decodeMap(t, rec)
	if settings["hasAppKey"] != false || settings["hasAppSecret"] != false || settings["hasConsumerKey"] != false {
		t.Fatalf("expected credentials cleared, got %+v", settings)
	}
}

func TestOVHAccountHubEndpoints(t *testing.T) {
	env := setupFleetTest(t, false)

	// Missing credentials error structure
	rec := doReq(t, env.router, http.MethodGet, "/api/ovh/account/info", nil, nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 when credentials missing, got %d", rec.Code)
	}
	if decodeMap(t, rec)["code"] != "OVH_CREDENTIALS_MISSING" {
		t.Fatalf("expected OVH_CREDENTIALS_MISSING, got %s", rec.Body.String())
	}

	if _, err := env.store.UpsertAccount(context.Background(), store.OVHAccount{
		ID:          "hub-acc",
		Alias:       "Hub",
		Endpoint:    "ovh-eu",
		Zone:        "EU",
		AppKey:      "ak_hub_12345678",
		AppSecret:   "as_hub_12345678",
		ConsumerKey: "ck_hub_12345678",
		IsDefault:   true,
	}); err != nil {
		t.Fatalf("seed account: %v", err)
	}
	env.attachAccountClient("hub-acc")

	soonExp := time.Now().UTC().Add(5 * 24 * time.Hour).Format("2006-01-02")
	laterExp := time.Now().UTC().Add(30 * 24 * time.Hour).Format("2006-01-02")

	env.mockOVH.handler = func(method, path string, reqBody any) (any, error) {
		switch {
		case method == http.MethodGet && path == "/me":
			return map[string]any{"nichandle": "hub-acc", "email": "hub@example.com"}, nil
		case method == http.MethodGet && path == "/me/order":
			return []int64{100, 101, 102}, nil
		case method == http.MethodGet && path == "/me/order/102":
			return map[string]any{"orderId": 102, "date": "2026-11-16T00:00:00Z", "priceWithTax": map[string]any{"text": "29.99 EUR"}}, nil
		case method == http.MethodGet && path == "/me/order/101":
			return map[string]any{"orderId": 101, "date": "2026-11-15T00:00:00Z"}, nil
		case method == http.MethodGet && path == "/me/order/101/followUp":
			return []map[string]any{{"step": "DELIVERED", "status": "DONE"}}, nil
		case method == http.MethodGet && path == "/me/order/101/details":
			return []int64{1}, nil
		case method == http.MethodGet && path == "/me/order/101/details/1":
			return map[string]any{"orderDetailId": 1, "description": "Dedicated Server"}, nil
		case method == http.MethodGet && path == "/me/order/101/status":
			return "delivered", nil
		case method == http.MethodGet && path == "/dedicated/server":
			return []string{"ns1.ip.net"}, nil
		case method == http.MethodGet && path == "/dedicated/server/ns1.ip.net/serviceInfos":
			return map[string]any{
				"expiration": laterExp,
				"status":     "ok",
				"renew":      map[string]any{"automatic": true, "period": 1},
			}, nil
		case method == http.MethodGet && path == "/vps":
			return []string{"vps-1.ovh.net"}, nil
		case method == http.MethodGet && path == "/vps/vps-1.ovh.net/serviceInfos":
			return map[string]any{
				"expiration": soonExp,
				"status":     "ok",
				"renew":      map[string]any{"deleteAtExpiration": true},
			}, nil
		case method == http.MethodGet && path == "/vps/vps-1.ovh.net":
			return map[string]any{"name": "vps-1.ovh.net", "state": "running", "vcore": 2, "memoryLimit": 4096}, nil
		case method == http.MethodGet && path == "/vps/vps-1.ovh.net/ips":
			return []string{"198.51.100.10"}, nil
		case method == http.MethodPost && path == "/vps/vps-1.ovh.net/reboot":
			return map[string]any{"id": 77, "state": "todo"}, nil
		case method == http.MethodGet && path == "/me/sshKey":
			return []string{"laptop"}, nil
		case method == http.MethodGet && path == "/me/sshKey/laptop":
			return map[string]any{"keyName": "laptop", "key": "ssh-ed25519 AAAA", "default": true}, nil
		case method == http.MethodPost && path == "/me/sshKey":
			return map[string]any{"keyName": "deploy"}, nil
		case method == http.MethodPut && path == "/me/sshKey/laptop":
			return nil, nil
		case method == http.MethodDelete && path == "/me/sshKey/laptop":
			return nil, nil
		case method == http.MethodGet && path == "/me/credit/balance":
			return []string{"MAIN"}, nil
		case method == http.MethodGet && path == "/me/credit/balance/MAIN":
			return map[string]any{"balanceName": "MAIN", "amount": map[string]any{"text": "50.00 EUR"}}, nil
		case method == http.MethodGet && path == "/me/credit/balance/MAIN/movement":
			return []int64{1}, nil
		case method == http.MethodGet && path == "/me/credit/balance/MAIN/movement/1":
			return map[string]any{"movementId": 1, "amount": map[string]any{"text": "50.00 EUR"}}, nil
		case method == http.MethodGet && path == "/me/bill":
			return []string{"BILL-1"}, nil
		case method == http.MethodGet && path == "/me/bill/BILL-1":
			return map[string]any{"billId": "BILL-1"}, nil
		case method == http.MethodGet && path == "/me/refund":
			return []string{"REF-1"}, nil
		case method == http.MethodGet && path == "/me/refund/REF-1":
			return map[string]any{"refundId": "REF-1"}, nil
		case method == http.MethodGet && path == "/me/notification/email/history":
			return []int64{9}, nil
		case method == http.MethodGet && path == "/me/notification/email/history/9":
			return map[string]any{"id": 9, "subject": "Order confirmation"}, nil
		case method == http.MethodGet && path == "/me/subAccount":
			return []int64{10}, nil
		case method == http.MethodGet && path == "/me/subAccount/10":
			return map[string]any{"id": 10, "name": "ci"}, nil
		case method == http.MethodGet && path == "/me/task/contactChange":
			return []int64{55}, nil
		case method == http.MethodGet && path == "/me/task/contactChange/55":
			return map[string]any{"id": 55, "state": "todo"}, nil
		case method == http.MethodPost && path == "/me/task/contactChange/55/accept":
			return nil, nil
		case method == http.MethodPost && path == "/me/task/contactChange/55/refuse":
			return nil, nil
		case method == http.MethodPost && path == "/me/task/contactChange/55/resendEmail":
			return nil, nil
		}
		return nil, fmt.Errorf("unexpected mock call %s %s", method, path)
	}

	// Account info & orders
	rec = doReq(t, env.router, http.MethodGet, "/api/ovh/account/info", nil, nil)
	if rec.Code != http.StatusOK || decodeMap(t, rec)["status"] != "success" {
		t.Fatalf("account/info failed: %d %s", rec.Code, rec.Body.String())
	}

	rec = doReq(t, env.router, http.MethodGet, "/api/ovh/account/orders?limit=2", nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("account/orders failed: %d %s", rec.Code, rec.Body.String())
	}
	ordersData := decodeMap(t, rec)["data"].([]any)
	if len(ordersData) != 2 {
		t.Fatalf("expected 2 orders with limit=2, got %d", len(ordersData))
	}

	rec = doReq(t, env.router, http.MethodGet, "/api/ovh/account/orders/101/follow-up", nil, nil)
	if rec.Code != http.StatusOK || decodeMap(t, rec)["status"] != "success" {
		t.Fatalf("order follow-up failed: %d %s", rec.Code, rec.Body.String())
	}

	// Services overview sorted by daysRemaining ascending (VPS 5d before dedicated 30d)
	rec = doReq(t, env.router, http.MethodGet, "/api/ovh/account/services-overview", nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("services-overview failed: %d %s", rec.Code, rec.Body.String())
	}
	svcs := decodeMap(t, rec)["data"].([]any)
	if len(svcs) != 2 || svcs[0].(map[string]any)["serviceName"] != "vps-1.ovh.net" {
		t.Fatalf("expected vps-1.ovh.net sorted first by daysRemaining, got %+v", svcs)
	}
	if svcs[0].(map[string]any)["renewMode"] != "deleteAtExpiration" || svcs[1].(map[string]any)["renewMode"] != "auto" {
		t.Fatalf("unexpected renewMode values: %+v", svcs)
	}

	// SSH keys CRUD
	for _, tc := range []struct {
		method string
		path   string
		body   any
	}{
		{http.MethodGet, "/api/ovh/account/ssh-keys", nil},
		{http.MethodPost, "/api/ovh/account/ssh-keys", map[string]any{"keyName": "deploy", "key": "ssh-ed25519 BBBB", "default": false}},
		{http.MethodPut, "/api/ovh/account/ssh-keys/laptop", map[string]any{"default": false}},
		{http.MethodDelete, "/api/ovh/account/ssh-keys/laptop", nil},
	} {
		rec = doReq(t, env.router, tc.method, tc.path, tc.body, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s %s failed: %d %s", tc.method, tc.path, rec.Code, rec.Body.String())
		}
	}

	// VPS & Account misc endpoints
	for _, path := range []string{
		"/api/ovh/vps/list",
		"/api/ovh/account/refunds",
		"/api/ovh/account/email-history",
		"/api/ovh/account/credit-balance",
		"/api/ovh/account/sub-accounts",
		"/api/ovh/account/bills",
		"/api/ovh/contact-change-requests",
		"/api/ovh/contact-change-requests/55",
	} {
		rec = doReq(t, env.router, http.MethodGet, path, nil, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s failed: %d %s", path, rec.Code, rec.Body.String())
		}
	}
	rec = doReq(t, env.router, http.MethodPost, "/api/ovh/vps/vps-1.ovh.net/reboot", nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST vps reboot failed: %d %s", rec.Code, rec.Body.String())
	}

	// Contact change mutations (accept requires token)
	rec = doReq(t, env.router, http.MethodPost, "/api/ovh/contact-change-requests/55/accept", map[string]any{}, nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 when accept token missing, got %d", rec.Code)
	}
	for _, tc := range []struct {
		path string
		body any
	}{
		{"/api/ovh/contact-change-requests/55/accept", map[string]any{"token": "tok-123"}},
		{"/api/ovh/contact-change-requests/55/refuse", map[string]any{"token": "tok-123"}},
		{"/api/ovh/contact-change-requests/55/resend-email", map[string]any{}},
	} {
		rec = doReq(t, env.router, http.MethodPost, tc.path, tc.body, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("POST %s failed: %d %s", tc.path, rec.Code, rec.Body.String())
		}
	}
}

func TestServerControlEndpoints(t *testing.T) {
	env := setupFleetTest(t, false)
	if _, err := env.store.UpsertAccount(context.Background(), store.OVHAccount{
		ID:          "sc-acc",
		Alias:       "SC",
		Endpoint:    "ovh-eu",
		Zone:        "EU",
		AppKey:      "ak_sc_12345678",
		AppSecret:   "as_sc_12345678",
		ConsumerKey: "ck_sc_12345678",
		IsDefault:   true,
	}); err != nil {
		t.Fatalf("seed account: %v", err)
	}
	env.attachAccountClient("sc-acc")

	const srv = "ns123456.ip-198-51-100.eu"
	installStatusNotFound := false
	hardwarePendingErr := false

	env.mockOVH.handler = func(method, path string, reqBody any) (any, error) {
		switch {
		case method == http.MethodGet && path == "/dedicated/server":
			return []string{srv}, nil
		case method == http.MethodGet && path == "/dedicated/server/"+srv:
			return map[string]any{"name": srv, "datacenter": "rbx", "state": "ok", "ip": "198.51.100.10", "commercialRange": "Rise-1"}, nil
		case method == http.MethodGet && path == "/dedicated/server/"+srv+"/serviceInfos":
			return map[string]any{"status": "ok", "expiration": "2027-01-01", "creation": "2026-01-01", "renew": map[string]any{"automatic": true, "period": 1}}, nil
		case method == http.MethodGet && path == "/dedicated/server/"+srv+"/specifications/hardware":
			return map[string]any{
				"processorName": "AMD EPYC",
				"numberOfProcessors": 1,
				"coresPerProcessor": 8,
				"threadsPerProcessor": 16,
				"memorySize": map[string]any{"value": 64, "unit": "GB"},
				"diskGroups": []any{
					map[string]any{"diskGroupId": 1, "numberOfDisks": 2, "diskType": "NVMe", "diskSize": map[string]any{"value": 960, "unit": "GB"}},
				},
			}, nil
		case method == http.MethodGet && path == "/dedicated/server/"+srv+"/install/hardwareRaidProfile":
			return []map[string]any{{"name": "raid1"}}, nil
		case method == http.MethodGet && path == "/dedicated/server/"+srv+"/install/compatibleTemplates":
			return map[string]any{"ovh": []string{"proxmox9_64", "debian12_64"}, "personal": []string{}}, nil
		case method == http.MethodGet && strings.HasPrefix(path, "/dedicated/server/"+srv+"/install/compatibleTemplatePartitionSchemes"):
			return []string{"default"}, nil
		case method == http.MethodGet && path == "/dedicated/server/"+srv+"/install/templatePartitions":
			return []string{"/"}, nil
		case method == http.MethodGet && strings.HasPrefix(path, "/dedicated/installationTemplate/"):
			if strings.HasSuffix(path, "/partitionScheme") {
				return []string{"default"}, nil
			}
			if strings.HasSuffix(path, "/partitionScheme/default/partition") {
				return []string{"/"}, nil
			}
			if strings.Contains(path, "/partitionScheme/default/partition/") {
				return map[string]any{"filesystem": "ext4", "mountpoint": "/", "size": map[string]any{"value": 0, "unit": "MB"}, "order": 1, "raid": "1", "type": "primary"}, nil
			}
			if strings.HasSuffix(path, "/partitionScheme/default") {
				return map[string]any{"name": "default", "priority": 1}, nil
			}
			return map[string]any{"templateName": "proxmox9_64", "family": "linux", "description": "Proxmox VE 9"}, nil
		case method == http.MethodGet && path == "/me/sshKey/mykey":
			return map[string]any{"keyName": "mykey", "key": "ssh-ed25519 AAAAC3"}, nil
		case method == http.MethodPost && path == "/dedicated/server/"+srv+"/reinstall":
			return map[string]any{"taskId": 901}, nil
		case method == http.MethodGet && path == "/dedicated/server/"+srv+"/install/status":
			if installStatusNotFound {
				return nil, &ovh.APIError{Code: 404, Message: "No installation in progress"}
			}
			return map[string]any{
				"elapsedTime": 120,
				"progress": []map[string]any{
					{"comment": "Partitioning disks", "status": "done", "error": ""},
					{"comment": "Installing packages", "status": "doing", "error": ""},
				},
			}, nil
		case method == http.MethodGet && path == "/dedicated/server/"+srv+"/task":
			return []int64{701}, nil
		case method == http.MethodGet && path == "/dedicated/server/"+srv+"/task/701":
			return map[string]any{"taskId": 701, "function": "hardReboot", "status": "done"}, nil
		case method == http.MethodGet && strings.HasPrefix(path, "/dedicated/server/"+srv+"/task/701/availableTimeslots"):
			return []string{"2026-11-17T02:00:00Z"}, nil
		case method == http.MethodPost && path == "/dedicated/server/"+srv+"/task/701/schedule":
			return map[string]any{"taskId": 701, "status": "todo"}, nil
		case method == http.MethodPost && path == "/dedicated/server/"+srv+"/reboot":
			return map[string]any{"taskId": 702, "status": "todo"}, nil
		case method == http.MethodGet && path == "/dedicated/server/"+srv+"/boot":
			return []int64{1, 2}, nil
		case method == http.MethodGet && path == "/dedicated/server/"+srv+"/boot/1":
			return map[string]any{"bootId": 1, "bootType": "harddisk", "description": "Boot from disk"}, nil
		case method == http.MethodGet && path == "/dedicated/server/"+srv+"/boot/2":
			return map[string]any{"bootId": 2, "bootType": "rescue", "description": "Rescue"}, nil
		case method == http.MethodPut && path == "/dedicated/server/"+srv:
			return nil, nil
		case method == http.MethodGet && path == "/dedicated/server/"+srv+"/features/ipmi":
			return map[string]any{"activated": true, "supportedFeatures": map[string]any{"kvmipHtml5URL": true}}, nil
		case method == http.MethodPost && path == "/dedicated/server/"+srv+"/features/ipmi/access":
			return map[string]any{"taskId": 701}, nil
		case method == http.MethodGet && strings.HasPrefix(path, "/dedicated/server/"+srv+"/features/ipmi/access"):
			return map[string]any{"value": "https://ipmi.example/console", "expiration": "2026-11-16T10:15:00Z"}, nil
		case method == http.MethodGet && path == "/dedicated/server/"+srv+"/ips":
			return []string{"198.51.100.10/32"}, nil
		case method == http.MethodGet && path == "/dedicated/server/"+srv+"/specifications/ip":
			return []map[string]any{{"ip": "198.51.100.10/32"}}, nil
		case method == http.MethodGet && path == "/dedicated/server/"+srv+"/specifications/network":
			return map[string]any{"routing": map[string]any{"ipv4": map[string]any{"ip": "198.51.100.10"}}}, nil
		case method == http.MethodGet && path == "/dedicated/server/"+srv+"/networkInterfaceController":
			return []string{"00:11:22:33:44:55"}, nil
		case method == http.MethodGet && path == "/dedicated/server/"+srv+"/networkInterfaceController/00:11:22:33:44:55":
			return map[string]any{"mac": "00:11:22:33:44:55", "linkType": "public"}, nil
		case method == http.MethodGet && strings.HasPrefix(path, "/dedicated/server/"+srv+"/statistics"):
			return []map[string]any{{"timestamp": 1700000000, "value": 100}}, nil
		case method == http.MethodGet && path == "/dedicated/server/"+srv+"/burst":
			return map[string]any{"status": "inactive", "capacity": 1000}, nil
		case method == http.MethodPut && path == "/dedicated/server/"+srv+"/burst":
			return nil, nil
		case method == http.MethodGet && path == "/dedicated/server/"+srv+"/features/firewall":
			return map[string]any{"enabled": true, "mode": "routed"}, nil
		case method == http.MethodPut && path == "/dedicated/server/"+srv+"/features/firewall":
			return map[string]any{"status": "done"}, nil
		case method == http.MethodGet && strings.HasPrefix(path, "/ip/"):
			if strings.HasSuffix(path, "/firewall") {
				return []string{"198.51.100.10"}, nil
			}
			if strings.HasSuffix(path, "/firewall/198.51.100.10") {
				return map[string]any{"enabled": true, "state": "ok"}, nil
			}
			if strings.Contains(path, "/rule") {
				if method == http.MethodPost {
					return map[string]any{"sequence": 0, "state": "creationPending"}, nil
				}
				return []int{0}, nil
			}
			if strings.HasSuffix(path, "/mitigation") {
				return []string{"198.51.100.10"}, nil
			}
			if strings.HasSuffix(path, "/mitigation/198.51.100.10") {
				return map[string]any{"permanent": false, "state": "ok"}, nil
			}
			if strings.HasSuffix(path, "/reverse") {
				return []string{"198.51.100.10"}, nil
			}
			if strings.HasSuffix(path, "/reverse/198.51.100.10") {
				return map[string]any{"ipReverse": "198.51.100.10", "reverse": "host.example.com."}, nil
			}
			return map[string]any{"ip": "198.51.100.10/32"}, nil
		case method == http.MethodPost && strings.HasPrefix(path, "/ip/") && strings.Contains(path, "/rule"):
			return map[string]any{"sequence": 1, "action": "permit"}, nil
		case method == http.MethodDelete && strings.HasPrefix(path, "/ip/"):
			return nil, nil
		case method == http.MethodPut && strings.HasPrefix(path, "/ip/"):
			return nil, nil
		case method == http.MethodPost && strings.HasPrefix(path, "/ip/") && strings.HasSuffix(path, "/mitigation"):
			return map[string]any{"ipOnMitigation": "198.51.100.10"}, nil
		case method == http.MethodGet && path == "/dedicated/server/"+srv+"/features/backupFTP":
			return map[string]any{"ftpBackupName": "ftpback-rbx.ovh.net", "quota": map[string]any{"value": 500, "unit": "GB"}}, nil
		case method == http.MethodPost && strings.HasPrefix(path, "/dedicated/server/"+srv+"/features/backupFTP"):
			return map[string]any{"taskId": 703}, nil
		case method == http.MethodDelete && strings.HasPrefix(path, "/dedicated/server/"+srv+"/features/backupFTP"):
			return map[string]any{"taskId": 704}, nil
		case method == http.MethodGet && path == "/dedicated/server/"+srv+"/features/backupFTP/access":
			return []string{"198.51.100.0/24"}, nil
		case method == http.MethodGet && strings.HasPrefix(path, "/dedicated/server/"+srv+"/features/backupFTP/access/"):
			return map[string]any{"ipBlock": "198.51.100.0/24", "ftp": true}, nil
		case method == http.MethodGet && path == "/dedicated/server/"+srv+"/features/backupFTP/authorizableBlocks":
			return []string{"198.51.100.0/24"}, nil
		case method == http.MethodGet && path == "/dedicated/server/"+srv+"/features/backupCloud":
			return map[string]any{"name": "bc-1"}, nil
		case method == http.MethodGet && path == "/dedicated/server/"+srv+"/backupCloudOfferDetails":
			return map[string]any{"offer": "basic"}, nil
		case method == http.MethodGet && path == "/dedicated/server/"+srv+"/secondaryDnsDomains":
			return []string{"example.org"}, nil
		case method == http.MethodGet && path == "/dedicated/server/"+srv+"/secondaryDnsDomains/example.org":
			return map[string]any{"domain": "example.org"}, nil
		case method == http.MethodPost && path == "/dedicated/server/"+srv+"/secondaryDnsDomains":
			return nil, nil
		case method == http.MethodDelete && strings.HasPrefix(path, "/dedicated/server/"+srv+"/secondaryDnsDomains/"):
			return nil, nil
		case method == http.MethodGet && path == "/dedicated/server/"+srv+"/virtualMac":
			return []string{"02:00:00:aa:bb:cc"}, nil
		case method == http.MethodGet && path == "/dedicated/server/"+srv+"/virtualMac/02:00:00:aa:bb:cc":
			return map[string]any{"macAddress": "02:00:00:aa:bb:cc"}, nil
		case method == http.MethodGet && path == "/dedicated/server/"+srv+"/virtualMac/02:00:00:aa:bb:cc/virtualAddress":
			return []string{"198.51.100.20"}, nil
		case method == http.MethodGet && path == "/dedicated/server/"+srv+"/virtualMac/02:00:00:aa:bb:cc/virtualAddress/198.51.100.20":
			return map[string]any{"ipAddress": "198.51.100.20", "virtualMachineName": "vm1"}, nil
		case method == http.MethodPost && path == "/dedicated/server/"+srv+"/virtualMac":
			return map[string]any{"taskId": 705}, nil
		case method == http.MethodGet && path == "/dedicated/server/"+srv+"/virtualNetworkInterface":
			return []string{"uuid-1"}, nil
		case method == http.MethodGet && path == "/dedicated/server/"+srv+"/virtualNetworkInterface/uuid-1":
			return map[string]any{"uuid": "uuid-1", "mode": "public", "enabled": true}, nil
		case method == http.MethodPost && strings.HasPrefix(path, "/dedicated/server/"+srv+"/virtualNetworkInterface/uuid-1/"):
			return map[string]any{"taskId": 706}, nil
		case method == http.MethodPost && strings.HasPrefix(path, "/dedicated/server/"+srv+"/ola/"):
			return map[string]any{"taskId": 707}, nil
		case method == http.MethodGet && path == "/dedicated/server/"+srv+"/vrack":
			return []string{"pn-1234"}, nil
		case method == http.MethodDelete && path == "/dedicated/server/"+srv+"/vrack/pn-1234":
			return map[string]any{"taskId": 708}, nil
		case method == http.MethodGet && strings.HasPrefix(path, "/dedicated/server/"+srv+"/orderable/"):
			return map[string]any{"orderable": true}, nil
		case method == http.MethodGet && path == "/dedicated/server/"+srv+"/option":
			return []string{"TRAFFIC_UNLIMITED"}, nil
		case method == http.MethodGet && path == "/dedicated/server/"+srv+"/option/TRAFFIC_UNLIMITED":
			return map[string]any{"option": "TRAFFIC_UNLIMITED", "state": "subscribed"}, nil
		case method == http.MethodGet && path == "/dedicated/server/"+srv+"/intervention":
			return []int64{10}, nil
		case method == http.MethodGet && path == "/dedicated/server/"+srv+"/intervention/10":
			return map[string]any{"interventionId": 10, "type": "hardware"}, nil
		case method == http.MethodGet && path == "/dedicated/server/"+srv+"/plannedIntervention":
			return []int64{20}, nil
		case method == http.MethodGet && path == "/dedicated/server/"+srv+"/plannedIntervention/20":
			return map[string]any{"interventionId": 20, "type": "maintenance"}, nil
		case method == http.MethodGet && path == "/dedicated/server/"+srv+"/ongoing":
			return []any{}, nil
		case method == http.MethodGet && strings.HasPrefix(path, "/dedicated/server/"+srv+"/license/"):
			return []string{"2022-datacenter"}, nil
		case method == http.MethodGet && path == "/dedicated/server/"+srv+"/spla":
			return []int64{30}, nil
		case method == http.MethodGet && path == "/dedicated/server/"+srv+"/spla/30":
			return map[string]any{"id": 30, "type": "os", "status": "used"}, nil
		case method == http.MethodPost && path == "/dedicated/server/"+srv+"/spla":
			return map[string]any{"id": 31}, nil
		case method == http.MethodGet && path == "/dedicated/server/"+srv+"/biosSettings":
			return map[string]any{"status": "ok"}, nil
		case method == http.MethodGet && path == "/dedicated/server/"+srv+"/biosSettings/sgx":
			return map[string]any{"status": "disabled"}, nil
		case method == http.MethodPost && path == "/dedicated/server/"+srv+"/support/replace/hardDiskDrive":
			if hardwarePendingErr {
				return nil, &ovh.APIError{Code: 400, Message: "Action pending, ticketId: 887766"}
			}
			return map[string]any{"taskId": 709}, nil
		case method == http.MethodPost && (strings.HasSuffix(path, "/changeContact") || strings.HasSuffix(path, "/terminate") || strings.HasSuffix(path, "/confirmTermination")):
			return map[string]any{"status": "ok"}, nil
		case method == http.MethodPost && strings.HasSuffix(path, "/reverse"):
			return nil, nil
		case method == http.MethodGet && strings.HasSuffix(path, "/ipCanBeMovedTo"):
			return []string{"ns2.ip.net"}, nil
		case method == http.MethodGet && strings.HasSuffix(path, "/ipCountryAvailable"):
			return []string{"FR", "DE"}, nil
		case method == http.MethodPost && strings.HasSuffix(path, "/ipMove"):
			return map[string]any{"taskId": 710}, nil
		}
		return nil, fmt.Errorf("unexpected server-control mock %s %s", method, path)
	}

	// GET routes sweep
	getPaths := []string{
		"/api/server-control/list",
		"/api/server-control/servers",
		"/api/server-control/" + srv,
		"/api/server-control/" + srv + "/serviceinfo",
		"/api/server-control/" + srv + "/hardware",
		"/api/server-control/" + srv + "/hardware-disk-info",
		"/api/server-control/" + srv + "/hardware-raid-profiles",
		"/api/server-control/" + srv + "/templates",
		"/api/server-control/" + srv + "/partition-schemes?templateName=proxmox9_64",
		"/api/server-control/" + srv + "/tasks",
		"/api/server-control/" + srv + "/tasks/701/available-timeslots?periodStart=2026-11-16T00:00:00Z&periodEnd=2026-11-23T00:00:00Z",
		"/api/server-control/" + srv + "/boot",
		"/api/server-control/" + srv + "/boot-mode",
		"/api/server-control/" + srv + "/monitoring",
		"/api/server-control/" + srv + "/ips",
		"/api/server-control/" + srv + "/ip-specs",
		"/api/server-control/" + srv + "/network-specs",
		"/api/server-control/" + srv + "/network-interfaces",
		"/api/server-control/" + srv + "/network-stats",
		"/api/server-control/" + srv + "/mrtg",
		"/api/server-control/" + srv + "/statistics",
		"/api/server-control/" + srv + "/reverse",
		"/api/server-control/" + srv + "/ip/can-be-moved-to",
		"/api/server-control/" + srv + "/ip/country-available",
		"/api/server-control/" + srv + "/burst",
		"/api/server-control/" + srv + "/firewall",
		"/api/server-control/" + srv + "/ip-security",
		"/api/server-control/" + srv + "/backup-ftp",
		"/api/server-control/" + srv + "/backup-ftp/access",
		"/api/server-control/" + srv + "/backup-ftp/authorizable-blocks",
		"/api/server-control/" + srv + "/backup-cloud",
		"/api/server-control/" + srv + "/backup-cloud/offer-details",
		"/api/server-control/" + srv + "/secondary-dns",
		"/api/server-control/" + srv + "/virtual-mac",
		"/api/server-control/" + srv + "/virtual-network-interface",
		"/api/server-control/" + srv + "/vrack",
		"/api/server-control/" + srv + "/orderable/bandwidth",
		"/api/server-control/" + srv + "/orderable/traffic",
		"/api/server-control/" + srv + "/orderable/ip",
		"/api/server-control/" + srv + "/options",
		"/api/server-control/" + srv + "/interventions",
		"/api/server-control/" + srv + "/interventions/10",
		"/api/server-control/" + srv + "/planned-interventions",
		"/api/server-control/" + srv + "/planned-interventions/20",
		"/api/server-control/" + srv + "/ongoing",
		"/api/server-control/" + srv + "/license/windows/compliant",
		"/api/server-control/" + srv + "/license/windows-sql/compliant",
		"/api/server-control/" + srv + "/spla",
		"/api/server-control/" + srv + "/bios-settings",
		"/api/server-control/" + srv + "/bios-settings/sgx",
	}
	for _, p := range getPaths {
		rec := doReq(t, env.router, http.MethodGet, p, nil, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s failed: status=%d body=%s", p, rec.Code, rec.Body.String())
		}
	}

	// Missing periodStart/periodEnd returns 400
	missingRec := doReq(t, env.router, http.MethodGet, "/api/server-control/"+srv+"/tasks/701/available-timeslots", nil, nil)
	if missingRec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 when periodStart/periodEnd missing, got %d", missingRec.Code)
	}

	// install/status: active progress + 404 -> hasInstallation:false
	rec := doReq(t, env.router, http.MethodGet, "/api/server-control/"+srv+"/install/status", nil, nil)
	if rec.Code != http.StatusOK || decodeMap(t, rec)["status"].(map[string]any)["progressPercentage"].(float64) != 50 {
		t.Fatalf("unexpected active install/status: %d %s", rec.Code, rec.Body.String())
	}
	installStatusNotFound = true
	rec = doReq(t, env.router, http.MethodGet, "/api/server-control/"+srv+"/install/status", nil, nil)
	if rec.Code != http.StatusOK || decodeMap(t, rec)["hasInstallation"] != false {
		t.Fatalf("expected 200 hasInstallation:false when no install active, got %d %s", rec.Code, rec.Body.String())
	}
	installStatusNotFound = false

	// Console endpoint checks public RemoteAddr sets ipToAllow, loopback omits it
	pubReq := httptest.NewRequest(http.MethodGet, "/api/server-control/"+srv+"/console", nil)
	pubReq.RemoteAddr = "1.1.1.1:4444"
	pubRec := httptest.NewRecorder()
	env.router.ServeHTTP(pubRec, pubReq)
	if pubRec.Code != http.StatusOK {
		t.Fatalf("GET console with public IP failed: %d %s", pubRec.Code, pubRec.Body.String())
	}
	var sawPublicAllow bool
	for _, c := range env.mockOVH.callsCopy() {
		if c.Method == http.MethodPost && strings.HasSuffix(c.Path, "/features/ipmi/access") {
			if bodyMap, ok := c.Body.(map[string]any); ok && bodyMap["ipToAllow"] == "1.1.1.1" {
				sawPublicAllow = true
			}
		}
	}
	if !sawPublicAllow {
		t.Fatalf("expected ipToAllow=1.1.1.1 for public client IP")
	}

	// Install OS with Proxmox 9 ZFS + SSH key lookup
	rec = doReq(t, env.router, http.MethodPost, "/api/server-control/"+srv+"/install", map[string]any{
		"templateName":   "proxmox9_64",
		"customHostname": "pve01.example.com",
		"sshKeyName":     "mykey",
		"useProxmox9Zfs": true,
		"zfsRaidLevel":   1,
	}, nil)
	if rec.Code != http.StatusOK || decodeMap(t, rec)["success"] != true {
		t.Fatalf("POST install failed: %d %s", rec.Code, rec.Body.String())
	}
	var sawInstallStorage bool
	for _, c := range env.mockOVH.callsCopy() {
		if c.Method == http.MethodPost && strings.HasSuffix(c.Path, "/reinstall") {
			m := c.Body.(map[string]any)
			if _, ok := m["storage"]; ok && m["operatingSystem"] == "proxmox9_64" {
				sawInstallStorage = true
			}
		}
	}
	if !sawInstallStorage {
		t.Fatalf("expected Proxmox 9 ZFS reinstall call to include storage spec")
	}

	// Mutating endpoints sweep
	mutations := []struct {
		method string
		path   string
		body   any
	}{
		{http.MethodPost, "/api/server-control/" + srv + "/reboot", nil},
		{http.MethodPut, "/api/server-control/" + srv + "/boot/1", nil},
		{http.MethodPut, "/api/server-control/" + srv + "/boot-mode", map[string]any{"bootId": 2}},
		{http.MethodPost, "/api/server-control/" + srv + "/tasks/701/schedule", map[string]any{"startDate": "2026-11-17T02:00:00Z", "endDate": "2026-11-17T04:00:00Z"}},
		{http.MethodPut, "/api/server-control/" + srv + "/monitoring", map[string]any{"monitoring": true}},
		{http.MethodPut, "/api/server-control/" + srv + "/burst", map[string]any{"status": "active"}},
		{http.MethodPost, "/api/server-control/" + srv + "/hardware/replace", map[string]any{"componentType": "hardDiskDrive"}},
		{http.MethodPost, "/api/server-control/" + srv + "/reverse", map[string]any{"ip": "198.51.100.10", "reverse": "node1.example.com."}},
		{http.MethodPost, "/api/server-control/" + srv + "/ip/move", map[string]any{"ip": "198.51.100.10/32", "to": "ns2.ip.net"}},
		{http.MethodPut, "/api/server-control/" + srv + "/firewall", map[string]any{"enabled": true}},
		{http.MethodPost, "/api/server-control/" + srv + "/ip-security/rules", map[string]any{"ip": "198.51.100.10", "action": "permit", "protocol": "tcp", "sequence": 2}},
		{http.MethodDelete, "/api/server-control/" + srv + "/ip-security/rules/2?ip=198.51.100.10", nil},
		{http.MethodPost, "/api/server-control/" + srv + "/ip-security/mitigation", map[string]any{"ip": "198.51.100.10", "permanent": true}},
		{http.MethodPost, "/api/server-control/" + srv + "/backup-ftp", nil},
		{http.MethodPost, "/api/server-control/" + srv + "/backup-ftp/access", map[string]any{"ipBlock": "198.51.100.0/24", "ftp": true}},
		{http.MethodDelete, "/api/server-control/" + srv + "/backup-ftp/access/198.51.100.0%2F24", nil},
		{http.MethodPost, "/api/server-control/" + srv + "/backup-ftp/password", nil},
		{http.MethodDelete, "/api/server-control/" + srv + "/backup-ftp", nil},
		{http.MethodPost, "/api/server-control/" + srv + "/secondary-dns", map[string]any{"domain": "example.net"}},
		{http.MethodDelete, "/api/server-control/" + srv + "/secondary-dns/example.net", nil},
		{http.MethodPost, "/api/server-control/" + srv + "/virtual-mac", map[string]any{"ipAddress": "198.51.100.20", "type": "ovh", "virtualMachineName": "vm1"}},
		{http.MethodPost, "/api/server-control/" + srv + "/virtual-network-interface/uuid-1/enable", nil},
		{http.MethodPost, "/api/server-control/" + srv + "/virtual-network-interface/uuid-1/disable", nil},
		{http.MethodPost, "/api/server-control/" + srv + "/ola/aggregation", map[string]any{"name": "ola0", "virtualNetworkInterfaces": []string{"uuid-1", "uuid-2"}}},
		{http.MethodPost, "/api/server-control/" + srv + "/ola/reset", map[string]any{"virtualNetworkInterface": "uuid-1"}},
		{http.MethodPost, "/api/server-control/" + srv + "/ola/group", map[string]any{"name": "ola0", "virtualNetworkInterfaces": []string{"uuid-1"}}},
		{http.MethodPost, "/api/server-control/" + srv + "/ola/ungroup", map[string]any{"virtualNetworkInterface": "uuid-1"}},
		{http.MethodDelete, "/api/server-control/" + srv + "/vrack/pn-1234", nil},
		{http.MethodPost, "/api/server-control/" + srv + "/spla", map[string]any{"serialNumber": "SN123", "type": "os"}},
		{http.MethodPost, "/api/server-control/" + srv + "/change-contact", map[string]any{"contactAdmin": "ab123-ovh"}},
		{http.MethodPost, "/api/server-control/" + srv + "/terminate", nil},
		{http.MethodPost, "/api/server-control/" + srv + "/confirm-termination", map[string]any{"token": "term-tok"}},
	}
	for _, tc := range mutations {
		rec := doReq(t, env.router, tc.method, tc.path, tc.body, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s %s failed: status=%d body=%s", tc.method, tc.path, rec.Code, rec.Body.String())
		}
	}

	// Pending hardware replacement ticket returns structured 400
	hardwarePendingErr = true
	rec = doReq(t, env.router, http.MethodPost, "/api/server-control/"+srv+"/hardware/replace", map[string]any{"componentType": "hardDiskDrive"}, nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for pending replacement, got %d", rec.Code)
	}
	pendMap := decodeMap(t, rec)
	if pendMap["isPending"] != true || pendMap["ticketId"] != "887766" {
		t.Fatalf("unexpected pending hardware replace payload: %+v", pendMap)
	}
}

func TestFleetRoutesProtectedByAuthAndTOTP(t *testing.T) {
	env := setupFleetTest(t, true)
	if _, err := env.store.UpsertAccount(context.Background(), store.OVHAccount{
		ID:          "gate-acc",
		Alias:       "Gate",
		Endpoint:    "ovh-eu",
		Zone:        "EU",
		AppKey:      "ak_gate_12345678",
		AppSecret:   "as_gate_12345678",
		ConsumerKey: "ck_gate_12345678",
		IsDefault:   true,
	}); err != nil {
		t.Fatalf("seed account: %v", err)
	}
	env.attachAccountClient("gate-acc")
	env.mockOVH.handler = func(method, path string, _ any) (any, error) {
		if path == "/me" {
			return map[string]any{"customerCode": "gate-acc", "nichandle": "gate-acc", "email": "gate@example.com"}, nil
		}
		return map[string]any{"taskId": 42}, nil
	}

	// Unauthenticated request -> 401
	rec := doReq(t, env.router, http.MethodGet, "/api/accounts", nil, nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without API key, got %d", rec.Code)
	}

	headers := map[string]string{"X-API-Key": testPanelKey}

	// Strict / sudo mutations without 2FA enabled -> 403 TOTP_SETUP_REQUIRED
	for _, tc := range []struct {
		path string
		body any
	}{
		{"/api/accounts", map[string]any{"id": "gate-acc", "alias": "Gate2"}},
		{"/api/server-control/ns1.ip.net/reboot", nil},
		{"/api/server-control/ns1.ip.net/install", map[string]any{"templateName": "debian12_64"}},
	} {
		rec = doReq(t, env.router, http.MethodPost, tc.path, tc.body, headers)
		if rec.Code != http.StatusForbidden || decodeMap(t, rec)["code"] != "TOTP_SETUP_REQUIRED" {
			t.Fatalf("expected 403 TOTP_SETUP_REQUIRED for %s without 2FA, got %d %s", tc.path, rec.Code, rec.Body.String())
		}
	}

	// Activate 2FA
	info, err := env.authSvc.BeginTOTPSetup(context.Background(), "Admin")
	if err != nil {
		t.Fatalf("BeginTOTPSetup: %v", err)
	}
	code, _, err := auth.ComputeCurrentTOTP(info.Secret, time.Now().UTC())
	if err != nil {
		t.Fatalf("ComputeCurrentTOTP: %v", err)
	}
	if _, err := env.authSvc.ActivateTOTPSetup(context.Background(), code); err != nil {
		t.Fatalf("ActivateTOTPSetup: %v", err)
	}

	// Without X-TOTP-Code -> both strict and sudo return TOTP_STEP_UP_REQUIRED with matching tier
	rec = doReq(t, env.router, http.MethodPost, "/api/accounts", map[string]any{"id": "gate-acc", "alias": "Gate2"}, headers)
	resMap := decodeMap(t, rec)
	if rec.Code != http.StatusForbidden || resMap["code"] != "TOTP_STEP_UP_REQUIRED" || resMap["tier"] != "strict" {
		t.Fatalf("expected 403 TOTP_STEP_UP_REQUIRED (strict) for /api/accounts without X-TOTP-Code, got %d %s", rec.Code, rec.Body.String())
	}
	rec = doReq(t, env.router, http.MethodPost, "/api/server-control/ns1.ip.net/reboot", nil, headers)
	resMap = decodeMap(t, rec)
	if rec.Code != http.StatusForbidden || resMap["code"] != "TOTP_STEP_UP_REQUIRED" || resMap["tier"] != "sudo" {
		t.Fatalf("expected 403 TOTP_STEP_UP_REQUIRED (sudo) for reboot without X-TOTP-Code, got %d %s", rec.Code, rec.Body.String())
	}

	// Reusing the activation code fails with TOTP_REPLAYED; advancing +31s and using a fresh code succeeds
	headers["X-TOTP-Code"] = code
	rec = doReq(t, env.router, http.MethodPost, "/api/server-control/ns1.ip.net/reboot", nil, headers)
	if rec.Code != http.StatusForbidden || decodeMap(t, rec)["code"] != "TOTP_REPLAYED" {
		t.Fatalf("expected 403 TOTP_REPLAYED on code reuse, got %d %s", rec.Code, rec.Body.String())
	}

	nextTime := time.Now().UTC().Add(31 * time.Second)
	env.authSvc.SetNowFunc(func() time.Time { return nextTime })
	code2, _, err := auth.ComputeCurrentTOTP(info.Secret, nextTime)
	if err != nil {
		t.Fatalf("ComputeCurrentTOTP next step: %v", err)
	}
	headers["X-TOTP-Code"] = code2
	rec = doReq(t, env.router, http.MethodPost, "/api/server-control/ns1.ip.net/reboot", nil, headers)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 with fresh X-TOTP-Code, got %d %s", rec.Code, rec.Body.String())
	}
}
