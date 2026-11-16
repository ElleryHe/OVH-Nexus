package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"strings"

	"ovh-nexus/server/internal/vault"
)

// NotificationRules defines SKU watcher and service expiration notification parameters.
type NotificationRules struct {
	SKUEnabled       bool     `json:"skuEnabled"`
	SKUInterval      int      `json:"skuInterval"`
	SKUTrackConfigs  bool     `json:"skuTrackConfigs"`
	SKURanges        []string `json:"skuRanges"`
	SKUMergeRegional bool     `json:"skuMergeRegional"`
	ExpiryDays       int      `json:"expiryDays"`
}

// NotificationSettings stores global notification center preferences.
type NotificationSettings struct {
	PanelURL string            `json:"panelUrl"`
	Locale   string            `json:"locale"`
	Rules    NotificationRules `json:"rules"`
}

// NotificationChannel represents an outbound notification provider (telegram or smtp).
type NotificationChannel struct {
	ID         string          `json:"id"`
	Type       string          `json:"type"`
	Name       string          `json:"name"`
	Enabled    bool            `json:"enabled"`
	Events     []string        `json:"events"`
	Config     map[string]any  `json:"config"`
	HasSecrets map[string]bool `json:"hasSecrets,omitempty"`
	CreatedAt  string          `json:"createdAt,omitempty"`
	UpdatedAt  string          `json:"updatedAt,omitempty"`
}

// Redacted returns a deep copy of the channel with secret fields masked for API responses.
func (c NotificationChannel) Redacted() NotificationChannel {
	out := c
	out.Events = append([]string(nil), c.Events...)
	out.Config = make(map[string]any, len(c.Config))
	maps.Copy(out.Config, c.Config)
	out.HasSecrets = make(map[string]bool)

	for _, sk := range secretKeysForChannelType(c.Type) {
		raw, _ := out.Config[sk].(string)
		out.HasSecrets[sk] = raw != ""
		if raw != "" {
			out.Config[sk] = vault.MaskSecret(raw)
		} else {
			out.Config[sk] = ""
		}
	}
	return out
}

// MCPScopes represents enabled MCP tool permission levels.
type MCPScopes struct {
	Read        bool `json:"read"`
	Procurement bool `json:"procurement"`
	Operations  bool `json:"operations"`
}

// MCPConfig persists MCP Gateway settings and SHA-256 hashed Bearer token.
type MCPConfig struct {
	Enabled           bool      `json:"enabled"`
	TokenHash         string    `json:"tokenHash"`
	MaskedToken       string    `json:"maskedToken"`
	Scopes            MCPScopes `json:"scopes"`
	AllowAllAccounts  bool      `json:"allowAllAccounts"`
	AllowedAccountIDs []string  `json:"allowedAccountIds"`
	HourlyOrderLimit  int       `json:"hourlyOrderLimit"`
	NotifyOnMutation  bool      `json:"notifyOnMutation"`
	CreatedAt         string    `json:"createdAt,omitempty"`
	UpdatedAt         string    `json:"updatedAt,omitempty"`
	TokenRotatedAt    string    `json:"tokenRotatedAt,omitempty"`
}

// MCPAliasState stores the bidirectional Privacy Shield alias maps and counters.
type MCPAliasState struct {
	Forward  map[string]map[string]string         `json:"forward"`
	Reverse  map[string]map[string]string         `json:"reverse"`
	Meta     map[string]map[string]map[string]any `json:"meta"`
	Counters map[string]int                       `json:"counters"`
}

// MCPAuditLog records an MCP tool execution event.
type MCPAuditLog struct {
	ID        string         `json:"id"`
	Timestamp string         `json:"timestamp"`
	Tool      string         `json:"tool"`
	Scope     string         `json:"scope"`
	Arguments map[string]any `json:"arguments"`
	Status    string         `json:"status"`
	Summary   string         `json:"summary"`
	ClientIP  string         `json:"clientIp"`
}

// LogRow represents a stored structured log entry in system_logs.
type LogRow struct {
	ID        int64  `json:"id"`
	Timestamp string `json:"timestamp"`
	Level     string `json:"level"`
	Source    string `json:"source"`
	Message   string `json:"message"`
	AttrsJSON string `json:"attrsJson"`
}

func DefaultNotificationRules() NotificationRules {
	return NotificationRules{
		SKUEnabled:       true,
		SKUInterval:      600,
		SKUTrackConfigs:  false,
		SKURanges:        []string{"KS", "RISE", "SYS", "ADV", "SCALE", "HGR"},
		SKUMergeRegional: true,
		ExpiryDays:       7,
	}
}

func secretKeysForChannelType(chType string) []string {
	switch strings.ToLower(strings.TrimSpace(chType)) {
	case "telegram":
		return []string{"token"}
	case "smtp":
		return []string{"password"}
	default:
		return nil
	}
}

// ==================== notification_channels & notified_markers ====================

func (s *Store) decryptChannelRow(id, chType, name string, enabledInt int, eventsJSON, cfgJSON, createdAt, updatedAt string) (*NotificationChannel, error) {
	var events []string
	_ = json.Unmarshal([]byte(eventsJSON), &events)
	if events == nil {
		events = []string{}
	}

	cfg := make(map[string]any)
	if cfgJSON != "" {
		_ = json.Unmarshal([]byte(cfgJSON), &cfg)
	}

	hasSecrets := make(map[string]bool)
	for _, sk := range secretKeysForChannelType(chType) {
		if raw, ok := cfg[sk].(string); ok && raw != "" {
			pt, err := s.vault.Decrypt(raw)
			if err != nil {
				return nil, fmt.Errorf("store: decrypt channel %q secret %q: %w", id, sk, err)
			}
			cfg[sk] = pt
			hasSecrets[sk] = pt != ""
		} else {
			hasSecrets[sk] = false
		}
	}

	return &NotificationChannel{
		ID:         id,
		Type:       chType,
		Name:       name,
		Enabled:    enabledInt == 1,
		Events:     events,
		Config:     cfg,
		HasSecrets: hasSecrets,
		CreatedAt:  createdAt,
		UpdatedAt:  updatedAt,
	}, nil
}

// ListChannels loads all notification channels with decrypted secrets in memory.
func (s *Store) ListChannels(ctx context.Context) ([]NotificationChannel, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, type, name, enabled, events_json, config_json, created_at, updated_at
		FROM notification_channels
		ORDER BY created_at ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("store: list channels: %w", err)
	}
	defer rows.Close()

	channels := make([]NotificationChannel, 0)
	for rows.Next() {
		var id, chType, name, eventsJSON, cfgJSON, createdAt, updatedAt string
		var enabledInt int
		if err := rows.Scan(&id, &chType, &name, &enabledInt, &eventsJSON, &cfgJSON, &createdAt, &updatedAt); err != nil {
			return nil, fmt.Errorf("store: scan channel: %w", err)
		}
		ch, err := s.decryptChannelRow(id, chType, name, enabledInt, eventsJSON, cfgJSON, createdAt, updatedAt)
		if err != nil {
			return nil, err
		}
		channels = append(channels, *ch)
	}
	return channels, rows.Err()
}

// UpsertChannel encrypts channel secrets (preserving existing secrets if incoming value is masked/empty)
// and persists the notification channel.
func (s *Store) UpsertChannel(ctx context.Context, ch NotificationChannel) (*NotificationChannel, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("store: begin upsert channel tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	saved, err := s.upsertChannelTx(ctx, tx, ch)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("store: commit upsert channel: %w", err)
	}
	return saved, nil
}

// ReplaceChannels atomically replaces all notification channels while preserving existing secrets
// for matching channel IDs when incoming secrets are empty or masked placeholders.
func (s *Store) ReplaceChannels(ctx context.Context, channels []NotificationChannel) ([]NotificationChannel, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("store: begin replace channels tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Snapshot existing config_json by ID before clearing so masked secrets are preserved.
	existingCfg := make(map[string]map[string]any)
	existingCreated := make(map[string]string)
	rows, err := tx.QueryContext(ctx, `SELECT id, config_json, created_at FROM notification_channels`)
	if err != nil {
		return nil, fmt.Errorf("store: snapshot channels: %w", err)
	}
	for rows.Next() {
		var id, cfgJSON, createdAt string
		if err := rows.Scan(&id, &cfgJSON, &createdAt); err != nil {
			_ = rows.Close()
			return nil, err
		}
		var m map[string]any
		_ = json.Unmarshal([]byte(cfgJSON), &m)
		existingCfg[id] = m
		existingCreated[id] = createdAt
	}
	_ = rows.Close()

	if _, err := tx.ExecContext(ctx, `DELETE FROM notification_channels`); err != nil {
		return nil, fmt.Errorf("store: clear channels: %w", err)
	}

	out := make([]NotificationChannel, 0, len(channels))
	for _, ch := range channels {
		if ch.ID != "" {
			if prev := existingCfg[ch.ID]; prev != nil {
				if ch.Config == nil {
					ch.Config = make(map[string]any)
				}
				for _, sk := range secretKeysForChannelType(ch.Type) {
					inVal, _ := ch.Config[sk].(string)
					if vault.IsMaskedPlaceholder(inVal) {
						if prevEnc, ok := prev[sk].(string); ok && prevEnc != "" {
							pt, decErr := s.vault.Decrypt(prevEnc)
							if decErr == nil {
								ch.Config[sk] = pt
							}
						}
					}
				}
			}
			if ch.CreatedAt == "" {
				ch.CreatedAt = existingCreated[ch.ID]
			}
		}
		saved, err := s.upsertChannelTx(ctx, tx, ch)
		if err != nil {
			return nil, err
		}
		out = append(out, *saved)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("store: commit replace channels: %w", err)
	}
	return out, nil
}

func (s *Store) upsertChannelTx(ctx context.Context, tx *sql.Tx, ch NotificationChannel) (*NotificationChannel, error) {
	ch.Type = strings.ToLower(strings.TrimSpace(ch.Type))
	if ch.Type == "" {
		return nil, errors.New("store: notification channel type is required")
	}
	ch.ID = strings.TrimSpace(ch.ID)
	if ch.ID == "" {
		ch.ID = ch.Type + "-" + newUUID()[:8]
	}
	if ch.Events == nil {
		ch.Events = []string{}
	}
	cfgPlain := make(map[string]any, len(ch.Config))
	maps.Copy(cfgPlain, ch.Config)

	var prevCfgJSON, prevCreated string
	err := tx.QueryRowContext(ctx, `SELECT config_json, created_at FROM notification_channels WHERE id = ?`, ch.ID).
		Scan(&prevCfgJSON, &prevCreated)
	hasPrev := err == nil
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	var prevCfg map[string]any
	if hasPrev && prevCfgJSON != "" {
		_ = json.Unmarshal([]byte(prevCfgJSON), &prevCfg)
	}

	cfgEnc := make(map[string]any, len(cfgPlain))
	maps.Copy(cfgEnc, cfgPlain)
	hasSecrets := make(map[string]bool)

	for _, sk := range secretKeysForChannelType(ch.Type) {
		inVal, _ := cfgPlain[sk].(string)
		if hasPrev && vault.IsMaskedPlaceholder(inVal) {
			if prevEnc, ok := prevCfg[sk].(string); ok && prevEnc != "" {
				pt, decErr := s.vault.Decrypt(prevEnc)
				if decErr != nil {
					return nil, fmt.Errorf("store: decrypt existing secret %q: %w", sk, decErr)
				}
				cfgPlain[sk] = pt
				cfgEnc[sk] = prevEnc
				hasSecrets[sk] = pt != ""
				continue
			}
		}
		trimmed := strings.TrimSpace(inVal)
		cfgPlain[sk] = trimmed
		hasSecrets[sk] = trimmed != ""
		if trimmed != "" {
			encVal, encErr := s.vault.Encrypt(trimmed)
			if encErr != nil {
				return nil, fmt.Errorf("store: encrypt channel secret %q: %w", sk, encErr)
			}
			cfgEnc[sk] = encVal
		} else {
			cfgEnc[sk] = ""
		}
	}

	now := nowUTC()
	if hasPrev && prevCreated != "" {
		ch.CreatedAt = prevCreated
	} else if ch.CreatedAt == "" {
		ch.CreatedAt = now
	}
	ch.UpdatedAt = now

	eventsBytes, _ := json.Marshal(ch.Events)
	cfgBytes, _ := json.Marshal(cfgEnc)

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO notification_channels (id, type, name, enabled, events_json, config_json, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			type = excluded.type,
			name = excluded.name,
			enabled = excluded.enabled,
			events_json = excluded.events_json,
			config_json = excluded.config_json,
			updated_at = excluded.updated_at
	`, ch.ID, ch.Type, ch.Name, boolToInt(ch.Enabled), string(eventsBytes), string(cfgBytes), ch.CreatedAt, ch.UpdatedAt); err != nil {
		return nil, fmt.Errorf("store: upsert channel %q: %w", ch.ID, err)
	}

	out := ch
	out.Config = cfgPlain
	out.HasSecrets = hasSecrets
	return &out, nil
}

// DeleteChannel deletes a notification channel by ID.
func (s *Store) DeleteChannel(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM notification_channels WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("store: delete channel %q: %w", id, err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// GetNotificationSettings loads notification center settings (locale defaults to "en").
func (s *Store) GetNotificationSettings(ctx context.Context) (*NotificationSettings, error) {
	settings := &NotificationSettings{
		Locale: "en",
		Rules:  DefaultNotificationRules(),
	}
	raw, found, err := s.GetSetting(ctx, "notification_settings")
	if err != nil {
		return nil, err
	}
	if !found || raw == "" {
		return settings, nil
	}
	if err := json.Unmarshal([]byte(raw), settings); err != nil {
		return &NotificationSettings{
			Locale: "en",
			Rules:  DefaultNotificationRules(),
		}, nil
	}
	if strings.TrimSpace(settings.Locale) == "" {
		settings.Locale = "en"
	}
	if settings.Rules.SKUInterval < 60 {
		settings.Rules.SKUInterval = 600
	}
	if len(settings.Rules.SKURanges) == 0 {
		settings.Rules.SKURanges = DefaultNotificationRules().SKURanges
	}
	if settings.Rules.ExpiryDays <= 0 {
		settings.Rules.ExpiryDays = 7
	}
	return settings, nil
}

// SaveNotificationSettings persists notification center settings (panelUrl, locale, rules).
func (s *Store) SaveNotificationSettings(ctx context.Context, settings NotificationSettings) (*NotificationSettings, error) {
	switch strings.ToLower(strings.TrimSpace(settings.Locale)) {
	case "fr", "fr-fr":
		settings.Locale = "fr"
	case "zh", "zh-cn", "zh_cn":
		settings.Locale = "zh-CN"
	default:
		settings.Locale = "en"
	}
	settings.PanelURL = strings.TrimSpace(settings.PanelURL)
	if settings.Rules.SKUInterval < 60 {
		settings.Rules.SKUInterval = 600
	}
	if settings.Rules.ExpiryDays < 1 {
		settings.Rules.ExpiryDays = 7
	} else if settings.Rules.ExpiryDays > 90 {
		settings.Rules.ExpiryDays = 90
	}
	if len(settings.Rules.SKURanges) == 0 {
		settings.Rules.SKURanges = DefaultNotificationRules().SKURanges
	}

	b, err := json.Marshal(settings)
	if err != nil {
		return nil, fmt.Errorf("store: marshal notification settings: %w", err)
	}
	if err := s.SetSetting(ctx, "notification_settings", string(b)); err != nil {
		return nil, err
	}
	out := settings
	return &out, nil
}

// HasNotifiedMarker checks whether a deduplication marker key has already been recorded.
func (s *Store) HasNotifiedMarker(ctx context.Context, key string) (bool, error) {
	var cnt int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM notified_markers WHERE marker_key = ?`, key).Scan(&cnt)
	if err != nil {
		return false, fmt.Errorf("store: check notified marker %q: %w", key, err)
	}
	return cnt > 0, nil
}

// SetNotifiedMarker inserts or updates a notification deduplication marker.
func (s *Store) SetNotifiedMarker(ctx context.Context, key, markerType string) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO notified_markers (marker_key, marker_type, created_at)
		VALUES (?, ?, ?)
		ON CONFLICT(marker_key) DO UPDATE SET marker_type = excluded.marker_type
	`, key, markerType, nowUTC())
	if err != nil {
		return fmt.Errorf("store: set notified marker %q: %w", key, err)
	}
	return nil
}

// ==================== mcp_config, mcp_aliases, mcp_audit_logs ====================

// LoadMCPConfig loads the singleton MCP Gateway configuration.
func (s *Store) LoadMCPConfig(ctx context.Context) (*MCPConfig, error) {
	var enabledInt, allowAllInt, notifyInt, hourlyLimit int
	var tokenHash, maskedToken, scopesJSON, allowedJSON, createdAt, updatedAt, rotatedAt string
	err := s.db.QueryRowContext(ctx, `
		SELECT enabled, token_hash, masked_token, scopes_json, allow_all_accounts,
		       allowed_account_ids_json, hourly_order_limit, notify_on_mutation,
		       created_at, updated_at, token_rotated_at
		FROM mcp_config WHERE id = 1
	`).Scan(&enabledInt, &tokenHash, &maskedToken, &scopesJSON, &allowAllInt, &allowedJSON,
		&hourlyLimit, &notifyInt, &createdAt, &updatedAt, &rotatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return &MCPConfig{
			Scopes:            MCPScopes{Read: true},
			AllowAllAccounts:  true,
			AllowedAccountIDs: []string{},
			HourlyOrderLimit:  5,
			NotifyOnMutation:  true,
		}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("store: load mcp_config: %w", err)
	}

	scopes := MCPScopes{Read: true}
	_ = json.Unmarshal([]byte(scopesJSON), &scopes)
	var allowed []string
	_ = json.Unmarshal([]byte(allowedJSON), &allowed)
	if allowed == nil {
		allowed = []string{}
	}
	if hourlyLimit < 1 {
		hourlyLimit = 5
	} else if hourlyLimit > 20 {
		hourlyLimit = 20
	}

	return &MCPConfig{
		Enabled:           enabledInt == 1,
		TokenHash:         tokenHash,
		MaskedToken:       maskedToken,
		Scopes:            scopes,
		AllowAllAccounts:  allowAllInt == 1,
		AllowedAccountIDs: allowed,
		HourlyOrderLimit:  hourlyLimit,
		NotifyOnMutation:  notifyInt == 1,
		CreatedAt:         createdAt,
		UpdatedAt:         updatedAt,
		TokenRotatedAt:    rotatedAt,
	}, nil
}

// SaveMCPConfig persists the singleton MCP Gateway configuration.
func (s *Store) SaveMCPConfig(ctx context.Context, cfg *MCPConfig) error {
	if cfg == nil {
		return errors.New("store: mcp config is nil")
	}
	if cfg.AllowedAccountIDs == nil {
		cfg.AllowedAccountIDs = []string{}
	}
	if cfg.HourlyOrderLimit < 1 {
		cfg.HourlyOrderLimit = 5
	} else if cfg.HourlyOrderLimit > 20 {
		cfg.HourlyOrderLimit = 20
	}
	now := nowUTC()
	if cfg.CreatedAt == "" {
		cfg.CreatedAt = now
	}
	cfg.UpdatedAt = now

	scopesBytes, _ := json.Marshal(cfg.Scopes)
	allowedBytes, _ := json.Marshal(cfg.AllowedAccountIDs)

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO mcp_config (
			id, enabled, token_hash, masked_token, scopes_json, allow_all_accounts,
			allowed_account_ids_json, hourly_order_limit, notify_on_mutation,
			created_at, updated_at, token_rotated_at
		) VALUES (1, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			enabled = excluded.enabled,
			token_hash = excluded.token_hash,
			masked_token = excluded.masked_token,
			scopes_json = excluded.scopes_json,
			allow_all_accounts = excluded.allow_all_accounts,
			allowed_account_ids_json = excluded.allowed_account_ids_json,
			hourly_order_limit = excluded.hourly_order_limit,
			notify_on_mutation = excluded.notify_on_mutation,
			updated_at = excluded.updated_at,
			token_rotated_at = excluded.token_rotated_at
	`,
		boolToInt(cfg.Enabled), cfg.TokenHash, cfg.MaskedToken, string(scopesBytes),
		boolToInt(cfg.AllowAllAccounts), string(allowedBytes), cfg.HourlyOrderLimit,
		boolToInt(cfg.NotifyOnMutation), cfg.CreatedAt, cfg.UpdatedAt, cfg.TokenRotatedAt,
	)
	if err != nil {
		return fmt.Errorf("store: save mcp_config: %w", err)
	}
	return nil
}

func emptyMCPAliasState() *MCPAliasState {
	cats := []string{"account", "baremetal", "vps", "ip", "order"}
	st := &MCPAliasState{
		Forward:  make(map[string]map[string]string, len(cats)),
		Reverse:  make(map[string]map[string]string, len(cats)),
		Meta:     make(map[string]map[string]map[string]any, len(cats)),
		Counters: make(map[string]int, len(cats)),
	}
	for _, cat := range cats {
		st.Forward[cat] = make(map[string]string)
		st.Reverse[cat] = make(map[string]string)
		st.Meta[cat] = make(map[string]map[string]any)
		st.Counters[cat] = 0
	}
	return st
}

// LoadMCPAliases loads the Privacy Shield alias mappings and monotonic counters.
func (s *Store) LoadMCPAliases(ctx context.Context) (*MCPAliasState, error) {
	st := emptyMCPAliasState()

	if rawCounters, found, err := s.GetSetting(ctx, "mcp_alias_counters"); err == nil && found && rawCounters != "" {
		var savedCounters map[string]int
		if json.Unmarshal([]byte(rawCounters), &savedCounters) == nil {
			for k, v := range savedCounters {
				st.Counters[k] = v
			}
		}
	}

	rows, err := s.db.QueryContext(ctx, `SELECT category, real_value, alias, meta_json FROM mcp_aliases`)
	if err != nil {
		return nil, fmt.Errorf("store: load mcp_aliases: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var cat, realVal, alias, metaJSON string
		if err := rows.Scan(&cat, &realVal, &alias, &metaJSON); err != nil {
			return nil, fmt.Errorf("store: scan mcp_aliases: %w", err)
		}
		if st.Forward[cat] == nil {
			st.Forward[cat] = make(map[string]string)
		}
		if st.Reverse[cat] == nil {
			st.Reverse[cat] = make(map[string]string)
		}
		if st.Meta[cat] == nil {
			st.Meta[cat] = make(map[string]map[string]any)
		}
		st.Forward[cat][realVal] = alias
		st.Reverse[cat][alias] = realVal
		if metaJSON != "" {
			var meta map[string]any
			if json.Unmarshal([]byte(metaJSON), &meta) == nil && meta != nil {
				st.Meta[cat][alias] = meta
			}
		}
		if len(st.Forward[cat]) > st.Counters[cat] {
			st.Counters[cat] = len(st.Forward[cat])
		}
	}
	return st, rows.Err()
}

// SaveMCPAliases persists the full Privacy Shield alias map and counters atomically.
func (s *Store) SaveMCPAliases(ctx context.Context, st *MCPAliasState) error {
	if st == nil {
		return errors.New("store: mcp alias state is nil")
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: begin save mcp aliases tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `DELETE FROM mcp_aliases`); err != nil {
		return fmt.Errorf("store: clear mcp_aliases: %w", err)
	}

	for cat, fwd := range st.Forward {
		for realVal, alias := range fwd {
			meta := map[string]any{}
			if st.Meta[cat] != nil && st.Meta[cat][alias] != nil {
				meta = st.Meta[cat][alias]
			}
			metaBytes, _ := json.Marshal(meta)
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO mcp_aliases (category, real_value, alias, meta_json) VALUES (?, ?, ?, ?)`,
				cat, realVal, alias, string(metaBytes),
			); err != nil {
				return fmt.Errorf("store: insert mcp alias (%s, %s): %w", cat, alias, err)
			}
		}
	}

	countersBytes, _ := json.Marshal(st.Counters)
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO kv_settings (key, value, updated_at) VALUES ('mcp_alias_counters', ?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at
	`, string(countersBytes), nowUTC()); err != nil {
		return fmt.Errorf("store: save mcp_alias_counters: %w", err)
	}

	return tx.Commit()
}

// AppendMCPAudit appends an MCP tool audit log entry, pruning entries older than newest 300.
func (s *Store) AppendMCPAudit(ctx context.Context, entry MCPAuditLog) (*MCPAuditLog, error) {
	if strings.TrimSpace(entry.ID) == "" {
		entry.ID = "mcp_" + newUUID()[:12]
	}
	if entry.Timestamp == "" {
		entry.Timestamp = nowUTC()
	}
	if entry.Arguments == nil {
		entry.Arguments = map[string]any{}
	}
	if entry.ClientIP == "" {
		entry.ClientIP = "unknown"
	}

	argsBytes, _ := json.Marshal(entry.Arguments)
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO mcp_audit_logs (id, timestamp, tool, scope, arguments_json, status, summary, client_ip)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, entry.ID, entry.Timestamp, entry.Tool, entry.Scope, string(argsBytes), entry.Status, entry.Summary, entry.ClientIP)
	if err != nil {
		return nil, fmt.Errorf("store: append mcp audit: %w", err)
	}

	_, _ = s.db.ExecContext(ctx, `
		DELETE FROM mcp_audit_logs WHERE id NOT IN (
			SELECT id FROM mcp_audit_logs ORDER BY timestamp DESC LIMIT 300
		)
	`)

	out := entry
	return &out, nil
}

// ListMCPAudit returns recent MCP audit entries ordered newest first.
func (s *Store) ListMCPAudit(ctx context.Context, limit int) ([]MCPAuditLog, error) {
	if limit <= 0 {
		limit = 100
	} else if limit > 300 {
		limit = 300
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, timestamp, tool, scope, arguments_json, status, summary, client_ip
		FROM mcp_audit_logs
		ORDER BY timestamp DESC
		LIMIT ?
	`, limit)
	if err != nil {
		return nil, fmt.Errorf("store: list mcp audit: %w", err)
	}
	defer rows.Close()

	out := make([]MCPAuditLog, 0)
	for rows.Next() {
		var item MCPAuditLog
		var argsJSON string
		if err := rows.Scan(&item.ID, &item.Timestamp, &item.Tool, &item.Scope, &argsJSON, &item.Status, &item.Summary, &item.ClientIP); err != nil {
			return nil, fmt.Errorf("store: scan mcp audit: %w", err)
		}
		_ = json.Unmarshal([]byte(argsJSON), &item.Arguments)
		if item.Arguments == nil {
			item.Arguments = map[string]any{}
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

// ClearMCPAudit deletes all MCP audit log records.
func (s *Store) ClearMCPAudit(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM mcp_audit_logs`)
	if err != nil {
		return fmt.Errorf("store: clear mcp audit: %w", err)
	}
	return nil
}

// ==================== system_logs ====================

const maxSystemLogs = 10000

// InsertLog writes a structured log entry and prunes rows older than the newest 10,000 entries.
func (s *Store) InsertLog(ctx context.Context, timestamp, level, source, message, attrsJSON string) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if timestamp == "" {
		timestamp = nowUTC()
	}
	if attrsJSON == "" {
		attrsJSON = "{}"
	}

	res, err := s.db.ExecContext(ctx, `
		INSERT INTO system_logs (timestamp, level, source, message, attrs_json)
		VALUES (?, ?, ?, ?, ?)
	`, timestamp, level, source, message, attrsJSON)
	if err != nil {
		return 0, fmt.Errorf("store: insert system log: %w", err)
	}
	id, _ := res.LastInsertId()

	s.logCount++
	if s.logCount > maxSystemLogs {
		if _, pruneErr := s.db.ExecContext(ctx, `
			DELETE FROM system_logs WHERE id <= (
				SELECT id FROM system_logs ORDER BY id DESC LIMIT 1 OFFSET ?
			)
		`, maxSystemLogs); pruneErr == nil {
			var cnt int64
			if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM system_logs`).Scan(&cnt); err == nil {
				s.logCount = cnt
			} else {
				s.logCount = maxSystemLogs
			}
		}
	}

	return id, nil
}

// QueryLogs retrieves logs matching optional level, source, and substring filters,
// returning up to limit entries in chronological order (oldest -> newest).
func (s *Store) QueryLogs(ctx context.Context, level, source, search string, limit int) ([]LogRow, error) {
	if limit <= 0 {
		limit = 1000
	} else if limit > maxSystemLogs {
		limit = maxSystemLogs
	}

	var conds []string
	var args []any

	if lvl := strings.TrimSpace(level); lvl != "" && !strings.EqualFold(lvl, "all") {
		conds = append(conds, `UPPER(level) = UPPER(?)`)
		args = append(args, lvl)
	}
	if src := strings.TrimSpace(source); src != "" && !strings.EqualFold(src, "all") {
		conds = append(conds, `LOWER(source) = LOWER(?)`)
		args = append(args, src)
	}
	if q := strings.TrimSpace(search); q != "" {
		escaped := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(strings.ToLower(q))
		pattern := "%" + escaped + "%"
		conds = append(conds, `(LOWER(message) LIKE ? ESCAPE '\' OR LOWER(source) LIKE ? ESCAPE '\' OR LOWER(attrs_json) LIKE ? ESCAPE '\')`)
		args = append(args, pattern, pattern, pattern)
	}

	query := `SELECT id, timestamp, level, source, message, attrs_json FROM system_logs`
	if len(conds) > 0 {
		query += ` WHERE ` + strings.Join(conds, ` AND `)
	}
	query += ` ORDER BY id DESC LIMIT ?`
	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("store: query logs: %w", err)
	}
	defer rows.Close()

	desc := make([]LogRow, 0)
	for rows.Next() {
		var r LogRow
		if err := rows.Scan(&r.ID, &r.Timestamp, &r.Level, &r.Source, &r.Message, &r.AttrsJSON); err != nil {
			return nil, fmt.Errorf("store: scan log row: %w", err)
		}
		desc = append(desc, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Reverse descending results into chronological order (oldest -> newest).
	for i, j := 0, len(desc)-1; i < j; i, j = i+1, j-1 {
		desc[i], desc[j] = desc[j], desc[i]
	}
	return desc, nil
}

// ClearLogs removes all entries from system_logs.
func (s *Store) ClearLogs(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, err := s.db.ExecContext(ctx, `DELETE FROM system_logs`); err != nil {
		return fmt.Errorf("store: clear logs: %w", err)
	}
	s.logCount = 0
	return nil
}
