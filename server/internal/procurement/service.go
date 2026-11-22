package procurement

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"math/rand/v2"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ovh/go-ovh/ovh"

	"ovh-nexus/server/internal/catalog"
	"ovh-nexus/server/internal/logger"
	"ovh-nexus/server/internal/notify"
	"ovh-nexus/server/internal/ovhclient"
	"ovh-nexus/server/internal/store"
)

var (
	// ErrOutOfStock indicates the target planCode is unavailable in all requested datacenters.
	ErrOutOfStock = errors.New("server is currently out of stock in the requested datacenters")
	// ErrNoPricedConfig indicates no available configuration with valid catalog options exists in the target datacenter.
	ErrNoPricedConfig = errors.New("No priced configuration available in the specified datacenter")
	// ErrConfigRequiredForAutoOrder indicates stock-monitor auto-order was triggered without explicit configuration options.
	ErrConfigRequiredForAutoOrder = errors.New("CONFIG_REQUIRED_FOR_AUTO_ORDER: stock-monitor auto-order requires explicit configuration options")
	// ErrPriceAboveLimit indicates the live OVH cart recurring monthly ex-VAT price or setup fee exceeds configured limits.
	ErrPriceAboveLimit = errors.New("PRICE_ABOVE_LIMIT: live cart price exceeds configured price cap")
	// ErrPriceChangedSinceQuote indicates the live OVH cart price increased above the quoted ex-VAT monthly price or setup fee.
	ErrPriceChangedSinceQuote = errors.New("PRICE_CHANGED_SINCE_QUOTE: live cart price exceeded quoted price")
	// ErrInvalidCoupon indicates OVH rejected the custom promo/voucher coupon code.
	ErrInvalidCoupon = errors.New("INVALID_COUPON: coupon code rejected by OVH")
	// ErrDuplicateOrder indicates an active queue task or recent order exists within the 2-minute window.
	ErrDuplicateOrder = errors.New("duplicate order detected within the 2-minute cooldown window")
	// ErrPriceCheckFailed indicates OVH price validation failed or returned zero price.
	ErrPriceCheckFailed = errors.New("price validation failed for the requested configuration")
)

// QuoteRequest holds parameters for quoting a server order (Step 1 of unpaid ordering).
type QuoteRequest struct {
	AccountID   string   `json:"accountId"`
	PlanCode    string   `json:"planCode"`
	Datacenter  string   `json:"datacenter"`
	Options     []string `json:"options"`
	PricingMode string   `json:"pricingMode,omitempty"`
	Duration    string   `json:"duration,omitempty"`
	Coupon      string   `json:"coupon,omitempty"`
}

// QuoteResult holds the monthly price breakdown returned by QuoteOrder.
type QuoteResult struct {
	PlanCode             string                      `json:"planCode"`
	ModelName            string                      `json:"modelName"`
	Datacenter           string                      `json:"datacenter"`
	Options              []string                    `json:"options"`
	PricingMode          string                      `json:"pricingMode,omitempty"`
	Duration             string                      `json:"duration,omitempty"`
	Coupon               string                      `json:"coupon,omitempty"`
	CouponStripped       bool                        `json:"couponStripped,omitempty"`
	AutoAppliedPromoCode string                      `json:"autoAppliedPromoCode,omitempty"`
	Price                *catalog.ServerPricePayload `json:"price"`
}

// UnpaidOrderRequest holds parameters for placing an unpaid order (auto_pay=false, quantity=1 hardcoded).
type UnpaidOrderRequest struct {
	AccountID       string   `json:"accountId"`
	PlanCode        string   `json:"planCode"`
	Datacenter      string   `json:"datacenter"`
	Options         []string `json:"options"`
	PricingMode     string   `json:"pricingMode,omitempty"`
	Duration        string   `json:"duration,omitempty"`
	Coupon          string   `json:"coupon,omitempty"`
	TaskID          string   `json:"taskId,omitempty"`
	MaxMonthlyPrice float64  `json:"maxMonthlyPrice,omitempty"`
	MaxSetupFee     float64  `json:"maxSetupFee,omitempty"`
	EnforceQuoteCap bool     `json:"enforceQuoteCap,omitempty"`
}

// UnpaidOrderResult holds the created unpaid order summary.
type UnpaidOrderResult struct {
	HistoryID               string           `json:"historyId"`
	OrderID                 string           `json:"orderId"`
	OrderURL                string           `json:"orderUrl"`
	PlanCode                string           `json:"planCode"`
	Datacenter              string           `json:"datacenter"`
	Options                 []string         `json:"options"`
	PricingMode             string           `json:"pricingMode,omitempty"`
	Duration                string           `json:"duration,omitempty"`
	Coupon                  string           `json:"coupon,omitempty"`
	Price                   *store.PriceInfo `json:"price,omitempty"`
	AutoPay                 bool             `json:"autoPay"`
	WaiveRetractationPeriod bool             `json:"waiveRetractationPeriod"`
	Quantity                int              `json:"quantity"`
}

// QuickOrderRequest holds parameters for POST /api/orders/quick-order and stock auto-order triggers.
type QuickOrderRequest struct {
	AccountID               string   `json:"accountId"`
	PlanCode                string   `json:"planCode"`
	Datacenter              string   `json:"datacenter"`
	Options                 []string `json:"options"`
	ConfigKey               string   `json:"configKey,omitempty"`
	ConfigLabel             string   `json:"configLabel,omitempty"`
	PricingMode             string   `json:"pricingMode,omitempty"`
	Duration                string   `json:"duration,omitempty"`
	Coupon                  string   `json:"coupon,omitempty"`
	Quantity                int      `json:"quantity"`
	MaxMonthlyPrice         float64  `json:"maxMonthlyPrice"`
	MaxSetupFee             float64  `json:"maxSetupFee"`
	Currency                string   `json:"currency,omitempty"`
	Subsidiary              string   `json:"subsidiary,omitempty"`
	SkipPriceCheck          bool     `json:"skipPriceCheck"`
	SkipDuplicateCheck      bool     `json:"skipDuplicateCheck"`
	AutoPay                 bool     `json:"auto_pay"`
	WaiveRetractationPeriod bool     `json:"waive_retractation_period"`
	Source                  string   `json:"source,omitempty"`
	SourceRef               string   `json:"sourceRef,omitempty"`
}

// QuickOrderResult holds the queued quick-order task and validated quote payload.
type QuickOrderResult struct {
	Success      bool                        `json:"success"`
	ID           string                      `json:"id"`
	Message      string                      `json:"message"`
	Price        *catalog.ServerPricePayload `json:"price,omitempty"`
	PeriodPrice  float64                     `json:"periodPrice,omitempty"`
	MonthlyPrice float64                     `json:"monthlyPrice,omitempty"`
	SetupFee     float64                     `json:"setupFee,omitempty"`
	PricingMode          string                      `json:"pricingMode,omitempty"`
	Duration             string                      `json:"duration,omitempty"`
	Coupon               string                      `json:"coupon,omitempty"`
	CouponStripped       bool                        `json:"couponStripped,omitempty"`
	AutoAppliedPromoCode string                      `json:"autoAppliedPromoCode,omitempty"`
	Quantity             int                         `json:"quantity,omitempty"`
	OrderIDs     []string                    `json:"orderIds,omitempty"`
	Options      []string                    `json:"options"`
	Code         string                      `json:"code,omitempty"`
	ErrorCode    string                      `json:"errorCode,omitempty"`
	Error        string                      `json:"error,omitempty"`
	Item         *store.QueueItem            `json:"item,omitempty"`
}

// CreateQueueRequest captures queue task creation fields. AutoPay and WaiveRetractationPeriod default to false.
type CreateQueueRequest struct {
	AccountID               string
	PlanCode                string
	Datacenters             []string
	Options                 []string
	ConfigKey               string
	ConfigLabel             string
	PricingMode             string
	Duration                string
	Coupon                  string
	MaxMonthlyPrice         float64
	MaxSetupFee             float64
	Currency                string
	Quantity                int
	MaxRetryCount           int
	RetryInterval           int
	AutoPay                 bool
	WaiveRetractationPeriod bool
	Source                  string
	SourceRef               string
}

// UpdateQueueRequest captures partial queue task update fields.
type UpdateQueueRequest struct {
	PlanCode                *string
	Datacenters             []string
	HasDatacenters          bool
	Options                 []string
	HasOptions              bool
	ConfigKey               *string
	ConfigLabel             *string
	PricingMode             *string
	Duration                *string
	Coupon                  *string
	Currency                *string
	MaxMonthlyPrice         *float64
	MaxSetupFee             *float64
	Quantity                *int
	RetryInterval           *int
	AutoPay                 *bool
	WaiveRetractationPeriod *bool
}

// Service manages OVH cart procurement, unpaid/auto-order queue execution, purchase history, and stats.
type Service struct {
	store   *store.Store
	log     *logger.Hub
	ovhPool *ovhclient.Pool
	catalog *catalog.Service
	notify  *notify.Dispatcher

	monitorRunningFn func() bool
	accountClientFn  func(ctx context.Context, accountID string) (*ovh.Client, *store.OVHAccount, error)
	tickInterval     time.Duration

	running  atomic.Bool
	workerMu sync.Mutex
	inFlight map[string]bool

	acctMuLock    sync.Mutex
	acctMus       map[string]*sync.Mutex
	inflightLocks sync.Map // key: accountID|planCode|optionsFingerprint(opts, mode, coupon) -> *sync.Mutex
}

// NewService constructs a procurement Service.
func NewService(
	st *store.Store,
	log *logger.Hub,
	ovhPool *ovhclient.Pool,
	cat *catalog.Service,
	disp *notify.Dispatcher,
) *Service {
	return &Service{
		store:        st,
		log:          log,
		ovhPool:      ovhPool,
		catalog:      cat,
		notify:       disp,
		tickInterval: time.Second,
		inFlight:     make(map[string]bool),
		acctMus:      make(map[string]*sync.Mutex),
	}
}

// SetMonitorRunningFunc sets a callback returning whether the dedicated stock monitor is running (for /api/stats).
func (s *Service) SetMonitorRunningFunc(fn func() bool) {
	s.monitorRunningFn = fn
}

// SetAccountClientFunc overrides OVH client lookup (intended for tests).
func (s *Service) SetAccountClientFunc(fn func(ctx context.Context, accountID string) (*ovh.Client, *store.OVHAccount, error)) {
	s.accountClientFn = fn
}

// SetTickInterval overrides the queue worker polling interval (intended for tests).
func (s *Service) SetTickInterval(d time.Duration) {
	if d > 0 {
		s.tickInterval = d
	}
}

// IsRunning reports whether the background queue processor goroutine is running.
func (s *Service) IsRunning() bool {
	return s.running.Load()
}

func (s *Service) isMonitorRunning() bool {
	if s.monitorRunningFn != nil {
		return s.monitorRunningFn()
	}
	return false
}

func (s *Service) accountLock(accountID string) *sync.Mutex {
	key := strings.TrimSpace(accountID)
	s.acctMuLock.Lock()
	defer s.acctMuLock.Unlock()
	mu, ok := s.acctMus[key]
	if !ok {
		mu = &sync.Mutex{}
		s.acctMus[key] = mu
	}
	return mu
}

// lockInflightConfig serializes concurrent order/queue operations for identical (account, plan, options, pricingMode, coupon)
// configurations while allowing distinct plans or hardware configurations to proceed concurrently.
func (s *Service) lockInflightConfig(accountID, planCode string, opts []string, pricingMode, coupon string) func() {
	key := strings.TrimSpace(accountID) + "|" + strings.ToLower(strings.TrimSpace(planCode)) + "|" + optionsFingerprint(opts, pricingMode, coupon)
	val, _ := s.inflightLocks.LoadOrStore(key, &sync.Mutex{})
	mu := val.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}

// stripAutoAppliedCoupon detects whether rawCoupon matches an active OVH auto-applied catalog campaign
// (context=="standard") for the exact target planCode and pricingMode, and strips it with a WARNING log
// so checkout receives the automatic discount instead of failing with INVALID_COUPON.
// Coupons belonging to other models or other pricing modes are preserved verbatim for OVH validation.
func (s *Service) stripAutoAppliedCoupon(ctx context.Context, accountID, subsidiary, planCode, pricingMode, rawCoupon string) (cleanCoupon string, strippedCode string) {
	trimmed := strings.TrimSpace(rawCoupon)
	if trimmed == "" || s.catalog == nil || strings.TrimSpace(planCode) == "" {
		return trimmed, ""
	}
	ep, zn := s.resolveAccountEndpointZone(ctx, accountID)
	if strings.TrimSpace(subsidiary) != "" {
		zn = strings.TrimSpace(subsidiary)
	}
	camp, isAuto := s.catalog.LookupAutoAppliedPromoForPlan(ctx, ep, zn, planCode, pricingMode, trimmed)
	if !isAuto || camp == nil {
		return trimmed, ""
	}
	slog.Warn("stripped auto-applied OVH catalog campaign code from manual coupon field",
		"planCode", planCode,
		"pricingMode", pricingMode,
		"coupon", trimmed,
		"campaign", camp.PromoCode,
		"context", camp.Context,
		"modes", strings.Join(camp.SupportedModes, ","),
	)
	if s.log != nil {
		s.log.Warn(logger.SourceQueue, "Stripped auto-applied OVH catalog campaign code from manual coupon field (applied automatically by OVH)",
			"planCode", planCode,
			"pricingMode", pricingMode,
			"coupon", trimmed,
			"campaign", camp.PromoCode,
			"context", camp.Context,
			"modes", strings.Join(camp.SupportedModes, ","),
		)
	}
	return "", camp.PromoCode
}

func (s *Service) getAccountClient(ctx context.Context, accountID string) (*ovh.Client, *store.OVHAccount, error) {
	if s.accountClientFn != nil {
		return s.accountClientFn(ctx, accountID)
	}
	if s.ovhPool == nil {
		return nil, nil, ovhclient.ErrCredentialsMissing
	}
	return s.ovhPool.ClientForAccount(ctx, accountID)
}

// Start runs the background queue processor goroutine until ctx is canceled.
func (s *Service) Start(ctx context.Context) {
	if !s.running.CompareAndSwap(false, true) {
		return
	}
	if s.log != nil {
		s.log.Info(logger.SourceQueue, "Procurement queue worker started")
	}

	go func() {
		defer s.running.Store(false)
		ticker := time.NewTicker(s.tickInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				if s.log != nil {
					s.log.Info(logger.SourceQueue, "Procurement queue worker stopped")
				}
				return
			case <-ticker.C:
				s.ProcessDueTasksOnce(ctx)
			}
		}
	}()
}

// ProcessDueTasksOnce evaluates active queue tasks and dispatches up to 3 concurrent purchase attempts.
func (s *Service) ProcessDueTasksOnce(ctx context.Context) {
	if s.store == nil || ctx.Err() != nil {
		return
	}
	items, err := s.store.ListQueue(ctx, "")
	if err != nil {
		if s.log != nil {
			s.log.Error(logger.SourceQueue, "Failed to list queue items", "error", err.Error())
		}
		return
	}

	now := time.Now().Unix()
	var due []store.QueueItem
	for _, it := range items {
		if it.Status != "running" || it.Purchased >= it.Quantity {
			continue
		}
		if it.NextAttemptAt == 0 || now >= it.NextAttemptAt {
			s.workerMu.Lock()
			busy := s.inFlight[it.ID]
			s.workerMu.Unlock()
			if !busy {
				due = append(due, it)
			}
		}
	}

	if len(due) == 0 {
		return
	}

	// Prioritize faster retry tasks (e.g. quick orders) and newer items.
	slices.SortFunc(due, func(a, b store.QueueItem) int {
		if a.RetryInterval != b.RetryInterval {
			return a.RetryInterval - b.RetryInterval
		}
		return strings.Compare(b.CreatedAt, a.CreatedAt)
	})

	s.workerMu.Lock()
	activeCount := len(s.inFlight)
	capacity := max(0, 3-activeCount)
	var batch []store.QueueItem
	for _, it := range due {
		if len(batch) >= capacity {
			break
		}
		s.inFlight[it.ID] = true
		batch = append(batch, it)
	}
	s.workerMu.Unlock()

	var wg sync.WaitGroup
	for _, item := range batch {
		wg.Add(1)
		go func(task store.QueueItem) {
			defer wg.Done()
			defer func() {
				s.workerMu.Lock()
				delete(s.inFlight, task.ID)
				s.workerMu.Unlock()
			}()
			s.executeQueueTask(ctx, task.ID)
		}(item)
	}
	wg.Wait()
}

func (s *Service) executeQueueTask(ctx context.Context, taskID string) {
	for {
		item, err := s.store.GetQueueItem(ctx, taskID)
		if err != nil || item == nil {
			return
		}
		if item.Status != "running" {
			return
		}
		if item.Quantity <= 0 {
			item.Quantity = 1
		}
		if item.Purchased >= item.Quantity {
			item.Status = "completed"
			item.NextAttemptAt = 0
			_, _ = s.store.UpdateQueueItem(ctx, *item)
			return
		}

		item.LastCheckTime = time.Now().Unix()
		item.RetryCount++
		_, _ = s.store.UpdateQueueItem(ctx, *item)

		if s.log != nil {
			s.log.Info(logger.SourceQueue, "Executing procurement queue task",
				"taskId", item.ID,
				"planCode", item.PlanCode,
				"datacenters", strings.Join(item.Datacenters, ","),
				"attempt", item.RetryCount,
				"quantity", item.Quantity,
				"purchased", item.Purchased,
			)
		}

		purchaseErr := func() error {
			unlockFlight := s.lockInflightConfig(item.AccountID, item.PlanCode, item.Options, item.PricingMode, item.Coupon)
			defer unlockFlight()
			_, err := s.purchaseServer(ctx, *item)
			return err
		}()

		// Reload latest item state in case the user paused/deleted/edited while checkout ran.
		latest, getErr := s.store.GetQueueItem(ctx, taskID)
		if getErr != nil || latest == nil {
			return
		}
		if latest.Quantity <= 0 {
			latest.Quantity = 1
		}

		if purchaseErr == nil {
			latest.Purchased++
			latest.FailureCount = 0
			latest.LastError = ""
			if latest.Purchased >= latest.Quantity {
				latest.Status = "completed"
				latest.NextAttemptAt = 0
				_, _ = s.store.UpdateQueueItem(ctx, *latest)
				return
			}
			_, _ = s.store.UpdateQueueItem(ctx, *latest)
			if latest.Status != "running" {
				return
			}
			continue
		}

		latest.FailureCount++
		latest.LastError = purchaseErr.Error()
		maxRetries := latest.MaxRetryCount
		if maxRetries <= 0 {
			maxRetries = 50
		}
		if errors.Is(purchaseErr, ErrPriceAboveLimit) || errors.Is(purchaseErr, ErrPriceChangedSinceQuote) || errors.Is(purchaseErr, ErrInvalidCoupon) || latest.FailureCount >= maxRetries {
			latest.Status = "failed"
			latest.NextAttemptAt = 0
		} else {
			latest.NextAttemptAt = computeNextAttempt(latest.RetryInterval, latest.FailureCount)
		}
		_, _ = s.store.UpdateQueueItem(ctx, *latest)
		return
	}
}

func computeNextAttempt(baseInterval, failures int) int64 {
	if baseInterval < 1 {
		baseInterval = 30
	}
	exp := min(max(failures, 0), 6)
	interval := min(300.0, float64(baseInterval)*math.Pow(2, float64(exp)))
	jitter := 0.8 + 0.4*rand.Float64()
	return time.Now().Unix() + max(1, int64(math.Round(interval*jitter)))
}

// CreateQueueTask validates and inserts a new queue item. AutoPay & WaiveRetractationPeriod strictly default to false.
func (s *Service) CreateQueueTask(ctx context.Context, req CreateQueueRequest) (*store.QueueItem, error) {
	planCode := strings.TrimSpace(req.PlanCode)
	if planCode == "" {
		return nil, errors.New("planCode is required")
	}
	dcs := cleanStringList(req.Datacenters)
	if len(dcs) == 0 {
		return nil, errors.New("at least one datacenter is required")
	}
	qty := req.Quantity
	if qty <= 0 {
		qty = 1
	} else if qty > 100 {
		qty = 100
	}
	retryInt := req.RetryInterval
	if retryInt <= 0 {
		retryInt = 30
	}
	maxRetries := req.MaxRetryCount
	if maxRetries <= 0 {
		maxRetries = 50
	}

	source := strings.TrimSpace(req.Source)
	if source == "" {
		source = "manual"
	}
	normMode, normDur, _ := catalog.NormalizePricingModeAndDuration(req.PricingMode, req.Duration)
	trimmedCoupon, _ := s.stripAutoAppliedCoupon(ctx, req.AccountID, "", planCode, normMode, req.Coupon)

	item := store.QueueItem{
		AccountID:               strings.TrimSpace(req.AccountID),
		PlanCode:                planCode,
		Datacenters:             dcs,
		Options:                 cleanStringList(req.Options),
		ConfigKey:               strings.TrimSpace(req.ConfigKey),
		ConfigLabel:             strings.TrimSpace(req.ConfigLabel),
		PricingMode:             normMode,
		Duration:                normDur,
		Coupon:                  trimmedCoupon,
		MaxMonthlyPrice:         req.MaxMonthlyPrice,
		MaxSetupFee:             req.MaxSetupFee,
		Currency:                strings.TrimSpace(req.Currency),
		Quantity:                qty,
		MaxRetryCount:           maxRetries,
		Status:                  "running",
		RetryInterval:           retryInt,
		AutoPay:                 req.AutoPay,                 // defaults false
		WaiveRetractationPeriod: req.WaiveRetractationPeriod, // defaults false
		Source:                  source,
		SourceRef:               strings.TrimSpace(req.SourceRef),
	}

	created, err := s.store.CreateQueueItem(ctx, item)
	if err != nil {
		return nil, err
	}
	if s.log != nil {
		s.log.Info(logger.SourceQueue, "Created procurement queue task",
			"taskId", created.ID,
			"planCode", created.PlanCode,
			"datacenters", strings.Join(created.Datacenters, ","),
			"pricingMode", created.PricingMode,
			"duration", created.Duration,
			"quantity", created.Quantity,
			"autoPay", created.AutoPay,
			"waiveRetractationPeriod", created.WaiveRetractationPeriod,
		)
	}
	return created, nil
}

// UpdateQueueTask updates an existing queue task, preserving or updating autoPay/waiveRetractationPeriod cleanly.
func (s *Service) UpdateQueueTask(ctx context.Context, id string, req UpdateQueueRequest) (*store.QueueItem, error) {
	existing, err := s.store.GetQueueItem(ctx, id)
	if err != nil {
		return nil, err
	}
	if req.PlanCode != nil && strings.TrimSpace(*req.PlanCode) != "" {
		existing.PlanCode = strings.TrimSpace(*req.PlanCode)
	}
	if req.HasDatacenters {
		existing.Datacenters = cleanStringList(req.Datacenters)
	}
	if req.HasOptions {
		existing.Options = cleanStringList(req.Options)
	}
	if req.ConfigKey != nil {
		existing.ConfigKey = strings.TrimSpace(*req.ConfigKey)
	}
	if req.ConfigLabel != nil {
		existing.ConfigLabel = strings.TrimSpace(*req.ConfigLabel)
	}
	if req.PricingMode != nil || req.Duration != nil {
		pm := existing.PricingMode
		if req.PricingMode != nil {
			pm = *req.PricingMode
		}
		dur := ""
		if req.Duration != nil {
			dur = *req.Duration
		} else if req.PricingMode == nil {
			dur = existing.Duration
		}
		existing.PricingMode, existing.Duration, _ = catalog.NormalizePricingModeAndDuration(pm, dur)
	}
	if req.Coupon != nil {
		cleanCoupon, _ := s.stripAutoAppliedCoupon(ctx, existing.AccountID, "", existing.PlanCode, existing.PricingMode, *req.Coupon)
		existing.Coupon = cleanCoupon
	}
	if req.Currency != nil {
		existing.Currency = strings.TrimSpace(*req.Currency)
	}
	if req.MaxMonthlyPrice != nil && *req.MaxMonthlyPrice >= 0 {
		existing.MaxMonthlyPrice = *req.MaxMonthlyPrice
	}
	if req.MaxSetupFee != nil && *req.MaxSetupFee >= 0 {
		existing.MaxSetupFee = *req.MaxSetupFee
	}
	if req.RetryInterval != nil && *req.RetryInterval > 0 {
		existing.RetryInterval = *req.RetryInterval
	}
	if req.Quantity != nil && *req.Quantity > 0 {
		existing.Quantity = min(*req.Quantity, 100)
	}
	if req.AutoPay != nil {
		existing.AutoPay = *req.AutoPay
	}
	if req.WaiveRetractationPeriod != nil {
		existing.WaiveRetractationPeriod = *req.WaiveRetractationPeriod
	}

	existing.RetryCount = 0
	existing.LastCheckTime = 0
	existing.NextAttemptAt = 0
	if existing.Purchased >= existing.Quantity {
		existing.Status = "completed"
	}

	updated, err := s.store.UpdateQueueItem(ctx, *existing)
	if err != nil {
		return nil, err
	}
	if s.log != nil {
		s.log.Info(logger.SourceQueue, "Updated procurement queue task", "taskId", updated.ID, "planCode", updated.PlanCode)
	}
	return updated, nil
}

// QuoteOrder quotes monthly pricing for a server configuration via OVH Cart without ordering.
func (s *Service) QuoteOrder(ctx context.Context, req QuoteRequest) (*QuoteResult, error) {
	planCode := strings.TrimSpace(req.PlanCode)
	if planCode == "" {
		return nil, errors.New("planCode is required")
	}
	dc := strings.ToLower(strings.TrimSpace(req.Datacenter))
	if dc == "" {
		dc = "gra"
	}
	opts := cleanStringList(req.Options)
	normMode, normDur, _ := catalog.NormalizePricingModeAndDuration(req.PricingMode, req.Duration)
	trimmedCoupon, strippedCode := s.stripAutoAppliedCoupon(ctx, req.AccountID, "", planCode, normMode, req.Coupon)

	if s.catalog == nil {
		return nil, errors.New("catalog service is unavailable")
	}
	priceRes, err := s.catalog.GetServerPriceWithMode(ctx, req.AccountID, planCode, dc, opts, normMode, normDur, trimmedCoupon)
	if err != nil {
		if priceRes != nil && priceRes.Code == "INVALID_COUPON" {
			return nil, fmt.Errorf("%w: %v", ErrInvalidCoupon, err)
		}
		return nil, err
	}
	if priceRes == nil || !priceRes.Success || priceRes.Price == nil {
		msg := "unknown pricing error"
		if priceRes != nil && priceRes.Error != "" {
			msg = priceRes.Error
		}
		return nil, fmt.Errorf("%w: %s", ErrPriceCheckFailed, msg)
	}

	meta := s.catalog.ResolvePlan(ctx, planCode, "", "", "")
	modelName := meta.Name
	if modelName == "" {
		modelName = planCode
	}

	return &QuoteResult{
		PlanCode:             planCode,
		ModelName:            modelName,
		Datacenter:           strings.ToUpper(dc),
		Options:              opts,
		PricingMode:          normMode,
		Duration:             normDur,
		Coupon:               trimmedCoupon,
		CouponStripped:       strippedCode != "",
		AutoAppliedPromoCode: strippedCode,
		Price:                priceRes.Price,
	}, nil
}

// CreateUnpaidOrder immediately executes a single unpaid server checkout.
// SECURITY INVARIANT: autoPay=false, waiveRetractationPeriod=false, and quantity=1 are unconditionally hardcoded.
func (s *Service) CreateUnpaidOrder(ctx context.Context, req UnpaidOrderRequest) (*UnpaidOrderResult, error) {
	planCode := strings.TrimSpace(req.PlanCode)
	if planCode == "" {
		return nil, errors.New("planCode is required")
	}
	dc := strings.ToLower(strings.TrimSpace(req.Datacenter))
	if dc == "" {
		return nil, errors.New("datacenter is required")
	}
	taskID := strings.TrimSpace(req.TaskID)
	if taskID == "" {
		taskID = fmt.Sprintf("mcp-order-%d", time.Now().UnixNano())
	}
	opts := cleanStringList(req.Options)
	normMode, normDur, _ := catalog.NormalizePricingModeAndDuration(req.PricingMode, req.Duration)
	trimmedCoupon, _ := s.stripAutoAppliedCoupon(ctx, req.AccountID, "", planCode, normMode, req.Coupon)

	unlockFlight := s.lockInflightConfig(req.AccountID, planCode, opts, normMode, trimmedCoupon)
	defer unlockFlight()

	// Hardcoded safety invariants: never auto-pay, never waive 14-day EU retractation, quantity strictly 1.
	syntheticItem := store.QueueItem{
		ID:                      taskID,
		AccountID:               strings.TrimSpace(req.AccountID),
		PlanCode:                planCode,
		Datacenters:             []string{dc},
		Options:                 opts,
		PricingMode:             normMode,
		Duration:                normDur,
		Coupon:                  trimmedCoupon,
		MaxMonthlyPrice:         req.MaxMonthlyPrice,
		MaxSetupFee:             req.MaxSetupFee,
		Quantity:                1,
		Purchased:               0,
		RetryCount:              1,
		AutoPay:                 false,
		WaiveRetractationPeriod: false,
	}

	hist, err := s.purchaseServerWithOptions(ctx, syntheticItem, req.EnforceQuoteCap)
	if err != nil {
		return nil, err
	}

	return &UnpaidOrderResult{
		HistoryID:               hist.ID,
		OrderID:                 hist.OrderID,
		OrderURL:                hist.OrderURL,
		PlanCode:                hist.PlanCode,
		Datacenter:              hist.Datacenter,
		Options:                 hist.Options,
		PricingMode:             hist.PricingMode,
		Duration:                hist.Duration,
		Coupon:                  hist.Coupon,
		Price:                   hist.Price,
		AutoPay:                 false,
		WaiveRetractationPeriod: false,
		Quantity:                1,
	}, nil
}

// QuickOrder validates priced configurations and enqueues a high-priority procurement task.
// AutoPay and WaiveRetractationPeriod default to false.
func (s *Service) QuickOrder(ctx context.Context, req QuickOrderRequest) (*QuickOrderResult, int, error) {
	planCode := strings.TrimSpace(req.PlanCode)
	datacenter := strings.ToLower(strings.TrimSpace(req.Datacenter))
	if planCode == "" || datacenter == "" {
		return &QuickOrderResult{
			Success:   false,
			Code:      "MISSING_PARAMS",
			ErrorCode: "MISSING_PARAMS",
			Error:     "planCode and datacenter are required",
			Message:   "planCode and datacenter are required",
		}, http.StatusBadRequest, errors.New("planCode and datacenter are required")
	}

	qty := req.Quantity
	if qty <= 0 {
		qty = 1
	} else if qty > 10 {
		qty = 10
	}

	opts := cleanStringList(req.Options)
	if len(opts) == 0 {
		if strings.EqualFold(strings.TrimSpace(req.Source), "stock-monitor") {
			if s.log != nil {
				s.log.Warn(logger.SourceQueue, "Quick order rejected: stock-monitor auto-order requires explicit configuration options",
					"planCode", planCode,
					"datacenter", datacenter,
				)
			}
			return &QuickOrderResult{
				Success:   false,
				Code:      "CONFIG_REQUIRED_FOR_AUTO_ORDER",
				ErrorCode: "CONFIG_REQUIRED_FOR_AUTO_ORDER",
				Error:     ErrConfigRequiredForAutoOrder.Error(),
				Message:   ErrConfigRequiredForAutoOrder.Error(),
				Quantity:  qty,
			}, http.StatusBadRequest, ErrConfigRequiredForAutoOrder
		}

		if s.catalog != nil {
			ep, zn := s.resolveAccountEndpointZone(ctx, req.AccountID)
			if strings.TrimSpace(req.Subsidiary) != "" {
				zn = strings.TrimSpace(req.Subsidiary)
			}
			cfgs, err := s.catalog.GetConfigLevelAvailabilities(ctx, ep, zn, planCode)
			if err == nil {
				apiDC := catalog.NormalizeAPIDatacenter(datacenter)
				dict, _ := s.catalog.GetDictionary(ctx, ep, zn, false)
				var defMem, defSto string
				if dict != nil {
					if e, ok := dict.LookupPlan(planCode); ok {
						defMem = nonEmptyStr(e.DefaultMemory, e.DefaultSpecs.Memory)
						defSto = nonEmptyStr(e.DefaultStorage, e.DefaultSpecs.Storage)
					}
				}

				for _, cfg := range cfgs {
					if cfg == nil {
						continue
					}
					isDef := cfg.IsDefault
					if !isDef && (defMem != "" || defSto != "") {
						memOK := defMem == "" || strings.EqualFold(cfg.Memory, defMem) || catalog.StandardizeConfig(cfg.Memory) == catalog.StandardizeConfig(defMem)
						stoOK := defSto == "" || strings.EqualFold(cfg.Storage, defSto) || catalog.StandardizeConfig(cfg.Storage) == catalog.StandardizeConfig(defSto)
						isDef = memOK && stoOK
					}
					if !isDef {
						continue
					}
					status := cfg.Datacenters[apiDC]
					if status == "" {
						status = cfg.Datacenters[datacenter]
					}
					if status != "" && status != "unavailable" && status != "unknown" && len(cfg.Options) > 0 {
						opts = cfg.Options
						if strings.TrimSpace(req.ConfigKey) == "" {
							req.ConfigKey = cfg.ConfigKey
						}
						if strings.TrimSpace(req.ConfigLabel) == "" {
							req.ConfigLabel = cfg.Label
						}
						break
					}
				}
			}
		}
		if len(opts) == 0 {
			if s.log != nil {
				s.log.Warn(logger.SourceQueue, "Quick order rejected: no priced default configuration in datacenter", "planCode", planCode, "datacenter", datacenter)
			}
			return &QuickOrderResult{
				Success:   false,
				Code:      "NO_PRICED_CONFIG",
				ErrorCode: "NO_PRICED_CONFIG",
				Error:     "No priced configuration available in the specified datacenter",
				Message:   "No priced configuration available in the specified datacenter",
				Quantity:  qty,
			}, http.StatusBadRequest, ErrNoPricedConfig
		}
	}

	normMode, normDur, intervalMonths := catalog.NormalizePricingModeAndDuration(req.PricingMode, req.Duration)
	trimmedCoupon, strippedCode := s.stripAutoAppliedCoupon(ctx, req.AccountID, req.Subsidiary, planCode, normMode, req.Coupon)

	if !req.SkipDuplicateCheck {
		unlockFlight := s.lockInflightConfig(req.AccountID, planCode, opts, normMode, trimmedCoupon)
		defer unlockFlight()
	}

	var pricePayload *catalog.ServerPricePayload
	var periodExVAT, liveMonthlyExVAT, setupFeeExVAT float64
	currency := nonEmptyStr(req.Currency, "EUR")

	if req.SkipPriceCheck && req.MaxMonthlyPrice <= 0 && req.MaxSetupFee <= 0 && trimmedCoupon == "" {
		zero := 0.0
		pricePayload = &catalog.ServerPricePayload{
			PricingMode:    normMode,
			Duration:       normDur,
			IntervalMonths: intervalMonths,
			Prices: catalog.ServerPriceValues{
				WithTax:      &zero,
				CurrencyCode: currency,
			},
		}
	} else {
		var quoteErr error
		pricePayload, periodExVAT, liveMonthlyExVAT, setupFeeExVAT, currency, quoteErr = s.quoteLiveCart(
			ctx,
			req.AccountID,
			req.Subsidiary,
			planCode,
			datacenter,
			opts,
			normMode,
			normDur,
			intervalMonths,
			trimmedCoupon,
		)
		if quoteErr != nil {
			if errors.Is(quoteErr, ErrInvalidCoupon) {
				return s.abortInvalidCoupon(ctx, req, planCode, datacenter, opts, qty, normMode, normDur, trimmedCoupon, currency, quoteErr)
			}
			if req.SkipPriceCheck && errors.Is(quoteErr, ovhclient.ErrCredentialsMissing) && trimmedCoupon == "" {
				zero := 0.0
				pricePayload = &catalog.ServerPricePayload{
					PricingMode:    normMode,
					Duration:       normDur,
					IntervalMonths: intervalMonths,
					Prices: catalog.ServerPriceValues{
						WithTax:      &zero,
						CurrencyCode: currency,
					},
				}
			} else {
				code := "PRICE_CHECK_FAILED"
				errMsg := quoteErr.Error()
				if errors.Is(quoteErr, ovhclient.ErrCredentialsMissing) {
					code = "OVH_CREDENTIALS_MISSING"
					errMsg = "OVH API credentials are not configured"
				}
				return &QuickOrderResult{
					Success:     false,
					Code:        code,
					ErrorCode:   code,
					Error:       errMsg,
					Message:     errMsg,
					PricingMode: normMode,
					Duration:    normDur,
					Coupon:      trimmedCoupon,
					Quantity:    qty,
				}, http.StatusBadRequest, quoteErr
			}
		} else {
			validPrice := liveMonthlyExVAT > 0 || periodExVAT > 0 || (pricePayload != nil && pricePayload.Prices.WithTax != nil && *pricePayload.Prices.WithTax > 0)
			if !validPrice {
				return &QuickOrderResult{
					Success:     false,
					Code:        "PRICE_CHECK_FAILED",
					ErrorCode:   "PRICE_CHECK_FAILED",
					Error:       "No valid price returned for the selected configuration",
					Message:     "No valid price returned for the selected configuration",
					PricingMode: normMode,
					Duration:    normDur,
					Coupon:      trimmedCoupon,
					Quantity:    qty,
				}, http.StatusBadRequest, ErrPriceCheckFailed
			}
			if (req.MaxMonthlyPrice > 0 && liveMonthlyExVAT > req.MaxMonthlyPrice+0.01) ||
				(req.MaxSetupFee > 0 && setupFeeExVAT > req.MaxSetupFee+0.01) {
				return s.abortPriceAboveLimit(ctx, req, planCode, datacenter, opts, qty, pricePayload, periodExVAT, liveMonthlyExVAT, setupFeeExVAT, normMode, normDur, trimmedCoupon, currency)
			}
		}
	}

	if !req.SkipDuplicateCheck {
		targetFP := optionsFingerprint(opts, normMode, trimmedCoupon)
		activeItems, err := s.store.ListQueue(ctx, req.AccountID)
		if err == nil {
			for _, item := range activeItems {
				if item.PlanCode == planCode &&
					datacentersOverlap(item.Datacenters, datacenter) &&
					(item.Status == "running" || item.Status == "pending" || item.Status == "paused") &&
					optionsFingerprint(item.Options, item.PricingMode, item.Coupon) == targetFP {
					return &QuickOrderResult{
						Success:     false,
						Code:        "DUPLICATE_QUEUE_TASK",
						ErrorCode:   "DUPLICATE_QUEUE_TASK",
						Error:       "An active queue task with identical configuration already exists",
						Message:     "An active queue task with identical configuration already exists",
						PricingMode: normMode,
						Duration:    normDur,
						Coupon:      trimmedCoupon,
						Quantity:    qty,
					}, http.StatusTooManyRequests, ErrDuplicateOrder
				}
			}
		}

		histItems, err := s.store.ListHistory(ctx, req.AccountID)
		if err == nil {
			cutoff := time.Now().Add(-120 * time.Second)
			for _, h := range histItems {
				if h.PlanCode == planCode &&
					datacentersMatch(h.Datacenter, datacenter) &&
					h.Status == "success" &&
					optionsFingerprint(h.Options, h.PricingMode, h.Coupon) == targetFP {
					if ts, parseErr := time.Parse(time.RFC3339Nano, h.Timestamp); parseErr == nil && ts.After(cutoff) {
						return &QuickOrderResult{
							Success:     false,
							Code:        "RECENT_DUPLICATE_ORDER",
							ErrorCode:   "RECENT_DUPLICATE_ORDER",
							Error:       "Identical configuration ordered within the last 120 seconds",
							Message:     "Identical configuration ordered within the last 120 seconds",
							PricingMode: normMode,
							Duration:    normDur,
							Coupon:      trimmedCoupon,
							Quantity:    qty,
						}, http.StatusTooManyRequests, ErrDuplicateOrder
					}
				}
			}
		}
	}

	source := strings.TrimSpace(req.Source)
	if source == "" {
		source = "manual"
	}

	created, err := s.store.CreateQueueItem(ctx, store.QueueItem{
		AccountID:               strings.TrimSpace(req.AccountID),
		PlanCode:                planCode,
		Datacenters:             []string{datacenter},
		Options:                 opts,
		ConfigKey:               strings.TrimSpace(req.ConfigKey),
		ConfigLabel:             strings.TrimSpace(req.ConfigLabel),
		PricingMode:             normMode,
		Duration:                normDur,
		Coupon:                  trimmedCoupon,
		MaxMonthlyPrice:         req.MaxMonthlyPrice,
		MaxSetupFee:             req.MaxSetupFee,
		Currency:                currency,
		Quantity:                qty,
		MaxRetryCount:           3,
		Status:                  "running",
		RetryInterval:           2,
		AutoPay:                 req.AutoPay,                 // defaults false
		WaiveRetractationPeriod: req.WaiveRetractationPeriod, // defaults false
		Source:                  source,
		SourceRef:               strings.TrimSpace(req.SourceRef),
	})
	if err != nil {
		return &QuickOrderResult{
			Success:     false,
			Code:        "QUEUE_CREATE_FAILED",
			ErrorCode:   "QUEUE_CREATE_FAILED",
			Error:       err.Error(),
			Message:     err.Error(),
			PricingMode: normMode,
			Duration:    normDur,
			Coupon:      trimmedCoupon,
			Quantity:    qty,
		}, http.StatusInternalServerError, err
	}

	if s.log != nil {
		s.log.Info(logger.SourceQueue, "Enqueued quick order task",
			"taskId", created.ID,
			"planCode", planCode,
			"datacenter", datacenter,
			"pricingMode", normMode,
			"duration", normDur,
			"quantity", qty,
			"maxMonthlyPrice", req.MaxMonthlyPrice,
			"maxSetupFee", req.MaxSetupFee,
			"periodExVat", periodExVAT,
			"monthlyExVat", liveMonthlyExVAT,
			"setupFeeExVat", setupFeeExVAT,
			"autoPay", created.AutoPay,
			"waiveRetractationPeriod", created.WaiveRetractationPeriod,
		)
	}

	return &QuickOrderResult{
		Success:              true,
		ID:                   created.ID,
		Message:              fmt.Sprintf("%s (%s) added to procurement queue", planCode, strings.ToUpper(datacenter)),
		Price:                pricePayload,
		PeriodPrice:          periodExVAT,
		MonthlyPrice:         liveMonthlyExVAT,
		SetupFee:             setupFeeExVAT,
		PricingMode:          normMode,
		Duration:             normDur,
		Coupon:               trimmedCoupon,
		CouponStripped:       strippedCode != "",
		AutoAppliedPromoCode: strippedCode,
		Quantity:             qty,
		Options:              opts,
		Item:                 created,
	}, http.StatusOK, nil
}

func (s *Service) quoteLiveCart(
	ctx context.Context,
	accountID string,
	subsidiary string,
	planCode string,
	datacenter string,
	options []string,
	normMode string,
	normDur string,
	intervalMonths int,
	coupon string,
) (*catalog.ServerPricePayload, float64, float64, float64, string, error) {
	trimmedCoupon := strings.TrimSpace(coupon)
	if intervalMonths <= 0 {
		normMode, normDur, intervalMonths = catalog.NormalizePricingModeAndDuration(normMode, normDur)
	}

	client, acc, err := s.getAccountClient(ctx, accountID)
	if err != nil {
		if s.catalog != nil {
			res, catErr := s.catalog.GetServerPriceWithMode(ctx, accountID, planCode, datacenter, options, normMode, normDur, trimmedCoupon)
			if catErr != nil {
				if res != nil && res.Code == "INVALID_COUPON" {
					return nil, 0, 0, 0, "EUR", fmt.Errorf("%w: %v", ErrInvalidCoupon, catErr)
				}
				return nil, 0, 0, 0, "EUR", catErr
			}
			if res == nil || !res.Success || res.Price == nil {
				return nil, 0, 0, 0, "EUR", ErrPriceCheckFailed
			}
			periodExVAT, setup, curr := extractCartRecurringAndSetupPrice(nil, nil, res.Price)
			liveMonthlyExVAT := periodExVAT
			if intervalMonths > 1 && periodExVAT > 0 {
				liveMonthlyExVAT = math.Round((periodExVAT/float64(intervalMonths))*100) / 100
			}
			return res.Price, periodExVAT, liveMonthlyExVAT, setup, curr, nil
		}
		return nil, 0, 0, 0, "EUR", err
	}

	ep := ovhclient.EndpointEU
	zone := "IE"
	if acc != nil {
		ep = ovhclient.NormalizeEndpoint(acc.Endpoint)
		zone = ovhclient.NormalizeZone(acc.Zone, ep)
	}
	if strings.TrimSpace(subsidiary) != "" {
		zone = strings.TrimSpace(subsidiary)
	}

	preferredSource := ""
	if s.catalog != nil {
		meta := s.catalog.ResolvePlan(ctx, planCode, "", ep, zone)
		preferredSource = meta.CatalogSource
	}

	apiDC := catalog.NormalizeAPIDatacenter(datacenter)
	var cartResp struct {
		CartID string `json:"cartId"`
	}
	if err := client.PostWithContext(ctx, "/order/cart", map[string]any{
		"ovhSubsidiary": normalizeSub(ep, zone),
	}, &cartResp); err != nil {
		return nil, 0, 0, 0, "EUR", err
	}
	cartID := cartResp.CartID
	cartDeleted := false
	deleteCartOnce := func() {
		if !cartDeleted && cartID != "" {
			cartDeleted = true
			s.deleteCartQuietly(client, cartID)
		}
	}
	defer deleteCartOnce()

	_ = client.PostWithContext(ctx, fmt.Sprintf("/order/cart/%s/assign", url.PathEscape(cartID)), nil, nil)

	catPath, itemID, addErr := catalog.AddCartServerItemWithMode(ctx, client, cartID, planCode, preferredSource, 1, normMode, normDur)
	if addErr != nil {
		return nil, 0, 0, 0, "EUR", addErr
	}

	configs := map[string]string{
		"dedicated_datacenter": apiDC,
		"dedicated_os":         "none_64.en",
	}
	if reg := catalog.InferCartRegion(apiDC); reg != "" {
		configs["region"] = reg
	}
	for k, v := range configs {
		_ = client.PostWithContext(ctx,
			fmt.Sprintf("/order/cart/%s/item/%d/configuration", url.PathEscape(cartID), itemID),
			map[string]any{"label": k, "value": v},
			nil,
		)
	}

	if len(options) > 0 {
		catalog.AddCartOptionsWithMode(ctx, client, cartID, itemID, catPath, planCode, options, normMode, normDur, s.log)
	}

	if trimmedCoupon != "" {
		if couponErr := catalog.ApplyCartCoupon(ctx, client, cartID, trimmedCoupon); couponErr != nil {
			deleteCartOnce()
			if ovhclient.IsTransientError(couponErr) {
				return nil, 0, 0, 0, "EUR", couponErr
			}
			if s.catalog != nil {
				if camp, isAuto := s.catalog.LookupAutoAppliedPromo(ctx, ep, zone, trimmedCoupon); isAuto && camp != nil {
					return nil, 0, 0, 0, "EUR", fmt.Errorf(
						"%w: %s is an automatic OVH catalog promotion (context=%s, modes=%s) applied automatically without a coupon code — leave Coupon empty (%v)",
						ErrInvalidCoupon, camp.PromoCode, camp.Context, strings.Join(camp.SupportedModes, "/"), couponErr,
					)
				}
			}
			return nil, 0, 0, 0, "EUR", fmt.Errorf("%w: %v", ErrInvalidCoupon, couponErr)
		}
	}

	var cartSummary map[string]any
	if err := client.GetWithContext(ctx, fmt.Sprintf("/order/cart/%s/summary", url.PathEscape(cartID)), &cartSummary); err != nil {
		return nil, 0, 0, 0, "EUR", err
	}

	var cartInfo map[string]any
	_ = client.GetWithContext(ctx, fmt.Sprintf("/order/cart/%s", url.PathEscape(cartID)), &cartInfo)

	parsed := catalog.ExtractCartPricePayload(cartSummary, cartInfo)
	periodExVAT, setupFeeExVAT, currency := extractCartRecurringAndSetupPrice(cartSummary, cartInfo, parsed)
	liveMonthlyExVAT := periodExVAT
	if intervalMonths > 1 && periodExVAT > 0 {
		liveMonthlyExVAT = math.Round((periodExVAT/float64(intervalMonths))*100) / 100
	}
	if parsed != nil {
		parsed.PricingMode = normMode
		parsed.Duration = normDur
		parsed.IntervalMonths = intervalMonths
		if parsed.Prices.WithoutTax == nil && periodExVAT > 0 {
			pCopy := periodExVAT
			parsed.Prices.WithoutTax = &pCopy
		}
		if parsed.Prices.WithTax == nil && periodExVAT > 0 {
			tot := periodExVAT + setupFeeExVAT
			parsed.Prices.WithTax = &tot
		}
		if liveMonthlyExVAT > 0 {
			mCopy := liveMonthlyExVAT
			parsed.MonthlyEquivalentWithoutTax = &mCopy
		}
		if parsed.Prices.CurrencyCode == "" && currency != "" {
			parsed.Prices.CurrencyCode = currency
		}
	}
	return parsed, periodExVAT, liveMonthlyExVAT, setupFeeExVAT, currency, nil
}

func (s *Service) abortInvalidCoupon(
	ctx context.Context,
	req QuickOrderRequest,
	planCode string,
	datacenter string,
	opts []string,
	qty int,
	normMode string,
	normDur string,
	coupon string,
	currency string,
	couponErr error,
) (*QuickOrderResult, int, error) {
	slog.Warn("quick order aborted: OVH rejected cart coupon",
		"planCode", planCode,
		"datacenter", datacenter,
		"coupon", coupon,
		"pricingMode", normMode,
		"error", couponErr.Error(),
	)
	if s.log != nil {
		s.log.Warn(logger.SourceQueue, "quick order aborted: OVH rejected cart coupon",
			"planCode", planCode,
			"datacenter", datacenter,
			"coupon", coupon,
			"pricingMode", normMode,
			"error", couponErr.Error(),
		)
	}

	errMsg := fmt.Sprintf("INVALID_COUPON: coupon %q was rejected by OVH for %s (%v)", coupon, planCode, couponErr)
	syntheticItem := store.QueueItem{
		AccountID:       strings.TrimSpace(req.AccountID),
		PlanCode:        planCode,
		Datacenters:     []string{datacenter},
		Options:         opts,
		ConfigKey:       strings.TrimSpace(req.ConfigKey),
		ConfigLabel:     strings.TrimSpace(req.ConfigLabel),
		PricingMode:     normMode,
		Duration:        normDur,
		Coupon:          coupon,
		MaxMonthlyPrice: req.MaxMonthlyPrice,
		Currency:        currency,
		Quantity:        qty,
		AutoPay:         req.AutoPay,
		Source:          nonEmptyStr(req.Source, "manual"),
		SourceRef:       strings.TrimSpace(req.SourceRef),
	}
	if s.store != nil {
		_, _ = s.store.AddHistory(ctx, store.PurchaseHistory{
			AccountID:    syntheticItem.AccountID,
			PlanCode:     planCode,
			Datacenter:   datacenter,
			Options:      opts,
			ConfigKey:    syntheticItem.ConfigKey,
			ConfigLabel:  syntheticItem.ConfigLabel,
			PricingMode:  normMode,
			Duration:     normDur,
			Coupon:       coupon,
			Quantity:     qty,
			Status:       "failed",
			ErrorMessage: errMsg,
			AttemptCount: 1,
		})
	}
	s.emitOrderNotification(ctx, notify.EventOrderFailed, syntheticItem, datacenter, "", "", "", errMsg)

	return &QuickOrderResult{
		Success:     false,
		Code:        "INVALID_COUPON",
		ErrorCode:   "INVALID_COUPON",
		Error:       errMsg,
		Message:     errMsg,
		PricingMode: normMode,
		Duration:    normDur,
		Coupon:      coupon,
		Quantity:    qty,
		Options:     opts,
	}, http.StatusBadRequest, fmt.Errorf("%w: %s", ErrInvalidCoupon, errMsg)
}

func (s *Service) abortPriceAboveLimit(
	ctx context.Context,
	req QuickOrderRequest,
	planCode string,
	datacenter string,
	opts []string,
	qty int,
	pricePayload *catalog.ServerPricePayload,
	periodExVAT float64,
	liveMonthlyExVAT float64,
	setupFeeExVAT float64,
	normMode string,
	normDur string,
	coupon string,
	currency string,
) (*QuickOrderResult, int, error) {
	slog.Warn("quick order aborted: live cart monthly price exceeds configured maxMonthlyPrice",
		"planCode", planCode,
		"pricingMode", normMode,
		"periodExVAT", periodExVAT,
		"liveMonthlyExVAT", liveMonthlyExVAT,
		"maxMonthlyPrice", req.MaxMonthlyPrice,
		"setupFeeExVAT", setupFeeExVAT,
	)
	if s.log != nil {
		s.log.Warn(logger.SourceQueue, "quick order aborted: live cart monthly price exceeds configured maxMonthlyPrice",
			"planCode", planCode,
			"pricingMode", normMode,
			"periodExVAT", periodExVAT,
			"liveMonthlyExVAT", liveMonthlyExVAT,
			"maxMonthlyPrice", req.MaxMonthlyPrice,
			"setupFeeExVAT", setupFeeExVAT,
		)
	}

	labelPart := strings.TrimSpace(req.ConfigLabel)
	if labelPart == "" {
		labelPart = strings.TrimSpace(req.ConfigKey)
	}
	var errMsg string
	if labelPart != "" {
		errMsg = fmt.Sprintf("PRICE_ABOVE_LIMIT: auto-order for %s (%s) was blocked by the price cap (liveMonthlyExVAT %.2f %s > maxMonthlyPrice %.2f %s)",
			planCode, labelPart, liveMonthlyExVAT, currency, req.MaxMonthlyPrice, currency)
	} else {
		errMsg = fmt.Sprintf("PRICE_ABOVE_LIMIT: auto-order for %s was blocked by the price cap (liveMonthlyExVAT %.2f %s > maxMonthlyPrice %.2f %s)",
			planCode, liveMonthlyExVAT, currency, req.MaxMonthlyPrice, currency)
	}

	syntheticItem := store.QueueItem{
		AccountID:       strings.TrimSpace(req.AccountID),
		PlanCode:        planCode,
		Datacenters:     []string{datacenter},
		Options:         opts,
		ConfigKey:       strings.TrimSpace(req.ConfigKey),
		ConfigLabel:     strings.TrimSpace(req.ConfigLabel),
		PricingMode:     normMode,
		Duration:        normDur,
		Coupon:          coupon,
		MaxMonthlyPrice: req.MaxMonthlyPrice,
		Currency:        currency,
		Quantity:        qty,
		AutoPay:         req.AutoPay,
		Source:          nonEmptyStr(req.Source, "manual"),
		SourceRef:       strings.TrimSpace(req.SourceRef),
	}
	if s.store != nil {
		_, _ = s.store.AddHistory(ctx, store.PurchaseHistory{
			AccountID:    syntheticItem.AccountID,
			PlanCode:     planCode,
			Datacenter:   datacenter,
			Options:      opts,
			ConfigKey:    syntheticItem.ConfigKey,
			ConfigLabel:  syntheticItem.ConfigLabel,
			PricingMode:  normMode,
			Duration:     normDur,
			Coupon:       coupon,
			Quantity:     qty,
			Status:       "failed",
			PriceText:    fmt.Sprintf("%.2f %s", liveMonthlyExVAT, currency),
			PeriodPrice:  periodExVAT,
			MonthlyPrice: liveMonthlyExVAT,
			SetupFee:     setupFeeExVAT,
			ErrorMessage: errMsg,
			AttemptCount: 1,
		})
	}
	s.emitOrderNotification(ctx, notify.EventOrderFailed, syntheticItem, datacenter, "", "", fmt.Sprintf("%.2f %s", liveMonthlyExVAT, currency), errMsg)

	return &QuickOrderResult{
		Success:      false,
		Code:         "PRICE_ABOVE_LIMIT",
		ErrorCode:    "PRICE_ABOVE_LIMIT",
		Error:        errMsg,
		Message:      errMsg,
		Price:        pricePayload,
		PeriodPrice:  periodExVAT,
		MonthlyPrice: liveMonthlyExVAT,
		SetupFee:     setupFeeExVAT,
		PricingMode:  normMode,
		Duration:     normDur,
		Coupon:       coupon,
		Quantity:     qty,
		Options:      opts,
	}, http.StatusBadRequest, fmt.Errorf("%w: %s", ErrPriceAboveLimit, errMsg)
}

func (s *Service) purchaseServer(ctx context.Context, item store.QueueItem) (*store.PurchaseHistory, error) {
	return s.purchaseServerWithOptions(ctx, item, false)
}

func (s *Service) purchaseServerWithOptions(ctx context.Context, item store.QueueItem, enforceQuoteCap bool) (*store.PurchaseHistory, error) {
	acctMu := s.accountLock(item.AccountID)
	acctMu.Lock()
	defer acctMu.Unlock()

	normMode, normDur, intervalMonths := catalog.NormalizePricingModeAndDuration(item.PricingMode, item.Duration)
	trimmedCoupon := strings.TrimSpace(item.Coupon)
	item.PricingMode = normMode
	item.Duration = normDur
	item.Coupon = trimmedCoupon

	client, acc, err := s.getAccountClient(ctx, item.AccountID)
	if err != nil {
		return nil, err
	}

	ep := ovhclient.EndpointEU
	zone := "IE"
	if acc != nil {
		ep = ovhclient.NormalizeEndpoint(acc.Endpoint)
		zone = ovhclient.NormalizeZone(acc.Zone, ep)
	}

	targetDCs := cleanStringList(item.Datacenters)
	if len(targetDCs) == 0 {
		return nil, errors.New("no target datacenters configured")
	}

	selectedDisplayDC, selectedAPIDC, err := s.findAvailableDatacenter(ctx, client, ep, item.PlanCode, targetDCs, item.Options)
	if err != nil {
		return nil, err
	}

	var cartID string
	defer func() {
		// Cleanup uncommitted cart if checkout failed or panicked before completion.
		if cartID != "" && ctx.Err() == nil {
			// Best effort cleanup only on failure path; handled explicitly below when checkout succeeds.
		}
	}()

	var cartResp struct {
		CartID string `json:"cartId"`
	}
	if err := client.PostWithContext(ctx, "/order/cart", map[string]any{
		"ovhSubsidiary": normalizeSub(ep, zone),
	}, &cartResp); err != nil {
		return nil, s.recordFailedPurchase(ctx, item, selectedDisplayDC, fmt.Sprintf("create cart: %v", err))
	}
	cartID = cartResp.CartID

	if err := client.PostWithContext(ctx, fmt.Sprintf("/order/cart/%s/assign", url.PathEscape(cartID)), nil, nil); err != nil {
		s.deleteCartQuietly(client, cartID)
		return nil, s.recordFailedPurchase(ctx, item, selectedDisplayDC, fmt.Sprintf("assign cart: %v", err))
	}

	preferredSource := ""
	if s.catalog != nil {
		meta := s.catalog.ResolvePlan(ctx, item.PlanCode, "", ep, zone)
		preferredSource = meta.CatalogSource
	}

	catPath, itemID, addErr := catalog.AddCartServerItemWithMode(ctx, client, cartID, item.PlanCode, preferredSource, 1, normMode, normDur)
	if addErr != nil {
		s.deleteCartQuietly(client, cartID)
		return nil, s.recordFailedPurchase(ctx, item, selectedDisplayDC, fmt.Sprintf("add server to cart: %v", addErr))
	}

	configs := map[string]string{
		"dedicated_datacenter": selectedAPIDC,
		"dedicated_os":         "none_64.en",
	}
	if reg := catalog.InferCartRegion(selectedAPIDC); reg != "" {
		configs["region"] = reg
	}
	for k, v := range configs {
		if cfgErr := client.PostWithContext(ctx,
			fmt.Sprintf("/order/cart/%s/item/%d/configuration", url.PathEscape(cartID), itemID),
			map[string]any{"label": k, "value": v},
			nil,
		); cfgErr != nil {
			s.deleteCartQuietly(client, cartID)
			return nil, s.recordFailedPurchase(ctx, item, selectedDisplayDC, fmt.Sprintf("configure %s: %v", k, cfgErr))
		}
	}

	if len(item.Options) > 0 {
		catalog.AddCartOptionsWithMode(ctx, client, cartID, itemID, catPath, item.PlanCode, item.Options, normMode, normDur, s.log)
	}

	if trimmedCoupon != "" {
		if couponErr := catalog.ApplyCartCoupon(ctx, client, cartID, trimmedCoupon); couponErr != nil {
			s.deleteCartQuietly(client, cartID)
			if ovhclient.IsTransientError(couponErr) {
				if s.log != nil {
					s.log.Warn(logger.SourceQueue, "Transient OVH error applying cart coupon; keeping task for retry",
						"taskId", item.ID,
						"planCode", item.PlanCode,
						"datacenter", selectedDisplayDC,
						"coupon", trimmedCoupon,
						"error", couponErr.Error(),
					)
				}
				return nil, s.recordFailedPurchase(ctx, item, selectedDisplayDC, fmt.Sprintf("apply coupon transient error: %v", couponErr))
			}
			errMsg := fmt.Sprintf("INVALID_COUPON: coupon %q was rejected by OVH for %s (%v)", trimmedCoupon, item.PlanCode, couponErr)
			slog.Warn("order aborted: OVH rejected cart coupon",
				"planCode", item.PlanCode,
				"datacenter", selectedDisplayDC,
				"coupon", trimmedCoupon,
				"pricingMode", normMode,
				"error", couponErr.Error(),
			)
			if s.log != nil {
				s.log.Warn(logger.SourceQueue, "order aborted: OVH rejected cart coupon",
					"taskId", item.ID,
					"planCode", item.PlanCode,
					"datacenter", selectedDisplayDC,
					"coupon", trimmedCoupon,
					"pricingMode", normMode,
					"error", couponErr.Error(),
				)
			}
			if s.store != nil {
				_, _ = s.store.AddHistory(ctx, store.PurchaseHistory{
					TaskID:       item.ID,
					AccountID:    item.AccountID,
					PlanCode:     item.PlanCode,
					Datacenter:   selectedDisplayDC,
					Options:      item.Options,
					ConfigKey:    item.ConfigKey,
					ConfigLabel:  item.ConfigLabel,
					PricingMode:  normMode,
					Duration:     normDur,
					Coupon:       trimmedCoupon,
					Quantity:     max(1, item.Quantity),
					Status:       "failed",
					ErrorMessage: errMsg,
					AttemptCount: max(1, item.RetryCount),
				})
			}
			s.emitOrderNotification(ctx, notify.EventOrderFailed, item, selectedDisplayDC, "", "", "", errMsg)
			return nil, fmt.Errorf("%w: %s", ErrInvalidCoupon, errMsg)
		}
	}

	var priceInfo *store.PriceInfo
	var priceText string
	var periodExVAT, liveMonthlyExVAT, setupFeeExVAT float64
	currency := nonEmptyStr(item.Currency, "EUR")
	var summary map[string]any
	var cartInfo map[string]any
	var summaryErr error
	summaryURL := fmt.Sprintf("/order/cart/%s/summary", url.PathEscape(cartID))
	sumErr := client.GetWithContext(ctx, summaryURL, &summary)
	if sumErr != nil && enforceQuoteCap && ovhclient.IsTransientError(sumErr) {
		timer := time.NewTimer(200 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
		case <-timer.C:
			summary = nil
			sumErr = client.GetWithContext(ctx, summaryURL, &summary)
		}
	}
	if sumErr == nil {
		_ = client.GetWithContext(ctx, fmt.Sprintf("/order/cart/%s", url.PathEscape(cartID)), &cartInfo)
		parsed := catalog.ExtractCartPricePayload(summary, cartInfo)
		periodExVAT, setupFeeExVAT, currency = extractCartRecurringAndSetupPrice(summary, cartInfo, parsed)
		liveMonthlyExVAT = periodExVAT
		if intervalMonths > 1 && periodExVAT > 0 {
			liveMonthlyExVAT = math.Round((periodExVAT/float64(intervalMonths))*100) / 100
		}
		if parsed != nil && (parsed.Prices.WithTax != nil || parsed.Prices.WithoutTax != nil || periodExVAT > 0 || liveMonthlyExVAT > 0) {
			priceInfo = &store.PriceInfo{
				CurrencyCode: nonEmptyStr(parsed.Prices.CurrencyCode, currency),
				PeriodPrice:  periodExVAT,
				SetupFee:     setupFeeExVAT,
				MonthlyExVAT: liveMonthlyExVAT,
				PricingMode:  normMode,
				Duration:     normDur,
			}
			if parsed.Prices.WithTax != nil {
				priceInfo.WithTax = *parsed.Prices.WithTax
			}
			if parsed.Prices.WithoutTax != nil {
				priceInfo.WithoutTax = *parsed.Prices.WithoutTax
			} else if periodExVAT > 0 {
				priceInfo.WithoutTax = periodExVAT
			}
			if parsed.Prices.Tax != nil {
				priceInfo.Tax = *parsed.Prices.Tax
			}
			if parsed.Prices.WithTax != nil {
				priceText = fmt.Sprintf("%.2f %s", *parsed.Prices.WithTax, priceInfo.CurrencyCode)
				priceInfo.Text = priceText
			} else if periodExVAT > 0 {
				priceText = fmt.Sprintf("%.2f %s", periodExVAT, priceInfo.CurrencyCode)
				priceInfo.Text = priceText
			}
		}
	} else {
		summaryErr = sumErr
	}

	if enforceQuoteCap {
		if summaryErr != nil || (liveMonthlyExVAT <= 0 && periodExVAT <= 0) {
			s.deleteCartQuietly(client, cartID)
			errMsg := fmt.Sprintf("PRICE_CHECK_FAILED: unable to verify live cart summary before checkout for %s (%v)", item.PlanCode, summaryErr)
			_ = s.recordFailedPurchase(ctx, item, selectedDisplayDC, errMsg)
			return nil, fmt.Errorf("%w: %s", ErrPriceCheckFailed, errMsg)
		}
		monthlyExceeded := item.MaxMonthlyPrice > 0 && liveMonthlyExVAT > item.MaxMonthlyPrice+0.01
		setupExceeded := setupFeeExVAT > item.MaxSetupFee+0.01
		if monthlyExceeded || setupExceeded {
			s.deleteCartQuietly(client, cartID)
			errMsg := fmt.Sprintf(
				"PRICE_CHANGED_SINCE_QUOTE: live cart price for %s exceeded quote (liveMonthly=%.2f %s vs quoted=%.2f, liveSetup=%.2f vs quoted=%.2f)",
				item.PlanCode, liveMonthlyExVAT, currency, item.MaxMonthlyPrice, setupFeeExVAT, item.MaxSetupFee,
			)
			if s.log != nil {
				s.log.Warn(logger.SourceQueue, "MCP unpaid order aborted: live cart price increased since quote",
					"planCode", item.PlanCode,
					"pricingMode", normMode,
					"liveMonthlyExVAT", liveMonthlyExVAT,
					"quotedMonthlyExVAT", item.MaxMonthlyPrice,
					"liveSetupFeeExVAT", setupFeeExVAT,
					"quotedSetupFeeExVAT", item.MaxSetupFee,
				)
			}
			_ = s.recordFailedPurchase(ctx, item, selectedDisplayDC, errMsg)
			return nil, fmt.Errorf("%w: %s", ErrPriceChangedSinceQuote, errMsg)
		}
	}

	hasPriceCap := item.MaxMonthlyPrice > 0 || item.MaxSetupFee > 0
	if !enforceQuoteCap && hasPriceCap && (summaryErr != nil || (liveMonthlyExVAT <= 0 && periodExVAT <= 0)) {
		s.deleteCartQuietly(client, cartID)
		errMsg := fmt.Sprintf("PRICE_CHECK_FAILED: unable to verify live cart price against configured cap for %s (%v)", item.PlanCode, summaryErr)
		if s.log != nil {
			s.log.Warn(logger.SourceQueue, "Order attempt aborted (will retry): failed to verify live cart summary against price cap",
				"taskId", item.ID,
				"planCode", item.PlanCode,
				"maxMonthlyPrice", item.MaxMonthlyPrice,
				"maxSetupFee", item.MaxSetupFee,
				"error", errMsg,
			)
		}
		_ = s.recordFailedPurchase(ctx, item, selectedDisplayDC, errMsg)
		return nil, fmt.Errorf("%w: %s", ErrPriceCheckFailed, errMsg)
	}

	monthlyCapExceeded := item.MaxMonthlyPrice > 0 && liveMonthlyExVAT > item.MaxMonthlyPrice+0.01
	setupCapExceeded := item.MaxSetupFee > 0 && setupFeeExVAT > item.MaxSetupFee+0.01
	if monthlyCapExceeded || setupCapExceeded {
		s.deleteCartQuietly(client, cartID)
		slog.Warn("order aborted: live cart price exceeds configured cap",
			"planCode", item.PlanCode,
			"pricingMode", normMode,
			"periodExVAT", periodExVAT,
			"liveMonthlyExVAT", liveMonthlyExVAT,
			"maxMonthlyPrice", item.MaxMonthlyPrice,
			"setupFeeExVAT", setupFeeExVAT,
			"maxSetupFee", item.MaxSetupFee,
		)
		if s.log != nil {
			s.log.Warn(logger.SourceQueue, "order aborted: live cart price exceeds configured cap",
				"planCode", item.PlanCode,
				"pricingMode", normMode,
				"periodExVAT", periodExVAT,
				"liveMonthlyExVAT", liveMonthlyExVAT,
				"maxMonthlyPrice", item.MaxMonthlyPrice,
				"setupFeeExVAT", setupFeeExVAT,
				"maxSetupFee", item.MaxSetupFee,
			)
		}
		labelPart := strings.TrimSpace(item.ConfigLabel)
		if labelPart == "" {
			labelPart = strings.TrimSpace(item.ConfigKey)
		}
		var errMsg string
		if setupCapExceeded && !monthlyCapExceeded {
			errMsg = fmt.Sprintf("PRICE_ABOVE_LIMIT: order for %s was blocked by setup fee cap (liveSetupFeeExVAT %.2f %s > maxSetupFee %.2f %s)",
				item.PlanCode, setupFeeExVAT, currency, item.MaxSetupFee, currency)
		} else if labelPart != "" {
			errMsg = fmt.Sprintf("PRICE_ABOVE_LIMIT: auto-order for %s (%s) was blocked by the price cap (liveMonthlyExVAT %.2f %s > maxMonthlyPrice %.2f %s)",
				item.PlanCode, labelPart, liveMonthlyExVAT, currency, item.MaxMonthlyPrice, currency)
		} else {
			errMsg = fmt.Sprintf("PRICE_ABOVE_LIMIT: auto-order for %s was blocked by the price cap (liveMonthlyExVAT %.2f %s > maxMonthlyPrice %.2f %s)",
				item.PlanCode, liveMonthlyExVAT, currency, item.MaxMonthlyPrice, currency)
		}
		_ = s.recordFailedPurchase(ctx, item, selectedDisplayDC, errMsg)
		return nil, fmt.Errorf("%w: %s", ErrPriceAboveLimit, errMsg)
	}

	// Pre-checkout preview (GET /order/cart/{cartId}/checkout); non-fatal if unavailable.
	var checkoutPreview map[string]any
	_ = client.GetWithContext(ctx, fmt.Sprintf("/order/cart/%s/checkout", url.PathEscape(cartID)), &checkoutPreview)

	// SECURITY INVARIANT: autoPay & waiveRetractationPeriod reflect item fields (default false).
	checkoutPayload := map[string]any{
		"autoPayWithPreferredPaymentMethod": item.AutoPay,
		"waiveRetractationPeriod":           item.WaiveRetractationPeriod,
	}
	var checkoutRes struct {
		OrderID any    `json:"orderId"`
		URL     string `json:"url"`
	}
	if coErr := client.PostWithContext(ctx, fmt.Sprintf("/order/cart/%s/checkout", url.PathEscape(cartID)), checkoutPayload, &checkoutRes); coErr != nil {
		s.deleteCartQuietly(client, cartID)
		return nil, s.recordFailedPurchase(ctx, item, selectedDisplayDC, fmt.Sprintf("checkout failed: %v", coErr))
	}

	orderIDStr := strings.TrimSpace(fmt.Sprint(checkoutRes.OrderID))
	if orderIDStr == "<nil>" {
		orderIDStr = ""
	}

	histEntry, saveErr := s.store.AddHistory(ctx, store.PurchaseHistory{
		TaskID:       item.ID,
		AccountID:    item.AccountID,
		PlanCode:     item.PlanCode,
		Datacenter:   selectedDisplayDC,
		Options:      item.Options,
		ConfigKey:    item.ConfigKey,
		ConfigLabel:  item.ConfigLabel,
		PricingMode:  normMode,
		Duration:     normDur,
		Coupon:       trimmedCoupon,
		Quantity:     max(1, item.Quantity),
		Status:       "success",
		OrderID:      orderIDStr,
		OrderURL:     checkoutRes.URL,
		PriceText:    priceText,
		Price:        priceInfo,
		PeriodPrice:  periodExVAT,
		MonthlyPrice: liveMonthlyExVAT,
		SetupFee:     setupFeeExVAT,
		AutoPaid:     item.AutoPay,
		AttemptCount: item.RetryCount,
		Sequence:     item.Purchased + 1,
	})
	if saveErr != nil {
		return nil, saveErr
	}

	if s.log != nil {
		s.log.Info(logger.SourceQueue, "Order checkout succeeded",
			"planCode", item.PlanCode,
			"datacenter", selectedDisplayDC,
			"pricingMode", normMode,
			"duration", normDur,
			"orderId", orderIDStr,
			"quantity", max(1, item.Quantity),
			"sequence", item.Purchased+1,
			"periodExVat", periodExVAT,
			"monthlyExVat", liveMonthlyExVAT,
			"setupFeeExVat", setupFeeExVAT,
			"autoPay", item.AutoPay,
			"waiveRetractationPeriod", item.WaiveRetractationPeriod,
		)
	}

	s.emitOrderNotification(ctx, notify.EventOrderSuccess, item, selectedDisplayDC, orderIDStr, checkoutRes.URL, priceText, "")
	return histEntry, nil
}

func extractCartRecurringAndSetupPrice(
	summary map[string]any,
	info map[string]any,
	parsed *catalog.ServerPricePayload,
) (monthlyExVAT float64, setupFeeExVAT float64, currency string) {
	currency = "EUR"
	if parsed != nil && strings.TrimSpace(parsed.Prices.CurrencyCode) != "" {
		currency = strings.TrimSpace(parsed.Prices.CurrencyCode)
	}

	var explicitMonthly, explicitSetup float64
	if summary != nil {
		if c := stringFromAny(summary["currencyCode"]); c != "" {
			currency = c
		}
		if v, cc := extractNumericPriceAndCurrency(summary["setupFee"]); v > 0 {
			explicitSetup = v
			if cc != "" {
				currency = cc
			}
		}
		if v, cc := extractNumericPriceAndCurrency(summary["installationFee"]); v > 0 && explicitSetup == 0 {
			explicitSetup = v
			if cc != "" {
				currency = cc
			}
		}
		if pm, ok := summary["prices"].(map[string]any); ok {
			if c := stringFromAny(pm["currencyCode"]); c != "" {
				currency = c
			}
			for _, k := range []string{"setup", "installation", "setupFee", "installationFee"} {
				if v, cc := extractNumericPriceAndCurrency(pm[k]); v > 0 {
					explicitSetup = v
					if cc != "" {
						currency = cc
					}
					break
				}
			}
			for _, k := range []string{"monthly", "recurring", "renew", "monthlyExVat", "monthlyWithoutTax"} {
				if v, cc := extractNumericPriceAndCurrency(pm[k]); v > 0 {
					explicitMonthly = v
					if cc != "" {
						currency = cc
					}
					break
				}
			}
		}
	}

	var rawItems []map[string]any
	if summary != nil {
		rawItems = mapSliceFromAny(summary["details"])
		if len(rawItems) == 0 {
			rawItems = mapSliceFromAny(summary["items"])
		}
	}
	if len(rawItems) == 0 && info != nil {
		rawItems = mapSliceFromAny(info["items"])
	}

	var itemsMonthly, itemsSetup float64
	hasItemsBreakdown := false
	for _, it := range rawItems {
		val, cc := extractCartItemExVATPrice(it)
		if cc != "" {
			currency = cc
		}
		if val == 0 {
			continue
		}
		hasItemsBreakdown = true
		if isSetupFeeCartItem(it) {
			itemsSetup += val
		} else {
			itemsMonthly += val
		}
	}
	if itemsSetup < 0 {
		itemsSetup = 0
	}
	if itemsMonthly < 0 {
		itemsMonthly = 0
	}

	if !hasItemsBreakdown && parsed != nil && len(parsed.Items) > 0 {
		for _, it := range parsed.Items {
			var val float64
			if it.Prices.WithoutTax != nil && *it.Prices.WithoutTax != 0 {
				val = *it.Prices.WithoutTax
			}
			if val == 0 {
				continue
			}
			low := strings.ToLower(it.PlanCode + " " + it.Description)
			if strings.Contains(low, "install") || strings.Contains(low, "setup") || strings.Contains(low, "frais d'installation") || strings.Contains(low, "安装费") {
				itemsSetup += val
			} else {
				itemsMonthly += val
			}
		}
		if itemsSetup < 0 {
			itemsSetup = 0
		}
		if itemsMonthly < 0 {
			itemsMonthly = 0
		}
	}

	if itemsSetup > 0 {
		setupFeeExVAT = itemsSetup
	} else if explicitSetup > 0 {
		setupFeeExVAT = explicitSetup
	}

	switch {
	case explicitMonthly > 0:
		monthlyExVAT = explicitMonthly
	case itemsMonthly > 0:
		monthlyExVAT = itemsMonthly
		if parsed != nil && parsed.Prices.WithoutTax != nil && *parsed.Prices.WithoutTax > 0 {
			totalEx := *parsed.Prices.WithoutTax
			if totalEx+0.02 < itemsMonthly+setupFeeExVAT {
				if net := totalEx - setupFeeExVAT; net > 0 {
					monthlyExVAT = net
				}
			}
		}
	case parsed != nil && parsed.Prices.WithoutTax != nil && *parsed.Prices.WithoutTax > 0:
		totalEx := *parsed.Prices.WithoutTax
		if setupFeeExVAT > 0 && totalEx > setupFeeExVAT {
			monthlyExVAT = totalEx - setupFeeExVAT
		} else {
			monthlyExVAT = totalEx
		}
	}

	monthlyExVAT = math.Round(monthlyExVAT*100) / 100
	setupFeeExVAT = math.Round(setupFeeExVAT*100) / 100
	return monthlyExVAT, setupFeeExVAT, currency
}

func isSetupFeeCartItem(it map[string]any) bool {
	if it == nil {
		return false
	}
	detailType := strings.ToUpper(strings.TrimSpace(stringFromAny(it["detailType"])))
	switch detailType {
	case "INSTALLATION", "SETUP", "ONE_OFF", "ONESHOT":
		return true
	case "DURATION", "RENEW", "MONTHLY":
		return false
	}

	pricingMode := strings.ToLower(strings.TrimSpace(stringFromAny(it["pricingMode"])))
	mode := strings.ToLower(strings.TrimSpace(stringFromAny(it["mode"])))
	if pricingMode == "installation" || pricingMode == "setup" || pricingMode == "oneshot" || pricingMode == "one-shot" ||
		mode == "installation" || mode == "setup" || mode == "oneshot" || mode == "one-shot" {
		return true
	}

	duration := strings.ToUpper(strings.TrimSpace(stringFromAny(it["duration"])))
	if duration == "NONE" || duration == "ONCE" || duration == "ONE-OFF" || duration == "ONESHOT" || duration == "P0D" {
		return true
	}

	caps := stringsFromAny(it["capacities"])
	if len(caps) > 0 {
		hasInstall := false
		hasRenew := false
		for _, c := range caps {
			switch strings.ToLower(strings.TrimSpace(c)) {
			case "installation", "setup":
				hasInstall = true
			case "renew":
				hasRenew = true
			}
		}
		if hasInstall && !hasRenew {
			return true
		}
		if hasRenew {
			return false
		}
	}

	combined := strings.ToLower(strings.Join([]string{
		stringFromAny(it["planCode"]),
		stringFromAny(it["description"]),
		stringFromAny(it["label"]),
		stringFromAny(it["productType"]),
	}, " "))
	return strings.Contains(combined, "install") ||
		strings.Contains(combined, "setup") ||
		strings.Contains(combined, "frais d'installation") ||
		strings.Contains(combined, "安装费")
}

func extractCartItemExVATPrice(it map[string]any) (float64, string) {
	if it == nil {
		return 0, ""
	}
	if pm, ok := it["prices"].(map[string]any); ok {
		if v, cc := extractNumericPriceAndCurrency(pm["withoutTax"]); v != 0 {
			return v, cc
		}
		if v, cc := extractNumericPriceAndCurrency(pm["price"]); v != 0 {
			return v, cc
		}
		if v, cc := extractNumericPriceAndCurrency(pm["totalPrice"]); v != 0 {
			return v, cc
		}
	}
	for _, k := range []string{"withoutTax", "totalPrice", "unitPrice", "price"} {
		if v, cc := extractNumericPriceAndCurrency(it[k]); v != 0 {
			return v, cc
		}
	}
	return 0, ""
}

func extractNumericPriceAndCurrency(raw any) (float64, string) {
	if raw == nil {
		return 0, ""
	}
	switch v := raw.(type) {
	case map[string]any:
		cc := stringFromAny(v["currencyCode"])
		if withoutTax, ok := v["withoutTax"]; ok && withoutTax != nil {
			val, subCC := extractNumericPriceAndCurrency(withoutTax)
			if subCC != "" {
				cc = subCC
			}
			return val, cc
		}
		if val, ok := v["value"]; ok && val != nil {
			return floatFromAny(val, 0), cc
		}
		return 0, cc
	case float64:
		return v, ""
	case int:
		return float64(v), ""
	default:
		return floatFromAny(raw, 0), ""
	}
}

func mapSliceFromAny(raw any) []map[string]any {
	switch s := raw.(type) {
	case []map[string]any:
		return s
	case []any:
		out := make([]map[string]any, 0, len(s))
		for _, el := range s {
			if m, ok := el.(map[string]any); ok {
				out = append(out, m)
			}
		}
		return out
	default:
		return nil
	}
}

func (s *Service) findAvailableDatacenter(
	ctx context.Context,
	client *ovh.Client,
	endpoint string,
	planCode string,
	targetDCs []string,
	options []string,
) (string, string, error) {
	var availItems []catalog.RawAvailabilityItem
	var err error
	if s.catalog != nil {
		availItems, err = s.catalog.GetRawAvailabilities(ctx, endpoint, planCode)
	}
	if (err != nil || len(availItems) == 0) && client != nil {
		err = client.GetWithContext(ctx, "/dedicated/server/datacenter/availabilities?planCode="+url.QueryEscape(planCode), &availItems)
	}
	if err != nil {
		return "", "", fmt.Errorf("fetch datacenter availabilities for %s: %w", planCode, err)
	}

	// Check if options specify memory/storage constraints so we match the right configuration if present.
	var wantMem, wantSto string
	for _, o := range options {
		low := strings.ToLower(o)
		if strings.Contains(low, "ram-") {
			wantMem = catalog.StandardizeConfig(low)
		} else if strings.Contains(low, "softraid-") || strings.Contains(low, "hardraid-") ||
			strings.Contains(low, "hybridsoftraid-") || strings.Contains(low, "noraid-") {
			wantSto = catalog.StandardizeConfig(low)
		}
	}

	matchesDC := func(rawDC, targetDisplayDC, targetAPIDC string) (string, bool) {
		rawClean := strings.ToLower(strings.TrimSpace(rawDC))
		if rawClean == "" {
			return "", false
		}
		if strings.EqualFold(rawClean, targetAPIDC) {
			return targetAPIDC, true
		}
		if strings.EqualFold(catalog.NormalizeDisplayDatacenter(rawClean), catalog.NormalizeDisplayDatacenter(targetDisplayDC)) {
			return rawClean, true
		}
		return "", false
	}

	matchedAnyConfigRow := false
	for _, displayDC := range targetDCs {
		apiDC := catalog.NormalizeAPIDatacenter(displayDC)
		for _, av := range availItems {
			if wantMem != "" && av.Memory != "" && catalog.StandardizeConfig(av.Memory) != wantMem {
				continue
			}
			if wantSto != "" && av.Storage != "" && catalog.StandardizeConfig(av.Storage) != wantSto {
				continue
			}
			matchedAnyConfigRow = true
			for _, dc := range av.Datacenters {
				if resolvedAPIDC, ok := matchesDC(dc.Datacenter, displayDC, apiDC); ok &&
					dc.Availability != "" &&
					!strings.EqualFold(dc.Availability, "unavailable") &&
					!strings.EqualFold(dc.Availability, "unknown") {
					return displayDC, resolvedAPIDC, nil
				}
			}
		}
	}

	// If strict memory/storage filtering matched no availability row at all (e.g. generic mock row), retry without hardware filter.
	if !matchedAnyConfigRow && (wantMem != "" || wantSto != "") {
		for _, displayDC := range targetDCs {
			apiDC := catalog.NormalizeAPIDatacenter(displayDC)
			for _, av := range availItems {
				for _, dc := range av.Datacenters {
					if resolvedAPIDC, ok := matchesDC(dc.Datacenter, displayDC, apiDC); ok &&
						dc.Availability != "" &&
						!strings.EqualFold(dc.Availability, "unavailable") &&
						!strings.EqualFold(dc.Availability, "unknown") {
						return displayDC, resolvedAPIDC, nil
					}
				}
			}
		}
	}

	return "", "", ErrOutOfStock
}

func (s *Service) recordFailedPurchase(ctx context.Context, item store.QueueItem, dc, errMsg string) error {
	if dc == "" && len(item.Datacenters) > 0 {
		dc = item.Datacenters[0]
	}
	_, _ = s.store.AddHistory(ctx, store.PurchaseHistory{
		TaskID:       item.ID,
		AccountID:    item.AccountID,
		PlanCode:     item.PlanCode,
		Datacenter:   dc,
		Options:      item.Options,
		ConfigKey:    item.ConfigKey,
		ConfigLabel:  item.ConfigLabel,
		PricingMode:  item.PricingMode,
		Duration:     item.Duration,
		Coupon:       item.Coupon,
		Quantity:     max(1, item.Quantity),
		Status:       "failed",
		ErrorMessage: errMsg,
		AttemptCount: item.RetryCount,
	})
	if s.log != nil {
		s.log.Error(logger.SourceQueue, "Order checkout failed",
			"taskId", item.ID,
			"planCode", item.PlanCode,
			"datacenter", dc,
			"error", errMsg,
		)
	}
	s.emitOrderNotification(ctx, notify.EventOrderFailed, item, dc, "", "", "", errMsg)
	return errors.New(errMsg)
}

func (s *Service) emitOrderNotification(
	ctx context.Context,
	eventType string,
	item store.QueueItem,
	dc string,
	orderID string,
	orderURL string,
	priceText string,
	errMsg string,
) {
	if s.notify == nil {
		return
	}
	modelName := item.PlanCode
	if s.catalog != nil {
		meta := s.catalog.ResolvePlan(ctx, item.PlanCode, "", "", "")
		if meta.Name != "" {
			modelName = meta.Name
		}
	}
	qty := max(1, item.Quantity)
	displayPrice := priceText
	if qty > 1 && displayPrice != "" && !strings.Contains(displayPrice, "x") {
		displayPrice = fmt.Sprintf("%s (x%d)", displayPrice, qty)
	}
	data := map[string]any{
		"preferTemplate": true,
		"planCode":       item.PlanCode,
		"modelName":      modelName,
		"datacenter":     strings.ToUpper(dc),
		"orderId":        orderID,
		"orderUrl":       orderURL,
		"price":          displayPrice,
		"quantity":       qty,
		"configKey":      item.ConfigKey,
		"configLabel":    item.ConfigLabel,
		"autoPaid":       item.AutoPay,
		"error":          errMsg,
	}
	_ = s.notify.EmitEvent(ctx, eventType, "", "", nil, notify.WithTemplateData(data))
}

func (s *Service) deleteCartQuietly(client *ovh.Client, cartID string) {
	if client == nil || cartID == "" {
		return
	}
	cleanCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = client.DeleteWithContext(cleanCtx, fmt.Sprintf("/order/cart/%s", url.PathEscape(cartID)), nil)
}

func (s *Service) resolveAccountEndpointZone(ctx context.Context, accountID string) (string, string) {
	if s.store != nil {
		if acc, err := s.store.GetActiveAccount(ctx, accountID); err == nil && acc != nil {
			ep := ovhclient.NormalizeEndpoint(acc.Endpoint)
			return ep, ovhclient.NormalizeZone(acc.Zone, ep)
		}
	}
	return ovhclient.EndpointEU, "IE"
}

func cleanStringList(in []string) []string {
	out := make([]string, 0, len(in))
	for _, item := range in {
		if trimmed := strings.TrimSpace(item); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func nonEmptyStr(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return strings.TrimSpace(a)
	}
	return strings.TrimSpace(b)
}

func optionsFingerprint(opts []string, modeAndCoupon ...string) string {
	mode := "default"
	if len(modeAndCoupon) > 0 && strings.TrimSpace(modeAndCoupon[0]) != "" {
		mode, _, _ = catalog.NormalizePricingModeAndDuration(modeAndCoupon[0], "")
	}
	coupon := ""
	if len(modeAndCoupon) > 1 {
		coupon = strings.ToLower(strings.TrimSpace(modeAndCoupon[1]))
	}
	norm := make([]string, 0, len(opts))
	for _, o := range opts {
		if s := strings.TrimSpace(o); s != "" && !slices.Contains(norm, s) {
			norm = append(norm, s)
		}
	}
	slices.Sort(norm)
	return mode + "::" + coupon + "::" + strings.Join(norm, "|")
}

func datacentersMatch(a, b string) bool {
	na := strings.ToLower(strings.TrimSpace(a))
	nb := strings.ToLower(strings.TrimSpace(b))
	if na == "" || nb == "" {
		return false
	}
	if na == "any" || nb == "any" {
		return true
	}
	return na == nb
}

func datacentersOverlap(list []string, target string) bool {
	for _, dc := range list {
		if datacentersMatch(dc, target) {
			return true
		}
	}
	return false
}

func normalizeSub(endpoint, zone string) string {
	sub := strings.ToUpper(strings.TrimSpace(zone))
	if sub == "" {
		sub = ovhclient.DefaultZoneForEndpoint(endpoint)
	}
	if endpoint == ovhclient.EndpointUS {
		return "US"
	}
	if endpoint == ovhclient.EndpointCA {
		switch sub {
		case "CA", "QC", "WE", "WS", "SG", "AU", "IN":
		default:
			return "CA"
		}
	}
	return sub
}
