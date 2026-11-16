package logger

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"ovh-nexus/server/internal/store"
	"ovh-nexus/server/internal/vault"
)

func newTestLoggerStore(t *testing.T) *store.Store {
	t.Helper()
	v, err := vault.New("logger-test-encryption-key-0001")
	if err != nil {
		t.Fatalf("vault.New: %v", err)
	}
	st, err := store.Open(filepath.Join(t.TempDir(), "data"), v)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

func TestLoggerCanonicalSourcesAndRedaction(t *testing.T) {
	if len(AllSources()) != 14 {
		t.Fatalf("expected 14 canonical sources, got %d", len(AllSources()))
	}
	for _, src := range AllSources() {
		if src == "" || strings.Contains(src, " ") {
			t.Fatalf("unexpected malformed source %q", src)
		}
	}

	st := newTestLoggerStore(t)
	var buf bytes.Buffer
	hub := NewHub(st, Options{Level: "DEBUG", Format: "text", Output: &buf})
	hub.AddSensitiveValue("top-secret-runtime-key")

	s := hub.WithSource(SourceAuth)
	s.Info("Attempted auth with Bearer supersecrettoken123 and top-secret-runtime-key",
		slog.String("appSecret", "ovh-secret-xyz"),
		slog.String("token", "nxs_mcp_abcdef1234567890"),
		slog.String("ip", "203.0.113.8"),
		slog.String("encrypted", "enc:v1:abc123:def456"),
	)
	hub.Warn(SourceQueue, "Order fallback with password=my_pw_123 and bot123456:ABCDEF_secret_token_999",
		slog.Group("nested", slog.String("consumerKey", "ck-999"), slog.String("planCode", "24ska01")),
	)

	outText := buf.String()
	for _, leaked := range []string{
		"supersecrettoken123",
		"top-secret-runtime-key",
		"ovh-secret-xyz",
		"nxs_mcp_abcdef1234567890",
		"enc:v1:abc123:def456",
		"my_pw_123",
		"ABCDEF_secret_token_999",
		"ck-999",
	} {
		if strings.Contains(outText, leaked) {
			t.Fatalf("console output leaked sensitive value %q:\n%s", leaked, outText)
		}
	}

	rows, err := st.QueryLogs(context.Background(), "", "", "", 10)
	if err != nil || len(rows) != 2 {
		t.Fatalf("expected 2 persisted rows, got %d err=%v", len(rows), err)
	}
	if rows[0].Source != SourceAuth || rows[1].Source != SourceQueue {
		t.Fatalf("unexpected sources: %q, %q", rows[0].Source, rows[1].Source)
	}
	if rows[0].Level != "INFO" || rows[1].Level != "WARNING" {
		t.Fatalf("unexpected levels: %q, %q", rows[0].Level, rows[1].Level)
	}
	if strings.Contains(rows[0].AttrsJSON, "ovh-secret-xyz") || !strings.Contains(rows[0].AttrsJSON, "[REDACTED]") {
		t.Fatalf("expected attrs redacted in SQLite, got %s", rows[0].AttrsJSON)
	}
}

func TestLoggerSSEBrokerConcurrency(t *testing.T) {
	st := newTestLoggerStore(t)
	var buf bytes.Buffer
	hub := NewHub(st, Options{Level: "INFO", Format: "json", Output: &buf})

	ch, cancel := hub.Subscribe()
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := range 25 {
			hub.Info(SourceStockMonitor, "Availability update", slog.Int("seq", i))
		}
	}()

	received := 0
	timeout := time.After(2 * time.Second)
	for received < 25 {
		select {
		case entry := <-ch:
			if entry.ID == "" || entry.Source != SourceStockMonitor || entry.Timestamp == "" {
				t.Fatalf("invalid streamed LogEntry: %+v", entry)
			}
			received++
		case <-timeout:
			t.Fatalf("timed out waiting for SSE entries, got %d/25", received)
		}
	}
	cancel()
	cancel() // must be idempotent
	wg.Wait()
}

func TestLogsHTTPHandlersAndSSEStream(t *testing.T) {
	st := newTestLoggerStore(t)
	var buf bytes.Buffer
	hub := NewHub(st, Options{Level: "INFO", Format: "text", Output: &buf})
	handler := NewLogsHandler(hub, st)
	handler.SetHeartbeatInterval(40 * time.Millisecond)

	r := chi.NewRouter()
	r.Route("/api", func(api chi.Router) {
		handler.MountRoutes(api)
	})

	hub.Info(SourceServer, "Server started", slog.String("port", "8080"))
	hub.Error(SourceOVH, "Transient OVH failure", slog.Int("status", 502))

	// 1. GET /api/logs
	req := httptest.NewRequest(http.MethodGet, "/api/logs", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/logs status=%d body=%s", rec.Code, rec.Body.String())
	}
	var entries []LogEntry
	if err := json.Unmarshal(rec.Body.Bytes(), &entries); err != nil {
		t.Fatalf("decode GET /api/logs: %v", err)
	}
	if len(entries) != 2 || entries[0].Source != SourceServer || entries[1].Source != SourceOVH {
		t.Fatalf("unexpected GET /api/logs entries: %+v", entries)
	}

	// 2. GET /api/logs/export?format=jsonl and format=log
	reqJSONL := httptest.NewRequest(http.MethodGet, "/api/logs/export?format=jsonl", nil)
	recJSONL := httptest.NewRecorder()
	r.ServeHTTP(recJSONL, reqJSONL)
	if recJSONL.Code != http.StatusOK || !strings.Contains(recJSONL.Header().Get("Content-Disposition"), ".jsonl") {
		t.Fatalf("export jsonl failed: code=%d headers=%v", recJSONL.Code, recJSONL.Header())
	}
	lines := strings.Split(strings.TrimSpace(recJSONL.Body.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 jsonl lines, got %d", len(lines))
	}

	reqLog := httptest.NewRequest(http.MethodGet, "/api/logs/export?format=log&level=ERROR", nil)
	recLog := httptest.NewRecorder()
	r.ServeHTTP(recLog, reqLog)
	if recLog.Code != http.StatusOK || !strings.Contains(recLog.Body.String(), "[ERROR] [ovh] Transient OVH failure") {
		t.Fatalf("export log failed: code=%d body=%s", recLog.Code, recLog.Body.String())
	}

	reqBad := httptest.NewRequest(http.MethodGet, "/api/logs/export?format=csv", nil)
	recBad := httptest.NewRecorder()
	r.ServeHTTP(recBad, reqBad)
	if recBad.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid export format, got %d", recBad.Code)
	}

	// 3. GET /api/logs/stream (real HTTP test server for SSE streaming)
	ts := httptest.NewServer(r)
	defer ts.Close()

	streamCtx, cancelStream := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancelStream()
	streamReq, _ := http.NewRequestWithContext(streamCtx, http.MethodGet, ts.URL+"/api/logs/stream", nil)
	resp, err := http.DefaultClient.Do(streamReq)
	if err != nil {
		t.Fatalf("SSE connect failed: %v", err)
	}
	defer resp.Body.Close()

	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "text/event-stream") {
		t.Fatalf("unexpected SSE Content-Type: %q", ct)
	}
	if xb := resp.Header.Get("X-Accel-Buffering"); xb != "no" {
		t.Fatalf("expected X-Accel-Buffering: no, got %q", xb)
	}

	scanner := bufio.NewScanner(resp.Body)
	gotConnected := false
	gotData := false
	gotKeepalive := false
	go func() {
		time.Sleep(30 * time.Millisecond)
		hub.Info(SourceMCP, "MCP tool invoked", slog.String("tool", "baremetal_list"))
	}()
	for scanner.Scan() {
		line := scanner.Text()
		if line == ": connected" {
			gotConnected = true
		}
		if line == ": keepalive" {
			gotKeepalive = true
		}
		if strings.HasPrefix(line, "data: ") {
			var streamed LogEntry
			if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &streamed); err != nil {
				t.Fatalf("invalid SSE data JSON: %v", err)
			}
			if streamed.Source == SourceMCP && streamed.Message == "MCP tool invoked" {
				gotData = true
			}
		}
		if gotConnected && gotData && gotKeepalive {
			break
		}
	}
	if !gotConnected || !gotData || !gotKeepalive {
		t.Fatalf("SSE stream missing expected events: connected=%v data=%v keepalive=%v err=%v",
			gotConnected, gotData, gotKeepalive, scanner.Err())
	}

	// 4. DELETE /api/logs
	reqDel := httptest.NewRequest(http.MethodDelete, "/api/logs", nil)
	recDel := httptest.NewRecorder()
	r.ServeHTTP(recDel, reqDel)
	if recDel.Code != http.StatusOK {
		t.Fatalf("DELETE /api/logs status=%d", recDel.Code)
	}
}
