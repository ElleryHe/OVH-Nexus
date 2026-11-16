package procurement

import (
	"context"
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
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
	// ErrDuplicateOrder indicates an active queue task or recent order exists within the 2-minute window.
	ErrDuplicateOrder = errors.New("duplicate order detected within the 2-minute cooldown window")
	// ErrPriceCheckFailed indicates OVH price validation failed or returned zero price.
	ErrPriceCheckFailed = errors.New("price validation failed for the requested configuration")
)

// QuoteRequest holds parameters for quoting a server order (Step 1 of unpaid ordering).
type QuoteRequest struct {
	AccountID  string   `json:"accountId"`
	PlanCode   string   `json:"planCode"`
	Datacenter string   `json:"datacenter"`
	Options    []string `json:"options"`
}

// QuoteResult holds the monthly price breakdown returned by QuoteOrder.
type QuoteResult struct {
	PlanCode   string                      `json:"planCode"`
	ModelName  string                      `json:"modelName"`
	Datacenter string                      `json:"datacenter"`
	Options    []string                    `json:"options"`
	Price      *catalog.ServerPricePayload `json:"price"`
}

// UnpaidOrderRequest holds parameters for placing an unpaid order (auto_pay=false, quantity=1 hardcoded).
type UnpaidOrderRequest struct {
	AccountID  string   `json:"accountId"`
	PlanCode   string   `json:"planCode"`
	Datacenter string   `json:"datacenter"`
	Options    []string `json:"options"`
	TaskID     string   `json:"taskId,omitempty"`
}

// UnpaidOrderResult holds the created unpaid order summary.
type UnpaidOrderResult struct {
	HistoryID               string           `json:"historyId"`
	OrderID                 string           `json:"orderId"`
	OrderURL                string           `json:"orderUrl"`
	PlanCode                string           `json:"planCode"`
	Datacenter              string           `json:"datacenter"`
	Options                 []string         `json:"options"`
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
	SkipPriceCheck          bool     `json:"skipPriceCheck"`
	SkipDuplicateCheck      bool     `json:"skipDuplicateCheck"`
	AutoPay                 bool     `json:"auto_pay"`
	WaiveRetractationPeriod bool     `json:"waive_retractation_period"`
	Source                  string   `json:"source,omitempty"`
	SourceRef               string   `json:"sourceRef,omitempty"`
}

// QuickOrderResult holds the queued quick-order task and validated quote payload.
type QuickOrderResult struct {
	Success  bool                        `json:"success"`
	ID       string                      `json:"id"`
	Message  string                      `json:"message"`
	Price    *catalog.ServerPricePayload `json:"price,omitempty"`
	Options  []string                    `json:"options"`
	Code     string                      `json:"code,omitempty"`
	Error    string                      `json:"error,omitempty"`
	Item     *store.QueueItem            `json:"item,omitempty"`
}

// CreateQueueRequest captures queue task creation fields. AutoPay and WaiveRetractationPeriod default to false.
type CreateQueueRequest struct {
	AccountID               string
	PlanCode                string
	Datacenters             []string
	Options                 []string
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

	acctMuLock sync.Mutex
	acctMus    map[string]*sync.Mutex
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
	item, err := s.store.GetQueueItem(ctx, taskID)
	if err != nil || item == nil {
		return
	}
	if item.Status != "running" {
		return
	}
	if item.Purchased >= item.Quantity {
		item.Status = "completed"
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
		)
	}

	_, purchaseErr := s.purchaseServer(ctx, *item)

	// Reload latest item state in case the user paused/deleted/edited while checkout ran.
	latest, getErr := s.store.GetQueueItem(ctx, taskID)
	if getErr != nil || latest == nil {
		return
	}

	if purchaseErr == nil {
		latest.Purchased++
		latest.FailureCount = 0
		latest.LastError = ""
		if latest.Purchased >= latest.Quantity {
			latest.Status = "completed"
			latest.NextAttemptAt = 0
		} else {
			latest.NextAttemptAt = computeNextAttempt(latest.RetryInterval, 0)
		}
	} else {
		latest.FailureCount++
		latest.LastError = purchaseErr.Error()
		maxRetries := latest.MaxRetryCount
		if maxRetries <= 0 {
			maxRetries = 50
		}
		if latest.FailureCount >= maxRetries {
			latest.Status = "failed"
		} else {
			latest.NextAttemptAt = computeNextAttempt(latest.RetryInterval, latest.FailureCount)
		}
	}

	_, _ = s.store.UpdateQueueItem(ctx, *latest)
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

	item := store.QueueItem{
		AccountID:               strings.TrimSpace(req.AccountID),
		PlanCode:                planCode,
		Datacenters:             dcs,
		Options:                 cleanStringList(req.Options),
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

	if s.catalog == nil {
		return nil, errors.New("catalog service is unavailable")
	}
	priceRes, err := s.catalog.GetServerPrice(ctx, req.AccountID, planCode, dc, opts)
	if err != nil {
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
		PlanCode:   planCode,
		ModelName:  modelName,
		Datacenter: strings.ToUpper(dc),
		Options:    opts,
		Price:      priceRes.Price,
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

	// Hardcoded safety invariants: never auto-pay, never waive 14-day EU retractation, quantity strictly 1.
	syntheticItem := store.QueueItem{
		ID:                      taskID,
		AccountID:               strings.TrimSpace(req.AccountID),
		PlanCode:                planCode,
		Datacenters:             []string{dc},
		Options:                 cleanStringList(req.Options),
		Quantity:                1,
		Purchased:               0,
		RetryCount:              1,
		AutoPay:                 false,
		WaiveRetractationPeriod: false,
	}

	hist, err := s.purchaseServer(ctx, syntheticItem)
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
			Success: false,
			Code:    "MISSING_PARAMS",
			Error:   "planCode and datacenter are required",
		}, 400, errors.New("planCode and datacenter are required")
	}

	opts := cleanStringList(req.Options)
	if len(opts) == 0 && s.catalog != nil {
		ep, zn := s.resolveAccountEndpointZone(ctx, req.AccountID)
		cfgs, err := s.catalog.GetConfigLevelAvailabilities(ctx, ep, zn, planCode)
		if err == nil {
			apiDC := catalog.NormalizeAPIDatacenter(datacenter)
			for _, cfg := range cfgs {
				if cfg == nil {
					continue
				}
				status := cfg.Datacenters[apiDC]
				if status == "" {
					status = cfg.Datacenters[datacenter]
				}
				if status != "" && status != "unavailable" && status != "unknown" && len(cfg.Options) > 0 {
					opts = cfg.Options
					break
				}
			}
		}
		if len(opts) == 0 {
			if s.log != nil && !strings.EqualFold(strings.TrimSpace(req.Source), "stock-monitor") {
				s.log.Warn(logger.SourceQueue, "Quick order rejected: no priced configuration in datacenter", "planCode", planCode, "datacenter", datacenter)
			}
			return &QuickOrderResult{
				Success: false,
				Code:    "NO_PRICED_CONFIG",
				Error:   "No priced configuration available in the specified datacenter",
			}, 400, ErrNoPricedConfig
		}
	}

	var pricePayload *catalog.ServerPricePayload
	if req.SkipPriceCheck {
		zero := 0.0
		pricePayload = &catalog.ServerPricePayload{
			PricingMode: "default",
			Prices: catalog.ServerPriceValues{
				WithTax:      &zero,
				CurrencyCode: "EUR",
			},
		}
	} else {
		if s.catalog == nil {
			return &QuickOrderResult{
				Success: false,
				Code:    "CATALOG_UNAVAILABLE",
				Error:   "Catalog service is unavailable",
			}, 500, errors.New("catalog service is unavailable")
		}
		priceRes, err := s.catalog.GetServerPrice(ctx, req.AccountID, planCode, datacenter, opts)
		if err != nil {
			code := "PRICE_CHECK_FAILED"
			errMsg := err.Error()
			if errors.Is(err, ovhclient.ErrCredentialsMissing) {
				code = "OVH_CREDENTIALS_MISSING"
				errMsg = "OVH API credentials are not configured"
			}
			return &QuickOrderResult{
				Success: false,
				Code:    code,
				Error:   errMsg,
			}, 400, err
		}
		if priceRes == nil || !priceRes.Success || priceRes.Price == nil ||
			priceRes.Price.Prices.WithTax == nil || *priceRes.Price.Prices.WithTax <= 0 {
			return &QuickOrderResult{
				Success: false,
				Code:    "PRICE_CHECK_FAILED",
				Error:   "No valid price returned for the selected configuration",
			}, 400, ErrPriceCheckFailed
		}
		pricePayload = priceRes.Price
	}

	if !req.SkipDuplicateCheck {
		targetFP := optionsFingerprint(opts)
		activeItems, err := s.store.ListQueue(ctx, req.AccountID)
		if err == nil {
			for _, item := range activeItems {
				if item.PlanCode == planCode &&
					slices.Contains(item.Datacenters, datacenter) &&
					(item.Status == "running" || item.Status == "pending" || item.Status == "paused") &&
					optionsFingerprint(item.Options) == targetFP {
					return &QuickOrderResult{
						Success: false,
						Code:    "DUPLICATE_QUEUE_TASK",
						Error:   "An active queue task with identical configuration already exists",
					}, 429, ErrDuplicateOrder
				}
			}
		}

		histItems, err := s.store.ListHistory(ctx, req.AccountID)
		if err == nil {
			cutoff := time.Now().Add(-120 * time.Second)
			for _, h := range histItems {
				if h.PlanCode == planCode &&
					h.Datacenter == datacenter &&
					h.Status == "success" &&
					optionsFingerprint(h.Options) == targetFP {
					if ts, parseErr := time.Parse(time.RFC3339Nano, h.Timestamp); parseErr == nil && ts.After(cutoff) {
						return &QuickOrderResult{
							Success: false,
							Code:    "RECENT_DUPLICATE_ORDER",
							Error:   "Identical configuration ordered within the last 120 seconds",
						}, 429, ErrDuplicateOrder
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
		Quantity:                1,
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
			Success: false,
			Code:    "QUEUE_CREATE_FAILED",
			Error:   err.Error(),
		}, 500, err
	}

	if s.log != nil {
		s.log.Info(logger.SourceQueue, "Enqueued quick order task",
			"taskId", created.ID,
			"planCode", planCode,
			"datacenter", datacenter,
			"autoPay", created.AutoPay,
			"waiveRetractationPeriod", created.WaiveRetractationPeriod,
		)
	}

	return &QuickOrderResult{
		Success: true,
		ID:      created.ID,
		Message: fmt.Sprintf("%s (%s) added to procurement queue", planCode, strings.ToUpper(datacenter)),
		Price:   pricePayload,
		Options: opts,
		Item:    created,
	}, 200, nil
}

func (s *Service) purchaseServer(ctx context.Context, item store.QueueItem) (*store.PurchaseHistory, error) {
	acctMu := s.accountLock(item.AccountID)
	acctMu.Lock()
	defer acctMu.Unlock()

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

	catPath, itemID, addErr := catalog.AddCartServerItem(ctx, client, cartID, item.PlanCode, preferredSource, 1)
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
		catalog.AddCartOptions(ctx, client, cartID, itemID, catPath, item.PlanCode, item.Options, s.log)
	}

	var priceInfo *store.PriceInfo
	var priceText string
	var summary map[string]any
	if sumErr := client.GetWithContext(ctx, fmt.Sprintf("/order/cart/%s/summary", url.PathEscape(cartID)), &summary); sumErr == nil {
		parsed := catalog.ExtractCartPricePayload(summary, nil)
		if parsed != nil && (parsed.Prices.WithTax != nil || parsed.Prices.WithoutTax != nil) {
			priceInfo = &store.PriceInfo{
				CurrencyCode: parsed.Prices.CurrencyCode,
			}
			if parsed.Prices.WithTax != nil {
				priceInfo.WithTax = *parsed.Prices.WithTax
			}
			if parsed.Prices.WithoutTax != nil {
				priceInfo.WithoutTax = *parsed.Prices.WithoutTax
			}
			if parsed.Prices.Tax != nil {
				priceInfo.Tax = *parsed.Prices.Tax
			}
			if parsed.Prices.WithTax != nil {
				priceText = fmt.Sprintf("%.2f %s", *parsed.Prices.WithTax, parsed.Prices.CurrencyCode)
				priceInfo.Text = priceText
			}
		}
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
		Status:       "success",
		OrderID:      orderIDStr,
		OrderURL:     checkoutRes.URL,
		PriceText:    priceText,
		Price:        priceInfo,
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
			"orderId", orderIDStr,
			"autoPay", item.AutoPay,
			"waiveRetractationPeriod", item.WaiveRetractationPeriod,
		)
	}

	s.emitOrderNotification(ctx, notify.EventOrderSuccess, item, selectedDisplayDC, orderIDStr, checkoutRes.URL, priceText, "")
	return histEntry, nil
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

	for _, displayDC := range targetDCs {
		apiDC := catalog.NormalizeAPIDatacenter(displayDC)
		for _, av := range availItems {
			if wantMem != "" && av.Memory != "" && catalog.StandardizeConfig(av.Memory) != wantMem {
				continue
			}
			if wantSto != "" && av.Storage != "" && catalog.StandardizeConfig(av.Storage) != wantSto {
				continue
			}
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

	// If strict memory/storage filtering found no match, retry without hardware filter if options didn't match formatting.
	if wantMem != "" || wantSto != "" {
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
	data := map[string]any{
		"preferTemplate": true,
		"planCode":       item.PlanCode,
		"modelName":      modelName,
		"datacenter":     strings.ToUpper(dc),
		"orderId":        orderID,
		"orderUrl":       orderURL,
		"price":          priceText,
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

func optionsFingerprint(opts []string) string {
	norm := make([]string, 0, len(opts))
	for _, o := range opts {
		if s := strings.TrimSpace(o); s != "" && !slices.Contains(norm, s) {
			norm = append(norm, s)
		}
	}
	slices.Sort(norm)
	return strings.Join(norm, "|")
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
