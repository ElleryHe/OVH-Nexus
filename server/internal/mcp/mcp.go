package mcp

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"ovh-nexus/server/internal/auth"
	"ovh-nexus/server/internal/config"
	"ovh-nexus/server/internal/logger"
	"ovh-nexus/server/internal/store"
	"ovh-nexus/server/internal/vault"
)

const (
	MCPProtocolVersion = "2025-03-26"
	MCPServerName      = "ovh-nexus-mcp"
	MCPServerVersion   = "1.3.1"
	maxMCPBodyBytes    = 2 << 20
)

var supportedProtocolVersions = map[string]struct{}{
	"2024-11-05": {},
	"2025-03-26": {},
	"2025-06-18": {},
}

// Service implements the Privacy-Shielded MCP Streamable HTTP Gateway
// and WebUI Management APIs.
type Service struct {
	deps   Dependencies
	cfg    *config.Config
	store  *store.Store
	auth   *auth.Service
	log    *logger.Hub
	shield *PrivacyShield

	nowFn        func() time.Time
	quotes       *quoteStore
	orderLimiter *orderRateLimiter
	authLimiter  *authAttemptLimiter

	cfgMu       sync.Mutex
	checkoutMu  sync.Mutex
	checkoutMap map[string]*sync.Mutex

	clientForAccountFn func(ctx context.Context, accountID string) (OVHClient, *store.OVHAccount, error)
}

// NewService creates a new MCP Gateway service.
func NewService(deps Dependencies) (*Service, error) {
	if deps.Store == nil {
		return nil, errors.New("mcp: store must not be nil")
	}
	shield, err := NewPrivacyShield(deps.Store)
	if err != nil {
		return nil, fmt.Errorf("mcp: init privacy shield: %w", err)
	}

	nowFn := func() time.Time { return time.Now().UTC() }
	s := &Service{
		deps:         deps,
		cfg:          deps.Config,
		store:        deps.Store,
		auth:         deps.Auth,
		log:          deps.Log,
		shield:       shield,
		nowFn:        nowFn,
		quotes:       newQuoteStore(nowFn),
		orderLimiter: newOrderRateLimiter(nowFn),
		authLimiter:  newAuthAttemptLimiter(MCPAuthMaxAttempts, MCPAuthWindowSeconds*time.Second, nowFn),
		checkoutMap:  make(map[string]*sync.Mutex),
	}
	s.clientForAccountFn = func(ctx context.Context, accountID string) (OVHClient, *store.OVHAccount, error) {
		if deps.OVHPool == nil {
			return nil, nil, errors.New("OVH client pool is not configured")
		}
		c, acc, err := deps.OVHPool.ClientForAccount(ctx, accountID)
		if err != nil {
			return nil, acc, err
		}
		return c, acc, nil
	}
	return s, nil
}

// SetNowFunc overrides the internal clock (intended for deterministic unit tests).
func (s *Service) SetNowFunc(fn func() time.Time) {
	if fn == nil {
		return
	}
	s.nowFn = fn
	s.quotes.nowFn = fn
	s.orderLimiter.nowFn = fn
	s.authLimiter.nowFn = fn
}

// Shield exposes the bidirectional Privacy Shield instance.
func (s *Service) Shield() *PrivacyShield {
	return s.shield
}

// MountRoutes registers both the Streamable HTTP endpoint (/api/mcp) and WebUI management endpoints (/api/mcp/*).
func (s *Service) MountRoutes(r chi.Router) {
	s.MountMCPRoutes(r)
	s.MountAdminRoutes(r)
}

// MountMCPRoutes registers the Streamable HTTP JSON-RPC 2.0 endpoint (/mcp on an /api router).
func (s *Service) MountMCPRoutes(r chi.Router) {
	r.HandleFunc("/mcp", s.HandleMCPStreamableHTTP)
}

// MountAdminRoutes registers WebUI MCP Gateway management endpoints onto an /api chi.Router.
func (s *Service) MountAdminRoutes(r chi.Router) {
	r.Get("/mcp/settings", s.HandleGetSettings)
	r.Put("/mcp/settings", s.HandleUpdateSettings)
	r.Post("/mcp/settings", s.HandleUpdateSettings)

	r.Get("/mcp/config", s.HandleGetSettings)
	r.Put("/mcp/config", s.HandleUpdateSettings)
	r.Post("/mcp/config", s.HandleUpdateSettings)

	r.Post("/mcp/token/rotate", s.HandleRotateToken)
	r.Post("/mcp/token/revoke", s.HandleRevokeToken)

	r.Get("/mcp/aliases", s.HandleGetAliases)
	r.Delete("/mcp/aliases", s.HandleClearAliases)

	r.Get("/mcp/audit", s.HandleGetAudit)
	r.Delete("/mcp/audit", s.HandleClearAudit)
}

// ==================== Config, TOTP Hard Lock & Token Helpers ====================

func (s *Service) isTOTPEnabled(ctx context.Context) (bool, error) {
	if s.deps.TOTP != nil {
		return s.deps.TOTP.IsTOTPEnabled(ctx)
	}
	if s.auth != nil {
		return s.auth.IsTOTPEnabled(ctx)
	}
	// Fail closed if no TOTP checker is wired.
	return false, nil
}

func (s *Service) loadEffectiveConfig(ctx context.Context) (*store.MCPConfig, bool, error) {
	cfg, err := s.store.LoadMCPConfig(ctx)
	if err != nil {
		return nil, false, err
	}
	totpEnabled, totpErr := s.isTOTPEnabled(ctx)
	if totpErr != nil {
		totpEnabled = false
	}
	// Hard runtime security lock: L3 operations MUST evaluate to false when 2FA is disabled.
	if !totpEnabled {
		cfg.Scopes.Operations = false
	}
	return cfg, totpEnabled, nil
}

func (s *Service) buildPublicConfig(cfg *store.MCPConfig, totpEnabled bool, includeCatalog bool) map[string]any {
	hasToken := strings.TrimSpace(cfg.TokenHash) != ""
	var rotatedAt *string
	if strings.TrimSpace(cfg.TokenRotatedAt) != "" {
		v := cfg.TokenRotatedAt
		rotatedAt = &v
	}
	var updatedAt *string
	if strings.TrimSpace(cfg.UpdatedAt) != "" {
		v := cfg.UpdatedAt
		updatedAt = &v
	}
	masked := cfg.MaskedToken
	if !hasToken {
		masked = ""
	}
	allowed := cfg.AllowedAccountIDs
	if allowed == nil {
		allowed = []string{}
	}
	scopes := cfg.Scopes
	if !totpEnabled {
		scopes.Operations = false
	}

	out := map[string]any{
		"enabled":                cfg.Enabled && hasToken,
		"hasToken":               hasToken,
		"maskedToken":            masked,
		"scopes":                 scopes,
		"totpEnabled":            totpEnabled,
		"operationsLockedByTotp": !totpEnabled,
		"allowAllAccounts":       cfg.AllowAllAccounts,
		"allowedAccountIds":      allowed,
		"hourlyOrderLimit":       cfg.HourlyOrderLimit,
		"notifyOnMutation":       cfg.NotifyOnMutation,
		"tokenRotatedAt":         rotatedAt,
		"updatedAt":              updatedAt,
		"hardBlocked": []string{
			"auto_pay",
			"waive_retractation_period",
			"baremetal_reinstall",
			"vps_reinstall",
			"service_terminate",
			"ipmi_raw_console",
			"credentials_mutation",
		},
	}
	if includeCatalog {
		out["toolCatalog"] = AllToolCatalog()
	}
	return out
}

func mintMCPToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("failed to generate MCP token: %w", err)
	}
	return "nxs_mcp_" + base64.RawURLEncoding.EncodeToString(buf), nil
}

func maskMCPToken(token string) string {
	s := strings.TrimSpace(token)
	if s == "" {
		return ""
	}
	if len(s) <= 14 {
		return "****"
	}
	return s[:10] + "****" + s[len(s)-4:]
}

func (s *Service) registerSensitiveToken(tok string) {
	if s.log != nil && strings.TrimSpace(tok) != "" {
		s.log.AddSensitiveValue(tok)
	}
}

func (s *Service) verifyBearerToken(ctx context.Context, candidate string) bool {
	if strings.TrimSpace(candidate) == "" {
		return false
	}
	cfg, _, err := s.loadEffectiveConfig(ctx)
	if err != nil || !cfg.Enabled || strings.TrimSpace(cfg.TokenHash) == "" {
		return false
	}
	clean := strings.TrimSpace(candidate)
	if len(clean) >= 7 && strings.EqualFold(clean[:7], "bearer ") {
		clean = strings.TrimSpace(clean[7:])
	}
	if len(clean) < 20 || !strings.HasPrefix(clean, "nxs_mcp_") {
		return false
	}
	return vault.VerifyTokenSHA256(clean, cfg.TokenHash)
}

func (s *Service) clientIP(r *http.Request) string {
	if s.auth != nil {
		return s.auth.ClientIP(r)
	}
	if r == nil || strings.TrimSpace(r.RemoteAddr) == "" {
		return "unknown"
	}
	host := r.RemoteAddr
	if idx := strings.LastIndex(host, ":"); idx > 0 && !strings.Contains(host[idx:], "]") {
		host = host[:idx]
	}
	return strings.Trim(host, "[]")
}

func (s *Service) checkoutMutexForAccount(accountID string) *sync.Mutex {
	s.checkoutMu.Lock()
	defer s.checkoutMu.Unlock()
	if s.checkoutMap == nil {
		s.checkoutMap = make(map[string]*sync.Mutex)
	}
	mu, ok := s.checkoutMap[accountID]
	if !ok {
		mu = &sync.Mutex{}
		s.checkoutMap[accountID] = mu
	}
	return mu
}

// ==================== Streamable HTTP JSON-RPC 2.0 Endpoint (/api/mcp) ====================

// HandleMCPStreamableHTTP handles POST|GET|DELETE|OPTIONS /api/mcp.
func (s *Service) HandleMCPStreamableHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		writeJSON(w, http.StatusOK, map[string]any{})
		return
	}

	ctx := r.Context()
	cfg, _, err := s.loadEffectiveConfig(ctx)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, jsonRPCError(nil, -32603, "Failed to load MCP Gateway configuration", nil))
		return
	}
	if !cfg.Enabled || strings.TrimSpace(cfg.TokenHash) == "" {
		writeJSON(w, http.StatusForbidden, jsonRPCError(nil, -32003, "MCP Gateway is disabled. Enable it and generate a Bearer token in the OVH Nexus Web Console (/mcp).", nil))
		return
	}

	ip := s.clientIP(r)
	if allowed, retryAfter := s.authLimiter.check(ip); !allowed {
		if s.log != nil {
			s.log.Warn(logger.SourceMCP, "MCP authentication rate limit exceeded", "ip", ip, "retryAfter", retryAfter)
		}
		w.Header().Set("Retry-After", strconv.Itoa(retryAfter))
		writeJSON(w, http.StatusTooManyRequests, jsonRPCError(nil, -32029, fmt.Sprintf("Too many failed MCP authentication attempts. Retry in %ds.", retryAfter), nil))
		return
	}

	candidate := extractMCPBearerToken(r)
	if !s.verifyBearerToken(ctx, candidate) {
		s.authLimiter.recordFailure(ip)
		if s.log != nil {
			s.log.Warn(logger.SourceMCP, "Rejected unauthorized MCP request", "ip", ip)
		}
		writeJSON(w, http.StatusUnauthorized, jsonRPCError(nil, -32001, "Unauthorized: valid `Authorization: Bearer <MCP_TOKEN>` header required.", nil))
		return
	}
	s.authLimiter.reset(ip)

	if r.Method == http.MethodGet || r.Method == http.MethodDelete {
		writeJSON(w, http.StatusMethodNotAllowed, jsonRPCError(nil, -32600, "Stateless Streamable HTTP endpoint accepts POST requests.", nil))
		return
	}
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, jsonRPCError(nil, -32600, "Method not allowed.", nil))
		return
	}

	bodyBytes, err := io.ReadAll(io.LimitReader(r.Body, maxMCPBodyBytes))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, jsonRPCError(nil, -32700, "Parse error: failed to read request body", nil))
		return
	}
	trimmed := bytes.TrimSpace(bodyBytes)
	if len(trimmed) == 0 {
		writeJSON(w, http.StatusBadRequest, jsonRPCError(nil, -32700, "Parse error: invalid JSON body", nil))
		return
	}

	dec := json.NewDecoder(bytes.NewReader(trimmed))
	dec.UseNumber()

	var payload any
	if err := dec.Decode(&payload); err != nil {
		writeJSON(w, http.StatusBadRequest, jsonRPCError(nil, -32700, "Parse error: invalid JSON body", nil))
		return
	}

	if batch, isBatch := payload.([]any); isBatch {
		if len(batch) == 0 {
			writeJSON(w, http.StatusBadRequest, jsonRPCError(nil, -32600, "Empty JSON-RPC batch", nil))
			return
		}
		responses := make([]map[string]any, 0, len(batch))
		for _, item := range batch {
			msgMap, ok := item.(map[string]any)
			if !ok {
				responses = append(responses, jsonRPCError(nil, -32600, "Invalid JSON-RPC 2.0 request", nil))
				continue
			}
			if resp := s.handleSingleJSONRPC(ctx, msgMap, ip); resp != nil {
				responses = append(responses, resp)
			}
		}
		if len(responses) == 0 {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		writeJSON(w, http.StatusOK, responses)
		return
	}

	msgMap, ok := payload.(map[string]any)
	if !ok {
		writeJSON(w, http.StatusBadRequest, jsonRPCError(nil, -32600, "Invalid JSON-RPC 2.0 request", nil))
		return
	}
	resp := s.handleSingleJSONRPC(ctx, msgMap, ip)
	if resp == nil {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Service) handleSingleJSONRPC(ctx context.Context, msg map[string]any, clientIP string) map[string]any {
	reqID, hasID := msg["id"]
	if asString(msg["jsonrpc"]) != "2.0" {
		return jsonRPCError(reqID, -32600, "Invalid JSON-RPC 2.0 request", nil)
	}

	method := strings.TrimSpace(asString(msg["method"]))
	params := asMap(msg["params"])
	isNotification := !hasID

	switch method {
	case "initialize":
		clientProto := strings.TrimSpace(asString(params["protocolVersion"]))
		if clientProto == "" {
			clientProto = MCPProtocolVersion
		}
		negotiated := MCPProtocolVersion
		if _, ok := supportedProtocolVersions[clientProto]; ok {
			negotiated = clientProto
		}
		return jsonRPCResponse(reqID, map[string]any{
			"protocolVersion": negotiated,
			"capabilities": map[string]any{
				"tools": map[string]any{"listChanged": false},
			},
			"serverInfo": map[string]any{
				"name":    MCPServerName,
				"version": MCPServerVersion,
			},
			"instructions": "OVH Nexus MCP Gateway (Privacy-Shielded & No-Auto-Pay Enforced). " +
				"All server hostnames, IPs, accounts, and orders use persistent aliases (acct-01, bm-01, vps-01, ip-01, ord-01). " +
				"Pass these aliases directly to tool parameters. Direct ordering requires calling `order_quote` first, " +
				"confirming the price with the user, and then calling `order_create_unpaid` (which never charges any payment method).",
		})

	case "ping":
		if isNotification {
			return nil
		}
		return jsonRPCResponse(reqID, map[string]any{})

	case "resources/list":
		if isNotification {
			return nil
		}
		return jsonRPCResponse(reqID, map[string]any{"resources": []any{}})

	case "prompts/list":
		if isNotification {
			return nil
		}
		return jsonRPCResponse(reqID, map[string]any{"prompts": []any{}})

	case "tools/list":
		if isNotification {
			return nil
		}
		tools, err := s.enabledToolsForMCP(ctx)
		if err != nil {
			return jsonRPCError(reqID, -32603, "Failed to enumerate MCP tools", nil)
		}
		return jsonRPCResponse(reqID, map[string]any{"tools": tools})

	case "tools/call":
		toolName := strings.TrimSpace(asString(params["name"]))
		if toolName == "" {
			return jsonRPCError(reqID, -32602, "Missing tool `name` in tools/call", nil)
		}
		toolArgs := asMap(params["arguments"])
		cfg, _, _ := s.loadEffectiveConfig(ctx)
		notifyEnabled := cfg != nil && cfg.NotifyOnMutation

		resultData, scopeName, summary, err := s.executeTool(ctx, toolName, toolArgs)
		if err != nil {
			var permErr *PermissionError
			if errors.As(err, &permErr) {
				errMsg := s.shield.ScrubText(permErr.Message)
				s.recordAudit(ctx, toolName, "blocked", toolArgs, "blocked", errMsg, clientIP, false)
				errJSON, _ := json.Marshal(map[string]any{
					"error":   "PERMISSION_DENIED",
					"message": errMsg,
				})
				return jsonRPCResponse(reqID, map[string]any{
					"content": []map[string]any{{"type": "text", "text": string(errJSON)}},
					"isError": true,
				})
			}

			errMsg := s.shield.ScrubText(err.Error())
			s.recordAudit(ctx, toolName, "error", toolArgs, "error", errMsg, clientIP, false)
			errJSON, _ := json.Marshal(map[string]any{
				"error":   "TOOL_EXECUTION_ERROR",
				"message": errMsg,
			})
			return jsonRPCResponse(reqID, map[string]any{
				"content": []map[string]any{{"type": "text", "text": string(errJSON)}},
				"isError": true,
			})
		}

		rawJSON, _ := json.MarshalIndent(resultData, "", "  ")
		safeText := s.shield.ScrubText(string(rawJSON))
		safeSummary := s.shield.ScrubText(summary)
		s.recordAudit(ctx, toolName, scopeName, toolArgs, "success", safeSummary, clientIP, notifyEnabled)

		return jsonRPCResponse(reqID, map[string]any{
			"content": []map[string]any{{"type": "text", "text": safeText}},
			"isError": false,
		})
	}

	if strings.HasPrefix(method, "notifications/") || isNotification {
		return nil
	}
	return jsonRPCError(reqID, -32601, "Method not found: "+method, nil)
}

func extractMCPBearerToken(r *http.Request) string {
	authHdr := strings.TrimSpace(r.Header.Get("Authorization"))
	if len(authHdr) >= 7 && strings.EqualFold(authHdr[:7], "bearer ") {
		return strings.TrimSpace(authHdr[7:])
	}
	return strings.TrimSpace(r.Header.Get("X-MCP-Token"))
}

func jsonRPCResponse(reqID any, result any) map[string]any {
	return map[string]any{
		"jsonrpc": "2.0",
		"id":      reqID,
		"result":  result,
	}
}

func jsonRPCError(reqID any, code int, message string, data any) map[string]any {
	errObj := map[string]any{
		"code":    code,
		"message": message,
	}
	if data != nil {
		errObj["data"] = data
	}
	return map[string]any{
		"jsonrpc": "2.0",
		"id":      reqID,
		"error":   errObj,
	}
}

// ==================== WebUI Management API (/api/mcp/*) ====================

// HandleGetSettings handles GET /api/mcp/settings and GET /api/mcp/config.
func (s *Service) HandleGetSettings(w http.ResponseWriter, r *http.Request) {
	cfg, totpEnabled, err := s.loadEffectiveConfig(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"status":  "error",
			"success": false,
			"error":   "Failed to load MCP Gateway settings",
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status": "success",
		"data":   s.buildPublicConfig(cfg, totpEnabled, true),
	})
}

// HandleUpdateSettings handles PUT|POST /api/mcp/settings and PUT|POST /api/mcp/config.
func (s *Service) HandleUpdateSettings(w http.ResponseWriter, r *http.Request) {
	var patch struct {
		Enabled *bool `json:"enabled"`
		Scopes  *struct {
			Read        *bool `json:"read"`
			Procurement *bool `json:"procurement"`
			Operations  *bool `json:"operations"`
		} `json:"scopes"`
		AllowAllAccounts  *bool     `json:"allowAllAccounts"`
		AllowedAccountIDs *[]string `json:"allowedAccountIds"`
		HourlyOrderLimit  *int      `json:"hourlyOrderLimit"`
		NotifyOnMutation  *bool     `json:"notifyOnMutation"`
	}
	if err := decodeJSONBody(r, &patch); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"status":  "error",
			"success": false,
			"error":   "Invalid JSON payload",
		})
		return
	}

	ctx := r.Context()
	s.cfgMu.Lock()
	defer s.cfgMu.Unlock()

	cfg, totpEnabled, err := s.loadEffectiveConfig(ctx)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"status":  "error",
			"success": false,
			"error":   "Failed to load MCP Gateway configuration",
		})
		return
	}

	// Hard 2FA security lock: reject attempts to enable L3 operations when TOTP is disabled.
	if patch.Scopes != nil && patch.Scopes.Operations != nil && *patch.Scopes.Operations && !totpEnabled {
		if s.log != nil {
			s.log.Warn(logger.SourceMCP, "Rejected L3 MCP operations enable attempt without active 2FA")
		}
		writeJSON(w, http.StatusForbidden, map[string]any{
			"status":  "error",
			"success": false,
			"error":   "2FA Setup Required",
			"code":    "TOTP_SETUP_REQUIRED",
			"message": "Two-Factor Authentication (2FA) must be enabled before activating L3 operations scope.",
		})
		return
	}

	var newlyMintedToken string
	if patch.Enabled != nil {
		wantEnable := *patch.Enabled
		if wantEnable && strings.TrimSpace(cfg.TokenHash) == "" {
			tok, mintErr := mintMCPToken()
			if mintErr != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]any{
					"status":  "error",
					"success": false,
					"error":   mintErr.Error(),
				})
				return
			}
			newlyMintedToken = tok
			cfg.TokenHash = vault.HashTokenSHA256(tok)
			cfg.MaskedToken = maskMCPToken(tok)
			cfg.TokenRotatedAt = s.nowFn().UTC().Format(time.RFC3339)
			s.registerSensitiveToken(tok)
		}
		cfg.Enabled = wantEnable
	}

	if patch.Scopes != nil {
		if patch.Scopes.Read != nil {
			cfg.Scopes.Read = *patch.Scopes.Read
		}
		if patch.Scopes.Procurement != nil {
			cfg.Scopes.Procurement = *patch.Scopes.Procurement
		}
		if patch.Scopes.Operations != nil {
			cfg.Scopes.Operations = *patch.Scopes.Operations && totpEnabled
		}
	}
	if !totpEnabled {
		cfg.Scopes.Operations = false
	}

	if patch.AllowAllAccounts != nil {
		cfg.AllowAllAccounts = *patch.AllowAllAccounts
	}
	if patch.AllowedAccountIDs != nil {
		cleaned := make([]string, 0, len(*patch.AllowedAccountIDs))
		for _, id := range *patch.AllowedAccountIDs {
			if trimmed := strings.TrimSpace(id); trimmed != "" {
				cleaned = append(cleaned, trimmed)
			}
		}
		cfg.AllowedAccountIDs = cleaned
	}
	if patch.HourlyOrderLimit != nil {
		limit := *patch.HourlyOrderLimit
		if limit < 1 {
			limit = 1
		} else if limit > 20 {
			limit = 20
		}
		cfg.HourlyOrderLimit = limit
	}
	if patch.NotifyOnMutation != nil {
		cfg.NotifyOnMutation = *patch.NotifyOnMutation
	}

	if err := s.store.SaveMCPConfig(ctx, cfg); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"status":  "error",
			"success": false,
			"error":   "Failed to save MCP Gateway configuration",
		})
		return
	}

	if s.log != nil {
		s.log.Info(logger.SourceMCP, "Updated MCP Gateway configuration",
			"enabled", cfg.Enabled,
			"read", cfg.Scopes.Read,
			"procurement", cfg.Scopes.Procurement,
			"operations", cfg.Scopes.Operations,
			"hourlyOrderLimit", cfg.HourlyOrderLimit,
		)
	}

	pub := s.buildPublicConfig(cfg, totpEnabled, false)
	if newlyMintedToken != "" {
		pub["revealedToken"] = newlyMintedToken
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status": "success",
		"data":   pub,
	})
}

// HandleRotateToken handles POST /api/mcp/token/rotate.
func (s *Service) HandleRotateToken(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	s.cfgMu.Lock()
	defer s.cfgMu.Unlock()

	cfg, totpEnabled, err := s.loadEffectiveConfig(ctx)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"status": "error", "error": "Failed to load MCP configuration"})
		return
	}

	newTok, err := mintMCPToken()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"status": "error", "error": err.Error()})
		return
	}

	cfg.TokenHash = vault.HashTokenSHA256(newTok)
	cfg.MaskedToken = maskMCPToken(newTok)
	cfg.TokenRotatedAt = s.nowFn().UTC().Format(time.RFC3339)

	if err := s.store.SaveMCPConfig(ctx, cfg); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"status": "error", "error": "Failed to persist rotated MCP token"})
		return
	}
	s.registerSensitiveToken(newTok)

	if s.log != nil {
		s.log.Info(logger.SourceMCP, "Rotated MCP Gateway Bearer token")
	}

	pub := s.buildPublicConfig(cfg, totpEnabled, false)
	pub["revealedToken"] = newTok
	writeJSON(w, http.StatusOK, map[string]any{
		"status": "success",
		"data":   pub,
	})
}

// HandleRevokeToken handles POST /api/mcp/token/revoke.
func (s *Service) HandleRevokeToken(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	s.cfgMu.Lock()
	defer s.cfgMu.Unlock()

	cfg, totpEnabled, err := s.loadEffectiveConfig(ctx)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"status": "error", "error": "Failed to load MCP configuration"})
		return
	}

	cfg.TokenHash = ""
	cfg.MaskedToken = ""
	cfg.Enabled = false
	cfg.TokenRotatedAt = s.nowFn().UTC().Format(time.RFC3339)

	if err := s.store.SaveMCPConfig(ctx, cfg); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"status": "error", "error": "Failed to revoke MCP token"})
		return
	}

	if s.log != nil {
		s.log.Warn(logger.SourceMCP, "Revoked MCP Gateway Bearer token and disabled gateway")
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status": "success",
		"data":   s.buildPublicConfig(cfg, totpEnabled, false),
	})
}

// HandleGetAliases handles GET /api/mcp/aliases.
func (s *Service) HandleGetAliases(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	accounts, err := s.store.ListAccounts(ctx)
	if err == nil {
		for _, acc := range accounts {
			_, _, _ = s.shield.RedactAccount(ctx, acc, acc.IsDefault)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status": "success",
		"data":   s.shield.ListMappingsForUI(),
	})
}

// HandleClearAliases handles DELETE /api/mcp/aliases.
func (s *Service) HandleClearAliases(w http.ResponseWriter, r *http.Request) {
	if err := s.shield.ClearAll(r.Context()); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"status": "error", "error": "Failed to clear Privacy Shield aliases"})
		return
	}
	if s.log != nil {
		s.log.Info(logger.SourceMCP, "Cleared all MCP Privacy Shield alias mappings")
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status": "success",
		"data":   []any{},
	})
}

// HandleGetAudit handles GET /api/mcp/audit.
func (s *Service) HandleGetAudit(w http.ResponseWriter, r *http.Request) {
	limit := 100
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil {
			if parsed < 1 {
				parsed = 1
			} else if parsed > 300 {
				parsed = 300
			}
			limit = parsed
		}
	}
	logs, err := s.store.ListMCPAudit(r.Context(), limit)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"status": "error", "error": "Failed to load MCP audit logs"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status": "success",
		"data":   logs,
	})
}

// HandleClearAudit handles DELETE /api/mcp/audit.
func (s *Service) HandleClearAudit(w http.ResponseWriter, r *http.Request) {
	if err := s.store.ClearMCPAudit(r.Context()); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"status": "error", "error": "Failed to clear MCP audit logs"})
		return
	}
	if s.log != nil {
		s.log.Info(logger.SourceMCP, "Cleared MCP audit logs")
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status": "success",
		"data":   []any{},
	})
}

// ==================== Utility Helpers ====================

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func decodeJSONBody(r *http.Request, dst any) error {
	if r == nil || r.Body == nil {
		return nil
	}
	data, err := io.ReadAll(io.LimitReader(r.Body, maxMCPBodyBytes))
	if err != nil {
		return err
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return nil
	}
	return json.Unmarshal(data, dst)
}

func asString(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case json.Number:
		return t.String()
	case fmt.Stringer:
		return t.String()
	default:
		return fmt.Sprintf("%v", t)
	}
}

func asBool(v any) bool {
	switch t := v.(type) {
	case bool:
		return t
	case string:
		low := strings.ToLower(strings.TrimSpace(t))
		return low == "true" || low == "1" || low == "yes"
	case float64:
		return t != 0
	case int:
		return t != 0
	case int64:
		return t != 0
	case json.Number:
		if i, err := t.Int64(); err == nil {
			return i != 0
		}
		if f, err := t.Float64(); err == nil {
			return f != 0
		}
		return false
	default:
		return false
	}
}

func asInt(v any, def int) int {
	switch t := v.(type) {
	case nil:
		return def
	case int:
		return t
	case int64:
		return int(t)
	case float64:
		return int(t)
	case json.Number:
		if i, err := t.Int64(); err == nil {
			return int(i)
		}
		if f, err := t.Float64(); err == nil {
			return int(f)
		}
		return def
	case string:
		if i, err := strconv.Atoi(strings.TrimSpace(t)); err == nil {
			return i
		}
		return def
	default:
		return def
	}
}

func asMap(v any) map[string]any {
	if m, ok := v.(map[string]any); ok && m != nil {
		return m
	}
	return map[string]any{}
}
