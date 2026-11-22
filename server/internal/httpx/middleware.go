package httpx

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"runtime/debug"
	"slices"
	"strings"
	"sync"
	"time"

	"ovh-nexus/server/internal/logger"
)

const (
	// MaxRequestBodyBytes caps HTTP request bodies at 2 MiB globally.
	MaxRequestBodyBytes int64 = 2 << 20

	contentSecurityPolicy = "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; " +
		"img-src 'self' data:; font-src 'self' data:; " +
		"connect-src 'self' https://eu.api.ovh.com https://ca.api.ovh.com https://api.us.ovhcloud.com; " +
		"frame-ancestors 'none'; base-uri 'self'; form-action 'self'"
)

var gzipWriterPool = sync.Pool{
	New: func() any {
		return gzip.NewWriter(io.Discard)
	},
}

// SecurityHeadersMiddleware injects strict defensive HTTP security headers on every response.
func SecurityHeadersMiddleware(next http.Handler) http.Handler {
	return SecurityHeadersWithConfig(false)(next)
}

// SecurityHeadersWithConfig injects strict defensive HTTP security headers and adds HSTS when cookieSecure is true.
func SecurityHeadersWithConfig(cookieSecure bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := w.Header()
			h.Set("X-Frame-Options", "DENY")
			h.Set("X-Content-Type-Options", "nosniff")
			h.Set("Referrer-Policy", "no-referrer")
			h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
			h.Set("Content-Security-Policy", contentSecurityPolicy)
			if cookieSecure {
				h.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
			}
			next.ServeHTTP(w, r)
		})
	}
}

// CORSMiddleware validates Origin against configured CORSOrigins and handles browser preflights.
// SECURITY: Wildcard ("*") origins NEVER set Access-Control-Allow-Credentials: true and NEVER reflect arbitrary Origins.
func CORSMiddleware(allowedOrigins []string) func(http.Handler) http.Handler {
	allowed := make([]string, 0, len(allowedOrigins))
	hasWildcard := false
	for _, o := range allowedOrigins {
		if trimmed := strings.TrimSpace(o); trimmed != "" {
			if trimmed == "*" {
				hasWildcard = true
			} else {
				allowed = append(allowed, trimmed)
			}
		}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := strings.TrimSpace(r.Header.Get("Origin"))
			if origin != "" {
				explicit := matchesExplicitOrigin(origin, allowed)
				if explicit || hasWildcard {
					h := w.Header()
					if explicit {
						h.Set("Access-Control-Allow-Origin", origin)
						h.Set("Access-Control-Allow-Credentials", "true")
						h.Add("Vary", "Origin")
					} else {
						h.Set("Access-Control-Allow-Origin", "*")
					}
					h.Set("Access-Control-Expose-Headers", "X-Cache-Warning, Content-Disposition, Mcp-Session-Id")

					if r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != "" {
						h.Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, PATCH, OPTIONS")
						h.Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-API-Key, X-OVH-Account, X-TOTP-Code, X-MCP-Token, X-Internal-Token, Mcp-Session-Id")
						h.Set("Access-Control-Max-Age", "600")
						w.WriteHeader(http.StatusNoContent)
						return
					}
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

// CSRFProtectionMiddleware validates Origin / Sec-Fetch-Site headers on cookie-authenticated
// non-idempotent requests (POST/PUT/PATCH/DELETE) while exempting Bearer MCP and API-key clients.
func CSRFProtectionMiddleware(allowedOrigins []string) func(http.Handler) http.Handler {
	explicitAllowed := make([]string, 0, len(allowedOrigins))
	for _, o := range allowedOrigins {
		if trimmed := strings.TrimSpace(o); trimmed != "" && trimmed != "*" {
			explicitAllowed = append(explicitAllowed, trimmed)
		}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
			default:
				next.ServeHTTP(w, r)
				return
			}

			normPath := strings.TrimRight(r.URL.Path, "/")
			if !strings.HasPrefix(normPath, "/api/") || normPath == "/api/mcp" {
				next.ServeHTTP(w, r)
				return
			}
			// Non-cookie programmatic auth mechanisms (MCP Bearer, loopback X-API-Key, internal token) are exempt from CSRF.
			if strings.HasPrefix(strings.ToLower(strings.TrimSpace(r.Header.Get("Authorization"))), "bearer ") ||
				strings.TrimSpace(r.Header.Get("X-API-Key")) != "" ||
				strings.TrimSpace(r.Header.Get("X-Internal-Token")) != "" {
				next.ServeHTTP(w, r)
				return
			}

			_, hasSessionCookie := r.Cookie("ovh_session")
			isAuthMutation := normPath == "/api/auth/login" || normPath == "/api/auth/logout"
			if hasSessionCookie != nil && !isAuthMutation {
				next.ServeHTTP(w, r)
				return
			}

			origin := strings.TrimSpace(r.Header.Get("Origin"))
			if origin != "" {
				if !originMatchesHost(origin, r.Host) && !matchesExplicitOrigin(origin, explicitAllowed) {
					writeJSONError(w, http.StatusForbidden, "CSRF_ORIGIN_REJECTED", "Cross-origin state-changing request rejected.")
					return
				}
				next.ServeHTTP(w, r)
				return
			}

			if sfs := strings.ToLower(strings.TrimSpace(r.Header.Get("Sec-Fetch-Site"))); sfs != "" {
				if sfs != "same-origin" && sfs != "none" {
					writeJSONError(w, http.StatusForbidden, "CSRF_CROSS_SITE_REJECTED", "Cross-site state-changing request rejected.")
					return
				}
				next.ServeHTTP(w, r)
				return
			}

			if ref := strings.TrimSpace(r.Header.Get("Referer")); ref != "" {
				if !originMatchesHost(ref, r.Host) && !matchesExplicitOrigin(refOrigin(ref), explicitAllowed) {
					writeJSONError(w, http.StatusForbidden, "CSRF_REFERER_REJECTED", "Cross-origin referer rejected.")
					return
				}
			}

			next.ServeHTTP(w, r)
		})
	}
}

func matchesExplicitOrigin(origin string, allowed []string) bool {
	for _, item := range allowed {
		if item != "*" && strings.EqualFold(origin, item) {
			return true
		}
	}
	return false
}

func originMatchesHost(rawURL, reqHost string) bool {
	if strings.TrimSpace(rawURL) == "" || strings.TrimSpace(reqHost) == "" {
		return false
	}
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" {
		return false
	}
	return strings.EqualFold(u.Host, strings.TrimSpace(reqHost))
}

func refOrigin(rawRef string) string {
	u, err := url.Parse(rawRef)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return ""
	}
	return u.Scheme + "://" + u.Host
}

func isOriginAllowed(origin string, allowed []string) bool {
	if slices.Contains(allowed, "*") {
		return true
	}
	return matchesExplicitOrigin(origin, allowed)
}

// MaxBodySizeMiddleware enforces a global 2 MiB request body cap using http.MaxBytesReader.
func MaxBodySizeMiddleware(limit int64) func(http.Handler) http.Handler {
	if limit <= 0 {
		limit = MaxRequestBodyBytes
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.ContentLength > limit {
				writeJSONError(w, http.StatusRequestEntityTooLarge, "PAYLOAD_TOO_LARGE", "Request body exceeds the 2 MB maximum limit.")
				return
			}
			if r.Body != nil && r.Body != http.NoBody {
				r.Body = http.MaxBytesReader(w, r.Body, limit)
			}
			next.ServeHTTP(w, r)
		})
	}
}

// RecoveryMiddleware catches panics inside HTTP handlers and logs them via logger.SourceHTTP.
func RecoveryMiddleware(logHub *logger.Hub) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					if logHub != nil {
						logHub.Error(logger.SourceHTTP, "Recovered from HTTP handler panic",
							"method", r.Method,
							"path", r.URL.Path,
							"panic", rec,
							"stack", string(debug.Stack()),
						)
					}
					if strings.HasPrefix(r.URL.Path, "/api") {
						writeJSONError(w, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "An unexpected internal server error occurred.")
					} else {
						http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
					}
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// GzipMiddleware compresses JSON and static WebUI assets while skipping SSE streams
// and preserving http.Flusher and http.ResponseController unwrapping.
func GzipMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if shouldSkipGzip(r) {
			next.ServeHTTP(w, r)
			return
		}

		grw := &gzipResponseWriter{
			ResponseWriter: w,
		}
		defer grw.finish()
		next.ServeHTTP(grw, r)
	})
}

func shouldSkipGzip(r *http.Request) bool {
	if r.Method == http.MethodHead {
		return true
	}
	if !acceptsGzip(r.Header.Get("Accept-Encoding")) {
		return true
	}
	if strings.Contains(strings.ToLower(r.Header.Get("Accept")), "text/event-stream") {
		return true
	}
	if r.URL.Path == "/api/logs/stream" || strings.HasSuffix(r.URL.Path, "/stream") {
		return true
	}
	if r.Header.Get("Upgrade") != "" || r.Header.Get("Range") != "" {
		return true
	}
	return false
}

func acceptsGzip(hdr string) bool {
	for _, part := range strings.Split(hdr, ",") {
		coding := strings.TrimSpace(strings.Split(part, ";")[0])
		if strings.EqualFold(coding, "gzip") {
			return true
		}
	}
	return false
}

type gzipResponseWriter struct {
	http.ResponseWriter
	gz          *gzip.Writer
	decided     bool
	compressing bool
	wroteHeader bool
	statusCode  int
}

func (g *gzipResponseWriter) Unwrap() http.ResponseWriter {
	return g.ResponseWriter
}

func (g *gzipResponseWriter) WriteHeader(code int) {
	if g.wroteHeader {
		return
	}
	g.statusCode = code

	// Statuses with no response body must bypass compression immediately.
	if code == http.StatusNoContent || code == http.StatusNotModified || (code >= 100 && code < 200) {
		g.decided = true
		g.compressing = false
		g.wroteHeader = true
		g.ResponseWriter.WriteHeader(code)
		return
	}

	// If Content-Encoding is already set or response is SSE, pass through directly.
	if g.Header().Get("Content-Encoding") != "" {
		g.decided = true
		g.compressing = false
		g.wroteHeader = true
		g.ResponseWriter.WriteHeader(code)
		return
	}
	ct := strings.ToLower(g.Header().Get("Content-Type"))
	if strings.Contains(ct, "text/event-stream") {
		g.decided = true
		g.compressing = false
		g.wroteHeader = true
		g.ResponseWriter.WriteHeader(code)
		return
	}
	if ct != "" && !isCompressibleContentType(ct) {
		g.decided = true
		g.compressing = false
		g.wroteHeader = true
		g.ResponseWriter.WriteHeader(code)
		return
	}

	// Defer sending headers until first non-empty Write or finish() so empty responses
	// (e.g. 202 Accepted) do not emit bogus Content-Encoding: gzip headers.
}

func (g *gzipResponseWriter) Write(p []byte) (int, error) {
	if len(p) == 0 {
		if !g.wroteHeader && g.statusCode != 0 {
			g.wroteHeader = true
			g.ResponseWriter.WriteHeader(g.statusCode)
		}
		return 0, nil
	}

	if !g.decided {
		g.decideFromPayload(p)
	}
	if !g.wroteHeader {
		g.commitHeader()
	}
	if g.compressing && g.gz != nil {
		return g.gz.Write(p)
	}
	return g.ResponseWriter.Write(p)
}

func (g *gzipResponseWriter) decideFromPayload(p []byte) {
	g.decided = true
	if g.Header().Get("Content-Encoding") != "" {
		g.compressing = false
		return
	}
	ct := strings.ToLower(g.Header().Get("Content-Type"))
	if ct == "" {
		ct = strings.ToLower(http.DetectContentType(p))
	}
	if strings.Contains(ct, "text/event-stream") {
		g.compressing = false
		return
	}
	g.compressing = isCompressibleContentType(ct)
}

func (g *gzipResponseWriter) commitHeader() {
	if g.wroteHeader {
		return
	}
	code := g.statusCode
	if code == 0 {
		code = http.StatusOK
	}
	if g.compressing {
		h := g.Header()
		h.Del("Content-Length")
		h.Set("Content-Encoding", "gzip")
		h.Add("Vary", "Accept-Encoding")
		if etag := h.Get("ETag"); etag != "" && !strings.HasPrefix(etag, "W/") {
			h.Set("ETag", "W/"+etag)
		}
		gz := gzipWriterPool.Get().(*gzip.Writer)
		gz.Reset(g.ResponseWriter)
		g.gz = gz
	}
	g.wroteHeader = true
	g.ResponseWriter.WriteHeader(code)
}

func (g *gzipResponseWriter) Flush() {
	if !g.decided {
		g.decided = true
		ct := strings.ToLower(g.Header().Get("Content-Type"))
		if strings.Contains(ct, "text/event-stream") {
			g.compressing = false
		}
	}
	if !g.wroteHeader {
		g.commitHeader()
	}
	if g.gz != nil {
		_ = g.gz.Flush()
	}
	_ = http.NewResponseController(g.ResponseWriter).Flush()
}

func (g *gzipResponseWriter) finish() {
	if !g.wroteHeader && g.statusCode != 0 {
		g.decided = true
		g.compressing = false
		g.wroteHeader = true
		g.ResponseWriter.WriteHeader(g.statusCode)
	}
	if g.gz != nil {
		_ = g.gz.Close()
		g.gz.Reset(io.Discard)
		gzipWriterPool.Put(g.gz)
		g.gz = nil
	}
}

func isCompressibleContentType(ct string) bool {
	mimeType := strings.TrimSpace(strings.Split(strings.ToLower(ct), ";")[0])
	switch {
	case strings.HasPrefix(mimeType, "text/"):
		return mimeType != "text/event-stream"
	case mimeType == "application/json",
		mimeType == "application/ld+json",
		mimeType == "application/javascript",
		mimeType == "application/x-javascript",
		mimeType == "application/xml",
		mimeType == "application/xhtml+xml",
		mimeType == "application/x-ndjson",
		mimeType == "application/wasm",
		mimeType == "image/svg+xml":
		return true
	case strings.HasSuffix(mimeType, "+json"),
		strings.HasSuffix(mimeType, "+xml"):
		return true
	default:
		return false
	}
}

// sseStreamMiddleware clears the server write deadline for long-lived SSE log streams
// and aborts active SSE handlers immediately when SignalShutdown is invoked.
func sseStreamMiddleware(shutdownCh <-chan struct{}) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/api/logs/stream" {
				rc := http.NewResponseController(w)
				_ = rc.SetWriteDeadline(time.Time{})

				if shutdownCh != nil {
					reqCtx, cancel := context.WithCancel(r.Context())
					defer cancel()
					go func() {
						select {
						case <-shutdownCh:
							cancel()
						case <-reqCtx.Done():
						}
					}()
					r = r.WithContext(reqCtx)
				}
			}
			next.ServeHTTP(w, r)
		})
	}
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
