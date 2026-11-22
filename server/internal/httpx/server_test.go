package httpx

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"ovh-nexus/server/internal/auth"
	"ovh-nexus/server/internal/config"
	"ovh-nexus/server/internal/logger"
	"ovh-nexus/server/internal/store"
)

const (
	testSecretKey     = "e2e-master-panel-secret-key-2026"
	testEncryptionKey = "e2e-vault-encryption-key-material-2026"
)

func newTestConfig(t *testing.T) *config.Config {
	t.Helper()
	dataDir := filepath.Join(t.TempDir(), "data")
	env := map[string]string{
		"API_SECRET_KEY":      testSecretKey,
		"DATA_ENCRYPTION_KEY": testEncryptionKey,
		"PORT":                "18080",
		"DATA_DIR":            dataDir,
		"TZ":                  "UTC",
		"LOG_LEVEL":           "INFO",
		"LOG_FORMAT":          "text",
		"CORS_ORIGINS":        "http://localhost:19999,http://127.0.0.1:19999",
	}
	cfg, err := config.LoadFromEnv(func(k string) (string, bool) {
		v, ok := env[k]
		return v, ok
	})
	if err != nil {
		t.Fatalf("config.LoadFromEnv: %v", err)
	}
	return cfg
}

func newTestApp(t *testing.T) (*Services, http.Handler, *config.Config) {
	t.Helper()
	cfg := newTestConfig(t)
	svcs, err := OpenAndWire(context.Background(), cfg)
	if err != nil {
		t.Fatalf("OpenAndWire: %v", err)
	}
	t.Cleanup(func() {
		svcs.SignalShutdown()
		if svcs.Monitor != nil {
			svcs.Monitor.StopStockMonitor()
			svcs.Monitor.StopVPSMonitor()
			svcs.Monitor.StopSKUWatcher()
			svcs.Monitor.StopLifecycleWatcher()
		}
		_ = svcs.Close()
	})
	return svcs, NewRouter(svcs), cfg
}

func newCookieClient(t *testing.T) *http.Client {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar.New: %v", err)
	}
	return &http.Client{
		Jar:     jar,
		Timeout: 10 * time.Second,
	}
}

func doHTTPJSON(
	t *testing.T,
	client *http.Client,
	method, targetURL string,
	body any,
	headers map[string]string,
) (int, http.Header, []byte) {
	t.Helper()
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("json.Marshal: %v", err)
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, targetURL, reader)
	if err != nil {
		t.Fatalf("http.NewRequest: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("%s %s failed: %v", method, targetURL, err)
	}
	defer resp.Body.Close()
	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response %s %s: %v", method, targetURL, err)
	}
	return resp.StatusCode, resp.Header, respBytes
}

func decodeMap(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("decode JSON map: %v\nbody=%s", err, string(raw))
	}
	return m
}

func TestHTTPServerHardeningHealthGzipCSPAndSPA(t *testing.T) {
	svcs, handler, cfg := newTestApp(t)

	// Verify encryption key was wiped right after Vault construction.
	if cfg.DataEncryptionKey != "" {
		t.Fatalf("expected cfg.DataEncryptionKey to be wiped, got non-empty string")
	}

	// Verify Slowloris & header hardening on NewHTTPServer.
	httpSrv := NewHTTPServer(cfg, handler)
	if httpSrv.Addr != ":18080" {
		t.Fatalf("expected Addr=:18080, got %q", httpSrv.Addr)
	}
	if httpSrv.ReadHeaderTimeout != 5*time.Second ||
		httpSrv.ReadTimeout != 15*time.Second ||
		httpSrv.WriteTimeout != 120*time.Second ||
		httpSrv.IdleTimeout != 60*time.Second ||
		httpSrv.MaxHeaderBytes != 1<<20 {
		t.Fatalf("unexpected http.Server timeouts/limits: %+v", httpSrv)
	}

	ts := httptest.NewServer(handler)
	defer ts.Close()
	client := newCookieClient(t)

	// 1. /api/health unauthenticated + gzip + security headers
	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/health", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	// Disable Go http.Transport transparent gunzip so we can explicitly verify gzip encoding.
	rawClient := &http.Client{
		Transport: &http.Transport{DisableCompression: true},
		Timeout:   5 * time.Second,
	}
	resp, err := rawClient.Do(req)
	if err != nil {
		t.Fatalf("GET /api/health: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 on /api/health, got %d", resp.StatusCode)
	}
	if got := resp.Header.Get("Content-Encoding"); got != "gzip" {
		t.Fatalf("expected gzip Content-Encoding on /api/health, got %q", got)
	}
	expectedHeaders := map[string]string{
		"X-Frame-Options":        "DENY",
		"X-Content-Type-Options": "nosniff",
		"Referrer-Policy":        "no-referrer",
		"Permissions-Policy":     "camera=(), microphone=(), geolocation=()",
		"Content-Security-Policy": "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; " +
			"img-src 'self' data:; font-src 'self' data:; " +
			"connect-src 'self' https://eu.api.ovh.com https://ca.api.ovh.com https://api.us.ovhcloud.com; " +
			"frame-ancestors 'none'; base-uri 'self'; form-action 'self'",
	}
	for k, want := range expectedHeaders {
		if got := resp.Header.Get(k); got != want {
			t.Fatalf("header %s = %q, want %q", k, got, want)
		}
	}

	gzReader, err := gzip.NewReader(resp.Body)
	if err != nil {
		t.Fatalf("gzip.NewReader: %v", err)
	}
	defer gzReader.Close()
	healthBytes, err := io.ReadAll(gzReader)
	if err != nil {
		t.Fatalf("read gunzipped /api/health: %v", err)
	}
	healthMap := decodeMap(t, healthBytes)
	if healthMap["status"] != "ok" || healthMap["version"] != "1.4.0" {
		t.Fatalf("unexpected health payload: %+v", healthMap)
	}

	// 2. Embedded WebUI SPA fallback + gzip + no-cache on index.html
	for _, spaPath := range []string{"/", "/settings", "/mcp"} {
		respSPA, err := rawClient.Get(ts.URL + spaPath)
		if err != nil {
			t.Fatalf("GET %s: %v", spaPath, err)
		}
		bodyBytes, _ := io.ReadAll(respSPA.Body)
		_ = respSPA.Body.Close()
		if respSPA.StatusCode != http.StatusOK || !strings.Contains(string(bodyBytes), "OVH Nexus") {
			t.Fatalf("expected SPA index.html on %s, got %d body=%s", spaPath, respSPA.StatusCode, string(bodyBytes))
		}
		if !strings.Contains(respSPA.Header.Get("Content-Type"), "text/html") {
			t.Fatalf("expected text/html on %s, got %q", spaPath, respSPA.Header.Get("Content-Type"))
		}
		if respSPA.Header.Get("X-Frame-Options") != "DENY" {
			t.Fatalf("missing security headers on SPA route %s", spaPath)
		}
	}

	// 3. Unauthenticated /api/* routes return 401 AUTH_REQUIRED (including unknown paths to prevent enumeration)
	code, _, body := doHTTPJSON(t, client, http.MethodGet, ts.URL+"/api/servers", nil, nil)
	if code != http.StatusUnauthorized || decodeMap(t, body)["code"] != "AUTH_REQUIRED" {
		t.Fatalf("expected 401 AUTH_REQUIRED on /api/servers, got %d body=%s", code, string(body))
	}
	code, _, _ = doHTTPJSON(t, client, http.MethodGet, ts.URL+"/api/auth/totp/status", nil, nil)
	if code != http.StatusUnauthorized {
		t.Fatalf("expected 401 on /api/auth/totp/status without cookie, got %d", code)
	}
	code, _, _ = doHTTPJSON(t, client, http.MethodGet, ts.URL+"/api/does-not-exist", nil, nil)
	if code != http.StatusUnauthorized {
		t.Fatalf("expected 401 on unknown /api/does-not-exist without cookie, got %d", code)
	}

	// 4. Login and verify forbidden legacy routes and unknown /api/* return 404 JSON
	code, _, _ = doHTTPJSON(t, client, http.MethodPost, ts.URL+"/api/auth/login", map[string]any{
		"username": "Nexus",
		"password": testSecretKey,
	}, nil)
	if code != http.StatusOK {
		t.Fatalf("expected 200 login, got %d", code)
	}

	for _, deadRoute := range []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api"},
		{http.MethodGet, "/api/does-not-exist"},
		{http.MethodPost, "/api/config-sniper/quick-order"},
		{http.MethodGet, "/api/config-sniper/status"},
		{http.MethodPost, "/api/telegram/webhook"},
		{http.MethodPost, "/api/telegram/set-webhook"},
	} {
		c, hdr, b := doHTTPJSON(t, client, deadRoute.method, ts.URL+deadRoute.path, map[string]any{}, nil)
		if c != http.StatusNotFound || !strings.Contains(hdr.Get("Content-Type"), "application/json") {
			t.Fatalf("expected 404 JSON on %s %s, got %d ct=%q body=%s", deadRoute.method, deadRoute.path, c, hdr.Get("Content-Type"), string(b))
		}
	}

	// Wrong HTTP method on /api/health returns JSON error (never HTML), and missing asset returns 404 (not SPA HTML).
	cWrong, hdrWrong, _ := doHTTPJSON(t, client, http.MethodPost, ts.URL+"/api/health", map[string]any{}, nil)
	if (cWrong != http.StatusMethodNotAllowed && cWrong != http.StatusNotFound) || !strings.Contains(hdrWrong.Get("Content-Type"), "application/json") {
		t.Fatalf("expected 404/405 JSON on POST /api/health, got %d ct=%q", cWrong, hdrWrong.Get("Content-Type"))
	}
	cAsset, _, bAsset := doHTTPJSON(t, client, http.MethodGet, ts.URL+"/assets/missing.js", nil, nil)
	if cAsset != http.StatusNotFound || strings.Contains(string(bAsset), "<html") {
		t.Fatalf("expected 404 non-HTML on /assets/missing.js, got %d body=%s", cAsset, string(bAsset))
	}

	// 5. Global 2 MB body cap
	oversized := bytes.Repeat([]byte("a"), int(MaxRequestBodyBytes)+128)
	reqBig, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/queue", bytes.NewReader(oversized))
	reqBig.Header.Set("Content-Type", "application/json")
	respBig, err := client.Do(reqBig)
	if err != nil {
		t.Fatalf("oversized request failed: %v", err)
	}
	_ = respBig.Body.Close()
	if respBig.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413 on >2MB body, got %d", respBig.StatusCode)
	}

	// 6. CORS preflight check for allowed origin
	reqCORS, _ := http.NewRequest(http.MethodOptions, ts.URL+"/api/servers", nil)
	reqCORS.Header.Set("Origin", "http://localhost:19999")
	reqCORS.Header.Set("Access-Control-Request-Method", "GET")
	respCORS, err := client.Do(reqCORS)
	if err != nil {
		t.Fatalf("CORS preflight failed: %v", err)
	}
	_ = respCORS.Body.Close()
	if respCORS.StatusCode != http.StatusNoContent || respCORS.Header.Get("Access-Control-Allow-Origin") != "http://localhost:19999" {
		t.Fatalf("unexpected CORS preflight response: status=%d allow-origin=%q", respCORS.StatusCode, respCORS.Header.Get("Access-Control-Allow-Origin"))
	}

	_ = svcs
}

func TestE2EAuth2FAStrictSudoRecoveryAndEncryptionAtRest(t *testing.T) {
	svcs, handler, cfg := newTestApp(t)

	var clockMu sync.Mutex
	curNow := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	getNow := func() time.Time {
		clockMu.Lock()
		defer clockMu.Unlock()
		return curNow
	}
	advanceClock := func(d time.Duration) {
		clockMu.Lock()
		defer clockMu.Unlock()
		curNow = curNow.Add(d)
	}
	svcs.Auth.SetNowFunc(getNow)

	ts := httptest.NewServer(handler)
	defer ts.Close()
	client := newCookieClient(t)

	// Track account deletion callback wiring.
	var deletedCh = make(chan string, 1)
	svcs.Fleet.SetOnAccountDeleted(func(accountID string) {
		deletedCh <- accountID
	})

	// 1. Login -> session cookie
	code, _, body := doHTTPJSON(t, client, http.MethodPost, ts.URL+"/api/auth/login", map[string]any{
		"username": "Nexus",
		"password": "wrong-password",
	}, nil)
	if code != http.StatusUnauthorized {
		t.Fatalf("expected 401 on wrong password, got %d", code)
	}

	code, _, body = doHTTPJSON(t, client, http.MethodPost, ts.URL+"/api/auth/login", map[string]any{
		"username": "Nexus",
		"password": testSecretKey,
	}, nil)
	if code != http.StatusOK || decodeMap(t, body)["authenticated"] != true {
		t.Fatalf("login failed: status=%d body=%s", code, string(body))
	}

	// High-risk strict route blocked prior to 2FA setup.
	code, _, body = doHTTPJSON(t, client, http.MethodPost, ts.URL+"/api/accounts", map[string]any{
		"id":          "acct-e2e-1",
		"appKey":      "ak-super-secret-value-111",
		"appSecret":   "as-super-secret-value-222",
		"consumerKey": "ck-super-secret-value-333",
	}, nil)
	if code != http.StatusForbidden || decodeMap(t, body)["code"] != "TOTP_SETUP_REQUIRED" {
		t.Fatalf("expected 403 TOTP_SETUP_REQUIRED before 2FA, got %d body=%s", code, string(body))
	}

	// 2. Full 2FA enrollment (/setup -> /activate -> /status)
	code, _, body = doHTTPJSON(t, client, http.MethodPost, ts.URL+"/api/auth/totp/setup", map[string]any{
		"accountName": "E2E-Admin",
	}, nil)
	if code != http.StatusOK {
		t.Fatalf("totp/setup failed: %d body=%s", code, string(body))
	}
	setupRes := decodeMap(t, body)
	totpSecret, _ := setupRes["secret"].(string)
	if totpSecret == "" || !strings.HasPrefix(fmt.Sprint(setupRes["otpauthUri"]), "otpauth://totp/") {
		t.Fatalf("invalid totp setup payload: %+v", setupRes)
	}

	otp1, _, err := auth.ComputeCurrentTOTP(totpSecret, getNow())
	if err != nil {
		t.Fatalf("ComputeCurrentTOTP: %v", err)
	}
	code, _, body = doHTTPJSON(t, client, http.MethodPost, ts.URL+"/api/auth/totp/activate", map[string]any{
		"code": otp1,
	}, nil)
	if code != http.StatusOK {
		t.Fatalf("totp/activate failed: %d body=%s", code, string(body))
	}
	actRes := decodeMap(t, body)
	if actRes["enabled"] != true || actRes["totpEnabled"] != true {
		t.Fatalf("expected enabled=true & totpEnabled=true, got %+v", actRes)
	}
	rawRecCodes, _ := actRes["recoveryCodes"].([]any)
	if len(rawRecCodes) != 8 {
		t.Fatalf("expected 8 recovery codes, got %d", len(rawRecCodes))
	}
	recoveryCode0 := fmt.Sprint(rawRecCodes[0])

	// Check both /api/auth/totp/status and /api/auth/status
	code, _, body = doHTTPJSON(t, client, http.MethodGet, ts.URL+"/api/auth/totp/status", nil, nil)
	if code != http.StatusOK {
		t.Fatalf("totp/status failed: %d", code)
	}
	totpStatus := decodeMap(t, body)
	if totpStatus["enabled"] != true || totpStatus["totpEnabled"] != true {
		t.Fatalf("expected totp/status enabled & totpEnabled true, got %+v", totpStatus)
	}
	code, _, body = doHTTPJSON(t, client, http.MethodGet, ts.URL+"/api/auth/status", nil, nil)
	if code != http.StatusOK || decodeMap(t, body)["totpEnabled"] != true {
		t.Fatalf("expected auth/status totpEnabled=true, got %d body=%s", code, string(body))
	}

	// 3. Create OVH account (strict route: even with active sudo from activate, must reject without fresh X-TOTP-Code).
	plainAK := "ak-plaintext-never-in-db-1111"
	plainAS := "as-plaintext-never-in-db-2222"
	plainCK := "ck-plaintext-never-in-db-3333"
	acctPayload := map[string]any{
		"id":          "acct-e2e-1",
		"alias":       "Primary EU",
		"endpoint":    "ovh-eu",
		"zone":        "IE",
		"appKey":      plainAK,
		"appSecret":   plainAS,
		"consumerKey": plainCK,
	}
	code, _, body = doHTTPJSON(t, client, http.MethodPost, ts.URL+"/api/accounts", acctPayload, nil)
	if code != http.StatusForbidden || decodeMap(t, body)["code"] != "TOTP_STEP_UP_REQUIRED" || decodeMap(t, body)["tier"] != "strict" {
		t.Fatalf("expected strict 403 TOTP_STEP_UP_REQUIRED, got %d body=%s", code, string(body))
	}

	advanceClock(35 * time.Second)
	otp2, _, _ := auth.ComputeCurrentTOTP(totpSecret, getNow())
	code, _, body = doHTTPJSON(t, client, http.MethodPost, ts.URL+"/api/accounts", acctPayload, map[string]string{
		"X-TOTP-Code": otp2,
	})
	if code != http.StatusOK {
		t.Fatalf("POST /api/accounts failed: %d body=%s", code, string(body))
	}

	// Save a Telegram notification channel to test channel secret encryption at rest.
	plainTGToken := "123456789:AABBCCDDEEFF_PLAIN_TG_SECRET_XYZ"
	code, _, body = doHTTPJSON(t, client, http.MethodPut, ts.URL+"/api/notifications", map[string]any{
		"locale": "en",
		"channels": []map[string]any{
			{
				"id":      "tg-e2e",
				"type":    "telegram",
				"name":    "Ops TG",
				"enabled": true,
				"events":  []string{"order.success"},
				"config": map[string]any{
					"token":  plainTGToken,
					"chatId": "10002000",
				},
			},
		},
	}, nil)
	if code != http.StatusOK {
		t.Fatalf("PUT /api/notifications failed: %d body=%s", code, string(body))
	}
	if strings.Contains(string(body), plainTGToken) {
		t.Fatalf("notification API response leaked plaintext telegram token!")
	}

	// 4. Verify encryption at rest in SQLite & raw nexus.db bytes.
	var encAK, encAS, encCK, encTOTP, encChannelCfg string
	if err := svcs.Store.DB().QueryRow(`SELECT app_key, app_secret, consumer_key FROM ovh_accounts WHERE id = ?`, "acct-e2e-1").
		Scan(&encAK, &encAS, &encCK); err != nil {
		t.Fatalf("query raw ovh_accounts: %v", err)
	}
	if err := svcs.Store.DB().QueryRow(`SELECT secret FROM totp_state WHERE id = 1`).Scan(&encTOTP); err != nil {
		t.Fatalf("query raw totp_state: %v", err)
	}
	if err := svcs.Store.DB().QueryRow(`SELECT config_json FROM notification_channels WHERE id = ?`, "tg-e2e").Scan(&encChannelCfg); err != nil {
		t.Fatalf("query raw notification_channels: %v", err)
	}

	for name, val := range map[string]string{
		"app_key":      encAK,
		"app_secret":   encAS,
		"consumer_key": encCK,
		"totp_secret":  encTOTP,
	} {
		if !strings.HasPrefix(val, "enc:v1:") {
			t.Fatalf("expected %s to start with enc:v1:, got %q", name, val)
		}
	}
	var rawCfgMap map[string]any
	if err := json.Unmarshal([]byte(encChannelCfg), &rawCfgMap); err != nil {
		t.Fatalf("unmarshal raw channel config: %v", err)
	}
	if tokStr, _ := rawCfgMap["token"].(string); !strings.HasPrefix(tokStr, "enc:v1:") {
		t.Fatalf("expected stored telegram token to start with enc:v1:, got %q", tokStr)
	}

	// Checkpoint WAL so raw nexus.db file on disk contains all committed pages.
	if _, err := svcs.Store.DB().Exec(`PRAGMA wal_checkpoint(TRUNCATE);`); err != nil {
		t.Fatalf("wal_checkpoint: %v", err)
	}
	dbBytes, err := os.ReadFile(filepath.Join(cfg.DataDir, "nexus.db"))
	if err != nil {
		t.Fatalf("read raw nexus.db: %v", err)
	}
	for _, secret := range []string{plainAK, plainAS, plainCK, totpSecret, plainTGToken, testSecretKey} {
		if bytes.Contains(dbBytes, []byte(secret)) {
			t.Fatalf("raw nexus.db contains plaintext secret %q!", secret)
		}
	}

	// 5. Strict vs Sudo behavior on /api/queue and DELETE /api/accounts/{id}.
	// Advance clock by 6 minutes so 5-minute sudo cache expires while 12h session cookie remains valid.
	advanceClock(6 * time.Minute)

	// POST /api/queue with auto_pay=false must pass WITHOUT 2FA step-up.
	code, _, body = doHTTPJSON(t, client, http.MethodPost, ts.URL+"/api/queue", map[string]any{
		"accountId":   "acct-e2e-1",
		"planCode":    "24ska01",
		"datacenters": []string{"gra"},
		"auto_pay":    false,
	}, nil)
	if code != http.StatusOK {
		t.Fatalf("expected 200 on POST /api/queue (auto_pay=false) without step-up, got %d body=%s", code, string(body))
	}

	// POST /api/queue with auto_pay=true must require sudo step-up after sudo window expired.
	code, _, body = doHTTPJSON(t, client, http.MethodPost, ts.URL+"/api/queue", map[string]any{
		"accountId":   "acct-e2e-1",
		"planCode":    "24ska01",
		"datacenters": []string{"rbx"},
		"auto_pay":    true,
	}, nil)
	if code != http.StatusForbidden || decodeMap(t, body)["code"] != "TOTP_STEP_UP_REQUIRED" || decodeMap(t, body)["tier"] != "sudo" {
		t.Fatalf("expected 403 TOTP_STEP_UP_REQUIRED (tier=sudo) on auto_pay=true, got %d body=%s", code, string(body))
	}

	// Provide fresh TOTP code for sudo -> succeeds and opens fresh 5m sudo window.
	advanceClock(35 * time.Second)
	otp3, _, _ := auth.ComputeCurrentTOTP(totpSecret, getNow())
	code, _, body = doHTTPJSON(t, client, http.MethodPost, ts.URL+"/api/queue", map[string]any{
		"accountId":   "acct-e2e-1",
		"planCode":    "24ska01",
		"datacenters": []string{"rbx"},
		"auto_pay":    true,
	}, map[string]string{
		"X-TOTP-Code": otp3,
	})
	if code != http.StatusOK {
		t.Fatalf("expected 200 on POST /api/queue (auto_pay=true) with X-TOTP-Code, got %d body=%s", code, string(body))
	}

	// Even while sudo is active, DELETE /api/accounts/{id} (strict) MUST require a fresh TOTP code.
	code, _, body = doHTTPJSON(t, client, http.MethodDelete, ts.URL+"/api/accounts/acct-e2e-1", nil, nil)
	if code != http.StatusForbidden || decodeMap(t, body)["code"] != "TOTP_STEP_UP_REQUIRED" || decodeMap(t, body)["tier"] != "strict" {
		t.Fatalf("expected strict 403 on DELETE /api/accounts/{id} without fresh X-TOTP-Code, got %d body=%s", code, string(body))
	}

	advanceClock(35 * time.Second)
	otp4, _, _ := auth.ComputeCurrentTOTP(totpSecret, getNow())
	code, _, body = doHTTPJSON(t, client, http.MethodDelete, ts.URL+"/api/accounts/acct-e2e-1", nil, map[string]string{
		"X-TOTP-Code": otp4,
	})
	if code != http.StatusOK {
		t.Fatalf("DELETE /api/accounts/acct-e2e-1 with fresh TOTP failed: %d body=%s", code, string(body))
	}
	select {
	case delID := <-deletedCh:
		if delID != "acct-e2e-1" {
			t.Fatalf("expected deleted callback for acct-e2e-1, got %q", delID)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("expected SetOnAccountDeleted callback to fire")
	}

	// 6. Logout and log back in using single-use recovery code.
	code, _, _ = doHTTPJSON(t, client, http.MethodPost, ts.URL+"/api/auth/logout", nil, nil)
	if code != http.StatusOK {
		t.Fatalf("logout failed: %d", code)
	}
	code, _, body = doHTTPJSON(t, client, http.MethodPost, ts.URL+"/api/auth/login", map[string]any{
		"username": "Nexus",
		"password": testSecretKey,
	}, nil)
	if code != http.StatusOK || decodeMap(t, body)["requireTotp"] != true {
		t.Fatalf("expected requireTotp=true when password-only login with 2FA active, got %d body=%s", code, string(body))
	}

	code, _, body = doHTTPJSON(t, client, http.MethodPost, ts.URL+"/api/auth/login", map[string]any{
		"username": "Nexus",
		"password": testSecretKey,
		"totpCode": recoveryCode0,
	}, nil)
	if code != http.StatusOK || decodeMap(t, body)["authenticated"] != true {
		t.Fatalf("recovery code login failed: %d body=%s", code, string(body))
	}

	code, _, body = doHTTPJSON(t, client, http.MethodGet, ts.URL+"/api/auth/totp/status", nil, nil)
	if code != http.StatusOK {
		t.Fatalf("totp/status after recovery failed: %d", code)
	}
	if rem := decodeMap(t, body)["recoveryCodesRemaining"]; rem != float64(7) {
		t.Fatalf("expected 7 remaining recovery codes, got %v", rem)
	}

	// Reusing the consumed recovery code must be rejected.
	_, _, _ = doHTTPJSON(t, client, http.MethodPost, ts.URL+"/api/auth/logout", nil, nil)
	code, _, _ = doHTTPJSON(t, client, http.MethodPost, ts.URL+"/api/auth/login", map[string]any{
		"username": "Nexus",
		"password": testSecretKey,
		"totpCode": recoveryCode0,
	}, nil)
	if code != http.StatusUnauthorized {
		t.Fatalf("expected reused recovery code to fail with 401, got %d", code)
	}
}

func TestE2ELogsSSEAndExportAndDeadlines(t *testing.T) {
	svcs, handler, _ := newTestApp(t)
	svcs.LogsHandler.SetHeartbeatInterval(40 * time.Millisecond)

	// Start real http.Server with aggressive WriteTimeout to verify SSE stream clears its write deadline.
	ts := httptest.NewUnstartedServer(handler)
	ts.Config.WriteTimeout = 120 * time.Millisecond
	ts.Start()
	defer ts.Close()

	client := newCookieClient(t)
	code, _, _ := doHTTPJSON(t, client, http.MethodPost, ts.URL+"/api/auth/login", map[string]any{
		"username": "Nexus",
		"password": testSecretKey,
	}, nil)
	if code != http.StatusOK {
		t.Fatalf("login failed: %d", code)
	}

	// Open SSE stream with Accept-Encoding: gzip to confirm gzip middleware skips SSE.
	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/logs/stream", nil)
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Accept-Encoding", "gzip")
	sseClient := &http.Client{
		Jar: client.Jar,
		Transport: &http.Transport{
			DisableCompression: true,
		},
	}
	resp, err := sseClient.Do(req)
	if err != nil {
		t.Fatalf("GET /api/logs/stream: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 on /api/logs/stream, got %d", resp.StatusCode)
	}
	if ce := resp.Header.Get("Content-Encoding"); ce != "" {
		t.Fatalf("SSE stream must skip gzip compression, got Content-Encoding=%q", ce)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "text/event-stream") {
		t.Fatalf("unexpected SSE Content-Type=%q", ct)
	}

	reader := bufio.NewReader(resp.Body)
	firstLine, err := reader.ReadString('\n')
	if err != nil || !strings.Contains(firstLine, ": connected") {
		t.Fatalf("expected ': connected' line, got %q err=%v", firstLine, err)
	}

	// Sleep past WriteTimeout (120ms) and verify keepalive & live events still stream seamlessly.
	time.Sleep(180 * time.Millisecond)

	svcs.Log.Info(logger.SourceServer, "Live SSE integration log with secret "+testSecretKey)

	deadline := time.Now().Add(3 * time.Second)
	var liveEntry logger.LogEntry
	foundEvent := false
	for time.Now().Before(deadline) {
		line, readErr := reader.ReadString('\n')
		if readErr != nil {
			t.Fatalf("SSE stream terminated unexpectedly (WriteTimeout leaked?): %v", readErr)
		}
		if strings.HasPrefix(line, "data: ") {
			payload := strings.TrimSpace(strings.TrimPrefix(line, "data: "))
			if err := json.Unmarshal([]byte(payload), &liveEntry); err == nil &&
				strings.Contains(liveEntry.Message, "Live SSE integration log") {
				foundEvent = true
				break
			}
		}
	}
	if !foundEvent {
		t.Fatalf("timed out waiting for live SSE log entry")
	}
	if strings.Contains(liveEntry.Message, testSecretKey) || !strings.Contains(liveEntry.Message, "[REDACTED]") {
		t.Fatalf("expected SSE log message to redact secret, got %q", liveEntry.Message)
	}

	// Query REST logs and export logs.
	code, _, logsBody := doHTTPJSON(t, client, http.MethodGet, ts.URL+"/api/logs?source=server", nil, nil)
	if code != http.StatusOK {
		t.Fatalf("GET /api/logs failed: %d", code)
	}
	var entries []logger.LogEntry
	if err := json.Unmarshal(logsBody, &entries); err != nil || len(entries) == 0 {
		t.Fatalf("expected persisted server logs, got len=%d err=%v", len(entries), err)
	}

	code, hdr, expJSONL := doHTTPJSON(t, client, http.MethodGet, ts.URL+"/api/logs/export?format=jsonl", nil, nil)
	if code != http.StatusOK || !strings.Contains(hdr.Get("Content-Disposition"), ".jsonl") || strings.Contains(string(expJSONL), testSecretKey) {
		t.Fatalf("unexpected jsonl export: status=%d disp=%q", code, hdr.Get("Content-Disposition"))
	}

	code, hdr, expLog := doHTTPJSON(t, client, http.MethodGet, ts.URL+"/api/logs/export?format=log", nil, nil)
	if code != http.StatusOK || !strings.Contains(hdr.Get("Content-Disposition"), ".log") || !strings.Contains(string(expLog), "Live SSE integration log") {
		t.Fatalf("unexpected text log export: status=%d disp=%q", code, hdr.Get("Content-Disposition"))
	}

	// SignalShutdown must unblock the open SSE stream cleanly.
	svcs.SignalShutdown()
	_, readErr := reader.ReadString('\n')
	if readErr == nil {
		// Drain remaining buffered line if any, then expect EOF.
		for range 5 {
			if _, readErr = reader.ReadString('\n'); readErr != nil {
				break
			}
		}
	}
	if readErr == nil {
		t.Fatalf("expected SSE stream to close after SignalShutdown")
	}
}

type rewriteHostTransport struct {
	target *url.URL
	base   http.RoundTripper
}

func (rt *rewriteHostTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	clone.URL.Scheme = rt.target.Scheme
	clone.URL.Host = rt.target.Host
	clone.Host = rt.target.Host
	return rt.base.RoundTrip(clone)
}

func TestE2EMCPGatewayAndDomainAdapters(t *testing.T) {
	svcs, handler, _ := newTestApp(t)

	var (
		mu              sync.Mutex
		checkoutPayload map[string]any
		cartItemQty     int
		rebootCalled    bool
		includeNewSKU   bool
	)

	mockOVH := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		p := strings.TrimPrefix(r.URL.Path, "/1.0")
		switch {
		case r.Method == http.MethodGet && p == "/auth/time":
			_ = json.NewEncoder(w).Encode(time.Now().Unix())
		case r.Method == http.MethodGet && p == "/dedicated/server/datacenter/availabilities":
			mu.Lock()
			withNew := includeNewSKU
			mu.Unlock()
			avails := []map[string]any{
				{
					"planCode": "24ska01",
					"server":   "Intel Xeon E3-1245v2",
					"memory":   "ram-32g-ecc-1333",
					"storage":  "softraid-2x2000sa",
					"datacenters": []map[string]any{
						{"datacenter": "gra", "availability": "1H-high"},
						{"datacenter": "rbx", "availability": "unavailable"},
					},
				},
				{
					"planCode": "27ska01",
					"server":   "AMD EPYC 4244P",
					"memory":   "ram-64g-ecc-5200",
					"storage":  "softraid-2x960nvme",
					"datacenters": []map[string]any{
						{"datacenter": "ca-east-tor", "availability": "unavailable"},
					},
				},
			}
			if withNew {
				avails = append(avails, map[string]any{
					"planCode": "27rise01",
					"server":   "AMD EPYC 4344P",
					"memory":   "ram-64g-ecc-5200",
					"storage":  "softraid-2x960nvme",
					"datacenters": []map[string]any{
						{"datacenter": "gra", "availability": "72H"},
					},
				})
			}
			_ = json.NewEncoder(w).Encode(avails)
		case r.Method == http.MethodGet && strings.HasPrefix(p, "/order/catalog/public/"):
			plans := []map[string]any{}
			if strings.HasSuffix(p, "/eco") || strings.HasSuffix(p, "/baremetalServers") {
				plans = []map[string]any{
					{
						"planCode":    "24ska01",
						"invoiceName": "KS-A | Intel Xeon E3-1245v2",
					},
				}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"locale":   map[string]any{"currencyCode": "EUR"},
				"plans":    plans,
				"products": []any{},
				"addons":   []any{},
			})
		case r.Method == http.MethodGet && p == "/dedicated/server":
			_ = json.NewEncoder(w).Encode([]string{"ns123456.ip-198-51-100.eu"})
		case r.Method == http.MethodGet && p == "/dedicated/server/ns123456.ip-198-51-100.eu":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"name":            "ns123456.ip-198-51-100.eu",
				"commercialRange": "kimsufi",
				"datacenter":      "gra",
				"state":           "ok",
				"os":              "debian12_64",
				"monitoring":      true,
				"ip":              "198.51.100.42",
				"iam":             map[string]any{"displayName": "prod-kimsufi-node"},
			})
		case r.Method == http.MethodGet && p == "/dedicated/server/ns123456.ip-198-51-100.eu/serviceInfos":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status":     "ok",
				"expiration": "2027-01-15T00:00:00Z",
				"renew":      map[string]any{"automatic": true},
			})
		case r.Method == http.MethodPost && p == "/dedicated/server/ns123456.ip-198-51-100.eu/reboot":
			mu.Lock()
			rebootCalled = true
			mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]any{"taskId": 9901})
		case r.Method == http.MethodPost && p == "/order/cart":
			_ = json.NewEncoder(w).Encode(map[string]any{"cartId": "cart-e2e-1"})
		case r.Method == http.MethodPost && p == "/order/cart/cart-e2e-1/assign":
			_ = json.NewEncoder(w).Encode(map[string]any{})
		case r.Method == http.MethodPost && (p == "/order/cart/cart-e2e-1/eco" || p == "/order/cart/cart-e2e-1/baremetalServers"):
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			mu.Lock()
			if q, ok := body["quantity"].(float64); ok {
				cartItemQty = int(q)
			}
			mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]any{"itemId": 101})
		case r.Method == http.MethodPost && strings.HasSuffix(p, "/configuration"):
			_ = json.NewEncoder(w).Encode(map[string]any{})
		case r.Method == http.MethodGet && p == "/order/cart/cart-e2e-1/summary":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"prices": map[string]any{
					"withTax":      map[string]any{"value": 14.99, "currencyCode": "EUR"},
					"withoutTax":   map[string]any{"value": 12.49, "currencyCode": "EUR"},
					"tax":          map[string]any{"value": 2.50, "currencyCode": "EUR"},
					"currencyCode": "EUR",
				},
			})
		case r.Method == http.MethodGet && p == "/order/cart/cart-e2e-1":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"items": []map[string]any{
					{
						"itemId":      101,
						"planCode":    "24ska01",
						"description": "KS-A",
					},
				},
			})
		case r.Method == http.MethodGet && p == "/order/cart/cart-e2e-1/checkout":
			_ = json.NewEncoder(w).Encode(map[string]any{})
		case r.Method == http.MethodPost && p == "/order/cart/cart-e2e-1/checkout":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			mu.Lock()
			checkoutPayload = body
			mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]any{
				"orderId": 778899,
				"url":     "https://www.ovh.com/order/bill/secret?orderPassword=TOPSECRET",
			})
		case r.Method == http.MethodDelete && strings.HasPrefix(p, "/order/cart/"):
			_ = json.NewEncoder(w).Encode(map[string]any{})
		default:
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]any{"message": "not mocked: " + r.Method + " " + p})
		}
	}))
	defer mockOVH.Close()

	mockURL, _ := url.Parse(mockOVH.URL)
	svcs.Catalog.SetPublicAPIBaseURL(mockOVH.URL + "/1.0")

	// Seed a mock Telegram channel to verify async mcp.action notification dispatch cleanly.
	tgHit := make(chan string, 10)
	mockTG := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		tgHit <- string(b)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":1}}`))
	}))
	defer mockTG.Close()
	svcs.Notify.SetTelegramBaseURL(mockTG.URL)

	// Seed an encrypted account in SQLite and point the cached *ovh.Client transport to mockOVH.
	ctx := context.Background()
	if _, err := svcs.Store.UpsertChannel(ctx, store.NotificationChannel{
		ID:      "tg-mcp-audit",
		Type:    "telegram",
		Name:    "MCP Audit TG",
		Enabled: true,
		Events:  []string{"mcp.action"},
		Config: map[string]any{
			"token":  "999999:MCP_TG_TEST_SECRET_KEY",
			"chatId": "55555",
		},
	}); err != nil {
		t.Fatalf("UpsertChannel: %v", err)
	}
	if _, err := svcs.Store.UpsertAccount(ctx, store.OVHAccount{
		ID:          "acct-mcp-1",
		Alias:       "MCP Test Profile",
		Endpoint:    "ovh-eu",
		Zone:        "IE",
		AppKey:      "ak-mcp-1",
		AppSecret:   "as-mcp-1",
		ConsumerKey: "ck-mcp-1",
		IsDefault:   true,
	}); err != nil {
		t.Fatalf("UpsertAccount: %v", err)
	}
	ovhClient, _, err := svcs.OVHPool.ClientForAccount(ctx, "acct-mcp-1")
	if err != nil {
		t.Fatalf("ClientForAccount: %v", err)
	}
	ovhClient.Client = &http.Client{
		Timeout:   5 * time.Second,
		Transport: &rewriteHostTransport{target: mockURL, base: http.DefaultTransport},
	}

	ts := httptest.NewServer(handler)
	defer ts.Close()
	client := newCookieClient(t)

	// 1. Login to WebUI
	code, _, _ := doHTTPJSON(t, client, http.MethodPost, ts.URL+"/api/auth/login", map[string]any{
		"username": "Nexus",
		"password": testSecretKey,
	}, nil)
	if code != http.StatusOK {
		t.Fatalf("login failed: %d", code)
	}

	// 2. Hard L3 / MCP governance 2FA lock: modifying MCP settings before 2FA is enabled must return 403 TOTP_SETUP_REQUIRED.
	code, _, body := doHTTPJSON(t, client, http.MethodPut, ts.URL+"/api/mcp/settings", map[string]any{
		"enabled": true,
		"scopes": map[string]any{
			"read":        true,
			"procurement": true,
			"operations":  true,
		},
	}, nil)
	if code != http.StatusForbidden || decodeMap(t, body)["code"] != "TOTP_SETUP_REQUIRED" {
		t.Fatalf("expected 403 TOTP_SETUP_REQUIRED for MCP governance without 2FA, got %d body=%s", code, string(body))
	}

	// Enroll 2FA so MCP Gateway can be configured and activated via the sudo-gated API.
	now := time.Date(2026, 11, 16, 14, 0, 0, 0, time.UTC)
	svcs.Auth.SetNowFunc(func() time.Time { return now })
	_, _, setupRaw := doHTTPJSON(t, client, http.MethodPost, ts.URL+"/api/auth/totp/setup", map[string]any{"accountName": "MCP-Admin"}, nil)
	totpSecret := fmt.Sprint(decodeMap(t, setupRaw)["secret"])
	otpAct, _, _ := auth.ComputeCurrentTOTP(totpSecret, now)
	code, _, _ = doHTTPJSON(t, client, http.MethodPost, ts.URL+"/api/auth/totp/activate", map[string]any{"code": otpAct}, nil)
	if code != http.StatusOK {
		t.Fatalf("activate 2FA failed: %d", code)
	}

	// 3. Enable MCP with L1+L2+L3 scopes while 2FA is active -> receive Bearer token.
	now = now.Add(35 * time.Second)
	otpEnable, _, _ := auth.ComputeCurrentTOTP(totpSecret, now)
	code, _, body = doHTTPJSON(t, client, http.MethodPut, ts.URL+"/api/mcp/settings", map[string]any{
		"enabled": true,
		"scopes": map[string]any{
			"read":        true,
			"procurement": true,
			"operations":  true,
		},
	}, map[string]string{"X-TOTP-Code": otpEnable})
	if code != http.StatusOK {
		t.Fatalf("PUT /api/mcp/settings failed: %d body=%s", code, string(body))
	}
	settingsData := decodeMap(t, body)["data"].(map[string]any)
	mcpToken, _ := settingsData["revealedToken"].(string)
	if !strings.HasPrefix(mcpToken, "nxs_mcp_") {
		t.Fatalf("expected nxs_mcp_... revealedToken, got %q", mcpToken)
	}
	if settingsData["operationsLockedByTotp"] != false {
		t.Fatalf("expected operationsLockedByTotp=false with 2FA enabled, got %+v", settingsData)
	}

	// Now disable 2FA (strict) to prove the runtime L3 2FA lock STILL locks operations even when stored config had operations=true.
	now = now.Add(35 * time.Second)
	otpDisable, _, _ := auth.ComputeCurrentTOTP(totpSecret, now)
	code, _, body = doHTTPJSON(t, client, http.MethodPost, ts.URL+"/api/auth/totp/disable", nil, map[string]string{"X-TOTP-Code": otpDisable})
	if code != http.StatusOK {
		t.Fatalf("POST /api/auth/totp/disable failed: %d body=%s", code, string(body))
	}

	code, _, body = doHTTPJSON(t, client, http.MethodGet, ts.URL+"/api/mcp/settings", nil, nil)
	if code != http.StatusOK {
		t.Fatalf("GET /api/mcp/settings failed: %d", code)
	}
	lockedSettings := decodeMap(t, body)["data"].(map[string]any)
	if lockedSettings["operationsLockedByTotp"] != true || lockedSettings["scopes"].(map[string]any)["operations"] != false {
		t.Fatalf("expected L3 operations runtime-locked when 2FA disabled, got %+v", lockedSettings)
	}

	// 4. /api/mcp rejects unauthenticated and cookie-only requests (Bearer token required).
	code, _, _ = doHTTPJSON(t, client, http.MethodPost, ts.URL+"/api/mcp", map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
	}, nil)
	if code != http.StatusUnauthorized {
		t.Fatalf("expected 401 on /api/mcp with session cookie only (no Bearer), got %d", code)
	}

	mcpHeaders := map[string]string{
		"Authorization": "Bearer " + mcpToken,
	}

	// 5. JSON-RPC 2.0 initialize -> serverInfo.version == "1.4.0"
	code, _, body = doHTTPJSON(t, client, http.MethodPost, ts.URL+"/api/mcp", map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]any{
			"protocolVersion": "2025-03-26",
		},
	}, mcpHeaders)
	if code != http.StatusOK {
		t.Fatalf("MCP initialize failed: %d body=%s", code, string(body))
	}
	initRes := decodeMap(t, body)["result"].(map[string]any)
	srvInfo := initRes["serverInfo"].(map[string]any)
	if srvInfo["name"] != "ovh-nexus-mcp" || srvInfo["version"] != "1.4.0" {
		t.Fatalf("unexpected MCP serverInfo: %+v", srvInfo)
	}

	// 6. tools/list -> 17 tools (12 L1 + 5 L2), 0 L3 tools when 2FA is off.
	code, _, body = doHTTPJSON(t, client, http.MethodPost, ts.URL+"/api/mcp", map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "tools/list",
	}, mcpHeaders)
	if code != http.StatusOK {
		t.Fatalf("MCP tools/list failed: %d", code)
	}
	tools := decodeMap(t, body)["result"].(map[string]any)["tools"].([]any)
	if len(tools) != 17 {
		t.Fatalf("expected 17 tools (L1+L2) with 2FA off, got %d", len(tools))
	}

	// 7. Calling L3 tool baremetal_reboot must be blocked while 2FA is disabled.
	code, _, body = doHTTPJSON(t, client, http.MethodPost, ts.URL+"/api/mcp", map[string]any{
		"jsonrpc": "2.0",
		"id":      3,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      "baremetal_reboot",
			"arguments": map[string]any{"server": "bm-01"},
		},
	}, mcpHeaders)
	if code != http.StatusOK {
		t.Fatalf("MCP tools/call status=%d", code)
	}
	callRes := decodeMap(t, body)["result"].(map[string]any)
	if callRes["isError"] != true {
		t.Fatalf("expected baremetal_reboot to be blocked with isError=true, got %+v", callRes)
	}

	// 8. L1 catalog_availability + baremetal_list (Privacy Shield redacts raw hostname/IP).
	code, _, body = doHTTPJSON(t, client, http.MethodPost, ts.URL+"/api/mcp", map[string]any{
		"jsonrpc": "2.0",
		"id":      4,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      "catalog_availability",
			"arguments": map[string]any{"plan_code": "24ska01", "only_available": true},
		},
	}, mcpHeaders)
	if code != http.StatusOK || !strings.Contains(string(body), "24ska01") {
		t.Fatalf("catalog_availability failed: %d body=%s", code, string(body))
	}
	availText := decodeMap(t, body)["result"].(map[string]any)["content"].([]any)[0].(map[string]any)["text"].(string)
	availJSON := decodeMap(t, []byte(availText))
	availItems := availJSON["items"].([]any)
	if len(availItems) != 1 {
		t.Fatalf("expected 1 item for 24ska01, got %d", len(availItems))
	}
	firstAvail := availItems[0].(map[string]any)
	if firstAvail["listingState"] != "listed" || firstAvail["orderable"] != true || int(firstAvail["generation"].(float64)) != 2024 {
		t.Fatalf("expected listed orderable 2024 item for 24ska01, got %+v", firstAvail)
	}

	// 8b. catalog_availability offset pagination + presale_only filtering via httpx catalogAdapter.
	code, _, body = doHTTPJSON(t, client, http.MethodPost, ts.URL+"/api/mcp", map[string]any{
		"jsonrpc": "2.0",
		"id":      41,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      "catalog_availability",
			"arguments": map[string]any{"offset": 0, "limit": 1},
		},
	}, mcpHeaders)
	if code != http.StatusOK {
		t.Fatalf("catalog_availability offset=0 failed: %d body=%s", code, string(body))
	}
	page1JSON := decodeMap(t, []byte(decodeMap(t, body)["result"].(map[string]any)["content"].([]any)[0].(map[string]any)["text"].(string)))
	if int(page1JSON["totalMatched"].(float64)) != 2 || int(page1JSON["returned"].(float64)) != 1 || int(page1JSON["nextOffset"].(float64)) != 1 {
		t.Fatalf("unexpected page1 catalog_availability pagination: %+v", page1JSON)
	}

	code, _, body = doHTTPJSON(t, client, http.MethodPost, ts.URL+"/api/mcp", map[string]any{
		"jsonrpc": "2.0",
		"id":      42,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      "catalog_availability",
			"arguments": map[string]any{"offset": 1, "limit": 1},
		},
	}, mcpHeaders)
	if code != http.StatusOK {
		t.Fatalf("catalog_availability offset=1 failed: %d body=%s", code, string(body))
	}
	page2JSON := decodeMap(t, []byte(decodeMap(t, body)["result"].(map[string]any)["content"].([]any)[0].(map[string]any)["text"].(string)))
	if int(page2JSON["totalMatched"].(float64)) != 2 || int(page2JSON["returned"].(float64)) != 1 || page2JSON["nextOffset"] != nil {
		t.Fatalf("unexpected page2 catalog_availability pagination: %+v", page2JSON)
	}

	code, _, body = doHTTPJSON(t, client, http.MethodPost, ts.URL+"/api/mcp", map[string]any{
		"jsonrpc": "2.0",
		"id":      43,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      "catalog_availability",
			"arguments": map[string]any{"presale_only": true},
		},
	}, mcpHeaders)
	if code != http.StatusOK {
		t.Fatalf("catalog_availability presale_only failed: %d body=%s", code, string(body))
	}
	presaleJSON := decodeMap(t, []byte(decodeMap(t, body)["result"].(map[string]any)["content"].([]any)[0].(map[string]any)["text"].(string)))
	if int(presaleJSON["totalMatched"].(float64)) != 1 || int(presaleJSON["returned"].(float64)) != 1 {
		t.Fatalf("expected 1 presale item in catalog_availability presale_only=true, got %+v", presaleJSON)
	}
	presaleEntry := presaleJSON["items"].([]any)[0].(map[string]any)
	if presaleEntry["planCode"] != "27ska01" || presaleEntry["listingState"] != "presale" || presaleEntry["orderable"] != false || int(presaleEntry["generation"].(float64)) != 2027 {
		t.Fatalf("unexpected presale catalog_availability entry: %+v", presaleEntry)
	}

	// 8c. catalog_sku_changes backed by monitor.Service.GetSKUStatusAndChanges via httpx monitorAdapter.
	if _, err := svcs.Monitor.CheckSKUOnce(ctx, true); err != nil {
		t.Fatalf("CheckSKUOnce baseline: %v", err)
	}
	mu.Lock()
	includeNewSKU = true
	mu.Unlock()
	if _, err := svcs.Monitor.CheckSKUOnce(ctx, true); err != nil {
		t.Fatalf("CheckSKUOnce diff: %v", err)
	}

	code, _, body = doHTTPJSON(t, client, http.MethodPost, ts.URL+"/api/mcp", map[string]any{
		"jsonrpc": "2.0",
		"id":      44,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      "catalog_sku_changes",
			"arguments": map[string]any{"limit": 10, "change_type": "ADDED"},
		},
	}, mcpHeaders)
	if code != http.StatusOK {
		t.Fatalf("catalog_sku_changes failed: %d body=%s", code, string(body))
	}
	skuChangesJSON := decodeMap(t, []byte(decodeMap(t, body)["result"].(map[string]any)["content"].([]any)[0].(map[string]any)["text"].(string)))
	skuStatus := skuChangesJSON["status"].(map[string]any)
	if skuStatus["initialized"] != true || int(skuStatus["totalSkus"].(float64)) != 3 || int(skuStatus["presaleCount"].(float64)) < 2 {
		t.Fatalf("unexpected catalog_sku_changes status: %+v", skuStatus)
	}
	skuChangesList := skuChangesJSON["changes"].([]any)
	if len(skuChangesList) != 1 {
		t.Fatalf("expected 1 added SKU change event from catalog_sku_changes, got %+v", skuChangesList)
	}
	skuCh := skuChangesList[0].(map[string]any)
	if skuCh["planCode"] != "27rise01" || skuCh["skuKey"] == "" || skuCh["modelName"] == "" || skuCh["listingState"] != "presale" || int(skuCh["generation"].(float64)) != 2027 || skuCh["detectedAt"] == "" {
		t.Fatalf("unexpected catalog_sku_changes item: %+v", skuCh)
	}

	code, _, body = doHTTPJSON(t, client, http.MethodPost, ts.URL+"/api/mcp", map[string]any{
		"jsonrpc": "2.0",
		"id":      5,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      "baremetal_list",
			"arguments": map[string]any{},
		},
	}, mcpHeaders)
	if code != http.StatusOK {
		t.Fatalf("baremetal_list failed: %d body=%s", code, string(body))
	}
	if strings.Contains(string(body), "ns123456.ip-198-51-100.eu") || strings.Contains(string(body), "198.51.100.42") {
		t.Fatalf("Privacy Shield leaked raw hostname or IP: %s", string(body))
	}
	if !strings.Contains(string(body), "bm-01") {
		t.Fatalf("expected obfuscated bm-01 alias in baremetal_list: %s", string(body))
	}

	// 9. L2 order_quote -> order_create_unpaid enforcing auto_pay=false & quantity=1 redlines.
	code, _, body = doHTTPJSON(t, client, http.MethodPost, ts.URL+"/api/mcp", map[string]any{
		"jsonrpc": "2.0",
		"id":      6,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "order_quote",
			"arguments": map[string]any{
				"plan_code":  "24ska01",
				"datacenter": "gra",
			},
		},
	}, mcpHeaders)
	if code != http.StatusOK {
		t.Fatalf("order_quote failed: %d body=%s", code, string(body))
	}
	quoteText := decodeMap(t, body)["result"].(map[string]any)["content"].([]any)[0].(map[string]any)["text"].(string)
	quoteJSON := decodeMap(t, []byte(quoteText))
	quoteID, _ := quoteJSON["quoteId"].(string)
	if quoteID == "" {
		t.Fatalf("expected quoteId from order_quote, got %s", quoteText)
	}

	code, _, body = doHTTPJSON(t, client, http.MethodPost, ts.URL+"/api/mcp", map[string]any{
		"jsonrpc": "2.0",
		"id":      7,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      "order_create_unpaid",
			"arguments": map[string]any{"quote_id": quoteID},
		},
	}, mcpHeaders)
	if code != http.StatusOK {
		t.Fatalf("order_create_unpaid failed: %d body=%s", code, string(body))
	}
	orderText := decodeMap(t, body)["result"].(map[string]any)["content"].([]any)[0].(map[string]any)["text"].(string)
	if strings.Contains(orderText, "orderPassword") || strings.Contains(orderText, "ovh.com/order/bill") {
		t.Fatalf("payment URL leaked to MCP client: %s", orderText)
	}
	orderRes := decodeMap(t, []byte(orderText))
	if orderRes["autoPay"] != false || orderRes["quantity"] != float64(1) {
		t.Fatalf("expected autoPay=false & quantity=1, got %+v", orderRes)
	}

	mu.Lock()
	cp := checkoutPayload
	qty := cartItemQty
	mu.Unlock()
	if cp["autoPayWithPreferredPaymentMethod"] != false || cp["waiveRetractationPeriod"] != false || qty != 1 {
		t.Fatalf("OVH checkout redline violated: qty=%d payload=%+v", qty, cp)
	}

	hist, err := svcs.Store.ListHistory(ctx, "")
	if err != nil || len(hist) != 1 || hist[0].AutoPaid {
		t.Fatalf("expected 1 unpaid history record in SQLite, got %+v err=%v", hist, err)
	}

	// 10. L2 queue_add must also hardcode auto_pay=false & quantity=1.
	code, _, body = doHTTPJSON(t, client, http.MethodPost, ts.URL+"/api/mcp", map[string]any{
		"jsonrpc": "2.0",
		"id":      8,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "queue_add",
			"arguments": map[string]any{
				"plan_code":   "24ska01",
				"datacenters": []string{"gra"},
			},
		},
	}, mcpHeaders)
	if code != http.StatusOK {
		t.Fatalf("queue_add failed: %d body=%s", code, string(body))
	}
	qItems, err := svcs.Store.ListQueue(ctx, "")
	if err != nil || len(qItems) != 1 || qItems[0].AutoPay || qItems[0].WaiveRetractationPeriod || qItems[0].Quantity != 1 {
		t.Fatalf("expected queue item with AutoPay=false, Waive=false, Quantity=1, got %+v err=%v", qItems, err)
	}

	// 10b. L2 stock_subscription_manage exercises monitorAdapter + auto-starts stock monitor safely.
	code, _, body = doHTTPJSON(t, client, http.MethodPost, ts.URL+"/api/mcp", map[string]any{
		"jsonrpc": "2.0",
		"id":      81,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "stock_subscription_manage",
			"arguments": map[string]any{
				"target":    "baremetal",
				"action":    "add",
				"plan_code": "24ska01",
			},
		},
	}, mcpHeaders)
	if code != http.StatusOK || decodeMap(t, body)["result"].(map[string]any)["isError"] != false {
		t.Fatalf("stock_subscription_manage add failed: %d body=%s", code, string(body))
	}
	subs, err := svcs.Store.ListStockSubs(ctx, "")
	if err != nil || len(subs) != 1 || subs[0].PlanCode != "24ska01" {
		t.Fatalf("expected 1 stock subscription for 24ska01, got %+v err=%v", subs, err)
	}
	svcs.Monitor.StopStockMonitor()

	// 11. L4 redline blocked tool.
	code, _, body = doHTTPJSON(t, client, http.MethodPost, ts.URL+"/api/mcp", map[string]any{
		"jsonrpc": "2.0",
		"id":      9,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      "service_terminate",
			"arguments": map[string]any{"server": "bm-01"},
		},
	}, mcpHeaders)
	if code != http.StatusOK || decodeMap(t, body)["result"].(map[string]any)["isError"] != true {
		t.Fatalf("expected L4 tool service_terminate to be blocked, got %d body=%s", code, string(body))
	}

	// 12. Re-enable 2FA -> unlock L3 operations -> verify 20 tools listed and baremetal_reboot works.
	now = now.Add(35 * time.Second)
	_, _, setupRaw2 := doHTTPJSON(t, client, http.MethodPost, ts.URL+"/api/auth/totp/setup", map[string]any{"accountName": "MCP-Admin-2"}, nil)
	secret2 := fmt.Sprint(decodeMap(t, setupRaw2)["secret"])
	otpAct2, _, _ := auth.ComputeCurrentTOTP(secret2, now)
	code, _, _ = doHTTPJSON(t, client, http.MethodPost, ts.URL+"/api/auth/totp/activate", map[string]any{"code": otpAct2}, nil)
	if code != http.StatusOK {
		t.Fatalf("re-activate 2FA failed: %d", code)
	}

	now = now.Add(35 * time.Second)
	otpSudo, _, _ := auth.ComputeCurrentTOTP(secret2, now)
	code, _, body = doHTTPJSON(t, client, http.MethodPut, ts.URL+"/api/mcp/settings", map[string]any{
		"scopes": map[string]any{
			"read":        true,
			"procurement": true,
			"operations":  true,
		},
	}, map[string]string{"X-TOTP-Code": otpSudo})
	if code != http.StatusOK {
		t.Fatalf("unlock L3 operations failed: %d body=%s", code, string(body))
	}

	code, _, body = doHTTPJSON(t, client, http.MethodPost, ts.URL+"/api/mcp", map[string]any{
		"jsonrpc": "2.0",
		"id":      10,
		"method":  "tools/list",
	}, mcpHeaders)
	tools = decodeMap(t, body)["result"].(map[string]any)["tools"].([]any)
	if len(tools) != 21 {
		t.Fatalf("expected all 21 tools after 2FA + L3 unlock, got %d", len(tools))
	}

	code, _, body = doHTTPJSON(t, client, http.MethodPost, ts.URL+"/api/mcp", map[string]any{
		"jsonrpc": "2.0",
		"id":      11,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      "baremetal_reboot",
			"arguments": map[string]any{"server": "bm-01"},
		},
	}, mcpHeaders)
	if code != http.StatusOK || decodeMap(t, body)["result"].(map[string]any)["isError"] != false {
		t.Fatalf("expected baremetal_reboot to succeed after L3 unlock, got %d body=%s", code, string(body))
	}
	mu.Lock()
	rb := rebootCalled
	mu.Unlock()
	if !rb {
		t.Fatalf("expected mock OVH /dedicated/server/.../reboot to be invoked")
	}

	// Confirm async mcp.action notifications fired via NotificationEmitter -> notify.Dispatcher.
	select {
	case msg := <-tgHit:
		if !strings.Contains(msg, "MCP") {
			t.Fatalf("expected MCP audit payload in Telegram notification, got %s", msg)
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("timed out waiting for async mcp.action Telegram dispatch")
	}
	// Drain any remaining async notification goroutines triggered during this test.
	deadline := time.Now().Add(300 * time.Millisecond)
	for time.Now().Before(deadline) {
		select {
		case <-tgHit:
		case <-time.After(40 * time.Millisecond):
			deadline = time.Time{}
		}
	}
}

func TestE2EV132AnnouncedModelsListedAtAndSKUChanges(t *testing.T) {
	svcs, handler, _ := newTestApp(t)
	ctx := context.Background()

	var (
		mu              sync.Mutex
		phase           = 1
		sysPlanCode     = "24sys022"
		le01InAvailFeed = false
	)

	mockOVH := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		p := strings.TrimPrefix(r.URL.Path, "/1.0")
		mu.Lock()
		curSYS := sysPlanCode
		le01Live := le01InAvailFeed
		curPhase := phase
		mu.Unlock()

		switch {
		case r.Method == http.MethodGet && p == "/dedicated/server/datacenter/availabilities":
			avails := []map[string]any{
				{
					"fqn":      curSYS + ".ram-32g-ecc-2133.softraid-2x512nvme",
					"planCode": curSYS,
					"server":   "Intel Xeon-E 2136",
					"memory":   "ram-32g-ecc-2133",
					"storage":  "softraid-2x512nvme",
					"datacenters": []map[string]any{
						{"datacenter": "gra", "availability": "1H-high"},
					},
				},
			}
			if le01Live {
				avails = append(avails, map[string]any{
					"fqn":      "27skle01.ram-32g-ecc-3200.softraid-2x960nvme",
					"planCode": "27skle01",
					"server":   "Intel Xeon E-2386G",
					"memory":   "ram-32g-ecc-3200",
					"storage":  "softraid-2x960nvme",
					"datacenters": []map[string]any{
						{"datacenter": "rbx", "availability": "72H"},
					},
				})
			}
			_ = json.NewEncoder(w).Encode(avails)

		case r.Method == http.MethodGet && strings.HasPrefix(p, "/order/catalog/public/"):
			plans := []map[string]any{}
			if strings.HasSuffix(p, "/eco") {
				plans = append(plans, map[string]any{
					"planCode":    curSYS,
					"invoiceName": "SYS-2 | Intel Xeon-E 2136",
					"pricings": []map[string]any{
						{"mode": "default", "capacities": []string{"renew"}, "price": 3999000000},
					},
					"addonFamilies": []map[string]any{
						{"name": "memory", "default": "ram-32g-ecc-2133", "addons": []string{"ram-32g-ecc-2133"}},
						{"name": "storage", "default": "softraid-2x512nvme", "addons": []string{"softraid-2x512nvme"}},
					},
				})
				for i := 1; i <= 14; i++ {
					code := fmt.Sprintf("27skle%02d", i)
					plans = append(plans, map[string]any{
						"planCode":    code,
						"invoiceName": fmt.Sprintf("KS-LE-%d | Intel Xeon E-2386G", i),
						"pricings": []map[string]any{
							{"mode": "default", "capacities": []string{"renew"}, "price": 2999000000},
						},
						"addonFamilies": []map[string]any{
							{"name": "memory", "default": "ram-32g-ecc-3200", "addons": []string{"ram-32g-ecc-3200"}},
							{"name": "storage", "default": "softraid-2x960nvme", "addons": []string{"softraid-2x960nvme"}},
						},
					})
				}
			}
			_ = curPhase
			_ = json.NewEncoder(w).Encode(map[string]any{
				"locale":   map[string]any{"currencyCode": "EUR"},
				"plans":    plans,
				"products": []any{},
				"addons":   []any{},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer mockOVH.Close()

	svcs.Catalog.SetPublicAPIBaseURL(mockOVH.URL + "/1.0")
	ts := httptest.NewServer(handler)
	defer ts.Close()
	client := newCookieClient(t)

	// Login + enable 2FA + enable MCP
	code, _, _ := doHTTPJSON(t, client, http.MethodPost, ts.URL+"/api/auth/login", map[string]any{"username": "Nexus", "password": testSecretKey}, nil)
	if code != http.StatusOK {
		t.Fatalf("login failed: %d", code)
	}
	now := time.Date(2026, 11, 17, 10, 0, 0, 0, time.UTC)
	svcs.Auth.SetNowFunc(func() time.Time { return now })
	_, _, setupRaw := doHTTPJSON(t, client, http.MethodPost, ts.URL+"/api/auth/totp/setup", map[string]any{"accountName": "Admin"}, nil)
	totpSecret := fmt.Sprint(decodeMap(t, setupRaw)["secret"])
	otpAct, _, _ := auth.ComputeCurrentTOTP(totpSecret, now)
	_, _, _ = doHTTPJSON(t, client, http.MethodPost, ts.URL+"/api/auth/totp/activate", map[string]any{"code": otpAct}, nil)

	now = now.Add(35 * time.Second)
	otpEnable, _, _ := auth.ComputeCurrentTOTP(totpSecret, now)
	code, _, body := doHTTPJSON(t, client, http.MethodPut, ts.URL+"/api/mcp/settings", map[string]any{
		"enabled": true,
		"scopes":  map[string]any{"read": true},
	}, map[string]string{"X-TOTP-Code": otpEnable})
	if code != http.StatusOK {
		t.Fatalf("enable MCP failed: %d body=%s", code, string(body))
	}
	mcpToken := decodeMap(t, body)["data"].(map[string]any)["revealedToken"].(string)
	mcpHeaders := map[string]string{"Authorization": "Bearer " + mcpToken}

	// Phase 1: Baseline SKU check (1 listed: 24sys022, 14 announced: 27skle01..27skle14)
	if _, err := svcs.Monitor.CheckSKUOnce(ctx, true); err != nil {
		t.Fatalf("CheckSKUOnce baseline: %v", err)
	}

	// Verify catalog_availability for "27skle" or only_announced=true returns all 14 announced Limited Edition models
	for _, args := range []map[string]any{
		{"plan_code": "27skle"},
		{"only_announced": true},
		{"listing_state": "announced"},
	} {
		code, _, body = doHTTPJSON(t, client, http.MethodPost, ts.URL+"/api/mcp", map[string]any{
			"jsonrpc": "2.0",
			"id":      100,
			"method":  "tools/call",
			"params": map[string]any{
				"name":      "catalog_availability",
				"arguments": args,
			},
		}, mcpHeaders)
		if code != http.StatusOK {
			t.Fatalf("catalog_availability (%+v) failed: %d body=%s", args, code, string(body))
		}
		resJSON := decodeMap(t, []byte(decodeMap(t, body)["result"].(map[string]any)["content"].([]any)[0].(map[string]any)["text"].(string)))
		if int(resJSON["totalMatched"].(float64)) != 14 || int(resJSON["returned"].(float64)) != 14 {
			t.Fatalf("expected 14 announced models for args %+v, got %+v", args, resJSON)
		}
		first := resJSON["items"].([]any)[0].(map[string]any)
		if first["listingState"] != "announced" ||
			first["orderable"] != false ||
			first["configKey"] != "ram-32g-ecc-3200+softraid-2x960nvme" ||
			first["isDefault"] != true ||
			first["estimatedMonthlyPrice"].(float64) != 29.99 ||
			first["currency"] != "EUR" {
			t.Fatalf("unexpected announced item fields for args %+v: %+v", args, first)
		}
	}

	// Phase 2: 24sys022 is replaced by 24sys02-v3 AND 27skle01 transitions from announced -> listed (saleStarted=true)
	mu.Lock()
	phase = 2
	sysPlanCode = "24sys02-v3"
	le01InAvailFeed = true
	mu.Unlock()
	_, _ = svcs.Catalog.GetDictionary(ctx, "ovh-eu", "IE", true)
	// First check marks 24sys022 missing (count=1) and detects 27skle01 saleStarted + 24sys02-v3; second check confirms removal/replacement if needed
	if _, err := svcs.Monitor.CheckSKUOnce(ctx, true); err != nil {
		t.Fatalf("CheckSKUOnce phase2 step1: %v", err)
	}

	// Verify GET /api/catalog/availability and GET /api/catalog/plans populate listedAt from SKU monitor!
	code, _, availRaw := doHTTPJSON(t, client, http.MethodGet, ts.URL+"/api/catalog/availability?planCode=27skle01", nil, nil)
	if code != http.StatusOK {
		t.Fatalf("GET /api/catalog/availability failed: %d body=%s", code, string(availRaw))
	}
	var availList []map[string]any
	if err := json.Unmarshal(availRaw, &availList); err != nil || len(availList) != 1 {
		t.Fatalf("expected 1 availability item for 27skle01, got %s", string(availRaw))
	}
	if strings.TrimSpace(fmt.Sprint(availList[0]["listedAt"])) == "" {
		t.Fatalf("expected listedAt populated from SKU monitor on GET /api/catalog/availability, got %+v", availList[0])
	}

	// Verify catalog_sku_changes exposes announcedSkus=13, listedSkus=2, saleStarted=true on 27skle01
	code, _, body = doHTTPJSON(t, client, http.MethodPost, ts.URL+"/api/mcp", map[string]any{
		"jsonrpc": "2.0",
		"id":      101,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      "catalog_sku_changes",
			"arguments": map[string]any{"limit": 20},
		},
	}, mcpHeaders)
	if code != http.StatusOK {
		t.Fatalf("catalog_sku_changes failed: %d body=%s", code, string(body))
	}
	skuRes := decodeMap(t, []byte(decodeMap(t, body)["result"].(map[string]any)["content"].([]any)[0].(map[string]any)["text"].(string)))
	if int(skuRes["announcedSkus"].(float64)) != 13 {
		t.Fatalf("expected announcedSkus=13 after 27skle01 went on sale, got %+v", skuRes)
	}
	foundSaleStarted := false
	for _, rawCh := range skuRes["recentChanges"].([]any) {
		ch := rawCh.(map[string]any)
		if ch["planCode"] == "27skle01" && ch["saleStarted"] == true {
			foundSaleStarted = true
		}
	}
	if !foundSaleStarted {
		t.Fatalf("expected recentChanges to include 27skle01 with saleStarted=true, got %+v", skuRes["recentChanges"])
	}
}

func TestSecurityHotfixCORSWildcardCSRFAndHSTS(t *testing.T) {
	// 1. CORS wildcard "*" MUST NOT reflect Origin and MUST NOT emit Access-Control-Allow-Credentials: true.
	hWildcard := CORSMiddleware([]string{"*"})(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	reqWild := httptest.NewRequest(http.MethodGet, "/api/servers", nil)
	reqWild.Header.Set("Origin", "https://evil.example")
	recWild := httptest.NewRecorder()
	hWildcard.ServeHTTP(recWild, reqWild)
	if got := recWild.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Fatalf("expected wildcard ACAO '*', got %q", got)
	}
	if creds := recWild.Header().Get("Access-Control-Allow-Credentials"); creds != "" {
		t.Fatalf("expected NO Access-Control-Allow-Credentials on wildcard CORS, got %q", creds)
	}

	// 2. HSTS emitted iff cookieSecure=true.
	recHSTSOff := httptest.NewRecorder()
	SecurityHeadersWithConfig(false)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })).
		ServeHTTP(recHSTSOff, httptest.NewRequest(http.MethodGet, "/health", nil))
	if recHSTSOff.Header().Get("Strict-Transport-Security") != "" {
		t.Fatalf("expected no HSTS when cookieSecure=false")
	}
	recHSTSOn := httptest.NewRecorder()
	SecurityHeadersWithConfig(true)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })).
		ServeHTTP(recHSTSOn, httptest.NewRequest(http.MethodGet, "/health", nil))
	if !strings.Contains(recHSTSOn.Header().Get("Strict-Transport-Security"), "max-age=31536000") {
		t.Fatalf("expected HSTS header when cookieSecure=true, got %q", recHSTSOn.Header().Get("Strict-Transport-Security"))
	}

	// 3. CSRF middleware blocks cross-origin cookie POST while allowing same-origin and Bearer MCP.
	hCSRF := CSRFProtectionMiddleware([]string{"https://trusted.example"})(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	reqEvilOrigin := httptest.NewRequest(http.MethodPost, "https://panel.example/api/queue", nil)
	reqEvilOrigin.AddCookie(&http.Cookie{Name: "ovh_session", Value: "tok"})
	reqEvilOrigin.Header.Set("Origin", "https://evil.example")
	recEvilOrigin := httptest.NewRecorder()
	hCSRF.ServeHTTP(recEvilOrigin, reqEvilOrigin)
	if recEvilOrigin.Code != http.StatusForbidden {
		t.Fatalf("expected 403 CSRF_ORIGIN_REJECTED for cross-origin cookie POST, got %d", recEvilOrigin.Code)
	}

	reqCrossSite := httptest.NewRequest(http.MethodPost, "https://panel.example/api/queue", nil)
	reqCrossSite.AddCookie(&http.Cookie{Name: "ovh_session", Value: "tok"})
	reqCrossSite.Header.Set("Sec-Fetch-Site", "cross-site")
	recCrossSite := httptest.NewRecorder()
	hCSRF.ServeHTTP(recCrossSite, reqCrossSite)
	if recCrossSite.Code != http.StatusForbidden {
		t.Fatalf("expected 403 CSRF_CROSS_SITE_REJECTED for cross-site Sec-Fetch-Site, got %d", recCrossSite.Code)
	}

	reqSameOrigin := httptest.NewRequest(http.MethodPost, "https://panel.example/api/queue", nil)
	reqSameOrigin.AddCookie(&http.Cookie{Name: "ovh_session", Value: "tok"})
	reqSameOrigin.Header.Set("Origin", "https://panel.example")
	recSameOrigin := httptest.NewRecorder()
	hCSRF.ServeHTTP(recSameOrigin, reqSameOrigin)
	if recSameOrigin.Code != http.StatusOK {
		t.Fatalf("expected 200 for same-origin cookie POST, got %d", recSameOrigin.Code)
	}

	reqMCPBearer := httptest.NewRequest(http.MethodPost, "https://panel.example/api/mcp", nil)
	reqMCPBearer.Header.Set("Origin", "https://any-mcp-client.example")
	reqMCPBearer.Header.Set("Authorization", "Bearer nxs_mcp_testtoken")
	recMCPBearer := httptest.NewRecorder()
	hCSRF.ServeHTTP(recMCPBearer, reqMCPBearer)
	if recMCPBearer.Code != http.StatusOK {
		t.Fatalf("expected Bearer MCP POST to bypass browser CSRF check, got %d", recMCPBearer.Code)
	}

	// 4. Forged Authorization/X-API-Key/X-Internal-Token headers MUST NOT bypass CSRF when ovh_session cookie is present.
	for _, hdr := range []struct {
		k string
		v string
	}{
		{"Authorization", "Bearer forged-token"},
		{"X-API-Key", "forged-key"},
		{"X-Internal-Token", "forged-internal"},
	} {
		reqForged := httptest.NewRequest(http.MethodPost, "https://panel.example/api/queue", nil)
		reqForged.AddCookie(&http.Cookie{Name: "ovh_session", Value: "tok"})
		reqForged.Header.Set("Origin", "https://evil.example")
		reqForged.Header.Set(hdr.k, hdr.v)
		recForged := httptest.NewRecorder()
		hCSRF.ServeHTTP(recForged, reqForged)
		if recForged.Code != http.StatusForbidden {
			t.Fatalf("expected 403 CSRF rejection for cookie POST with forged %s header, got %d", hdr.k, recForged.Code)
		}
	}

	// Non-cookie programmatic X-API-Key call still passes CSRF middleware cleanly.
	reqProgAPIKey := httptest.NewRequest(http.MethodPost, "https://panel.example/api/queue", nil)
	reqProgAPIKey.Header.Set("X-API-Key", "loopback-key")
	recProgAPIKey := httptest.NewRecorder()
	hCSRF.ServeHTTP(recProgAPIKey, reqProgAPIKey)
	if recProgAPIKey.Code != http.StatusOK {
		t.Fatalf("expected non-cookie X-API-Key POST to pass CSRF middleware, got %d", recProgAPIKey.Code)
	}
}
