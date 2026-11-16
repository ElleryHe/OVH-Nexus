package webui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWebUIHandlerSPAFallbackAndAPIGuard(t *testing.T) {
	h := Handler()

	// Root serves index.html with no-cache
	recRoot := httptest.NewRecorder()
	h.ServeHTTP(recRoot, httptest.NewRequest(http.MethodGet, "/", nil))
	if recRoot.Code != http.StatusOK || !strings.Contains(recRoot.Body.String(), "OVH Nexus") {
		t.Fatalf("expected index.html on /, got %d body=%s", recRoot.Code, recRoot.Body.String())
	}
	if !strings.Contains(recRoot.Header().Get("Cache-Control"), "no-cache") {
		t.Fatalf("expected no-cache on index.html, got %q", recRoot.Header().Get("Cache-Control"))
	}

	// Unknown SPA path falls back to index.html
	recSPA := httptest.NewRecorder()
	h.ServeHTTP(recSPA, httptest.NewRequest(http.MethodGet, "/settings/totp", nil))
	if recSPA.Code != http.StatusOK || !strings.Contains(recSPA.Body.String(), "OVH Nexus") {
		t.Fatalf("expected SPA fallback on /settings/totp, got %d", recSPA.Code)
	}

	// Unknown /api/* returns 404 JSON, never index.html
	recAPI := httptest.NewRecorder()
	h.ServeHTTP(recAPI, httptest.NewRequest(http.MethodGet, "/api/does-not-exist", nil))
	if recAPI.Code != http.StatusNotFound || !strings.Contains(recAPI.Header().Get("Content-Type"), "application/json") {
		t.Fatalf("expected 404 JSON on unknown /api path, got %d ct=%q", recAPI.Code, recAPI.Header().Get("Content-Type"))
	}

	// Missing asset returns 404, not index.html
	recAsset := httptest.NewRecorder()
	h.ServeHTTP(recAsset, httptest.NewRequest(http.MethodGet, "/assets/missing.js", nil))
	if recAsset.Code != http.StatusNotFound {
		t.Fatalf("expected 404 on missing asset, got %d", recAsset.Code)
	}
}
