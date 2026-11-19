package catalog

import (
	"compress/gzip"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// Minimal strongly-typed structs for streaming OVH /1.0/order/catalog/public/{eco,baremetalServers,vps}
// responses directly via json.NewDecoder without allocating hundreds of MB of map[string]any trees.

type rawCatalogPayload struct {
	Locale   rawCatalogLocale    `json:"locale"`
	Plans    []rawCatalogPlan    `json:"plans"`
	Addons   []rawCatalogAddon   `json:"addons"`
	Products []rawCatalogProduct `json:"products"`
}

type rawCatalogLocale struct {
	CurrencyCode string `json:"currencyCode"`
}

type rawCatalogPlan struct {
	PlanCode      string                  `json:"planCode"`
	Product       string                  `json:"product"`
	InvoiceName   string                  `json:"invoiceName"`
	Description   string                  `json:"description"`
	Blobs         *rawCatalogBlobs        `json:"blobs"`
	Pricings      []rawCatalogPricing     `json:"pricings"`
	AddonFamilies []rawCatalogAddonFamily `json:"addonFamilies"`
}

type rawCatalogAddon struct {
	PlanCode    string              `json:"planCode"`
	Product     string              `json:"product"`
	InvoiceName string              `json:"invoiceName"`
	Pricings    []rawCatalogPricing `json:"pricings"`
}

type rawCatalogAddonFamily struct {
	Name    string   `json:"name"`
	Default string   `json:"default"`
	Addons  []string `json:"addons"`
}

type rawCatalogPricing struct {
	Mode        string                `json:"mode"`
	PricingMode string                `json:"pricingMode"`
	Capacities  []string              `json:"capacities"`
	Price       float64               `json:"price"`
	Interval    int                   `json:"interval"`
	Commitment  int                   `json:"commitment"`
	Promotions  []rawCatalogPromotion `json:"promotions"`
}

type rawCatalogPromotion struct {
	Name                    string                 `json:"name"`
	Code                    string                 `json:"code"`
	Description             string                 `json:"description"`
	Context                 string                 `json:"context"`
	Type                    string                 `json:"type"`
	Value                   float64                `json:"value"`
	Duration                *int                   `json:"duration"`
	MinimumDuration         int                    `json:"minimumDuration"`
	StartDate               string                 `json:"startDate"`
	EndDate                 string                 `json:"endDate"`
	IsGlobalQuantityLimited bool                   `json:"isGlobalQuantityLimited"`
	GlobalQuantity          *int                   `json:"globalQuantity"`
	Quantity                *int                   `json:"quantity"`
	Total                   *rawCatalogPriceAmount `json:"total"`
	Discount                *rawCatalogPriceAmount `json:"discount"`
}

type rawCatalogPriceAmount struct {
	Value          *float64 `json:"value"`
	FormattedValue string   `json:"formattedValue"`
	Tax            *float64 `json:"tax"`
}

type rawCatalogProduct struct {
	Name  string           `json:"name"`
	Blobs *rawCatalogBlobs `json:"blobs"`
}

type rawCatalogBlobs struct {
	Commercial *rawCatalogCommercial `json:"commercial"`
	Technical  *rawCatalogTechnical  `json:"technical"`
}

type rawCatalogCommercial struct {
	Name  string `json:"name"`
	Range string `json:"range"`
}

type rawCatalogTechnical struct {
	Server  *rawCatalogServerTech `json:"server"`
	Memory  *rawCatalogMemoryTech `json:"memory"`
	Storage json.RawMessage       `json:"storage"`
	GPU     json.RawMessage       `json:"gpu"`
}

type rawCatalogServerTech struct {
	Range string             `json:"range"`
	CPU   *rawCatalogCPUTech `json:"cpu"`
}

type rawCatalogCPUTech struct {
	Brand     string  `json:"brand"`
	Model     string  `json:"model"`
	Cores     int     `json:"cores"`
	Threads   int     `json:"threads"`
	Frequency float64 `json:"frequency"`
	Boost     float64 `json:"boost"`
}

type rawCatalogMemoryTech struct {
	Size      int    `json:"size"`
	RAMType   string `json:"ramType"`
	Frequency int    `json:"frequency"`
	ECC       *bool  `json:"ecc"`
}

// isHardwareOrNetworkAddonFamily keeps only hardware and network addon families,
// discarding software/OS/panel license families (e.g. application-license, distribution-license)
// that otherwise account for >50% of all plan addon references and bloat caches unnecessarily.
func isHardwareOrNetworkAddonFamily(family string) bool {
	fam := strings.ToLower(strings.TrimSpace(family))
	if fam == "" {
		return false
	}
	for _, skip := range []string{
		"license", "distribution", "application", "software", "panel",
		"security", "antivirus", "firewall", "veeam", "backup", "monitoring", "support",
	} {
		if strings.Contains(fam, skip) {
			return false
		}
	}
	return strings.Contains(fam, "memory") ||
		strings.Contains(fam, "ram") ||
		strings.Contains(fam, "storage") ||
		strings.Contains(fam, "disk") ||
		strings.Contains(fam, "drive") ||
		strings.Contains(fam, "gpu") ||
		strings.Contains(fam, "bandwidth") ||
		strings.Contains(fam, "traffic") ||
		strings.Contains(fam, "network") ||
		strings.Contains(fam, "vrack") ||
		strings.Contains(fam, "cpu") ||
		strings.Contains(fam, "processor")
}

// isHardwareOrNetworkAddonCode reports whether an addon planCode/productCode belongs to hardware or network options.
func isHardwareOrNetworkAddonCode(addonCode, productCode string) bool {
	if isLicenseOrOSOption(addonCode) || (productCode != "" && isLicenseOrOSOption(productCode)) {
		return false
	}
	for _, raw := range []string{addonCode, productCode} {
		low := strings.ToLower(strings.TrimSpace(raw))
		if low == "" {
			continue
		}
		for _, skip := range []string{
			"windows", "sql-", "sql_", "cpanel", "plesk", "cloudlinux",
			"veeam", "spla", "acronis", "directadmin", "ispconfig",
			"proxmox-license", "vmware", "hyperv", "redhat", "rhel",
			"suse", "ubuntu-pro", "backup-storage", "ftpbackup",
		} {
			if strings.Contains(low, skip) {
				return false
			}
		}
		if strings.HasPrefix(low, "ram-") ||
			strings.HasPrefix(low, "softraid-") ||
			strings.HasPrefix(low, "hardraid-") ||
			strings.HasPrefix(low, "hybridsoftraid-") ||
			strings.HasPrefix(low, "noraid-") ||
			strings.HasPrefix(low, "gpu-") ||
			strings.HasPrefix(low, "bandwidth-") ||
			strings.HasPrefix(low, "traffic-") ||
			strings.HasPrefix(low, "vrack-") ||
			strings.HasPrefix(low, "dedicated-bandwidth-") ||
			strings.Contains(low, "vrack-bandwidth") ||
			strings.Contains(low, "nvme") ||
			strings.Contains(low, "ssd") ||
			strings.Contains(low, "hdd") {
			return true
		}
	}
	return false
}

func rawMemoryToMetaMap(m *rawCatalogMemoryTech) map[string]any {
	if m == nil {
		return nil
	}
	out := map[string]any{
		"size":      m.Size,
		"ramType":   m.RAMType,
		"frequency": m.Frequency,
	}
	if m.ECC != nil {
		out["ecc"] = *m.ECC
	}
	return out
}

func normalizePromoContext(rawCtx string) (ctx string, autoApplied bool) {
	c := strings.ToLower(strings.TrimSpace(rawCtx))
	if c == "" {
		c = "standard"
	}
	return c, c == "standard"
}

func computePromoPriceAndDetail(
	capacity string,
	originalPrice float64,
	p rawCatalogPromotion,
) (finalPrice float64, detail *PromoDetail, active bool) {
	finalPrice = originalPrice
	code := nonEmpty(strings.TrimSpace(p.Name), strings.TrimSpace(p.Code))
	desc := strings.TrimSpace(p.Description)
	pType := strings.TrimSpace(p.Type)
	pCtx, autoApplied := normalizePromoContext(p.Context)
	dur := 0
	if p.Duration != nil {
		dur = *p.Duration
	}

	if p.Total != nil && p.Total.Value != nil {
		finalPrice = math.Max(0, roundCents(*p.Total.Value/1e8))
	} else if p.Discount != nil && p.Discount.Value != nil {
		finalPrice = math.Max(0, roundCents(originalPrice-*p.Discount.Value/1e8))
	} else if (strings.EqualFold(pType, "percentage") || pType == "") && p.Value > 0 && p.Value <= 100 {
		finalPrice = math.Max(0, roundCents(originalPrice*(1.0-p.Value/100.0)))
	} else if p.Value > 1e5 {
		finalPrice = math.Max(0, roundCents(originalPrice-p.Value/1e8))
	} else if p.Value > 0 {
		finalPrice = math.Max(0, roundCents(originalPrice-p.Value))
	} else if capacity == "installation" &&
		(strings.Contains(strings.ToUpper(code), "FREESETUP") || strings.Contains(strings.ToUpper(desc), "FREE SETUP")) {
		finalPrice = 0
	}

	active = originalPrice > 0 && finalPrice < originalPrice
	if !active && code == "" {
		return finalPrice, nil, false
	}
	discountAmt := math.Max(0, roundCents(originalPrice-finalPrice))
	if !active {
		return finalPrice, nil, false
	}

	var globalQty, remQty *int
	if p.GlobalQuantity != nil {
		v := *p.GlobalQuantity
		globalQty = &v
	}
	if p.Quantity != nil {
		v := *p.Quantity
		remQty = &v
	}

	detail = &PromoDetail{
		Code:                    code,
		Description:             desc,
		Context:                 pCtx,
		AutoApplied:             autoApplied,
		Capacity:                capacity,
		Type:                    pType,
		Value:                   p.Value,
		DiscountAmount:          discountAmt,
		Duration:                dur,
		MinimumDuration:         p.MinimumDuration,
		StartDate:               strings.TrimSpace(p.StartDate),
		EndDate:                 strings.TrimSpace(p.EndDate),
		IsGlobalQuantityLimited: p.IsGlobalQuantityLimited,
		GlobalQuantity:          globalQty,
		Quantity:                remQty,
	}
	return finalPrice, detail, true
}

func buildPlanPricingModeFromParsed(
	mode string,
	intervalMonths, commitmentMonths int,
	originalPeriodPrice, periodPrice float64,
	renewPromo *PromoDetail,
	originalSetupFee, setupFee float64,
	setupPromo *PromoDetail,
) PlanPricingMode {
	switch strings.ToLower(mode) {
	case "upfront12":
		intervalMonths = 12
		if commitmentMonths == 0 {
			commitmentMonths = 12
		}
	case "upfront24":
		intervalMonths = 24
		if commitmentMonths == 0 {
			commitmentMonths = 24
		}
	case "degressivity12":
		if commitmentMonths == 0 {
			commitmentMonths = 12
		}
	case "degressivity24":
		if commitmentMonths == 0 {
			commitmentMonths = 24
		}
	}
	intervalMonths = max(1, intervalMonths)
	duration := DurationForModeAndInterval(mode, intervalMonths)

	originalMonthlyPrice := roundCents(originalPeriodPrice / float64(intervalMonths))
	monthlyEquivalentPrice := roundCents(periodPrice / float64(intervalMonths))
	hasRenewPromo := renewPromo != nil
	hasInstPromo := setupPromo != nil
	hasPromo := hasRenewPromo || hasInstPromo

	var renewBadge, instBadge string
	if hasRenewPromo {
		if (strings.EqualFold(renewPromo.Type, "percentage") || renewPromo.Type == "") && renewPromo.Value > 0 && renewPromo.Value <= 100 {
			renewBadge = fmt.Sprintf("-%g%%", math.Round(renewPromo.Value*10)/10)
		} else if originalPeriodPrice > 0 {
			pct := math.Round(((originalPeriodPrice - periodPrice) / originalPeriodPrice) * 100)
			if pct > 0 {
				renewBadge = fmt.Sprintf("-%g%%", pct)
			} else {
				renewBadge = "PROMO"
			}
		}
	}
	if hasInstPromo {
		if setupFee <= 0 {
			instBadge = "FREE SETUP"
		} else if (strings.EqualFold(setupPromo.Type, "percentage") || setupPromo.Type == "") && setupPromo.Value > 0 && setupPromo.Value <= 100 {
			instBadge = fmt.Sprintf("-%g%% SETUP", math.Round(setupPromo.Value*10)/10)
		} else {
			instBadge = "SETUP PROMO"
		}
	}

	promoBadge := ""
	if renewBadge != "" && instBadge != "" {
		promoBadge = renewBadge + " + " + instBadge
	} else if renewBadge != "" {
		promoBadge = renewBadge
	} else if instBadge != "" {
		promoBadge = instBadge
	}

	var promoCode, promoDesc, promoType, promoCtx, promoStart, promoEnd string
	var promoVal float64
	var promoDur, promoMinDur int
	autoApplied := false
	if hasRenewPromo {
		promoCode = renewPromo.Code
		promoDesc = renewPromo.Description
		if hasInstPromo && setupPromo.Description != "" && setupPromo.Description != renewPromo.Description {
			promoDesc = joinNonEmpty(" · ", renewPromo.Description, setupPromo.Description)
		}
		promoVal = renewPromo.Value
		promoType = renewPromo.Type
		promoDur = renewPromo.Duration
		promoMinDur = renewPromo.MinimumDuration
		promoCtx = renewPromo.Context
		autoApplied = renewPromo.AutoApplied
		promoStart = nonEmpty(renewPromo.StartDate, setupPromoStart(setupPromo))
		promoEnd = nonEmpty(renewPromo.EndDate, setupPromoEnd(setupPromo))
	} else if hasInstPromo {
		promoCode = setupPromo.Code
		promoDesc = setupPromo.Description
		promoVal = setupPromo.Value
		promoType = setupPromo.Type
		promoDur = setupPromo.Duration
		promoMinDur = setupPromo.MinimumDuration
		promoCtx = setupPromo.Context
		autoApplied = setupPromo.AutoApplied
		promoStart = setupPromo.StartDate
		promoEnd = setupPromo.EndDate
	}

	return PlanPricingMode{
		Mode:                   mode,
		Duration:               duration,
		IntervalMonths:         intervalMonths,
		CommitmentMonths:       commitmentMonths,
		PeriodPrice:            periodPrice,
		OriginalPeriodPrice:    originalPeriodPrice,
		MonthlyEquivalentPrice: monthlyEquivalentPrice,
		OriginalMonthlyPrice:   originalMonthlyPrice,
		SetupFee:               setupFee,
		OriginalSetupFee:       originalSetupFee,
		FreeSetup:              setupFee <= 0,
		HasPromo:               hasPromo,
		PromoCode:              promoCode,
		PromoBadge:             promoBadge,
		PromoDescription:       promoDesc,
		PromoValue:             promoVal,
		PromoType:              promoType,
		PromoDuration:          promoDur,
		PromoMinimumDuration:   promoMinDur,
		PromoContext:           promoCtx,
		AutoApplied:            autoApplied,
		PromoStartDate:         promoStart,
		PromoEndDate:           promoEnd,
		RenewPromo:             renewPromo,
		SetupPromo:             setupPromo,
	}
}

func setupPromoStart(p *PromoDetail) string {
	if p == nil {
		return ""
	}
	return p.StartDate
}

func setupPromoEnd(p *PromoDetail) string {
	if p == nil {
		return ""
	}
	return p.EndDate
}

// ParseTypedPlanPricingModes extracts structured billing modes, setup fees, and dual-channel promotions from typed pricings.
func ParseTypedPlanPricingModes(pricings []rawCatalogPricing) []PlanPricingMode {
	if len(pricings) == 0 {
		return nil
	}

	type typedModeBucket struct {
		mode       string
		renewEntry *rawCatalogPricing
		instEntry  *rawCatalogPricing
	}
	buckets := make(map[string]*typedModeBucket, 4)
	modeOrder := make([]string, 0, 4)

	for i := range pricings {
		pr := &pricings[i]
		mode := strings.TrimSpace(pr.Mode)
		if mode == "" {
			mode = strings.TrimSpace(pr.PricingMode)
		}
		if mode == "" {
			mode = "default"
		}
		isRenew := false
		isInst := false
		for _, capName := range pr.Capacities {
			switch strings.ToLower(strings.TrimSpace(capName)) {
			case "renew":
				isRenew = true
			case "installation":
				isInst = true
			}
		}
		if isRenew {
			isInst = false
		}
		if !isRenew && !isInst {
			continue
		}
		b, exists := buckets[mode]
		if !exists {
			b = &typedModeBucket{mode: mode}
			buckets[mode] = b
			modeOrder = append(modeOrder, mode)
		}
		if isRenew && b.renewEntry == nil {
			b.renewEntry = pr
		}
		if isInst && b.instEntry == nil {
			b.instEntry = pr
		}
	}

	out := make([]PlanPricingMode, 0, len(modeOrder))
	for _, mode := range modeOrder {
		b := buckets[mode]
		if b.renewEntry == nil && b.instEntry == nil {
			continue
		}

		originalSetupFee := 0.0
		setupFee := 0.0
		var setupPromo *PromoDetail
		if b.instEntry != nil {
			originalSetupFee = roundCents(b.instEntry.Price / 1e8)
			setupFee = originalSetupFee
			if len(b.instEntry.Promotions) > 0 {
				var active bool
				setupFee, setupPromo, active = computePromoPriceAndDetail("installation", originalSetupFee, b.instEntry.Promotions[0])
				if !active {
					setupPromo = nil
				}
			}
		}

		intervalMonths := 1
		commitmentMonths := 0
		originalPeriodPrice := 0.0
		periodPrice := 0.0
		var renewPromo *PromoDetail
		if b.renewEntry != nil {
			intervalMonths = max(1, b.renewEntry.Interval)
			commitmentMonths = max(0, b.renewEntry.Commitment)
			originalPeriodPrice = roundCents(b.renewEntry.Price / 1e8)
			periodPrice = originalPeriodPrice
			if len(b.renewEntry.Promotions) > 0 {
				var active bool
				periodPrice, renewPromo, active = computePromoPriceAndDetail("renew", originalPeriodPrice, b.renewEntry.Promotions[0])
				if !active {
					renewPromo = nil
				}
			}
		}

		out = append(out, buildPlanPricingModeFromParsed(
			mode,
			intervalMonths,
			commitmentMonths,
			originalPeriodPrice,
			periodPrice,
			renewPromo,
			originalSetupFee,
			setupFee,
			setupPromo,
		))
	}

	sortPricingModes(out)
	return out
}

func extractTypedMonthlyPriceFromModes(pricings []rawCatalogPricing, modes []PlanPricingMode) *float64 {
	if len(modes) > 0 {
		for _, m := range modes {
			if strings.EqualFold(m.Mode, "default") && m.MonthlyEquivalentPrice > 0 {
				v := m.MonthlyEquivalentPrice
				return &v
			}
		}
		if modes[0].MonthlyEquivalentPrice > 0 {
			v := modes[0].MonthlyEquivalentPrice
			return &v
		}
	}
	var renewFallback *float64
	for i := range pricings {
		pr := &pricings[i]
		mode := strings.TrimSpace(pr.Mode)
		isRenew := false
		for _, c := range pr.Capacities {
			if strings.EqualFold(c, "renew") {
				isRenew = true
				break
			}
		}
		if !isRenew {
			continue
		}
		val := roundCents(pr.Price / 1e8)
		if mode == "default" || mode == "" {
			return &val
		}
		if renewFallback == nil {
			v := val
			renewFallback = &v
		}
	}
	if renewFallback != nil {
		return renewFallback
	}
	for i := range pricings {
		if pricings[i].Price > 0 {
			val := roundCents(pricings[i].Price / 1e8)
			return &val
		}
	}
	return nil
}

// BuildDictionaryFromTypedCatalogs builds a deduplicated, hardware-filtered CatalogDictionary
// from strongly-typed streaming catalog payloads (zero 5x struct copies, zero software license bloat).
func BuildDictionaryFromTypedCatalogs(eco, baremetal, vps *rawCatalogPayload) *CatalogDictionary {
	type parsedAddonPricing struct {
		product string
		price   *float64
		modes   map[string]PlanPricingMode
	}

	productsByName := make(map[string]*rawCatalogProduct)
	addonsPriceByCode := make(map[string]parsedAddonPricing)
	realPlansMap := make(map[string]CatalogPlanEntry)
	extraPlansMap := make(map[string]CatalogPlanEntry)
	planAliases := make(map[string]string)
	globalAddonPrices := make(map[string]float64)
	globalAddonModePrices := make(map[string]map[string]PlanPricingMode)
	optionsMap := make(map[string]any)
	defaultCurrency := "EUR"

	type srcPair struct {
		source string
		data   *rawCatalogPayload
	}
	sources := []srcPair{
		{source: "eco", data: eco},
		{source: "baremetalServers", data: baremetal},
		{source: "vps", data: vps},
	}

	for _, src := range sources {
		cat := src.data
		if cat == nil {
			continue
		}
		if cc := strings.ToUpper(strings.TrimSpace(cat.Locale.CurrencyCode)); cc != "" {
			defaultCurrency = cc
		}
		for i := range cat.Addons {
			rawAddon := &cat.Addons[i]
			ac := strings.TrimSpace(rawAddon.PlanCode)
			if ac == "" {
				continue
			}
			prodCode := strings.TrimSpace(rawAddon.Product)
			if !isHardwareOrNetworkAddonCode(ac, prodCode) {
				continue
			}
			parsedModes := ParseTypedPlanPricingModes(rawAddon.Pricings)
			modesMap := make(map[string]PlanPricingMode, len(parsedModes))
			for _, m := range parsedModes {
				modesMap[m.Mode] = m
			}
			pr := extractTypedMonthlyPriceFromModes(rawAddon.Pricings, parsedModes)
			addonsPriceByCode[ac] = parsedAddonPricing{
				product: prodCode,
				price:   pr,
				modes:   modesMap,
			}
			if pr != nil && src.source != "vps" {
				globalAddonPrices[ac] = *pr
				if len(modesMap) > 0 {
					globalAddonModePrices[ac] = copyPricingModesMap(modesMap)
				}
				if prodCode != "" {
					cur, exists := globalAddonPrices[prodCode]
					if !exists || (cur == 0 && *pr > 0) {
						globalAddonPrices[prodCode] = *pr
						if len(modesMap) > 0 {
							globalAddonModePrices[prodCode] = copyPricingModesMap(modesMap)
						}
					}
				}
			}
		}

		for i := range cat.Products {
			prod := &cat.Products[i]
			pname := strings.TrimSpace(prod.Name)
			if pname == "" {
				continue
			}
			productsByName[pname] = prod
			if prod.Blobs == nil || prod.Blobs.Technical == nil {
				continue
			}
			tech := prod.Blobs.Technical
			if tech.Memory != nil {
				optionsMap[pname] = map[string]any{
					"kind":   "memory",
					"parsed": ParseMemoryCode(pname, rawMemoryToMetaMap(tech.Memory)),
				}
			} else if len(tech.Storage) > 0 && string(tech.Storage) != "null" ||
				strings.HasPrefix(pname, "softraid-") ||
				strings.HasPrefix(pname, "hardraid-") ||
				strings.HasPrefix(pname, "hybridsoftraid-") ||
				strings.HasPrefix(pname, "noraid-") {
				optionsMap[pname] = map[string]any{
					"kind":   "storage",
					"parsed": ParseStorageCode(pname, strings.HasSuffix(pname, "-system")),
				}
			} else if len(tech.GPU) > 0 && string(tech.GPU) != "null" || strings.HasPrefix(pname, "gpu-") {
				optionsMap[pname] = map[string]any{
					"kind":   "gpu",
					"parsed": ParseGPUCode(pname),
				}
			}
		}
	}

	registerAlias := func(alias, canonical string, isRegional bool) {
		alias = strings.TrimSpace(alias)
		if alias == "" || alias == canonical {
			return
		}
		if _, isReal := realPlansMap[alias]; isReal {
			return
		}
		if _, exists := planAliases[alias]; !exists || !isRegional {
			planAliases[alias] = canonical
		}
	}

	for _, src := range sources {
		cat := src.data
		if cat == nil {
			continue
		}
		catCurrency := defaultCurrency
		if cc := strings.ToUpper(strings.TrimSpace(cat.Locale.CurrencyCode)); cc != "" {
			catCurrency = cc
		}

		for i := range cat.Plans {
			plan := &cat.Plans[i]
			planCode := strings.TrimSpace(plan.PlanCode)
			if planCode == "" {
				continue
			}
			productKey := strings.TrimSpace(plan.Product)
			invoiceName := strings.TrimSpace(plan.InvoiceName)
			desc := strings.TrimSpace(plan.Description)
			if desc == "" {
				desc = invoiceName
			}
			commName := ""
			if plan.Blobs != nil && plan.Blobs.Commercial != nil {
				commName = strings.TrimSpace(plan.Blobs.Commercial.Name)
			}

			parts := splitPipeNonEmpty(invoiceName)
			displayName := commName
			if displayName == "" && len(parts) > 0 {
				displayName = parts[0]
			}
			if displayName == "" {
				displayName = planCode
			}
			invoiceCPU := ""
			if len(parts) > 1 {
				invoiceCPU = parts[1]
			}

			prodEntry := productsByName[productKey]
			if prodEntry == nil {
				prodEntry = productsByName[planCode]
			}
			var srvTech *rawCatalogServerTech
			if prodEntry != nil && prodEntry.Blobs != nil && prodEntry.Blobs.Technical != nil {
				srvTech = prodEntry.Blobs.Technical.Server
			}
			rawRange := ""
			if srvTech != nil {
				rawRange = strings.TrimSpace(srvTech.Range)
			}
			if src.source == "vps" && rawRange == "" {
				rawRange = "vps"
			}

			var cpuInfo *CPUInfo
			if (srvTech != nil && srvTech.CPU != nil && strings.TrimSpace(srvTech.CPU.Model) != "") || invoiceCPU != "" {
				brand := ""
				model := invoiceCPU
				cores, threads := 0, 0
				freq, boost := 0.0, 0.0
				if srvTech != nil && srvTech.CPU != nil {
					brand = strings.TrimSpace(srvTech.CPU.Brand)
					if m := strings.TrimSpace(srvTech.CPU.Model); m != "" {
						model = m
					}
					cores = srvTech.CPU.Cores
					threads = srvTech.CPU.Threads
					freq = srvTech.CPU.Frequency
					boost = srvTech.CPU.Boost
				}
				cpuInfo = &CPUInfo{
					Brand:     brand,
					Model:     model,
					Cores:     cores,
					Threads:   threads,
					Frequency: freq,
					Boost:     boost,
				}
			}

			normRange := ClassifyProductRange(displayName, rawRange, planCode)
			baseCode, regionInfo := ParsePlanRegion(planCode)
			noVerCode := StripPlanVersion(planCode)
			pricingModes := ParseTypedPlanPricingModes(plan.Pricings)
			planSum := summarizePricingModes(pricingModes)
			basePrice := extractTypedMonthlyPriceFromModes(plan.Pricings, pricingModes)
			basePriceVal := 0.0
			if basePrice != nil {
				basePriceVal = *basePrice
			}
			if basePrice != nil && src.source != "vps" {
				globalAddonPrices[planCode] = *basePrice
			}
			if len(pricingModes) > 0 && src.source != "vps" {
				pmMap := make(map[string]PlanPricingMode, len(pricingModes))
				for _, m := range pricingModes {
					pmMap[m.Mode] = m
				}
				globalAddonModePrices[planCode] = pmMap
			}

			addonPrices := make(map[string]float64)
			addonModePrices := make(map[string]map[string]PlanPricingMode)
			var defMemCode, defStoCode, defSysStoCode, defGPUCode, defBWStr, defVRackStr string
			var defaultAddonOpts []string
			defaultOpts := make([]ServerOption, 0)
			availOpts := make([]ServerOption, 0)
			hasMemory := false
			hasStorage := false

			for _, fam := range plan.AddonFamilies {
				famName := strings.ToLower(strings.TrimSpace(fam.Name))
				if !isHardwareOrNetworkAddonFamily(famName) {
					continue
				}
				defAddon := strings.TrimSpace(fam.Default)

				isMemFam := strings.Contains(famName, "memory") || strings.Contains(famName, "ram")
				isStoFam := (strings.Contains(famName, "storage") || strings.Contains(famName, "disk") || strings.Contains(famName, "drive")) && !strings.Contains(famName, "system")
				isSysStoFam := strings.Contains(famName, "system-storage") || strings.Contains(famName, "system_storage")
				isGPUFam := strings.Contains(famName, "gpu")
				if isMemFam {
					hasMemory = true
				}
				if isStoFam || isSysStoFam {
					hasStorage = true
				}

				for _, rawAC := range fam.Addons {
					rawAC = strings.TrimSpace(rawAC)
					if rawAC == "" || isLicenseOrOSOption(rawAC) {
						continue
					}
					ap, hasAP := addonsPriceByCode[rawAC]
					isDef := rawAC == defAddon
					prodKey := ap.product
					if prodKey == "" {
						prodKey = stripPlanAddonSuffix(rawAC, planCode, baseCode, noVerCode, productKey)
					}
					addonDelta := 0.0
					if hasAP && ap.price != nil {
						addonDelta = *ap.price
						addonPrices[rawAC] = *ap.price
						if prodKey != "" {
							if _, exists := addonPrices[prodKey]; !exists || isDef {
								addonPrices[prodKey] = *ap.price
							}
						}
					}
					if hasAP && len(ap.modes) > 0 {
						addonModePrices[rawAC] = copyPricingModesMap(ap.modes)
						if prodKey != "" {
							if _, exists := addonModePrices[prodKey]; !exists || isDef {
								addonModePrices[prodKey] = copyPricingModesMap(ap.modes)
							}
						}
					}
					estOptPrice := roundCents(basePriceVal + addonDelta)
					var optModes []PlanPricingMode
					if hasAP && len(ap.modes) > 0 {
						for _, m := range ap.modes {
							optModes = append(optModes, m)
						}
						sortPricingModes(optModes)
					}
					optSum := summarizePricingModes(optModes)
					opt := ServerOption{
						Label:                 rawAC,
						Value:                 rawAC,
						Family:                famName,
						IsDefault:             isDef,
						ConfigKey:             prodKey,
						Options:               []string{rawAC},
						EstimatedMonthlyPrice: estOptPrice,
						Price:                 addonDelta,
						OriginalPrice:         optSum.OriginalMonthlyPrice,
						OriginalMonthlyPrice:  optSum.OriginalMonthlyPrice,
						SetupFee:              optSum.SetupFee,
						OriginalSetupFee:      optSum.OriginalSetupFee,
						FreeSetup:             optSum.FreeSetup,
						HasPromo:              optSum.HasPromo,
						PromoCode:             optSum.PromoCode,
						PromoBadge:            optSum.PromoBadge,
						PromoDescription:      optSum.PromoDescription,
						PricingModes:          optModes,
						Currency:              catCurrency,
					}
					if isMemFam {
						opt.Memory = prodKey
					} else if isStoFam {
						opt.Storage = prodKey
					}
					availOpts = append(availOpts, opt)
					if isDef {
						defaultOpts = append(defaultOpts, opt)
					}
				}

				if defAddon != "" {
					ap := addonsPriceByCode[defAddon]
					prodKey := ap.product
					if prodKey == "" {
						prodKey = stripPlanAddonSuffix(defAddon, planCode, baseCode, noVerCode, productKey)
					}
					switch {
					case isMemFam && defMemCode == "":
						defMemCode = prodKey
						if !slices.Contains(defaultAddonOpts, defAddon) {
							defaultAddonOpts = append(defaultAddonOpts, defAddon)
						}
					case isStoFam && defStoCode == "":
						defStoCode = prodKey
						if !slices.Contains(defaultAddonOpts, defAddon) {
							defaultAddonOpts = append(defaultAddonOpts, defAddon)
						}
					case isSysStoFam && defSysStoCode == "":
						defSysStoCode = prodKey
					case isGPUFam && defGPUCode == "":
						defGPUCode = prodKey
					case strings.Contains(famName, "vrack") && defVRackStr == "":
						defVRackStr = FormatVRackCode(defAddon)
					case (strings.Contains(famName, "bandwidth") || strings.Contains(famName, "traffic") || strings.Contains(famName, "network")) && defBWStr == "":
						if strings.Contains(strings.ToLower(defAddon), "vrack") {
							defVRackStr = FormatVRackCode(defAddon)
						} else {
							defBWStr = FormatBandwidthCode(defAddon)
						}
					}
				}
			}

			prodName := productKey
			if prodName == "" {
				prodName = baseCode
			}
			defConfigKey := BuildConfigKey(defMemCode, defStoCode)

			entry := CatalogPlanEntry{
				PlanCode:             planCode,
				Name:                 displayName,
				Description:          desc,
				Product:              prodName,
				Range:                normRange,
				CPU:                  cpuInfo,
				Price:                basePrice,
				OriginalPrice:        planSum.OriginalMonthlyPrice,
				OriginalMonthlyPrice: planSum.OriginalMonthlyPrice,
				SetupFee:             planSum.SetupFee,
				OriginalSetupFee:     planSum.OriginalSetupFee,
				FreeSetup:            planSum.FreeSetup,
				HasPromo:             planSum.HasPromo,
				PromoCode:            planSum.PromoCode,
				PromoBadge:           planSum.PromoBadge,
				PromoDescription:     planSum.PromoDescription,
				PricingModes:         pricingModes,
				Currency:             catCurrency,
				AddonPrices:          addonPrices,
				AddonModePrices:      addonModePrices,
				CatalogSource:        src.source,
				DefaultSpecs: CatalogPlanSpecs{
					Memory:         defMemCode,
					Storage:        defStoCode,
					SystemStorage:  defSysStoCode,
					GPU:            defGPUCode,
					Bandwidth:      defBWStr,
					VRackBandwidth: defVRackStr,
				},
				DefaultMemory:       defMemCode,
				DefaultStorage:      defStoCode,
				DefaultConfigKey:    defConfigKey,
				DefaultAddonOptions: defaultAddonOpts,
				DefaultOptions:      defaultOpts,
				AvailableOptions:    availOpts,
			}

			isRealBaremetal := (src.source == "eco" || src.source == "baremetalServers") && !IsNonServerAddonCode(planCode) &&
				((hasMemory && hasStorage) || (!hasMemory && !hasStorage))
			if isRealBaremetal {
				realPlansMap[planCode] = entry
				delete(extraPlansMap, planCode)
			} else if _, alreadyReal := realPlansMap[planCode]; !alreadyReal {
				extraPlansMap[planCode] = entry
			}

			isRegional := regionInfo != nil && regionInfo.Code != ""
			registerAlias(baseCode, planCode, isRegional)
			registerAlias(noVerCode, planCode, isRegional)
			registerAlias(productKey, planCode, isRegional)
		}
	}

	return &CatalogDictionary{
		UpdatedAt:       time.Now().Unix(),
		Currency:        defaultCurrency,
		Plans:           extraPlansMap,
		RealPlans:       realPlansMap,
		PlanAliases:     planAliases,
		AddonPrices:     globalAddonPrices,
		AddonModePrices: globalAddonModePrices,
		Options:         optionsMap,
	}
}

// CanonicalPlanCode resolves key through RealPlans, Plans, and PlanAliases (case-insensitive).
func (d *CatalogDictionary) CanonicalPlanCode(key string) (string, bool) {
	if d == nil {
		return "", false
	}
	k := strings.TrimSpace(key)
	if k == "" {
		return "", false
	}
	if _, ok := d.RealPlans[k]; ok {
		return k, true
	}
	if _, ok := d.Plans[k]; ok {
		return k, true
	}
	if target, ok := d.PlanAliases[k]; ok && target != "" {
		return target, true
	}
	low := strings.ToLower(k)
	if _, ok := d.RealPlans[low]; ok {
		return low, true
	}
	if _, ok := d.Plans[low]; ok {
		return low, true
	}
	if target, ok := d.PlanAliases[low]; ok && target != "" {
		return target, true
	}
	for rk := range d.RealPlans {
		if strings.EqualFold(rk, low) {
			return rk, true
		}
	}
	for pk := range d.Plans {
		if strings.EqualFold(pk, low) {
			return pk, true
		}
	}
	for ak, target := range d.PlanAliases {
		if strings.EqualFold(ak, low) && target != "" {
			return target, true
		}
	}
	return "", false
}

// LookupPlan looks up a CatalogPlanEntry across RealPlans, Plans, and PlanAliases.
func (d *CatalogDictionary) LookupPlan(key string) (CatalogPlanEntry, bool) {
	if d == nil {
		return CatalogPlanEntry{}, false
	}
	if e, ok := lookupPlanCaseInsensitive(d.RealPlans, key); ok {
		return e, true
	}
	if e, ok := lookupPlanCaseInsensitive(d.Plans, key); ok {
		return e, true
	}
	if target, ok := d.CanonicalPlanCode(key); ok {
		if e, ok2 := lookupPlanCaseInsensitive(d.RealPlans, target); ok2 {
			return e, true
		}
		if e, ok2 := lookupPlanCaseInsensitive(d.Plans, target); ok2 {
			return e, true
		}
	}
	return CatalogPlanEntry{}, false
}

// HasPlanOrAlias reports whether key exists directly in RealPlans, Plans, or PlanAliases.
func (d *CatalogDictionary) HasPlanOrAlias(key string) bool {
	_, ok := d.CanonicalPlanCode(key)
	return ok
}

// WireCatalogPlanEntry is the lightweight browser projection of CatalogPlanEntry
// sent over GET /api/catalog/dictionary (omitting heavy AvailableOptions/DefaultOptions/AddonModePrices
// that the frontend never reads, keeping the JSON payload well under browser localStorage's 5 MB quota).
type WireCatalogPlanEntry struct {
	PlanCode             string             `json:"planCode,omitempty"`
	Name                 string             `json:"name"`
	Product              string             `json:"product,omitempty"`
	Range                string             `json:"range,omitempty"`
	CPU                  *CPUInfo           `json:"cpu,omitempty"`
	Price                *float64           `json:"price,omitempty"`
	OriginalPrice        float64            `json:"originalPrice"`
	OriginalMonthlyPrice float64            `json:"originalMonthlyPrice"`
	SetupFee             float64            `json:"setupFee"`
	OriginalSetupFee     float64            `json:"originalSetupFee"`
	FreeSetup            bool               `json:"freeSetup"`
	HasPromo             bool               `json:"hasPromo"`
	PromoCode            string             `json:"promoCode,omitempty"`
	PromoBadge           string             `json:"promoBadge,omitempty"`
	PromoDescription     string             `json:"promoDescription,omitempty"`
	PricingModes         []PlanPricingMode  `json:"pricingModes,omitempty"`
	Currency             string             `json:"currency,omitempty"`
	AddonPrices          map[string]float64 `json:"addonPrices,omitempty"`
	CatalogSource        string             `json:"catalogSource,omitempty"`
	DefaultSpecs         CatalogPlanSpecs   `json:"defaultSpecs,omitempty"`
	DefaultMemory        string             `json:"defaultMemory,omitempty"`
	DefaultStorage       string             `json:"defaultStorage,omitempty"`
	DefaultConfigKey     string             `json:"defaultConfigKey,omitempty"`
}

// WireCatalogDictionary is the browser-optimized JSON payload returned by GET /api/catalog/dictionary.
type WireCatalogDictionary struct {
	UpdatedAt   int64                           `json:"updatedAt"`
	Currency    string                          `json:"currency,omitempty"`
	Plans       map[string]WireCatalogPlanEntry `json:"plans"`
	RealPlans   map[string]WireCatalogPlanEntry `json:"realPlans,omitempty"`
	PlanAliases map[string]string               `json:"planAliases,omitempty"`
	AddonPrices map[string]float64              `json:"addonPrices,omitempty"`
	Options     map[string]any                  `json:"options"`
}

func toWirePlanEntry(e CatalogPlanEntry) WireCatalogPlanEntry {
	return WireCatalogPlanEntry{
		PlanCode:             e.PlanCode,
		Name:                 e.Name,
		Product:              e.Product,
		Range:                e.Range,
		CPU:                  e.CPU,
		Price:                e.Price,
		OriginalPrice:        e.OriginalPrice,
		OriginalMonthlyPrice: e.OriginalMonthlyPrice,
		SetupFee:             e.SetupFee,
		OriginalSetupFee:     e.OriginalSetupFee,
		FreeSetup:            e.FreeSetup,
		HasPromo:             e.HasPromo,
		PromoCode:            e.PromoCode,
		PromoBadge:           e.PromoBadge,
		PromoDescription:     e.PromoDescription,
		PricingModes:         e.PricingModes,
		Currency:             e.Currency,
		AddonPrices:          e.AddonPrices,
		CatalogSource:        e.CatalogSource,
		DefaultSpecs:         e.DefaultSpecs,
		DefaultMemory:        e.DefaultMemory,
		DefaultStorage:       e.DefaultStorage,
		DefaultConfigKey:     e.DefaultConfigKey,
	}
}

func toWireCatalogDictionary(d *CatalogDictionary) *WireCatalogDictionary {
	if d == nil {
		return &WireCatalogDictionary{
			Plans:       map[string]WireCatalogPlanEntry{},
			RealPlans:   map[string]WireCatalogPlanEntry{},
			PlanAliases: map[string]string{},
			Options:     map[string]any{},
		}
	}
	wirePlans := make(map[string]WireCatalogPlanEntry, len(d.Plans))
	for k, e := range d.Plans {
		wirePlans[k] = toWirePlanEntry(e)
	}
	wireReal := make(map[string]WireCatalogPlanEntry, len(d.RealPlans))
	for k, e := range d.RealPlans {
		wireReal[k] = toWirePlanEntry(e)
	}
	return &WireCatalogDictionary{
		UpdatedAt:   d.UpdatedAt,
		Currency:    d.Currency,
		Plans:       wirePlans,
		RealPlans:   wireReal,
		PlanAliases: d.PlanAliases,
		AddonPrices: d.AddonPrices,
		Options:     d.Options,
	}
}

// writeGzipJSONFile streams JSON directly through gzip.NewWriter into a temp file and atomically renames it,
// avoiding large contiguous json.Marshal []byte allocations and cutting disk footprint by >95%.
func writeGzipJSONFile(path string, v any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	gz, err := gzip.NewWriterLevel(f, gzip.BestSpeed)
	if err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return err
	}
	enc := json.NewEncoder(gz)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		_ = gz.Close()
		_ = f.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := gz.Close(); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, path)
}

func readGzipJSONFile[T any](path string) (T, bool) {
	var zero T
	f, err := os.Open(path)
	if err != nil {
		return zero, false
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return zero, false
	}
	defer gz.Close()

	var out T
	if err := json.NewDecoder(gz).Decode(&out); err != nil {
		return zero, false
	}
	return out, true
}

// pruneLegacyCacheFiles removes obsolete v1/v2/v3 uncompressed .json caches, legacy vps_*.json files,
// and incomplete .tmp files from cacheDir on startup so upgrading immediately reclaims disk space.
func pruneLegacyCacheFiles(cacheDir string) int {
	entries, err := os.ReadDir(cacheDir)
	if err != nil {
		return 0
	}
	removed := 0
	for _, ent := range entries {
		if ent.IsDir() {
			continue
		}
		name := ent.Name()
		isLegacy := strings.HasSuffix(name, ".json") ||
			strings.HasSuffix(name, ".tmp") ||
			strings.HasPrefix(name, "vps_") ||
			strings.HasPrefix(name, "catalog_dict_v2_") ||
			strings.HasPrefix(name, "catalog_dict_v3_") ||
			strings.HasPrefix(name, "servers_v2_") ||
			strings.HasPrefix(name, "servers_v3_")
		if isLegacy {
			if os.Remove(filepath.Join(cacheDir, name)) == nil {
				removed++
			}
		}
	}
	return removed
}

func dirHasCacheFiles(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, ent := range entries {
		if ent.IsDir() {
			continue
		}
		name := ent.Name()
		if strings.HasSuffix(name, ".json.gz") || strings.HasSuffix(name, ".json") {
			return true
		}
	}
	return false
}
