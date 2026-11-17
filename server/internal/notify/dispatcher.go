package notify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"ovh-nexus/server/internal/logger"
	"ovh-nexus/server/internal/ovhclient"
	"ovh-nexus/server/internal/store"
	"ovh-nexus/server/internal/vault"
)

// PublicState represents the redacted Notification Center state exposed to the WebUI.
type PublicState struct {
	Channels        []store.NotificationChannel `json:"channels"`
	PanelURL        string                      `json:"panelUrl"`
	Locale          string                      `json:"locale"`
	Rules           store.NotificationRules     `json:"rules"`
	HasAPIKey       bool                        `json:"hasApiKey"`
	SupportedEvents []EventDefinition           `json:"supportedEvents,omitempty"`
}

// UpdateStateRequest is the JSON payload accepted by PUT /api/notifications.
type UpdateStateRequest struct {
	Channels []store.NotificationChannel `json:"channels"`
	PanelURL *string                     `json:"panelUrl"`
	Locale   *string                     `json:"locale"`
	Rules    *store.NotificationRules    `json:"rules"`
}

// Dispatcher manages outbound notifications across Telegram and SMTP channels.
type Dispatcher struct {
	store           *store.Store
	vault           *vault.Vault
	log             *logger.Hub
	ovhPool         *ovhclient.Pool
	httpClient      *http.Client
	telegramBaseURL string
	smtpSendFn      func(ctx context.Context, msg SMTPMessage) error
}

// NewDispatcher constructs a Notification Dispatcher backed by SQLite, Vault, and Logger Hub.
func NewDispatcher(st *store.Store, v *vault.Vault, log *logger.Hub, ovhPool *ovhclient.Pool) *Dispatcher {
	var client *http.Client
	if ovhPool != nil {
		client = ovhPool.PublicHTTPClient()
	}
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	return &Dispatcher{
		store:           st,
		vault:           v,
		log:             log,
		ovhPool:         ovhPool,
		httpClient:      client,
		telegramBaseURL: defaultTelegramBaseURL,
	}
}

// SetHTTPClient overrides the HTTP client used for outbound Telegram requests (for testing).
func (d *Dispatcher) SetHTTPClient(c *http.Client) {
	if c != nil {
		d.httpClient = c
	}
}

// SetTelegramBaseURL overrides the Telegram API origin (for testing).
func (d *Dispatcher) SetTelegramBaseURL(u string) {
	if strings.TrimSpace(u) != "" {
		d.telegramBaseURL = strings.TrimRight(strings.TrimSpace(u), "/")
	}
}

// SetSMTPSender overrides the low-level SMTP transport function (for testing).
func (d *Dispatcher) SetSMTPSender(fn func(ctx context.Context, msg SMTPMessage) error) {
	d.smtpSendFn = fn
}

// MountRoutes registers all Notification Center REST routes on an /api chi.Router.
func (d *Dispatcher) MountRoutes(r chi.Router) {
	r.Get("/notifications/events", d.HandleGetEvents)
	r.Get("/notifications", d.HandleGetSettings)
	r.Get("/notifications/settings", d.HandleGetSettings)
	r.Put("/notifications", d.HandleUpdateSettings)
	r.Post("/notifications", d.HandleUpdateSettings)
	r.Put("/notifications/settings", d.HandleUpdateSettings)
	r.Post("/notifications/settings", d.HandleUpdateSettings)
	r.Post("/notifications/channels/{id}/test", d.HandleTestChannel)
}

// HandleGetEvents handles GET /api/notifications/events.
func (d *Dispatcher) HandleGetEvents(w http.ResponseWriter, _ *http.Request) {
	events := AllEvents()
	writeJSON(w, http.StatusOK, map[string]any{
		"events":          events,
		"supportedEvents": events,
	})
}

// HandleGetSettings handles GET /api/notifications (and /api/notifications/settings).
func (d *Dispatcher) HandleGetSettings(w http.ResponseWriter, r *http.Request) {
	state, err := d.GetPublicState(r.Context())
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "NOTIF_LOAD_FAILED", "Failed to load notification settings.")
		return
	}
	writeJSON(w, http.StatusOK, state)
}

// HandleUpdateSettings handles PUT/POST /api/notifications.
func (d *Dispatcher) HandleUpdateSettings(w http.ResponseWriter, r *http.Request) {
	var raw map[string]json.RawMessage
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&raw); err != nil {
		writeJSONError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid notification configuration JSON payload.")
		return
	}

	var req UpdateStateRequest
	if b, ok := raw["channels"]; ok {
		if err := json.Unmarshal(b, &req.Channels); err != nil {
			writeJSONError(w, http.StatusBadRequest, "INVALID_CHANNELS", "Invalid notification channels payload.")
			return
		}
		req.Channels = normalizeIncomingChannels(req.Channels)
	}
	if b, ok := raw["panelUrl"]; ok {
		var p string
		if json.Unmarshal(b, &p) == nil {
			req.PanelURL = &p
		}
	}
	if b, ok := raw["locale"]; ok {
		var loc string
		if json.Unmarshal(b, &loc) == nil {
			req.Locale = &loc
		}
	}
	if b, ok := raw["rules"]; ok {
		var rules store.NotificationRules
		if json.Unmarshal(b, &rules) == nil {
			req.Rules = &rules
		}
	}

	_, hasChannels := raw["channels"]
	updated, err := d.updateState(r.Context(), req, hasChannels)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "NOTIF_SAVE_FAILED", "Failed to save notification settings.")
		return
	}
	if d.log != nil {
		d.log.Info(logger.SourceNotify, "Notification center settings updated", "channels", len(updated.Channels), "locale", updated.Locale)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"status":  "success",
		"data":    updated,
	})
}

// HandleTestChannel handles POST /api/notifications/channels/{id}/test.
func (d *Dispatcher) HandleTestChannel(w http.ResponseWriter, r *http.Request) {
	channelID := strings.TrimSpace(chi.URLParam(r, "id"))
	var body struct {
		Channel *store.NotificationChannel `json:"channel"`
	}
	_ = json.NewDecoder(io.LimitReader(r.Body, 512<<10)).Decode(&body)

	if err := d.TestChannel(r.Context(), channelID, body.Channel); err != nil {
		if d.log != nil {
			d.log.Warn(logger.SourceNotify, "Notification channel test failed", "channelId", channelID, "error", err.Error())
		}
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"success": false,
			"status":  "error",
			"message": err.Error(),
		})
		return
	}

	if d.log != nil {
		d.log.Info(logger.SourceNotify, "Notification channel test succeeded", "channelId", channelID)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"status":  "success",
		"message": "Test notification dispatched successfully.",
	})
}

// GetPublicState returns redacted channels, settings, supported events, and hasApiKey status.
func (d *Dispatcher) GetPublicState(ctx context.Context) (*PublicState, error) {
	if d == nil || d.store == nil {
		return nil, errors.New("notify dispatcher store is nil")
	}

	settings, err := d.store.GetNotificationSettings(ctx)
	if err != nil {
		return nil, err
	}
	if settings.Rules.SKUInterval == 600 || settings.Rules.SKUInterval < 60 {
		settings.Rules.SKUInterval = 120
		if saved, saveErr := d.store.SaveNotificationSettings(ctx, *settings); saveErr == nil && saved != nil {
			settings = saved
		}
	}
	channels, err := d.store.ListChannels(ctx)
	if err != nil {
		return nil, err
	}

	redacted := make([]store.NotificationChannel, 0, len(channels))
	for _, ch := range channels {
		redacted = append(redacted, ch.Redacted())
	}

	return &PublicState{
		Channels:        redacted,
		PanelURL:        settings.PanelURL,
		Locale:          NormalizeLocale(settings.Locale),
		Rules:           settings.Rules,
		HasAPIKey:       d.hasActiveOVHKey(ctx),
		SupportedEvents: AllEvents(),
	}, nil
}

// UpdateState updates notification settings and channels while preserving masked secrets.
func (d *Dispatcher) UpdateState(ctx context.Context, req UpdateStateRequest) (*PublicState, error) {
	return d.updateState(ctx, req, req.Channels != nil)
}

func (d *Dispatcher) updateState(ctx context.Context, req UpdateStateRequest, replaceChannels bool) (*PublicState, error) {
	if d == nil || d.store == nil {
		return nil, errors.New("notify dispatcher store is nil")
	}

	current, err := d.store.GetNotificationSettings(ctx)
	if err != nil {
		return nil, err
	}
	next := *current
	if req.PanelURL != nil {
		next.PanelURL = strings.TrimSpace(*req.PanelURL)
	}
	if req.Locale != nil {
		next.Locale = NormalizeLocale(*req.Locale)
	}
	if req.Rules != nil {
		next.Rules = normalizeRules(*req.Rules)
	}

	if _, err := d.store.SaveNotificationSettings(ctx, next); err != nil {
		return nil, err
	}

	if replaceChannels {
		norm := normalizeIncomingChannels(req.Channels)
		saved, err := d.store.ReplaceChannels(ctx, norm)
		if err != nil {
			return nil, err
		}
		if d.log != nil {
			for _, ch := range saved {
				registerChannelSecrets(d.log, ch)
			}
		}
	}

	return d.GetPublicState(ctx)
}

// TestChannel dispatches a localized test message to the specified channel (with optional unsaved form overrides).
func (d *Dispatcher) TestChannel(ctx context.Context, channelID string, override *store.NotificationChannel) error {
	if d == nil || d.store == nil {
		return errors.New("notify dispatcher store is nil")
	}

	settings, err := d.store.GetNotificationSettings(ctx)
	if err != nil {
		return err
	}
	channels, err := d.store.ListChannels(ctx)
	if err != nil {
		return err
	}

	var stored *store.NotificationChannel
	for i := range channels {
		if channels[i].ID == channelID {
			stored = &channels[i]
			break
		}
	}

	var target *store.NotificationChannel
	if override != nil {
		merged := mergeTestChannel(channelID, stored, override)
		target = &merged
	} else {
		target = stored
	}
	if target == nil {
		return errors.New("notification channel not found")
	}
	if d.log != nil {
		registerChannelSecrets(d.log, *target)
	}

	loc := NormalizeLocale(settings.Locale)
	tr := translations[loc]
	fields := []Field{
		{Label: tr.TestChannelLbl, Value: target.Name},
		{Label: tr.TestTypeLbl, Value: strings.ToUpper(target.Type)},
	}
	rendered := RenderNotification(loc, EventStockAvailable, tr.TestTitle, tr.TestBody, fields, settings.PanelURL, nil, time.Now())

	return d.sendToChannel(ctx, *target, rendered)
}

// EmitEvent dispatches a notification event to all enabled channels subscribed to eventType.
func (d *Dispatcher) EmitEvent(
	ctx context.Context,
	eventType string,
	title string,
	plainText string,
	fields []Field,
	extra ...EventOption,
) error {
	if d == nil || d.store == nil {
		return errors.New("notify dispatcher is not initialized")
	}
	if !IsValidEvent(eventType) {
		return fmt.Errorf("unsupported notification event type %q", eventType)
	}

	var cfg eventConfig
	for _, opt := range extra {
		if opt != nil {
			opt(&cfg)
		}
	}

	settings, err := d.store.GetNotificationSettings(ctx)
	if err != nil {
		return err
	}
	channels, err := d.store.ListChannels(ctx)
	if err != nil {
		return err
	}

	rendered := RenderNotification(
		settings.Locale,
		eventType,
		title,
		plainText,
		fields,
		settings.PanelURL,
		cfg.templateData,
		cfg.timestamp,
	)

	matched := 0
	var errs []error
	for _, ch := range channels {
		if !ch.Enabled {
			continue
		}
		if !slices.Contains(ch.Events, eventType) {
			continue
		}
		matched++
		if d.log != nil {
			registerChannelSecrets(d.log, ch)
		}
		if err := d.sendToChannel(ctx, ch, rendered); err != nil {
			errs = append(errs, fmt.Errorf("%s (%s): %w", ch.ID, ch.Type, err))
			if d.log != nil {
				d.log.Error(logger.SourceNotify, "Failed to dispatch notification", "channelId", ch.ID, "type", ch.Type, "event", eventType, "error", err.Error())
			}
		} else if d.log != nil {
			d.log.Info(logger.SourceNotify, "Notification dispatched", "channelId", ch.ID, "type", ch.Type, "event", eventType)
		}
	}

	if matched == 0 && d.log != nil {
		d.log.Info(logger.SourceNotify, "Skipped notification dispatch: no enabled channel subscribed", "event", eventType)
	}
	if len(errs) > 0 && len(errs) == matched {
		return errors.Join(errs...)
	}
	return nil
}

func (d *Dispatcher) sendToChannel(ctx context.Context, ch store.NotificationChannel, rendered RenderedNotification) error {
	switch strings.ToLower(strings.TrimSpace(ch.Type)) {
	case "telegram":
		return d.sendTelegram(ctx, ch.Config, rendered)
	case "smtp":
		return d.sendSMTP(ctx, ch.Config, rendered)
	default:
		return fmt.Errorf("unsupported notification channel type %q", ch.Type)
	}
}

func (d *Dispatcher) hasActiveOVHKey(ctx context.Context) bool {
	if d.store == nil {
		return false
	}
	acc, err := d.store.GetActiveAccount(ctx, "")
	if err != nil || acc == nil {
		return false
	}
	return strings.TrimSpace(acc.AppKey) != "" &&
		strings.TrimSpace(acc.AppSecret) != "" &&
		strings.TrimSpace(acc.ConsumerKey) != ""
}

func normalizeIncomingChannels(in []store.NotificationChannel) []store.NotificationChannel {
	out := make([]store.NotificationChannel, 0, len(in))
	for _, ch := range in {
		ch.Type = strings.ToLower(strings.TrimSpace(ch.Type))
		if ch.Type != "telegram" && ch.Type != "smtp" {
			continue
		}
		ch.ID = strings.TrimSpace(ch.ID)
		ch.Name = strings.TrimSpace(ch.Name)
		if ch.Name == "" {
			ch.Name = strings.ToUpper(ch.Type)
		}
		validEvents := make([]string, 0, len(ch.Events))
		for _, ev := range ch.Events {
			if IsValidEvent(ev) && !slices.Contains(validEvents, ev) {
				validEvents = append(validEvents, ev)
			}
		}
		ch.Events = validEvents
		if ch.Config == nil {
			ch.Config = make(map[string]any)
		}
		if ch.Type == "smtp" {
			if rcpts, ok := ch.Config["recipients"]; ok {
				ch.Config["recipients"] = parseRecipients(rcpts)
			}
			ch.Config["port"] = parsePort(ch.Config["port"], 465)
			sec := strings.ToLower(strings.TrimSpace(stringVal(ch.Config, "security")))
			if sec != "ssl" && sec != "starttls" && sec != "none" {
				sec = "ssl"
			}
			ch.Config["security"] = sec
		}
		out = append(out, ch)
	}
	return out
}

func normalizeRules(r store.NotificationRules) store.NotificationRules {
	if r.SKUInterval < 60 || r.SKUInterval == 600 {
		r.SKUInterval = 120
	}
	if r.ExpiryDays < 1 {
		r.ExpiryDays = 7
	} else if r.ExpiryDays > 90 {
		r.ExpiryDays = 90
	}
	if len(r.SKURanges) == 0 {
		r.SKURanges = store.DefaultNotificationRules().SKURanges
	} else {
		norm := make([]string, 0, len(r.SKURanges))
		for _, rg := range r.SKURanges {
			up := strings.ToUpper(strings.TrimSpace(rg))
			if up != "" && !slices.Contains(norm, up) {
				norm = append(norm, up)
			}
		}
		if len(norm) == 0 {
			norm = store.DefaultNotificationRules().SKURanges
		}
		r.SKURanges = norm
	}
	return r
}

func mergeTestChannel(channelID string, stored *store.NotificationChannel, override *store.NotificationChannel) store.NotificationChannel {
	chType := strings.ToLower(strings.TrimSpace(override.Type))
	if chType == "" && stored != nil {
		chType = stored.Type
	}
	mergedCfg := make(map[string]any)
	if stored != nil && stored.Config != nil {
		maps.Copy(mergedCfg, stored.Config)
	}
	for k, v := range override.Config {
		if (chType == "telegram" && k == "token") || (chType == "smtp" && k == "password") {
			if str, ok := v.(string); ok && !vault.IsMaskedPlaceholder(str) {
				mergedCfg[k] = strings.TrimSpace(str)
			}
			continue
		}
		mergedCfg[k] = v
	}
	name := strings.TrimSpace(override.Name)
	if name == "" && stored != nil {
		name = stored.Name
	}
	if name == "" {
		name = strings.ToUpper(chType)
	}
	return store.NotificationChannel{
		ID:      channelID,
		Type:    chType,
		Name:    name,
		Enabled: true,
		Events:  override.Events,
		Config:  mergedCfg,
	}
}

func registerChannelSecrets(log *logger.Hub, ch store.NotificationChannel) {
	if log == nil || ch.Config == nil {
		return
	}
	if ch.Type == "telegram" {
		if tok := strings.TrimSpace(stringVal(ch.Config, "token")); tok != "" {
			log.AddSensitiveValue(tok)
		}
	}
	if ch.Type == "smtp" {
		if pwd := stringVal(ch.Config, "password"); pwd != "" {
			log.AddSensitiveValue(pwd)
		}
	}
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeJSONError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{
		"success": false,
		"error":   http.StatusText(status),
		"code":    code,
		"message": message,
	})
}
