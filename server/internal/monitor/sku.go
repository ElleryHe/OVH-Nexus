package monitor

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"ovh-nexus/server/internal/catalog"
	"ovh-nexus/server/internal/logger"
	"ovh-nexus/server/internal/notify"
	"ovh-nexus/server/internal/store"
)

const (
	skuMaxChanges            = 200
	defaultSKUIntervalSec    = 120
	minSKUIntervalSec        = 60
	legacySKUIntervalSec     = 600
	defaultSKUFastRecheckDur = 20 * time.Second
)

var reAliasPortSuffix = regexp.MustCompile(`(?i)-\d+g$`)

type skuFastRecheckState struct {
	timer          *time.Timer
	pending        bool
	delayOverride  time.Duration
	listedAtByCode map[string]string
	listedAtLoaded bool
}

var (
	skuFastRecheckMu     sync.Mutex
	skuFastRecheckStates = make(map[*Service]*skuFastRecheckState)
)

func getSKUFastRecheckState(s *Service) *skuFastRecheckState {
	skuFastRecheckMu.Lock()
	defer skuFastRecheckMu.Unlock()
	st, ok := skuFastRecheckStates[s]
	if !ok {
		st = &skuFastRecheckState{
			listedAtByCode: make(map[string]string),
		}
		skuFastRecheckStates[s] = st
	}
	return st
}

// SetSKUFastRecheckDelay overrides the 20s missing-SKU confirmation delay (intended for tests).
func (s *Service) SetSKUFastRecheckDelay(d time.Duration) {
	skuFastRecheckMu.Lock()
	defer skuFastRecheckMu.Unlock()
	st, ok := skuFastRecheckStates[s]
	if !ok {
		st = &skuFastRecheckState{
			listedAtByCode: make(map[string]string),
		}
		skuFastRecheckStates[s] = st
	}
	st.delayOverride = d
}

func (s *Service) scheduleSKUFastRecheck() {
	skuFastRecheckMu.Lock()
	st, ok := skuFastRecheckStates[s]
	if !ok {
		st = &skuFastRecheckState{
			listedAtByCode: make(map[string]string),
		}
		skuFastRecheckStates[s] = st
	}
	delay := defaultSKUFastRecheckDur
	hasOverride := st.delayOverride > 0
	if hasOverride {
		delay = st.delayOverride
	}
	if !s.skuRunning.Load() && !hasOverride {
		skuFastRecheckMu.Unlock()
		return
	}
	if st.pending {
		skuFastRecheckMu.Unlock()
		return
	}
	st.pending = true
	st.timer = time.AfterFunc(delay, func() {
		skuFastRecheckMu.Lock()
		st.pending = false
		st.timer = nil
		skuFastRecheckMu.Unlock()

		if !s.skuRunning.Load() && !hasOverride {
			return
		}
		ctx := s.getRootCtx()
		if ctx != nil && ctx.Err() != nil {
			return
		}
		_, _ = s.checkSKUOnce(ctx, false, true)
	})
	skuFastRecheckMu.Unlock()
}

func (s *Service) cancelSKUFastRecheck() {
	skuFastRecheckMu.Lock()
	if st, ok := skuFastRecheckStates[s]; ok {
		if st.timer != nil {
			st.timer.Stop()
			st.timer = nil
		}
		st.pending = false
	}
	skuFastRecheckMu.Unlock()
}

func (s *Service) ensureCatalogListedAtResolver() {
	if s.catalog == nil {
		return
	}
	s.catalog.SetListedAtResolver(s.ResolveSKUListedAt)
}

// ResolveSKUListedAt returns the recorded listedAt timestamp for a planCode if known.
func (s *Service) ResolveSKUListedAt(planCode string) string {
	pc := strings.TrimSpace(planCode)
	if pc == "" {
		return ""
	}
	skuFastRecheckMu.Lock()
	st := skuFastRecheckStates[s]
	loaded := st != nil && st.listedAtLoaded
	skuFastRecheckMu.Unlock()
	if !loaded && s.store != nil {
		if state, err := s.store.LoadSKUWatcherState(context.Background()); err == nil && state != nil {
			var history []SKUChangeRecord
			_ = json.Unmarshal(state.ChangesJSON, &history)
			skus := make(map[string]TrackedSKU)
			_ = json.Unmarshal(state.SKUsJSON, &skus)
			if normalizeLoadedSKUs(skus, history, state.Initialized) {
				missingCounts := make(map[string]int)
				_ = json.Unmarshal(state.MissingJSON, &missingCounts)
				_ = s.saveSKUState(context.Background(), state.Initialized, state.Endpoint, skus, missingCounts, history)
			}
			s.updateCachedListedAtMap(skus)
		}
	}
	skuFastRecheckMu.Lock()
	st = skuFastRecheckStates[s]
	if st != nil && len(st.listedAtByCode) > 0 {
		if ts := st.listedAtByCode[strings.ToLower(pc)]; ts != "" {
			skuFastRecheckMu.Unlock()
			return ts
		}
		if base, _ := catalog.ParsePlanRegion(pc); base != "" {
			if ts := st.listedAtByCode[strings.ToLower(base)]; ts != "" {
				skuFastRecheckMu.Unlock()
				return ts
			}
		}
	}
	skuFastRecheckMu.Unlock()
	return ""
}

func (s *Service) updateCachedListedAtMap(skus map[string]TrackedSKU) {
	m := make(map[string]string, len(skus)*2)
	for _, entry := range skus {
		if entry.ListedAt == "" {
			continue
		}
		if entry.PlanCode != "" {
			m[strings.ToLower(entry.PlanCode)] = entry.ListedAt
			if base, _ := catalog.ParsePlanRegion(entry.PlanCode); base != "" {
				m[strings.ToLower(base)] = entry.ListedAt
			}
		}
		if entry.Key != "" {
			m[strings.ToLower(entry.Key)] = entry.ListedAt
		}
		for _, pc := range entry.PlanCodes {
			if pc != "" {
				m[strings.ToLower(pc)] = entry.ListedAt
			}
		}
	}
	skuFastRecheckMu.Lock()
	st, ok := skuFastRecheckStates[s]
	if !ok {
		st = &skuFastRecheckState{}
		skuFastRecheckStates[s] = st
	}
	st.listedAtByCode = m
	st.listedAtLoaded = true
	skuFastRecheckMu.Unlock()
}

// TrackedSKU captures the baseline state of a dedicated server SKU in the public catalog.
type TrackedSKU struct {
	Key            string   `json:"key"`
	SKUKey         string   `json:"skuKey,omitempty"`
	PlanCode       string   `json:"planCode"`
	PlanCodes      []string `json:"planCodes,omitempty"`
	Name           string   `json:"name"`
	Range          string   `json:"range"`
	Price          *float64 `json:"price,omitempty"`
	Currency       string   `json:"currency,omitempty"`
	FirstSeen      string   `json:"firstSeen"`
	FirstSeenAt    string   `json:"firstSeenAt,omitempty"`
	Baseline       bool     `json:"baseline,omitempty"`
	ListingState   string   `json:"listingState,omitempty"`
	SaleStarted    bool     `json:"saleStarted,omitempty"`
	ReplacedFrom   string   `json:"replacedFrom,omitempty"`
	PresaleSince   string   `json:"presaleSince,omitempty"`
	ListedAt       string   `json:"listedAt,omitempty"`
	Generation     int      `json:"generation,omitempty"`
	CPUText        string   `json:"cpuText,omitempty"`
	MemorySummary  string   `json:"memorySummary,omitempty"`
	StorageSummary string   `json:"storageSummary,omitempty"`
	StockState     string   `json:"stockState,omitempty"`
	Configs        []string `json:"configs,omitempty"`
}

// SKUSnapshotEntry aliases TrackedSKU.
type SKUSnapshotEntry = TrackedSKU

// SKUChangeRecord represents an added, removed, replaced, or hardware-config-modified SKU change event.
type SKUChangeRecord struct {
	ID             string   `json:"id"`
	Type           string   `json:"type"`
	ListingState   string   `json:"listingState,omitempty"`
	SaleStarted    bool     `json:"saleStarted,omitempty"`
	ReplacedFrom   string   `json:"replacedFrom,omitempty"`
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

// SKUHistoryEntry aliases SKUChangeRecord.
type SKUHistoryEntry = SKUChangeRecord

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
	AnnouncedSKUs      map[string]string       `json:"announcedSkus,omitempty"`
	Rules              store.NotificationRules `json:"rules"`
}

// SKUCheckResult summarizes the outcome of a single SKU Watcher execution cycle.
type SKUCheckResult struct {
	Status           string            `json:"status"`
	Endpoint         string            `json:"endpoint"`
	TotalSKUs        int               `json:"totalSkus"`
	TrackedCount     int               `json:"trackedCount"`
	AddedCount       int               `json:"addedCount"`
	RemovedCount     int               `json:"removedCount"`
	ReplacedCount    int               `json:"replacedCount,omitempty"`
	SaleStartedCount int               `json:"saleStartedCount,omitempty"`
	ConfigChanges    int               `json:"configChanges"`
	Added            []SKUChangeRecord `json:"added"`
	Removed          []SKUChangeRecord `json:"removed"`
	Replaced         []SKUChangeRecord `json:"replaced,omitempty"`
	Changes          []SKUChangeRecord `json:"changes,omitempty"`
}

type skuAvailSummary struct {
	configs        []string
	memorySummary  string
	storageSummary string
	stockState     string
}

// StartSKUWatcher runs the periodic Public SKU Watcher loop using configured NotificationRules.
func (s *Service) StartSKUWatcher(ctx context.Context) {
	s.ensureCatalogListedAtResolver()

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

// StopSKUWatcher stops the SKU Watcher loop and cancels any pending fast recheck timer.
func (s *Service) StopSKUWatcher() {
	s.cancelSKUFastRecheck()
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

func (s *Service) loadAndMigrateSKUSettings(ctx context.Context) (*store.NotificationSettings, error) {
	if s.store == nil {
		return nil, errors.New("sku watcher store is unavailable")
	}
	settings, err := s.store.GetNotificationSettings(ctx)
	if err != nil {
		return nil, err
	}
	if settings != nil && (settings.Rules.SKUInterval == legacySKUIntervalSec || settings.Rules.SKUInterval < minSKUIntervalSec) {
		settings.Rules.SKUInterval = defaultSKUIntervalSec
		if saved, saveErr := s.store.SaveNotificationSettings(ctx, *settings); saveErr == nil && saved != nil {
			settings = saved
		}
	}
	return settings, nil
}

// CheckSKUOnce inspects OVH public catalogs and detects newly added, removed, replaced, or reconfigured SKUs.
func (s *Service) CheckSKUOnce(ctx context.Context, manual bool) (*SKUCheckResult, error) {
	return s.checkSKUOnce(ctx, manual, false)
}

func (s *Service) checkSKUOnce(ctx context.Context, manual, fastRecheck bool) (*SKUCheckResult, error) {
	if s.store == nil || s.catalog == nil {
		return nil, errors.New("sku watcher store or catalog service is unavailable")
	}
	s.ensureCatalogListedAtResolver()

	s.skuCheckMu.Lock()
	defer s.skuCheckMu.Unlock()

	settings, err := s.loadAndMigrateSKUSettings(ctx)
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
	if intervalSec < minSKUIntervalSec {
		intervalSec = defaultSKUIntervalSec
	}
	if !manual && !fastRecheck && !s.skuLastRun.IsZero() && time.Since(s.skuLastRun) < time.Duration(intervalSec)*time.Second {
		return &SKUCheckResult{
			Status:  "cooldown",
			Added:   []SKUChangeRecord{},
			Removed: []SKUChangeRecord{},
		}, nil
	}

	ep, zn := s.resolveEndpointAndZone(ctx, "")
	dict, err := s.catalog.GetBaremetalDictionary(ctx, ep, zn, true)
	if err != nil || dict == nil || (len(dict.RealPlans) == 0 && len(dict.Plans) == 0) {
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
	missingCounts := make(map[string]int)
	_ = json.Unmarshal(state.MissingJSON, &missingCounts)
	var history []SKUChangeRecord
	_ = json.Unmarshal(state.ChangesJSON, &history)
	if normalizeLoadedSKUs(prevSKUs, history, state.Initialized) {
		s.updateCachedListedAtMap(prevSKUs)
		_ = s.saveSKUState(ctx, state.Initialized, state.Endpoint, prevSKUs, missingCounts, history)
	}

	if len(prevSKUs) > 0 && len(dict.RealPlans) > 0 {
		pruneAndMigratePrevSKUs(prevSKUs, missingCounts, currSKUs, dict, availItems, rules)
	}

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
				entry.PresaleSince = ""
				if entry.ListingState != "announced" {
					entry.ListingState = "listed"
				}
			}
			currSKUs[k] = entry
		}
		s.cancelSKUFastRecheck()
		s.updateCachedListedAtMap(currSKUs)
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
	addedRecords := make([]SKUChangeRecord, 0)
	saleStartedRecords := make([]SKUChangeRecord, 0)
	replacedRecords := make([]SKUChangeRecord, 0)
	removedRecords := make([]SKUChangeRecord, 0)
	configRecords := make([]SKUChangeRecord, 0)

	for k, curr := range currSKUs {
		delete(missingCounts, k)
		skuKey := nonEmpty(curr.Key, curr.PlanCode)
		prev, existed := prevSKUs[k]
		if !existed {
			curr.Baseline = false
			curr.FirstSeen = nowUTC
			details := fmt.Sprintf("New SKU detected: %s (%s)", curr.Name, curr.PlanCode)
			switch curr.ListingState {
			case "presale":
				curr.PresaleSince = nowUTC
				curr.ListedAt = ""
				details = fmt.Sprintf("New presale SKU detected: %s (%s)", curr.Name, curr.PlanCode)
			case "announced":
				curr.PresaleSince = ""
				curr.ListedAt = ""
				details = fmt.Sprintf("New announced SKU detected: %s (%s)", curr.Name, curr.PlanCode)
			default:
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
		curr.SaleStarted = prev.SaleStarted
		curr.ReplacedFrom = prev.ReplacedFrom

		if prev.ListingState == "announced" && curr.ListingState == "listed" {
			curr.Baseline = false
			curr.ListingState = "listed"
			curr.ListedAt = nowUTC
			curr.SaleStarted = true
			rec := SKUChangeRecord{
				ID:             newShortID(),
				Type:           "added",
				ListingState:   "listed",
				SaleStarted:    true,
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
				Details:        fmt.Sprintf("Announced SKU sale started: %s (%s)", curr.Name, curr.PlanCode),
			}
			saleStartedRecords = append(saleStartedRecords, rec)
		} else if prev.ListingState == "presale" && curr.ListingState == "listed" {
			curr.Baseline = false
			curr.ListingState = "listed"
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
		} else if curr.ListingState == "presale" && curr.PresaleSince == "" {
			curr.PresaleSince = nonEmpty(prev.PresaleSince, curr.FirstSeen)
		} else if prev.ListingState == "listed" && curr.ListingState == "announced" {
			// Legacy baseline entries lacked "announced"; allow baseline entries without ListedAt to settle into "announced" silently.
			if !prev.Baseline && prev.ListedAt != "" && len(prev.Configs) > 0 {
				curr.ListingState = "listed"
			}
		}

		if rules.SKUTrackConfigs && len(prev.Configs) > 0 && len(curr.Configs) > 0 {
			addedCfgs, removedCfgs := diffStringSets(prev.Configs, curr.Configs)
			// Guard against partial availability payload wiping configs.
			if len(removedCfgs) > 0 && float64(len(removedCfgs))/float64(len(prev.Configs)) > 0.50 {
				removedCfgs = nil
			}
			if len(addedCfgs) > 0 || len(removedCfgs) > 0 {
				var detailParts []string
				if len(addedCfgs) > 0 {
					detailParts = append(detailParts, "+"+strings.Join(addedCfgs, ", "))
				}
				if len(removedCfgs) > 0 {
					detailParts = append(detailParts, "-"+strings.Join(removedCfgs, ", "))
				}
				configRecords = append(configRecords, SKUChangeRecord{
					ID:             newShortID(),
					Type:           "config_changed",
					ListingState:   curr.ListingState,
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
		} else if len(curr.Configs) == 0 && len(prev.Configs) > 0 && curr.ListingState != "announced" {
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

	// Process missing SKUs:
	// 1. If a missing SKU matches a newly-added SKU's product line (Name + CPUText/Range) in the same cycle,
	//    pair them into a single "replaced" record (e.g. 24sys022 -> 24sys02-v3).
	// 2. Otherwise, require 2 consecutive missing checks (scheduling a fast 20s recheck on MissCount == 1).
	for k, prev := range prevSKUs {
		if _, present := currSKUs[k]; present {
			continue
		}

		matchedAddedIdx := -1
		for idx, addedRec := range addedRecords {
			if addedRec.SaleStarted {
				continue
			}
			if isReplacedProductLineMatch(addedRec, prev) {
				matchedAddedIdx = idx
				break
			}
		}

		if matchedAddedIdx >= 0 {
			delete(missingCounts, k)
			addedRec := addedRecords[matchedAddedIdx]
			addedRecords = append(addedRecords[:matchedAddedIdx], addedRecords[matchedAddedIdx+1:]...)

			addedKey := strings.ToLower(nonEmpty(addedRec.SKUKey, addedRec.PlanCode))
			if tracked, ok := nextSKUs[addedKey]; ok {
				tracked.ReplacedFrom = prev.PlanCode
				nextSKUs[addedKey] = tracked
			}

			repRec := SKUChangeRecord{
				ID:             newShortID(),
				Type:           "replaced",
				ListingState:   addedRec.ListingState,
				ReplacedFrom:   prev.PlanCode,
				Generation:     addedRec.Generation,
				SKUKey:         addedRec.SKUKey,
				PlanCode:       addedRec.PlanCode,
				PlanCodes:      addedRec.PlanCodes,
				Name:           addedRec.Name,
				Range:          nonEmpty(addedRec.Range, prev.Range),
				Endpoint:       ep,
				Timestamp:      nowUTC,
				Price:          addedRec.Price,
				Currency:       addedRec.Currency,
				CPUText:        nonEmpty(addedRec.CPUText, prev.CPUText),
				MemorySummary:  addedRec.MemorySummary,
				StorageSummary: addedRec.StorageSummary,
				StockState:     addedRec.StockState,
				Details:        fmt.Sprintf("SKU planCode updated: %s (%s -> %s)", addedRec.Name, prev.PlanCode, addedRec.PlanCode),
			}
			replacedRecords = append(replacedRecords, repRec)
			continue
		}

		missingCounts[k]++
		if missingCounts[k] >= 2 {
			delete(missingCounts, k)
			rec := SKUChangeRecord{
				ID:             newShortID(),
				Type:           "removed",
				ListingState:   prev.ListingState,
				Generation:     prev.Generation,
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
		} else {
			// Retain until confirmed missing on the 20s fast recheck (or next cycle).
			nextSKUs[k] = prev
		}
	}

	if len(missingCounts) > 0 {
		s.scheduleSKUFastRecheck()
	} else {
		s.cancelSKUFastRecheck()
	}

	allAddedRecords := make([]SKUChangeRecord, 0, len(addedRecords)+len(saleStartedRecords))
	allAddedRecords = append(allAddedRecords, addedRecords...)
	allAddedRecords = append(allAddedRecords, saleStartedRecords...)

	newChanges := make([]SKUChangeRecord, 0, len(allAddedRecords)+len(replacedRecords)+len(removedRecords)+len(configRecords))
	newChanges = append(newChanges, addedRecords...)
	newChanges = append(newChanges, saleStartedRecords...)
	newChanges = append(newChanges, replacedRecords...)
	newChanges = append(newChanges, removedRecords...)
	newChanges = append(newChanges, configRecords...)

	if len(newChanges) > 0 {
		combined := make([]SKUChangeRecord, 0, min(skuMaxChanges, len(newChanges)+len(history)))
		combined = append(combined, newChanges...)
		combined = append(combined, history...)
		if len(combined) > skuMaxChanges {
			combined = combined[:skuMaxChanges]
		}
		history = combined
	}

	s.updateCachedListedAtMap(nextSKUs)
	if err := s.saveSKUState(ctx, true, ep, nextSKUs, missingCounts, history); err != nil {
		s.setSKUError(err)
		return nil, err
	}

	configChangesCount := len(configRecords)
	if len(newChanges) > 0 {
		if s.log != nil {
			s.log.Info(logger.SourceSKUWatcher, "Detected public catalog SKU changes",
				"endpoint", ep,
				"added", len(addedRecords),
				"saleStarted", len(saleStartedRecords),
				"replaced", len(replacedRecords),
				"removed", len(removedRecords),
				"configChanges", configChangesCount,
			)
		}
		if s.notify != nil {
			detailsText := FormatSKUChangesNotification(newChanges, locale)
			_ = s.notify.EmitEvent(ctx, notify.EventSKUChanged, "", "", nil, notify.WithTemplateData(map[string]any{
				"preferTemplate":   true,
				"endpoint":         ep,
				"addedCount":       len(allAddedRecords),
				"newCount":         len(addedRecords),
				"saleStartedCount": len(saleStartedRecords),
				"replacedCount":    len(replacedRecords),
				"removedCount":     len(removedRecords),
				"changedCount":     configChangesCount,
				"details":          detailsText,
			}))
		}
	}

	return &SKUCheckResult{
		Status:           "ok",
		Endpoint:         ep,
		TotalSKUs:        len(nextSKUs),
		TrackedCount:     len(nextSKUs),
		AddedCount:       len(allAddedRecords),
		RemovedCount:     len(removedRecords),
		ReplacedCount:    len(replacedRecords),
		SaleStartedCount: len(saleStartedRecords),
		ConfigChanges:    configChangesCount,
		Added:            allAddedRecords,
		Removed:          removedRecords,
		Replaced:         replacedRecords,
		Changes:          newChanges,
	}, nil
}

// FormatSKUChangesNotification formats SKU change records grouped by section
// (🆕 New, 🚀 Sale Started, 🔄 Replaced (planCode updated), ➖ Discontinued)
// and grouped under each item's product line (Name | CPUText).
func FormatSKUChangesNotification(changes []SKUChangeRecord, locale string) string {
	var (
		newItems         []SKUChangeRecord
		saleStartedItems []SKUChangeRecord
		replacedItems    []SKUChangeRecord
		removedItems     []SKUChangeRecord
		configItems      []SKUChangeRecord
	)
	for _, ch := range changes {
		switch ch.Type {
		case "added":
			if ch.SaleStarted {
				saleStartedItems = append(saleStartedItems, ch)
			} else {
				newItems = append(newItems, ch)
			}
		case "replaced":
			replacedItems = append(replacedItems, ch)
		case "removed":
			removedItems = append(removedItems, ch)
		case "config_changed":
			configItems = append(configItems, ch)
		}
	}

	type sectionSpec struct {
		header string
		items  []SKUChangeRecord
		format func(SKUChangeRecord) string
	}

	priceSuffix := func(ch SKUChangeRecord) string {
		if ch.Price == nil || *ch.Price <= 0 {
			return ""
		}
		curr := nonEmpty(ch.Currency, "EUR")
		return fmt.Sprintf(" (%.2f %s)", *ch.Price, curr)
	}

	loc := notify.NormalizeLocale(locale)
	hdrNew := "🆕 New"
	hdrSale := "🚀 Sale Started"
	hdrReplaced := "🔄 Replaced (planCode updated)"
	hdrRemoved := "➖ Discontinued"
	hdrConfig := "⚙️ Configurations Updated"
	switch loc {
	case notify.LocaleZHCN:
		hdrNew = "🆕 新增 (New)"
		hdrSale = "🚀 开售 (Sale Started)"
		hdrReplaced = "🔄 换代 (Replaced: planCode updated)"
		hdrRemoved = "➖ 下架 (Discontinued)"
		hdrConfig = "⚙️ 配置更新 (Config Updated)"
	case notify.LocaleFR:
		hdrNew = "🆕 Nouveau (New)"
		hdrSale = "🚀 Ouverture des ventes (Sale Started)"
		hdrReplaced = "🔄 Remplacé (Replaced: planCode updated)"
		hdrRemoved = "➖ Retiré (Discontinued)"
		hdrConfig = "⚙️ Configurations mises à jour"
	}

	sections := []sectionSpec{
		{
			header: hdrNew,
			items:  newItems,
			format: func(ch SKUChangeRecord) string {
				stateTag := ""
				if ch.ListingState == "announced" || ch.ListingState == "presale" {
					stateTag = fmt.Sprintf(" [%s]", ch.ListingState)
				}
				return fmt.Sprintf("  • %s%s%s", ch.PlanCode, stateTag, priceSuffix(ch))
			},
		},
		{
			header: hdrSale,
			items:  saleStartedItems,
			format: func(ch SKUChangeRecord) string {
				return fmt.Sprintf("  • %s%s", ch.PlanCode, priceSuffix(ch))
			},
		},
		{
			header: hdrReplaced,
			items:  replacedItems,
			format: func(ch SKUChangeRecord) string {
				return fmt.Sprintf("  • %s → %s%s", ch.ReplacedFrom, ch.PlanCode, priceSuffix(ch))
			},
		},
		{
			header: hdrRemoved,
			items:  removedItems,
			format: func(ch SKUChangeRecord) string {
				return fmt.Sprintf("  • %s%s", ch.PlanCode, priceSuffix(ch))
			},
		},
		{
			header: hdrConfig,
			items:  configItems,
			format: func(ch SKUChangeRecord) string {
				return fmt.Sprintf("  • %s", nonEmpty(ch.Details, ch.PlanCode))
			},
		},
	}

	var blocks []string
	for _, sec := range sections {
		if len(sec.items) == 0 {
			continue
		}
		var sb strings.Builder
		sb.WriteString(sec.header)

		var lineOrder []string
		grouped := make(map[string][]SKUChangeRecord)
		for _, item := range sec.items {
			pl := formatProductLineHeader(item.Name, item.CPUText, item.PlanCode)
			if _, exists := grouped[pl]; !exists {
				lineOrder = append(lineOrder, pl)
			}
			grouped[pl] = append(grouped[pl], item)
		}
		for _, pl := range lineOrder {
			sb.WriteString("\n")
			sb.WriteString(pl)
			for _, item := range grouped[pl] {
				sb.WriteString("\n")
				sb.WriteString(sec.format(item))
			}
		}
		blocks = append(blocks, sb.String())
	}

	return strings.Join(blocks, "\n\n")
}

func formatProductLineHeader(name, cpuText, fallbackCode string) string {
	n := strings.TrimSpace(name)
	if n == "" {
		n = fallbackCode
	}
	cpu := strings.TrimSpace(cpuText)
	if cpu != "" && cpu != "-" && !strings.Contains(n, "|") {
		return fmt.Sprintf("%s | %s", n, cpu)
	}
	return n
}

func normalizeProductLineName(name string) string {
	n := strings.TrimSpace(name)
	if idx := strings.Index(n, "|"); idx >= 0 {
		n = strings.TrimSpace(n[:idx])
	}
	return strings.ToUpper(n)
}

func normalizeCPUTextForMatch(cpu string) string {
	c := strings.TrimSpace(cpu)
	if idx := strings.Index(c, "·"); idx >= 0 {
		c = strings.TrimSpace(c[:idx])
	}
	return strings.ToUpper(strings.Join(strings.Fields(c), " "))
}

func isReplacedProductLineMatch(added SKUChangeRecord, removed TrackedSKU) bool {
	if strings.EqualFold(added.PlanCode, removed.PlanCode) {
		return false
	}
	addName := normalizeProductLineName(added.Name)
	remName := normalizeProductLineName(removed.Name)
	if addName == "" || remName == "" || addName != remName {
		return false
	}
	if added.Range != "" && removed.Range != "" && !strings.EqualFold(added.Range, removed.Range) {
		return false
	}
	addCPU := normalizeCPUTextForMatch(added.CPUText)
	remCPU := normalizeCPUTextForMatch(removed.CPUText)
	if addCPU != "" && remCPU != "" && addCPU != remCPU {
		return false
	}
	return true
}

func pruneAndMigratePrevSKUs(
	prevSKUs map[string]TrackedSKU,
	missingCounts map[string]int,
	currSKUs map[string]TrackedSKU,
	dict *catalog.CatalogDictionary,
	availItems []catalog.RawAvailabilityItem,
	rules store.NotificationRules,
) {
	if dict == nil || len(dict.RealPlans) == 0 {
		return
	}
	availCodes := make(map[string]bool, len(availItems))
	for _, it := range availItems {
		pc := strings.ToLower(strings.TrimSpace(it.PlanCode))
		if pc != "" {
			availCodes[pc] = true
			if base, _ := catalog.ParsePlanRegion(pc); base != "" {
				availCodes[strings.ToLower(base)] = true
			}
		}
	}

	realLower := make(map[string]string, len(dict.RealPlans)*2)
	strippedVerToRealKey := make(map[string]string, len(dict.RealPlans))
	for pc := range dict.RealPlans {
		low := strings.ToLower(pc)
		realLower[low] = pc
		base, _ := catalog.ParsePlanRegion(pc)
		lowBase := strings.ToLower(base)
		realLower[lowBase] = base
		noVer := strings.ToLower(catalog.StripPlanVersion(pc))
		if noVer != "" && noVer != lowBase {
			targetKey := low
			if rules.SKUMergeRegional && lowBase != "" {
				targetKey = lowBase
			}
			strippedVerToRealKey[noVer] = targetKey
		}
	}

	type migration struct {
		oldKey string
		newKey string
		entry  TrackedSKU
	}
	var migrations []migration
	var toDelete []string

	for k, prev := range prevSKUs {
		if _, inCurr := currSKUs[k]; inCurr {
			continue
		}
		lowKey := strings.ToLower(strings.TrimSpace(k))
		lowPC := strings.ToLower(strings.TrimSpace(prev.PlanCode))

		// 1. Check if prev's PlanCodes contain an exact real planCode now keyed under its full versioned name in currSKUs.
		migratedViaPlanCodes := false
		for _, pc := range prev.PlanCodes {
			candKey := strings.ToLower(strings.TrimSpace(pc))
			if rules.SKUMergeRegional {
				if b, _ := catalog.ParsePlanRegion(pc); b != "" {
					candKey = strings.ToLower(b)
				}
			}
			if candKey != "" && candKey != k {
				if currEntry, ok := currSKUs[candKey]; ok {
					cp := prev
					cp.Key = candKey
					cp.PlanCode = currEntry.PlanCode
					migrations = append(migrations, migration{oldKey: k, newKey: candKey, entry: cp})
					migratedViaPlanCodes = true
					break
				}
			}
		}
		if migratedViaPlanCodes {
			continue
		}

		// 2. If neither in RealPlans nor in availCodes, check if it is an alias (in dict.Plans, port alias like -1g, or stripped -vN prefix).
		inReal := realLower[lowKey] != "" || realLower[lowPC] != ""
		inAvail := availCodes[lowKey] || availCodes[lowPC]
		if !inReal && !inAvail {
			_, inDictPlansKey := dict.Plans[k]
			_, inDictPlansPC := dict.Plans[prev.PlanCode]
			_, inDictPlansLow := dict.Plans[lowKey]
			targetRealKey := strippedVerToRealKey[lowKey]
			if targetRealKey == "" {
				targetRealKey = strippedVerToRealKey[lowPC]
			}
			isPortAlias := reAliasPortSuffix.MatchString(lowKey) || reAliasPortSuffix.MatchString(lowPC)
			if inDictPlansKey || inDictPlansPC || inDictPlansLow || targetRealKey != "" || isPortAlias {
				if targetRealKey != "" {
					if currEntry, ok := currSKUs[targetRealKey]; ok {
						cp := prev
						cp.Key = targetRealKey
						cp.PlanCode = currEntry.PlanCode
						migrations = append(migrations, migration{oldKey: k, newKey: targetRealKey, entry: cp})
						continue
					}
				}
				toDelete = append(toDelete, k)
			}
		}
	}

	for _, m := range migrations {
		delete(prevSKUs, m.oldKey)
		delete(missingCounts, m.oldKey)
		if _, exists := prevSKUs[m.newKey]; !exists {
			prevSKUs[m.newKey] = m.entry
		}
	}
	for _, k := range toDelete {
		delete(prevSKUs, k)
		delete(missingCounts, k)
	}
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

	availCodes := make(map[string]bool, len(availItems))
	availBaseCodes := make(map[string]bool, len(availItems))
	for _, it := range availItems {
		pc := strings.TrimSpace(it.PlanCode)
		if pc == "" || catalog.IsNonServerAddonCode(pc) {
			continue
		}
		availCodes[strings.ToLower(pc)] = true
		if base, _ := catalog.ParsePlanRegion(pc); base != "" {
			availBaseCodes[strings.ToLower(base)] = true
		}
	}

	sourcePlans := dict.RealPlans
	if len(sourcePlans) == 0 {
		sourcePlans = dict.Plans
	}

	out := make(map[string]TrackedSKU)
	for planCode, entry := range sourcePlans {
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

		hasAvail := availCodes[strings.ToLower(planCode)] || (rules.SKUMergeRegional && availBaseCodes[strings.ToLower(displayCode)])
		listingState := "announced"
		if hasAvail {
			listingState = "listed"
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
				ListingState:   listingState,
				Generation:     catalog.InferPlanGeneration(displayCode),
				CPUText:        cpuText,
				MemorySummary:  memSum,
				StorageSummary: stoSum,
				StockState:     stockState,
				Configs:        cfgs,
			}
			continue
		}

		if existing.ListingState == "listed" || listingState == "listed" {
			existing.ListingState = "listed"
		} else {
			existing.ListingState = "announced"
		}
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

	// Include availability-only SKUs not yet present in dict.RealPlans as presale SKUs.
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

// normalizeLoadedSKUs applies migration guards for legacy snapshots, repairs corrupted baseline
// entries against recorded change history, and returns true if any entry was modified.
func normalizeLoadedSKUs(skus map[string]TrackedSKU, history []SKUChangeRecord, initialized bool) bool {
	if len(skus) == 0 {
		return false
	}

	dirty := false

	// Normalize legacy field aliases (skuKey -> Key, firstSeenAt -> FirstSeen) first.
	for k, entry := range skus {
		changed := false
		if entry.Key == "" {
			entry.Key = nonEmpty(entry.SKUKey, k)
			changed = true
		}
		if entry.SKUKey != "" {
			entry.SKUKey = ""
			changed = true
		}
		if entry.FirstSeen == "" && entry.FirstSeenAt != "" {
			entry.FirstSeen = entry.FirstSeenAt
			changed = true
		}
		if entry.FirstSeenAt != "" {
			entry.FirstSeenAt = ""
			changed = true
		}
		if entry.PlanCode == "" {
			if len(entry.PlanCodes) > 0 && entry.PlanCodes[0] != "" {
				entry.PlanCode = entry.PlanCodes[0]
			} else {
				entry.PlanCode = entry.Key
			}
			changed = true
		}
		if changed {
			dirty = true
			skus[k] = entry
		}
	}

	genuinelyAdded := make(map[string]bool)
	markAdded := func(code string) {
		c := strings.ToLower(strings.TrimSpace(code))
		if c == "" {
			return
		}
		genuinelyAdded[c] = true
		if base, _ := catalog.ParsePlanRegion(c); base != "" {
			genuinelyAdded[strings.ToLower(base)] = true
		}
	}
	for _, rec := range history {
		if rec.Type == "added" || rec.Type == "replaced" || rec.SaleStarted {
			markAdded(rec.SKUKey)
			markAdded(rec.PlanCode)
			for _, pc := range rec.PlanCodes {
				markAdded(pc)
			}
		}
	}
	isEntryGenuinelyAdded := func(entry TrackedSKU) bool {
		if genuinelyAdded[strings.ToLower(strings.TrimSpace(entry.Key))] ||
			genuinelyAdded[strings.ToLower(strings.TrimSpace(entry.PlanCode))] {
			return true
		}
		for _, pc := range entry.PlanCodes {
			if genuinelyAdded[strings.ToLower(strings.TrimSpace(pc))] {
				return true
			}
		}
		return false
	}

	allUnmigratedSameTS := initialized
	var sharedFirstSeen string
	for _, entry := range skus {
		if entry.Baseline || entry.ListedAt != "" || entry.PresaleSince != "" || entry.FirstSeen == "" {
			allUnmigratedSameTS = false
		}
		if sharedFirstSeen == "" {
			sharedFirstSeen = entry.FirstSeen
		} else if entry.FirstSeen != sharedFirstSeen {
			allUnmigratedSameTS = false
		}
	}

	for k, entry := range skus {
		changed := false
		if allUnmigratedSameTS && !isEntryGenuinelyAdded(entry) {
			if !entry.Baseline || entry.ListedAt != "" {
				entry.Baseline = true
				entry.ListedAt = ""
				changed = true
			}
		}
		if entry.ListingState == "" {
			entry.ListingState = "listed"
			changed = true
		}
		if entry.Baseline {
			if !entry.SaleStarted && entry.ListedAt != "" {
				entry.ListedAt = ""
				changed = true
			}
		} else if entry.ListingState == "listed" && !entry.SaleStarted {
			if isEntryGenuinelyAdded(entry) {
				if entry.ListedAt == "" && entry.FirstSeen != "" {
					entry.ListedAt = entry.FirstSeen
					changed = true
				}
			} else if entry.ListedAt == "" || entry.ListedAt == entry.FirstSeen {
				entry.Baseline = true
				entry.ListedAt = ""
				changed = true
			}
		}
		if entry.Generation == 0 {
			if gen := catalog.InferPlanGeneration(entry.PlanCode); gen != 0 {
				entry.Generation = gen
				changed = true
			}
		}
		if changed {
			dirty = true
			skus[k] = entry
		}
	}

	return dirty
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
	s.ensureCatalogListedAtResolver()
	state, err := s.store.LoadSKUWatcherState(ctx)
	if err != nil {
		return nil, nil, err
	}
	settings, _ := s.loadAndMigrateSKUSettings(ctx)
	var rules store.NotificationRules
	if settings != nil {
		rules = settings.Rules
	}

	var changes []SKUChangeRecord
	_ = json.Unmarshal(state.ChangesJSON, &changes)
	if changes == nil {
		changes = []SKUChangeRecord{}
	}
	skus := make(map[string]TrackedSKU)
	_ = json.Unmarshal(state.SKUsJSON, &skus)
	if normalizeLoadedSKUs(skus, changes, state.Initialized) {
		missingCounts := make(map[string]int)
		_ = json.Unmarshal(state.MissingJSON, &missingCounts)
		_ = s.saveSKUState(ctx, state.Initialized, state.Endpoint, skus, missingCounts, changes)
	}
	s.updateCachedListedAtMap(skus)

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
	announcedSKUs := make(map[string]string)
	for _, entry := range skus {
		if entry.ListingState == "presale" {
			recordPlanCodeTimestamp(presaleSKUs, entry, nonEmpty(entry.PresaleSince, entry.FirstSeen))
			continue
		}
		if entry.ListingState == "announced" {
			recordPlanCodeTimestamp(announcedSKUs, entry, entry.FirstSeen)
		}
		listedTS := entry.ListedAt
		if listedTS == "" && !entry.Baseline && entry.FirstSeen != "" {
			listedTS = entry.FirstSeen
		}
		if listedTS == "" {
			continue
		}
		ts, parseErr := time.Parse(time.RFC3339Nano, listedTS)
		if parseErr != nil || !ts.After(cutoff) {
			continue
		}
		recordPlanCodeTimestamp(recentFirstSeen, entry, listedTS)
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
		AnnouncedSKUs:      announcedSKUs,
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
