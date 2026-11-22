package logger

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-chi/chi/v5"

	"ovh-nexus/server/internal/store"
)

// Canonical English log sources for OVH Nexus v1.3.0.
const (
	SourceServer       = "server"
	SourceHTTP         = "http"
	SourceAuth         = "auth"
	SourceTOTP         = "totp"
	SourceVault        = "vault"
	SourceOVH          = "ovh"
	SourceCatalog      = "catalog"
	SourceQueue        = "queue"
	SourceStockMonitor = "stock-monitor"
	SourceVPSMonitor   = "vps-monitor"
	SourceSKUWatcher   = "sku-watcher"
	SourceFleet        = "fleet"
	SourceNotify       = "notify"
	SourceMCP          = "mcp"
)

// AllSources lists the 14 standardized module sources.
func AllSources() []string {
	return []string{
		SourceServer,
		SourceHTTP,
		SourceAuth,
		SourceTOTP,
		SourceVault,
		SourceOVH,
		SourceCatalog,
		SourceQueue,
		SourceStockMonitor,
		SourceVPSMonitor,
		SourceSKUWatcher,
		SourceFleet,
		SourceNotify,
		SourceMCP,
	}
}

var (
	mcpTokenRegex   = regexp.MustCompile(`nxs_mcp_[A-Za-z0-9_-]{8,}`)
	encEnvRegex     = regexp.MustCompile(`enc:v1:[A-Za-z0-9_=-]+:[A-Za-z0-9_=-]+`)
	bearerRegex     = regexp.MustCompile(`(?i)(Bearer\s+)[A-Za-z0-9._~+/=-]{8,}`)
	kvSecretRegex   = regexp.MustCompile(`(?i)((?:password|passwd|secret|token|appKey|appSecret|consumerKey|apiKey|authorization)\s*[=:]\s*)([^\s,;'"&]+)`)
	telegramBotReg  = regexp.MustCompile(`bot\d+:[A-Za-z0-9_-]{16,}`)
	sensitiveTokens = []string{
		"secret", "password", "passwd", "token", "apikey", "api_key",
		"appkey", "app_key", "appsecret", "app_secret",
		"consumerkey", "consumer_key", "authorization", "cookie",
		"totp", "recovery", "private_key", "privatekey", "credential", "encryption",
	}
)

// LogEntry represents an English structured log event returned by REST and SSE endpoints.
type LogEntry struct {
	ID        string         `json:"id"`
	Timestamp string         `json:"timestamp"`
	Level     string         `json:"level"`
	Message   string         `json:"message"`
	Source    string         `json:"source"`
	Attrs     map[string]any `json:"attrs,omitempty"`
}

const (
	// DefaultMaxSubscribers caps concurrent SSE log stream subscribers to prevent memory/FD exhaustion.
	DefaultMaxSubscribers = 32
	// DefaultSSEWriteTimeout bounds each SSE frame/keepalive write+flush to detect half-open clients promptly.
	DefaultSSEWriteTimeout = 10 * time.Second
)

// Options configures a logger Hub.
type Options struct {
	Level          string
	Format         string // "text" or "json"
	Output         io.Writer
	BufferSize     int
	MaxSubscribers int
}

// Hub is a thread-safe slog.Handler that writes redacted UTC RFC3339 entries to stdout,
// persists them into SQLite system_logs, and fans them out to live SSE subscribers.
type Hub struct {
	store      *store.Store
	out        io.Writer
	minLevel   slog.Level
	jsonFormat bool
	bufSize    int
	maxSubs    int

	writeMu sync.Mutex
	subMu   sync.RWMutex
	subs    map[uint64]chan LogEntry
	nextSub uint64
	closed  bool

	secretMu sync.RWMutex
	secrets  []string

	fallbackSeq atomic.Int64
}

// NewHub creates a new structured logging hub.
func NewHub(st *store.Store, opts Options) *Hub {
	out := opts.Output
	if out == nil {
		out = os.Stdout
	}
	bufSize := opts.BufferSize
	if bufSize <= 0 {
		bufSize = 128
	}
	maxSubs := opts.MaxSubscribers
	if maxSubs <= 0 {
		maxSubs = DefaultMaxSubscribers
	}
	return &Hub{
		store:      st,
		out:        out,
		minLevel:   parseSlogLevel(opts.Level),
		jsonFormat: strings.EqualFold(strings.TrimSpace(opts.Format), "json"),
		bufSize:    bufSize,
		maxSubs:    maxSubs,
		subs:       make(map[uint64]chan LogEntry),
	}
}

// AddSensitiveValue registers a runtime secret (and its URL-encoded representation) so it is
// automatically scrubbed if it ever appears in a log message or attribute.
func (h *Hub) AddSensitiveValue(val string) {
	trimmed := strings.TrimSpace(val)
	if len(trimmed) < 4 {
		return
	}
	candidates := []string{trimmed}
	if escaped := url.QueryEscape(trimmed); escaped != trimmed && len(escaped) >= 4 {
		candidates = append(candidates, escaped)
	}
	h.secretMu.Lock()
	defer h.secretMu.Unlock()
	for _, cand := range candidates {
		dup := false
		for _, existing := range h.secrets {
			if existing == cand {
				dup = true
				break
			}
		}
		if !dup {
			h.secrets = append(h.secrets, cand)
		}
	}
}

// Slog returns a *slog.Logger backed by this Hub.
func (h *Hub) Slog() *slog.Logger {
	return slog.New(h.withSource(SourceServer))
}

// WithSource returns a *slog.Logger pre-tagged with the given canonical source module.
func (h *Hub) WithSource(source string) *slog.Logger {
	return slog.New(h.withSource(source))
}

func (h *Hub) withSource(source string) slog.Handler {
	return &hubHandler{
		hub:    h,
		source: normalizeSource(source),
	}
}

// Info logs an INFO entry with the given source and optional key/value attrs.
func (h *Hub) Info(source, msg string, attrs ...any) {
	h.logWithLevel(context.Background(), slog.LevelInfo, source, msg, attrs...)
}

// Warn logs a WARNING entry with the given source and optional key/value attrs.
func (h *Hub) Warn(source, msg string, attrs ...any) {
	h.logWithLevel(context.Background(), slog.LevelWarn, source, msg, attrs...)
}

// Error logs an ERROR entry with the given source and optional key/value attrs.
func (h *Hub) Error(source, msg string, attrs ...any) {
	h.logWithLevel(context.Background(), slog.LevelError, source, msg, attrs...)
}

// Debug logs a DEBUG entry with the given source and optional key/value attrs.
func (h *Hub) Debug(source, msg string, attrs ...any) {
	h.logWithLevel(context.Background(), slog.LevelDebug, source, msg, attrs...)
}

func (h *Hub) logWithLevel(ctx context.Context, level slog.Level, source, msg string, attrs ...any) {
	if !h.Enabled(ctx, level) {
		return
	}
	rec := slog.NewRecord(time.Now().UTC(), level, msg, 0)
	if len(attrs) > 0 {
		rec.Add(attrs...)
	}
	_ = h.handleRecord(ctx, normalizeSource(source), nil, rec)
}

// Enabled implements slog.Handler.
func (h *Hub) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.minLevel
}

// Handle implements slog.Handler.
func (h *Hub) Handle(ctx context.Context, rec slog.Record) error {
	return h.handleRecord(ctx, SourceServer, nil, rec)
}

// WithAttrs implements slog.Handler.
func (h *Hub) WithAttrs(attrs []slog.Attr) slog.Handler {
	return (&hubHandler{hub: h, source: SourceServer}).WithAttrs(attrs)
}

// WithGroup implements slog.Handler.
func (h *Hub) WithGroup(name string) slog.Handler {
	return (&hubHandler{hub: h, source: SourceServer}).WithGroup(name)
}

type hubHandler struct {
	hub    *Hub
	source string
	attrs  []slog.Attr
	groups []string
}

func (hh *hubHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return hh.hub.Enabled(ctx, level)
}

func (hh *hubHandler) Handle(ctx context.Context, rec slog.Record) error {
	return hh.hub.handleRecord(ctx, hh.source, hh.attrs, rec, hh.groups...)
}

func (hh *hubHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	if len(attrs) == 0 {
		return hh
	}
	clone := *hh
	clone.attrs = append(append([]slog.Attr(nil), hh.attrs...), attrs...)
	for _, a := range attrs {
		if a.Key == "source" && a.Value.Kind() == slog.KindString {
			clone.source = normalizeSource(a.Value.String())
		}
	}
	return &clone
}

func (hh *hubHandler) WithGroup(name string) slog.Handler {
	if strings.TrimSpace(name) == "" {
		return hh
	}
	clone := *hh
	clone.groups = append(append([]string(nil), hh.groups...), name)
	return &clone
}

func (h *Hub) handleRecord(ctx context.Context, defaultSource string, baseAttrs []slog.Attr, rec slog.Record, groups ...string) error {
	source := defaultSource
	rawAttrs := make(map[string]any)

	collect := func(a slog.Attr) {
		if a.Equal(slog.Attr{}) {
			return
		}
		if a.Key == "source" && a.Value.Kind() == slog.KindString {
			if s := strings.TrimSpace(a.Value.String()); s != "" {
				source = normalizeSource(s)
			}
			return
		}
		target := rawAttrs
		for _, g := range groups {
			sub, ok := target[g].(map[string]any)
			if !ok {
				sub = make(map[string]any)
				target[g] = sub
			}
			target = sub
		}
		target[a.Key] = h.resolveAttrValue(a.Key, a.Value)
	}

	for _, a := range baseAttrs {
		collect(a)
	}
	rec.Attrs(func(a slog.Attr) bool {
		collect(a)
		return true
	})

	ts := rec.Time
	if ts.IsZero() {
		ts = time.Now().UTC()
	} else {
		ts = ts.UTC()
	}
	tsStr := ts.Format(time.RFC3339)
	levelStr := formatLevel(rec.Level)
	cleanMsg := h.redactString(rec.Message)

	var attrsOut map[string]any
	if len(rawAttrs) > 0 {
		attrsOut = h.redactMap(rawAttrs)
	}

	var attrsJSON string
	if len(attrsOut) > 0 {
		if b, err := json.Marshal(attrsOut); err == nil {
			attrsJSON = string(b)
		}
	}
	if attrsJSON == "" {
		attrsJSON = "{}"
	}

	var idStr string
	if h.store != nil {
		insertCtx := ctx
		if insertCtx == nil || insertCtx.Err() != nil {
			insertCtx = context.Background()
		}
		if rowID, err := h.store.InsertLog(insertCtx, tsStr, levelStr, source, cleanMsg, attrsJSON); err == nil {
			idStr = strconv.FormatInt(rowID, 10)
		}
	}
	if idStr == "" {
		idStr = strconv.FormatInt(h.fallbackSeq.Add(1), 10)
	}

	entry := LogEntry{
		ID:        idStr,
		Timestamp: tsStr,
		Level:     levelStr,
		Message:   cleanMsg,
		Source:    source,
		Attrs:     attrsOut,
	}

	h.writeConsole(entry, attrsJSON)
	h.broadcast(entry)
	return nil
}

func (h *Hub) writeConsole(entry LogEntry, attrsJSON string) {
	if h.out == nil {
		return
	}
	h.writeMu.Lock()
	defer h.writeMu.Unlock()

	if h.jsonFormat {
		if b, err := json.Marshal(entry); err == nil {
			_, _ = h.out.Write(append(b, '\n'))
		}
		return
	}

	if attrsJSON != "" && attrsJSON != "{}" {
		_, _ = fmt.Fprintf(h.out, "%s [%s] [%s] %s %s\n", entry.Timestamp, entry.Level, entry.Source, entry.Message, attrsJSON)
	} else {
		_, _ = fmt.Fprintf(h.out, "%s [%s] [%s] %s\n", entry.Timestamp, entry.Level, entry.Source, entry.Message)
	}
}

func (h *Hub) broadcast(entry LogEntry) {
	h.subMu.RLock()
	defer h.subMu.RUnlock()

	for _, ch := range h.subs {
		select {
		case ch <- entry:
		default:
			// Drop when subscriber cannot keep up so producers never block.
		}
	}
}

// Subscribe registers an SSE subscriber channel and returns a channel plus an idempotent cancel func.
func (h *Hub) Subscribe() (<-chan LogEntry, func()) {
	ch, cancel, ok := h.TrySubscribe()
	if !ok {
		closedCh := make(chan LogEntry)
		close(closedCh)
		return closedCh, func() {}
	}
	return ch, cancel
}

// TrySubscribe registers an SSE subscriber channel if the Hub is open and below its concurrency cap.
func (h *Hub) TrySubscribe() (<-chan LogEntry, func(), bool) {
	ch := make(chan LogEntry, h.bufSize)

	h.subMu.Lock()
	if h.closed || (h.maxSubs > 0 && len(h.subs) >= h.maxSubs) {
		h.subMu.Unlock()
		return nil, nil, false
	}
	h.nextSub++
	subID := h.nextSub
	h.subs[subID] = ch
	h.subMu.Unlock()

	var once sync.Once
	cancel := func() {
		once.Do(func() {
			h.subMu.Lock()
			if existing, ok := h.subs[subID]; ok {
				delete(h.subs, subID)
				close(existing)
			}
			h.subMu.Unlock()
		})
	}
	return ch, cancel, true
}

// Close terminates all active SSE subscriber channels cleanly during server shutdown.
func (h *Hub) Close() {
	h.subMu.Lock()
	defer h.subMu.Unlock()
	if h.closed {
		return
	}
	h.closed = true
	for id, ch := range h.subs {
		delete(h.subs, id)
		close(ch)
	}
}

// ==================== Secret Redaction ====================

func (h *Hub) resolveAttrValue(key string, v slog.Value) any {
	v = v.Resolve()
	if isSensitiveKey(key) {
		return "[REDACTED]"
	}
	switch v.Kind() {
	case slog.KindString:
		return h.redactString(v.String())
	case slog.KindInt64:
		return v.Int64()
	case slog.KindUint64:
		return v.Uint64()
	case slog.KindFloat64:
		return v.Float64()
	case slog.KindBool:
		return v.Bool()
	case slog.KindDuration:
		return v.Duration().String()
	case slog.KindTime:
		return v.Time().UTC().Format(time.RFC3339)
	case slog.KindGroup:
		groupMap := make(map[string]any)
		for _, a := range v.Group() {
			groupMap[a.Key] = h.resolveAttrValue(a.Key, a.Value)
		}
		return groupMap
	case slog.KindAny:
		return h.redactAny(v.Any())
	default:
		return h.redactString(v.String())
	}
}

func (h *Hub) redactMap(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, val := range m {
		if isSensitiveKey(k) {
			out[k] = "[REDACTED]"
			continue
		}
		out[k] = h.redactAny(val)
	}
	return out
}

func (h *Hub) redactAny(val any) any {
	switch t := val.(type) {
	case nil:
		return nil
	case string:
		return h.redactString(t)
	case error:
		return h.redactString(t.Error())
	case map[string]any:
		return h.redactMap(t)
	case []any:
		res := make([]any, len(t))
		for i, item := range t {
			res[i] = h.redactAny(item)
		}
		return res
	case []string:
		res := make([]string, len(t))
		for i, item := range t {
			res[i] = h.redactString(item)
		}
		return res
	default:
		return val
	}
}

func (h *Hub) redactString(s string) string {
	if s == "" {
		return ""
	}
	out := s

	h.secretMu.RLock()
	for _, sec := range h.secrets {
		if sec != "" && strings.Contains(out, sec) {
			out = strings.ReplaceAll(out, sec, "[REDACTED]")
		}
	}
	h.secretMu.RUnlock()

	out = mcpTokenRegex.ReplaceAllString(out, "[REDACTED_MCP_TOKEN]")
	out = encEnvRegex.ReplaceAllString(out, "[REDACTED_CIPHERTEXT]")
	out = telegramBotReg.ReplaceAllString(out, "bot[REDACTED]")
	out = bearerRegex.ReplaceAllString(out, "${1}[REDACTED]")
	out = kvSecretRegex.ReplaceAllString(out, "${1}[REDACTED]")
	return out
}

func isSensitiveKey(key string) bool {
	lower := strings.ToLower(strings.TrimSpace(key))
	if lower == "" {
		return false
	}
	for _, tok := range sensitiveTokens {
		if strings.Contains(lower, tok) {
			return true
		}
	}
	return false
}

func parseSlogLevel(raw string) slog.Level {
	switch strings.ToUpper(strings.TrimSpace(raw)) {
	case "DEBUG":
		return slog.LevelDebug
	case "WARN", "WARNING":
		return slog.LevelWarn
	case "ERROR":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

func formatLevel(l slog.Level) string {
	switch {
	case l < slog.LevelInfo:
		return "DEBUG"
	case l < slog.LevelWarn:
		return "INFO"
	case l < slog.LevelError:
		return "WARNING"
	default:
		return "ERROR"
	}
}

func normalizeSource(raw string) string {
	s := strings.ToLower(strings.TrimSpace(raw))
	if s == "" {
		return SourceServer
	}
	return s
}

// ==================== HTTP Handlers ====================

// LogsHandler exposes REST, SSE streaming, and export endpoints for system logs.
type LogsHandler struct {
	hub               *Hub
	store             *store.Store
	heartbeatInterval time.Duration
	writeTimeout      time.Duration
}

// NewLogsHandler constructs an HTTP handler for /api/logs endpoints.
func NewLogsHandler(hub *Hub, st *store.Store) *LogsHandler {
	return &LogsHandler{
		hub:               hub,
		store:             st,
		heartbeatInterval: 20 * time.Second,
		writeTimeout:      DefaultSSEWriteTimeout,
	}
}

// SetWriteTimeout overrides the per-frame SSE write deadline (intended for unit tests).
func (lh *LogsHandler) SetWriteTimeout(d time.Duration) {
	if d > 0 {
		lh.writeTimeout = d
	}
}

// SetHeartbeatInterval overrides the SSE keepalive interval (useful in tests).
func (lh *LogsHandler) SetHeartbeatInterval(d time.Duration) {
	if d > 0 {
		lh.heartbeatInterval = d
	}
}

// MountRoutes registers log routes onto a chi.Router mounted at /api.
func (lh *LogsHandler) MountRoutes(r chi.Router) {
	r.Get("/logs", lh.HandleGetLogs)
	r.Delete("/logs", lh.HandleClearLogs)
	r.Post("/logs/flush", lh.HandleFlushLogs)
	r.Get("/logs/stream", lh.HandleStreamLogs)
	r.Get("/logs/export", lh.HandleExportLogs)
}

// HandleGetLogs handles GET /api/logs with optional level, source, search, and limit params.
func (lh *LogsHandler) HandleGetLogs(w http.ResponseWriter, r *http.Request) {
	entries, err := lh.queryEntries(r, 1000)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "LOG_QUERY_FAILED", "Failed to query system logs.")
		return
	}
	writeJSON(w, http.StatusOK, entries)
}

// HandleClearLogs handles DELETE /api/logs.
func (lh *LogsHandler) HandleClearLogs(w http.ResponseWriter, r *http.Request) {
	if lh.store != nil {
		if err := lh.store.ClearLogs(r.Context()); err != nil {
			writeJSONError(w, http.StatusInternalServerError, "LOG_CLEAR_FAILED", "Failed to clear system logs.")
			return
		}
	}
	if lh.hub != nil {
		lh.hub.Info(SourceServer, "System logs cleared")
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"status":  "success",
		"message": "System logs cleared",
	})
}

// HandleFlushLogs handles POST /api/logs/flush for compatibility.
func (lh *LogsHandler) HandleFlushLogs(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"status":  "success",
		"message": "System logs flushed",
	})
}

// HandleStreamLogs handles GET /api/logs/stream using Server-Sent Events.
func (lh *LogsHandler) HandleStreamLogs(w http.ResponseWriter, r *http.Request) {
	if lh.hub == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "LOGGER_UNAVAILABLE", "Log stream broker is unavailable.")
		return
	}

	ch, cancel, ok := lh.hub.TrySubscribe()
	if !ok {
		writeJSONError(w, http.StatusServiceUnavailable, "SSE_MAX_SUBSCRIBERS", "Too many concurrent log stream connections.")
		return
	}
	defer cancel()

	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	rc := http.NewResponseController(w)
	writeTimeout := lh.writeTimeout
	if writeTimeout <= 0 {
		writeTimeout = DefaultSSEWriteTimeout
	}
	writeFrame := func(frame string) bool {
		_ = rc.SetWriteDeadline(time.Now().Add(writeTimeout))
		if _, err := fmt.Fprint(w, frame); err != nil {
			return false
		}
		if err := rc.Flush(); err != nil {
			return false
		}
		return true
	}

	if !writeFrame(": connected\n\n") {
		return
	}

	interval := lh.heartbeatInterval
	if interval <= 0 {
		interval = 20 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case entry, ok := <-ch:
			if !ok {
				return
			}
			payload, err := json.Marshal(entry)
			if err != nil {
				continue
			}
			if !writeFrame(fmt.Sprintf("id: %s\ndata: %s\n\n", entry.ID, payload)) {
				return
			}
		case <-ticker.C:
			if !writeFrame(": keepalive\n\n") {
				return
			}
		}
	}
}

// HandleExportLogs handles GET /api/logs/export?format=jsonl|log.
func (lh *LogsHandler) HandleExportLogs(w http.ResponseWriter, r *http.Request) {
	format := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("format")))
	if format == "" {
		format = "jsonl"
	}
	if format != "jsonl" && format != "log" {
		writeJSONError(w, http.StatusBadRequest, "INVALID_EXPORT_FORMAT", "Invalid export format. Supported formats: jsonl, log.")
		return
	}

	entries, err := lh.queryEntries(r, 10000)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "LOG_EXPORT_FAILED", "Failed to export system logs.")
		return
	}

	tsTag := time.Now().UTC().Format("20060102-150405")
	if format == "jsonl" {
		w.Header().Set("Content-Type", "application/x-ndjson; charset=utf-8")
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="ovh-nexus-logs-%s.jsonl"`, tsTag))
		enc := json.NewEncoder(w)
		for _, entry := range entries {
			if err := enc.Encode(entry); err != nil {
				return
			}
		}
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="ovh-nexus-logs-%s.log"`, tsTag))
	for _, entry := range entries {
		if len(entry.Attrs) > 0 {
			if b, err := json.Marshal(entry.Attrs); err == nil {
				_, _ = fmt.Fprintf(w, "%s [%s] [%s] %s %s\n", entry.Timestamp, entry.Level, entry.Source, entry.Message, string(b))
				continue
			}
		}
		_, _ = fmt.Fprintf(w, "%s [%s] [%s] %s\n", entry.Timestamp, entry.Level, entry.Source, entry.Message)
	}
}

func (lh *LogsHandler) queryEntries(r *http.Request, defaultLimit int) ([]LogEntry, error) {
	if lh.store == nil {
		return []LogEntry{}, nil
	}
	q := r.URL.Query()
	limit := defaultLimit
	if rawLimit := strings.TrimSpace(q.Get("limit")); rawLimit != "" {
		if parsed, err := strconv.Atoi(rawLimit); err == nil && parsed > 0 {
			limit = parsed
		}
	}

	rows, err := lh.store.QueryLogs(r.Context(), q.Get("level"), q.Get("source"), q.Get("search"), limit)
	if err != nil {
		return nil, err
	}

	entries := make([]LogEntry, 0, len(rows))
	for _, row := range rows {
		var attrs map[string]any
		if row.AttrsJSON != "" && row.AttrsJSON != "{}" {
			_ = json.Unmarshal([]byte(row.AttrsJSON), &attrs)
		}
		entries = append(entries, LogEntry{
			ID:        strconv.FormatInt(row.ID, 10),
			Timestamp: row.Timestamp,
			Level:     row.Level,
			Message:   row.Message,
			Source:    row.Source,
			Attrs:     attrs,
		})
	}
	return entries, nil
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeJSONError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]string{
		"error":   http.StatusText(status),
		"code":    code,
		"message": message,
	})
}
