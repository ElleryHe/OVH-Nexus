package monitor

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/ovh/go-ovh/ovh"

	"ovh-nexus/server/internal/catalog"
	"ovh-nexus/server/internal/logger"
	"ovh-nexus/server/internal/notify"
	"ovh-nexus/server/internal/ovhclient"
	"ovh-nexus/server/internal/procurement"
	"ovh-nexus/server/internal/store"
)

const (
	defaultStockInterval     = 5 * time.Second
	defaultVPSIntervalSec    = 60
	defaultLifecycleInterval = 30 * time.Minute
	priceCacheTTL            = 3 * 24 * time.Hour
)

type cachedPrice struct {
	text      string
	fetchedAt time.Time
}

// Service orchestrates the Dedicated Stock Monitor, VPS Monitor, Public SKU Watcher, and 30m Lifecycle Watcher.
type Service struct {
	store       *store.Store
	log         *logger.Hub
	ovhPool     *ovhclient.Pool
	catalog     *catalog.Service
	procurement *procurement.Service
	notify      *notify.Dispatcher

	rootMu          sync.RWMutex
	rootCtx         context.Context
	accountClientFn func(ctx context.Context, accountID string) (*ovh.Client, *store.OVHAccount, error)

	// Dedicated Stock Monitor (5s interval)
	stockMu           sync.Mutex
	stockCheckMu      sync.Mutex
	stockRunning      atomic.Bool
	stockCancel       context.CancelFunc
	stockInterval     time.Duration
	knownServersCount atomic.Int64
	stateMu           sync.RWMutex
	validPlanCodes    map[string]bool
	priceCache        map[string]cachedPrice
	inStockSince      map[string]time.Time

	// VPS Monitor (60s+ configurable interval)
	vpsMu          sync.Mutex
	vpsCheckMu     sync.Mutex
	vpsRunning     atomic.Bool
	vpsCancel      context.CancelFunc
	vpsIntervalSec atomic.Int64

	// Public SKU Watcher
	skuMu        sync.Mutex
	skuCheckMu   sync.Mutex
	skuRunning   atomic.Bool
	skuCancel    context.CancelFunc
	skuLastRun   time.Time
	skuLastError string

	// 30m Order Delivery & Service Expiration Watcher
	lifecycleMu      sync.Mutex
	lifecycleCheckMu sync.Mutex
	lifecycleRunning atomic.Bool
	lifecycleCancel  context.CancelFunc
}

// NewService constructs a unified monitor Service and links stock monitor status into procurement stats.
func NewService(
	st *store.Store,
	log *logger.Hub,
	ovhPool *ovhclient.Pool,
	cat *catalog.Service,
	proc *procurement.Service,
	disp *notify.Dispatcher,
) *Service {
	svc := &Service{
		store:          st,
		log:            log,
		ovhPool:        ovhPool,
		catalog:        cat,
		procurement:    proc,
		notify:         disp,
		rootCtx:        context.Background(),
		stockInterval:  defaultStockInterval,
		validPlanCodes: make(map[string]bool),
		priceCache:     make(map[string]cachedPrice),
		inStockSince:   make(map[string]time.Time),
	}
	svc.vpsIntervalSec.Store(defaultVPSIntervalSec)
	if proc != nil {
		proc.SetMonitorRunningFunc(svc.IsStockRunning)
	}
	return svc
}

// SetAccountClientFunc overrides OVH authenticated client lookup (intended for tests).
func (s *Service) SetAccountClientFunc(fn func(ctx context.Context, accountID string) (*ovh.Client, *store.OVHAccount, error)) {
	s.rootMu.Lock()
	defer s.rootMu.Unlock()
	s.accountClientFn = fn
}

// SetStockInterval overrides the dedicated stock monitor polling interval (intended for tests).
func (s *Service) SetStockInterval(d time.Duration) {
	if d > 0 {
		s.stockMu.Lock()
		s.stockInterval = d
		s.stockMu.Unlock()
	}
}

func (s *Service) getRootCtx() context.Context {
	s.rootMu.RLock()
	defer s.rootMu.RUnlock()
	if s.rootCtx != nil {
		return s.rootCtx
	}
	return context.Background()
}

func (s *Service) getAccountClient(ctx context.Context, accountID string) (*ovh.Client, *store.OVHAccount, error) {
	s.rootMu.RLock()
	fn := s.accountClientFn
	s.rootMu.RUnlock()
	if fn != nil {
		return fn(ctx, accountID)
	}
	if s.ovhPool == nil {
		return nil, nil, ovhclient.ErrCredentialsMissing
	}
	return s.ovhPool.ClientForAccount(ctx, accountID)
}

func (s *Service) resolveEndpointAndZone(ctx context.Context, accountID string) (string, string) {
	if s.store != nil {
		if acc, err := s.store.GetActiveAccount(ctx, strings.TrimSpace(accountID)); err == nil && acc != nil {
			ep := ovhclient.NormalizeEndpoint(acc.Endpoint)
			return ep, ovhclient.NormalizeZone(acc.Zone, ep)
		}
	}
	return ovhclient.EndpointEU, "IE"
}

// Start launches background monitor goroutines and restores active stock/VPS subscriptions on boot.
func (s *Service) Start(ctx context.Context) {
	if ctx == nil {
		ctx = context.Background()
	}
	s.rootMu.Lock()
	s.rootCtx = ctx
	s.rootMu.Unlock()

	if s.store != nil {
		if subs, err := s.store.ListStockSubs(ctx, ""); err == nil && len(subs) > 0 {
			s.StartStockMonitor(ctx)
		}
		if vpsSubs, err := s.store.ListVPSSubs(ctx); err == nil && len(vpsSubs) > 0 {
			s.StartVPSMonitor(ctx)
		}
	}

	s.StartSKUWatcher(ctx)
	s.StartLifecycleWatcher(ctx)
}

// MountRoutes registers Dedicated Monitor, VPS Monitor, and SKU Watcher routes on an /api chi.Router.
func (s *Service) MountRoutes(r chi.Router) {
	// Dedicated Stock Monitor
	r.Get("/monitor/status", s.HandleStockStatus)
	r.Post("/monitor/start", s.HandleStockStart)
	r.Post("/monitor/stop", s.HandleStockStop)
	r.Get("/monitor/subscriptions", s.HandleListStockSubs)
	r.Post("/monitor/subscriptions", s.HandleAddStockSub)
	r.Delete("/monitor/subscriptions/clear", s.HandleClearStockSubs)
	r.Post("/monitor/subscriptions/clear", s.HandleClearStockSubs)
	r.Post("/monitor/subscriptions/batch-add-all", s.HandleBatchAddStockSubs)
	r.Delete("/monitor/subscriptions/{id}", s.HandleDeleteStockSub)
	r.Get("/monitor/subscriptions/{planCode}/history", s.HandleGetStockHistory)
	r.Post("/monitor/check-now", s.HandleStockCheckNow)

	// VPS Monitor
	r.Get("/vps-monitor/status", s.HandleVPSStatus)
	r.Post("/vps-monitor/start", s.HandleVPSStart)
	r.Post("/vps-monitor/stop", s.HandleVPSStop)
	r.Put("/vps-monitor/interval", s.HandleSetVPSInterval)
	r.Post("/vps-monitor/interval", s.HandleSetVPSInterval)
	r.Get("/vps-monitor/subscriptions", s.HandleListVPSSubs)
	r.Post("/vps-monitor/subscriptions", s.HandleAddVPSSub)
	r.Delete("/vps-monitor/subscriptions", s.HandleDeleteVPSSubFromBodyOrQuery)
	r.Delete("/vps-monitor/subscriptions/clear", s.HandleClearVPSSubs)
	r.Post("/vps-monitor/subscriptions/clear", s.HandleClearVPSSubs)
	r.Delete("/vps-monitor/subscriptions/{id}", s.HandleDeleteVPSSub)
	r.Get("/vps-monitor/subscriptions/{id}/history", s.HandleGetVPSHistory)
	r.Post("/vps-monitor/check/{planCode}", s.HandleVPSCheckNow)
	r.Post("/vps-monitor/check-now", s.HandleVPSCheckNow)

	// Public SKU Watcher
	r.Get("/sku/status", s.HandleSKUStatus)
	r.Get("/sku/changes", s.HandleSKUChanges)
	r.Post("/sku/check", s.HandleSKUCheck)
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeJSONError(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, map[string]any{
		"success": false,
		"code":    code,
		"error":   msg,
		"message": msg,
	})
}

func cleanStrings(in []string) []string {
	out := make([]string, 0, len(in))
	seen := make(map[string]bool)
	for _, s := range in {
		trimmed := strings.TrimSpace(s)
		if trimmed != "" && !seen[trimmed] {
			seen[trimmed] = true
			out = append(out, trimmed)
		}
	}
	return out
}
