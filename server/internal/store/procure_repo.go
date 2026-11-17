package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
)

// QueueItem represents an automated server procurement task in order_queue.
// AutoPay and WaiveRetractationPeriod strictly default to false.
type QueueItem struct {
	ID                      string   `json:"id"`
	AccountID               string   `json:"accountId,omitempty"`
	PlanCode                string   `json:"planCode"`
	Datacenters             []string `json:"datacenters"`
	Options                 []string `json:"options"`
	ConfigKey               string   `json:"configKey,omitempty"`
	ConfigLabel             string   `json:"configLabel,omitempty"`
	PricingMode             string   `json:"pricingMode,omitempty"`
	Duration                string   `json:"duration,omitempty"`
	Coupon                  string   `json:"coupon,omitempty"`
	MaxMonthlyPrice         float64  `json:"maxMonthlyPrice,omitempty"`
	Currency                string   `json:"currency,omitempty"`
	Quantity                int      `json:"quantity"`
	Purchased               int      `json:"purchased"`
	FailureCount            int      `json:"failureCount"`
	MaxRetryCount           int      `json:"maxRetryCount"`
	Status                  string   `json:"status"`
	RetryInterval           int      `json:"retryInterval"`
	RetryCount              int      `json:"retryCount"`
	LastCheckTime           int64    `json:"lastCheckTime"`
	NextAttemptAt           int64    `json:"nextAttemptAt"`
	AutoPay                 bool     `json:"auto_pay"`
	WaiveRetractationPeriod bool     `json:"waive_retractation_period"`
	Source                  string   `json:"source"`
	SourceRef               string   `json:"sourceRef,omitempty"`
	LastError               string   `json:"lastError,omitempty"`
	CreatedAt               string   `json:"createdAt"`
	UpdatedAt               string   `json:"updatedAt"`
}

// PriceInfo holds structured price metadata recorded with a purchase attempt.
type PriceInfo struct {
	WithTax      float64 `json:"withTax,omitempty"`
	WithoutTax   float64 `json:"withoutTax,omitempty"`
	Tax          float64 `json:"tax,omitempty"`
	PeriodPrice  float64 `json:"periodPrice,omitempty"`
	SetupFee     float64 `json:"setupFee,omitempty"`
	MonthlyExVAT float64 `json:"monthlyExVat,omitempty"`
	PricingMode  string  `json:"pricingMode,omitempty"`
	Duration     string  `json:"duration,omitempty"`
	CurrencyCode string  `json:"currencyCode,omitempty"`
	Text         string  `json:"text,omitempty"`
}

// PurchaseHistory represents an individual order checkout attempt result.
type PurchaseHistory struct {
	ID                string     `json:"id"`
	TaskID            string     `json:"taskId,omitempty"`
	AccountID         string     `json:"accountId,omitempty"`
	PlanCode          string     `json:"planCode"`
	Datacenter        string     `json:"datacenter"`
	Options           []string   `json:"options"`
	ConfigKey         string     `json:"configKey,omitempty"`
	ConfigLabel       string     `json:"configLabel,omitempty"`
	PricingMode       string     `json:"pricingMode,omitempty"`
	Duration          string     `json:"duration,omitempty"`
	Coupon            string     `json:"coupon,omitempty"`
	Quantity          int        `json:"quantity,omitempty"`
	Status            string     `json:"status"`
	OrderID           string     `json:"orderId,omitempty"`
	OrderURL          string     `json:"orderUrl,omitempty"`
	PriceText         string     `json:"priceText,omitempty"`
	Price             *PriceInfo `json:"price,omitempty"`
	PeriodPrice       float64    `json:"periodPrice,omitempty"`
	MonthlyPrice      float64    `json:"monthlyPrice,omitempty"`
	SetupFee          float64    `json:"setupFee,omitempty"`
	AutoPaid          bool       `json:"autoPaid"`
	AttemptCount      int        `json:"attemptCount,omitempty"`
	Sequence          int        `json:"sequence,omitempty"`
	DeliveredNotified bool       `json:"deliveredNotified,omitempty"`
	ErrorMessage      string     `json:"errorMessage,omitempty"`
	Timestamp         string     `json:"purchaseTime"`
}

// Stats aggregates queue and purchase metrics from SQLite.
type Stats struct {
	ActiveQueues          int  `json:"activeQueues"`
	TotalServers          int  `json:"totalServers"`
	AvailableServers      int  `json:"availableServers"`
	PurchaseSuccess       int  `json:"purchaseSuccess"`
	PurchaseFailed        int  `json:"purchaseFailed"`
	QueueProcessorRunning bool `json:"queueProcessorRunning"`
	MonitorRunning        bool `json:"monitorRunning"`
}

// StockHistoryEntry records an availability transition for a dedicated stock subscription.
type StockHistoryEntry struct {
	Timestamp   string         `json:"timestamp"`
	Datacenter  string         `json:"datacenter"`
	Status      string         `json:"status"`
	ChangeType  string         `json:"changeType"`
	OldStatus   string         `json:"oldStatus"`
	TargetID    string         `json:"targetId,omitempty"`
	ConfigKey   string         `json:"configKey,omitempty"`
	ConfigLabel string         `json:"configLabel,omitempty"`
	Config      map[string]any `json:"config,omitempty"`
}

// StockSubscription represents a dedicated server stock monitor subscription.
type StockSubscription struct {
	AccountID            string              `json:"accountId,omitempty"`
	PlanCode             string              `json:"planCode"`
	ServerName           string              `json:"serverName,omitempty"`
	Subsidiary           string              `json:"subsidiary,omitempty"`
	Datacenters          []string            `json:"datacenters"`
	NotifyAvailable      bool                `json:"notifyAvailable"`
	NotifyUnavailable    bool                `json:"notifyUnavailable"`
	AutoOrder            bool                `json:"autoOrder"`
	AutoOrderQuantity    int                 `json:"autoOrderQuantity"`
	Targets              []StockTarget       `json:"targets"`
	NeedsConfigSelection bool                `json:"needsConfigSelection,omitempty"`
	LastStatus           map[string]string   `json:"lastStatus"`
	History              []StockHistoryEntry `json:"history"`
	CreatedAt            string              `json:"createdAt"`
	UpdatedAt            string              `json:"updatedAt,omitempty"`
}

// VPSHistoryEntry records a VPS datacenter availability transition.
type VPSHistoryEntry struct {
	ID             int64  `json:"id,omitempty"`
	SubscriptionID string `json:"subscriptionId,omitempty"`
	Timestamp      string `json:"timestamp"`
	Datacenter     string `json:"datacenter"`
	DatacenterCode string `json:"datacenterCode"`
	Status         string `json:"status"`
	ChangeType     string `json:"changeType"`
	OldStatus      string `json:"oldStatus"`
}

// VPSSubscription represents a monitored VPS model & subsidiary subscription.
type VPSSubscription struct {
	ID                string            `json:"id"`
	PlanCode          string            `json:"planCode"`
	OVHSubsidiary     string            `json:"ovhSubsidiary"`
	Datacenters       []string          `json:"datacenters"`
	MonitorLinux      bool              `json:"monitorLinux"`
	MonitorWindows    bool              `json:"monitorWindows"`
	NotifyAvailable   bool              `json:"notifyAvailable"`
	NotifyUnavailable bool              `json:"notifyUnavailable"`
	LastStatus        map[string]string `json:"lastStatus"`
	CreatedAt         string            `json:"createdAt"`
	UpdatedAt         string            `json:"updatedAt,omitempty"`
}

// SKUWatcherState persists the public SKU diff baseline and recent changes.
type SKUWatcherState struct {
	Initialized bool            `json:"initialized"`
	Endpoint    string          `json:"endpoint"`
	SKUsJSON    json.RawMessage `json:"skus"`
	MissingJSON json.RawMessage `json:"missingCounts"`
	ChangesJSON json.RawMessage `json:"changes"`
	UpdatedAt   string          `json:"updatedAt"`
}

// ==================== order_queue ====================

func scanQueueItem(scanner interface{ Scan(...any) error }) (*QueueItem, error) {
	var item QueueItem
	var dcsJSON, optsJSON string
	var autoPayInt, waiveInt int
	if err := scanner.Scan(
		&item.ID,
		&item.AccountID,
		&item.PlanCode,
		&dcsJSON,
		&optsJSON,
		&item.ConfigKey,
		&item.ConfigLabel,
		&item.PricingMode,
		&item.Duration,
		&item.Coupon,
		&item.MaxMonthlyPrice,
		&item.Currency,
		&item.Quantity,
		&item.Purchased,
		&item.FailureCount,
		&item.MaxRetryCount,
		&item.Status,
		&item.RetryInterval,
		&item.RetryCount,
		&item.LastCheckTime,
		&item.NextAttemptAt,
		&autoPayInt,
		&waiveInt,
		&item.Source,
		&item.SourceRef,
		&item.LastError,
		&item.CreatedAt,
		&item.UpdatedAt,
	); err != nil {
		return nil, err
	}
	_ = json.Unmarshal([]byte(dcsJSON), &item.Datacenters)
	_ = json.Unmarshal([]byte(optsJSON), &item.Options)
	if item.Datacenters == nil {
		item.Datacenters = []string{}
	}
	if item.Options == nil {
		item.Options = []string{}
	}
	if strings.TrimSpace(item.Source) == "" {
		item.Source = "manual"
	}
	item.AutoPay = autoPayInt == 1
	item.WaiveRetractationPeriod = waiveInt == 1
	return &item, nil
}

// ListQueue returns queued tasks, optionally scoped to accountID when non-empty.
func (s *Store) ListQueue(ctx context.Context, accountID string) ([]QueueItem, error) {
	query := `
		SELECT id, account_id, plan_code, datacenters_json, options_json,
		       config_key, config_label, pricing_mode, duration, coupon, max_monthly_price, currency,
		       quantity, purchased, failure_count, max_retry_count, status,
		       retry_interval, retry_count, last_check_time, next_attempt_at,
		       auto_pay, waive_retractation_period, source, source_ref, last_error, created_at, updated_at
		FROM order_queue`
	var args []any
	if strings.TrimSpace(accountID) != "" {
		query += ` WHERE account_id = ?`
		args = append(args, strings.TrimSpace(accountID))
	}
	query += ` ORDER BY created_at ASC`

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("store: list queue: %w", err)
	}
	defer rows.Close()

	items := make([]QueueItem, 0)
	for rows.Next() {
		it, err := scanQueueItem(rows)
		if err != nil {
			return nil, fmt.Errorf("store: scan queue item: %w", err)
		}
		items = append(items, *it)
	}
	return items, rows.Err()
}

// GetQueueItem returns a single queued task by ID.
func (s *Store) GetQueueItem(ctx context.Context, id string) (*QueueItem, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, account_id, plan_code, datacenters_json, options_json,
		       config_key, config_label, pricing_mode, duration, coupon, max_monthly_price, currency,
		       quantity, purchased, failure_count, max_retry_count, status,
		       retry_interval, retry_count, last_check_time, next_attempt_at,
		       auto_pay, waive_retractation_period, source, source_ref, last_error, created_at, updated_at
		FROM order_queue WHERE id = ?
	`, id)
	it, err := scanQueueItem(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: get queue item %q: %w", id, err)
	}
	return it, nil
}

// CreateQueueItem persists a new procurement task. AutoPay and WaiveRetractationPeriod default to false.
func (s *Store) CreateQueueItem(ctx context.Context, item QueueItem) (*QueueItem, error) {
	if strings.TrimSpace(item.PlanCode) == "" {
		return nil, errors.New("store: queue item planCode is required")
	}
	if strings.TrimSpace(item.ID) == "" {
		item.ID = newUUID()
	}
	if item.Datacenters == nil {
		item.Datacenters = []string{}
	}
	if item.Options == nil {
		item.Options = []string{}
	}
	if item.Quantity <= 0 {
		item.Quantity = 1
	}
	if item.MaxRetryCount <= 0 {
		item.MaxRetryCount = 50
	}
	if item.RetryInterval <= 0 {
		item.RetryInterval = 30
	}
	if strings.TrimSpace(item.Status) == "" {
		item.Status = "running"
	}
	if strings.TrimSpace(item.Source) == "" {
		item.Source = "manual"
	} else {
		item.Source = strings.TrimSpace(item.Source)
	}
	item.SourceRef = strings.TrimSpace(item.SourceRef)
	item.ConfigKey = strings.TrimSpace(item.ConfigKey)
	item.ConfigLabel = strings.TrimSpace(item.ConfigLabel)
	item.PricingMode = strings.TrimSpace(item.PricingMode)
	item.Duration = strings.TrimSpace(item.Duration)
	item.Coupon = strings.TrimSpace(item.Coupon)
	item.Currency = strings.TrimSpace(item.Currency)
	now := nowUTC()
	if item.CreatedAt == "" {
		item.CreatedAt = now
	}
	item.UpdatedAt = now

	dcsJSON, _ := json.Marshal(item.Datacenters)
	optsJSON, _ := json.Marshal(item.Options)

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO order_queue (
			id, account_id, plan_code, datacenters_json, options_json,
			config_key, config_label, pricing_mode, duration, coupon, max_monthly_price, currency,
			quantity, purchased, failure_count, max_retry_count, status,
			retry_interval, retry_count, last_check_time, next_attempt_at,
			auto_pay, waive_retractation_period, source, source_ref, last_error, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		item.ID, item.AccountID, item.PlanCode, string(dcsJSON), string(optsJSON),
		item.ConfigKey, item.ConfigLabel, item.PricingMode, item.Duration, item.Coupon, item.MaxMonthlyPrice, item.Currency,
		item.Quantity, item.Purchased, item.FailureCount, item.MaxRetryCount, item.Status,
		item.RetryInterval, item.RetryCount, item.LastCheckTime, item.NextAttemptAt,
		boolToInt(item.AutoPay), boolToInt(item.WaiveRetractationPeriod), item.Source, item.SourceRef, item.LastError,
		item.CreatedAt, item.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("store: create queue item: %w", err)
	}
	out := item
	return &out, nil
}

// UpdateQueueItem updates an existing queued task.
func (s *Store) UpdateQueueItem(ctx context.Context, item QueueItem) (*QueueItem, error) {
	if item.Datacenters == nil {
		item.Datacenters = []string{}
	}
	if item.Options == nil {
		item.Options = []string{}
	}
	if strings.TrimSpace(item.Source) == "" {
		item.Source = "manual"
	} else {
		item.Source = strings.TrimSpace(item.Source)
	}
	item.SourceRef = strings.TrimSpace(item.SourceRef)
	item.ConfigKey = strings.TrimSpace(item.ConfigKey)
	item.ConfigLabel = strings.TrimSpace(item.ConfigLabel)
	item.PricingMode = strings.TrimSpace(item.PricingMode)
	item.Duration = strings.TrimSpace(item.Duration)
	item.Coupon = strings.TrimSpace(item.Coupon)
	item.Currency = strings.TrimSpace(item.Currency)
	item.UpdatedAt = nowUTC()
	dcsJSON, _ := json.Marshal(item.Datacenters)
	optsJSON, _ := json.Marshal(item.Options)

	res, err := s.db.ExecContext(ctx, `
		UPDATE order_queue SET
			account_id = ?, plan_code = ?, datacenters_json = ?, options_json = ?,
			config_key = ?, config_label = ?, pricing_mode = ?, duration = ?, coupon = ?, max_monthly_price = ?, currency = ?,
			quantity = ?, purchased = ?, failure_count = ?, max_retry_count = ?, status = ?,
			retry_interval = ?, retry_count = ?, last_check_time = ?, next_attempt_at = ?,
			auto_pay = ?, waive_retractation_period = ?, source = ?, source_ref = ?, last_error = ?, updated_at = ?
		WHERE id = ?
	`,
		item.AccountID, item.PlanCode, string(dcsJSON), string(optsJSON),
		item.ConfigKey, item.ConfigLabel, item.PricingMode, item.Duration, item.Coupon, item.MaxMonthlyPrice, item.Currency,
		item.Quantity, item.Purchased, item.FailureCount, item.MaxRetryCount, item.Status,
		item.RetryInterval, item.RetryCount, item.LastCheckTime, item.NextAttemptAt,
		boolToInt(item.AutoPay), boolToInt(item.WaiveRetractationPeriod), item.Source, item.SourceRef, item.LastError, item.UpdatedAt,
		item.ID,
	)
	if err != nil {
		return nil, fmt.Errorf("store: update queue item %q: %w", item.ID, err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return nil, ErrNotFound
	}
	out := item
	return &out, nil
}

// DeleteQueueItem removes a single queue item by ID.
func (s *Store) DeleteQueueItem(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM order_queue WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("store: delete queue item %q: %w", id, err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// ClearQueue removes all queue items (or those matching accountID when non-empty).
func (s *Store) ClearQueue(ctx context.Context, accountID string) (int64, error) {
	var res sql.Result
	var err error
	if strings.TrimSpace(accountID) != "" {
		res, err = s.db.ExecContext(ctx, `DELETE FROM order_queue WHERE account_id = ?`, strings.TrimSpace(accountID))
	} else {
		res, err = s.db.ExecContext(ctx, `DELETE FROM order_queue`)
	}
	if err != nil {
		return 0, fmt.Errorf("store: clear queue: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// ==================== purchase_history & stats ====================

// ListHistory returns purchase history entries newest first, optionally filtered by accountID.
func (s *Store) ListHistory(ctx context.Context, accountID string) ([]PurchaseHistory, error) {
	query := `
		SELECT id, task_id, account_id, plan_code, datacenter, options_json,
		       config_key, config_label, pricing_mode, duration, coupon, quantity, status,
		       order_id, order_url, price_text, price_json, auto_paid,
		       attempt_count, sequence, delivered_notified, error_message, timestamp
		FROM purchase_history`
	var args []any
	if strings.TrimSpace(accountID) != "" {
		query += ` WHERE account_id = ?`
		args = append(args, strings.TrimSpace(accountID))
	}
	query += ` ORDER BY timestamp DESC`

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("store: list history: %w", err)
	}
	defer rows.Close()

	entries := make([]PurchaseHistory, 0)
	for rows.Next() {
		var h PurchaseHistory
		var optsJSON, priceJSON string
		var autoPaidInt, deliveredInt int
		if err := rows.Scan(
			&h.ID, &h.TaskID, &h.AccountID, &h.PlanCode, &h.Datacenter, &optsJSON,
			&h.ConfigKey, &h.ConfigLabel, &h.PricingMode, &h.Duration, &h.Coupon, &h.Quantity, &h.Status,
			&h.OrderID, &h.OrderURL, &h.PriceText, &priceJSON, &autoPaidInt,
			&h.AttemptCount, &h.Sequence, &deliveredInt, &h.ErrorMessage, &h.Timestamp,
		); err != nil {
			return nil, fmt.Errorf("store: scan history: %w", err)
		}
		_ = json.Unmarshal([]byte(optsJSON), &h.Options)
		if h.Options == nil {
			h.Options = []string{}
		}
		if h.Quantity <= 0 {
			h.Quantity = 1
		}
		if priceJSON != "" && priceJSON != "{}" && priceJSON != "null" {
			var p PriceInfo
			if json.Unmarshal([]byte(priceJSON), &p) == nil {
				h.Price = &p
				if h.SetupFee == 0 && p.SetupFee > 0 {
					h.SetupFee = p.SetupFee
				}
				if h.PeriodPrice == 0 && p.PeriodPrice > 0 {
					h.PeriodPrice = p.PeriodPrice
				}
				if h.MonthlyPrice == 0 && p.MonthlyExVAT > 0 {
					h.MonthlyPrice = p.MonthlyExVAT
				}
				if h.PricingMode == "" && p.PricingMode != "" {
					h.PricingMode = p.PricingMode
				}
				if h.Duration == "" && p.Duration != "" {
					h.Duration = p.Duration
				}
			}
		}
		h.AutoPaid = autoPaidInt == 1
		h.DeliveredNotified = deliveredInt == 1
		entries = append(entries, h)
	}
	return entries, rows.Err()
}

// AddHistory appends a purchase history entry.
func (s *Store) AddHistory(ctx context.Context, entry PurchaseHistory) (*PurchaseHistory, error) {
	if strings.TrimSpace(entry.ID) == "" {
		entry.ID = newUUID()
	}
	if entry.Options == nil {
		entry.Options = []string{}
	}
	if entry.Quantity <= 0 {
		entry.Quantity = 1
	}
	if entry.Timestamp == "" {
		entry.Timestamp = nowUTC()
	}
	entry.PricingMode = strings.TrimSpace(entry.PricingMode)
	entry.Duration = strings.TrimSpace(entry.Duration)
	entry.Coupon = strings.TrimSpace(entry.Coupon)

	optsJSON, _ := json.Marshal(entry.Options)
	priceJSON := "{}"
	if entry.Price == nil && (entry.PeriodPrice > 0 || entry.MonthlyPrice > 0 || entry.SetupFee > 0) {
		entry.Price = &PriceInfo{
			PeriodPrice:  entry.PeriodPrice,
			MonthlyExVAT: entry.MonthlyPrice,
			SetupFee:     entry.SetupFee,
			PricingMode:  entry.PricingMode,
			Duration:     entry.Duration,
		}
	}
	if entry.Price != nil {
		if entry.SetupFee > 0 && entry.Price.SetupFee == 0 {
			entry.Price.SetupFee = entry.SetupFee
		}
		if entry.PeriodPrice > 0 && entry.Price.PeriodPrice == 0 {
			entry.Price.PeriodPrice = entry.PeriodPrice
		}
		if entry.MonthlyPrice > 0 && entry.Price.MonthlyExVAT == 0 {
			entry.Price.MonthlyExVAT = entry.MonthlyPrice
		}
		if entry.PricingMode != "" && entry.Price.PricingMode == "" {
			entry.Price.PricingMode = entry.PricingMode
		}
		if entry.Duration != "" && entry.Price.Duration == "" {
			entry.Price.Duration = entry.Duration
		}
		if b, err := json.Marshal(entry.Price); err == nil {
			priceJSON = string(b)
		}
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO purchase_history (
			id, task_id, account_id, plan_code, datacenter, options_json,
			config_key, config_label, pricing_mode, duration, coupon, quantity, status,
			order_id, order_url, price_text, price_json, auto_paid,
			attempt_count, sequence, delivered_notified, error_message, timestamp
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		entry.ID, entry.TaskID, entry.AccountID, entry.PlanCode, entry.Datacenter, string(optsJSON),
		entry.ConfigKey, entry.ConfigLabel, entry.PricingMode, entry.Duration, entry.Coupon, entry.Quantity, entry.Status,
		entry.OrderID, entry.OrderURL, entry.PriceText, priceJSON, boolToInt(entry.AutoPaid),
		entry.AttemptCount, entry.Sequence, boolToInt(entry.DeliveredNotified), entry.ErrorMessage, entry.Timestamp,
	)
	if err != nil {
		return nil, fmt.Errorf("store: add history: %w", err)
	}
	out := entry
	return &out, nil
}

// MarkOrderDeliveredNotified marks a history record as having dispatched its delivery notification.
func (s *Store) MarkOrderDeliveredNotified(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `UPDATE purchase_history SET delivered_notified = 1 WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("store: mark delivered notified %q: %w", id, err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// ClearHistory deletes purchase history entries (all or filtered by accountID).
func (s *Store) ClearHistory(ctx context.Context, accountID string) (int64, error) {
	var res sql.Result
	var err error
	if strings.TrimSpace(accountID) != "" {
		res, err = s.db.ExecContext(ctx, `DELETE FROM purchase_history WHERE account_id = ?`, strings.TrimSpace(accountID))
	} else {
		res, err = s.db.ExecContext(ctx, `DELETE FROM purchase_history`)
	}
	if err != nil {
		return 0, fmt.Errorf("store: clear history: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// ComputeStats calculates active queue counts and purchase success/failure totals.
func (s *Store) ComputeStats(ctx context.Context, accountID string) (*Stats, error) {
	var st Stats
	aid := strings.TrimSpace(accountID)

	if aid != "" {
		if err := s.db.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM order_queue WHERE account_id = ? AND status IN ('running', 'pending', 'paused')`, aid,
		).Scan(&st.ActiveQueues); err != nil {
			return nil, fmt.Errorf("store: compute queue stats: %w", err)
		}
		if err := s.db.QueryRowContext(ctx,
			`SELECT
				COALESCE(SUM(CASE WHEN status = 'success' THEN 1 ELSE 0 END), 0),
				COALESCE(SUM(CASE WHEN status = 'failed' THEN 1 ELSE 0 END), 0)
			 FROM purchase_history WHERE account_id = ?`, aid,
		).Scan(&st.PurchaseSuccess, &st.PurchaseFailed); err != nil {
			return nil, fmt.Errorf("store: compute history stats: %w", err)
		}
	} else {
		if err := s.db.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM order_queue WHERE status IN ('running', 'pending', 'paused')`,
		).Scan(&st.ActiveQueues); err != nil {
			return nil, fmt.Errorf("store: compute queue stats: %w", err)
		}
		if err := s.db.QueryRowContext(ctx,
			`SELECT
				COALESCE(SUM(CASE WHEN status = 'success' THEN 1 ELSE 0 END), 0),
				COALESCE(SUM(CASE WHEN status = 'failed' THEN 1 ELSE 0 END), 0)
			 FROM purchase_history`,
		).Scan(&st.PurchaseSuccess, &st.PurchaseFailed); err != nil {
			return nil, fmt.Errorf("store: compute history stats: %w", err)
		}
	}
	return &st, nil
}

// ==================== stock_subscriptions ====================

// MigrateLegacyStockSub migrates a legacy StockSubscription with empty Targets into a single-target notify subscription.
// It returns true if migration was performed, and pausedAutoOrder=true if legacy AutoOrder was paused.
func MigrateLegacyStockSub(sub *StockSubscription) (migrated bool, pausedAutoOrder bool) {
	if sub == nil {
		return false, false
	}
	if sub.Datacenters == nil {
		sub.Datacenters = []string{}
	}
	if sub.LastStatus == nil {
		sub.LastStatus = map[string]string{}
	}
	if sub.History == nil {
		sub.History = []StockHistoryEntry{}
	}
	if len(sub.Targets) == 0 {
		lastCopy := make(map[string]string, len(sub.LastStatus))
		for k, v := range sub.LastStatus {
			lastCopy[k] = v
		}
		dcsCopy := append([]string{}, sub.Datacenters...)
		if dcsCopy == nil {
			dcsCopy = []string{}
		}
		sub.Targets = []StockTarget{
			{
				ID:                "default",
				ConfigKey:         "",
				Options:           []string{},
				Label:             "",
				Datacenters:       dcsCopy,
				NotifyAvailable:   sub.NotifyAvailable,
				NotifyUnavailable: sub.NotifyUnavailable,
				AutoOrder:         false,
				Quantity:          max(1, sub.AutoOrderQuantity),
				LastStatus:        lastCopy,
			},
		}
		if sub.AutoOrder {
			sub.AutoOrder = false
			sub.NeedsConfigSelection = true
			pausedAutoOrder = true
		}
		return true, pausedAutoOrder
	}

	for i := range sub.Targets {
		t := &sub.Targets[i]
		if strings.TrimSpace(t.ID) == "" {
			t.ID = fmt.Sprintf("tgt-%d", i+1)
		}
		if t.Options == nil {
			t.Options = []string{}
		}
		if t.Datacenters == nil {
			t.Datacenters = []string{}
		}
		if t.LastStatus == nil {
			t.LastStatus = map[string]string{}
		}
		if t.Quantity <= 0 {
			t.Quantity = 1
		}
	}
	return false, false
}

// ListStockSubs lists dedicated stock subscriptions, optionally filtered by accountID,
// transparently migrating legacy subscriptions that lack explicit targets.
func (s *Store) ListStockSubs(ctx context.Context, accountID string) ([]StockSubscription, error) {
	query := `
		SELECT account_id, plan_code, server_name, subsidiary, datacenters_json,
		       notify_available, notify_unavailable, auto_order, auto_order_quantity,
		       targets_json, needs_config_selection, last_status_json, history_json, created_at, updated_at
		FROM stock_subscriptions`
	var args []any
	if strings.TrimSpace(accountID) != "" {
		query += ` WHERE account_id = ?`
		args = append(args, strings.TrimSpace(accountID))
	}
	query += ` ORDER BY created_at ASC`

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("store: list stock subscriptions: %w", err)
	}

	subs := make([]StockSubscription, 0)
	for rows.Next() {
		var sub StockSubscription
		var dcsJSON, targetsJSON, lastJSON, histJSON string
		var nAvail, nUnavail, autoOrd, needsCfg int
		if err := rows.Scan(
			&sub.AccountID, &sub.PlanCode, &sub.ServerName, &sub.Subsidiary, &dcsJSON,
			&nAvail, &nUnavail, &autoOrd, &sub.AutoOrderQuantity,
			&targetsJSON, &needsCfg, &lastJSON, &histJSON, &sub.CreatedAt, &sub.UpdatedAt,
		); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("store: scan stock subscription: %w", err)
		}
		_ = json.Unmarshal([]byte(dcsJSON), &sub.Datacenters)
		_ = json.Unmarshal([]byte(targetsJSON), &sub.Targets)
		_ = json.Unmarshal([]byte(lastJSON), &sub.LastStatus)
		_ = json.Unmarshal([]byte(histJSON), &sub.History)
		if sub.Datacenters == nil {
			sub.Datacenters = []string{}
		}
		if sub.LastStatus == nil {
			sub.LastStatus = map[string]string{}
		}
		if sub.History == nil {
			sub.History = []StockHistoryEntry{}
		}
		sub.NotifyAvailable = nAvail == 1
		sub.NotifyUnavailable = nUnavail == 1
		sub.AutoOrder = autoOrd == 1
		sub.NeedsConfigSelection = needsCfg == 1
		subs = append(subs, sub)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	_ = rows.Close()

	for i := range subs {
		migrated, pausedAuto := MigrateLegacyStockSub(&subs[i])
		if migrated {
			if pausedAuto {
				slog.Info("migrated legacy stock subscription: paused auto-order pending explicit configuration selection", "planCode", subs[i].PlanCode)
				_, _ = s.InsertLog(
					ctx,
					nowUTC(),
					"INFO",
					"stock-monitor",
					"migrated legacy stock subscription: paused auto-order pending explicit configuration selection",
					fmt.Sprintf(`{"planCode":%q}`, subs[i].PlanCode),
				)
			}
			if saved, upErr := s.UpsertStockSub(ctx, subs[i]); upErr == nil && saved != nil {
				subs[i] = *saved
			}
		}
	}
	return subs, nil
}

// UpsertStockSub creates or updates a stock monitor subscription for (account_id, plan_code).
func (s *Store) UpsertStockSub(ctx context.Context, sub StockSubscription) (*StockSubscription, error) {
	if strings.TrimSpace(sub.PlanCode) == "" {
		return nil, errors.New("store: stock subscription planCode is required")
	}
	if sub.Datacenters == nil {
		sub.Datacenters = []string{}
	}
	if sub.Targets == nil {
		sub.Targets = []StockTarget{}
	}
	for i := range sub.Targets {
		t := &sub.Targets[i]
		if strings.TrimSpace(t.ID) == "" {
			t.ID = fmt.Sprintf("tgt-%d", i+1)
		}
		if t.Options == nil {
			t.Options = []string{}
		}
		if t.Datacenters == nil {
			t.Datacenters = []string{}
		}
		if t.LastStatus == nil {
			t.LastStatus = map[string]string{}
		}
		if t.Quantity <= 0 {
			t.Quantity = 1
		}
	}
	if sub.LastStatus == nil {
		sub.LastStatus = map[string]string{}
	}
	if sub.History == nil {
		sub.History = []StockHistoryEntry{}
	}
	if len(sub.History) > 100 {
		sub.History = sub.History[len(sub.History)-100:]
	}
	now := nowUTC()
	if sub.CreatedAt == "" {
		sub.CreatedAt = now
	}
	sub.UpdatedAt = now

	dcsJSON, _ := json.Marshal(sub.Datacenters)
	targetsJSON, _ := json.Marshal(sub.Targets)
	lastJSON, _ := json.Marshal(sub.LastStatus)
	histJSON, _ := json.Marshal(sub.History)

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO stock_subscriptions (
			account_id, plan_code, server_name, subsidiary, datacenters_json,
			notify_available, notify_unavailable, auto_order, auto_order_quantity,
			targets_json, needs_config_selection, last_status_json, history_json, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(account_id, plan_code) DO UPDATE SET
			server_name = excluded.server_name,
			subsidiary = excluded.subsidiary,
			datacenters_json = excluded.datacenters_json,
			notify_available = excluded.notify_available,
			notify_unavailable = excluded.notify_unavailable,
			auto_order = excluded.auto_order,
			auto_order_quantity = excluded.auto_order_quantity,
			targets_json = excluded.targets_json,
			needs_config_selection = excluded.needs_config_selection,
			last_status_json = excluded.last_status_json,
			history_json = excluded.history_json,
			updated_at = excluded.updated_at
	`,
		sub.AccountID, sub.PlanCode, sub.ServerName, sub.Subsidiary, string(dcsJSON),
		boolToInt(sub.NotifyAvailable), boolToInt(sub.NotifyUnavailable),
		boolToInt(sub.AutoOrder), sub.AutoOrderQuantity,
		string(targetsJSON), boolToInt(sub.NeedsConfigSelection),
		string(lastJSON), string(histJSON), sub.CreatedAt, sub.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("store: upsert stock subscription: %w", err)
	}
	out := sub
	return &out, nil
}

// BatchUpsertStockSubs inserts or updates multiple stock subscriptions in a single transaction,
// returning counts of newly inserted vs updated/existing rows.
func (s *Store) BatchUpsertStockSubs(ctx context.Context, subs []StockSubscription) (int, int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, 0, fmt.Errorf("store: begin batch stock sub tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	inserted := 0
	updated := 0
	now := nowUTC()

	for _, sub := range subs {
		if strings.TrimSpace(sub.PlanCode) == "" {
			continue
		}
		if sub.Datacenters == nil {
			sub.Datacenters = []string{}
		}
		if sub.Targets == nil {
			sub.Targets = []StockTarget{}
		}
		for i := range sub.Targets {
			t := &sub.Targets[i]
			if strings.TrimSpace(t.ID) == "" {
				t.ID = fmt.Sprintf("tgt-%d", i+1)
			}
			if t.Options == nil {
				t.Options = []string{}
			}
			if t.Datacenters == nil {
				t.Datacenters = []string{}
			}
			if t.LastStatus == nil {
				t.LastStatus = map[string]string{}
			}
			if t.Quantity <= 0 {
				t.Quantity = 1
			}
		}
		if sub.LastStatus == nil {
			sub.LastStatus = map[string]string{}
		}
		if sub.History == nil {
			sub.History = []StockHistoryEntry{}
		}
		if sub.CreatedAt == "" {
			sub.CreatedAt = now
		}
		sub.UpdatedAt = now

		var exists int
		if err := tx.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM stock_subscriptions WHERE account_id = ? AND plan_code = ?`,
			sub.AccountID, sub.PlanCode,
		).Scan(&exists); err != nil {
			return 0, 0, err
		}

		dcsJSON, _ := json.Marshal(sub.Datacenters)
		targetsJSON, _ := json.Marshal(sub.Targets)
		lastJSON, _ := json.Marshal(sub.LastStatus)
		histJSON, _ := json.Marshal(sub.History)

		if _, err := tx.ExecContext(ctx, `
			INSERT INTO stock_subscriptions (
				account_id, plan_code, server_name, subsidiary, datacenters_json,
				notify_available, notify_unavailable, auto_order, auto_order_quantity,
				targets_json, needs_config_selection, last_status_json, history_json, created_at, updated_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(account_id, plan_code) DO UPDATE SET
				server_name = excluded.server_name,
				subsidiary = excluded.subsidiary,
				datacenters_json = excluded.datacenters_json,
				notify_available = excluded.notify_available,
				notify_unavailable = excluded.notify_unavailable,
				auto_order = excluded.auto_order,
				auto_order_quantity = excluded.auto_order_quantity,
				targets_json = excluded.targets_json,
				needs_config_selection = excluded.needs_config_selection,
				last_status_json = excluded.last_status_json,
				history_json = excluded.history_json,
				updated_at = excluded.updated_at
		`,
			sub.AccountID, sub.PlanCode, sub.ServerName, sub.Subsidiary, string(dcsJSON),
			boolToInt(sub.NotifyAvailable), boolToInt(sub.NotifyUnavailable),
			boolToInt(sub.AutoOrder), sub.AutoOrderQuantity,
			string(targetsJSON), boolToInt(sub.NeedsConfigSelection),
			string(lastJSON), string(histJSON), sub.CreatedAt, sub.UpdatedAt,
		); err != nil {
			return 0, 0, fmt.Errorf("store: batch upsert stock sub %q: %w", sub.PlanCode, err)
		}

		if exists > 0 {
			updated++
		} else {
			inserted++
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, 0, fmt.Errorf("store: commit batch stock subs: %w", err)
	}
	return inserted, updated, nil
}

// DeleteStockSub deletes a stock subscription by (accountID, planCode).
func (s *Store) DeleteStockSub(ctx context.Context, accountID, planCode string) error {
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM stock_subscriptions WHERE account_id = ? AND plan_code = ?`,
		strings.TrimSpace(accountID), strings.TrimSpace(planCode),
	)
	if err != nil {
		return fmt.Errorf("store: delete stock subscription: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// ClearStockSubs removes stock subscriptions (all or for a specific accountID).
func (s *Store) ClearStockSubs(ctx context.Context, accountID string) (int64, error) {
	var res sql.Result
	var err error
	if strings.TrimSpace(accountID) != "" {
		res, err = s.db.ExecContext(ctx, `DELETE FROM stock_subscriptions WHERE account_id = ?`, strings.TrimSpace(accountID))
	} else {
		res, err = s.db.ExecContext(ctx, `DELETE FROM stock_subscriptions`)
	}
	if err != nil {
		return 0, fmt.Errorf("store: clear stock subscriptions: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// ==================== vps_subscriptions & history ====================

func scanVPSSub(scanner interface{ Scan(...any) error }) (*VPSSubscription, error) {
	var sub VPSSubscription
	var dcsJSON, lastJSON string
	var linuxInt, winInt, nAvail, nUnavail int
	if err := scanner.Scan(
		&sub.ID, &sub.PlanCode, &sub.OVHSubsidiary, &dcsJSON,
		&linuxInt, &winInt, &nAvail, &nUnavail,
		&lastJSON, &sub.CreatedAt, &sub.UpdatedAt,
	); err != nil {
		return nil, err
	}
	_ = json.Unmarshal([]byte(dcsJSON), &sub.Datacenters)
	_ = json.Unmarshal([]byte(lastJSON), &sub.LastStatus)
	if sub.Datacenters == nil {
		sub.Datacenters = []string{}
	}
	if sub.LastStatus == nil {
		sub.LastStatus = map[string]string{}
	}
	sub.MonitorLinux = linuxInt == 1
	sub.MonitorWindows = winInt == 1
	sub.NotifyAvailable = nAvail == 1
	sub.NotifyUnavailable = nUnavail == 1
	return &sub, nil
}

// ListVPSSubs returns all VPS monitor subscriptions.
func (s *Store) ListVPSSubs(ctx context.Context) ([]VPSSubscription, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, plan_code, ovh_subsidiary, datacenters_json,
		       monitor_linux, monitor_windows, notify_available, notify_unavailable,
		       last_status_json, created_at, updated_at
		FROM vps_subscriptions
		ORDER BY created_at ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("store: list vps subscriptions: %w", err)
	}
	defer rows.Close()

	subs := make([]VPSSubscription, 0)
	for rows.Next() {
		sub, err := scanVPSSub(rows)
		if err != nil {
			return nil, fmt.Errorf("store: scan vps subscription: %w", err)
		}
		subs = append(subs, *sub)
	}
	return subs, rows.Err()
}

// GetVPSSub fetches a single VPS subscription by ID.
func (s *Store) GetVPSSub(ctx context.Context, id string) (*VPSSubscription, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, plan_code, ovh_subsidiary, datacenters_json,
		       monitor_linux, monitor_windows, notify_available, notify_unavailable,
		       last_status_json, created_at, updated_at
		FROM vps_subscriptions WHERE id = ?
	`, id)
	sub, err := scanVPSSub(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: get vps subscription %q: %w", id, err)
	}
	return sub, nil
}

// UpsertVPSSub creates or updates a VPS subscription.
func (s *Store) UpsertVPSSub(ctx context.Context, sub VPSSubscription) (*VPSSubscription, error) {
	if strings.TrimSpace(sub.PlanCode) == "" {
		return nil, errors.New("store: vps subscription planCode is required")
	}
	if strings.TrimSpace(sub.ID) == "" {
		sub.ID = newUUID()
	}
	if strings.TrimSpace(sub.OVHSubsidiary) == "" {
		sub.OVHSubsidiary = "IE"
	}
	if sub.Datacenters == nil {
		sub.Datacenters = []string{}
	}
	if sub.LastStatus == nil {
		sub.LastStatus = map[string]string{}
	}
	now := nowUTC()
	if sub.CreatedAt == "" {
		sub.CreatedAt = now
	}
	sub.UpdatedAt = now

	dcsJSON, _ := json.Marshal(sub.Datacenters)
	lastJSON, _ := json.Marshal(sub.LastStatus)

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO vps_subscriptions (
			id, plan_code, ovh_subsidiary, datacenters_json,
			monitor_linux, monitor_windows, notify_available, notify_unavailable,
			last_status_json, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			plan_code = excluded.plan_code,
			ovh_subsidiary = excluded.ovh_subsidiary,
			datacenters_json = excluded.datacenters_json,
			monitor_linux = excluded.monitor_linux,
			monitor_windows = excluded.monitor_windows,
			notify_available = excluded.notify_available,
			notify_unavailable = excluded.notify_unavailable,
			last_status_json = excluded.last_status_json,
			updated_at = excluded.updated_at
	`,
		sub.ID, sub.PlanCode, sub.OVHSubsidiary, string(dcsJSON),
		boolToInt(sub.MonitorLinux), boolToInt(sub.MonitorWindows),
		boolToInt(sub.NotifyAvailable), boolToInt(sub.NotifyUnavailable),
		string(lastJSON), sub.CreatedAt, sub.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("store: upsert vps subscription: %w", err)
	}
	out := sub
	return &out, nil
}

// DeleteVPSSub removes a VPS subscription and its cascaded history entries.
func (s *Store) DeleteVPSSub(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM vps_subscriptions WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("store: delete vps subscription %q: %w", id, err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// ClearVPSSubs deletes all VPS subscriptions.
func (s *Store) ClearVPSSubs(ctx context.Context) (int64, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM vps_subscriptions`)
	if err != nil {
		return 0, fmt.Errorf("store: clear vps subscriptions: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// AppendVPSHistory records a VPS availability transition and prunes older entries beyond 100 per subscription.
func (s *Store) AppendVPSHistory(ctx context.Context, subscriptionID string, entry VPSHistoryEntry) error {
	if entry.Timestamp == "" {
		entry.Timestamp = nowUTC()
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO vps_history (subscription_id, timestamp, datacenter, datacenter_code, status, change_type, old_status)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, subscriptionID, entry.Timestamp, entry.Datacenter, entry.DatacenterCode, entry.Status, entry.ChangeType, entry.OldStatus)
	if err != nil {
		return fmt.Errorf("store: append vps history: %w", err)
	}

	_, _ = s.db.ExecContext(ctx, `
		DELETE FROM vps_history
		WHERE subscription_id = ? AND id NOT IN (
			SELECT id FROM vps_history WHERE subscription_id = ? ORDER BY id DESC LIMIT 100
		)
	`, subscriptionID, subscriptionID)
	return nil
}

// GetVPSHistory returns the availability history for a VPS subscription ordered newest first.
func (s *Store) GetVPSHistory(ctx context.Context, subscriptionID string) ([]VPSHistoryEntry, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, subscription_id, timestamp, datacenter, datacenter_code, status, change_type, old_status
		FROM vps_history
		WHERE subscription_id = ?
		ORDER BY id DESC
		LIMIT 100
	`, subscriptionID)
	if err != nil {
		return nil, fmt.Errorf("store: get vps history %q: %w", subscriptionID, err)
	}
	defer rows.Close()

	entries := make([]VPSHistoryEntry, 0)
	for rows.Next() {
		var e VPSHistoryEntry
		if err := rows.Scan(&e.ID, &e.SubscriptionID, &e.Timestamp, &e.Datacenter, &e.DatacenterCode, &e.Status, &e.ChangeType, &e.OldStatus); err != nil {
			return nil, fmt.Errorf("store: scan vps history: %w", err)
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

// ==================== sku_watcher_state ====================

// LoadSKUWatcherState loads the singleton SKU watcher snapshot and change history.
func (s *Store) LoadSKUWatcherState(ctx context.Context) (*SKUWatcherState, error) {
	var initInt int
	var endpoint, skusJSON, missingJSON, changesJSON, updatedAt string
	err := s.db.QueryRowContext(ctx, `
		SELECT initialized, endpoint, skus_json, missing_counts_json, changes_json, updated_at
		FROM sku_watcher_state WHERE id = 1
	`).Scan(&initInt, &endpoint, &skusJSON, &missingJSON, &changesJSON, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return &SKUWatcherState{
			Initialized: false,
			Endpoint:    "ovh-eu",
			SKUsJSON:    json.RawMessage(`{}`),
			MissingJSON: json.RawMessage(`{}`),
			ChangesJSON: json.RawMessage(`[]`),
		}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("store: load sku_watcher_state: %w", err)
	}
	if skusJSON == "" {
		skusJSON = "{}"
	}
	if missingJSON == "" {
		missingJSON = "{}"
	}
	if changesJSON == "" {
		changesJSON = "[]"
	}
	return &SKUWatcherState{
		Initialized: initInt == 1,
		Endpoint:    endpoint,
		SKUsJSON:    json.RawMessage(skusJSON),
		MissingJSON: json.RawMessage(missingJSON),
		ChangesJSON: json.RawMessage(changesJSON),
		UpdatedAt:   updatedAt,
	}, nil
}

// SaveSKUWatcherState persists the singleton SKU watcher snapshot and change history.
func (s *Store) SaveSKUWatcherState(ctx context.Context, st *SKUWatcherState) error {
	if st == nil {
		return errors.New("store: sku watcher state is nil")
	}
	endpoint := strings.TrimSpace(st.Endpoint)
	if endpoint == "" {
		endpoint = "ovh-eu"
	}
	skus := st.SKUsJSON
	if len(skus) == 0 {
		skus = json.RawMessage(`{}`)
	}
	missing := st.MissingJSON
	if len(missing) == 0 {
		missing = json.RawMessage(`{}`)
	}
	changes := st.ChangesJSON
	if len(changes) == 0 {
		changes = json.RawMessage(`[]`)
	}
	st.UpdatedAt = nowUTC()

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO sku_watcher_state (id, initialized, endpoint, skus_json, missing_counts_json, changes_json, updated_at)
		VALUES (1, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			initialized = excluded.initialized,
			endpoint = excluded.endpoint,
			skus_json = excluded.skus_json,
			missing_counts_json = excluded.missing_counts_json,
			changes_json = excluded.changes_json,
			updated_at = excluded.updated_at
	`, boolToInt(st.Initialized), endpoint, string(skus), string(missing), string(changes), st.UpdatedAt)
	if err != nil {
		return fmt.Errorf("store: save sku_watcher_state: %w", err)
	}
	return nil
}
