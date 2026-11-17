package notify

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"ovh-nexus/server/internal/logger"
	"ovh-nexus/server/internal/ovhclient"
	"ovh-nexus/server/internal/store"
	"ovh-nexus/server/internal/vault"
)

func newTestDispatcher(t *testing.T) (*Dispatcher, *store.Store, chi.Router) {
	t.Helper()
	v, err := vault.New("test-master-encryption-key-123456")
	if err != nil {
		t.Fatalf("vault.New failed: %v", err)
	}
	st, err := store.Open(t.TempDir(), v)
	if err != nil {
		t.Fatalf("store.Open failed: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	var buf bytes.Buffer
	hub := logger.NewHub(st, logger.Options{Level: "DEBUG", Format: "text", Output: &buf})
	pool := ovhclient.NewPool(st, hub)
	disp := NewDispatcher(st, v, hub, pool)

	r := chi.NewRouter()
	r.Route("/api", func(api chi.Router) {
		disp.MountRoutes(api)
	})
	return disp, st, r
}

func TestTrilingualTemplatesAll8Events(t *testing.T) {
	origLocal := time.Local
	tokyo, err := time.LoadLocation("Asia/Tokyo")
	if err != nil {
		tokyo = time.FixedZone("JST", 9*3600)
	}
	time.Local = tokyo
	t.Cleanup(func() { time.Local = origLocal })

	ts := time.Date(2026, 11, 16, 10, 0, 0, 0, time.UTC) // 19:00:00 JST

	events := []struct {
		key  string
		data map[string]any
	}{
		{EventStockAvailable, map[string]any{"planCode": "24ska01", "modelName": "KS-A", "datacenter": "GRA", "price": "EUR 9.99/mo"}},
		{EventStockUnavailable, map[string]any{"planCode": "24ska01", "modelName": "KS-A", "datacenter": "GRA", "duration": "3m10s"}},
		{EventOrderSuccess, map[string]any{"planCode": "24ska01", "modelName": "KS-A", "datacenter": "GRA", "orderId": "123456", "autoPaid": false}},
		{EventOrderFailed, map[string]any{"planCode": "24ska01", "modelName": "KS-A", "datacenter": "GRA", "error": "Out of stock <script>alert(1)</script>"}},
		{EventSKUChanged, map[string]any{"endpoint": "ovh-eu", "addedCount": "2", "removedCount": "1", "changedCount": "0"}},
		{EventServiceExpiring, map[string]any{"serviceName": "ns123.ip.eu", "serviceType": "dedicated", "expiration": "2026-11-20", "daysRemaining": "4", "renewMode": "manual"}},
		{EventOrderDelivered, map[string]any{"orderId": "123456", "planCode": "24ska01", "modelName": "KS-A", "datacenter": "GRA"}},
		{EventMCPAction, map[string]any{"tool": "order_create_unpaid", "scope": "procurement", "clientIp": "127.0.0.1", "summary": "Created unpaid order ord-01"}},
	}

	locales := []struct {
		code       string
		wantLang   string
		wantCTA    string
		wantFooter string
	}{
		{LocaleEN, `lang="en"`, "Open OVH Nexus Console", "Sent at:"},
		{LocaleFR, `lang="fr"`, "Ouvrir la console OVH Nexus", "Envoyé le:"},
		{LocaleZHCN, `lang="zh-CN"`, "打开 OVH Nexus 控制台", "发送时间:"},
	}

	for _, loc := range locales {
		for _, ev := range events {
			rn := RenderNotification(loc.code, ev.key, "", "", nil, "https://nexus.example.com", ev.data, ts)
			if rn.Locale != loc.code {
				t.Fatalf("expected locale %q, got %q", loc.code, rn.Locale)
			}
			if !strings.Contains(rn.HTMLBody, loc.wantLang) {
				t.Errorf("event %s (%s) HTML missing %s", ev.key, loc.code, loc.wantLang)
			}
			if !strings.Contains(rn.HTMLBody, loc.wantCTA) {
				t.Errorf("event %s (%s) HTML missing CTA %q", ev.key, loc.code, loc.wantCTA)
			}
			if !strings.Contains(rn.HTMLBody, "https://nexus.example.com") {
				t.Errorf("event %s (%s) HTML missing panelUrl", ev.key, loc.code)
			}
			if !strings.Contains(rn.HTMLBody, "19:00:00") {
				t.Errorf("event %s (%s) HTML did not use time.Local (expected 19:00:00, got %s)", ev.key, loc.code, rn.Timestamp)
			}
			if !strings.Contains(rn.TelegramText, loc.wantFooter) {
				t.Errorf("event %s (%s) Telegram text missing footer %q", ev.key, loc.code, loc.wantFooter)
			}
			if ev.key == EventOrderFailed && strings.Contains(rn.HTMLBody, "<script>") {
				t.Errorf("XSS payload was not HTML-escaped in HTMLBody")
			}
			if (ev.key == EventStockAvailable || ev.key == EventStockUnavailable || ev.key == EventOrderSuccess || ev.key == EventOrderFailed) && !strings.Contains(rn.TelegramText, "🇫🇷") {
				t.Errorf("event %s (%s) Telegram text missing localized datacenter flag 🇫🇷: %s", ev.key, loc.code, rn.TelegramText)
			}
		}
	}
}

func TestNotificationHTTPRoutesAndSecretPreservation(t *testing.T) {
	disp, st, router := newTestDispatcher(t)

	// No webhook routes must exist
	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/api/telegram/get-webhook-info"},
		{http.MethodPost, "/api/telegram/set-webhook"},
		{http.MethodPost, "/api/telegram/webhook"},
	} {
		req := httptest.NewRequest(tc.method, tc.path, nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound && rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("expected webhook route %s %s to be removed, got HTTP %d", tc.method, tc.path, rec.Code)
		}
	}

	// GET /api/notifications/events
	req := httptest.NewRequest(http.MethodGet, "/api/notifications/events", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/notifications/events status=%d", rec.Code)
	}
	var evResp struct {
		Events []EventDefinition `json:"events"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &evResp); err != nil {
		t.Fatalf("decode events response: %v", err)
	}
	if len(evResp.Events) != 8 {
		t.Fatalf("expected 8 events, got %d", len(evResp.Events))
	}

	// GET /api/notifications default locale "en"
	req = httptest.NewRequest(http.MethodGet, "/api/notifications", nil)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/notifications status=%d", rec.Code)
	}
	var initState PublicState
	if err := json.Unmarshal(rec.Body.Bytes(), &initState); err != nil {
		t.Fatalf("decode initial state: %v", err)
	}
	if initState.Locale != "en" {
		t.Fatalf("expected default locale 'en', got %q", initState.Locale)
	}
	if initState.HasAPIKey {
		t.Fatalf("expected hasApiKey=false when no account exists")
	}

	// PUT /api/notifications with Telegram + SMTP channels
	putBody := `{
		"panelUrl": "https://panel.ovh-nexus.test",
		"locale": "fr",
		"rules": {"skuEnabled": true, "skuInterval": 300, "skuTrackConfigs": true, "skuRanges": ["KS","RISE"], "skuMergeRegional": true, "expiryDays": 14},
		"channels": [
			{
				"id": "tg-1",
				"type": "telegram",
				"name": "Ops TG",
				"enabled": true,
				"events": ["stock.available", "order.success"],
				"config": {"token": "99887766:SECRET_TELEGRAM_TOKEN", "chatId": "-100123456"}
			},
			{
				"id": "smtp-1",
				"type": "smtp",
				"name": "Ops SMTP",
				"enabled": true,
				"events": ["order.failed", "mcp.action"],
				"config": {
					"host": "smtp.example.com",
					"port": 465,
					"security": "ssl",
					"username": "ops@example.com",
					"password": "super-secret-smtp-password",
					"fromName": "OVH Nexus",
					"fromAddress": "ops@example.com",
					"recipients": "a@example.com, b@example.com"
				}
			}
		]
	}`
	req = httptest.NewRequest(http.MethodPut, "/api/notifications", strings.NewReader(putBody))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT /api/notifications status=%d body=%s", rec.Code, rec.Body.String())
	}
	var putResp struct {
		Success bool        `json:"success"`
		Data    PublicState `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &putResp); err != nil {
		t.Fatalf("decode PUT response: %v", err)
	}
	if !putResp.Success || putResp.Data.Locale != "fr" || len(putResp.Data.Channels) != 2 {
		t.Fatalf("unexpected PUT response: %+v", putResp)
	}
	if strings.Contains(rec.Body.String(), "SECRET_TELEGRAM_TOKEN") || strings.Contains(rec.Body.String(), "super-secret-smtp-password") {
		t.Fatalf("PUT response leaked plaintext channel secrets: %s", rec.Body.String())
	}

	// Second PUT sending empty/masked secrets must preserve the stored encrypted secrets!
	putBodyPreserve := `{
		"panelUrl": "https://panel.ovh-nexus.test",
		"locale": "en",
		"channels": [
			{
				"id": "tg-1",
				"type": "telegram",
				"name": "Ops TG Renamed",
				"enabled": true,
				"events": ["stock.available", "order.success"],
				"config": {"token": "", "chatId": "-100123456"}
			},
			{
				"id": "smtp-1",
				"type": "smtp",
				"name": "Ops SMTP",
				"enabled": true,
				"events": ["order.failed", "mcp.action"],
				"config": {
					"host": "smtp.example.com",
					"port": 587,
					"security": "starttls",
					"username": "ops@example.com",
					"password": "supe****word",
					"fromName": "OVH Nexus",
					"fromAddress": "ops@example.com",
					"recipients": ["a@example.com"]
				}
			}
		]
	}`
	req = httptest.NewRequest(http.MethodPut, "/api/notifications", strings.NewReader(putBodyPreserve))
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("second PUT failed: %d %s", rec.Code, rec.Body.String())
	}

	storedChannels, err := st.ListChannels(context.Background())
	if err != nil {
		t.Fatalf("ListChannels failed: %v", err)
	}
	if storedChannels[0].Config["token"] != "99887766:SECRET_TELEGRAM_TOKEN" {
		t.Fatalf("telegram token was not preserved on masked update: got %q", storedChannels[0].Config["token"])
	}
	if storedChannels[1].Config["password"] != "super-secret-smtp-password" {
		t.Fatalf("smtp password was not preserved on masked update: got %q", storedChannels[1].Config["password"])
	}

	// Test outbound Telegram & SMTP delivery via POST /channels/{id}/test and EmitEvent
	var mu sync.Mutex
	var tgRequests []OutboundTelegramPayload
	var tgPath string
	tgServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var p OutboundTelegramPayload
		_ = json.Unmarshal(body, &p)
		mu.Lock()
		tgPath = r.URL.Path
		tgRequests = append(tgRequests, p)
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer tgServer.Close()
	disp.SetTelegramBaseURL(tgServer.URL)

	var smtpSent []SMTPMessage
	disp.SetSMTPSender(func(_ context.Context, msg SMTPMessage) error {
		mu.Lock()
		smtpSent = append(smtpSent, msg)
		mu.Unlock()
		return nil
	})

	// Test Telegram channel using empty token override (preserves stored token)
	req = httptest.NewRequest(http.MethodPost, "/api/notifications/channels/tg-1/test", strings.NewReader(`{"channel":{"id":"tg-1","type":"telegram","name":"Ops TG","config":{"token":"","chatId":"-100123456"}}}`))
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("test tg channel failed: %d %s", rec.Code, rec.Body.String())
	}
	if tgPath != "/bot99887766:SECRET_TELEGRAM_TOKEN/sendMessage" {
		t.Fatalf("unexpected telegram API path: %s", tgPath)
	}
	if len(tgRequests) != 1 || tgRequests[0].ReplyMarkup == nil {
		t.Fatalf("expected telegram inline CTA keyboard for panelUrl, got %+v", tgRequests)
	}

	// Test EmitEvent routing: EventOrderFailed is only subscribed by smtp-1, not tg-1
	err = disp.EmitEvent(context.Background(), EventOrderFailed, "", "", nil, WithTemplateData(map[string]any{
		"planCode":   "24ska01",
		"modelName":  "KS-A",
		"datacenter": "GRA",
		"error":      "Out of stock",
	}))
	if err != nil {
		t.Fatalf("EmitEvent failed: %v", err)
	}
	if len(tgRequests) != 1 {
		t.Fatalf("tg-1 should NOT have received EventOrderFailed, count=%d", len(tgRequests))
	}
	if len(smtpSent) != 1 || !strings.Contains(string(smtpSent[0].RawMIME), "multipart/alternative") {
		t.Fatalf("expected smtp-1 to receive multipart MIME email, got %d", len(smtpSent))
	}
}

func TestDefaultPlainSMTPDelivery(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen tcp: %v", err)
	}
	defer ln.Close()

	var receivedData strings.Builder
	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, acceptErr := ln.Accept()
		if acceptErr != nil {
			return
		}
		defer conn.Close()

		w := bufio.NewWriter(conn)
		r := bufio.NewReader(conn)
		writeLine := func(s string) {
			_, _ = w.WriteString(s + "\r\n")
			_ = w.Flush()
		}

		writeLine("220 localhost ESMTP test")
		inData := false
		for {
			line, readErr := r.ReadString('\n')
			if readErr != nil {
				return
			}
			trimmed := strings.TrimRight(line, "\r\n")
			if inData {
				if trimmed == "." {
					inData = false
					writeLine("250 OK queued")
					continue
				}
				receivedData.WriteString(line)
				continue
			}
			upper := strings.ToUpper(trimmed)
			switch {
			case strings.HasPrefix(upper, "EHLO"), strings.HasPrefix(upper, "HELO"):
				writeLine("250-localhost")
				writeLine("250 OK")
			case strings.HasPrefix(upper, "MAIL FROM"):
				writeLine("250 OK")
			case strings.HasPrefix(upper, "RCPT TO"):
				writeLine("250 OK")
			case upper == "DATA":
				inData = true
				writeLine("354 End data with <CR><LF>.<CR><LF>")
			case upper == "QUIT":
				writeLine("221 Bye")
				return
			default:
				writeLine("250 OK")
			}
		}
	}()

	host, portStr, _ := net.SplitHostPort(ln.Addr().String())
	port := parsePort(portStr, 25)
	disp, st, _ := newTestDispatcher(t)

	_, err = st.ReplaceChannels(context.Background(), []store.NotificationChannel{
		{
			ID:      "smtp-local",
			Type:    "smtp",
			Name:    "Local Relay",
			Enabled: true,
			Events:  []string{EventOrderSuccess},
			Config: map[string]any{
				"host":        host,
				"port":        port,
				"security":    "none",
				"fromName":    "OVH Nexus",
				"fromAddress": "nexus@example.com",
				"recipients":  []string{"ops@example.com"},
			},
		},
	})
	if err != nil {
		t.Fatalf("ReplaceChannels failed: %v", err)
	}

	if err := disp.EmitEvent(context.Background(), EventOrderSuccess, "", "", nil, WithTemplateData(map[string]any{
		"orderId":    "999001",
		"planCode":   "24ska01",
		"modelName":  "KS-A",
		"datacenter": "GRA",
	})); err != nil {
		t.Fatalf("EmitEvent over plain SMTP failed: %v", err)
	}
	<-done

	raw := receivedData.String()
	if !strings.Contains(raw, "Subject: [OVH Nexus] Order Created #999001: KS-A") {
		t.Fatalf("missing expected Subject in SMTP payload:\n%s", raw)
	}
	if !strings.Contains(raw, "multipart/alternative") {
		t.Fatalf("missing multipart/alternative in SMTP payload")
	}
}
