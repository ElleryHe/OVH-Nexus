package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"

	"ovh-nexus/server/internal/vault"
)

// ErrNotFound indicates the requested record does not exist in SQLite.
var ErrNotFound = errors.New("record not found")

// Store provides thread-safe, encrypted-at-rest persistence over a pure-Go SQLite WAL database.
type Store struct {
	db       *sql.DB
	vault    *vault.Vault
	mu       sync.Mutex
	logCount int64
}

// OVHAccount represents a managed OVHcloud API profile with decrypted credentials in memory.
type OVHAccount struct {
	ID          string `json:"id"`
	Alias       string `json:"alias"`
	Endpoint    string `json:"endpoint"`
	Zone        string `json:"zone"`
	AppKey      string `json:"appKey"`
	AppSecret   string `json:"appSecret"`
	ConsumerKey string `json:"consumerKey"`
	IsDefault   bool   `json:"isDefault"`
	CreatedAt   string `json:"createdAt"`
	UpdatedAt   string `json:"updatedAt"`
}

// TOTPState stores singleton 2FA TOTP configuration and anti-replay state.
type TOTPState struct {
	Enabled            bool     `json:"enabled"`
	Secret             string   `json:"secret"`
	PendingSecret      string   `json:"pendingSecret"`
	RecoveryCodeHashes []string `json:"recoveryCodeHashes"`
	LastUsedStep       int64    `json:"lastUsedStep"`
	EnabledAt          string   `json:"enabledAt"`
	UpdatedAt          string   `json:"updatedAt"`
}

// Open initializes <dataDir>/nexus.db in WAL mode with 0700 directory and 0600 file permissions.
func Open(dataDir string, v *vault.Vault) (*Store, error) {
	if strings.TrimSpace(dataDir) == "" {
		return nil, errors.New("store: dataDir must not be empty")
	}
	if v == nil {
		return nil, errors.New("store: vault must not be nil")
	}

	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return nil, fmt.Errorf("store: failed to create data directory %q: %w", dataDir, err)
	}
	_ = os.Chmod(dataDir, 0o700)

	dbPath := filepath.Join(dataDir, "nexus.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("store: failed to open sqlite database %q: %w", dbPath, err)
	}

	// Single writer connection prevents SQLITE_BUSY under high goroutine concurrency.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	pragmas := []string{
		"PRAGMA journal_mode=WAL;",
		"PRAGMA synchronous=NORMAL;",
		"PRAGMA busy_timeout=5000;",
		"PRAGMA foreign_keys=ON;",
	}
	for _, p := range pragmas {
		if _, err := db.Exec(p); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("store: failed executing %q: %w", p, err)
		}
	}

	if err := os.Chmod(dbPath, 0o600); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("store: failed setting 0600 permissions on %q: %w", dbPath, err)
	}

	s := &Store{
		db:    db,
		vault: v,
	}
	if err := s.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}

	var cnt int64
	if err := db.QueryRow("SELECT COUNT(*) FROM system_logs").Scan(&cnt); err == nil {
		s.logCount = cnt
	}

	return s, nil
}

// Close closes the underlying SQLite connection.
func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

// DB exposes the underlying *sql.DB for diagnostics or tests.
func (s *Store) DB() *sql.DB {
	return s.db
}

func (s *Store) migrate() error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS kv_settings (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS ovh_accounts (
			id TEXT PRIMARY KEY,
			alias TEXT NOT NULL DEFAULT '',
			endpoint TEXT NOT NULL DEFAULT 'ovh-eu',
			zone TEXT NOT NULL DEFAULT 'IE',
			app_key TEXT NOT NULL DEFAULT '',
			app_secret TEXT NOT NULL DEFAULT '',
			consumer_key TEXT NOT NULL DEFAULT '',
			is_default INTEGER NOT NULL DEFAULT 0,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS totp_state (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			enabled INTEGER NOT NULL DEFAULT 0,
			secret TEXT NOT NULL DEFAULT '',
			pending_secret TEXT NOT NULL DEFAULT '',
			recovery_hashes_json TEXT NOT NULL DEFAULT '[]',
			last_used_step INTEGER NOT NULL DEFAULT 0,
			enabled_at TEXT NOT NULL DEFAULT '',
			updated_at TEXT NOT NULL DEFAULT ''
		);`,
		`CREATE TABLE IF NOT EXISTS order_queue (
			id TEXT PRIMARY KEY,
			account_id TEXT NOT NULL DEFAULT '',
			plan_code TEXT NOT NULL,
			datacenters_json TEXT NOT NULL DEFAULT '[]',
			options_json TEXT NOT NULL DEFAULT '[]',
			config_key TEXT NOT NULL DEFAULT '',
			config_label TEXT NOT NULL DEFAULT '',
			pricing_mode TEXT NOT NULL DEFAULT '',
			duration TEXT NOT NULL DEFAULT '',
			coupon TEXT NOT NULL DEFAULT '',
			max_monthly_price REAL NOT NULL DEFAULT 0,
			currency TEXT NOT NULL DEFAULT '',
			quantity INTEGER NOT NULL DEFAULT 1,
			purchased INTEGER NOT NULL DEFAULT 0,
			failure_count INTEGER NOT NULL DEFAULT 0,
			max_retry_count INTEGER NOT NULL DEFAULT 50,
			status TEXT NOT NULL DEFAULT 'running',
			retry_interval INTEGER NOT NULL DEFAULT 30,
			retry_count INTEGER NOT NULL DEFAULT 0,
			last_check_time INTEGER NOT NULL DEFAULT 0,
			next_attempt_at INTEGER NOT NULL DEFAULT 0,
			auto_pay INTEGER NOT NULL DEFAULT 0,
			waive_retractation_period INTEGER NOT NULL DEFAULT 0,
			source TEXT NOT NULL DEFAULT 'manual',
			source_ref TEXT NOT NULL DEFAULT '',
			last_error TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);`,
		`CREATE INDEX IF NOT EXISTS idx_order_queue_account ON order_queue(account_id);`,
		`CREATE INDEX IF NOT EXISTS idx_order_queue_status ON order_queue(status);`,
		`CREATE TABLE IF NOT EXISTS purchase_history (
			id TEXT PRIMARY KEY,
			task_id TEXT NOT NULL DEFAULT '',
			account_id TEXT NOT NULL DEFAULT '',
			plan_code TEXT NOT NULL,
			datacenter TEXT NOT NULL DEFAULT '',
			options_json TEXT NOT NULL DEFAULT '[]',
			config_key TEXT NOT NULL DEFAULT '',
			config_label TEXT NOT NULL DEFAULT '',
			pricing_mode TEXT NOT NULL DEFAULT '',
			duration TEXT NOT NULL DEFAULT '',
			coupon TEXT NOT NULL DEFAULT '',
			quantity INTEGER NOT NULL DEFAULT 1,
			status TEXT NOT NULL,
			order_id TEXT NOT NULL DEFAULT '',
			order_url TEXT NOT NULL DEFAULT '',
			price_text TEXT NOT NULL DEFAULT '',
			price_json TEXT NOT NULL DEFAULT '{}',
			auto_paid INTEGER NOT NULL DEFAULT 0,
			attempt_count INTEGER NOT NULL DEFAULT 0,
			sequence INTEGER NOT NULL DEFAULT 0,
			delivered_notified INTEGER NOT NULL DEFAULT 0,
			error_message TEXT NOT NULL DEFAULT '',
			timestamp TEXT NOT NULL
		);`,
		`CREATE INDEX IF NOT EXISTS idx_purchase_history_account ON purchase_history(account_id);`,
		`CREATE INDEX IF NOT EXISTS idx_purchase_history_ts ON purchase_history(timestamp DESC);`,
		`CREATE TABLE IF NOT EXISTS stock_subscriptions (
			account_id TEXT NOT NULL DEFAULT '',
			plan_code TEXT NOT NULL,
			server_name TEXT NOT NULL DEFAULT '',
			subsidiary TEXT NOT NULL DEFAULT '',
			datacenters_json TEXT NOT NULL DEFAULT '[]',
			notify_available INTEGER NOT NULL DEFAULT 1,
			notify_unavailable INTEGER NOT NULL DEFAULT 0,
			auto_order INTEGER NOT NULL DEFAULT 0,
			auto_order_quantity INTEGER NOT NULL DEFAULT 0,
			targets_json TEXT NOT NULL DEFAULT '[]',
			needs_config_selection INTEGER NOT NULL DEFAULT 0,
			last_status_json TEXT NOT NULL DEFAULT '{}',
			history_json TEXT NOT NULL DEFAULT '[]',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			PRIMARY KEY (account_id, plan_code)
		);`,
		`CREATE TABLE IF NOT EXISTS vps_subscriptions (
			id TEXT PRIMARY KEY,
			plan_code TEXT NOT NULL,
			ovh_subsidiary TEXT NOT NULL DEFAULT 'IE',
			datacenters_json TEXT NOT NULL DEFAULT '[]',
			monitor_linux INTEGER NOT NULL DEFAULT 1,
			monitor_windows INTEGER NOT NULL DEFAULT 0,
			notify_available INTEGER NOT NULL DEFAULT 1,
			notify_unavailable INTEGER NOT NULL DEFAULT 0,
			last_status_json TEXT NOT NULL DEFAULT '{}',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			UNIQUE(plan_code, ovh_subsidiary)
		);`,
		`CREATE TABLE IF NOT EXISTS vps_history (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			subscription_id TEXT NOT NULL,
			timestamp TEXT NOT NULL,
			datacenter TEXT NOT NULL,
			datacenter_code TEXT NOT NULL,
			status TEXT NOT NULL,
			change_type TEXT NOT NULL,
			old_status TEXT NOT NULL DEFAULT '',
			FOREIGN KEY(subscription_id) REFERENCES vps_subscriptions(id) ON DELETE CASCADE
		);`,
		`CREATE INDEX IF NOT EXISTS idx_vps_history_sub ON vps_history(subscription_id, id DESC);`,
		`CREATE TABLE IF NOT EXISTS sku_watcher_state (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			initialized INTEGER NOT NULL DEFAULT 0,
			endpoint TEXT NOT NULL DEFAULT 'ovh-eu',
			skus_json TEXT NOT NULL DEFAULT '{}',
			missing_counts_json TEXT NOT NULL DEFAULT '{}',
			changes_json TEXT NOT NULL DEFAULT '[]',
			updated_at TEXT NOT NULL DEFAULT ''
		);`,
		`CREATE TABLE IF NOT EXISTS notification_channels (
			id TEXT PRIMARY KEY,
			type TEXT NOT NULL,
			name TEXT NOT NULL DEFAULT '',
			enabled INTEGER NOT NULL DEFAULT 1,
			events_json TEXT NOT NULL DEFAULT '[]',
			config_json TEXT NOT NULL DEFAULT '{}',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS notified_markers (
			marker_key TEXT PRIMARY KEY,
			marker_type TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS mcp_config (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			enabled INTEGER NOT NULL DEFAULT 0,
			token_hash TEXT NOT NULL DEFAULT '',
			masked_token TEXT NOT NULL DEFAULT '',
			scopes_json TEXT NOT NULL DEFAULT '{"read":true,"procurement":false,"operations":false}',
			allow_all_accounts INTEGER NOT NULL DEFAULT 1,
			allowed_account_ids_json TEXT NOT NULL DEFAULT '[]',
			hourly_order_limit INTEGER NOT NULL DEFAULT 5,
			notify_on_mutation INTEGER NOT NULL DEFAULT 1,
			created_at TEXT NOT NULL DEFAULT '',
			updated_at TEXT NOT NULL DEFAULT '',
			token_rotated_at TEXT NOT NULL DEFAULT ''
		);`,
		`CREATE TABLE IF NOT EXISTS mcp_aliases (
			category TEXT NOT NULL,
			real_value TEXT NOT NULL,
			alias TEXT NOT NULL,
			meta_json TEXT NOT NULL DEFAULT '{}',
			PRIMARY KEY (category, real_value),
			UNIQUE (category, alias)
		);`,
		`CREATE TABLE IF NOT EXISTS mcp_audit_logs (
			id TEXT PRIMARY KEY,
			timestamp TEXT NOT NULL,
			tool TEXT NOT NULL,
			scope TEXT NOT NULL,
			arguments_json TEXT NOT NULL DEFAULT '{}',
			status TEXT NOT NULL,
			summary TEXT NOT NULL DEFAULT '',
			client_ip TEXT NOT NULL DEFAULT 'unknown'
		);`,
		`CREATE INDEX IF NOT EXISTS idx_mcp_audit_ts ON mcp_audit_logs(timestamp DESC);`,
		`CREATE TABLE IF NOT EXISTS system_logs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			timestamp TEXT NOT NULL,
			level TEXT NOT NULL,
			source TEXT NOT NULL,
			message TEXT NOT NULL,
			attrs_json TEXT NOT NULL DEFAULT '{}'
		);`,
		`CREATE INDEX IF NOT EXISTS idx_system_logs_level ON system_logs(level);`,
		`CREATE INDEX IF NOT EXISTS idx_system_logs_source ON system_logs(source);`,
	}

	for _, stmt := range stmts {
		if _, err := s.db.Exec(stmt); err != nil {
			return fmt.Errorf("store: migration failed: %w", err)
		}
	}

	alterStmts := []string{
		`ALTER TABLE order_queue ADD COLUMN source TEXT NOT NULL DEFAULT 'manual';`,
		`ALTER TABLE order_queue ADD COLUMN source_ref TEXT NOT NULL DEFAULT '';`,
		`ALTER TABLE order_queue ADD COLUMN config_key TEXT NOT NULL DEFAULT '';`,
		`ALTER TABLE order_queue ADD COLUMN config_label TEXT NOT NULL DEFAULT '';`,
		`ALTER TABLE order_queue ADD COLUMN max_monthly_price REAL NOT NULL DEFAULT 0;`,
		`ALTER TABLE order_queue ADD COLUMN currency TEXT NOT NULL DEFAULT '';`,
		`ALTER TABLE order_queue ADD COLUMN pricing_mode TEXT NOT NULL DEFAULT '';`,
		`ALTER TABLE order_queue ADD COLUMN duration TEXT NOT NULL DEFAULT '';`,
		`ALTER TABLE order_queue ADD COLUMN coupon TEXT NOT NULL DEFAULT '';`,
		`ALTER TABLE purchase_history ADD COLUMN config_key TEXT NOT NULL DEFAULT '';`,
		`ALTER TABLE purchase_history ADD COLUMN config_label TEXT NOT NULL DEFAULT '';`,
		`ALTER TABLE purchase_history ADD COLUMN quantity INTEGER NOT NULL DEFAULT 1;`,
		`ALTER TABLE purchase_history ADD COLUMN pricing_mode TEXT NOT NULL DEFAULT '';`,
		`ALTER TABLE purchase_history ADD COLUMN duration TEXT NOT NULL DEFAULT '';`,
		`ALTER TABLE purchase_history ADD COLUMN coupon TEXT NOT NULL DEFAULT '';`,
		`ALTER TABLE stock_subscriptions ADD COLUMN subsidiary TEXT NOT NULL DEFAULT '';`,
		`ALTER TABLE stock_subscriptions ADD COLUMN targets_json TEXT NOT NULL DEFAULT '[]';`,
		`ALTER TABLE stock_subscriptions ADD COLUMN needs_config_selection INTEGER NOT NULL DEFAULT 0;`,
	}
	for _, stmt := range alterStmts {
		if _, err := s.db.Exec(stmt); err != nil {
			if !strings.Contains(strings.ToLower(err.Error()), "duplicate column") {
				return fmt.Errorf("store: alter migration failed: %w", err)
			}
		}
	}
	return nil
}

func nowUTC() string {
	return time.Now().UTC().Format(time.RFC3339Nano)
}

func newUUID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// ==================== kv_settings ====================

// GetSetting retrieves a setting value from kv_settings.
func (s *Store) GetSetting(ctx context.Context, key string) (string, bool, error) {
	var val string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM kv_settings WHERE key = ?`, key).Scan(&val)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("store: get setting %q: %w", key, err)
	}
	return val, true, nil
}

// SetSetting upserts a key-value pair in kv_settings.
func (s *Store) SetSetting(ctx context.Context, key, value string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO kv_settings (key, value, updated_at) VALUES (?, ?, ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
		key, value, nowUTC(),
	)
	if err != nil {
		return fmt.Errorf("store: set setting %q: %w", key, err)
	}
	return nil
}

// DeleteSetting deletes a key from kv_settings.
func (s *Store) DeleteSetting(ctx context.Context, key string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM kv_settings WHERE key = ?`, key)
	if err != nil {
		return fmt.Errorf("store: delete setting %q: %w", key, err)
	}
	return nil
}

// GetOrCreateSessionSecret returns the encrypted-at-rest HMAC session secret,
// creating and persisting a cryptographically random secret on first call.
func (s *Store) GetOrCreateSessionSecret(ctx context.Context) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	raw, found, err := s.GetSetting(ctx, "session_secret")
	if err != nil {
		return "", err
	}
	if found && raw != "" {
		pt, err := s.vault.Decrypt(raw)
		if err != nil {
			return "", fmt.Errorf("store: decrypt session_secret: %w", err)
		}
		if pt != "" {
			if !vault.IsEncrypted(raw) {
				if enc, encErr := s.vault.Encrypt(pt); encErr == nil {
					_ = s.SetSetting(ctx, "session_secret", enc)
				}
			}
			return pt, nil
		}
	}

	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("store: generate session_secret: %w", err)
	}
	secret := base64.RawURLEncoding.EncodeToString(buf)
	enc, err := s.vault.Encrypt(secret)
	if err != nil {
		return "", fmt.Errorf("store: encrypt session_secret: %w", err)
	}
	if err := s.SetSetting(ctx, "session_secret", enc); err != nil {
		return "", err
	}
	return secret, nil
}

// ==================== ovh_accounts ====================

func (s *Store) scanAccount(scanner interface{ Scan(...any) error }) (*OVHAccount, error) {
	var acc OVHAccount
	var encAppKey, encAppSecret, encConsumerKey string
	var isDef int
	if err := scanner.Scan(
		&acc.ID,
		&acc.Alias,
		&acc.Endpoint,
		&acc.Zone,
		&encAppKey,
		&encAppSecret,
		&encConsumerKey,
		&isDef,
		&acc.CreatedAt,
		&acc.UpdatedAt,
	); err != nil {
		return nil, err
	}
	var err error
	if acc.AppKey, err = s.vault.Decrypt(encAppKey); err != nil {
		return nil, fmt.Errorf("store: decrypt account %q app_key: %w", acc.ID, err)
	}
	if acc.AppSecret, err = s.vault.Decrypt(encAppSecret); err != nil {
		return nil, fmt.Errorf("store: decrypt account %q app_secret: %w", acc.ID, err)
	}
	if acc.ConsumerKey, err = s.vault.Decrypt(encConsumerKey); err != nil {
		return nil, fmt.Errorf("store: decrypt account %q consumer_key: %w", acc.ID, err)
	}
	acc.IsDefault = isDef == 1
	return &acc, nil
}

// ListAccounts returns all stored OVH accounts with decrypted credentials.
func (s *Store) ListAccounts(ctx context.Context) ([]OVHAccount, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, alias, endpoint, zone, app_key, app_secret, consumer_key, is_default, created_at, updated_at
		FROM ovh_accounts
		ORDER BY is_default DESC, created_at ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("store: list accounts: %w", err)
	}
	defer rows.Close()

	accounts := make([]OVHAccount, 0)
	for rows.Next() {
		acc, err := s.scanAccount(rows)
		if err != nil {
			return nil, err
		}
		accounts = append(accounts, *acc)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: iterate accounts: %w", err)
	}
	return accounts, nil
}

// GetAccount loads a single OVH account by ID with decrypted credentials.
func (s *Store) GetAccount(ctx context.Context, id string) (*OVHAccount, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, alias, endpoint, zone, app_key, app_secret, consumer_key, is_default, created_at, updated_at
		FROM ovh_accounts
		WHERE id = ?
	`, strings.TrimSpace(id))
	acc, err := s.scanAccount(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: get account %q: %w", id, err)
	}
	return acc, nil
}

// GetActiveAccount resolves the account matching headerID, or falls back to the
// default account (or earliest created account) when headerID is empty.
func (s *Store) GetActiveAccount(ctx context.Context, headerID string) (*OVHAccount, error) {
	trimmed := strings.TrimSpace(headerID)
	if trimmed != "" {
		return s.GetAccount(ctx, trimmed)
	}

	row := s.db.QueryRowContext(ctx, `
		SELECT id, alias, endpoint, zone, app_key, app_secret, consumer_key, is_default, created_at, updated_at
		FROM ovh_accounts
		ORDER BY is_default DESC, created_at ASC
		LIMIT 1
	`)
	acc, err := s.scanAccount(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: get active account: %w", err)
	}
	return acc, nil
}

// UpsertAccount encrypts credentials via vault and persists the account record.
func (s *Store) UpsertAccount(ctx context.Context, acc OVHAccount) (*OVHAccount, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	acc.ID = strings.TrimSpace(acc.ID)
	if acc.ID == "" {
		return nil, errors.New("store: account id is required")
	}
	if strings.TrimSpace(acc.Endpoint) == "" {
		acc.Endpoint = "ovh-eu"
	}
	if strings.TrimSpace(acc.Zone) == "" {
		acc.Zone = "IE"
	}

	encKey, err := s.vault.Encrypt(acc.AppKey)
	if err != nil {
		return nil, fmt.Errorf("store: encrypt app_key: %w", err)
	}
	encSecret, err := s.vault.Encrypt(acc.AppSecret)
	if err != nil {
		return nil, fmt.Errorf("store: encrypt app_secret: %w", err)
	}
	encConsumer, err := s.vault.Encrypt(acc.ConsumerKey)
	if err != nil {
		return nil, fmt.Errorf("store: encrypt consumer_key: %w", err)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("store: begin upsert account tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var existingCreated string
	var existingDefault int
	err = tx.QueryRowContext(ctx, `SELECT created_at, is_default FROM ovh_accounts WHERE id = ?`, acc.ID).
		Scan(&existingCreated, &existingDefault)
	exists := err == nil
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("store: check existing account: %w", err)
	}

	var totalCount int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM ovh_accounts`).Scan(&totalCount); err != nil {
		return nil, fmt.Errorf("store: count accounts: %w", err)
	}

	now := nowUTC()
	if exists && existingCreated != "" {
		acc.CreatedAt = existingCreated
	} else if acc.CreatedAt == "" {
		acc.CreatedAt = now
	}
	acc.UpdatedAt = now

	if totalCount == 0 || (exists && existingDefault == 1) {
		if totalCount == 0 {
			acc.IsDefault = true
		} else if exists && existingDefault == 1 {
			acc.IsDefault = true
		}
	}

	if acc.IsDefault {
		if _, err := tx.ExecContext(ctx, `UPDATE ovh_accounts SET is_default = 0 WHERE id != ?`, acc.ID); err != nil {
			return nil, fmt.Errorf("store: clear default account: %w", err)
		}
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO ovh_accounts (id, alias, endpoint, zone, app_key, app_secret, consumer_key, is_default, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			alias = excluded.alias,
			endpoint = excluded.endpoint,
			zone = excluded.zone,
			app_key = excluded.app_key,
			app_secret = excluded.app_secret,
			consumer_key = excluded.consumer_key,
			is_default = excluded.is_default,
			updated_at = excluded.updated_at
	`, acc.ID, acc.Alias, acc.Endpoint, acc.Zone, encKey, encSecret, encConsumer, boolToInt(acc.IsDefault), acc.CreatedAt, acc.UpdatedAt); err != nil {
		return nil, fmt.Errorf("store: upsert account: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("store: commit upsert account: %w", err)
	}
	out := acc
	return &out, nil
}

// DeleteAccount removes an account and promotes another account to default if needed.
func (s *Store) DeleteAccount(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: begin delete account tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var wasDefault int
	err = tx.QueryRowContext(ctx, `SELECT is_default FROM ovh_accounts WHERE id = ?`, id).Scan(&wasDefault)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("store: lookup account %q: %w", id, err)
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM ovh_accounts WHERE id = ?`, id); err != nil {
		return fmt.Errorf("store: delete account %q: %w", id, err)
	}

	if wasDefault == 1 {
		var nextID string
		err := tx.QueryRowContext(ctx, `SELECT id FROM ovh_accounts ORDER BY created_at ASC LIMIT 1`).Scan(&nextID)
		if err == nil && nextID != "" {
			if _, err := tx.ExecContext(ctx, `UPDATE ovh_accounts SET is_default = 1 WHERE id = ?`, nextID); err != nil {
				return fmt.Errorf("store: promote default account: %w", err)
			}
		}
	}

	return tx.Commit()
}

// SetDefaultAccount marks an existing account as the default active profile.
func (s *Store) SetDefaultAccount(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: begin set default account tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var exists int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM ovh_accounts WHERE id = ?`, id).Scan(&exists); err != nil {
		return fmt.Errorf("store: verify account %q: %w", id, err)
	}
	if exists == 0 {
		return ErrNotFound
	}

	if _, err := tx.ExecContext(ctx, `UPDATE ovh_accounts SET is_default = 0`); err != nil {
		return fmt.Errorf("store: clear default flags: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE ovh_accounts SET is_default = 1, updated_at = ? WHERE id = ?`, nowUTC(), id); err != nil {
		return fmt.Errorf("store: set default account %q: %w", id, err)
	}
	return tx.Commit()
}

// ==================== totp_state ====================

// LoadTOTPState loads the singleton TOTP state and transparently decrypts secret & pending_secret.
func (s *Store) LoadTOTPState(ctx context.Context) (*TOTPState, error) {
	var enabledInt int
	var encSecret, encPending, hashesJSON, enabledAt, updatedAt string
	var lastUsed int64

	err := s.db.QueryRowContext(ctx, `
		SELECT enabled, secret, pending_secret, recovery_hashes_json, last_used_step, enabled_at, updated_at
		FROM totp_state
		WHERE id = 1
	`).Scan(&enabledInt, &encSecret, &encPending, &hashesJSON, &lastUsed, &enabledAt, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return &TOTPState{
			RecoveryCodeHashes: []string{},
		}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("store: load totp_state: %w", err)
	}

	secret, err := s.vault.Decrypt(encSecret)
	if err != nil {
		return nil, fmt.Errorf("store: decrypt totp secret: %w", err)
	}
	pending, err := s.vault.Decrypt(encPending)
	if err != nil {
		return nil, fmt.Errorf("store: decrypt totp pending_secret: %w", err)
	}

	var hashes []string
	if hashesJSON != "" {
		if err := json.Unmarshal([]byte(hashesJSON), &hashes); err != nil {
			hashes = []string{}
		}
	}
	if hashes == nil {
		hashes = []string{}
	}

	return &TOTPState{
		Enabled:            enabledInt == 1 && secret != "",
		Secret:             secret,
		PendingSecret:      pending,
		RecoveryCodeHashes: hashes,
		LastUsedStep:       lastUsed,
		EnabledAt:          enabledAt,
		UpdatedAt:          updatedAt,
	}, nil
}

// SaveTOTPState encrypts secret & pending_secret via vault and persists singleton totp_state.
func (s *Store) SaveTOTPState(ctx context.Context, st *TOTPState) error {
	if st == nil {
		return errors.New("store: totp state is nil")
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	encSecret, err := s.vault.Encrypt(st.Secret)
	if err != nil {
		return fmt.Errorf("store: encrypt totp secret: %w", err)
	}
	encPending, err := s.vault.Encrypt(st.PendingSecret)
	if err != nil {
		return fmt.Errorf("store: encrypt totp pending_secret: %w", err)
	}

	hashes := st.RecoveryCodeHashes
	if hashes == nil {
		hashes = []string{}
	}
	hashesBytes, err := json.Marshal(hashes)
	if err != nil {
		return fmt.Errorf("store: marshal totp recovery hashes: %w", err)
	}

	st.UpdatedAt = nowUTC()
	effectiveEnabled := st.Enabled && st.Secret != ""

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO totp_state (id, enabled, secret, pending_secret, recovery_hashes_json, last_used_step, enabled_at, updated_at)
		VALUES (1, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			enabled = excluded.enabled,
			secret = excluded.secret,
			pending_secret = excluded.pending_secret,
			recovery_hashes_json = excluded.recovery_hashes_json,
			last_used_step = excluded.last_used_step,
			enabled_at = excluded.enabled_at,
			updated_at = excluded.updated_at
	`, boolToInt(effectiveEnabled), encSecret, encPending, string(hashesBytes), st.LastUsedStep, st.EnabledAt, st.UpdatedAt)
	if err != nil {
		return fmt.Errorf("store: save totp_state: %w", err)
	}
	st.Enabled = effectiveEnabled
	return nil
}
