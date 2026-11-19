package catalog

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"slices"
	"sort"
	"strings"
	"time"
)

// PromoDetail describes one promotion channel (either recurring "renew" or setup "installation")
// parsed directly from OVH's public catalog pricing promotions array.
type PromoDetail struct {
	Code                    string  `json:"code"`
	Description             string  `json:"description,omitempty"`
	Context                 string  `json:"context"`                           // e.g. "standard" (auto-applied) or "voucher"
	AutoApplied             bool    `json:"autoApplied"`                       // true when context == "standard"
	Capacity                string  `json:"capacity"`                          // "renew" or "installation"
	Type                    string  `json:"type,omitempty"`                    // e.g. "percentage", "amount"
	Value                   float64 `json:"value,omitempty"`                   // raw percentage or amount
	DiscountAmount          float64 `json:"discountAmount,omitempty"`          // concrete ex-VAT discount amount for this period/setup
	Duration                int     `json:"duration,omitempty"`                // promo duration in months (0 = unspecified / one-time)
	MinimumDuration         int     `json:"minimumDuration,omitempty"`         // minimum commitment/billing months required by OVH
	StartDate               string  `json:"startDate,omitempty"`               // RFC3339 start timestamp
	EndDate                 string  `json:"endDate,omitempty"`                 // RFC3339 expiration timestamp
	IsGlobalQuantityLimited bool    `json:"isGlobalQuantityLimited,omitempty"` // true if OVH caps total global redemptions
	GlobalQuantity          *int    `json:"globalQuantity,omitempty"`
	Quantity                *int    `json:"quantity,omitempty"`
}

// CatalogPromoCampaign aggregates one active OVH promotion campaign across all matching server plans.
type CatalogPromoCampaign struct {
	PromoCode               string   `json:"promoCode"`
	Context                 string   `json:"context"` // "standard" | "voucher"
	AutoApplied             bool     `json:"autoApplied"`
	Capacities              []string `json:"capacities"`              // ["renew"], ["installation"], or both
	SupportedModes          []string `json:"supportedModes"`          // e.g. ["default", "upfront12"]
	DiscountType            string   `json:"discountType,omitempty"`  // "percentage" | "amount"
	DiscountValue           float64  `json:"discountValue,omitempty"` // e.g. 20 for 20% off
	Badge                   string   `json:"badge"`                   // e.g. "-20%" | "FREE SETUP"
	Description             string   `json:"description,omitempty"`
	DurationMonths          int      `json:"durationMonths,omitempty"`
	MinimumDurationMonths   int      `json:"minimumDurationMonths,omitempty"`
	StartDate               string   `json:"startDate,omitempty"`
	EndDate                 string   `json:"endDate,omitempty"`
	RemainingHours          *float64 `json:"remainingHours,omitempty"`
	IsGlobalQuantityLimited bool     `json:"isGlobalQuantityLimited,omitempty"`
	GlobalQuantity          *int     `json:"globalQuantity,omitempty"`
	CatalogSources          []string `json:"catalogSources"`
	PlanCount               int      `json:"planCount"`
	InStockPlanCount        int      `json:"inStockPlanCount"`
	MaxSavingsAmount        float64  `json:"maxSavingsAmount"`
	Currency                string   `json:"currency"`
	PlanCodes               []string `json:"planCodes"`
	SamplePlans             []string `json:"samplePlans"`
}

// CatalogPromoPlan summarizes active promotions and pricing-mode coverage for a single bare-metal plan.
type CatalogPromoPlan struct {
	PlanCode             string            `json:"planCode"`
	Name                 string            `json:"name"`
	Range                string            `json:"range,omitempty"`
	CPU                  string            `json:"cpu,omitempty"`
	CatalogSource        string            `json:"catalogSource,omitempty"`
	ListingState         string            `json:"listingState"`
	Currency             string            `json:"currency"`
	StartingPrice        float64           `json:"startingPrice"`
	OriginalMonthlyPrice float64           `json:"originalMonthlyPrice"`
	SetupFee             float64           `json:"setupFee"`
	OriginalSetupFee     float64           `json:"originalSetupFee"`
	MonthlySavings       float64           `json:"monthlySavings"`
	SetupSavings         float64           `json:"setupSavings"`
	HasRecurringPromo    bool              `json:"hasRecurringPromo"`
	HasSetupPromo        bool              `json:"hasSetupPromo"`
	AutoApplied          bool              `json:"autoApplied"`
	PromoCodes           []string          `json:"promoCodes"`
	PromoBadge           string            `json:"promoBadge,omitempty"`
	PromoDescription     string            `json:"promoDescription,omitempty"`
	SupportedPromoModes  []string          `json:"supportedPromoModes"`
	MissedPromoModes     []string          `json:"missedPromoModes,omitempty"`
	AddonsDiscounted     bool              `json:"addonsDiscounted"`
	InStockDatacenters   []string          `json:"inStockDatacenters"`
	InStockCount         int               `json:"inStockCount"`
	PricingModes         []PlanPricingMode `json:"pricingModes"`
}

// CatalogPromotionsResponse is returned by GET /api/catalog/promotions and consumed by WebUI & MCP.
type CatalogPromotionsResponse struct {
	UpdatedAt          int64                  `json:"updatedAt"`
	Endpoint           string                 `json:"endpoint"`
	Zone               string                 `json:"zone"`
	Currency           string                 `json:"currency"`
	TotalCampaigns     int                    `json:"totalCampaigns"`
	TotalPromotedPlans int                    `json:"totalPromotedPlans"`
	InStockPlans       int                    `json:"inStockPromotedPlans"`
	AutoAppliedCodes   []string               `json:"autoAppliedCodes"`
	Campaigns          []CatalogPromoCampaign `json:"campaigns"`
	Plans              []CatalogPromoPlan     `json:"plans"`
}

// PromotionsQueryOptions configures filtering for Service.GetPromotions.
type PromotionsQueryOptions struct {
	AccountID    string
	Endpoint     string
	Zone         string
	PlanCode     string
	Range        string
	PromoType    string // "all" | "recurring" | "setup"
	OnlyInStock  bool
	ForceRefresh bool
}

// GetPromotions builds a structured, zero-extra-catalog-fetch promotion radar across all bare-metal plans.
func (s *Service) GetPromotions(ctx context.Context, opts PromotionsQueryOptions) (*CatalogPromotionsResponse, error) {
	ep, zn := s.resolveEndpointAndZone(ctx, opts.AccountID, opts.Endpoint, opts.Zone)
	dict, err := s.GetBaremetalDictionary(ctx, ep, zn, opts.ForceRefresh)
	if err != nil {
		return nil, err
	}

	// Best-effort live datacenter stock lookup (non-fatal if availability endpoint is slow/unavailable).
	inStockByPlan := make(map[string][]string)
	seenPlanInAvail := make(map[string]bool)
	if availItems, aErr := s.GetRawAvailabilities(ctx, ep, ""); aErr == nil {
		dcSets := make(map[string]map[string]struct{})
		for _, it := range availItems {
			pcLow := strings.ToLower(strings.TrimSpace(it.PlanCode))
			if pcLow == "" {
				continue
			}
			seenPlanInAvail[pcLow] = true
			for _, dc := range it.Datacenters {
				if dc.Availability == "" || strings.EqualFold(dc.Availability, "unavailable") || strings.EqualFold(dc.Availability, "unknown") {
					continue
				}
				normDC := NormalizeDisplayDatacenter(dc.Datacenter)
				if normDC == "" {
					continue
				}
				if dcSets[pcLow] == nil {
					dcSets[pcLow] = make(map[string]struct{})
				}
				dcSets[pcLow][normDC] = struct{}{}
			}
		}
		for pcLow, set := range dcSets {
			dcs := make([]string, 0, len(set))
			for dc := range set {
				dcs = append(dcs, dc)
			}
			sort.Strings(dcs)
			inStockByPlan[pcLow] = dcs
		}
	}

	planFilter := strings.ToLower(strings.TrimSpace(opts.PlanCode))
	rangeFilter := strings.ToUpper(strings.TrimSpace(opts.Range))
	promoType := strings.ToLower(strings.TrimSpace(opts.PromoType))
	if promoType == "" {
		promoType = "all"
	}

	type campAcc struct {
		camp         CatalogPromoCampaign
		capSet       map[string]struct{}
		modeSet      map[string]struct{}
		srcSet       map[string]struct{}
		planSet      map[string]struct{}
		inStockPlans map[string]struct{}
	}
	campMap := make(map[string]*campAcc)
	autoCodesSet := make(map[string]struct{})
	plansOut := make([]CatalogPromoPlan, 0, 32)
	now := time.Now()
	defaultCurr := nonEmpty(dict.Currency, "EUR")

	// Deterministic iteration over canonical bare-metal plans.
	planKeys := make([]string, 0, len(dict.RealPlans))
	for pc := range dict.RealPlans {
		planKeys = append(planKeys, pc)
	}
	sort.Strings(planKeys)

	for _, pc := range planKeys {
		entry := dict.RealPlans[pc]
		if len(entry.PricingModes) == 0 {
			continue
		}

		// Record all auto-applied codes globally BEFORE applying user filters so AutoAppliedCodes is always complete.
		for _, m := range entry.PricingModes {
			for _, pd := range []*PromoDetail{m.RenewPromo, m.SetupPromo} {
				if pd != nil && pd.Code != "" && pd.AutoApplied {
					autoCodesSet[pd.Code] = struct{}{}
				}
			}
			if m.HasPromo && m.PromoCode != "" && (m.AutoApplied || m.PromoContext == "" || strings.EqualFold(m.PromoContext, "standard")) {
				autoCodesSet[m.PromoCode] = struct{}{}
			}
		}

		// Check if this plan has any active promo at all.
		hasRecurring := false
		hasSetup := false
		hasAnyAuto := false
		supportedPromoModes := make([]string, 0, len(entry.PricingModes))
		missedPromoModes := make([]string, 0, len(entry.PricingModes))
		planPromoCodesSet := make(map[string]struct{})
		planPromoCodes := make([]string, 0, 2)

		addPlanPromoCode := func(code string) {
			code = strings.TrimSpace(code)
			if code == "" {
				return
			}
			if _, ok := planPromoCodesSet[code]; !ok {
				planPromoCodesSet[code] = struct{}{}
				planPromoCodes = append(planPromoCodes, code)
			}
		}

		for _, m := range entry.PricingModes {
			modeHasRec := m.RenewPromo != nil || (m.OriginalPeriodPrice > 0 && m.PeriodPrice < m.OriginalPeriodPrice)
			modeHasSetup := m.SetupPromo != nil || (m.OriginalSetupFee > 0 && m.SetupFee < m.OriginalSetupFee)
			if modeHasRec {
				hasRecurring = true
			}
			if modeHasSetup {
				hasSetup = true
			}
			if m.HasPromo || modeHasRec || modeHasSetup {
				supportedPromoModes = append(supportedPromoModes, m.Mode)
				if m.RenewPromo != nil {
					addPlanPromoCode(m.RenewPromo.Code)
					if m.RenewPromo.AutoApplied {
						hasAnyAuto = true
					}
				}
				if m.SetupPromo != nil {
					addPlanPromoCode(m.SetupPromo.Code)
					if m.SetupPromo.AutoApplied {
						hasAnyAuto = true
					}
				}
				if len(planPromoCodes) == 0 && m.PromoCode != "" {
					addPlanPromoCode(m.PromoCode)
					if m.AutoApplied || m.PromoContext == "" || strings.EqualFold(m.PromoContext, "standard") {
						hasAnyAuto = true
					}
				}
			} else {
				missedPromoModes = append(missedPromoModes, m.Mode)
			}
		}

		if !hasRecurring && !hasSetup {
			continue
		}
		if promoType == "recurring" && !hasRecurring {
			continue
		}
		if promoType == "setup" && !hasSetup {
			continue
		}
		if planFilter != "" &&
			!strings.Contains(strings.ToLower(pc), planFilter) &&
			!strings.Contains(strings.ToLower(entry.Name), planFilter) {
			continue
		}
		if rangeFilter != "" && rangeFilter != "ALL" {
			if rangeFilter == "ECO" {
				if entry.CatalogSource != "eco" && entry.Range != "KS" && entry.Range != "SYS" && entry.Range != "RISE" {
					continue
				}
			} else if !strings.EqualFold(entry.Range, rangeFilter) {
				continue
			}
		}

		pcLow := strings.ToLower(pc)
		inStockDCs := inStockByPlan[pcLow]
		if inStockDCs == nil {
			inStockDCs = []string{}
		}
		if opts.OnlyInStock && len(inStockDCs) == 0 {
			continue
		}

		listingState := "listed"
		if len(seenPlanInAvail) > 0 && !seenPlanInAvail[pcLow] {
			listingState = "announced"
		}

		// Check if any hardware upgrade addon on this plan carries its own catalog promotion.
		addonsDiscounted := false
		for _, am := range entry.AddonModePrices {
			for _, m := range am {
				if m.HasPromo {
					addonsDiscounted = true
					break
				}
			}
			if addonsDiscounted {
				break
			}
		}

		planSum := summarizePricingModes(entry.PricingModes)
		monthlySavings := math.Max(0, roundCents(planSum.OriginalMonthlyPrice-planSum.MonthlyPrice))
		setupSavings := math.Max(0, roundCents(planSum.OriginalSetupFee-planSum.SetupFee))
		planCurr := nonEmpty(entry.Currency, defaultCurr)

		// Aggregate into campaign buckets.
		for _, m := range entry.PricingModes {
			details := make([]*PromoDetail, 0, 2)
			if m.RenewPromo != nil {
				details = append(details, m.RenewPromo)
			}
			if m.SetupPromo != nil {
				details = append(details, m.SetupPromo)
			}
			// Fallback if loaded from a legacy cache entry lacking RenewPromo/SetupPromo pointers.
			if len(details) == 0 && m.HasPromo && m.PromoCode != "" {
				capKind := "renew"
				savings := math.Max(0, roundCents(m.OriginalPeriodPrice-m.PeriodPrice))
				if savings <= 0 && m.OriginalSetupFee > m.SetupFee {
					capKind = "installation"
					savings = math.Max(0, roundCents(m.OriginalSetupFee-m.SetupFee))
				}
				pCtx, autoApp := normalizePromoContext(m.PromoContext)
				details = append(details, &PromoDetail{
					Code:            m.PromoCode,
					Description:     m.PromoDescription,
					Context:         pCtx,
					AutoApplied:     autoApp,
					Capacity:        capKind,
					Type:            m.PromoType,
					Value:           m.PromoValue,
					DiscountAmount:  savings,
					Duration:        m.PromoDuration,
					MinimumDuration: m.PromoMinimumDuration,
					StartDate:       m.PromoStartDate,
					EndDate:         m.PromoEndDate,
				})
			}

			for _, pd := range details {
				if pd == nil || strings.TrimSpace(pd.Code) == "" {
					continue
				}
				if promoType == "recurring" && pd.Capacity != "renew" {
					continue
				}
				if promoType == "setup" && pd.Capacity != "installation" {
					continue
				}
				acc, ok := campMap[pd.Code]
				if !ok {
					badge := formatPromoDetailBadge(pd)
					var remHours *float64
					if pd.EndDate != "" {
						if tEnd, parseErr := time.Parse(time.RFC3339, pd.EndDate); parseErr == nil {
							h := math.Round(tEnd.Sub(now).Hours()*10) / 10
							remHours = &h
						}
					}
					acc = &campAcc{
						camp: CatalogPromoCampaign{
							PromoCode:               pd.Code,
							Context:                 pd.Context,
							AutoApplied:             pd.AutoApplied,
							DiscountType:            pd.Type,
							DiscountValue:           pd.Value,
							Badge:                   badge,
							Description:             pd.Description,
							DurationMonths:          pd.Duration,
							MinimumDurationMonths:   pd.MinimumDuration,
							StartDate:               pd.StartDate,
							EndDate:                 pd.EndDate,
							RemainingHours:          remHours,
							IsGlobalQuantityLimited: pd.IsGlobalQuantityLimited,
							GlobalQuantity:          pd.GlobalQuantity,
							Currency:                planCurr,
						},
						capSet:       make(map[string]struct{}),
						modeSet:      make(map[string]struct{}),
						srcSet:       make(map[string]struct{}),
						planSet:      make(map[string]struct{}),
						inStockPlans: make(map[string]struct{}),
					}
					campMap[pd.Code] = acc
				}
				if pd.Capacity != "" {
					acc.capSet[pd.Capacity] = struct{}{}
				}
				if m.Mode != "" {
					acc.modeSet[m.Mode] = struct{}{}
				}
				if entry.CatalogSource != "" {
					acc.srcSet[entry.CatalogSource] = struct{}{}
				}
				acc.planSet[pc] = struct{}{}
				if len(inStockDCs) > 0 {
					acc.inStockPlans[pc] = struct{}{}
				}
				if pd.DiscountAmount > acc.camp.MaxSavingsAmount {
					acc.camp.MaxSavingsAmount = pd.DiscountAmount
				}
				if acc.camp.Description == "" && pd.Description != "" {
					acc.camp.Description = pd.Description
				}
				if acc.camp.EndDate == "" && pd.EndDate != "" {
					acc.camp.EndDate = pd.EndDate
				}
				if acc.camp.DurationMonths == 0 && pd.Duration > 0 {
					acc.camp.DurationMonths = pd.Duration
				}
				if acc.camp.MinimumDurationMonths == 0 && pd.MinimumDuration > 0 {
					acc.camp.MinimumDurationMonths = pd.MinimumDuration
				}
			}
		}

		plansOut = append(plansOut, CatalogPromoPlan{
			PlanCode:             pc,
			Name:                 entry.Name,
			Range:                entry.Range,
			CPU:                  FormatCPU(entry.CPU, "en"),
			CatalogSource:        entry.CatalogSource,
			ListingState:         listingState,
			Currency:             planCurr,
			StartingPrice:        planSum.MonthlyPrice,
			OriginalMonthlyPrice: planSum.OriginalMonthlyPrice,
			SetupFee:             planSum.SetupFee,
			OriginalSetupFee:     planSum.OriginalSetupFee,
			MonthlySavings:       monthlySavings,
			SetupSavings:         setupSavings,
			HasRecurringPromo:    hasRecurring,
			HasSetupPromo:        hasSetup,
			AutoApplied:          hasAnyAuto,
			PromoCodes:           planPromoCodes,
			PromoBadge:           planSum.PromoBadge,
			PromoDescription:     planSum.PromoDescription,
			SupportedPromoModes:  supportedPromoModes,
			MissedPromoModes:     missedPromoModes,
			AddonsDiscounted:     addonsDiscounted,
			InStockDatacenters:   inStockDCs,
			InStockCount:         len(inStockDCs),
			PricingModes:         copyPricingModesSlice(entry.PricingModes),
		})
	}

	campaigns := make([]CatalogPromoCampaign, 0, len(campMap))
	for _, acc := range campMap {
		c := acc.camp
		c.Capacities = sortedMapKeys(acc.capSet)
		c.SupportedModes = sortModesList(sortedMapKeys(acc.modeSet))
		c.CatalogSources = sortedMapKeys(acc.srcSet)
		c.PlanCodes = sortedMapKeys(acc.planSet)
		c.PlanCount = len(c.PlanCodes)
		c.InStockPlanCount = len(acc.inStockPlans)
		if len(c.PlanCodes) > 6 {
			c.SamplePlans = append([]string(nil), c.PlanCodes[:6]...)
		} else {
			c.SamplePlans = append([]string(nil), c.PlanCodes...)
		}
		campaigns = append(campaigns, c)
	}

	// Sort campaigns: recurring percentage discounts first, then highest discount value / savings, then code.
	slices.SortFunc(campaigns, func(a, b CatalogPromoCampaign) int {
		aRec := slices.Contains(a.Capacities, "renew")
		bRec := slices.Contains(b.Capacities, "renew")
		if aRec != bRec {
			if aRec {
				return -1
			}
			return 1
		}
		if a.InStockPlanCount != b.InStockPlanCount {
			if a.InStockPlanCount > b.InStockPlanCount {
				return -1
			}
			return 1
		}
		if a.MaxSavingsAmount != b.MaxSavingsAmount {
			if a.MaxSavingsAmount > b.MaxSavingsAmount {
				return -1
			}
			return 1
		}
		return strings.Compare(a.PromoCode, b.PromoCode)
	})

	// Sort promoted plans: in-stock first, recurring promos before setup-only, then starting price ascending.
	slices.SortFunc(plansOut, func(a, b CatalogPromoPlan) int {
		aStock := a.InStockCount > 0
		bStock := b.InStockCount > 0
		if aStock != bStock {
			if aStock {
				return -1
			}
			return 1
		}
		if a.HasRecurringPromo != b.HasRecurringPromo {
			if a.HasRecurringPromo {
				return -1
			}
			return 1
		}
		if a.StartingPrice != b.StartingPrice {
			if a.StartingPrice < b.StartingPrice {
				return -1
			}
			return 1
		}
		return strings.Compare(a.PlanCode, b.PlanCode)
	})

	autoCodes := sortedMapKeys(autoCodesSet)
	inStockPlanCount := 0
	for _, p := range plansOut {
		if p.InStockCount > 0 {
			inStockPlanCount++
		}
	}

	return &CatalogPromotionsResponse{
		UpdatedAt:          dict.UpdatedAt,
		Endpoint:           ep,
		Zone:               zn,
		Currency:           defaultCurr,
		TotalCampaigns:     len(campaigns),
		TotalPromotedPlans: len(plansOut),
		InStockPlans:       inStockPlanCount,
		AutoAppliedCodes:   autoCodes,
		Campaigns:          campaigns,
		Plans:              plansOut,
	}, nil
}

// LookupAutoAppliedPromo checks whether code matches an active OVH auto-applied catalog campaign (context=="standard").
// Used to warn users/agents when they accidentally enter an internal campaign ID into the manual cart Coupon box.
func (s *Service) LookupAutoAppliedPromo(ctx context.Context, endpoint, zone, code string) (*CatalogPromoCampaign, bool) {
	trimmed := strings.TrimSpace(code)
	if trimmed == "" {
		return nil, false
	}
	res, err := s.GetPromotions(ctx, PromotionsQueryOptions{
		Endpoint: endpoint,
		Zone:     zone,
	})
	if err != nil || res == nil {
		return nil, false
	}
	for i := range res.Campaigns {
		c := &res.Campaigns[i]
		if c.AutoApplied && strings.EqualFold(c.PromoCode, trimmed) {
			cp := *c
			return &cp, true
		}
	}
	return nil, false
}

// HandleGetPromotions handles GET /api/catalog/promotions.
func (s *Service) HandleGetPromotions(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	accountID := strings.TrimSpace(r.Header.Get("X-OVH-Account"))
	zoneParam := nonEmpty(q.Get("subsidiary"), q.Get("zone"))
	ep, zn := s.resolveEndpointAndZone(r.Context(), accountID, q.Get("endpoint"), zoneParam)

	res, err := s.GetPromotions(r.Context(), PromotionsQueryOptions{
		AccountID:    accountID,
		Endpoint:     ep,
		Zone:         zn,
		PlanCode:     q.Get("planCode"),
		Range:        q.Get("range"),
		PromoType:    q.Get("promoType"),
		OnlyInStock:  parseBoolQuery(q.Get("onlyInStock")),
		ForceRefresh: parseBoolQuery(q.Get("refresh")) || parseBoolQuery(q.Get("force")),
	})
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"error":   "Catalog promotions unavailable",
			"message": err.Error(),
		})
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func formatPromoDetailBadge(pd *PromoDetail) string {
	if pd == nil {
		return ""
	}
	if pd.Capacity == "installation" {
		if (strings.EqualFold(pd.Type, "percentage") || pd.Type == "") && pd.Value >= 100 {
			return "FREE SETUP"
		}
		if (strings.EqualFold(pd.Type, "percentage") || pd.Type == "") && pd.Value > 0 && pd.Value < 100 {
			return fmt.Sprintf("-%g%% SETUP", math.Round(pd.Value*10)/10)
		}
		return "SETUP PROMO"
	}
	if (strings.EqualFold(pd.Type, "percentage") || pd.Type == "") && pd.Value > 0 && pd.Value <= 100 {
		return fmt.Sprintf("-%g%%", math.Round(pd.Value*10)/10)
	}
	return "PROMO"
}

func sortedMapKeys(m map[string]struct{}) []string {
	if len(m) == 0 {
		return []string{}
	}
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func sortModesList(modes []string) []string {
	slices.SortFunc(modes, func(a, b string) int {
		ra, rb := pricingModeOrderRank(a), pricingModeOrderRank(b)
		if ra != rb {
			return ra - rb
		}
		return strings.Compare(a, b)
	})
	return modes
}
