package monitor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"ovh-nexus/server/internal/catalog"
	"ovh-nexus/server/internal/logger"
	"ovh-nexus/server/internal/notify"
	"ovh-nexus/server/internal/procurement"
	"ovh-nexus/server/internal/store"
)

// StockTarget aliases store.StockTarget for monitor consumers.
type StockTarget = store.StockTarget

// AutoOrderRequest aliases procurement.QuickOrderRequest for monitor auto-order callbacks and tests.
type AutoOrderRequest = procurement.QuickOrderRequest

// AutoOrderFunc executes an automated quick-order triggered by a stock transition.
type AutoOrderFunc func(ctx context.Context, req AutoOrderRequest) (*procurement.QuickOrderResult, int, error)

// ErrConfigRequiredForAutoOrder indicates auto-order was enabled without an explicit configuration selection.
var ErrConfigRequiredForAutoOrder = errors.New("CONFIG_REQUIRED_FOR_AUTO_ORDER: auto-order requires an explicit configuration selection")

// IsStockRunning reports whether the 5s dedicated stock monitor goroutine is active.
func (s *Service) IsStockRunning() bool {
	return s.stockRunning.Load()
}

// StartStockMonitor starts the background 5s dedicated server stock polling loop.
func (s *Service) StartStockMonitor(ctx context.Context) {
	s.stockMu.Lock()
	defer s.stockMu.Unlock()

	if s.stockRunning.Load() {
		return
	}
	parent := ctx
	if parent == nil {
		parent = s.getRootCtx()
	}
	runCtx, cancel := context.WithCancel(parent)
	s.stockCancel = cancel
	s.stockRunning.Store(true)
	interval := s.stockInterval
	if interval <= 0 {
		interval = defaultStockInterval
	}

	if s.log != nil {
		s.log.Info(logger.SourceStockMonitor, "Dedicated stock monitor started", "intervalSeconds", int(interval.Seconds()))
	}

	go func() {
		defer s.stockRunning.Store(false)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-runCtx.Done():
				if s.log != nil {
					s.log.Info(logger.SourceStockMonitor, "Dedicated stock monitor stopped")
				}
				return
			case <-ticker.C:
				_, _ = s.CheckStockNow(runCtx)
			}
		}
	}()
}

// StopStockMonitor stops the dedicated stock monitor loop.
func (s *Service) StopStockMonitor() {
	s.stockMu.Lock()
	defer s.stockMu.Unlock()
	if s.stockCancel != nil {
		s.stockCancel()
		s.stockCancel = nil
	}
	s.stockRunning.Store(false)
}

// CheckStockNow executes one immediate stock check across all subscribed dedicated servers.
func (s *Service) CheckStockNow(ctx context.Context) (int, error) {
	if s.store == nil || s.catalog == nil {
		return 0, errors.New("monitor store or catalog service is unavailable")
	}
	s.stockCheckMu.Lock()
	defer s.stockCheckMu.Unlock()

	subs, err := s.store.ListStockSubs(ctx, "")
	if err != nil {
		return 0, err
	}
	if len(subs) == 0 {
		return 0, nil
	}

	type epZone struct {
		ep   string
		zone string
	}
	epCache := make(map[string][]catalog.RawAvailabilityItem)
	dictCache := make(map[string]*catalog.CatalogDictionary)
	subEP := make(map[string]epZone, len(subs))

	for _, sub := range subs {
		ep, zn := s.resolveEndpointAndZone(ctx, sub.AccountID)
		subEP[sub.AccountID+"|"+sub.PlanCode] = epZone{ep: ep, zone: zn}
		if _, ok := epCache[ep]; !ok {
			items, fetchErr := s.catalog.GetRawAvailabilities(ctx, ep, "")
			if fetchErr != nil {
				if s.log != nil {
					s.log.Warn(logger.SourceStockMonitor, "Failed to fetch dedicated availabilities", "endpoint", ep, "error", fetchErr.Error())
				}
				epCache[ep] = nil
				continue
			}
			epCache[ep] = items

			planSet := make(map[string]bool)
			for _, it := range items {
				if it.PlanCode != "" {
					planSet[it.PlanCode] = true
				}
			}
			s.knownServersCount.Store(int64(len(planSet)))
			s.stateMu.Lock()
			s.validPlanCodes = planSet
			s.stateMu.Unlock()
		}
		dictKey := ep + ":" + zn
		if _, ok := dictCache[dictKey]; !ok {
			if dict, dErr := s.catalog.GetDictionary(ctx, ep, zn, false); dErr == nil {
				dictCache[dictKey] = dict
			}
		}
	}

	locale := s.currentLocale(ctx)
	transitions := 0

	for _, sub := range subs {
		if ctx.Err() != nil {
			return transitions, ctx.Err()
		}
		ez := subEP[sub.AccountID+"|"+sub.PlanCode]
		items := epCache[ez.ep]
		if len(items) == 0 {
			continue
		}

		dict := dictCache[ez.ep+":"+ez.zone]
		meta := catalog.ResolvePlanWithDict(sub.PlanCode, "", dict)
		modelName := sub.ServerName
		if modelName == "" {
			modelName = meta.Name
		}
		if modelName == "" {
			modelName = sub.PlanCode
		}

		if sub.LastStatus == nil {
			sub.LastStatus = make(map[string]string)
		}
		if len(sub.Targets) == 0 {
			store.MigrateLegacyStockSub(&sub)
		}

		dirty := false
		for ti := range sub.Targets {
			target := &sub.Targets[ti]
			if target.LastStatus == nil {
				target.LastStatus = make(map[string]string)
			}
			if target.Quantity <= 0 {
				target.Quantity = 1
			}
			if strings.TrimSpace(target.ID) == "" {
				target.ID = fmt.Sprintf("tgt-%d", ti+1)
			}

			dcStatuses, dcConfigs, dcSpecs := evaluateTargetDatacenters(items, sub.PlanCode, *target, locale)
			if len(dcStatuses) == 0 {
				continue
			}

			dcs := make([]string, 0, len(dcStatuses))
			for dc := range dcStatuses {
				dcs = append(dcs, dc)
			}
			slices.Sort(dcs)

			for _, displayDC := range dcs {
				curStatus := dcStatuses[displayDC]
				oldStatus := target.LastStatus[displayDC]
				key := stockTargetTrackKey(sub.AccountID, sub.PlanCode, target.ID, target.ConfigKey, displayDC)
				now := time.Now()
				nowUTC := now.UTC().Format(time.RFC3339Nano)

				wasAvail := isStockStatusAvailable(oldStatus)
				curAvail := isStockStatusAvailable(curStatus)

				switch {
				case oldStatus == "":
					target.LastStatus[displayDC] = curStatus
					sub.LastStatus[displayDC] = pickBetterStockStatus(sub.LastStatus[displayDC], curStatus)
					if curAvail {
						s.stateMu.Lock()
						if _, exists := s.inStockSince[key]; !exists {
							s.inStockSince[key] = now
						}
						s.stateMu.Unlock()
					}
					dirty = true

				case !wasAvail && curAvail:
					transitions++
					s.stateMu.Lock()
					s.inStockSince[key] = now
					s.stateMu.Unlock()

					target.LastStatus[displayDC] = curStatus
					sub.LastStatus[displayDC] = curStatus
					cfgMap := dcConfigs[displayDC]
					if cfgMap == nil {
						cfgMap = make(map[string]any)
					}
					if strings.TrimSpace(target.ConfigKey) != "" {
						cfgMap["configKey"] = strings.TrimSpace(target.ConfigKey)
					}
					if strings.TrimSpace(target.Label) != "" {
						cfgMap["label"] = strings.TrimSpace(target.Label)
					}
					sub.History = appendStockHistory(sub.History, store.StockHistoryEntry{
						Timestamp:   nowUTC,
						Datacenter:  displayDC,
						Status:      curStatus,
						ChangeType:  "available",
						OldStatus:   oldStatus,
						TargetID:    target.ID,
						ConfigKey:   target.ConfigKey,
						ConfigLabel: target.Label,
						Config:      cfgMap,
					})
					// Persist immediately BEFORE firing alerts or auto-order to prevent duplicate triggers on concurrent ticks.
					_, _ = s.store.UpsertStockSub(ctx, sub)
					dirty = false

					if s.log != nil {
						s.log.Info(logger.SourceStockMonitor, "Dedicated stock transitioned to available",
							"planCode", sub.PlanCode,
							"datacenter", strings.ToUpper(displayDC),
							"configKey", target.ConfigKey,
							"label", target.Label,
							"availability", curStatus,
							"oldStatus", oldStatus,
						)
					}

					if target.NotifyAvailable && s.notify != nil {
						priceText := s.resolveStockPriceText(ctx, sub.AccountID, sub.PlanCode, displayDC, meta)
						specsTxt := buildTargetSpecsText(*target, dcSpecs[displayDC])
						_ = s.notify.EmitEvent(ctx, notify.EventStockAvailable, "", "", nil, notify.WithTemplateData(map[string]any{
							"preferTemplate": true,
							"planCode":       sub.PlanCode,
							"modelName":      modelName,
							"datacenter":     strings.ToUpper(displayDC),
							"specs":          specsTxt,
							"configKey":      target.ConfigKey,
							"configLabel":    target.Label,
							"price":          priceText,
						}))
					}

					autoOrderFn := s.getAutoOrderFunc()
					if target.AutoOrder && autoOrderFn != nil {
						normMode, normDur, _ := catalog.NormalizePricingModeAndDuration(target.PricingMode, target.Duration)
						qoRes, statusCode, qoErr := autoOrderFn(ctx, AutoOrderRequest{
							AccountID:               sub.AccountID,
							PlanCode:                sub.PlanCode,
							Datacenter:              displayDC,
							Options:                 append([]string{}, target.Options...),
							ConfigKey:               target.ConfigKey,
							ConfigLabel:             target.Label,
							PricingMode:             normMode,
							Duration:                normDur,
							Coupon:                  strings.TrimSpace(target.Coupon),
							Quantity:                max(1, target.Quantity),
							MaxMonthlyPrice:         target.MaxMonthlyPrice,
							Currency:                target.Currency,
							Subsidiary:              sub.Subsidiary,
							AutoPay:                 false,
							WaiveRetractationPeriod: false,
							Source:                  "stock-monitor",
							SourceRef:               sub.PlanCode,
						})
						if qoErr != nil && s.log != nil {
							if statusCode == http.StatusBadRequest && (errors.Is(qoErr, procurement.ErrNoPricedConfig) || (qoRes != nil && qoRes.Code == "NO_PRICED_CONFIG")) {
								s.log.Info(logger.SourceStockMonitor, "Stock available for presale SKU; awaiting public catalog pricing before auto-order",
									"planCode", sub.PlanCode,
									"datacenter", displayDC,
								)
							} else {
								s.log.Warn(logger.SourceStockMonitor, "Auto-order quick-order trigger skipped or failed",
									"planCode", sub.PlanCode,
									"datacenter", displayDC,
									"configKey", target.ConfigKey,
									"error", qoErr.Error(),
								)
							}
						}
					}

				case wasAvail && !curAvail:
					transitions++
					var durStr string
					s.stateMu.Lock()
					if started, ok := s.inStockSince[key]; ok {
						durStr = formatDurationShort(now.Sub(started))
						delete(s.inStockSince, key)
					}
					s.stateMu.Unlock()

					target.LastStatus[displayDC] = curStatus
					sub.LastStatus[displayDC] = curStatus
					sub.History = appendStockHistory(sub.History, store.StockHistoryEntry{
						Timestamp:   nowUTC,
						Datacenter:  displayDC,
						Status:      curStatus,
						ChangeType:  "unavailable",
						OldStatus:   oldStatus,
						TargetID:    target.ID,
						ConfigKey:   target.ConfigKey,
						ConfigLabel: target.Label,
					})
					_, _ = s.store.UpsertStockSub(ctx, sub)
					dirty = false

					if s.log != nil {
						s.log.Info(logger.SourceStockMonitor, "Dedicated stock transitioned to unavailable",
							"planCode", sub.PlanCode,
							"datacenter", strings.ToUpper(displayDC),
							"configKey", target.ConfigKey,
							"duration", durStr,
						)
					}

					if target.NotifyUnavailable && s.notify != nil {
						_ = s.notify.EmitEvent(ctx, notify.EventStockUnavailable, "", "", nil, notify.WithTemplateData(map[string]any{
							"preferTemplate": true,
							"planCode":       sub.PlanCode,
							"modelName":      modelName,
							"datacenter":     strings.ToUpper(displayDC),
							"configKey":      target.ConfigKey,
							"configLabel":    target.Label,
							"duration":       durStr,
						}))
					}

				default:
					if oldStatus != curStatus {
						target.LastStatus[displayDC] = curStatus
						sub.LastStatus[displayDC] = pickBetterStockStatus(sub.LastStatus[displayDC], curStatus)
						dirty = true
					}
				}
			}
		}

		// Recompute top-level LastStatus summary across all targets.
		for _, t := range sub.Targets {
			for dc, st := range t.LastStatus {
				sub.LastStatus[dc] = pickBetterStockStatus(sub.LastStatus[dc], st)
			}
		}

		if dirty {
			_, _ = s.store.UpsertStockSub(ctx, sub)
		}
	}

	return transitions, nil
}

func (s *Service) resolveStockPriceText(ctx context.Context, accountID, planCode, dc string, meta catalog.PlanMeta) string {
	cacheKey := strings.ToLower(planCode + ":" + dc)
	s.stateMu.RLock()
	cached, ok := s.priceCache[cacheKey]
	s.stateMu.RUnlock()
	if ok && time.Since(cached.fetchedAt) < priceCacheTTL && cached.text != "" {
		return cached.text
	}

	if s.catalog != nil {
		if res, err := s.catalog.GetServerPrice(ctx, accountID, planCode, dc, nil); err == nil &&
			res != nil && res.Success && res.Price != nil && res.Price.Prices.WithTax != nil {
			txt := fmt.Sprintf("%.2f %s", *res.Price.Prices.WithTax, res.Price.Prices.CurrencyCode)
			s.stateMu.Lock()
			s.priceCache[cacheKey] = cachedPrice{text: txt, fetchedAt: time.Now()}
			s.stateMu.Unlock()
			return txt
		}
	}

	if meta.Price != nil && *meta.Price > 0 {
		curr := meta.Currency
		if curr == "" {
			curr = "EUR"
		}
		txt := fmt.Sprintf("%.2f %s", *meta.Price, curr)
		s.stateMu.Lock()
		s.priceCache[cacheKey] = cachedPrice{text: txt, fetchedAt: time.Now()}
		s.stateMu.Unlock()
		return txt
	}
	return ""
}

func (s *Service) currentLocale(ctx context.Context) string {
	if s.store != nil {
		if st, err := s.store.GetNotificationSettings(ctx); err == nil && st != nil && st.Locale != "" {
			return st.Locale
		}
	}
	return notify.LocaleEN
}

func evaluateTargetDatacenters(
	items []catalog.RawAvailabilityItem,
	planCode string,
	target store.StockTarget,
	locale string,
) (map[string]string, map[string]map[string]any, map[string]string) {
	statuses := make(map[string]string)
	configs := make(map[string]map[string]any)
	specs := make(map[string]string)

	wantAll := len(target.Datacenters) == 0
	targetSet := make(map[string]bool, len(target.Datacenters))
	for _, dc := range target.Datacenters {
		norm := catalog.NormalizeDisplayDatacenter(dc)
		if norm != "" {
			targetSet[norm] = true
			statuses[norm] = "unavailable"
		}
	}

	if wantAll {
		hasPlanRow := false
		for _, item := range items {
			if !strings.EqualFold(item.PlanCode, planCode) {
				continue
			}
			hasPlanRow = true
			for _, dc := range item.Datacenters {
				if displayDC := catalog.NormalizeDisplayDatacenter(dc.Datacenter); displayDC != "" {
					statuses[displayDC] = "unavailable"
				}
			}
		}
		if hasPlanRow {
			for dc := range target.LastStatus {
				if displayDC := catalog.NormalizeDisplayDatacenter(dc); displayDC != "" {
					statuses[displayDC] = "unavailable"
				}
			}
		}
	}

	for _, item := range items {
		if !strings.EqualFold(item.PlanCode, planCode) {
			continue
		}
		if !rowMatchesStockTarget(item, target) {
			continue
		}
		for _, dc := range item.Datacenters {
			displayDC := catalog.NormalizeDisplayDatacenter(dc.Datacenter)
			if displayDC == "" {
				continue
			}
			if !wantAll && !targetSet[displayDC] {
				continue
			}

			avail := strings.TrimSpace(dc.Availability)
			if avail == "" {
				avail = "unavailable"
			}
			existing, seen := statuses[displayDC]
			if !seen {
				statuses[displayDC] = avail
			}
			if isStockStatusAvailable(avail) {
				if !seen || !isStockStatusAvailable(existing) || pickBetterStockStatus(existing, avail) != existing {
					statuses[displayDC] = avail
					cfgKey := catalog.BuildConfigKey(item.Memory, item.Storage)
					if strings.TrimSpace(target.ConfigKey) != "" {
						cfgKey = strings.TrimSpace(target.ConfigKey)
					}
					configs[displayDC] = map[string]any{
						"fqn":           item.FQN,
						"memory":        item.Memory,
						"storage":       item.Storage,
						"systemStorage": item.SystemStorage,
						"gpu":           item.GPU,
						"configKey":     cfgKey,
						"label":         target.Label,
					}
					memTxt := catalog.FormatMemory(catalog.ParseMemoryCode(item.Memory, nil), locale)
					stoTxt := catalog.FormatStorage(catalog.ParseStorageCode(item.Storage, false), locale)
					var parts []string
					if memTxt != "" && memTxt != "-" {
						parts = append(parts, memTxt)
					}
					if stoTxt != "" && stoTxt != "-" {
						parts = append(parts, stoTxt)
					}
					specs[displayDC] = strings.Join(parts, " / ")
				}
			}
		}
	}
	return statuses, configs, specs
}

func rowMatchesStockTarget(item catalog.RawAvailabilityItem, target store.StockTarget) bool {
	cfgKey := strings.TrimSpace(target.ConfigKey)
	if cfgKey == "" {
		return true
	}
	rowKey := catalog.BuildConfigKey(item.Memory, item.Storage)
	if strings.EqualFold(rowKey, cfgKey) || (strings.TrimSpace(item.ConfigKey) != "" && strings.EqualFold(strings.TrimSpace(item.ConfigKey), cfgKey)) {
		return true
	}
	if strings.Contains(cfgKey, "+") {
		parts := strings.SplitN(cfgKey, "+", 2)
		wantMem := strings.TrimSpace(parts[0])
		wantSto := strings.TrimSpace(parts[1])
		if hardwareCodeMatches(item.Memory, wantMem) && hardwareCodeMatches(item.Storage, wantSto) {
			return true
		}
	}
	if len(target.Options) > 0 {
		var optMem, optSto string
		for _, opt := range target.Options {
			low := strings.ToLower(strings.TrimSpace(opt))
			if strings.Contains(low, "ram-") || strings.Contains(low, "memory") {
				optMem = low
			} else if strings.Contains(low, "softraid-") || strings.Contains(low, "hardraid-") ||
				strings.Contains(low, "hybrid") || strings.Contains(low, "noraid-") ||
				strings.Contains(low, "nvme") || strings.Contains(low, "ssd") || strings.Contains(low, "hdd") {
				optSto = low
			}
		}
		if optMem != "" || optSto != "" {
			memOK := optMem == "" || hardwareCodeMatches(item.Memory, optMem)
			stoOK := optSto == "" || hardwareCodeMatches(item.Storage, optSto)
			if memOK && stoOK {
				return true
			}
		}
	}
	return false
}

func hardwareCodeMatches(rawCode, targetCode string) bool {
	r := strings.ToLower(strings.TrimSpace(rawCode))
	t := strings.ToLower(strings.TrimSpace(targetCode))
	if r == "" || t == "" {
		return r == t
	}
	if r == t {
		return true
	}
	if strings.HasPrefix(t, r+"-") || strings.HasPrefix(r, t+"-") {
		return true
	}
	stdR := catalog.StandardizeConfig(r)
	stdT := catalog.StandardizeConfig(t)
	return stdR != "" && stdR == stdT
}

func buildTargetSpecsText(target store.StockTarget, fallbackSpecs string) string {
	lbl := strings.TrimSpace(target.Label)
	key := strings.TrimSpace(target.ConfigKey)
	switch {
	case lbl != "" && key != "" && !strings.EqualFold(lbl, key) && !strings.Contains(lbl, key):
		return fmt.Sprintf("%s (%s)", lbl, key)
	case lbl != "":
		return lbl
	case fallbackSpecs != "" && key != "" && !strings.Contains(fallbackSpecs, key):
		return fmt.Sprintf("%s (%s)", fallbackSpecs, key)
	case fallbackSpecs != "":
		return fallbackSpecs
	default:
		return key
	}
}

func pickBetterStockStatus(a, b string) string {
	rank := func(s string) int {
		switch strings.ToLower(strings.TrimSpace(s)) {
		case "", "unknown":
			return 0
		case "unavailable":
			return 1
		case "480h":
			return 2
		case "72h", "240h":
			return 3
		case "1h-low":
			return 4
		case "1h-high", "available":
			return 5
		default:
			return 3
		}
	}
	if rank(b) > rank(a) {
		return b
	}
	return a
}

func evaluatePlanDatacenters(
	items []catalog.RawAvailabilityItem,
	planCode string,
	targetDCs []string,
	locale string,
) (map[string]string, map[string]map[string]any, map[string]string) {
	return evaluateTargetDatacenters(items, planCode, store.StockTarget{
		Datacenters: targetDCs,
	}, locale)
}

func isStockStatusAvailable(status string) bool {
	s := strings.ToLower(strings.TrimSpace(status))
	return s != "" && s != "unavailable" && s != "unknown"
}

func stockTrackKey(accountID, planCode, dc string) string {
	return accountID + "|" + planCode + "|" + strings.ToLower(dc)
}

func stockTargetTrackKey(accountID, planCode, targetID, configKey, dc string) string {
	return accountID + "|" + planCode + "|" + targetID + "|" + strings.ToLower(strings.TrimSpace(configKey)) + "|" + strings.ToLower(dc)
}

func appendStockHistory(hist []store.StockHistoryEntry, entry store.StockHistoryEntry) []store.StockHistoryEntry {
	out := append(hist, entry)
	if len(out) > 100 {
		out = out[len(out)-100:]
	}
	return out
}

func formatDurationShort(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	secs := int(d.Seconds())
	if secs < 60 {
		return fmt.Sprintf("%ds", secs)
	}
	mins := secs / 60
	remSec := secs % 60
	if mins < 60 {
		if remSec == 0 {
			return fmt.Sprintf("%dm", mins)
		}
		return fmt.Sprintf("%dm %ds", mins, remSec)
	}
	hours := mins / 60
	remMin := mins % 60
	return fmt.Sprintf("%dh %dm", hours, remMin)
}

// AddOrUpdateSubscription validates and persists a single or multi-target stock subscription.
func (s *Service) AddOrUpdateSubscription(ctx context.Context, sub store.StockSubscription) (*store.StockSubscription, error) {
	planCode := strings.TrimSpace(sub.PlanCode)
	if planCode == "" {
		return nil, errors.New("planCode is required")
	}
	sub.PlanCode = planCode
	sub.AccountID = strings.TrimSpace(sub.AccountID)
	sub.Subsidiary = strings.TrimSpace(sub.Subsidiary)

	var existing *store.StockSubscription
	if subs, err := s.store.ListStockSubs(ctx, sub.AccountID); err == nil {
		for _, it := range subs {
			if it.PlanCode == planCode {
				copyIt := it
				existing = &copyIt
				if sub.AccountID == "" && existing.AccountID != "" {
					sub.AccountID = existing.AccountID
				}
				break
			}
		}
	}

	dcs := make([]string, 0, len(sub.Datacenters))
	for _, dc := range sub.Datacenters {
		if norm := catalog.NormalizeDisplayDatacenter(dc); norm != "" {
			dcs = append(dcs, norm)
		}
	}
	sub.Datacenters = cleanStrings(dcs)

	if len(sub.Targets) == 0 {
		if sub.AutoOrder {
			return nil, ErrConfigRequiredForAutoOrder
		}
		qty := sub.AutoOrderQuantity
		if qty <= 0 {
			qty = 1
		}
		lastStatus := map[string]string{}
		if existing != nil && existing.LastStatus != nil {
			for k, v := range existing.LastStatus {
				lastStatus[k] = v
			}
		}
		sub.Targets = []store.StockTarget{
			{
				ID:                "default",
				ConfigKey:         "",
				Options:           []string{},
				Label:             "",
				Datacenters:       append([]string{}, sub.Datacenters...),
				NotifyAvailable:   sub.NotifyAvailable,
				NotifyUnavailable: sub.NotifyUnavailable,
				AutoOrder:         false,
				Quantity:          qty,
				LastStatus:        lastStatus,
			},
		}
	} else {
		for i := range sub.Targets {
			t := &sub.Targets[i]
			t.ConfigKey = strings.TrimSpace(t.ConfigKey)
			t.Label = strings.TrimSpace(t.Label)
			t.Options = cleanStrings(t.Options)
			t.Currency = strings.TrimSpace(t.Currency)
			t.Coupon = strings.TrimSpace(t.Coupon)
			t.PricingMode, t.Duration, _ = catalog.NormalizePricingModeAndDuration(t.PricingMode, t.Duration)

			tDCs := make([]string, 0, len(t.Datacenters))
			for _, dc := range t.Datacenters {
				if norm := catalog.NormalizeDisplayDatacenter(dc); norm != "" {
					tDCs = append(tDCs, norm)
				}
			}
			t.Datacenters = cleanStrings(tDCs)

			if t.AutoOrder {
				if t.ConfigKey == "" || strings.EqualFold(t.ConfigKey, "any") || strings.EqualFold(t.ConfigKey, "all") || len(t.Options) == 0 {
					return nil, ErrConfigRequiredForAutoOrder
				}
			}
			if t.Quantity <= 0 {
				t.Quantity = 1
			}
			if strings.TrimSpace(t.ID) == "" {
				t.ID = fmt.Sprintf("tgt-%d", i+1)
			}
			if len(t.LastStatus) == 0 && existing != nil {
				for _, prevT := range existing.Targets {
					if (prevT.ID == t.ID && prevT.ConfigKey == t.ConfigKey) || (t.ConfigKey != "" && prevT.ConfigKey == t.ConfigKey) {
						if len(prevT.LastStatus) > 0 {
							t.LastStatus = make(map[string]string, len(prevT.LastStatus))
							for k, v := range prevT.LastStatus {
								t.LastStatus[k] = v
							}
							break
						}
					}
				}
			}
			if t.LastStatus == nil {
				t.LastStatus = map[string]string{}
			}
		}
	}

	anyAutoOrder := false
	anyNotifyAvail := false
	anyNotifyUnavail := false
	autoQty := 1
	allSpecifyDCs := len(sub.Targets) > 0
	var mergedDCs []string
	for _, t := range sub.Targets {
		if t.AutoOrder {
			if !anyAutoOrder {
				autoQty = max(1, t.Quantity)
			}
			anyAutoOrder = true
		}
		if t.NotifyAvailable {
			anyNotifyAvail = true
		}
		if t.NotifyUnavailable {
			anyNotifyUnavail = true
		}
		if len(t.Datacenters) == 0 {
			allSpecifyDCs = false
		} else {
			mergedDCs = append(mergedDCs, t.Datacenters...)
		}
	}

	sub.AutoOrder = anyAutoOrder
	sub.NotifyAvailable = anyNotifyAvail
	sub.NotifyUnavailable = anyNotifyUnavail
	sub.NeedsConfigSelection = false
	if anyAutoOrder {
		sub.AutoOrderQuantity = autoQty
	} else if sub.AutoOrderQuantity <= 0 {
		sub.AutoOrderQuantity = 1
	}
	if len(sub.Datacenters) == 0 && allSpecifyDCs {
		sub.Datacenters = cleanStrings(mergedDCs)
	}

	serverName := strings.TrimSpace(sub.ServerName)
	if serverName == "" && s.catalog != nil {
		ep, zn := s.resolveEndpointAndZone(ctx, sub.AccountID)
		serverName = s.catalog.ResolvePlan(ctx, planCode, "", ep, zn).Name
	}
	sub.ServerName = serverName

	if sub.LastStatus == nil {
		sub.LastStatus = map[string]string{}
	}
	if sub.History == nil {
		sub.History = []store.StockHistoryEntry{}
	}
	if existing != nil {
		if len(sub.LastStatus) == 0 && existing.LastStatus != nil {
			sub.LastStatus = existing.LastStatus
		}
		if len(sub.History) == 0 && existing.History != nil {
			sub.History = existing.History
		}
		if sub.CreatedAt == "" {
			sub.CreatedAt = existing.CreatedAt
		}
		if sub.ServerName == "" {
			sub.ServerName = existing.ServerName
		}
	}

	saved, err := s.store.UpsertStockSub(ctx, sub)
	if err != nil {
		return nil, err
	}
	s.StartStockMonitor(s.getRootCtx())
	return saved, nil
}

// ==================== Dedicated Monitor HTTP Handlers ====================

// HandleStockStatus handles GET /api/monitor/status.
func (s *Service) HandleStockStatus(w http.ResponseWriter, r *http.Request) {
	accountID := strings.TrimSpace(r.Header.Get("X-OVH-Account"))
	subs, err := s.store.ListStockSubs(r.Context(), accountID)
	subCount := 0
	if err == nil {
		subCount = len(subs)
	}
	knownCount := int(s.knownServersCount.Load())
	if knownCount == 0 && s.catalog != nil {
		total, _ := s.catalog.GetCachedServerCounts()
		knownCount = total
	}
	s.stockMu.Lock()
	intervalSec := int(s.stockInterval.Seconds())
	s.stockMu.Unlock()
	if intervalSec <= 0 {
		intervalSec = 1
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"running":             s.IsStockRunning(),
		"subscriptions_count": subCount,
		"known_servers_count": knownCount,
		"check_interval":      intervalSec,
	})
}

// HandleStockStart handles POST /api/monitor/start.
func (s *Service) HandleStockStart(w http.ResponseWriter, _ *http.Request) {
	s.StartStockMonitor(s.getRootCtx())
	writeJSON(w, http.StatusOK, map[string]any{
		"status":  "success",
		"running": true,
		"message": "Stock monitor started",
	})
}

// HandleStockStop handles POST /api/monitor/stop.
func (s *Service) HandleStockStop(w http.ResponseWriter, _ *http.Request) {
	s.StopStockMonitor()
	writeJSON(w, http.StatusOK, map[string]any{
		"status":  "success",
		"running": false,
		"message": "Stock monitor stopped",
	})
}

// HandleListStockSubs handles GET /api/monitor/subscriptions.
func (s *Service) HandleListStockSubs(w http.ResponseWriter, r *http.Request) {
	accountID := strings.TrimSpace(r.Header.Get("X-OVH-Account"))
	subs, err := s.store.ListStockSubs(r.Context(), accountID)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "SUBSCRIPTIONS_LIST_FAILED", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, subs)
}

// HandleAddStockSub handles POST /api/monitor/subscriptions and auto-starts the stock monitor.
func (s *Service) HandleAddStockSub(w http.ResponseWriter, r *http.Request) {
	var body struct {
		AccountID         string              `json:"accountId"`
		PlanCode          string              `json:"planCode"`
		ServerName        string              `json:"serverName"`
		Subsidiary        string              `json:"subsidiary,omitempty"`
		Datacenters       []string            `json:"datacenters"`
		NotifyAvailable   *bool               `json:"notifyAvailable"`
		NotifyUnavailable *bool               `json:"notifyUnavailable"`
		AutoOrder         *bool               `json:"autoOrder"`
		AutoOrderQuantity int                 `json:"autoOrderQuantity"`
		Targets           []store.StockTarget `json:"targets"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 256<<10)).Decode(&body); err != nil {
		writeJSONError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid stock subscription JSON payload.")
		return
	}
	planCode := strings.TrimSpace(body.PlanCode)
	if planCode == "" {
		writeJSONError(w, http.StatusBadRequest, "MISSING_PLAN_CODE", "planCode is required.")
		return
	}

	accountID := strings.TrimSpace(r.Header.Get("X-OVH-Account"))
	if accountID == "" {
		accountID = strings.TrimSpace(body.AccountID)
	}

	notifyAvail := true
	if body.NotifyAvailable != nil {
		notifyAvail = *body.NotifyAvailable
	}
	notifyUnavail := false
	if body.NotifyUnavailable != nil {
		notifyUnavail = *body.NotifyUnavailable
	}
	autoOrder := false
	if body.AutoOrder != nil {
		autoOrder = *body.AutoOrder
	}
	qty := body.AutoOrderQuantity
	if qty <= 0 {
		qty = 1
	}

	saved, err := s.AddOrUpdateSubscription(r.Context(), store.StockSubscription{
		AccountID:         accountID,
		PlanCode:          planCode,
		ServerName:        strings.TrimSpace(body.ServerName),
		Subsidiary:        strings.TrimSpace(body.Subsidiary),
		Datacenters:       body.Datacenters,
		NotifyAvailable:   notifyAvail,
		NotifyUnavailable: notifyUnavail,
		AutoOrder:         autoOrder,
		AutoOrderQuantity: qty,
		Targets:           body.Targets,
	})
	if err != nil {
		if errors.Is(err, ErrConfigRequiredForAutoOrder) {
			writeJSONError(w, http.StatusBadRequest, "CONFIG_REQUIRED_FOR_AUTO_ORDER", ErrConfigRequiredForAutoOrder.Error())
			return
		}
		writeJSONError(w, http.StatusInternalServerError, "SUBSCRIPTION_SAVE_FAILED", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status":       "success",
		"subscription": saved,
	})
}

// HandleDeleteStockSub handles DELETE /api/monitor/subscriptions/{id}.
func (s *Service) HandleDeleteStockSub(w http.ResponseWriter, r *http.Request) {
	planCode := strings.TrimSpace(chi.URLParam(r, "id"))
	accountID := strings.TrimSpace(r.Header.Get("X-OVH-Account"))

	err := s.store.DeleteStockSub(r.Context(), accountID, planCode)
	if errors.Is(err, store.ErrNotFound) && accountID == "" {
		if subs, listErr := s.store.ListStockSubs(r.Context(), ""); listErr == nil {
			for _, it := range subs {
				if it.PlanCode == planCode {
					err = s.store.DeleteStockSub(r.Context(), it.AccountID, planCode)
					break
				}
			}
		}
	}
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		writeJSONError(w, http.StatusInternalServerError, "SUBSCRIPTION_DELETE_FAILED", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "success"})
}

// HandleBatchAddStockSubs handles POST /api/monitor/subscriptions/batch-add-all.
func (s *Service) HandleBatchAddStockSubs(w http.ResponseWriter, r *http.Request) {
	if s.catalog == nil {
		writeJSONError(w, http.StatusInternalServerError, "CATALOG_UNAVAILABLE", "Catalog service is unavailable.")
		return
	}
	var body struct {
		Datacenters       []string `json:"datacenters"`
		NotifyAvailable   *bool    `json:"notifyAvailable"`
		NotifyUnavailable *bool    `json:"notifyUnavailable"`
	}
	if r.Body != nil {
		_ = json.NewDecoder(io.LimitReader(r.Body, 64<<10)).Decode(&body)
	}

	accountID := strings.TrimSpace(r.Header.Get("X-OVH-Account"))
	listResp, err := s.catalog.ListServers(r.Context(), catalog.ListServersOptions{AccountID: accountID})
	if err != nil {
		writeJSONError(w, http.StatusBadGateway, "CATALOG_FETCH_FAILED", err.Error())
		return
	}

	existingMap := make(map[string]store.StockSubscription)
	if curSubs, err := s.store.ListStockSubs(r.Context(), accountID); err == nil {
		for _, it := range curSubs {
			existingMap[it.PlanCode] = it
		}
	}

	notifyAvail := true
	if body.NotifyAvailable != nil {
		notifyAvail = *body.NotifyAvailable
	}
	notifyUnavail := false
	if body.NotifyUnavailable != nil {
		notifyUnavail = *body.NotifyUnavailable
	}
	var dcs []string
	for _, dc := range body.Datacenters {
		if norm := catalog.NormalizeDisplayDatacenter(dc); norm != "" {
			dcs = append(dcs, norm)
		}
	}
	dcs = cleanStrings(dcs)

	batch := make([]store.StockSubscription, 0, len(listResp.Servers))
	for _, srv := range listResp.Servers {
		sub := store.StockSubscription{
			AccountID:         accountID,
			PlanCode:          srv.PlanCode,
			ServerName:        srv.Name,
			Datacenters:       dcs,
			NotifyAvailable:   notifyAvail,
			NotifyUnavailable: notifyUnavail,
			AutoOrder:         false,
			AutoOrderQuantity: 1,
			Targets: []store.StockTarget{
				{
					ID:                "default",
					ConfigKey:         "",
					Options:           []string{},
					Label:             "",
					Datacenters:       append([]string{}, dcs...),
					NotifyAvailable:   notifyAvail,
					NotifyUnavailable: notifyUnavail,
					AutoOrder:         false,
					Quantity:          1,
					LastStatus:        map[string]string{},
				},
			},
			LastStatus: map[string]string{},
			History:    []store.StockHistoryEntry{},
		}
		if prev, ok := existingMap[srv.PlanCode]; ok {
			sub.Datacenters = prev.Datacenters
			sub.AutoOrder = prev.AutoOrder
			sub.AutoOrderQuantity = prev.AutoOrderQuantity
			sub.Targets = prev.Targets
			sub.NeedsConfigSelection = prev.NeedsConfigSelection
			sub.LastStatus = prev.LastStatus
			sub.History = prev.History
			sub.CreatedAt = prev.CreatedAt
		}
		batch = append(batch, sub)
	}

	added, existing, err := s.store.BatchUpsertStockSubs(r.Context(), batch)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "BATCH_SUB_FAILED", err.Error())
		return
	}

	if len(batch) > 0 {
		s.StartStockMonitor(s.getRootCtx())
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status":   "success",
		"added":    added,
		"skipped":  existing,
		"existing": existing,
		"total":    len(batch),
		"message":  fmt.Sprintf("Added %d new subscriptions (%d existing updated)", added, existing),
	})
}

// HandleClearStockSubs handles DELETE|POST /api/monitor/subscriptions/clear.
func (s *Service) HandleClearStockSubs(w http.ResponseWriter, r *http.Request) {
	accountID := strings.TrimSpace(r.Header.Get("X-OVH-Account"))
	cleared, err := s.store.ClearStockSubs(r.Context(), accountID)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "SUBSCRIPTIONS_CLEAR_FAILED", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":  "success",
		"count":   cleared,
		"cleared": cleared,
	})
}

// HandleGetStockHistory handles GET /api/monitor/subscriptions/{planCode}/history returning newest-first entries.
func (s *Service) HandleGetStockHistory(w http.ResponseWriter, r *http.Request) {
	planCode := strings.TrimSpace(chi.URLParam(r, "planCode"))
	accountID := strings.TrimSpace(r.Header.Get("X-OVH-Account"))

	subs, err := s.store.ListStockSubs(r.Context(), accountID)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "HISTORY_LOAD_FAILED", err.Error())
		return
	}
	for _, it := range subs {
		if it.PlanCode == planCode {
			rev := make([]store.StockHistoryEntry, len(it.History))
			for i := range it.History {
				rev[i] = it.History[len(it.History)-1-i]
			}
			writeJSON(w, http.StatusOK, map[string]any{
				"planCode":   it.PlanCode,
				"serverName": it.ServerName,
				"history":    rev,
			})
			return
		}
	}
	writeJSONError(w, http.StatusNotFound, "SUBSCRIPTION_NOT_FOUND", "Stock subscription not found.")
}

// HandleStockCheckNow handles POST /api/monitor/check-now.
func (s *Service) HandleStockCheckNow(w http.ResponseWriter, r *http.Request) {
	transitions, err := s.CheckStockNow(r.Context())
	if err != nil {
		writeJSONError(w, http.StatusBadGateway, "STOCK_CHECK_FAILED", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":      "success",
		"transitions": transitions,
		"message":     "Stock check completed",
	})
}
