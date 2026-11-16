package mcp

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

	"ovh-nexus/server/internal/auth"
	"ovh-nexus/server/internal/config"
	"ovh-nexus/server/internal/logger"
	"ovh-nexus/server/internal/ovhclient"
	"ovh-nexus/server/internal/store"
	"ovh-nexus/server/internal/vault"
)

const testMCPPanelKey = "TestPanelSecretKey!123456"

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

type mockCatalog struct {
	mu      sync.Mutex
	entries []CatalogEntry
}

func (m *mockCatalog) QueryAvailability(_ context.Context, planFilter, dcFilter string, onlyAvailable, presaleOnly bool, offset, limit int) ([]CatalogEntry, int, error) {
	m.mu.Lock()
	base := m.entries
	m.mu.Unlock()
	if len(base) == 0 {
		base = []CatalogEntry{
			{
				PlanCode:               "24ska01",
				ModelName:              "KS-A (Kimsufi)",
				Range:                  "kimsufi",
				CPU:                    "Intel Xeon E3-1245v2",
				ListingState:           "listed",
				Orderable:              true,
				Generation:             2024,
				AvailableDatacenters:   []string{"gra", "rbx"},
				MatchingConfigurations: 2,
			},
		}
	}

	planLow := strings.ToLower(strings.TrimSpace(planFilter))
	dcLow := strings.ToLower(strings.TrimSpace(dcFilter))

	var matched []CatalogEntry
	for _, e := range base {
		state := e.ListingState
		if state == "" {
			state = "listed"
		}
		if presaleOnly && state != "presale" {
			continue
		}
		if planLow != "" && !strings.Contains(strings.ToLower(e.PlanCode), planLow) && !strings.Contains(strings.ToLower(e.ModelName), planLow) {
			continue
		}
		if dcLow != "" {
			hasDC := false
			for _, dc := range e.AvailableDatacenters {
				if strings.Contains(strings.ToLower(dc), dcLow) {
					hasDC = true
					break
				}
			}
			if !hasDC {
				continue
			}
		}
		if onlyAvailable && len(e.AvailableDatacenters) == 0 {
			continue
		}
		matched = append(matched, e)
	}

	total := len(matched)
	if offset < 0 {
		offset = 0
	}
	if offset > total {
		offset = total
	}
	if limit < 1 {
		limit = 50
	}
	end := min(offset+limit, total)
	return matched[offset:end], total, nil
}

func (m *mockCatalog) ResolveModelName(_ context.Context, planCode string) (string, error) {
	if planCode == "24ska01" {
		return "KS-A (Kimsufi)", nil
	}
	return planCode, nil
}

func (m *mockCatalog) GetSKUChanges(_ context.Context, _ int, _ string) (map[string]any, []map[string]any, error) {
	return map[string]any{"total": 1}, []map[string]any{
		{"planCode": "24ska01", "changeType": "ADDED", "timestamp": "2026-11-16T08:00:00Z"},
	}, nil
}

type mockProcurement struct {
	mu       sync.Mutex
	orderSeq int
	lastReqs []UnpaidOrderRequest
	failNext error
}

func (m *mockProcurement) QuoteOrder(_ context.Context, accountID, planCode, datacenter string, options []string) (*OrderQuoteResult, error) {
	return &OrderQuoteResult{
		Price: map[string]any{
			"totalWithTax": map[string]any{"text": "14.99 EUR", "value": 14.99},
		},
	}, nil
}

func (m *mockProcurement) CreateUnpaidOrder(_ context.Context, req UnpaidOrderRequest) (*UnpaidOrderResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lastReqs = append(m.lastReqs, req)
	if m.failNext != nil {
		err := m.failNext
		m.failNext = nil
		return nil, err
	}
	m.orderSeq++
	return &UnpaidOrderResult{
		OrderID: fmt.Sprintf("9900%d", m.orderSeq),
		Price: map[string]any{
			"totalWithTax": map[string]any{"text": "14.99 EUR", "value": 14.99},
		},
	}, nil
}

func (m *mockProcurement) reqsCopy() []UnpaidOrderRequest {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]UnpaidOrderRequest, len(m.lastReqs))
	copy(out, m.lastReqs)
	return out
}

type mockMonitor struct {
	mu          sync.Mutex
	bmAdded     []string
	bmRemoved   []string
	vpsAdded    []string
	vpsRemoved  []string
}

func (m *mockMonitor) AddStockSubscription(_ context.Context, accountID, planCode string, datacenters []string, serverName string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.bmAdded = append(m.bmAdded, accountID+":"+planCode)
	return nil
}

func (m *mockMonitor) RemoveStockSubscription(_ context.Context, accountID, planCode string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.bmRemoved = append(m.bmRemoved, accountID+":"+planCode)
	return nil
}

func (m *mockMonitor) AddVPSSubscription(_ context.Context, planCode string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.vpsAdded = append(m.vpsAdded, planCode)
	return nil
}

func (m *mockMonitor) RemoveVPSSubscription(_ context.Context, planCodeOrID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.vpsRemoved = append(m.vpsRemoved, planCodeOrID)
	return nil
}

type mockNotifier struct {
	ch chan MCPActionEvent
}

func (n *mockNotifier) EmitMCPAction(_ context.Context, evt MCPActionEvent) {
	select {
	case n.ch <- evt:
	default:
	}
}

type mcpTestEnv struct {
	cfg      *config.Config
	store    *store.Store
	ovhPool  *ovhclient.Pool
	authSvc  *auth.Service
	logHub   *logger.Hub
	logBuf   *syncBuffer
	svc      *Service
	router   chi.Router
	clients  map[string]*mockOVHClient
	catalog  *mockCatalog
	proc     *mockProcurement
	monitor  *mockMonitor
	notifier *mockNotifier
	mu       sync.Mutex
	now      time.Time
}

func setupMCPTest(t *testing.T) *mcpTestEnv {
	t.Helper()
	dir := t.TempDir()
	envMap := map[string]string{
		"API_SECRET_KEY":      testMCPPanelKey,
		"DATA_ENCRYPTION_KEY": "TestDataEncryptionKey!1234567890",
		"DATA_DIR":            dir,
	}
	cfg, err := config.LoadFromEnv(func(k string) (string, bool) {
		v, ok := envMap[k]
		return v, ok
	})
	if err != nil {
		t.Fatalf("LoadFromEnv: %v", err)
	}
	v, err := vault.New(cfg.DataEncryptionKey)
	if err != nil {
		t.Fatalf("vault.New: %v", err)
	}
	st, err := store.Open(cfg.DataDir, v)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
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
		t.Fatalf("auth.NewService: %v", err)
	}

	cat := &mockCatalog{}
	proc := &mockProcurement{}
	mon := &mockMonitor{}
	notifier := &mockNotifier{ch: make(chan MCPActionEvent, 32)}
	svc, err := NewService(Dependencies{
		Config:      cfg,
		Store:       st,
		OVHPool:     ovhPool,
		Auth:        authSvc,
		Log:         logHub,
		Catalog:     cat,
		Procurement: proc,
		Monitor:     mon,
		Notifier:    notifier,
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	env := &mcpTestEnv{
		cfg:      cfg,
		store:    st,
		ovhPool:  ovhPool,
		authSvc:  authSvc,
		logHub:   logHub,
		logBuf:   logBuf,
		svc:      svc,
		clients:  make(map[string]*mockOVHClient),
		catalog:  cat,
		proc:     proc,
		monitor:  mon,
		notifier: notifier,
		now:      time.Date(2026, 11, 16, 10, 0, 0, 0, time.UTC),
	}
	svc.SetNowFunc(func() time.Time {
		env.mu.Lock()
		defer env.mu.Unlock()
		return env.now
	})
	svc.clientForAccountFn = func(ctx context.Context, accountID string) (OVHClient, *store.OVHAccount, error) {
		acc, err := st.GetActiveAccount(ctx, accountID)
		if err != nil {
			return nil, nil, err
		}
		if c, ok := env.clients[acc.ID]; ok {
			return c, acc, nil
		}
		return nil, acc, fmt.Errorf("no mock OVH client for account %s", acc.ID)
	}

	r := chi.NewRouter()
	r.Route("/api", svc.MountRoutes)
	env.router = r
	return env
}

func (e *mcpTestEnv) advance(d time.Duration) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.now = e.now.Add(d)
}

func doHTTP(t *testing.T, h http.Handler, method, target string, body any, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	var rdr io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		rdr = bytes.NewReader(raw)
	}
	req := httptest.NewRequest(method, target, rdr)
	req.RemoteAddr = "198.51.100.77:5555"
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

func decodeJSONMap(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode JSON (%d %s): %v", rec.Code, rec.Body.String(), err)
	}
	return out
}

func mcpCall(t *testing.T, env *mcpTestEnv, token string, id any, method string, params any) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	payload := map[string]any{
		"jsonrpc": "2.0",
		"method":  method,
	}
	if id != nil {
		payload["id"] = id
	}
	if params != nil {
		payload["params"] = params
	}
	headers := map[string]string{}
	if token != "" {
		headers["Authorization"] = "Bearer " + token
	}
	rec := doHTTP(t, env.router, http.MethodPost, "/api/mcp", payload, headers)
	if rec.Code == http.StatusAccepted || len(bytes.TrimSpace(rec.Body.Bytes())) == 0 {
		return rec, nil
	}
	return rec, decodeJSONMap(t, rec)
}

func mcpCallTool(t *testing.T, env *mcpTestEnv, token, tool string, args map[string]any) (map[string]any, bool, string) {
	t.Helper()
	rec, rpc := mcpCall(t, env, token, 1, "tools/call", map[string]any{
		"name":      tool,
		"arguments": args,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("tools/call %s unexpected HTTP %d: %s", tool, rec.Code, rec.Body.String())
	}
	result := rpc["result"].(map[string]any)
	isErr, _ := result["isError"].(bool)
	content := result["content"].([]any)
	text := content[0].(map[string]any)["text"].(string)
	if isErr {
		return nil, true, text
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(text), &parsed); err != nil {
		t.Fatalf("tool %s returned non-JSON text: %v (%s)", tool, err, text)
	}
	return parsed, false, text
}

func enableTOTPForTest(t *testing.T, env *mcpTestEnv) {
	t.Helper()
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
}

func TestMCPGatewayLifecycleAuthAndRateLimiting(t *testing.T) {
	env := setupMCPTest(t)

	// Disabled by default -> 403
	rec, _ := mcpCall(t, env, "", 1, "ping", nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 when MCP disabled, got %d %s", rec.Code, rec.Body.String())
	}

	// Both /api/mcp/settings and /api/mcp/config return full schema
	for _, path := range []string{"/api/mcp/settings", "/api/mcp/config"} {
		sRec := doHTTP(t, env.router, http.MethodGet, path, nil, nil)
		if sRec.Code != http.StatusOK {
			t.Fatalf("GET %s failed: %d", path, sRec.Code)
		}
		data := decodeJSONMap(t, sRec)["data"].(map[string]any)
		if data["enabled"] != false || data["operationsLockedByTotp"] != true {
			t.Fatalf("unexpected default MCP settings on %s: %+v", path, data)
		}
		if tools, _ := data["toolCatalog"].([]any); len(tools) != 20 {
			t.Fatalf("expected 20 tools in toolCatalog, got %d", len(tools))
		}
	}

	// Enable gateway -> mints nxs_mcp_... Bearer token (never persisted plaintext on disk)
	rec = doHTTP(t, env.router, http.MethodPut, "/api/mcp/settings", map[string]any{
		"enabled": true,
		"scopes":  map[string]any{"read": true},
	}, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("enable MCP settings failed: %d %s", rec.Code, rec.Body.String())
	}
	data := decodeJSONMap(t, rec)["data"].(map[string]any)
	token1, _ := data["revealedToken"].(string)
	if !strings.HasPrefix(token1, "nxs_mcp_") {
		t.Fatalf("expected nxs_mcp_ token, got %q", token1)
	}
	storedCfg, err := env.store.LoadMCPConfig(context.Background())
	if err != nil {
		t.Fatalf("LoadMCPConfig: %v", err)
	}
	if storedCfg.TokenHash == "" || storedCfg.TokenHash == token1 || strings.Contains(storedCfg.MaskedToken, token1) {
		t.Fatalf("plaintext token must never be stored in mcp_config: %+v", storedCfg)
	}
	if !vault.VerifyTokenSHA256(token1, storedCfg.TokenHash) {
		t.Fatalf("stored TokenHash failed VerifyTokenSHA256")
	}
	if strings.Contains(env.logBuf.String(), token1) {
		t.Fatalf("plaintext MCP token leaked into logger output")
	}

	// Subsequent GET does not reveal raw token again
	sRec := doHTTP(t, env.router, http.MethodGet, "/api/mcp/settings", nil, nil)
	sData := decodeJSONMap(t, sRec)["data"].(map[string]any)
	if _, exists := sData["revealedToken"]; exists {
		t.Fatalf("GET /api/mcp/settings should not re-reveal revealedToken")
	}

	// Panel API_SECRET_KEY must be rejected by /api/mcp
	rec, _ = mcpCall(t, env, testMCPPanelKey, 1, "ping", nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("panel API_SECRET_KEY must be rejected with 401 on /api/mcp, got %d", rec.Code)
	}

	// Rotate token invalidates token1 immediately
	rec = doHTTP(t, env.router, http.MethodPost, "/api/mcp/token/rotate", nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("rotate token failed: %d %s", rec.Code, rec.Body.String())
	}
	token2 := decodeJSONMap(t, rec)["data"].(map[string]any)["revealedToken"].(string)
	if token2 == token1 || !strings.HasPrefix(token2, "nxs_mcp_") {
		t.Fatalf("expected distinct rotated token, got %q", token2)
	}
	rec, _ = mcpCall(t, env, token1, 1, "ping", nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("old token should fail after rotation, got %d", rec.Code)
	}
	rec, _ = mcpCall(t, env, token2, 1, "ping", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("rotated token failed: %d %s", rec.Code, rec.Body.String())
	}

	// Brute-force rate limiter: 5 failed Bearer attempts trigger 429 + Retry-After on 6th
	for i := 0; i < 5; i++ {
		badRec, _ := mcpCall(t, env, "nxs_mcp_invalid", i+1, "ping", nil)
		if badRec.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d expected 401, got %d", i+1, badRec.Code)
		}
	}
	lockedRec, _ := mcpCall(t, env, token2, 99, "ping", nil)
	if lockedRec.Code != http.StatusTooManyRequests || lockedRec.Header().Get("Retry-After") == "" {
		t.Fatalf("expected 429 with Retry-After after 5 failures, got %d header=%q", lockedRec.Code, lockedRec.Header().Get("Retry-After"))
	}

	// Advance clock past window -> lockout clears
	env.advance(301 * time.Second)
	okRec, _ := mcpCall(t, env, token2, 100, "ping", nil)
	if okRec.Code != http.StatusOK {
		t.Fatalf("expected lockout to clear after window expiry, got %d", okRec.Code)
	}

	// Revoke token clears hash and disables gateway
	rec = doHTTP(t, env.router, http.MethodPost, "/api/mcp/token/revoke", nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("revoke token failed: %d %s", rec.Code, rec.Body.String())
	}
	rec, _ = mcpCall(t, env, token2, 101, "ping", nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 after token revoke, got %d", rec.Code)
	}
}

func TestMCP2FAHardSecurityLock(t *testing.T) {
	env := setupMCPTest(t)

	// Enable gateway with read+procurement
	rec := doHTTP(t, env.router, http.MethodPut, "/api/mcp/settings", map[string]any{
		"enabled": true,
		"scopes":  map[string]any{"read": true, "procurement": true},
	}, nil)
	token := decodeJSONMap(t, rec)["data"].(map[string]any)["revealedToken"].(string)

	// Enabling L3 operations while panel 2FA is disabled MUST fail with 403 TOTP_SETUP_REQUIRED
	for _, endpoint := range []string{"/api/mcp/settings", "/api/mcp/config"} {
		deny := doHTTP(t, env.router, http.MethodPut, endpoint, map[string]any{
			"scopes": map[string]any{"operations": true},
		}, nil)
		if deny.Code != http.StatusForbidden || decodeJSONMap(t, deny)["code"] != "TOTP_SETUP_REQUIRED" {
			t.Fatalf("expected 403 TOTP_SETUP_REQUIRED on %s without 2FA, got %d %s", endpoint, deny.Code, deny.Body.String())
		}
	}

	// Even if SQLite mcp_config has operations=true forged in storage, runtime MUST lock it to false
	cfg, _ := env.store.LoadMCPConfig(context.Background())
	cfg.Scopes.Operations = true
	if err := env.store.SaveMCPConfig(context.Background(), cfg); err != nil {
		t.Fatalf("SaveMCPConfig: %v", err)
	}

	sRec := doHTTP(t, env.router, http.MethodGet, "/api/mcp/settings", nil, nil)
	sData := decodeJSONMap(t, sRec)["data"].(map[string]any)
	scopes := sData["scopes"].(map[string]any)
	if sData["operationsLockedByTotp"] != true || scopes["operations"] != false {
		t.Fatalf("runtime L3 lock failed: %+v", sData)
	}

	// tools/list hides L3 tools when TOTP is disabled (only 11 L1 + 5 L2 = 16 tools)
	_, listRPC := mcpCall(t, env, token, 1, "tools/list", nil)
	tools := listRPC["result"].(map[string]any)["tools"].([]any)
	if len(tools) != 16 {
		t.Fatalf("expected 16 tools (L1+L2 only) without 2FA, got %d", len(tools))
	}
	for _, item := range tools {
		name := item.(map[string]any)["name"].(string)
		if name == "baremetal_reboot" || name == "vps_reboot" || name == "baremetal_ipmi_reset" || name == "baremetal_firewall_update" {
			t.Fatalf("L3 tool %s leaked into tools/list while 2FA disabled", name)
		}
	}

	// Calling L3 tool fails with isError=true and does NOT invoke OVH client
	_, isErr, errText := mcpCallTool(t, env, token, "baremetal_reboot", map[string]any{"server": "bm-01"})
	if !isErr || !strings.Contains(errText, "Two-Factor Authentication") {
		t.Fatalf("expected 2FA lock error on baremetal_reboot, got isErr=%v text=%q", isErr, errText)
	}

	// Enable 2FA and activate L3 operations -> all 20 tools become exposed
	enableTOTPForTest(t, env)
	okUpd := doHTTP(t, env.router, http.MethodPut, "/api/mcp/settings", map[string]any{
		"scopes": map[string]any{"operations": true},
	}, nil)
	if okUpd.Code != http.StatusOK {
		t.Fatalf("expected 200 enabling operations after TOTP setup, got %d %s", okUpd.Code, okUpd.Body.String())
	}
	updData := decodeJSONMap(t, okUpd)["data"].(map[string]any)
	if updData["totpEnabled"] != true || updData["operationsLockedByTotp"] != false {
		t.Fatalf("expected operations unlocked after TOTP enabled: %+v", updData)
	}

	_, listRPC = mcpCall(t, env, token, 2, "tools/list", nil)
	tools = listRPC["result"].(map[string]any)["tools"].([]any)
	if len(tools) != 20 {
		t.Fatalf("expected all 20 tools after enabling 2FA+L3, got %d", len(tools))
	}
}

func TestMCPStreamableHTTPProtocolAndAudit(t *testing.T) {
	env := setupMCPTest(t)
	rec := doHTTP(t, env.router, http.MethodPut, "/api/mcp/settings", map[string]any{
		"enabled": true,
		"scopes":  map[string]any{"read": true},
	}, nil)
	token := decodeJSONMap(t, rec)["data"].(map[string]any)["revealedToken"].(string)
	headers := map[string]string{"Authorization": "Bearer " + token}

	// GET and DELETE return 405
	for _, m := range []string{http.MethodGet, http.MethodDelete} {
		r := doHTTP(t, env.router, m, "/api/mcp", nil, headers)
		if r.Code != http.StatusMethodNotAllowed {
			t.Fatalf("%s /api/mcp expected 405, got %d", m, r.Code)
		}
	}

	// Protocol version negotiation & serverInfo check
	initRec, initRPC := mcpCall(t, env, token, 1, "initialize", map[string]any{
		"protocolVersion": "2024-11-05",
	})
	if initRec.Code != http.StatusOK {
		t.Fatalf("initialize failed: %d", initRec.Code)
	}
	initRes := initRPC["result"].(map[string]any)
	if initRes["protocolVersion"] != "2024-11-05" {
		t.Fatalf("expected negotiated protocolVersion 2024-11-05, got %v", initRes["protocolVersion"])
	}
	srvInfo := initRes["serverInfo"].(map[string]any)
	if srvInfo["name"] != "ovh-nexus-mcp" || srvInfo["version"] != "1.3.1" {
		t.Fatalf("unexpected serverInfo: %+v", srvInfo)
	}

	// Notification without id -> 202 Accepted
	notifRec, _ := mcpCall(t, env, token, nil, "notifications/initialized", nil)
	if notifRec.Code != http.StatusAccepted {
		t.Fatalf("expected 202 Accepted for notification, got %d", notifRec.Code)
	}

	// Batch JSON-RPC request
	batchRec := doHTTP(t, env.router, http.MethodPost, "/api/mcp", []map[string]any{
		{"jsonrpc": "2.0", "id": 10, "method": "ping"},
		{"jsonrpc": "2.0", "id": 11, "method": "tools/call", "params": map[string]any{"name": "catalog_availability", "arguments": map[string]any{}}},
	}, headers)
	if batchRec.Code != http.StatusOK {
		t.Fatalf("batch request failed: %d %s", batchRec.Code, batchRec.Body.String())
	}
	var batchOut []map[string]any
	if err := json.Unmarshal(batchRec.Body.Bytes(), &batchOut); err != nil || len(batchOut) != 2 {
		t.Fatalf("expected 2 batch responses, got %s", batchRec.Body.String())
	}

	// Audit log persistence + GET / DELETE /api/mcp/audit
	auditRec := doHTTP(t, env.router, http.MethodGet, "/api/mcp/audit", nil, nil)
	if auditRec.Code != http.StatusOK {
		t.Fatalf("GET /api/mcp/audit failed: %d", auditRec.Code)
	}
	auditRows := decodeJSONMap(t, auditRec)["data"].([]any)
	if len(auditRows) == 0 {
		t.Fatalf("expected audit row recorded for catalog_availability")
	}

	delRec := doHTTP(t, env.router, http.MethodDelete, "/api/mcp/audit", nil, nil)
	if delRec.Code != http.StatusOK {
		t.Fatalf("DELETE /api/mcp/audit failed: %d", delRec.Code)
	}
	auditAfter, _ := env.store.ListMCPAudit(context.Background(), 10)
	if len(auditAfter) != 0 {
		t.Fatalf("expected empty audit log after DELETE, got %d", len(auditAfter))
	}
}

func TestMCPPrivacyShieldAndWhitelistIsolation(t *testing.T) {
	env := setupMCPTest(t)
	ctx := context.Background()

	// Seed 2 accounts, whitelist only acc-allowed
	if _, err := env.store.UpsertAccount(ctx, store.OVHAccount{
		ID:          "acc-allowed",
		Alias:       "Allowed Main",
		Endpoint:    "ovh-eu",
		Zone:        "IE",
		AppKey:      "ak_allowed_secret_1234",
		AppSecret:   "as_allowed_secret_5678",
		ConsumerKey: "ck_allowed_secret_9012",
		IsDefault:   true,
	}); err != nil {
		t.Fatalf("seed allowed account: %v", err)
	}
	if _, err := env.store.UpsertAccount(ctx, store.OVHAccount{
		ID:          "acc-hidden",
		Alias:       "Hidden Secret Account",
		Endpoint:    "ovh-ca",
		Zone:        "CA",
		AppKey:      "ak_hidden_secret_1234",
		AppSecret:   "as_hidden_secret_5678",
		ConsumerKey: "ck_hidden_secret_9012",
		IsDefault:   false,
	}); err != nil {
		t.Fatalf("seed hidden account: %v", err)
	}

	rec := doHTTP(t, env.router, http.MethodPut, "/api/mcp/settings", map[string]any{
		"enabled":           true,
		"scopes":            map[string]any{"read": true},
		"allowAllAccounts":  false,
		"allowedAccountIds": []string{"acc-allowed"},
	}, nil)
	token := decodeJSONMap(t, rec)["data"].(map[string]any)["revealedToken"].(string)

	const (
		realBM       = "ns555555.ip-198-51-100.eu"
		realVPS      = "vps-secret99.ovh.net"
		realIPv4     = "198.51.100.42"
		realIPv6     = "2001:db8:cafe::42"
		realNIC      = "xx9876-ovh"
		realEmail    = "private@owner.test"
		realPhone    = "+33612345678"
		realOrderPwd = "SuperSecretOrderPwd99"
		realPayURL   = "https://www.ovh.com/order/pay?id=1042001"
		realPDFURL   = "https://www.ovh.com/order/bill/1042001.pdf"
	)

	env.clients["acc-allowed"] = &mockOVHClient{
		handler: func(method, path string, reqBody any) (any, error) {
			switch path {
			case "/dedicated/server":
				return []string{realBM}, nil
			case "/dedicated/server/" + realBM:
				return map[string]any{
					"name":       realBM,
					"reverse":    "custom-edge-01.myinfra",
					"datacenter": "rbx",
					"state":      "ok",
					"ip":         realIPv4,
					"iam":        map[string]any{"displayName": "Edge Host"},
				}, nil
			case "/dedicated/server/" + realBM + "/ips":
				return []string{realIPv4 + "/32", realIPv6 + "/64"}, nil
			case "/dedicated/server/" + realBM + "/serviceInfos":
				return map[string]any{
					"expiration":   time.Now().UTC().Add(15 * 24 * time.Hour).Format("2006-01-02"),
					"status":       "ok",
					"contactAdmin": realNIC,
					"renew":        map[string]any{"automatic": true, "period": 1},
				}, nil
			case "/dedicated/server/" + realBM + "/specifications/hardware":
				return map[string]any{
					"processorName": "AMD EPYC",
					"description":   "Contact " + realEmail + " or " + realPhone,
				}, nil
			case "/dedicated/server/" + realBM + "/boot":
				return []int64{1}, nil
			case "/dedicated/server/" + realBM + "/task":
				return []int64{}, nil
			case "/ip/198.51.100.42%2F32/firewall/198.51.100.42":
				return map[string]any{"enabled": true, "state": "ok"}, nil
			case "/ip/198.51.100.42%2F32/firewall/198.51.100.42/rule":
				return []int{0}, nil
			case "/ip/198.51.100.42%2F32/firewall/198.51.100.42/rule/0":
				return map[string]any{"sequence": 0, "action": "permit", "protocol": "tcp", "destinationPort": 443, "source": "any"}, nil
			case "/ip/198.51.100.42%2F32/mitigation/198.51.100.42":
				return map[string]any{"permanent": false, "auto": true, "state": "ok"}, nil
			case "/vps":
				return []string{realVPS}, nil
			case "/vps/" + realVPS:
				return map[string]any{
					"name":        realVPS,
					"displayName": "app-node-1",
					"state":       "running",
					"zone":        "os-rbx",
					"vcore":       2,
					"memoryLimit": 4096,
					"model":       map[string]any{"offer": "vps-starter", "disk": 40},
				}, nil
			case "/vps/" + realVPS + "/ips":
				return []string{realIPv4}, nil
			case "/vps/" + realVPS + "/serviceInfos":
				return map[string]any{"expiration": time.Now().UTC().Add(10 * 24 * time.Hour).Format("2006-01-02"), "status": "ok"}, nil
			case "/me/order":
				return []int64{1042001}, nil
			case "/me/order/1042001":
				return map[string]any{
					"orderId":       1042001,
					"date":          "2026-11-16T08:00:00Z",
					"url":           realPayURL,
					"pdfUrl":        realPDFURL,
					"orderPassword": realOrderPwd,
					"priceWithTax":  map[string]any{"text": "19.99 EUR"},
				}, nil
			case "/me/order/1042001/status":
				return "unpaid", nil
			case "/me/order/1042001/followUp":
				return []map[string]any{{"step": "PAYMENT", "status": "DOING", "url": realPayURL}}, nil
			}
			return nil, fmt.Errorf("unexpected L1 OVH call %s %s", method, path)
		},
	}

	// Seed a queue task on hidden account to verify procurement_status isolates it
	_, _ = env.store.CreateQueueItem(ctx, store.QueueItem{ID: "q-hidden", AccountID: "acc-hidden", PlanCode: "secret-plan", Quantity: 1, Status: "running"})
	_, _ = env.store.CreateQueueItem(ctx, store.QueueItem{ID: "q-allowed", AccountID: "acc-allowed", PlanCode: "24ska01", Datacenters: []string{"gra"}, Quantity: 1, Status: "running"})

	// Sweep all 11 L1 tools and verify sensitive data never leaks
	forbiddenLeaks := []string{
		"acc-allowed", "acc-hidden", "Hidden Secret Account",
		"ak_allowed", "as_allowed", "ck_allowed", "ak_hidden",
		realBM, realVPS, realIPv4, realIPv6, realNIC, realEmail, realPhone,
		realOrderPwd, realPayURL, realPDFURL, "1042001", "q-hidden",
	}
	assertNoLeaks := func(tool string, rawText string) {
		for _, leak := range forbiddenLeaks {
			if strings.Contains(rawText, leak) {
				t.Fatalf("tool %s leaked sensitive string %q in response: %s", tool, leak, rawText)
			}
		}
	}

	res, isErr, raw := mcpCallTool(t, env, token, "profile_list", map[string]any{})
	if isErr || int(res["total"].(float64)) != 1 {
		t.Fatalf("profile_list failed: isErr=%v res=%v", isErr, res)
	}
	assertNoLeaks("profile_list", raw)

	res, isErr, raw = mcpCallTool(t, env, token, "catalog_availability", map[string]any{})
	if isErr || int(res["totalMatched"].(float64)) != 1 {
		t.Fatalf("catalog_availability failed: isErr=%v raw=%s", isErr, raw)
	}

	res, isErr, raw = mcpCallTool(t, env, token, "catalog_sku_changes", map[string]any{})
	if isErr {
		t.Fatalf("catalog_sku_changes failed: %s", raw)
	}

	res, isErr, raw = mcpCallTool(t, env, token, "baremetal_list", map[string]any{})
	if isErr || int(res["total"].(float64)) != 1 {
		t.Fatalf("baremetal_list failed: isErr=%v raw=%s", isErr, raw)
	}
	assertNoLeaks("baremetal_list", raw)
	bmAlias := res["servers"].([]any)[0].(map[string]any)["server"].(string)
	if !strings.HasPrefix(bmAlias, "bm-") {
		t.Fatalf("expected bm-xx alias, got %q", bmAlias)
	}
	if !strings.Contains(raw, "*.*.*.42") {
		t.Fatalf("expected masked IPv4 hint *.*.*.42, got %s", raw)
	}

	// Inbound alias resolution on baremetal_get & baremetal_firewall_get
	res, isErr, raw = mcpCallTool(t, env, token, "baremetal_get", map[string]any{"server": bmAlias})
	if isErr {
		t.Fatalf("baremetal_get failed: %s", raw)
	}
	assertNoLeaks("baremetal_get", raw)

	res, isErr, raw = mcpCallTool(t, env, token, "baremetal_firewall_get", map[string]any{"server": bmAlias})
	if isErr {
		t.Fatalf("baremetal_firewall_get failed: %s", raw)
	}
	assertNoLeaks("baremetal_firewall_get", raw)

	res, isErr, raw = mcpCallTool(t, env, token, "vps_list", map[string]any{})
	if isErr || int(res["total"].(float64)) != 1 {
		t.Fatalf("vps_list failed: %s", raw)
	}
	assertNoLeaks("vps_list", raw)
	if !strings.Contains(raw, "app-node-1") {
		t.Fatalf("expected safe custom displayName app-node-1 preserved, got %s", raw)
	}

	res, isErr, raw = mcpCallTool(t, env, token, "order_list", map[string]any{"limit": 5})
	if isErr || int(res["total"].(float64)) != 1 {
		t.Fatalf("order_list failed: %s", raw)
	}
	assertNoLeaks("order_list", raw)
	ordAlias := res["orders"].([]any)[0].(map[string]any)["order"].(string)

	res, isErr, raw = mcpCallTool(t, env, token, "order_follow_up", map[string]any{"order": ordAlias})
	if isErr {
		t.Fatalf("order_follow_up failed: %s", raw)
	}
	assertNoLeaks("order_follow_up", raw)

	res, isErr, raw = mcpCallTool(t, env, token, "service_renewals", map[string]any{"days_within": 30})
	if isErr || int(res["total"].(float64)) != 2 {
		t.Fatalf("service_renewals failed: %s", raw)
	}
	assertNoLeaks("service_renewals", raw)

	res, isErr, raw = mcpCallTool(t, env, token, "procurement_status", map[string]any{})
	if isErr {
		t.Fatalf("procurement_status failed: %s", raw)
	}
	assertNoLeaks("procurement_status", raw)

	// Verify aliases persisted across reload & /api/mcp/aliases GET/DELETE
	reloaded, err := NewPrivacyShield(env.store)
	if err != nil {
		t.Fatalf("NewPrivacyShield reload: %v", err)
	}
	if resolved, ok := reloaded.Resolve("baremetal", bmAlias); !ok || resolved != realBM {
		t.Fatalf("alias %s did not persist across PrivacyShield reload: %q %v", bmAlias, resolved, ok)
	}

	aliasRec := doHTTP(t, env.router, http.MethodGet, "/api/mcp/aliases", nil, nil)
	if aliasRec.Code != http.StatusOK || len(decodeJSONMap(t, aliasRec)["data"].([]any)) == 0 {
		t.Fatalf("GET /api/mcp/aliases failed: %d %s", aliasRec.Code, aliasRec.Body.String())
	}

	// ScrubText preserves non-IP colons like 12:34 while redacting valid IPv6 and order URLs
	scrubbed := env.svc.Shield().ScrubText("Window 12:34 at host 2001:db8:cafe::42 and mail admin@example.org order https://www.ovh.com/cgi-bin/order/displayOrder.cgi?orderId=123&orderPassword=secret")
	if !strings.Contains(scrubbed, "12:34") || strings.Contains(scrubbed, "2001:db8") || strings.Contains(scrubbed, "admin@example.org") || strings.Contains(scrubbed, "https://") || strings.Contains(scrubbed, "secret") {
		t.Fatalf("unexpected ScrubText result: %q", scrubbed)
	}

	delAliasRec := doHTTP(t, env.router, http.MethodDelete, "/api/mcp/aliases", nil, nil)
	if delAliasRec.Code != http.StatusOK {
		t.Fatalf("DELETE /api/mcp/aliases failed: %d", delAliasRec.Code)
	}
	if resolved, ok := env.svc.Shield().Resolve("baremetal", bmAlias); ok || resolved != "" {
		t.Fatalf("expected alias cleared after DELETE /api/mcp/aliases, got %q", resolved)
	}
}

func TestMCPProcurementRedlinesL2L3AndL4Blocking(t *testing.T) {
	env := setupMCPTest(t)
	ctx := context.Background()

	if _, err := env.store.UpsertAccount(ctx, store.OVHAccount{
		ID:          "acc-ops",
		Alias:       "Ops Main",
		Endpoint:    "ovh-eu",
		Zone:        "IE",
		AppKey:      "ak_ops_1234",
		AppSecret:   "as_ops_1234",
		ConsumerKey: "ck_ops_1234",
		IsDefault:   true,
	}); err != nil {
		t.Fatalf("seed account: %v", err)
	}
	if _, err := env.store.UpsertAccount(ctx, store.OVHAccount{
		ID:          "acc-other",
		Alias:       "Blocked Other",
		Endpoint:    "ovh-eu",
		Zone:        "IE",
		AppKey:      "ak_oth_1234",
		AppSecret:   "as_oth_1234",
		ConsumerKey: "ck_oth_1234",
	}); err != nil {
		t.Fatalf("seed other account: %v", err)
	}

	const srv = "ns777.ip-198-51-100.eu"
	const vps = "vps-ops.ovh.net"
	env.clients["acc-ops"] = &mockOVHClient{
		handler: func(method, path string, _ any) (any, error) {
			switch {
			case method == http.MethodGet && path == "/dedicated/server":
				return []string{srv}, nil
			case method == http.MethodGet && path == "/dedicated/server/"+srv:
				return map[string]any{"name": srv, "datacenter": "gra", "state": "ok", "ip": "198.51.100.99"}, nil
			case method == http.MethodGet && path == "/dedicated/server/"+srv+"/ips":
				return []string{"198.51.100.99/32"}, nil
			case method == http.MethodGet && path == "/dedicated/server/"+srv+"/serviceInfos":
				return map[string]any{"status": "ok"}, nil
			case method == http.MethodGet && path == "/dedicated/server/"+srv+"/specifications/hardware":
				return map[string]any{"processorName": "Xeon"}, nil
			case method == http.MethodGet && path == "/dedicated/server/"+srv+"/boot":
				return []int64{}, nil
			case method == http.MethodGet && path == "/dedicated/server/"+srv+"/task":
				return []int64{}, nil
			case method == http.MethodPost && path == "/dedicated/server/"+srv+"/reboot":
				return map[string]any{"taskId": 501, "status": "todo"}, nil
			case method == http.MethodPost && strings.HasPrefix(path, "/dedicated/server/"+srv+"/features/ipmi/"):
				return map[string]any{"taskId": 502}, nil
			case method == http.MethodGet && path == "/vps":
				return []string{vps}, nil
			case method == http.MethodGet && path == "/vps/"+vps:
				return map[string]any{"name": vps, "state": "running"}, nil
			case method == http.MethodGet && path == "/vps/"+vps+"/ips":
				return []string{"198.51.100.99"}, nil
			case method == http.MethodGet && path == "/vps/"+vps+"/serviceInfos":
				return map[string]any{"status": "ok"}, nil
			case method == http.MethodPost && path == "/vps/"+vps+"/reboot":
				return nil, nil
			case method == http.MethodGet && strings.Contains(path, "/mitigation/198.51.100.99"):
				return map[string]any{"permanent": false}, nil
			case method == http.MethodPut && strings.Contains(path, "/mitigation/198.51.100.99"):
				return nil, nil
			case method == http.MethodPost && strings.HasSuffix(path, "/rule"):
				return map[string]any{"sequence": 1, "action": "permit"}, nil
			case method == http.MethodDelete && strings.Contains(path, "/rule/1"):
				return nil, nil
			}
			return nil, fmt.Errorf("unexpected OVH call %s %s", method, path)
		},
	}

	enableTOTPForTest(t, env)
	rec := doHTTP(t, env.router, http.MethodPut, "/api/mcp/settings", map[string]any{
		"enabled":           true,
		"scopes":            map[string]any{"read": true, "procurement": true, "operations": true},
		"allowAllAccounts":  false,
		"allowedAccountIds": []string{"acc-ops"},
		"hourlyOrderLimit":  2,
		"notifyOnMutation":  true,
	}, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("settings setup failed: %d %s", rec.Code, rec.Body.String())
	}
	token := decodeJSONMap(t, rec)["data"].(map[string]any)["revealedToken"].(string)

	// Step 1: order_quote
	q1Res, isErr, raw := mcpCallTool(t, env, token, "order_quote", map[string]any{"plan_code": "24ska01", "datacenter": "gra"})
	if isErr || q1Res["autoPay"] != false || int(q1Res["quantity"].(float64)) != 1 {
		t.Fatalf("order_quote unexpected result: isErr=%v %s", isErr, raw)
	}
	<-env.notifier.ch
	quoteID1 := q1Res["quoteId"].(string)

	// Expired quote fails after +301s
	env.advance(301 * time.Second)
	_, isErr, errTxt := mcpCallTool(t, env, token, "order_create_unpaid", map[string]any{"quote_id": quoteID1})
	if !isErr || !strings.Contains(errTxt, "expired") {
		t.Fatalf("expected expired quote error, got isErr=%v text=%q", isErr, errTxt)
	}

	// Fresh quote -> unpaid order #1 enforces AutoPay=false, WaiveRetractationPeriod=false, Quantity=1
	q2Res, isErr, _ := mcpCallTool(t, env, token, "order_quote", map[string]any{"plan_code": "24ska01", "datacenter": "gra"})
	if isErr {
		t.Fatalf("fresh order_quote failed")
	}
	<-env.notifier.ch
	quoteID2 := q2Res["quoteId"].(string)
	o1Res, isErr, o1Raw := mcpCallTool(t, env, token, "order_create_unpaid", map[string]any{"quote_id": quoteID2})
	if isErr || o1Res["autoPay"] != false || int(o1Res["quantity"].(float64)) != 1 {
		t.Fatalf("order_create_unpaid failed: isErr=%v %s", isErr, o1Raw)
	}
	reqs := env.proc.reqsCopy()
	if len(reqs) != 1 || reqs[0].AutoPay || reqs[0].WaiveRetractationPeriod || reqs[0].Quantity != 1 {
		t.Fatalf("CRITICAL REDLINE VIOLATED in CreateUnpaidOrder request: %+v", reqs)
	}

	// Wait for async notification on order_create_unpaid
	select {
	case evt := <-env.notifier.ch:
		if evt.Tool != "order_create_unpaid" || evt.Scope != "procurement" {
			t.Fatalf("unexpected notification event: %+v", evt)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for async MCP notification")
	}

	// Reusing single-use quote_id2 fails
	_, isErr, _ = mcpCallTool(t, env, token, "order_create_unpaid", map[string]any{"quote_id": quoteID2})
	if !isErr {
		t.Fatalf("expected single-use quote replay to fail")
	}

	// Order #2 succeeds (cap=2)
	q3Res, _, _ := mcpCallTool(t, env, token, "order_quote", map[string]any{"plan_code": "24ska01", "datacenter": "gra"})
	<-env.notifier.ch
	_, isErr, o2Raw := mcpCallTool(t, env, token, "order_create_unpaid", map[string]any{"quote_id": q3Res["quoteId"].(string)})
	if isErr {
		t.Fatalf("order #2 should succeed: %s", o2Raw)
	}
	<-env.notifier.ch

	// Order #3 hits hourly cap=2 WITHOUT consuming quote_id4
	q4Res, _, _ := mcpCallTool(t, env, token, "order_quote", map[string]any{"plan_code": "24ska01", "datacenter": "gra"})
	<-env.notifier.ch
	quoteID4 := q4Res["quoteId"].(string)
	_, isErr, capErr := mcpCallTool(t, env, token, "order_create_unpaid", map[string]any{"quote_id": quoteID4})
	if !isErr || !strings.Contains(capErr, "Hourly MCP unpaid order limit") {
		t.Fatalf("expected hourly limit error, got isErr=%v text=%q", isErr, capErr)
	}

	// Advance clock +61 minutes -> hourly quota resets (hits 5m quote TTL on old quoteID4, then succeeds on fresh quoteID5)
	env.advance(61 * time.Minute)
	_, isErr, expAfterHour := mcpCallTool(t, env, token, "order_create_unpaid", map[string]any{"quote_id": quoteID4})
	if !isErr || !strings.Contains(expAfterHour, "expired") {
		t.Fatalf("expected quota reset and quote_id4 5m TTL expiry, got isErr=%v text=%q", isErr, expAfterHour)
	}
	q5Res, _, _ := mcpCallTool(t, env, token, "order_quote", map[string]any{"plan_code": "24ska01", "datacenter": "gra"})
	<-env.notifier.ch
	_, isErr, o3Raw := mcpCallTool(t, env, token, "order_create_unpaid", map[string]any{"quote_id": q5Res["quoteId"].(string)})
	if isErr {
		t.Fatalf("expected order to succeed after hourly window elapsed + fresh quote: %s", o3Raw)
	}
	<-env.notifier.ch

	// queue_add + queue_manage (hardcoded AutoPay=false, Quantity=1) + cross-account denial
	qAddRes, isErr, raw := mcpCallTool(t, env, token, "queue_add", map[string]any{
		"plan_code":   "24ska01",
		"datacenters": []string{"gra", "rbx"},
	})
	if isErr {
		t.Fatalf("queue_add failed: %s", raw)
	}
	<-env.notifier.ch
	taskID := qAddRes["taskId"].(string)
	storedTask, err := env.store.GetQueueItem(ctx, taskID)
	if err != nil || storedTask.AutoPay || storedTask.WaiveRetractationPeriod || storedTask.Quantity != 1 {
		t.Fatalf("stored queue task violated redlines: %+v err=%v", storedTask, err)
	}
	for _, action := range []string{"pause", "resume", "delete"} {
		_, isErr, mRaw := mcpCallTool(t, env, token, "queue_manage", map[string]any{"task_id": taskID, "action": action})
		if isErr {
			t.Fatalf("queue_manage %s failed: %s", action, mRaw)
		}
		<-env.notifier.ch
	}

	otherTask, _ := env.store.CreateQueueItem(ctx, store.QueueItem{ID: "q-other-acc", AccountID: "acc-other", PlanCode: "24ska01", Quantity: 1, Status: "running"})
	_, isErr, crossErr := mcpCallTool(t, env, token, "queue_manage", map[string]any{"task_id": otherTask.ID, "action": "delete"})
	if !isErr || !strings.Contains(crossErr, "outside the MCP whitelist") {
		t.Fatalf("expected cross-account queue_manage refusal, got isErr=%v text=%q", isErr, crossErr)
	}

	// stock_subscription_manage for baremetal and vps
	for _, tc := range []map[string]any{
		{"target": "baremetal", "action": "add", "plan_code": "24ska01"},
		{"target": "baremetal", "action": "remove", "plan_code": "24ska01"},
		{"target": "vps", "action": "add", "plan_code": "vps-le-1"},
		{"target": "vps", "action": "remove", "plan_code": "vps-le-1"},
	} {
		_, isErr, sRaw := mcpCallTool(t, env, token, "stock_subscription_manage", tc)
		if isErr {
			t.Fatalf("stock_subscription_manage %+v failed: %s", tc, sRaw)
		}
		<-env.notifier.ch
	}
	if len(env.monitor.bmAdded) != 1 || len(env.monitor.bmRemoved) != 1 || len(env.monitor.vpsAdded) != 1 || len(env.monitor.vpsRemoved) != 1 {
		t.Fatalf("expected MonitorClient calls recorded: %+v", env.monitor)
	}

	// Discover aliases via L1 before running L3 ops tools
	bmList, _, _ := mcpCallTool(t, env, token, "baremetal_list", map[string]any{})
	bmAlias := bmList["servers"].([]any)[0].(map[string]any)["server"].(string)
	vpsListRes, _, _ := mcpCallTool(t, env, token, "vps_list", map[string]any{})
	vpsAlias := vpsListRes["vps"].([]any)[0].(map[string]any)["vps"].(string)

	for _, tc := range []struct {
		tool string
		args map[string]any
	}{
		{"baremetal_reboot", map[string]any{"server": bmAlias}},
		{"vps_reboot", map[string]any{"vps": vpsAlias}},
		{"baremetal_ipmi_reset", map[string]any{"server": bmAlias}},
		{"baremetal_firewall_update", map[string]any{"server": bmAlias, "operation": "set_mitigation", "permanent_mitigation": true}},
		{"baremetal_firewall_update", map[string]any{"server": bmAlias, "operation": "add_rule", "sequence": 1, "rule_action": "permit", "protocol": "tcp", "destination_port": 443}},
		{"baremetal_firewall_update", map[string]any{"server": bmAlias, "operation": "delete_rule", "sequence": 1}},
	} {
		_, isErr, l3Raw := mcpCallTool(t, env, token, tc.tool, tc.args)
		if isErr {
			t.Fatalf("L3 tool %s failed: %s", tc.tool, l3Raw)
		}
		if strings.Contains(l3Raw, srv) || strings.Contains(l3Raw, vps) || strings.Contains(l3Raw, "https://") {
			t.Fatalf("L3 tool %s leaked sensitive string: %s", tc.tool, l3Raw)
		}
		<-env.notifier.ch
	}

	// L4 hardcoded redlines remain permanently blocked even with TOTP + all scopes enabled
	for _, blockedTool := range []string{
		"baremetal_reinstall",
		"baremetal_terminate",
		"order_pay",
		"ipmi_console",
		"account_credentials_update",
		"vps_terminate",
	} {
		_, isErr, msg := mcpCallTool(t, env, token, blockedTool, map[string]any{})
		if !isErr || !strings.Contains(msg, "permanently blocked") {
			t.Fatalf("L4 blocked tool %s should return policy denial, got isErr=%v msg=%q", blockedTool, isErr, msg)
		}
	}
}

func TestMCPCatalogAvailabilityPaginationPresaleAndProcurementSource(t *testing.T) {
	env := setupMCPTest(t)
	ctx := context.Background()

	if _, err := env.store.UpsertAccount(ctx, store.OVHAccount{
		ID:          "acc-v131",
		Alias:       "Main Account",
		Endpoint:    "ovh-eu",
		Zone:        "IE",
		AppKey:      "ak_131",
		AppSecret:   "as_131",
		ConsumerKey: "ck_131",
		IsDefault:   true,
	}); err != nil {
		t.Fatalf("UpsertAccount: %v", err)
	}

	rec := doHTTP(t, env.router, http.MethodPut, "/api/mcp/settings", map[string]any{
		"enabled": true,
		"scopes":  map[string]any{"read": true},
	}, nil)
	token := decodeJSONMap(t, rec)["data"].(map[string]any)["revealedToken"].(string)

	// 1. Verify catalog_availability inputSchema has offset and presale_only
	_, listRPC := mcpCall(t, env, token, 1, "tools/list", nil)
	tools := listRPC["result"].(map[string]any)["tools"].([]any)
	var availSchema map[string]any
	for _, rawTool := range tools {
		tm := rawTool.(map[string]any)
		if tm["name"] == "catalog_availability" {
			availSchema = tm["inputSchema"].(map[string]any)["properties"].(map[string]any)
			break
		}
	}
	if availSchema == nil || availSchema["offset"] == nil || availSchema["presale_only"] == nil {
		t.Fatalf("expected offset and presale_only in catalog_availability inputSchema: %+v", availSchema)
	}

	// 2. Seed mixed listed + presale entries and verify offset pagination + presale_only filtering
	env.catalog.mu.Lock()
	env.catalog.entries = []CatalogEntry{
		{
			PlanCode:               "24ska01",
			ModelName:              "KS-A",
			Range:                  "KS",
			CPU:                    "Intel Xeon E3-1245v2",
			ListingState:           "listed",
			Orderable:              true,
			Generation:             2024,
			AvailableDatacenters:   []string{"GRA (1H-high)"},
			MatchingConfigurations: 1,
		},
		{
			PlanCode:               "27ska01",
			ModelName:              "KS-A (2027)",
			Range:                  "KS",
			CPU:                    "AMD EPYC 4244P",
			ListingState:           "presale",
			Orderable:              false,
			Generation:             2027,
			AvailableDatacenters:   []string{},
			MatchingConfigurations: 1,
		},
		{
			PlanCode:               "27rise01",
			ModelName:              "RISE-1 (2027)",
			Range:                  "RISE",
			CPU:                    "AMD EPYC 4344P",
			ListingState:           "presale",
			Orderable:              false,
			Generation:             2027,
			AvailableDatacenters:   []string{"RBX (72H)"},
			MatchingConfigurations: 2,
		},
	}
	env.catalog.mu.Unlock()

	page1, isErr, raw1 := mcpCallTool(t, env, token, "catalog_availability", map[string]any{
		"offset": 0,
		"limit":  2,
	})
	if isErr {
		t.Fatalf("catalog_availability page1 failed: %s", raw1)
	}
	if int(page1["offset"].(float64)) != 0 ||
		int(page1["returned"].(float64)) != 2 ||
		int(page1["totalMatched"].(float64)) != 3 ||
		int(page1["nextOffset"].(float64)) != 2 {
		t.Fatalf("unexpected page1 pagination metadata: %+v", page1)
	}

	page2, isErr, raw2 := mcpCallTool(t, env, token, "catalog_availability", map[string]any{
		"offset": 2,
		"limit":  2,
	})
	if isErr {
		t.Fatalf("catalog_availability page2 failed: %s", raw2)
	}
	if int(page2["offset"].(float64)) != 2 ||
		int(page2["returned"].(float64)) != 1 ||
		int(page2["totalMatched"].(float64)) != 3 ||
		page2["nextOffset"] != nil {
		t.Fatalf("unexpected page2 pagination metadata (expected nextOffset=nil at end): %+v", page2)
	}

	// presale_only=true filters BEFORE offset and limit so totalMatched == 2
	presalePage, isErr, rawPresale := mcpCallTool(t, env, token, "catalog_availability", map[string]any{
		"presale_only": true,
		"offset":       0,
		"limit":        1,
	})
	if isErr {
		t.Fatalf("catalog_availability presale_only failed: %s", rawPresale)
	}
	if int(presalePage["totalMatched"].(float64)) != 2 ||
		int(presalePage["returned"].(float64)) != 1 ||
		int(presalePage["nextOffset"].(float64)) != 1 {
		t.Fatalf("expected presale_only totalMatched=2, returned=1, nextOffset=1, got %+v", presalePage)
	}
	presaleItem := presalePage["items"].([]any)[0].(map[string]any)
	if presaleItem["planCode"] != "27ska01" ||
		presaleItem["listingState"] != "presale" ||
		presaleItem["orderable"] != false ||
		int(presaleItem["generation"].(float64)) != 2027 {
		t.Fatalf("unexpected presale item metadata: %+v", presaleItem)
	}

	// 3. Verify procurement_status includes source and sourceRef on queue items
	_, _ = env.store.CreateQueueItem(ctx, store.QueueItem{
		ID:          "q-mon-linked",
		AccountID:   "acc-v131",
		PlanCode:    "27ska01",
		Datacenters: []string{"gra"},
		Quantity:    1,
		Status:      "running",
		Source:      "stock-monitor",
		SourceRef:   "27ska01",
	})
	procStatus, isErr, rawProc := mcpCallTool(t, env, token, "procurement_status", map[string]any{})
	if isErr {
		t.Fatalf("procurement_status failed: %s", rawProc)
	}
	qList := procStatus["queue"].([]any)
	if len(qList) != 1 {
		t.Fatalf("expected 1 queue item, got %d", len(qList))
	}
	qEntry := qList[0].(map[string]any)
	if qEntry["source"] != "stock-monitor" || qEntry["sourceRef"] != "27ska01" {
		t.Fatalf("expected source='stock-monitor' and sourceRef='27ska01' in procurement_status queue item, got %+v", qEntry)
	}
}
