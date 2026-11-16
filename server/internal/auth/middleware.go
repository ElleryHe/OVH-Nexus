package auth

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"ovh-nexus/server/internal/config"
	"ovh-nexus/server/internal/logger"
	"ovh-nexus/server/internal/store"
	"ovh-nexus/server/internal/vault"
)

const (
	SessionCookieName    = "ovh_session"
	SessionLifetimeHours = 12
	maxInspectBodyBytes  = 2 << 20
)

var digitsCleanRegex = regexp.MustCompile(`\s+`)

var whitelistPaths = map[string]struct{}{
	"/health":          {},
	"/api/health":      {},
	"/api/auth/login":  {},
	"/api/auth/status": {},
	"/api/auth/logout": {},
	"/api/mcp":         {},
}

// SessionPayload represents the signed cookie claims stored in ovh_session.
type SessionPayload struct {
	Authenticated bool  `json:"auth"`
	LoginAt       int64 `json:"loginAt"`
	SudoUntil     int64 `json:"sudoUntil,omitempty"`
	ExpiresAt     int64 `json:"exp"`
}

// Service manages session authentication, rate limiting, and RFC 6238 TOTP gatekeeping.
type Service struct {
	cfg           *config.Config
	store         *store.Store
	log           *logger.Hub
	sessionSecret []byte
	nowFn         func() time.Time

	totpMu       sync.Mutex
	loginLimiter *slidingRateLimiter
	totpLimiter  *slidingRateLimiter
}

// NewService creates an auth Service, loading or creating the persistent HMAC session secret.
func NewService(ctx context.Context, cfg *config.Config, st *store.Store, log *logger.Hub) (*Service, error) {
	if cfg == nil {
		return nil, errors.New("auth: config must not be nil")
	}
	if st == nil {
		return nil, errors.New("auth: store must not be nil")
	}

	secretStr := cfg.SessionSecretOverride
	if secretStr == "" {
		var err error
		secretStr, err = st.GetOrCreateSessionSecret(ctx)
		if err != nil {
			return nil, fmt.Errorf("auth: failed to obtain session secret: %w", err)
		}
	}

	nowFn := func() time.Time { return time.Now().UTC() }
	return &Service{
		cfg:           cfg,
		store:         st,
		log:           log,
		sessionSecret: []byte(secretStr),
		nowFn:         nowFn,
		loginLimiter:  newSlidingRateLimiter(LoginMaxAttempts, LoginWindowSeconds*time.Second, nowFn),
		totpLimiter:   newSlidingRateLimiter(TOTPMaxFailures, TOTPLockoutSeconds*time.Second, nowFn),
	}, nil
}

// SetNowFunc overrides the clock (intended for deterministic unit tests).
func (s *Service) SetNowFunc(fn func() time.Time) {
	if fn == nil {
		return
	}
	s.nowFn = fn
	s.loginLimiter.nowFn = fn
	s.totpLimiter.nowFn = fn
}

// MountRoutes registers all /api/auth/* routes on an /api chi.Router.
func (s *Service) MountRoutes(r chi.Router) {
	r.Post("/auth/login", s.HandleLogin)
	r.Get("/auth/status", s.HandleStatus)
	r.Post("/auth/logout", s.HandleLogout)
	r.Get("/auth/totp/status", s.HandleTOTPStatus)
	r.Post("/auth/totp/setup", s.HandleTOTPSetup)
	r.Post("/auth/totp/activate", s.HandleTOTPActivate)
	r.Post("/auth/totp/verify-sudo", s.HandleTOTPVerifySudo)
	r.Post("/auth/totp/disable", s.HandleTOTPDisable)
	r.Post("/auth/totp/recovery-codes/regenerate", s.HandleTOTPRegenerateRecovery)
}

// ==================== Middleware ====================

// RequireAuthAndTOTPGate enforces session / loopback-API-key / internal-token auth on /api/*
// and enforces the tiered strict/sudo 2FA gatekeeper on high-risk endpoints.
func (s *Service) RequireAuthAndTOTPGate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		if !strings.HasPrefix(p, "/api/") {
			next.ServeHTTP(w, r)
			return
		}
		if r.Method == http.MethodOptions {
			next.ServeHTTP(w, r)
			return
		}
		normPath := strings.TrimRight(p, "/")
		if _, ok := whitelistPaths[normPath]; ok {
			next.ServeHTTP(w, r)
			return
		}

		// Internal process calls bypass browser session & TOTP gate.
		if tok := strings.TrimSpace(r.Header.Get("X-Internal-Token")); tok != "" && s.cfg.VerifyInternalToken(tok) {
			next.ServeHTTP(w, r)
			return
		}

		sess, hasSession := s.readSessionCookie(r)
		authenticated := hasSession && sess.Authenticated
		if !authenticated {
			apiKey := strings.TrimSpace(r.Header.Get("X-API-Key"))
			if apiKey != "" && s.isDirectLoopback(r) && s.cfg.VerifyAPISecretKey(apiKey) {
				authenticated = true
			}
		}

		if !authenticated {
			writeAuthJSON(w, http.StatusUnauthorized, map[string]any{
				"error":   "Unauthorized",
				"code":    "AUTH_REQUIRED",
				"message": "Authentication required. Please log in.",
			})
			return
		}

		// Peek JSON body if needed for route tier classification or totpCode fallback, then restore r.Body.
		var bodyMap map[string]any
		if strings.Contains(strings.ToLower(r.Header.Get("Content-Type")), "application/json") && r.Body != nil {
			bodyBytes, err := io.ReadAll(io.LimitReader(r.Body, maxInspectBodyBytes))
			_ = r.Body.Close()
			r.Body = io.NopCloser(bytes.NewReader(bodyBytes))
			if err == nil && len(bytes.TrimSpace(bodyBytes)) > 0 {
				_ = json.Unmarshal(bodyBytes, &bodyMap)
			}
		}

		tier := ClassifyRouteTier(r.Method, normPath, bodyMap)
		if tier == "" {
			next.ServeHTTP(w, r)
			return
		}

		enabled, err := s.IsTOTPEnabled(r.Context())
		if err != nil {
			writeAuthJSON(w, http.StatusInternalServerError, map[string]any{
				"error":   "Internal Server Error",
				"code":    "TOTP_STATE_ERROR",
				"message": "Failed to verify two-factor authentication state.",
			})
			return
		}
		if !enabled {
			writeAuthJSON(w, http.StatusForbidden, map[string]any{
				"error":   "2FA Setup Required",
				"code":    "TOTP_SETUP_REQUIRED",
				"tier":    tier,
				"message": "Security policy requires Two-Factor Authentication (2FA) to be enabled before executing high-risk operations. Configure 2FA in Settings.",
			})
			return
		}

		candidateCode := extractRequestTOTPCode(r, bodyMap)
		nowTS := s.nowFn().Unix()
		clientIP := s.ClientIP(r)

		if tier == "sudo" {
			if candidateCode == "" && hasSession && sess.SudoUntil > nowTS {
				next.ServeHTTP(w, r)
				return
			}
			if candidateCode == "" {
				writeAuthJSON(w, http.StatusForbidden, map[string]any{
					"error":   "2FA Sudo Required",
					"code":    "TOTP_STEP_UP_REQUIRED",
					"tier":    "sudo",
					"message": "This elevated operation requires 2FA verification (valid for 5 minutes once verified).",
				})
				return
			}

			ok, statusCode, msg := s.VerifyTOTPOrRecovery(r.Context(), candidateCode, "sudo:"+clientIP)
			if !ok {
				if s.log != nil {
					s.log.Warn(logger.SourceTOTP, "Rejected sudo 2FA step-up verification", "ip", clientIP, "code", statusCode)
				}
				writeAuthJSON(w, http.StatusForbidden, map[string]any{
					"error":   "Invalid 2FA Code",
					"code":    statusCode,
					"tier":    "sudo",
					"message": msg,
				})
				return
			}

			if hasSession {
				sess.SudoUntil = nowTS + SudoDurationSeconds
				s.writeSessionCookie(w, sess)
			}
			next.ServeHTTP(w, r)
			return
		}

		// tier == "strict": requires fresh TOTP or recovery code on every call.
		if candidateCode == "" {
			writeAuthJSON(w, http.StatusForbidden, map[string]any{
				"error":   "2FA Strict Verification Required",
				"code":    "TOTP_STEP_UP_REQUIRED",
				"tier":    "strict",
				"message": "This irreversible operation requires a fresh 6-digit 2FA code or recovery code.",
			})
			return
		}

		ok, statusCode, msg := s.VerifyTOTPOrRecovery(r.Context(), candidateCode, "strict:"+clientIP)
		if !ok {
			if s.log != nil {
				s.log.Warn(logger.SourceTOTP, "Rejected strict 2FA step-up verification", "ip", clientIP, "code", statusCode)
			}
			writeAuthJSON(w, http.StatusForbidden, map[string]any{
				"error":   "Invalid 2FA Code",
				"code":    statusCode,
				"tier":    "strict",
				"message": msg,
			})
			return
		}

		if hasSession {
			sess.SudoUntil = nowTS + SudoDurationSeconds
			s.writeSessionCookie(w, sess)
		}
		next.ServeHTTP(w, r)
	})
}

// ==================== HTTP Handlers ====================

// HandleLogin processes POST /api/auth/login.
func (s *Service) HandleLogin(w http.ResponseWriter, r *http.Request) {
	ip := s.ClientIP(r)
	if allowed, retryAfter := s.loginLimiter.Check(ip); !allowed {
		if s.log != nil {
			s.log.Warn(logger.SourceAuth, "Login rate limit exceeded", "ip", ip, "retryAfter", retryAfter)
		}
		w.Header().Set("Retry-After", fmt.Sprintf("%d", retryAfter))
		writeAuthJSON(w, http.StatusTooManyRequests, map[string]any{
			"error":   "Too Many Requests",
			"code":    "RATE_LIMITED",
			"message": fmt.Sprintf("Too many login attempts. Please retry in %d seconds.", retryAfter),
		})
		return
	}

	var payload struct {
		Password     string `json:"password"`
		TOTPCode     string `json:"totpCode"`
		TOTPCodeSnak string `json:"totp_code"`
	}
	_ = json.NewDecoder(io.LimitReader(r.Body, 64<<10)).Decode(&payload)

	if !s.cfg.VerifyAPISecretKey(payload.Password) {
		s.loginLimiter.RecordFailure(ip)
		if s.log != nil {
			s.log.Warn(logger.SourceAuth, "Rejected login attempt: invalid credentials", "ip", ip)
		}
		writeAuthJSON(w, http.StatusUnauthorized, map[string]any{
			"error":   "Invalid credentials",
			"code":    "INVALID_PASSWORD",
			"message": "Invalid password.",
		})
		return
	}

	totpEnabled, err := s.IsTOTPEnabled(r.Context())
	if err != nil {
		writeAuthJSON(w, http.StatusInternalServerError, map[string]any{
			"error":   "Internal Server Error",
			"code":    "TOTP_STATE_ERROR",
			"message": "Failed to check two-factor authentication state.",
		})
		return
	}

	var sudoExpiresAt any
	now := s.nowFn()
	nowTS := now.Unix()
	var sudoUntil int64

	if totpEnabled {
		totpCode := strings.TrimSpace(payload.TOTPCode)
		if totpCode == "" {
			totpCode = strings.TrimSpace(payload.TOTPCodeSnak)
		}
		if totpCode == "" {
			writeAuthJSON(w, http.StatusOK, map[string]any{
				"authenticated": false,
				"requireTotp":   true,
				"totpEnabled":   true,
				"message":       "Please enter your 6-digit 2FA code or 8-character recovery code.",
			})
			return
		}

		ok, statusCode, msg := s.VerifyTOTPOrRecovery(r.Context(), totpCode, "login:"+ip)
		if !ok {
			s.loginLimiter.RecordFailure(ip)
			if s.log != nil {
				s.log.Warn(logger.SourceAuth, "Rejected login 2FA verification", "ip", ip, "code", statusCode)
			}
			writeAuthJSON(w, http.StatusUnauthorized, map[string]any{
				"error":       "Invalid 2FA code",
				"code":        statusCode,
				"requireTotp": true,
				"message":     msg,
			})
			return
		}
		sudoUntil = nowTS + SudoDurationSeconds
		sudoExpiresAt = sudoUntil
	}

	s.loginLimiter.Reset(ip)
	s.writeSessionCookie(w, SessionPayload{
		Authenticated: true,
		LoginAt:       nowTS,
		SudoUntil:     sudoUntil,
		ExpiresAt:     now.Add(SessionLifetimeHours * time.Hour).Unix(),
	})

	if s.log != nil {
		s.log.Info(logger.SourceAuth, "Authenticated session established", "ip", ip, "totpEnabled", totpEnabled)
	}

	writeAuthJSON(w, http.StatusOK, map[string]any{
		"authenticated": true,
		"totpEnabled":   totpEnabled,
		"sudoActive":    totpEnabled,
		"sudoExpiresAt": sudoExpiresAt,
	})
}

// HandleStatus processes GET /api/auth/status.
func (s *Service) HandleStatus(w http.ResponseWriter, r *http.Request) {
	sess, ok := s.readSessionCookie(r)
	isAuthed := ok && sess.Authenticated
	totpEnabled, _ := s.IsTOTPEnabled(r.Context())
	nowTS := s.nowFn().Unix()
	sudoActive := isAuthed && sess.SudoUntil > nowTS

	var sudoExpiresAt any
	if sudoActive {
		sudoExpiresAt = sess.SudoUntil
	}
	writeAuthJSON(w, http.StatusOK, map[string]any{
		"authenticated": isAuthed,
		"totpEnabled":   totpEnabled,
		"sudoActive":    sudoActive,
		"sudoExpiresAt": sudoExpiresAt,
	})
}

// HandleLogout processes POST /api/auth/logout.
func (s *Service) HandleLogout(w http.ResponseWriter, _ *http.Request) {
	s.clearSessionCookie(w)
	writeAuthJSON(w, http.StatusOK, map[string]any{
		"authenticated": false,
	})
}

// HandleTOTPStatus processes GET /api/auth/totp/status.
func (s *Service) HandleTOTPStatus(w http.ResponseWriter, r *http.Request) {
	st, err := s.store.LoadTOTPState(r.Context())
	if err != nil {
		writeAuthJSON(w, http.StatusInternalServerError, map[string]any{
			"error":   "Failed to load 2FA state",
			"message": err.Error(),
		})
		return
	}
	enabled := st.Enabled && st.Secret != ""
	sess, hasSession := s.readSessionCookie(r)
	nowTS := s.nowFn().Unix()
	sudoActive := enabled && hasSession && sess.SudoUntil > nowTS

	var enabledAt any
	var sudoExpiresAt any
	rem := 0
	if enabled {
		rem = len(st.RecoveryCodeHashes)
		if st.EnabledAt != "" {
			enabledAt = st.EnabledAt
		}
		if sudoActive {
			sudoExpiresAt = sess.SudoUntil
		}
	}

	writeAuthJSON(w, http.StatusOK, map[string]any{
		"enabled":                enabled,
		"totpEnabled":            enabled,
		"hasPendingSetup":        st.PendingSecret != "" && !enabled,
		"recoveryCodesRemaining": rem,
		"enabledAt":              enabledAt,
		"sudoActive":             sudoActive,
		"sudoExpiresAt":          sudoExpiresAt,
	})
}

// HandleTOTPSetup processes POST /api/auth/totp/setup.
func (s *Service) HandleTOTPSetup(w http.ResponseWriter, r *http.Request) {
	var body struct {
		AccountName string `json:"accountName"`
	}
	_ = json.NewDecoder(io.LimitReader(r.Body, 16<<10)).Decode(&body)
	accountName := strings.TrimSpace(body.AccountName)
	if accountName == "" {
		accountName = "Admin"
	}

	info, err := s.BeginTOTPSetup(r.Context(), accountName)
	if err != nil {
		writeAuthJSON(w, http.StatusBadRequest, map[string]any{
			"success": false,
			"error":   err.Error(),
			"message": err.Error(),
		})
		return
	}

	writeAuthJSON(w, http.StatusOK, map[string]any{
		"success":    true,
		"secret":     info.Secret,
		"otpauthUri": info.OTPAuthURI,
		"period":     info.Period,
		"digits":     info.Digits,
	})
}

// HandleTOTPActivate processes POST /api/auth/totp/activate.
func (s *Service) HandleTOTPActivate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Code     string `json:"code"`
		TOTPCode string `json:"totpCode"`
	}
	_ = json.NewDecoder(io.LimitReader(r.Body, 16<<10)).Decode(&body)
	code := strings.TrimSpace(body.Code)
	if code == "" {
		code = strings.TrimSpace(body.TOTPCode)
	}

	res, err := s.ActivateTOTPSetup(r.Context(), code)
	if err != nil {
		writeAuthJSON(w, http.StatusBadRequest, map[string]any{
			"success": false,
			"error":   err.Error(),
			"message": err.Error(),
		})
		return
	}

	nowTS := s.nowFn().Unix()
	sudoUntil := nowTS + SudoDurationSeconds
	if sess, ok := s.readSessionCookie(r); ok {
		sess.SudoUntil = sudoUntil
		s.writeSessionCookie(w, sess)
	}

	if s.log != nil {
		s.log.Info(logger.SourceTOTP, "Two-factor authentication enabled")
	}

	writeAuthJSON(w, http.StatusOK, map[string]any{
		"success":                true,
		"enabled":                true,
		"totpEnabled":            true,
		"recoveryCodes":          res.RecoveryCodes,
		"recoveryCodesRemaining": res.RecoveryCodesRemaining,
		"enabledAt":              res.EnabledAt,
		"sudoActive":             true,
		"sudoExpiresAt":          sudoUntil,
	})
}

// HandleTOTPVerifySudo processes POST /api/auth/totp/verify-sudo.
func (s *Service) HandleTOTPVerifySudo(w http.ResponseWriter, r *http.Request) {
	enabled, err := s.IsTOTPEnabled(r.Context())
	if err != nil || !enabled {
		msg := "Two-factor authentication is not enabled."
		writeAuthJSON(w, http.StatusBadRequest, map[string]any{
			"success": false,
			"code":    "TOTP_SETUP_REQUIRED",
			"error":   msg,
			"message": msg,
		})
		return
	}

	var body struct {
		Code     string `json:"code"`
		TOTPCode string `json:"totpCode"`
	}
	_ = json.NewDecoder(io.LimitReader(r.Body, 16<<10)).Decode(&body)
	code := strings.TrimSpace(body.Code)
	if code == "" {
		code = strings.TrimSpace(body.TOTPCode)
	}

	ip := s.ClientIP(r)
	ok, statusCode, msg := s.VerifyTOTPOrRecovery(r.Context(), code, "sudo:"+ip)
	if !ok {
		writeAuthJSON(w, http.StatusForbidden, map[string]any{
			"success": false,
			"code":    statusCode,
			"error":   msg,
			"message": msg,
		})
		return
	}

	sudoUntil := s.nowFn().Unix() + SudoDurationSeconds
	if sess, ok := s.readSessionCookie(r); ok {
		sess.SudoUntil = sudoUntil
		s.writeSessionCookie(w, sess)
	}

	writeAuthJSON(w, http.StatusOK, map[string]any{
		"success":       true,
		"message":       msg,
		"sudoActive":    true,
		"sudoExpiresAt": sudoUntil,
	})
}

// HandleTOTPDisable processes POST /api/auth/totp/disable (protected by strict TOTP gate).
func (s *Service) HandleTOTPDisable(w http.ResponseWriter, r *http.Request) {
	if err := s.DisableTOTP(r.Context()); err != nil {
		writeAuthJSON(w, http.StatusInternalServerError, map[string]any{
			"success": false,
			"error":   err.Error(),
			"message": err.Error(),
		})
		return
	}
	if sess, ok := s.readSessionCookie(r); ok {
		sess.SudoUntil = 0
		s.writeSessionCookie(w, sess)
	}
	if s.log != nil {
		s.log.Warn(logger.SourceTOTP, "Two-factor authentication disabled")
	}
	writeAuthJSON(w, http.StatusOK, map[string]any{
		"success":     true,
		"enabled":     false,
		"totpEnabled": false,
		"message":     "Two-factor authentication has been disabled.",
	})
}

// HandleTOTPRegenerateRecovery processes POST /api/auth/totp/recovery-codes/regenerate (strict gate).
func (s *Service) HandleTOTPRegenerateRecovery(w http.ResponseWriter, r *http.Request) {
	codes, err := s.RegenerateRecoveryCodes(r.Context())
	if err != nil {
		writeAuthJSON(w, http.StatusBadRequest, map[string]any{
			"success": false,
			"error":   err.Error(),
			"message": err.Error(),
		})
		return
	}
	if s.log != nil {
		s.log.Info(logger.SourceTOTP, "Regenerated 2FA recovery codes", "count", len(codes))
	}
	writeAuthJSON(w, http.StatusOK, map[string]any{
		"success":                true,
		"recoveryCodes":          codes,
		"recoveryCodesRemaining": len(codes),
	})
}

// ==================== TOTP Business Logic ====================

// IsTOTPEnabled reports whether 2FA TOTP is configured and active.
func (s *Service) IsTOTPEnabled(ctx context.Context) (bool, error) {
	st, err := s.store.LoadTOTPState(ctx)
	if err != nil {
		return false, err
	}
	return st.Enabled && st.Secret != "", nil
}

// BeginTOTPSetup creates a pending TOTP secret without overwriting an active configuration.
func (s *Service) BeginTOTPSetup(ctx context.Context, accountName string) (*TOTPSetupInfo, error) {
	s.totpMu.Lock()
	defer s.totpMu.Unlock()

	st, err := s.store.LoadTOTPState(ctx)
	if err != nil {
		return nil, err
	}
	if st.Enabled && st.Secret != "" {
		return nil, errors.New("Two-factor authentication is already enabled. Verify and disable current 2FA before re-enrolling.")
	}
	secret, err := GenerateTOTPSecret()
	if err != nil {
		return nil, err
	}
	st.PendingSecret = secret
	if err := s.store.SaveTOTPState(ctx, st); err != nil {
		return nil, err
	}
	return &TOTPSetupInfo{
		Secret:     secret,
		OTPAuthURI: BuildOTPAuthURI(secret, accountName, "OVH Nexus"),
		Period:     TOTPPeriodSeconds,
		Digits:     TOTPDigits,
	}, nil
}

// ActivateTOTPSetup verifies the 6-digit code against pendingSecret, activates 2FA,
// and returns 8 single-use recovery codes.
func (s *Service) ActivateTOTPSetup(ctx context.Context, code string) (*TOTPActivationResult, error) {
	cleaned := digitsCleanRegex.ReplaceAllString(strings.TrimSpace(code), "")
	if !sixDigitsRegex.MatchString(cleaned) {
		return nil, errors.New("Please enter the 6-digit code shown in your authenticator app.")
	}

	s.totpMu.Lock()
	defer s.totpMu.Unlock()

	st, err := s.store.LoadTOTPState(ctx)
	if err != nil {
		return nil, err
	}
	if st.PendingSecret == "" {
		return nil, errors.New("No pending 2FA setup found. Generate a new setup QR code first.")
	}

	nowStep := s.nowFn().Unix() / TOTPPeriodSeconds
	var matchedStep *int64
	for _, offset := range []int64{0, -1, 1} {
		candStep := nowStep + offset
		candCode, err := ComputeTOTPAtStep(st.PendingSecret, candStep)
		if err == nil && constantTimeEqualString(cleaned, candCode) {
			stepCopy := candStep
			matchedStep = &stepCopy
			break
		}
	}
	if matchedStep == nil {
		return nil, errors.New("Verification failed. Check your authenticator clock synchronization and enter the latest 6-digit code.")
	}

	plainCodes, hashes, err := GenerateRecoveryCodes(RecoveryCodeCount)
	if err != nil {
		return nil, err
	}
	nowISO := s.nowFn().UTC().Format(time.RFC3339)
	st.Enabled = true
	st.Secret = st.PendingSecret
	st.PendingSecret = ""
	st.RecoveryCodeHashes = hashes
	st.LastUsedStep = *matchedStep
	st.EnabledAt = nowISO

	if err := s.store.SaveTOTPState(ctx, st); err != nil {
		return nil, err
	}

	return &TOTPActivationResult{
		Enabled:                true,
		RecoveryCodes:          plainCodes,
		RecoveryCodesRemaining: len(hashes),
		EnabledAt:              nowISO,
	}, nil
}

// VerifyTOTPOrRecovery validates a 6-digit TOTP code (with +-1 step tolerance and anti-replay)
// or an 8-character single-use recovery code.
func (s *Service) VerifyTOTPOrRecovery(ctx context.Context, code, actorKey string) (bool, string, string) {
	if actorKey == "" {
		actorKey = "global"
	}
	if allowed, retryAfter := s.totpLimiter.Check(actorKey); !allowed {
		return false, "TOTP_RATE_LIMITED", fmt.Sprintf("Too many failed 2FA attempts. Please wait %d seconds before retrying.", retryAfter)
	}

	raw := strings.TrimSpace(code)
	if raw == "" {
		return false, "INVALID_TOTP_CODE", "Please enter a 6-digit 2FA code or an 8-character recovery code."
	}

	s.totpMu.Lock()
	defer s.totpMu.Unlock()

	st, err := s.store.LoadTOTPState(ctx)
	if err != nil {
		return false, "TOTP_STATE_ERROR", "Failed to load 2FA state."
	}
	if !st.Enabled || st.Secret == "" {
		return false, "TOTP_NOT_ENABLED", "Two-factor authentication is not enabled."
	}

	digitsOnly := digitsCleanRegex.ReplaceAllString(raw, "")
	if sixDigitsRegex.MatchString(digitsOnly) {
		nowStep := s.nowFn().Unix() / TOTPPeriodSeconds
		for _, offset := range []int64{0, -1, 1} {
			candStep := nowStep + offset
			candCode, err := ComputeTOTPAtStep(st.Secret, candStep)
			if err != nil {
				continue
			}
			if constantTimeEqualString(digitsOnly, candCode) {
				if candStep <= st.LastUsedStep {
					return false, "TOTP_REPLAYED", "This 6-digit code was already used in the current 30-second period. Wait for the next code."
				}
				st.LastUsedStep = candStep
				if err := s.store.SaveTOTPState(ctx, st); err != nil {
					return false, "TOTP_STATE_ERROR", "Failed to persist anti-replay state."
				}
				s.totpLimiter.Reset(actorKey)
				return true, "TOTP_OK", "2FA verification succeeded."
			}
		}

		s.totpLimiter.RecordFailure(actorKey)
		return false, "INVALID_TOTP_CODE", "Invalid or expired 2FA code."
	}

	normRec := normalizeRecoveryCode(raw)
	if len(normRec) == 8 {
		matchedIdx := -1
		for idx, h := range st.RecoveryCodeHashes {
			if vault.VerifyTokenSHA256(normRec, h) {
				matchedIdx = idx
				break
			}
		}
		if matchedIdx >= 0 {
			nextHashes := make([]string, 0, len(st.RecoveryCodeHashes)-1)
			nextHashes = append(nextHashes, st.RecoveryCodeHashes[:matchedIdx]...)
			nextHashes = append(nextHashes, st.RecoveryCodeHashes[matchedIdx+1:]...)
			st.RecoveryCodeHashes = nextHashes
			if err := s.store.SaveTOTPState(ctx, st); err != nil {
				return false, "TOTP_STATE_ERROR", "Failed to update recovery code state."
			}
			s.totpLimiter.Reset(actorKey)
			return true, "RECOVERY_OK", fmt.Sprintf("Recovery code accepted (%d recovery codes remaining).", len(nextHashes))
		}
	}

	s.totpLimiter.RecordFailure(actorKey)
	return false, "INVALID_TOTP_CODE", "Invalid 2FA code or recovery code."
}

// DisableTOTP disables 2FA and wipes secrets and recovery codes.
func (s *Service) DisableTOTP(ctx context.Context) error {
	s.totpMu.Lock()
	defer s.totpMu.Unlock()

	return s.store.SaveTOTPState(ctx, &store.TOTPState{
		Enabled:            false,
		RecoveryCodeHashes: []string{},
	})
}

// RegenerateRecoveryCodes generates 8 fresh recovery codes while 2FA is active.
func (s *Service) RegenerateRecoveryCodes(ctx context.Context) ([]string, error) {
	s.totpMu.Lock()
	defer s.totpMu.Unlock()

	st, err := s.store.LoadTOTPState(ctx)
	if err != nil {
		return nil, err
	}
	if !st.Enabled || st.Secret == "" {
		return nil, errors.New("Two-factor authentication is not enabled; cannot regenerate recovery codes.")
	}
	plainCodes, hashes, err := GenerateRecoveryCodes(RecoveryCodeCount)
	if err != nil {
		return nil, err
	}
	st.RecoveryCodeHashes = hashes
	if err := s.store.SaveTOTPState(ctx, st); err != nil {
		return nil, err
	}
	return plainCodes, nil
}

// ==================== Cookie Signing & IP Extraction ====================

func (s *Service) signSession(p SessionPayload) (string, error) {
	raw, err := json.Marshal(p)
	if err != nil {
		return "", err
	}
	encoded := base64.RawURLEncoding.EncodeToString(raw)
	mac := hmac.New(sha256.New, s.sessionSecret)
	_, _ = mac.Write([]byte(encoded))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return encoded + "." + sig, nil
}

func (s *Service) parseSession(token string) (SessionPayload, bool) {
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return SessionPayload{}, false
	}
	encoded, sigStr := parts[0], parts[1]
	sigBytes, err := base64.RawURLEncoding.DecodeString(sigStr)
	if err != nil {
		return SessionPayload{}, false
	}

	mac := hmac.New(sha256.New, s.sessionSecret)
	_, _ = mac.Write([]byte(encoded))
	expected := mac.Sum(nil)
	if !hmac.Equal(sigBytes, expected) {
		return SessionPayload{}, false
	}

	raw, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return SessionPayload{}, false
	}
	var payload SessionPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return SessionPayload{}, false
	}
	if !payload.Authenticated || payload.ExpiresAt <= s.nowFn().Unix() {
		return SessionPayload{}, false
	}
	return payload, true
}

func (s *Service) readSessionCookie(r *http.Request) (SessionPayload, bool) {
	cookie, err := r.Cookie(SessionCookieName)
	if err != nil || cookie == nil || strings.TrimSpace(cookie.Value) == "" {
		return SessionPayload{}, false
	}
	return s.parseSession(cookie.Value)
}

func (s *Service) writeSessionCookie(w http.ResponseWriter, payload SessionPayload) {
	if payload.ExpiresAt == 0 {
		payload.ExpiresAt = s.nowFn().Add(SessionLifetimeHours * time.Hour).Unix()
	}
	val, err := s.signSession(payload)
	if err != nil {
		return
	}
	maxAge := int(payload.ExpiresAt - s.nowFn().Unix())
	if maxAge < 1 {
		maxAge = SessionLifetimeHours * 3600
	}
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    val,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   s.cfg.CookieSecure,
		MaxAge:   maxAge,
	})
}

func (s *Service) clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   s.cfg.CookieSecure,
		MaxAge:   -1,
	})
}

// ClientIP extracts the client IP address while honoring X-Forwarded-For / X-Real-IP
// ONLY when the direct peer belongs to TRUSTED_PROXIES.
func (s *Service) ClientIP(r *http.Request) string {
	remoteAddr, ok := parseRemoteAddr(r.RemoteAddr)
	if !ok {
		return "unknown"
	}
	if !s.cfg.IsTrustedProxy(remoteAddr) {
		return remoteAddr.String()
	}

	if xff := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); xff != "" {
		parts := strings.Split(xff, ",")
		for i := len(parts) - 1; i >= 0; i-- {
			cand, err := netip.ParseAddr(strings.TrimSpace(parts[i]))
			if err != nil {
				continue
			}
			cand = cand.Unmap()
			if !s.cfg.IsTrustedProxy(cand) {
				return cand.String()
			}
		}
		for _, part := range parts {
			if cand, err := netip.ParseAddr(strings.TrimSpace(part)); err == nil {
				return cand.Unmap().String()
			}
		}
	}

	if xri := strings.TrimSpace(r.Header.Get("X-Real-IP")); xri != "" {
		if cand, err := netip.ParseAddr(xri); err == nil {
			return cand.Unmap().String()
		}
	}

	return remoteAddr.String()
}

func (s *Service) isDirectLoopback(r *http.Request) bool {
	addr, ok := parseRemoteAddr(r.RemoteAddr)
	if !ok || !addr.IsLoopback() {
		return false
	}
	if strings.TrimSpace(r.Header.Get("X-Forwarded-For")) != "" || strings.TrimSpace(r.Header.Get("X-Real-IP")) != "" {
		return false
	}
	return true
}

func parseRemoteAddr(remote string) (netip.Addr, bool) {
	trimmed := strings.TrimSpace(remote)
	if trimmed == "" {
		return netip.Addr{}, false
	}
	host, _, err := net.SplitHostPort(trimmed)
	if err != nil {
		host = trimmed
	}
	addr, err := netip.ParseAddr(strings.TrimSpace(host))
	if err != nil {
		return netip.Addr{}, false
	}
	return addr.Unmap(), true
}

func extractRequestTOTPCode(r *http.Request, body map[string]any) string {
	if hdr := strings.TrimSpace(r.Header.Get("X-TOTP-Code")); hdr != "" {
		return hdr
	}
	if body != nil {
		for _, key := range []string{"totpCode", "totp_code"} {
			if s, ok := body[key].(string); ok && strings.TrimSpace(s) != "" {
				return strings.TrimSpace(s)
			}
		}
	}
	return ""
}

func writeAuthJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
