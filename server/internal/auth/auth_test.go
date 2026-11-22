package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"ovh-nexus/server/internal/config"
	"ovh-nexus/server/internal/logger"
	"ovh-nexus/server/internal/store"
	"ovh-nexus/server/internal/vault"
)

func newTestAuthEnv(t *testing.T, trustedProxies string) (*Service, *store.Store, *config.Config, *atomic.Int64) {
	t.Helper()
	lookup := func(k string) (string, bool) {
		switch k {
		case "API_SECRET_KEY":
			return "auth-test-password-1234", true
		case "DATA_ENCRYPTION_KEY":
			return "auth-test-encryption-key-5678", true
		case "TRUSTED_PROXIES":
			return trustedProxies, trustedProxies != ""
		default:
			return "", false
		}
	}
	cfg, err := config.LoadFromEnv(lookup)
	if err != nil {
		t.Fatalf("config.LoadFromEnv: %v", err)
	}
	v, err := vault.New(cfg.DataEncryptionKey)
	if err != nil {
		t.Fatalf("vault.New: %v", err)
	}
	cfg.WipeEncryptionKey()
	st, err := store.Open(filepath.Join(t.TempDir(), "data"), v)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	var buf bytes.Buffer
	hub := logger.NewHub(st, logger.Options{Level: "INFO", Output: &buf})
	svc, err := NewService(context.Background(), cfg, st, hub)
	if err != nil {
		t.Fatalf("auth.NewService: %v", err)
	}

	var nowUnix atomic.Int64
	nowUnix.Store(1760000000)
	svc.SetNowFunc(func() time.Time {
		return time.Unix(nowUnix.Load(), 0).UTC()
	})
	return svc, st, cfg, &nowUnix
}

func TestRFC6238KnownVector(t *testing.T) {
	// RFC 6238 Appendix B SHA-1 test vector ("12345678901234567890" -> Base32 GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ)
	secret := "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ"
	code, step, err := ComputeCurrentTOTP(secret, time.Unix(59, 0).UTC())
	if err != nil {
		t.Fatalf("ComputeCurrentTOTP: %v", err)
	}
	if step != 1 || code != "287082" {
		t.Fatalf("RFC 6238 vector mismatch: step=%d code=%q (want step=1 code=287082)", step, code)
	}

	uri := BuildOTPAuthURI(secret, "Admin", "OVH Nexus")
	if !strings.HasPrefix(uri, "otpauth://totp/OVH%20Nexus:Admin?") || !strings.Contains(uri, "secret="+secret) {
		t.Fatalf("unexpected otpauth URI: %q", uri)
	}
}

func TestClassifyRouteTier(t *testing.T) {
	cases := []struct {
		method string
		path   string
		body   map[string]any
		want   string
	}{
		{"POST", "/api/auth/totp/disable", nil, "strict"},
		{"POST", "/api/auth/totp/recovery-codes/regenerate", nil, "strict"},
		{"POST", "/api/accounts", nil, "strict"},
		{"DELETE", "/api/accounts/acc-1", nil, "strict"},
		{"POST", "/api/settings", map[string]any{"clearCredentials": true}, "strict"},
		{"POST", "/api/settings", map[string]any{"appKey": "new-real-key"}, "strict"},
		{"POST", "/api/settings", map[string]any{"appKey": "abcd****wxyz", "endpoint": "ovh-eu"}, ""},
		{"POST", "/api/server-control/ns123.ip-1-2-3.eu/install", nil, "strict"},
		{"POST", "/api/server-control/ns123/terminate", nil, "strict"},
		{"POST", "/api/server-control/ns123/reboot", nil, "sudo"},
		{"POST", "/api/ovh/vps/vps-123/reboot", nil, "sudo"},
		{"GET", "/api/server-control/ns123/console", nil, "sudo"},
		{"PUT", "/api/server-control/ns123/firewall", nil, "sudo"},
		{"POST", "/api/mcp/settings", nil, "sudo"},
		{"POST", "/api/mcp/token/rotate", nil, "sudo"},
		{"POST", "/api/queue", map[string]any{"auto_pay": false}, ""},
		{"POST", "/api/queue", map[string]any{"auto_pay": true}, "sudo"},
		{"POST", "/api/queue", map[string]any{"waiveRetractationPeriod": true}, "sudo"},
		{"PUT", "/api/queue/q1", map[string]any{"autoPay": true}, "sudo"},
		{"POST", "/api/orders/quick-order", map[string]any{"auto_pay": false}, ""},
		{"POST", "/api/orders/quick-order", map[string]any{"auto_pay": true}, "sudo"},
		{"POST", "/api/orders/quick-order", map[string]any{"waive_retractation_period": true}, "sudo"},
		// Ensure unknown/legacy quick-order routes do not match high-risk tier
		{"POST", "/api/legacy/quick-order", map[string]any{"auto_pay": true}, ""},
		{"GET", "/api/servers", nil, ""},
		{"OPTIONS", "/api/server-control/ns123/install", nil, ""},
	}

	for _, tc := range cases {
		if got := ClassifyRouteTier(tc.method, tc.path, tc.body); got != tc.want {
			t.Errorf("ClassifyRouteTier(%s, %s, %v) = %q, want %q", tc.method, tc.path, tc.body, got, tc.want)
		}
	}
}

func doJSONReq(t *testing.T, h http.Handler, method, path string, body any, cookies []*http.Cookie, headers map[string]string, remoteAddr string) *httptest.ResponseRecorder {
	t.Helper()
	var bodyReader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		bodyReader = bytes.NewReader(b)
	}
	req := httptest.NewRequest(method, path, bodyReader)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if remoteAddr != "" {
		req.RemoteAddr = remoteAddr
	} else {
		req.RemoteAddr = "198.51.100.10:12345"
	}
	for _, c := range cookies {
		req.AddCookie(c)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func buildTestRouter(svc *Service) http.Handler {
	r := chi.NewRouter()
	r.Use(svc.RequireAuthAndTOTPGate)
	r.Route("/api", func(api chi.Router) {
		svc.MountRoutes(api)
		api.Get("/health", func(w http.ResponseWriter, _ *http.Request) {
			writeAuthJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		})
		api.Get("/servers", func(w http.ResponseWriter, _ *http.Request) {
			writeAuthJSON(w, http.StatusOK, map[string]any{"items": []string{}})
		})
		api.Post("/server-control/{svc}/reboot", func(w http.ResponseWriter, _ *http.Request) {
			writeAuthJSON(w, http.StatusOK, map[string]bool{"rebooted": true})
		})
		api.Post("/orders/quick-order", func(w http.ResponseWriter, r *http.Request) {
			// Verify middleware restored r.Body for downstream handler
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				writeAuthJSON(w, http.StatusBadRequest, map[string]string{"error": "body missing: " + err.Error()})
				return
			}
			writeAuthJSON(w, http.StatusOK, map[string]any{"ordered": true, "planCode": payload["planCode"]})
		})
	})
	return r
}

func TestAuthLoginRateLimitAndTrustedProxy(t *testing.T) {
	svc, _, cfg, nowUnix := newTestAuthEnv(t, "127.0.0.1,10.0.0.0/8")
	router := buildTestRouter(svc)

	// Unauthenticated request -> 401
	recUnauth := doJSONReq(t, router, http.MethodGet, "/api/servers", nil, nil, nil, "")
	if recUnauth.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 unauthenticated, got %d", recUnauth.Code)
	}

	// Untrusted peer cannot spoof X-Forwarded-For to evade rate limiting
	for i := range LoginMaxAttempts {
		rec := doJSONReq(t, router, http.MethodPost, "/api/auth/login", map[string]string{"username": "Nexus", "password": "wrong-password"},
			nil, map[string]string{"X-Forwarded-For": "203.0.113." + string(rune('1'+i))}, "198.51.100.77:5555")
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d expected 401, got %d", i+1, rec.Code)
		}
	}
	recLimited := doJSONReq(t, router, http.MethodPost, "/api/auth/login", map[string]string{"username": "Nexus", "password": "wrong-password"},
		nil, map[string]string{"X-Forwarded-For": "203.0.113.99"}, "198.51.100.77:5555")
	if recLimited.Code != http.StatusTooManyRequests || recLimited.Header().Get("Retry-After") == "" {
		t.Fatalf("expected 429 rate limited for untrusted peer spoofing XFF, got %d", recLimited.Code)
	}

	// Wrong or case-mismatched username must fail with 401 INVALID_CREDENTIALS even when password is valid
	for _, badUser := range []string{"", "nexus", "NEXUS", "admin"} {
		recBadUser := doJSONReq(t, router, http.MethodPost, "/api/auth/login", map[string]string{"username": badUser, "password": cfg.APISecretKey},
			nil, map[string]string{"X-Forwarded-For": "203.0.113.210"}, "127.0.0.1:4444")
		if recBadUser.Code != http.StatusUnauthorized || !strings.Contains(recBadUser.Body.String(), "INVALID_CREDENTIALS") {
			t.Fatalf("expected 401 INVALID_CREDENTIALS for username %q, got %d body=%s", badUser, recBadUser.Code, recBadUser.Body.String())
		}
	}

	// Trusted proxy peer extracts distinct client IP from XFF
	recTrusted := doJSONReq(t, router, http.MethodPost, "/api/auth/login", map[string]string{"username": "Nexus", "password": cfg.APISecretKey},
		nil, map[string]string{"X-Forwarded-For": "203.0.113.200"}, "127.0.0.1:4444")
	if recTrusted.Code != http.StatusOK {
		t.Fatalf("expected 200 login via trusted proxy, got %d body=%s", recTrusted.Code, recTrusted.Body.String())
	}
	cookies := recTrusted.Result().Cookies()
	if len(cookies) == 0 || cookies[0].Name != SessionCookieName || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteLaxMode {
		t.Fatalf("expected HttpOnly SameSite=Lax ovh_session cookie, got %+v", cookies)
	}

	// Direct loopback X-API-Key works ONLY without proxy headers
	recKeyOK := doJSONReq(t, router, http.MethodGet, "/api/servers", nil, nil,
		map[string]string{"X-API-Key": cfg.APISecretKey}, "127.0.0.1:1234")
	if recKeyOK.Code != http.StatusOK {
		t.Fatalf("expected direct loopback X-API-Key 200, got %d", recKeyOK.Code)
	}
	recKeySpoof := doJSONReq(t, router, http.MethodGet, "/api/servers", nil, nil,
		map[string]string{"X-API-Key": cfg.APISecretKey, "X-Real-IP": "203.0.113.5"}, "127.0.0.1:1234")
	if recKeySpoof.Code != http.StatusUnauthorized {
		t.Fatalf("expected proxied X-API-Key rejected with 401, got %d", recKeySpoof.Code)
	}

	// Internal token bypasses auth
	recInt := doJSONReq(t, router, http.MethodGet, "/api/servers", nil, nil,
		map[string]string{"X-Internal-Token": cfg.InternalToken}, "198.51.100.88:9000")
	if recInt.Code != http.StatusOK {
		t.Fatalf("expected internal token 200, got %d", recInt.Code)
	}

	// Advance past login lockout and verify status/logout
	nowUnix.Add(LoginWindowSeconds + 1)
	recStatus := doJSONReq(t, router, http.MethodGet, "/api/auth/status", nil, cookies, nil, "")
	if recStatus.Code != http.StatusOK || !strings.Contains(recStatus.Body.String(), `"authenticated":true`) {
		t.Fatalf("unexpected auth status: %s", recStatus.Body.String())
	}
}

func TestTOTPEnrollmentAntiReplayRecoveryAndGatekeeper(t *testing.T) {
	svc, _, cfg, nowUnix := newTestAuthEnv(t, "")
	router := buildTestRouter(svc)

	// Log in before 2FA is enabled
	recLogin := doJSONReq(t, router, http.MethodPost, "/api/auth/login", map[string]string{"username": "Nexus", "password": cfg.APISecretKey}, nil, nil, "")
	if recLogin.Code != http.StatusOK {
		t.Fatalf("login failed: %d", recLogin.Code)
	}
	cookies := recLogin.Result().Cookies()

	// High-risk sudo/strict routes are blocked with 403 TOTP_SETUP_REQUIRED when 2FA is not enabled
	recBlocked := doJSONReq(t, router, http.MethodPost, "/api/server-control/ns123/reboot", nil, cookies, nil, "")
	if recBlocked.Code != http.StatusForbidden || !strings.Contains(recBlocked.Body.String(), "TOTP_SETUP_REQUIRED") {
		t.Fatalf("expected 403 TOTP_SETUP_REQUIRED, got %d body=%s", recBlocked.Code, recBlocked.Body.String())
	}

	// Begin TOTP setup
	recSetup := doJSONReq(t, router, http.MethodPost, "/api/auth/totp/setup", map[string]string{"accountName": "OpsAdmin"}, cookies, nil, "")
	if recSetup.Code != http.StatusOK {
		t.Fatalf("totp setup failed: %d body=%s", recSetup.Code, recSetup.Body.String())
	}
	var setupResp struct {
		Secret     string `json:"secret"`
		OTPAuthURI string `json:"otpauthUri"`
	}
	_ = json.Unmarshal(recSetup.Body.Bytes(), &setupResp)
	if setupResp.Secret == "" || !strings.Contains(setupResp.OTPAuthURI, "OpsAdmin") {
		t.Fatalf("invalid setup response: %+v", setupResp)
	}

	// Activate with valid 6-digit code
	code0, _, err := ComputeCurrentTOTP(setupResp.Secret, time.Unix(nowUnix.Load(), 0).UTC())
	if err != nil {
		t.Fatalf("ComputeCurrentTOTP: %v", err)
	}
	recActivate := doJSONReq(t, router, http.MethodPost, "/api/auth/totp/activate", map[string]string{"code": code0}, cookies, nil, "")
	if recActivate.Code != http.StatusOK {
		t.Fatalf("activate failed: %d body=%s", recActivate.Code, recActivate.Body.String())
	}
	var actResp struct {
		Enabled       bool     `json:"enabled"`
		TOTPEnabled   bool     `json:"totpEnabled"`
		RecoveryCodes []string `json:"recoveryCodes"`
	}
	_ = json.Unmarshal(recActivate.Body.Bytes(), &actResp)
	if !actResp.Enabled || !actResp.TOTPEnabled || len(actResp.RecoveryCodes) != 8 {
		t.Fatalf("unexpected activate response: %+v", actResp)
	}
	if len(recActivate.Result().Cookies()) > 0 {
		cookies = recActivate.Result().Cookies()
	}

	// Verify /api/auth/totp/status returns BOTH enabled and totpEnabled
	recTStatus := doJSONReq(t, router, http.MethodGet, "/api/auth/totp/status", nil, cookies, nil, "")
	if recTStatus.Code != http.StatusOK ||
		!strings.Contains(recTStatus.Body.String(), `"enabled":true`) ||
		!strings.Contains(recTStatus.Body.String(), `"totpEnabled":true`) {
		t.Fatalf("totp status missing enabled/totpEnabled: %s", recTStatus.Body.String())
	}

	// Anti-replay: reusing code0 in same step must fail with TOTP_REPLAYED
	recReplay := doJSONReq(t, router, http.MethodPost, "/api/auth/totp/verify-sudo", map[string]string{"code": code0}, cookies, nil, "")
	if recReplay.Code != http.StatusForbidden || !strings.Contains(recReplay.Body.String(), "TOTP_REPLAYED") {
		t.Fatalf("expected TOTP_REPLAYED, got %d body=%s", recReplay.Code, recReplay.Body.String())
	}

	// Non-sudo quick-order works without TOTP and preserves body
	recNormalOrder := doJSONReq(t, router, http.MethodPost, "/api/orders/quick-order",
		map[string]any{"planCode": "24ska01", "auto_pay": false}, cookies, nil, "")
	if recNormalOrder.Code != http.StatusOK || !strings.Contains(recNormalOrder.Body.String(), `"planCode":"24ska01"`) {
		t.Fatalf("expected normal quick-order to pass and keep body, got %d body=%s", recNormalOrder.Code, recNormalOrder.Body.String())
	}

	// Expire sudo window (+301s) and test sudo step-up -> 5m window -> strict still requires fresh code
	nowUnix.Add(SudoDurationSeconds + 1)
	recNeedSudo := doJSONReq(t, router, http.MethodPost, "/api/orders/quick-order",
		map[string]any{"planCode": "24ska01", "auto_pay": true}, cookies, nil, "")
	if recNeedSudo.Code != http.StatusForbidden || !strings.Contains(recNeedSudo.Body.String(), "TOTP_STEP_UP_REQUIRED") {
		t.Fatalf("expected TOTP_STEP_UP_REQUIRED when auto_pay=true, got %d body=%s", recNeedSudo.Code, recNeedSudo.Body.String())
	}

	// Provide fresh TOTP code for sudo step-up
	code1, _, _ := ComputeCurrentTOTP(setupResp.Secret, time.Unix(nowUnix.Load(), 0).UTC())
	recSudoOK := doJSONReq(t, router, http.MethodPost, "/api/orders/quick-order",
		map[string]any{"planCode": "24ska01", "auto_pay": true}, cookies, map[string]string{"X-TOTP-Code": code1}, "")
	if recSudoOK.Code != http.StatusOK || !strings.Contains(recSudoOK.Body.String(), `"planCode":"24ska01"`) {
		t.Fatalf("expected sudo step-up success, got %d body=%s", recSudoOK.Code, recSudoOK.Body.String())
	}
	if len(recSudoOK.Result().Cookies()) > 0 {
		cookies = recSudoOK.Result().Cookies()
	}

	// Subsequent sudo operation within 5m passes without X-TOTP-Code
	recSudoCached := doJSONReq(t, router, http.MethodPost, "/api/server-control/ns123/reboot", nil, cookies, nil, "")
	if recSudoCached.Code != http.StatusOK {
		t.Fatalf("expected cached sudo window to allow reboot, got %d body=%s", recSudoCached.Code, recSudoCached.Body.String())
	}

	// Strict route STILL requires fresh code even when sudo is active
	recStrictNeed := doJSONReq(t, router, http.MethodPost, "/api/auth/totp/recovery-codes/regenerate", map[string]any{}, cookies, nil, "")
	if recStrictNeed.Code != http.StatusForbidden || !strings.Contains(recStrictNeed.Body.String(), `"tier":"strict"`) {
		t.Fatalf("expected strict tier prompt despite active sudo, got %d body=%s", recStrictNeed.Code, recStrictNeed.Body.String())
	}

	// Use single-use recovery code to regenerate recovery codes
	recCode := actResp.RecoveryCodes[0]
	recRegen := doJSONReq(t, router, http.MethodPost, "/api/auth/totp/recovery-codes/regenerate",
		map[string]any{}, cookies, map[string]string{"X-TOTP-Code": recCode}, "")
	if recRegen.Code != http.StatusOK {
		t.Fatalf("expected recovery code to satisfy strict gate, got %d body=%s", recRegen.Code, recRegen.Body.String())
	}
	var regenResp struct {
		RecoveryCodes []string `json:"recoveryCodes"`
	}
	_ = json.Unmarshal(recRegen.Body.Bytes(), &regenResp)
	if len(regenResp.RecoveryCodes) != 8 {
		t.Fatalf("expected 8 regenerated recovery codes, got %+v", regenResp)
	}

	// Regenerating recovery codes rotates session_epoch, immediately revoking the previous cookie.
	recStaleSession := doJSONReq(t, router, http.MethodPost, "/api/auth/totp/disable",
		map[string]any{}, cookies, map[string]string{"X-TOTP-Code": actResp.RecoveryCodes[1]}, "")
	if recStaleSession.Code != http.StatusUnauthorized {
		t.Fatalf("expected old session cookie to be revoked (401) after recovery code regeneration, got %d", recStaleSession.Code)
	}
	if len(recRegen.Result().Cookies()) == 0 {
		t.Fatalf("expected fresh rotated session cookie after recovery code regeneration")
	}
	cookies = recRegen.Result().Cookies()

	// Old recovery code #1 must no longer work after regeneration (with rotated session cookie)
	recOldRec := doJSONReq(t, router, http.MethodPost, "/api/auth/totp/disable",
		map[string]any{}, cookies, map[string]string{"X-TOTP-Code": actResp.RecoveryCodes[1]}, "")
	if recOldRec.Code != http.StatusForbidden {
		t.Fatalf("expected invalidated old recovery code to fail, got %d", recOldRec.Code)
	}

	// Login requires TOTP prompt when enabled
	recPassOnly := doJSONReq(t, router, http.MethodPost, "/api/auth/login", map[string]string{"username": "Nexus", "password": cfg.APISecretKey}, nil, nil, "")
	if recPassOnly.Code != http.StatusOK || !strings.Contains(recPassOnly.Body.String(), `"requireTotp":true`) {
		t.Fatalf("expected requireTotp prompt on password-only login, got %d body=%s", recPassOnly.Code, recPassOnly.Body.String())
	}

	// Login with regenerated recovery code succeeds and consumes it (8 -> 7)
	recLoginWithRec := doJSONReq(t, router, http.MethodPost, "/api/auth/login",
		map[string]string{"username": "Nexus", "password": cfg.APISecretKey, "totpCode": regenResp.RecoveryCodes[0]}, nil, nil, "")
	if recLoginWithRec.Code != http.StatusOK || !strings.Contains(recLoginWithRec.Body.String(), `"authenticated":true`) {
		t.Fatalf("expected login with recovery code to succeed, got %d body=%s", recLoginWithRec.Code, recLoginWithRec.Body.String())
	}

	// Disable 2FA using another fresh TOTP step (+30s)
	nowUnix.Add(TOTPPeriodSeconds)
	code2, _, _ := ComputeCurrentTOTP(setupResp.Secret, time.Unix(nowUnix.Load(), 0).UTC())
	recDisable := doJSONReq(t, router, http.MethodPost, "/api/auth/totp/disable",
		map[string]any{}, cookies, map[string]string{"X-TOTP-Code": code2}, "")
	if recDisable.Code != http.StatusOK || !strings.Contains(recDisable.Body.String(), `"totpEnabled":false`) {
		t.Fatalf("expected disable success, got %d body=%s", recDisable.Code, recDisable.Body.String())
	}
}

func TestSessionEpochAndKeyFingerprintRevocation(t *testing.T) {
	svc, st, cfg, _ := newTestAuthEnv(t, "")
	router := chi.NewRouter()
	router.Use(svc.RequireAuthAndTOTPGate)
	router.Route("/api", func(api chi.Router) {
		svc.MountRoutes(api)
		api.Get("/servers", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	})

	// Two devices log in concurrently.
	rec1 := doJSONReq(t, router, http.MethodPost, "/api/auth/login", map[string]string{"username": "Nexus", "password": cfg.APISecretKey}, nil, nil, "")
	rec2 := doJSONReq(t, router, http.MethodPost, "/api/auth/login", map[string]string{"username": "Nexus", "password": cfg.APISecretKey}, nil, nil, "")
	if rec1.Code != http.StatusOK || rec2.Code != http.StatusOK {
		t.Fatalf("logins failed: %d %d", rec1.Code, rec2.Code)
	}
	dev1 := rec1.Result().Cookies()
	dev2 := rec2.Result().Cookies()

	// Unauthenticated POST /api/auth/logout MUST NOT bump session epoch or kick out logged-in devices.
	recAnonLogout := doJSONReq(t, router, http.MethodPost, "/api/auth/logout", nil, nil, nil, "")
	if recAnonLogout.Code != http.StatusOK {
		t.Fatalf("anonymous logout status=%d", recAnonLogout.Code)
	}
	recCheck := doJSONReq(t, router, http.MethodGet, "/api/servers", nil, dev2, nil, "")
	if recCheck.Code != http.StatusOK {
		t.Fatalf("expected dev2 session to remain valid after unauthenticated logout, got %d", recCheck.Code)
	}

	// Authenticated logout from dev1 bumps persistent session_epoch -> immediately revokes dev2!
	recLogout := doJSONReq(t, router, http.MethodPost, "/api/auth/logout", nil, dev1, nil, "")
	if recLogout.Code != http.StatusOK {
		t.Fatalf("authenticated logout status=%d", recLogout.Code)
	}
	recRevoked := doJSONReq(t, router, http.MethodGet, "/api/servers", nil, dev2, nil, "")
	if recRevoked.Code != http.StatusUnauthorized {
		t.Fatalf("expected dev2 cookie to be immediately revoked (401) after dev1 logout, got %d", recRevoked.Code)
	}

	// Fresh login after logout works on the bumped epoch.
	rec3 := doJSONReq(t, router, http.MethodPost, "/api/auth/login", map[string]string{"username": "Nexus", "password": cfg.APISecretKey}, nil, nil, "")
	dev3 := rec3.Result().Cookies()
	if rec3.Code != http.StatusOK || len(dev3) == 0 {
		t.Fatalf("re-login failed: %d", rec3.Code)
	}

	// Rotating API_SECRET_KEY and recreating auth.Service on the same DB invalidates dev3 via keyFp mismatch!
	rotatedCfg := *cfg
	rotatedCfg.APISecretKey = "rotated-brand-new-secret-key-9999"
	svcRotated, err := NewService(context.Background(), &rotatedCfg, st, nil)
	if err != nil {
		t.Fatalf("NewService rotated: %v", err)
	}
	rRotated := chi.NewRouter()
	rRotated.Use(svcRotated.RequireAuthAndTOTPGate)
	rRotated.Route("/api", func(api chi.Router) {
		svcRotated.MountRoutes(api)
		api.Get("/servers", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	})
	recKeyRotated := doJSONReq(t, rRotated, http.MethodGet, "/api/servers", nil, dev3, nil, "")
	if recKeyRotated.Code != http.StatusUnauthorized {
		t.Fatalf("expected cookie from old API_SECRET_KEY to be rejected (401) after rotation, got %d", recKeyRotated.Code)
	}
}

func TestContentTypeAgnosticBodySensitive2FAGate(t *testing.T) {
	svc, _, cfg, _ := newTestAuthEnv(t, "")
	router := chi.NewRouter()
	router.Use(svc.RequireAuthAndTOTPGate)
	router.Route("/api", func(api chi.Router) {
		svc.MountRoutes(api)
		api.Post("/settings", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
		api.Post("/orders/quick-order", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
		api.Post("/queue", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	})

	recLogin := doJSONReq(t, router, http.MethodPost, "/api/auth/login", map[string]string{
		"username": "Nexus",
		"password": cfg.APISecretKey,
	}, nil, nil, "")
	if recLogin.Code != http.StatusOK {
		t.Fatalf("login failed: %d", recLogin.Code)
	}
	cookies := recLogin.Result().Cookies()

	sendRaw := func(path, contentType, rawBody string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(rawBody))
		if contentType != "" {
			req.Header.Set("Content-Type", contentType)
		}
		for _, c := range cookies {
			req.AddCookie(c)
		}
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}

	// 1. text/plain or omitted Content-Type carrying JSON credential update MUST still hit strict 2FA gate (403 TOTP_SETUP_REQUIRED).
	for _, ct := range []string{"text/plain", ""} {
		rec := sendRaw("/api/settings", ct, `{"clearCredentials":true}`)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("expected 403 2FA gate for POST /api/settings with Content-Type=%q, got %d body=%s", ct, rec.Code, rec.Body.String())
		}
		recAutoPay := sendRaw("/api/orders/quick-order", ct, `{"planCode":"24ska01","auto_pay":true}`)
		if recAutoPay.Code != http.StatusForbidden {
			t.Fatalf("expected 403 2FA gate for auto_pay quick-order with Content-Type=%q, got %d body=%s", ct, recAutoPay.Code, recAutoPay.Body.String())
		}
		recQueue := sendRaw("/api/queue", ct, `{"planCode":"24ska01","waiveRetractationPeriod":true}`)
		if recQueue.Code != http.StatusForbidden {
			t.Fatalf("expected 403 2FA gate for waiveRetractationPeriod queue with Content-Type=%q, got %d body=%s", ct, recQueue.Code, recQueue.Body.String())
		}
	}

	// 2. Malformed non-empty body on a body-sensitive route fails closed with 400 INVALID_JSON_BODY.
	recBad := sendRaw("/api/settings", "text/plain", `not-valid-json`)
	if recBad.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 INVALID_JSON_BODY for malformed body on /api/settings, got %d body=%s", recBad.Code, recBad.Body.String())
	}

	// 3. Non-sensitive payload on /api/orders/quick-order (auto_pay=false) with normal JSON still passes.
	recSafe := sendRaw("/api/orders/quick-order", "application/json", `{"planCode":"24ska01","auto_pay":false}`)
	if recSafe.Code != http.StatusOK {
		t.Fatalf("expected 200 for non-sensitive quick-order, got %d body=%s", recSafe.Code, recSafe.Body.String())
	}
}
