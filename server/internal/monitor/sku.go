package monitor

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"ovh-nexus/server/internal/catalog"
	"ovh-nexus/server/internal/logger"
	"ovh-nexus/server/internal/notify"
	"ovh-nexus/server/internal/store"
)

const skuMaxChanges = 200

// TrackedSKU captures the baseline state of a dedicated server SKU in the public catalog.
type TrackedSKU struct {
	Key            string   `json:"key"`
	PlanCode       string   `json:"planCode"`
	PlanCodes      []string `json:"planCodes,omitempty"`
	Name           string   `json:"name"`
	Range          string   `json:"range"`
	Price          *float64 `json:"price,omitempty"`
	Currency       string   `json:"currency,omitempty"`
	FirstSeen      string   `json:"firstSeen"`
	Baseline       bool     `json:"baseline,omitempty"`
	ListingState   string   `json:"listingState,omitempty"`
	PresaleSince   string   `json:"presaleSince,omitempty"`
	ListedAt       string   `json:"listedAt,omitempty"`
	Generation     int      `json:"generation,omitempty"`
	CPUText        string   `json:"cpuText,omitempty"`
	MemorySummary  string   `json:"memorySummary,omitempty"`
	StorageSummary string   `json:"storageSummary,omitempty"`
	StockState     string   `json:"stockState,omitempty"`
	Configs        []string `json:"configs,omitempty"`
}

// SKUChangeRecord represents an added, removed, or hardware-config-modified SKU change event.
type SKUChangeRecord struct {
	ID             string   `json:"id"`
	Type           string   `json:"type"`
	ListingState   string   `json:"listingState,omitempty"`
	Generation     int      `json:"generation,omitempty"`
	SKUKey         string   `json:"skuKey"`
	PlanCode       string   `json:"planCode"`
	PlanCodes      []string `json:"planCodes,omitempty"`
	Name           string   `json:"name"`
	Range          string   `json:"range,omitempty"`
	Endpoint       string   `json:"endpoint"`
	Timestamp      string   `json:"timestamp"`
	Price          *float64 `json:"price,omitempty"`
	Currency       string   `json:"currency,omitempty"`
	Details        string   `json:"details,omitempty"`
	CPUText        string   `json:"cpuText,omitempty"`
	MemorySummary  string   `json:"memorySummary,omitempty"`
	StorageSummary string   `json:"storageSummary,omitempty"`
	StockState     string   `json:"stockState,omitempty"`
	AddedConfigs   []string `json:"addedConfigs,omitempty"`
	RemovedConfigs []string `json:"removedConfigs,omitempty"`
	Added          []string `json:"added,omitempty"`
	Removed        []string `json:"removed,omitempty"`
}

// SKUWatcherStatus matches SkuWatcherStatus in NotificationCenterPage.tsx and OVHAvailabilityPage.tsx.
type SKUWatcherStatus struct {
	Running            bool                    `json:"running"`
	Initialized        bool                    `json:"initialized"`
	Endpoint           string                  `json:"endpoint"`
	TotalSKUs          int                     `json:"totalSkus"`
	SKUCount           int                     `json:"skuCount"`
	TotalTracked       int                     `json:"totalTracked"`
	LastCheckTime      *string                 `json:"lastCheckTime"`
	LastError          *string                 `json:"lastError"`
	UpdatedAt          string                  `json:"updatedAt"`
	LastCheck          string                  `json:"lastCheck"`
	RecentChangesCount int                     `json:"recentChangesCount"`
	RecentChanges      []SKUChangeRecord       `json:"recentChanges"`
	RecentFirstSeen    map[string]string       `json:"recentFirstSeen"`
	PresaleSKUs        map[string]string       `json:"presaleSkus,omitempty"`
	Rules              store.NotificationRules `json:"rules"`
}

// SKUCheckResult summarizes the outcome of a single SKU Watcher execution cycle.
type SKUCheckResult struct {
	Status        string            `json:"status"`
	Endpoint      string            `json:"endpoint"`
	TotalSKUs     int               `json:"totalSkus"`
	TrackedCount  int               `json:"trackedCount"`
	AddedCount    int               `json:"addedCount"`
	RemovedCount  int               `json:"removedCount"`
	ConfigChanges int               `json:"configChanges"`
	Added         []SKUChangeRecord `json:"added"`
	Removed       []SKUChangeRecord `json:"removed"`
	Changes       []SKUChangeRecord `json:"changes,omitempty"`
}

type skuAvailSummary struct {
	configs        []string
	memorySummary  string
	storageSummary string
	stockState     string
}

// StartSKUWatcher runs the periodic Public SKU Watcher loop using configured NotificationRules.
func (s *Service) StartSKUWatcher(ctx context.Context) {
	s.skuMu.Lock()
	defer s.skuMu.Unlock()

	if s.skuRunning.Load() {
		return
	}
	parent := ctx
	if parent == nil {
		parent = s.getRootCtx()
	}
	runCtx, cancel := context.WithCancel(parent)
	s.skuCancel = cancel
	s.skuRunning.Store(true)

	if s.log != nil {
		s.log.Info(logger.SourceSKUWatcher, "Public SKU watcher started")
	}

	go func() {
		defer s.skuRunning.Store(false)
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-runCtx.Done():
				if s.log != nil {
					s.log.Info(logger.SourceSKUWatcher, "Public SKU watcher stopped")
				}
				return
			case <-ticker.C:
				_, _ = s.CheckSKUOnce(runCtx, false)
			}
		}
	}()
}

// StopSKUWatcher stops the SKU Watcher loop.
func (s *Service) StopSKUWatcher() {
	s.skuMu.Lock()
	defer s.skuMu.Unlock()
	if s.skuCancel != nil {
		s.skuCancel()
		s.skuCancel = nil
	}
	s.skuRunning.Store(false)
}

func (s *Service) setSKUError(err error) {
	s.skuMu.Lock()
	defer s.skuMu.Unlock()
	if err == nil {
		s.skuLastError = ""
	} else {
		s.skuLastError = err.Error()
	}
}

// CheckSKUOnce inspects OVH public catalogs and detects newly added, removed, or reconfigured SKUs.
func (s *Service) CheckSKUOnce(ctx context.Context, manual bool) (*SKUCheckResult, error) {
	if s.store == nil || s.catalog == nil {
		return nil, errors.New("sku watcher store or catalog service is unavailable")
	}

	s.skuCheckMu.Lock()
	defer s.skuCheckMu.Unlock()

	settings, err := s.store.GetNotificationSettings(ctx)
	if err != nil {
		s.setSKUError(err)
		return nil, err
	}
	rules := settings.Rules
	locale := settings.Locale
	if locale == "" {
		locale = notify.LocaleEN
	}

	if !manual && !rules.SKUEnabled {
		return &SKUCheckResult{
			Status:  "disabled",
			Added:   []SKUChangeRecord{},
			Removed: []SKUChangeRecord{},
		}, nil
	}

	intervalSec := rules.SKUInterval
	if intervalSec < 60 {
		intervalSec = 60
	}
	if !manual && !s.skuLastRun.IsZero() && time.Since(s.skuLastRun) < time.Duration(intervalSec)*time.Second {
		return &SKUCheckResult{
			Status:  "cooldown",
			Added:   []SKUChangeRecord{},
			Removed: []SKUChangeRecord{},
		}, nil
	}

	ep, zn := s.resolveEndpointAndZone(ctx, "")
	dict, err := s.catalog.GetDictionary(ctx, ep, zn, true)
	if err != nil || dict == nil || len(dict.Plans) == 0 {
		if err != nil {
			s.setSKUError(err)
			return nil, fmt.Errorf("fetch public catalog dictionary: %w", err)
		}
		return &SKUCheckResult{
			Status:   "empty_catalog",
			Endpoint: ep,
			Added:    []SKUChangeRecord{},
			Removed:  []SKUChangeRecord{},
		}, nil
	}

	var availItems []catalog.RawAvailabilityItem
	var availSummaryMap map[string]*skuAvailSummary
	if items, aErr := s.catalog.GetRawAvailabilities(ctx, ep, ""); aErr == nil {
		availItems = items
		availSummaryMap = buildSKUAvailSummaries(availItems, locale)
	}

	currSKUs := collectCatalogSKUs(dict, rules, availItems, availSummaryMap, locale)
	if len(currSKUs) == 0 {
		return &SKUCheckResult{
			Status:   "empty_filtered_catalog",
			Endpoint: ep,
			Added:    []SKUChangeRecord{},
			Removed:  []SKUChangeRecord{},
		}, nil
	}

	state, err := s.store.LoadSKUWatcherState(ctx)
	if err != nil {
		s.setSKUError(err)
		return nil, err
	}

	prevSKUs := make(map[string]TrackedSKU)
	_ = json.Unmarshal(state.SKUsJSON, &prevSKUs)
	normalizeLoadedSKUs(prevSKUs, state.Initialized)
	missingCounts := make(map[string]int)
	_ = json.Unmarshal(state.MissingJSON, &missingCounts)
	var history []SKUChangeRecord
	_ = json.Unmarshal(state.ChangesJSON, &history)

	nowUTC := time.Now().UTC().Format(time.RFC3339Nano)
	s.skuLastRun = time.Now()
	s.setSKUError(nil)

	// Anti-false-positive Rule 1: First initialization or endpoint switch builds silent baseline.
	if !state.Initialized || state.Endpoint != ep || len(prevSKUs) == 0 {
		for k, entry := range currSKUs {
			entry.Baseline = true
			entry.FirstSeen = nowUTC
			entry.ListedAt = ""
			if entry.ListingState == "presale" {
				entry.PresaleSince = nowUTC
			} else {
				entry.ListingState = "listed"
				entry.PresaleSince = ""
			}
			currSKUs[k] = entry
		}
		if err := s.saveSKUState(ctx, true, ep, currSKUs, map[string]int{}, history); err != nil {
			s.setSKUError(err)
			return nil, err
		}
		if s.log != nil {
			s.log.Info(logger.SourceSKUWatcher, "Initialized SKU watcher baseline", "endpoint", ep, "trackedCount", len(currSKUs))
		}
		return &SKUCheckResult{
			Status:       "baseline_initialized",
			Endpoint:     ep,
			TotalSKUs:    len(currSKUs),
			TrackedCount: len(currSKUs),
			Added:        []SKUChangeRecord{},
			Removed:      []SKUChangeRecord{},
		}, nil
	}

	// Anti-false-positive Rule 2: >30% sudden drop when previous count >= 10 is skipped as a catalog glitch.
	missingFromCurr := 0
	for k := range prevSKUs {
		if _, ok := currSKUs[k]; !ok {
			missingFromCurr++
		}
	}
	if len(prevSKUs) >= 10 && float64(missingFromCurr)/float64(len(prevSKUs)) > 0.30 {
		if s.log != nil {
			s.log.Warn(logger.SourceSKUWatcher, "Skipped SKU diff due to suspected catalog API glitch",
				"endpoint", ep,
				"prevCount", len(prevSKUs),
				"currCount", len(currSKUs),
				"missingCount", missingFromCurr,
			)
		}
		return &SKUCheckResult{
			Status:       "skipped_glitch",
			Endpoint:     ep,
			TotalSKUs:    len(prevSKUs),
			TrackedCount: len(prevSKUs),
			Added:        []SKUChangeRecord{},
			Removed:      []SKUChangeRecord{},
		}, nil
	}

	nextSKUs := make(map[string]TrackedSKU, len(currSKUs)+missingFromCurr)
	newChanges := make([]SKUChangeRecord, 0)
	addedRecords := make([]SKUChangeRecord, 0)
	removedRecords := make([]SKUChangeRecord, 0)
	configChangesCount := 0

	for k, curr := range currSKUs {
		delete(missingCounts, k)
		skuKey := nonEmpty(curr.Key, curr.PlanCode)
		prev, existed := prevSKUs[k]
		if !existed {
			curr.Baseline = false
			curr.FirstSeen = nowUTC
			details := fmt.Sprintf("New SKU detected: %s (%s)", curr.Name, curr.PlanCode)
			if curr.ListingState == "presale" {
				curr.PresaleSince = nowUTC
				curr.ListedAt = ""
				details = fmt.Sprintf("New presale SKU detected: %s (%s)", curr.Name, curr.PlanCode)
			} else {
				curr.ListingState = "listed"
				curr.ListedAt = nowUTC
			}
			nextSKUs[k] = curr
			rec := SKUChangeRecord{
				ID:             newShortID(),
				Type:           "added",
				ListingState:   curr.ListingState,
				Generation:     curr.Generation,
				SKUKey:         skuKey,
				PlanCode:       curr.PlanCode,
				PlanCodes:      append([]string(nil), curr.PlanCodes...),
				Name:           curr.Name,
				Range:          curr.Range,
				Endpoint:       ep,
				Timestamp:      nowUTC,
				Price:          curr.Price,
				Currency:       curr.Currency,
				CPUText:        curr.CPUText,
				MemorySummary:  curr.MemorySummary,
				StorageSummary: curr.StorageSummary,
				StockState:     curr.StockState,
				Details:        details,
			}
			addedRecords = append(addedRecords, rec)
			newChanges = append(newChanges, rec)
			continue
		}

		curr.Baseline = prev.Baseline
		if prev.FirstSeen != "" {
			curr.FirstSeen = prev.FirstSeen
		} else {
			curr.FirstSeen = nowUTC
		}
		curr.PresaleSince = prev.PresaleSince
		curr.ListedAt = prev.ListedAt

		if prev.ListingState == "presale" && curr.ListingState == "listed" {
			curr.ListedAt = nowUTC
			curr.PresaleSince = nonEmpty(prev.PresaleSince, prev.FirstSeen)
			rec := SKUChangeRecord{
				ID:             newShortID(),
				Type:           "added",
				ListingState:   "listed",
				Generation:     curr.Generation,
				SKUKey:         skuKey,
				PlanCode:       curr.PlanCode,
				PlanCodes:      append([]string(nil), curr.PlanCodes...),
				Name:           curr.Name,
				Range:          curr.Range,
				Endpoint:       ep,
				Timestamp:      nowUTC,
				Price:          curr.Price,
				Currency:       curr.Currency,
				CPUText:        curr.CPUText,
				MemorySummary:  curr.MemorySummary,
				StorageSummary: curr.StorageSummary,
				StockState:     curr.StockState,
				Details:        fmt.Sprintf("Presale SKU officially listed: %s (%s)", curr.Name, curr.PlanCode),
			}
			addedRecords = append(addedRecords, rec)
			newChanges = append(newChanges, rec)
		} else if curr.ListingState == "presale" && curr.PresaleSince == "" {
			curr.PresaleSince = nonEmpty(prev.PresaleSince, curr.FirstSeen)
		}

		if rules.SKUTrackConfigs && len(prev.Configs) > 0 && len(curr.Configs) > 0 {
			addedCfgs, removedCfgs := diffStringSets(prev.Configs, curr.Configs)
			// Guard against partial availability payload wiping configs.
			if len(removedCfgs) > 0 && float64(len(removedCfgs))/float64(len(prev.Configs)) > 0.50 {
				removedCfgs = nil
			}
			if len(addedCfgs) > 0 || len(removedCfgs) > 0 {
				configChangesCount++
				var detailParts []string
				if len(addedCfgs) > 0 {
					detailParts = append(detailParts, "+"+strings.Join(addedCfgs, ", "))
				}
				if len(removedCfgs) > 0 {
					detailParts = append(detailParts, "-"+strings.Join(removedCfgs, ", "))
				}
				newChanges = append(newChanges, SKUChangeRecord{
					ID:             newShortID(),
					Type:           "config_changed",
					SKUKey:         skuKey,
					PlanCode:       curr.PlanCode,
					PlanCodes:      append([]string(nil), curr.PlanCodes...),
					Name:           curr.Name,
					Range:          curr.Range,
					Endpoint:       ep,
					Timestamp:      nowUTC,
					CPUText:        curr.CPUText,
					MemorySummary:  curr.MemorySummary,
					StorageSummary: curr.StorageSummary,
					StockState:     curr.StockState,
					AddedConfigs:   addedCfgs,
					RemovedConfigs: removedCfgs,
					Added:          addedCfgs,
					Removed:        removedCfgs,
					Details:        fmt.Sprintf("Updated configs for %s: %s", curr.PlanCode, strings.Join(detailParts, " | ")),
				})
			}
		} else if len(curr.Configs) == 0 && len(prev.Configs) > 0 {
			curr.Configs = prev.Configs
			if curr.MemorySummary == "" {
				curr.MemorySummary = prev.MemorySummary
			}
			if curr.StorageSummary == "" {
				curr.StorageSummary = prev.StorageSummary
			}
			if curr.StockState == "" {
				curr.StockState = prev.StockState
			}
		}

		nextSKUs[k] = curr
	}

	// Anti-false-positive Rule 3: removal requires 2 consecutive missing checks.
	for k, prev := range prevSKUs {
		if _, present := currSKUs[k]; present {
			continue
		}
		missingCounts[k]++
		if missingCounts[k] >= 2 {
			delete(missingCounts, k)
			rec := SKUChangeRecord{
				ID:             newShortID(),
				Type:           "removed",
				SKUKey:         nonEmpty(prev.Key, prev.PlanCode),
				PlanCode:       prev.PlanCode,
				PlanCodes:      append([]string(nil), prev.PlanCodes...),
				Name:           prev.Name,
				Range:          prev.Range,
				Endpoint:       ep,
				Timestamp:      nowUTC,
				Price:          prev.Price,
				Currency:       prev.Currency,
				CPUText:        prev.CPUText,
				MemorySummary:  prev.MemorySummary,
				StorageSummary: prev.StorageSummary,
				StockState:     prev.StockState,
				Details:        fmt.Sprintf("SKU removed from catalog: %s (%s)", prev.Name, prev.PlanCode),
			}
			removedRecords = append(removedRecords, rec)
			newChanges = append(newChanges, rec)
		} else {
			// Retain until confirmed missing a second consecutive cycle.
			nextSKUs[k] = prev
		}
	}

	if len(newChanges) > 0 {
		combined := make([]SKUChangeRecord, 0, min(skuMaxChanges, len(newChanges)+len(history)))
		combined = append(combined, newChanges...)
		combined = append(combined, history...)
		if len(combined) > skuMaxChanges {
			combined = combined[:skuMaxChanges]
		}
		history = combined
	}

	if err := s.saveSKUState(ctx, true, ep, nextSKUs, missingCounts, history); err != nil {
		s.setSKUError(err)
		return nil, err
	}

	if len(newChanges) > 0 {
		if s.log != nil {
			s.log.Info(logger.SourceSKUWatcher, "Detected public catalog SKU changes",
				"endpoint", ep,
				"added", len(addedRecords),
				"removed", len(removedRecords),
				"configChanges", configChangesCount,
			)
		}
		if s.notify != nil {
			var lines []string
			for i, ch := range newChanges {
				if i >= 12 {
					lines = append(lines, fmt.Sprintf("...and %d more changes", len(newChanges)-12))
					break
				}
				lines = append(lines, "- "+ch.Details)
			}
			_ = s.notify.EmitEvent(ctx, notify.EventSKUChanged, "", "", nil, notify.WithTemplateData(map[string]any{
				"preferTemplate": true,
				"endpoint":       ep,
				"addedCount":     len(addedRecords),
				"removedCount":   len(removedRecords),
				"changedCount":   configChangesCount,
				"details":        strings.Join(lines, "\n"),
			}))
		}
	}

	return &SKUCheckResult{
		Status:        "ok",
		Endpoint:      ep,
		TotalSKUs:     len(nextSKUs),
		TrackedCount:  len(nextSKUs),
		AddedCount:    len(addedRecords),
		RemovedCount:  len(removedRecords),
		ConfigChanges: configChangesCount,
		Added:         addedRecords,
		Removed:       removedRecords,
		Changes:       newChanges,
	}, nil
}

func (s *Service) saveSKUState(
	ctx context.Context,
	initialized bool,
	endpoint string,
	skus map[string]TrackedSKU,
	missing map[string]int,
	changes []SKUChangeRecord,
) error {
	if changes == nil {
		changes = []SKUChangeRecord{}
	}
	skusBytes, _ := json.Marshal(skus)
	missingBytes, _ := json.Marshal(missing)
	changesBytes, _ := json.Marshal(changes)
	return s.store.SaveSKUWatcherState(ctx, &store.SKUWatcherState{
		Initialized: initialized,
		Endpoint:    endpoint,
		SKUsJSON:    skusBytes,
		MissingJSON: missingBytes,
		ChangesJSON: changesBytes,
	})
}

func collectCatalogSKUs(
	dict *catalog.CatalogDictionary,
	rules store.NotificationRules,
	availItems []catalog.RawAvailabilityItem,
	availSummaries map[string]*skuAvailSummary,
	locale string,
) map[string]TrackedSKU {
	allowedRanges := make(map[string]bool, len(rules.SKURanges))
	for _, r := range rules.SKURanges {
		up := strings.ToUpper(strings.TrimSpace(r))
		if up != "" {
			if up == "ADVANCE" {
				up = "ADV"
			}
			allowedRanges[up] = true
		}
	}

	out := make(map[string]TrackedSKU)
	for planCode, entry := range dict.Plans {
		if strings.EqualFold(entry.CatalogSource, "vps") {
			continue
		}
		meta := catalog.ResolvePlanWithDict(planCode, "", dict)
		rng := strings.ToUpper(strings.TrimSpace(meta.Range))
		if rng == "" {
			rng = catalog.ClassifyProductRange(meta.Name, "", planCode)
		}
		if rng == "VPS" {
			continue
		}
		if len(allowedRanges) > 0 && !allowedRanges[rng] {
			continue
		}

		key := strings.ToLower(planCode)
		displayCode := planCode
		if rules.SKUMergeRegional {
			baseCode, _ := catalog.ParsePlanRegion(planCode)
			if baseCode != "" {
				key = strings.ToLower(baseCode)
				displayCode = baseCode
			}
		}

		cpuText := catalog.FormatCPU(meta.CPU, locale)
		if cpuText == "-" {
			cpuText = ""
		}

		var cfgs []string
		var memSum, stoSum, stockState string
		if sum := availSummaries[strings.ToLower(planCode)]; sum != nil {
			cfgs = append([]string(nil), sum.configs...)
			memSum = sum.memorySummary
			stoSum = sum.storageSummary
			stockState = sum.stockState
		}

		existing, ok := out[key]
		if !ok {
			out[key] = TrackedSKU{
				Key:            key,
				PlanCode:       displayCode,
				PlanCodes:      []string{planCode},
				Name:           meta.Name,
				Range:          rng,
				Price:          entry.Price,
				Currency:       nonEmpty(entry.Currency, dict.Currency),
				ListingState:   "listed",
				Generation:     catalog.InferPlanGeneration(displayCode),
				CPUText:        cpuText,
				MemorySummary:  memSum,
				StorageSummary: stoSum,
				StockState:     stockState,
				Configs:        cfgs,
			}
			continue
		}

		existing.ListingState = "listed"
		if existing.Generation == 0 {
			existing.Generation = catalog.InferPlanGeneration(displayCode)
		}
		if !slices.Contains(existing.PlanCodes, planCode) {
			existing.PlanCodes = append(existing.PlanCodes, planCode)
		}
		if strings.EqualFold(planCode, displayCode) {
			existing.PlanCode = planCode
			if meta.Name != "" {
				existing.Name = meta.Name
			}
			if cpuText != "" {
				existing.CPUText = cpuText
			}
		}
		if existing.Price == nil || (entry.Price != nil && *entry.Price < *existing.Price) {
			existing.Price = entry.Price
			existing.Currency = nonEmpty(entry.Currency, dict.Currency)
		}
		if existing.MemorySummary == "" {
			existing.MemorySummary = memSum
		}
		if existing.StorageSummary == "" {
			existing.StorageSummary = stoSum
		}
		if stockState == "in_stock" || existing.StockState == "" {
			existing.StockState = stockState
		}
		for _, c := range cfgs {
			if !slices.Contains(existing.Configs, c) {
				existing.Configs = append(existing.Configs, c)
			}
		}
		slices.Sort(existing.Configs)
		out[key] = existing
	}

	// Include availability-only SKUs not yet present in the public catalog as presale SKUs.
	for _, it := range availItems {
		planCode := strings.TrimSpace(it.PlanCode)
		if planCode == "" || catalog.IsNonServerAddonCode(planCode) {
			continue
		}
		if catalog.IsPlanListedInDict(planCode, dict) {
			continue
		}

		meta := catalog.ResolvePlanWithDict(planCode, it.Server, dict)
		rng := strings.ToUpper(strings.TrimSpace(meta.Range))
		if rng == "" {
			rng = catalog.ClassifyProductRange(meta.Name, "", planCode)
		}
		if rng == "VPS" {
			continue
		}
		if len(allowedRanges) > 0 && !allowedRanges[rng] {
			continue
		}

		key := strings.ToLower(planCode)
		displayCode := planCode
		if rules.SKUMergeRegional {
			baseCode, _ := catalog.ParsePlanRegion(planCode)
			if baseCode != "" {
				key = strings.ToLower(baseCode)
				displayCode = baseCode
			}
		}

		cpuText := catalog.FormatCPU(meta.CPU, locale)
		if cpuText == "-" {
			cpuText = ""
		}

		var cfgs []string
		var memSum, stoSum, stockState string
		if sum := availSummaries[strings.ToLower(planCode)]; sum != nil {
			cfgs = append([]string(nil), sum.configs...)
			memSum = sum.memorySummary
			stoSum = sum.storageSummary
			stockState = sum.stockState
		}

		existing, ok := out[key]
		if !ok {
			out[key] = TrackedSKU{
				Key:            key,
				PlanCode:       displayCode,
				PlanCodes:      []string{planCode},
				Name:           nonEmpty(meta.Name, displayCode),
				Range:          rng,
				Price:          nil,
				Currency:       nonEmpty(meta.Currency, dict.Currency),
				ListingState:   "presale",
				Generation:     catalog.InferPlanGeneration(displayCode),
				CPUText:        cpuText,
				MemorySummary:  memSum,
				StorageSummary: stoSum,
				StockState:     stockState,
				Configs:        cfgs,
			}
			continue
		}

		if !slices.Contains(existing.PlanCodes, planCode) {
			existing.PlanCodes = append(existing.PlanCodes, planCode)
		}
		if existing.MemorySummary == "" {
			existing.MemorySummary = memSum
		}
		if existing.StorageSummary == "" {
			existing.StorageSummary = stoSum
		}
		if stockState == "in_stock" || existing.StockState == "" {
			existing.StockState = stockState
		}
		for _, c := range cfgs {
			if !slices.Contains(existing.Configs, c) {
				existing.Configs = append(existing.Configs, c)
			}
		}
		slices.Sort(existing.Configs)
		out[key] = existing
	}

	return out
}

// normalizeLoadedSKUs applies a one-time migration guard for legacy v1.3.0 baseline snapshots
// where Initialized == true and all entries lack Baseline/ListedAt and share the exact same FirstSeen timestamp.
func normalizeLoadedSKUs(skus map[string]TrackedSKU, initialized bool) {
	if len(skus) == 0 {
		return
	}
	allUnmigratedSameTS := initialized
	var sharedFirstSeen string
	for _, entry := range skus {
		if entry.Baseline || entry.ListedAt != "" || entry.PresaleSince != "" || entry.FirstSeen == "" {
			allUnmigratedSameTS = false
			break
		}
		if sharedFirstSeen == "" {
			sharedFirstSeen = entry.FirstSeen
		} else if entry.FirstSeen != sharedFirstSeen {
			allUnmigratedSameTS = false
			break
		}
	}

	for k, entry := range skus {
		if allUnmigratedSameTS {
			entry.Baseline = true
			entry.ListedAt = ""
		}
		if entry.ListingState == "" {
			entry.ListingState = "listed"
		}
		if entry.Generation == 0 {
			entry.Generation = catalog.InferPlanGeneration(entry.PlanCode)
		}
		skus[k] = entry
	}
}

func buildSKUAvailSummaries(items []catalog.RawAvailabilityItem, locale string) map[string]*skuAvailSummary {
	type planAccum struct {
		cfgSet   map[string]bool
		mems     map[string]bool
		stos     map[string]bool
		hasStock bool
	}
	accMap := make(map[string]*planAccum)

	for _, it := range items {
		pc := strings.ToLower(strings.TrimSpace(it.PlanCode))
		if pc == "" {
			continue
		}
		acc, ok := accMap[pc]
		if !ok {
			acc = &planAccum{
				cfgSet: make(map[string]bool),
				mems:   make(map[string]bool),
				stos:   make(map[string]bool),
			}
			accMap[pc] = acc
		}

		mem := catalog.StandardizeConfig(it.Memory)
		sto := catalog.StandardizeConfig(it.Storage)
		fp := mem + "+" + sto
		if it.GPU != "" {
			fp += "+" + strings.ToLower(it.GPU)
		}
		acc.cfgSet[fp] = true

		if it.Memory != "" {
			if txt := catalog.FormatMemory(catalog.ParseMemoryCode(it.Memory, nil), locale); txt != "" && txt != "-" {
				acc.mems[txt] = true
			}
		}
		if it.Storage != "" {
			if txt := catalog.FormatStorage(catalog.ParseStorageCode(it.Storage, false), locale); txt != "" && txt != "-" {
				acc.stos[txt] = true
			}
		}
		for _, dc := range it.Datacenters {
			if isStockStatusAvailable(dc.Availability) {
				acc.hasStock = true
				break
			}
		}
	}

	out := make(map[string]*skuAvailSummary, len(accMap))
	for pc, acc := range accMap {
		cfgs := mapKeysSorted(acc.cfgSet)
		mems := mapKeysSorted(acc.mems)
		stos := mapKeysSorted(acc.stos)
		stockState := "out_of_stock"
		if acc.hasStock {
			stockState = "in_stock"
		}
		out[pc] = &skuAvailSummary{
			configs:        cfgs,
			memorySummary:  strings.Join(mems, " / "),
			storageSummary: strings.Join(stos, " / "),
			stockState:     stockState,
		}
	}
	return out
}

func mapKeysSorted(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

func diffStringSets(prev, curr []string) ([]string, []string) {
	prevSet := make(map[string]bool, len(prev))
	currSet := make(map[string]bool, len(curr))
	for _, p := range prev {
		prevSet[p] = true
	}
	for _, c := range curr {
		currSet[c] = true
	}
	var added, removed []string
	for _, c := range curr {
		if !prevSet[c] {
			added = append(added, c)
		}
	}
	for _, p := range prev {
		if !currSet[p] {
			removed = append(removed, p)
		}
	}
	slices.Sort(added)
	slices.Sort(removed)
	return added, removed
}

func nonEmpty(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}

func newShortID() string {
	var b [6]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// GetSKUStatusAndChanges returns the current SKU Watcher status and change timeline.
func (s *Service) GetSKUStatusAndChanges(ctx context.Context) (*SKUWatcherStatus, []SKUChangeRecord, error) {
	return s.buildSKUStatusAndChanges(ctx)
}

func (s *Service) buildSKUStatusAndChanges(ctx context.Context) (*SKUWatcherStatus, []SKUChangeRecord, error) {
	state, err := s.store.LoadSKUWatcherState(ctx)
	if err != nil {
		return nil, nil, err
	}
	settings, _ := s.store.GetNotificationSettings(ctx)
	var rules store.NotificationRules
	if settings != nil {
		rules = settings.Rules
	}

	skus := make(map[string]TrackedSKU)
	_ = json.Unmarshal(state.SKUsJSON, &skus)
	normalizeLoadedSKUs(skus, state.Initialized)
	var changes []SKUChangeRecord
	_ = json.Unmarshal(state.ChangesJSON, &changes)
	if changes == nil {
		changes = []SKUChangeRecord{}
	}

	recordPlanCodeTimestamp := func(target map[string]string, entry TrackedSKU, tsStr string) {
		if tsStr == "" {
			return
		}
		if entry.PlanCode != "" {
			target[entry.PlanCode] = tsStr
			if base, _ := catalog.ParsePlanRegion(entry.PlanCode); base != "" {
				target[base] = tsStr
			}
		}
		if entry.Key != "" {
			target[entry.Key] = tsStr
		}
		for _, pc := range entry.PlanCodes {
			if pc != "" {
				target[pc] = tsStr
				if base, _ := catalog.ParsePlanRegion(pc); base != "" {
					target[base] = tsStr
				}
			}
		}
	}

	cutoff := time.Now().Add(-7 * 24 * time.Hour)
	recentFirstSeen := make(map[string]string)
	presaleSKUs := make(map[string]string)
	for _, entry := range skus {
		if entry.ListingState == "presale" {
			recordPlanCodeTimestamp(presaleSKUs, entry, nonEmpty(entry.PresaleSince, entry.FirstSeen))
			continue
		}
		if entry.ListedAt == "" {
			continue
		}
		ts, parseErr := time.Parse(time.RFC3339Nano, entry.ListedAt)
		if parseErr != nil || !ts.After(cutoff) {
			continue
		}
		recordPlanCodeTimestamp(recentFirstSeen, entry, entry.ListedAt)
	}

	recentChanges := changes
	if len(recentChanges) > 20 {
		recentChanges = recentChanges[:20]
	}

	var lastCheckPtr *string
	if state.UpdatedAt != "" {
		ts := state.UpdatedAt
		lastCheckPtr = &ts
	}
	s.skuMu.Lock()
	errText := s.skuLastError
	s.skuMu.Unlock()
	var lastErrPtr *string
	if errText != "" {
		lastErrPtr = &errText
	}

	status := &SKUWatcherStatus{
		Running:            s.skuRunning.Load(),
		Initialized:        state.Initialized,
		Endpoint:           nonEmpty(state.Endpoint, "ovh-eu"),
		TotalSKUs:          len(skus),
		SKUCount:           len(skus),
		TotalTracked:       len(skus),
		LastCheckTime:      lastCheckPtr,
		LastError:          lastErrPtr,
		UpdatedAt:          state.UpdatedAt,
		LastCheck:          state.UpdatedAt,
		RecentChangesCount: len(changes),
		RecentChanges:      recentChanges,
		RecentFirstSeen:    recentFirstSeen,
		PresaleSKUs:        presaleSKUs,
		Rules:              rules,
	}
	return status, changes, nil
}

// ==================== SKU Watcher HTTP Handlers ====================

// HandleSKUStatus handles GET /api/sku/status.
func (s *Service) HandleSKUStatus(w http.ResponseWriter, r *http.Request) {
	status, _, err := s.buildSKUStatusAndChanges(r.Context())
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "SKU_STATUS_FAILED", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, status)
}

// HandleSKUChanges handles GET /api/sku/changes.
func (s *Service) HandleSKUChanges(w http.ResponseWriter, r *http.Request) {
	status, changes, err := s.buildSKUStatusAndChanges(r.Context())
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "SKU_CHANGES_FAILED", err.Error())
		return
	}

	limit := skuMaxChanges
	if rawLimit := strings.TrimSpace(r.URL.Query().Get("limit")); rawLimit != "" {
		if n, convErr := strconv.Atoi(rawLimit); convErr == nil && n > 0 {
			limit = min(n, 500)
		}
	}
	total := len(changes)
	if len(changes) > limit {
		changes = changes[:limit]
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"changes":     changes,
		"total":       total,
		"initialized": status.Initialized,
		"endpoint":    status.Endpoint,
		"updatedAt":   status.UpdatedAt,
		"status":      status,
	})
}

// HandleSKUCheck handles POST /api/sku/check.
func (s *Service) HandleSKUCheck(w http.ResponseWriter, r *http.Request) {
	res, err := s.CheckSKUOnce(r.Context(), true)
	if err != nil {
		writeJSONError(w, http.StatusBadGateway, "SKU_CHECK_FAILED", err.Error())
		return
	}
	status, changes, stErr := s.buildSKUStatusAndChanges(r.Context())
	if stErr != nil {
		writeJSONError(w, http.StatusInternalServerError, "SKU_STATUS_FAILED", stErr.Error())
		return
	}
	if len(changes) > skuMaxChanges {
		changes = changes[:skuMaxChanges]
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"status":  status,
		"result":  res,
		"changes": changes,
		"message": fmt.Sprintf("SKU check completed (%s)", res.Status),
	})
}
