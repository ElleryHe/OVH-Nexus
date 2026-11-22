package mcp

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"maps"
	"sync"
	"time"

	"ovh-nexus/server/internal/logger"
	"ovh-nexus/server/internal/store"
)

const (
	QuoteTTLSeconds      = 300
	MCPAuthMaxAttempts   = 5
	MCPAuthWindowSeconds = 300
)

type orderQuote struct {
	QuoteID             string
	PlanCode            string
	Datacenter          string
	Options             []string
	PricingMode         string
	Duration            string
	Coupon              string
	AccountID           string
	ProfileAlias        string
	Price               map[string]any
	QuotedMonthlyExVAT  float64
	QuotedSetupFeeExVAT float64
	HasQuotedPrice      bool
	AutoPay             bool
	Quantity            int
	CreatedAt           time.Time
	ExpiresAt           time.Time
}

type quoteStore struct {
	mu     sync.Mutex
	quotes map[string]orderQuote
	nowFn  func() time.Time
}

func newQuoteStore(nowFn func() time.Time) *quoteStore {
	return &quoteStore{
		quotes: make(map[string]orderQuote),
		nowFn:  nowFn,
	}
}

func (qs *quoteStore) create(planCode, datacenter string, options []string, accountID, profileAlias string, price map[string]any) orderQuote {
	return qs.createWithMode(planCode, datacenter, options, "default", "P1M", "", accountID, profileAlias, price)
}

func (qs *quoteStore) createWithMode(planCode, datacenter string, options []string, pricingMode, duration, coupon, accountID, profileAlias string, price map[string]any) orderQuote {
	return qs.createWithQuotePrices(planCode, datacenter, options, pricingMode, duration, coupon, accountID, profileAlias, price, 0, 0, false)
}

func (qs *quoteStore) createWithQuotePrices(
	planCode, datacenter string,
	options []string,
	pricingMode, duration, coupon, accountID, profileAlias string,
	price map[string]any,
	quotedMonthlyExVAT, quotedSetupFeeExVAT float64,
	hasQuotedPrice bool,
) orderQuote {
	qs.mu.Lock()
	defer qs.mu.Unlock()

	now := qs.nowFn()
	qs.pruneLocked(now)

	var buf [12]byte
	_, _ = rand.Read(buf[:])
	qid := "q_" + base64.RawURLEncoding.EncodeToString(buf[:])

	priceCopy := make(map[string]any, len(price))
	maps.Copy(priceCopy, price)

	q := orderQuote{
		QuoteID:             qid,
		PlanCode:            planCode,
		Datacenter:          datacenter,
		Options:             append([]string(nil), options...),
		PricingMode:         pricingMode,
		Duration:            duration,
		Coupon:              coupon,
		AccountID:           accountID,
		ProfileAlias:        profileAlias,
		Price:               priceCopy,
		QuotedMonthlyExVAT:  quotedMonthlyExVAT,
		QuotedSetupFeeExVAT: quotedSetupFeeExVAT,
		HasQuotedPrice:      hasQuotedPrice,
		AutoPay:             false,
		Quantity:            1,
		CreatedAt:           now,
		ExpiresAt:           now.Add(QuoteTTLSeconds * time.Second),
	}
	qs.quotes[qid] = q
	return q
}

func (qs *quoteStore) consume(quoteID string) (orderQuote, bool) {
	qs.mu.Lock()
	defer qs.mu.Unlock()

	now := qs.nowFn()
	qs.pruneLocked(now)

	q, ok := qs.quotes[quoteID]
	if !ok {
		return orderQuote{}, false
	}
	delete(qs.quotes, quoteID)
	if now.After(q.ExpiresAt) {
		return orderQuote{}, false
	}
	return q, true
}

// restore returns an unexpired quote back to the store after a pre-checkout transient failure
// (e.g., cart summary 429/503) so the caller can retry safely with the same quote_id.
func (qs *quoteStore) restore(q orderQuote) bool {
	if q.QuoteID == "" {
		return false
	}
	qs.mu.Lock()
	defer qs.mu.Unlock()

	now := qs.nowFn()
	qs.pruneLocked(now)
	if now.After(q.ExpiresAt) {
		return false
	}
	if _, exists := qs.quotes[q.QuoteID]; exists {
		return false
	}
	qs.quotes[q.QuoteID] = q
	return true
}

func (qs *quoteStore) pruneLocked(now time.Time) {
	for id, q := range qs.quotes {
		if now.After(q.ExpiresAt) {
			delete(qs.quotes, id)
		}
	}
}

type orderRateLimiter struct {
	mu         sync.Mutex
	timestamps []time.Time
	nowFn      func() time.Time
}

func newOrderRateLimiter(nowFn func() time.Time) *orderRateLimiter {
	return &orderRateLimiter{nowFn: nowFn}
}

func (orl *orderRateLimiter) check(hourlyLimit int) (bool, int) {
	if hourlyLimit < 1 {
		hourlyLimit = 1
	}
	orl.mu.Lock()
	defer orl.mu.Unlock()

	now := orl.nowFn()
	orl.pruneLocked(now)

	used := len(orl.timestamps)
	if used >= hourlyLimit {
		return false, 0
	}
	return true, hourlyLimit - used
}

func (orl *orderRateLimiter) record() {
	orl.mu.Lock()
	defer orl.mu.Unlock()

	now := orl.nowFn()
	orl.pruneLocked(now)
	orl.timestamps = append(orl.timestamps, now)
}

func (orl *orderRateLimiter) pruneLocked(now time.Time) {
	cutoff := now.Add(-1 * time.Hour)
	idx := 0
	for idx < len(orl.timestamps) && !orl.timestamps[idx].After(cutoff) {
		idx++
	}
	if idx > 0 {
		orl.timestamps = append([]time.Time(nil), orl.timestamps[idx:]...)
	}
}

type authAttemptLimiter struct {
	mu          sync.Mutex
	maxAttempts int
	window      time.Duration
	buckets     map[string][]time.Time
	nowFn       func() time.Time
}

func newAuthAttemptLimiter(maxAttempts int, window time.Duration, nowFn func() time.Time) *authAttemptLimiter {
	return &authAttemptLimiter{
		maxAttempts: maxAttempts,
		window:      window,
		buckets:     make(map[string][]time.Time),
		nowFn:       nowFn,
	}
}

func (al *authAttemptLimiter) check(ip string) (bool, int) {
	al.mu.Lock()
	defer al.mu.Unlock()

	now := al.nowFn()
	al.pruneLocked(now)
	b := al.buckets[ip]
	if len(b) >= al.maxAttempts {
		rem := int(al.window.Seconds() - now.Sub(b[0]).Seconds())
		if rem < 1 {
			rem = 1
		}
		return false, rem
	}
	return true, 0
}

func (al *authAttemptLimiter) recordFailure(ip string) {
	al.mu.Lock()
	defer al.mu.Unlock()

	now := al.nowFn()
	al.pruneLocked(now)
	al.buckets[ip] = append(al.buckets[ip], now)
}

func (al *authAttemptLimiter) reset(ip string) {
	al.mu.Lock()
	defer al.mu.Unlock()
	delete(al.buckets, ip)
}

func (al *authAttemptLimiter) pruneLocked(now time.Time) {
	cutoff := now.Add(-al.window)
	for k, list := range al.buckets {
		idx := 0
		for idx < len(list) && !list[idx].After(cutoff) {
			idx++
		}
		if idx >= len(list) {
			delete(al.buckets, k)
		} else if idx > 0 {
			al.buckets[k] = append([]time.Time(nil), list[idx:]...)
		}
	}
}

func (s *Service) recordAudit(
	ctx context.Context,
	toolName, scope string,
	args map[string]any,
	status, summary, clientIP string,
	notifyEnabled bool,
) {
	if args == nil {
		args = map[string]any{}
	}
	ts := s.nowFn().UTC().Format(time.RFC3339)

	if s.store != nil {
		if _, err := s.store.AppendMCPAudit(ctx, store.MCPAuditLog{
			Timestamp: ts,
			Tool:      toolName,
			Scope:     scope,
			Arguments: args,
			Status:    status,
			Summary:   summary,
			ClientIP:  clientIP,
		}); err != nil && s.log != nil {
			s.log.Error(logger.SourceMCP, "Failed to persist MCP audit record", "tool", toolName, "error", err.Error())
		}
	}

	if s.log != nil {
		switch status {
		case "blocked":
			s.log.Warn(logger.SourceMCP, "Blocked MCP tool invocation", "tool", toolName, "scope", scope, "clientIp", clientIP, "summary", summary)
		case "error":
			s.log.Error(logger.SourceMCP, "MCP tool execution failed", "tool", toolName, "scope", scope, "clientIp", clientIP, "summary", summary)
		default:
			s.log.Info(logger.SourceMCP, "Executed MCP tool", "tool", toolName, "scope", scope, "clientIp", clientIP, "summary", summary)
		}
	}

	if notifyEnabled && s.deps.Notifier != nil && (scope == "procurement" || scope == "operations") && status == "success" {
		argsCopy := make(map[string]any, len(args))
		maps.Copy(argsCopy, args)
		evt := MCPActionEvent{
			Tool:      toolName,
			Scope:     scope,
			Summary:   summary,
			Arguments: argsCopy,
			ClientIP:  clientIP,
			Timestamp: ts,
		}
		notifier := s.deps.Notifier
		go func() {
			defer func() { _ = recover() }()
			nCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			notifier.EmitMCPAction(nCtx, evt)
		}()
	}
}
