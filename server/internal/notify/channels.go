package notify

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/mail"
	"net/smtp"
	"net/textproto"
	"strconv"
	"strings"
	"time"
)

const defaultTelegramBaseURL = "https://api.telegram.org"

// OutboundTelegramPayload matches the Telegram Bot sendMessage JSON contract.
type OutboundTelegramPayload struct {
	ChatID      string         `json:"chat_id"`
	Text        string         `json:"text"`
	ParseMode   string         `json:"parse_mode,omitempty"`
	ReplyMarkup map[string]any `json:"reply_markup,omitempty"`
}

func (d *Dispatcher) sendTelegram(ctx context.Context, cfg map[string]any, rendered RenderedNotification) error {
	token := strings.TrimSpace(stringVal(cfg, "token"))
	chatID := strings.TrimSpace(stringVal(cfg, "chatId"))
	if token == "" {
		return errors.New("telegram bot token is not configured")
	}
	if chatID == "" {
		return errors.New("telegram chatId is not configured")
	}

	baseURL := strings.TrimRight(d.telegramBaseURL, "/")
	if baseURL == "" {
		baseURL = defaultTelegramBaseURL
	}
	targetURL := fmt.Sprintf("%s/bot%s/sendMessage", baseURL, token)

	payload := OutboundTelegramPayload{
		ChatID:    chatID,
		Text:      rendered.TelegramText,
		ParseMode: "Markdown",
	}
	if rendered.PanelURL != "" {
		tr := translations[NormalizeLocale(rendered.Locale)]
		payload.ReplyMarkup = map[string]any{
			"inline_keyboard": [][]map[string]string{
				{
					{
						"text": tr.CTAText,
						"url":  rendered.PanelURL,
					},
				},
			},
		}
	}

	status, body, err := d.postTelegramJSON(ctx, targetURL, payload)
	if err == nil && status >= 200 && status < 300 {
		return nil
	}

	// Fallback to plain text if Telegram rejected Markdown entity parsing.
	if err == nil && status == http.StatusBadRequest && strings.Contains(strings.ToLower(body), "parse") {
		payload.ParseMode = ""
		payload.Text = rendered.PlainText
		status, body, err = d.postTelegramJSON(ctx, targetURL, payload)
		if err == nil && status >= 200 && status < 300 {
			return nil
		}
	}

	if err != nil {
		return err
	}
	if len(body) > 200 {
		body = body[:200]
	}
	return fmt.Errorf("telegram API returned HTTP %d: %s", status, strings.TrimSpace(body))
}

func (d *Dispatcher) postTelegramJSON(ctx context.Context, targetURL string, payload OutboundTelegramPayload) (int, string, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return 0, "", fmt.Errorf("marshal telegram payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, targetURL, bytes.NewReader(raw))
	if err != nil {
		return 0, "", fmt.Errorf("build telegram request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "ovh-nexus/1.3.0")

	client := d.httpClient
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, "", fmt.Errorf("telegram HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	return resp.StatusCode, string(respBody), nil
}

// SMTPMessage encapsulates a formatted MIME email passed to the SMTP sender.
type SMTPMessage struct {
	Host       string
	Port       int
	Security   string
	Username   string
	Password   string
	From       string
	Recipients []string
	RawMIME    []byte
}

func (d *Dispatcher) sendSMTP(ctx context.Context, cfg map[string]any, rendered RenderedNotification) error {
	host := sanitizeHeaderValue(strings.TrimSpace(stringVal(cfg, "host")))
	port := parsePort(cfg["port"], 465)
	security := strings.ToLower(strings.TrimSpace(stringVal(cfg, "security")))
	if security == "" {
		security = "ssl"
	}
	username := strings.TrimSpace(stringVal(cfg, "username"))
	password := stringVal(cfg, "password")
	fromName := sanitizeHeaderValue(strings.TrimSpace(stringVal(cfg, "fromName")))
	if fromName == "" {
		fromName = "OVH Nexus"
	}
	fromAddr := sanitizeHeaderValue(strings.TrimSpace(stringVal(cfg, "fromAddress")))
	if fromAddr == "" {
		fromAddr = sanitizeHeaderValue(username)
	}
	recipients := parseRecipients(cfg["recipients"])

	if host == "" {
		return errors.New("smtp host is not configured")
	}
	if fromAddr == "" || !strings.Contains(fromAddr, "@") {
		return errors.New("smtp fromAddress is not configured")
	}
	if len(recipients) == 0 {
		return errors.New("no valid smtp recipients are configured")
	}

	rawMIME, err := buildMIMEMessage(fromName, fromAddr, recipients, rendered)
	if err != nil {
		return err
	}

	msg := SMTPMessage{
		Host:       host,
		Port:       port,
		Security:   security,
		Username:   username,
		Password:   password,
		From:       fromAddr,
		Recipients: recipients,
		RawMIME:    rawMIME,
	}

	if d.smtpSendFn != nil {
		return d.smtpSendFn(ctx, msg)
	}
	return defaultSMTPSend(ctx, msg)
}

func buildMIMEMessage(fromName, fromAddr string, recipients []string, rendered RenderedNotification) ([]byte, error) {
	var body bytes.Buffer
	altWriter := multipart.NewWriter(&body)
	boundary := altWriter.Boundary()

	fromHeader := (&mail.Address{Name: fromName, Address: fromAddr}).String()
	toHeaders := make([]string, 0, len(recipients))
	for _, r := range recipients {
		toHeaders = append(toHeaders, sanitizeHeaderValue(r))
	}

	domain := "ovh-nexus.local"
	if parts := strings.Split(fromAddr, "@"); len(parts) == 2 && parts[1] != "" {
		domain = sanitizeHeaderValue(parts[1])
	}

	var headers strings.Builder
	headers.WriteString("From: " + fromHeader + "\r\n")
	headers.WriteString("To: " + strings.Join(toHeaders, ", ") + "\r\n")
	headers.WriteString("Subject: " + encodeMIMEHeader(sanitizeHeaderValue(rendered.Subject)) + "\r\n")
	headers.WriteString("Date: " + time.Now().In(time.Local).Format(time.RFC1123Z) + "\r\n")
	headers.WriteString("Message-ID: " + generateMessageID(domain) + "\r\n")
	headers.WriteString("MIME-Version: 1.0\r\n")
	headers.WriteString("Content-Type: multipart/alternative; boundary=\"" + boundary + "\"\r\n")
	headers.WriteString("\r\n")

	plainHeader := make(textproto.MIMEHeader)
	plainHeader.Set("Content-Type", "text/plain; charset=utf-8")
	plainHeader.Set("Content-Transfer-Encoding", "base64")
	pw, err := altWriter.CreatePart(plainHeader)
	if err != nil {
		return nil, fmt.Errorf("create plain MIME part: %w", err)
	}
	_, _ = pw.Write([]byte(wrapBase64(rendered.PlainText)))

	htmlHeader := make(textproto.MIMEHeader)
	htmlHeader.Set("Content-Type", "text/html; charset=utf-8")
	htmlHeader.Set("Content-Transfer-Encoding", "base64")
	hw, err := altWriter.CreatePart(htmlHeader)
	if err != nil {
		return nil, fmt.Errorf("create html MIME part: %w", err)
	}
	_, _ = hw.Write([]byte(wrapBase64(rendered.HTMLBody)))

	if err := altWriter.Close(); err != nil {
		return nil, fmt.Errorf("close multipart MIME writer: %w", err)
	}

	var out bytes.Buffer
	out.WriteString(headers.String())
	out.Write(body.Bytes())
	return out.Bytes(), nil
}

func defaultSMTPSend(ctx context.Context, msg SMTPMessage) error {
	addr := net.JoinHostPort(msg.Host, strconv.Itoa(msg.Port))
	dialTimeout := 15 * time.Second
	if dl, ok := ctx.Deadline(); ok {
		if rem := time.Until(dl); rem > 0 && rem < dialTimeout {
			dialTimeout = rem
		}
	}

	dialer := &net.Dialer{Timeout: dialTimeout}
	tlsCfg := &tls.Config{
		ServerName: msg.Host,
		MinVersion: tls.VersionTLS12,
	}

	var client *smtp.Client
	var err error

	switch msg.Security {
	case "ssl", "tls":
		conn, dialErr := (&tls.Dialer{NetDialer: dialer, Config: tlsCfg}).DialContext(ctx, "tcp", addr)
		if dialErr != nil {
			return fmt.Errorf("smtp ssl dial %s: %w", addr, dialErr)
		}
		client, err = smtp.NewClient(conn, msg.Host)
		if err != nil {
			_ = conn.Close()
			return fmt.Errorf("smtp ssl handshake %s: %w", addr, err)
		}
	case "starttls":
		conn, dialErr := dialer.DialContext(ctx, "tcp", addr)
		if dialErr != nil {
			return fmt.Errorf("smtp starttls dial %s: %w", addr, dialErr)
		}
		client, err = smtp.NewClient(conn, msg.Host)
		if err != nil {
			_ = conn.Close()
			return fmt.Errorf("smtp client init %s: %w", addr, err)
		}
		if err := client.Hello("ovh-nexus.local"); err != nil {
			_ = client.Close()
			return fmt.Errorf("smtp ehlo: %w", err)
		}
		if ok, _ := client.Extension("STARTTLS"); ok {
			if err := client.StartTLS(tlsCfg); err != nil {
				_ = client.Close()
				return fmt.Errorf("smtp starttls upgrade: %w", err)
			}
		} else {
			_ = client.Close()
			return errors.New("smtp server does not advertise STARTTLS")
		}
	default: // "none" / plain
		conn, dialErr := dialer.DialContext(ctx, "tcp", addr)
		if dialErr != nil {
			return fmt.Errorf("smtp plain dial %s: %w", addr, dialErr)
		}
		client, err = smtp.NewClient(conn, msg.Host)
		if err != nil {
			_ = conn.Close()
			return fmt.Errorf("smtp plain client init %s: %w", addr, err)
		}
		if err := client.Hello("ovh-nexus.local"); err != nil {
			_ = client.Close()
			return fmt.Errorf("smtp ehlo: %w", err)
		}
	}
	defer func() { _ = client.Close() }()

	if msg.Username != "" && msg.Password != "" {
		if ok, _ := client.Extension("AUTH"); ok {
			var auth smtp.Auth
			if msg.Security == "none" || msg.Security == "plain" {
				auth = &plainAuthUnencrypted{username: msg.Username, password: msg.Password}
			} else {
				auth = smtp.PlainAuth("", msg.Username, msg.Password, msg.Host)
			}
			if err := client.Auth(auth); err != nil {
				return fmt.Errorf("smtp authentication failed: %w", err)
			}
		}
	}

	if err := client.Mail(msg.From); err != nil {
		return fmt.Errorf("smtp MAIL FROM failed: %w", err)
	}
	for _, rcpt := range msg.Recipients {
		if err := client.Rcpt(rcpt); err != nil {
			return fmt.Errorf("smtp RCPT TO %q failed: %w", rcpt, err)
		}
	}

	w, err := client.Data()
	if err != nil {
		return fmt.Errorf("smtp DATA command failed: %w", err)
	}
	if _, err := w.Write(msg.RawMIME); err != nil {
		_ = w.Close()
		return fmt.Errorf("smtp write body failed: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("smtp finish body failed: %w", err)
	}

	return client.Quit()
}

type plainAuthUnencrypted struct {
	username string
	password string
}

func (a *plainAuthUnencrypted) Start(_ *smtp.ServerInfo) (string, []byte, error) {
	resp := []byte("\x00" + a.username + "\x00" + a.password)
	return "PLAIN", resp, nil
}

func (a *plainAuthUnencrypted) Next(_ []byte, more bool) ([]byte, error) {
	if more {
		return nil, errors.New("unexpected server challenge during PLAIN auth")
	}
	return nil, nil
}

func parseRecipients(raw any) []string {
	var items []string
	switch v := raw.(type) {
	case string:
		norm := strings.ReplaceAll(v, ";", ",")
		for _, part := range strings.Split(norm, ",") {
			if s := sanitizeHeaderValue(strings.TrimSpace(part)); s != "" && strings.Contains(s, "@") {
				items = append(items, s)
			}
		}
	case []string:
		for _, part := range v {
			if s := sanitizeHeaderValue(strings.TrimSpace(part)); s != "" && strings.Contains(s, "@") {
				items = append(items, s)
			}
		}
	case []any:
		for _, part := range v {
			if s := sanitizeHeaderValue(strings.TrimSpace(fmt.Sprint(part))); s != "" && strings.Contains(s, "@") {
				items = append(items, s)
			}
		}
	}
	return items
}

func parsePort(raw any, fallback int) int {
	switch v := raw.(type) {
	case int:
		if v > 0 && v <= 65535 {
			return v
		}
	case int64:
		if v > 0 && v <= 65535 {
			return int(v)
		}
	case float64:
		if int(v) > 0 && int(v) <= 65535 {
			return int(v)
		}
	case string:
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && n > 0 && n <= 65535 {
			return n
		}
	}
	return fallback
}

func stringVal(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	v, ok := m[key]
	if !ok || v == nil {
		return ""
	}
	s, ok := v.(string)
	if ok {
		return s
	}
	return fmt.Sprint(v)
}

func sanitizeHeaderValue(s string) string {
	return strings.NewReplacer("\r", "", "\n", "").Replace(s)
}

func encodeMIMEHeader(s string) string {
	for i := 0; i < len(s); i++ {
		if s[i] > 127 {
			return "=?UTF-8?B?" + base64.StdEncoding.EncodeToString([]byte(s)) + "?="
		}
	}
	return s
}

func wrapBase64(s string) string {
	enc := base64.StdEncoding.EncodeToString([]byte(s))
	var sb strings.Builder
	for i := 0; i < len(enc); i += 76 {
		end := i + 76
		if end > len(enc) {
			end = len(enc)
		}
		sb.WriteString(enc[i:end])
		sb.WriteString("\r\n")
	}
	return sb.String()
}

func generateMessageID(domain string) string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return fmt.Sprintf("<%d.%x@%s>", time.Now().UnixNano(), b, domain)
}
