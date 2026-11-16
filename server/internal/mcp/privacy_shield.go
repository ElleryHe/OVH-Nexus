package mcp

import (
	"context"
	"fmt"
	"maps"
	"net/netip"
	"regexp"
	"sort"
	"strings"
	"sync"

	"ovh-nexus/server/internal/store"
	"ovh-nexus/server/internal/vault"
)

var categoryPrefixes = map[string]string{
	"account":   "acct",
	"baremetal": "bm",
	"vps":       "vps",
	"ip":        "ip",
	"order":     "ord",
}

var categoryOrder = []string{"account", "baremetal", "vps", "ip", "order"}

var (
	reIPv4          = regexp.MustCompile(`\b(\d{1,3})\.(\d{1,3})\.(\d{1,3})\.(\d{1,3})(?:/\d{1,2})?\b`)
	reIPv6Candidate = regexp.MustCompile(`\b(?:[0-9a-fA-F]{0,4}:){2,7}[0-9a-fA-F]{0,4}(?:/\d{1,3})?\b`)
	reOVHBMHost     = regexp.MustCompile(`(?i)\b(?:ns\d+\.ip-[\d-]+(?:\.[a-z0-9-]+)+|ip-\d+-\d+-\d+(?:\.[a-z0-9-]+)*|sd-\d+(?:\.[a-z0-9-]+)*|ks\d+(?:\.[a-z0-9-]+)*)\b`)
	reOVHVPSHost    = regexp.MustCompile(`(?i)\b(?:vps-[a-z0-9-]+\.vps\.ovh\.[a-z.]+|vps\d+\.ovh\.[a-z.]+)\b`)
	reEmail         = regexp.MustCompile(`\b[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}\b`)
	reNICHandle     = regexp.MustCompile(`(?i)\b[a-z]{2}\d{1,8}-ovh\b`)
	reOrderURL      = regexp.MustCompile(`(?i)https?://[^\s"'<>]+(?:orderId|orderPassword|displayOrder)[^\s"'<>]*`)
	reOrderPwdParam = regexp.MustCompile(`(?i)(orderPassword\s*[=:]\s*)[^\s&"'<>]+`)
	reIntlPhone     = regexp.MustCompile(`\+\d{1,3}[\s.-]?\(?\d{1,4}\)?(?:[\s.-]?\d{2,5}){2,4}`)
)

// AliasEntry represents a single bidirectional Privacy Shield alias mapping for WebUI display.
type AliasEntry struct {
	Category     string `json:"category"`
	Alias        string `json:"alias"`
	Label        string `json:"label"`
	Hint         string `json:"hint"`
	AccountAlias string `json:"accountAlias"`
	MaskedReal   string `json:"maskedReal"`
	RealValue    string `json:"realValue"`
}

// PrivacyShield provides thread-safe bidirectional alias tokenization and PII/IP/URL redaction
// persisted in SQLite via store.Store.
type PrivacyShield struct {
	store *store.Store
	mu    sync.Mutex
	state *store.MCPAliasState
}

// NewPrivacyShield creates a PrivacyShield backed by SQLite store.
func NewPrivacyShield(st *store.Store) (*PrivacyShield, error) {
	ps := &PrivacyShield{store: st}
	if st != nil {
		loaded, err := st.LoadMCPAliases(context.Background())
		if err != nil {
			return nil, err
		}
		ps.state = loaded
	} else {
		ps.state = emptyAliasState()
	}
	return ps, nil
}

func emptyAliasState() *store.MCPAliasState {
	st := &store.MCPAliasState{
		Forward:  make(map[string]map[string]string, len(categoryOrder)),
		Reverse:  make(map[string]map[string]string, len(categoryOrder)),
		Meta:     make(map[string]map[string]map[string]any, len(categoryOrder)),
		Counters: make(map[string]int, len(categoryOrder)),
	}
	for _, cat := range categoryOrder {
		st.Forward[cat] = make(map[string]string)
		st.Reverse[cat] = make(map[string]string)
		st.Meta[cat] = make(map[string]map[string]any)
		st.Counters[cat] = 0
	}
	return st
}

// FormatIPHint returns a masked IP tail hint such as "*.*.*.42" (IPv4) or "*::1" (IPv6).
func FormatIPHint(rawIP string) string {
	clean := strings.TrimSpace(rawIP)
	if clean == "" {
		return ""
	}
	addrOnly := strings.TrimSpace(strings.Split(clean, "/")[0])
	if addr, err := netip.ParseAddr(addrOnly); err == nil {
		addr = addr.Unmap()
		if addr.Is4() {
			b := addr.As4()
			return fmt.Sprintf("*.*.*.%d", b[3])
		}
		if addr.Is6() {
			parts := strings.Split(addrOnly, ":")
			for i := len(parts) - 1; i >= 0; i-- {
				if parts[i] != "" {
					return "*::" + parts[i]
				}
			}
			return "*::0"
		}
	}
	if m := reIPv4.FindStringSubmatch(addrOnly); len(m) == 5 && m[0] == addrOnly {
		return "*.*.*." + m[4]
	}
	if strings.Contains(addrOnly, ":") {
		parts := strings.Split(addrOnly, ":")
		for i := len(parts) - 1; i >= 0; i-- {
			if parts[i] != "" {
				return "*::" + parts[i]
			}
		}
		return "*::0"
	}
	return "***"
}

// SanitizeCustomLabel preserves user-defined labels while returning nil if the label
// matches the raw serviceName or leaks OVH default hostnames, IPs, emails, or NIC handles.
func SanitizeCustomLabel(rawLabel, realServiceName string) *string {
	label := strings.TrimSpace(rawLabel)
	if label == "" {
		return nil
	}
	if realServiceName != "" && strings.EqualFold(label, strings.TrimSpace(realServiceName)) {
		return nil
	}
	if reOVHBMHost.MatchString(label) || reOVHVPSHost.MatchString(label) {
		return nil
	}
	if reIPv4.MatchString(label) || hasValidIPv6(label) {
		return nil
	}
	if reEmail.MatchString(label) || reNICHandle.MatchString(label) || reIntlPhone.MatchString(label) {
		return nil
	}
	runes := []rune(label)
	if len(runes) > 64 {
		label = string(runes[:64])
	}
	return &label
}

func hasValidIPv6(text string) bool {
	for _, cand := range reIPv6Candidate.FindAllString(text, -1) {
		addrPart := strings.Split(cand, "/")[0]
		if addr, err := netip.ParseAddr(addrPart); err == nil && addr.Is6() {
			return true
		}
	}
	return false
}

// Reload refreshes the in-memory alias state from SQLite.
func (ps *PrivacyShield) Reload(ctx context.Context) error {
	if ps.store == nil {
		return nil
	}
	loaded, err := ps.store.LoadMCPAliases(ctx)
	if err != nil {
		return err
	}
	ps.mu.Lock()
	ps.state = loaded
	ps.mu.Unlock()
	return nil
}

// ClearAll deletes all Privacy Shield alias mappings and resets counters.
func (ps *PrivacyShield) ClearAll(ctx context.Context) error {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	ps.state = emptyAliasState()
	if ps.store != nil {
		return ps.store.SaveMCPAliases(ctx, ps.state)
	}
	return nil
}

// Register creates or returns a persistent alias (e.g. "bm-01") for realValue in category.
func (ps *PrivacyShield) Register(ctx context.Context, category, realValue string, meta map[string]any) (string, error) {
	prefix, ok := categoryPrefixes[category]
	if !ok {
		return "", fmt.Errorf("unsupported alias category: %s", category)
	}
	raw := strings.TrimSpace(realValue)
	if raw == "" {
		return "", nil
	}
	if category == "ip" {
		raw = strings.TrimSpace(strings.Split(raw, "/")[0])
	}

	ps.mu.Lock()
	defer ps.mu.Unlock()

	ps.ensureCategoryMapsLocked(category)
	fwd := ps.state.Forward[category]
	rev := ps.state.Reverse[category]
	metaMap := ps.state.Meta[category]

	changed := false
	alias, exists := fwd[raw]
	if !exists || alias == "" {
		nextNum := ps.state.Counters[category] + 1
		for {
			cand := fmt.Sprintf("%s-%02d", prefix, nextNum)
			if _, taken := rev[cand]; !taken {
				alias = cand
				break
			}
			nextNum++
		}
		ps.state.Counters[category] = nextNum
		fwd[raw] = alias
		rev[alias] = raw
		changed = true
	}

	if len(meta) > 0 {
		existingMeta := metaMap[alias]
		if existingMeta == nil {
			existingMeta = make(map[string]any)
		}
		for k, v := range meta {
			if v != nil && existingMeta[k] != v {
				existingMeta[k] = v
				changed = true
			}
		}
		metaMap[alias] = existingMeta
	}

	if changed && ps.store != nil {
		if err := ps.store.SaveMCPAliases(ctx, ps.state); err != nil {
			return "", err
		}
	}
	return alias, nil
}

// Resolve translates an alias ("bm-01", "acct-01") or registered safe label back to the real identifier.
func (ps *PrivacyShield) Resolve(category, aliasOrHint string) (string, bool) {
	if _, ok := categoryPrefixes[category]; !ok {
		return "", false
	}
	key := strings.TrimSpace(aliasOrHint)
	if key == "" {
		return "", false
	}

	ps.mu.Lock()
	defer ps.mu.Unlock()

	rev := ps.state.Reverse[category]
	for aliasK, realV := range rev {
		if strings.EqualFold(aliasK, key) {
			return realV, true
		}
	}

	metaMap := ps.state.Meta[category]
	for aliasK, m := range metaMap {
		if label := strings.TrimSpace(asString(m["label"])); label != "" && strings.EqualFold(label, key) {
			if realV, ok := rev[aliasK]; ok {
				return realV, true
			}
		}
	}
	return "", false
}

// GetMeta returns a shallow copy of stored metadata for (category, alias).
func (ps *PrivacyShield) GetMeta(category, alias string) map[string]any {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	if ps.state.Meta[category] == nil {
		return map[string]any{}
	}
	m := ps.state.Meta[category][alias]
	if m == nil {
		return map[string]any{}
	}
	out := make(map[string]any, len(m))
	maps.Copy(out, m)
	return out
}

// ListMappingsForUI returns all registered aliases for the WebUI MCP Gateway table.
func (ps *PrivacyShield) ListMappingsForUI() []AliasEntry {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	items := make([]AliasEntry, 0)
	for _, cat := range categoryOrder {
		rev := ps.state.Reverse[cat]
		metaMap := ps.state.Meta[cat]
		aliases := make([]string, 0, len(rev))
		for a := range rev {
			aliases = append(aliases, a)
		}
		sort.Strings(aliases)

		for _, alias := range aliases {
			realVal := rev[alias]
			m := metaMap[alias]
			maskedReal := realVal
			if cat == "ip" {
				maskedReal = FormatIPHint(realVal)
			} else if len([]rune(realVal)) > 10 {
				maskedReal = vault.MaskSecret(realVal)
			}
			items = append(items, AliasEntry{
				Category:     cat,
				Alias:        alias,
				Label:        asString(m["label"]),
				Hint:         asString(m["hint"]),
				AccountAlias: asString(m["accountAlias"]),
				MaskedReal:   maskedReal,
				RealValue:    realVal,
			})
		}
	}
	return items
}

// RedactAccount produces an AI-safe profile summary, omitting PII and API keys.
func (ps *PrivacyShield) RedactAccount(ctx context.Context, acc store.OVHAccount, isDefault bool) (map[string]any, string, error) {
	ep := strings.TrimSpace(acc.Endpoint)
	if ep == "" {
		ep = "ovh-eu"
	}
	zone := strings.ToUpper(strings.TrimSpace(acc.Zone))
	if zone == "" {
		zone = "IE"
	}
	safePtr := SanitizeCustomLabel(acc.Alias, "")
	safeLabel := "OVH-" + zone
	if safePtr != nil && *safePtr != "" {
		safeLabel = *safePtr
	}

	alias, err := ps.Register(ctx, "account", acc.ID, map[string]any{
		"label": safeLabel,
		"hint":  fmt.Sprintf("%s (%s)", ep, zone),
	})
	if err != nil {
		return nil, "", err
	}

	return map[string]any{
		"profile":   alias,
		"label":     safeLabel,
		"endpoint":  ep,
		"zone":      zone,
		"isDefault": isDefault,
	}, alias, nil
}

// RedactBaremetal produces an AI-safe bare-metal server representation with persistent bm-xx and ip-xx aliases.
func (ps *PrivacyShield) RedactBaremetal(ctx context.Context, rawSrv map[string]any, accountAlias, accountID string) (map[string]any, string, error) {
	serviceName := strings.TrimSpace(asString(rawSrv["serviceName"]))
	if serviceName == "" {
		serviceName = strings.TrimSpace(asString(rawSrv["name"]))
	}
	rawDisplay := asString(rawSrv["displayName"])
	if rawDisplay == "" {
		rawDisplay = asString(rawSrv["reverse"])
	}
	if rawDisplay == "" {
		rawDisplay = asString(rawSrv["name"])
	}
	safeLabel := SanitizeCustomLabel(rawDisplay, serviceName)

	dc := strings.ToUpper(strings.TrimSpace(asString(rawSrv["datacenter"])))
	if dc == "" {
		dc = "N/A"
	}
	model := strings.TrimSpace(asString(rawSrv["commercialRange"]))
	if model == "" {
		model = strings.TrimSpace(asString(rawSrv["model"]))
	}
	if model == "" {
		model = "baremetal"
	}

	labelHint := model
	if safeLabel != nil && *safeLabel != "" {
		labelHint = *safeLabel
	}

	rawIP := strings.TrimSpace(asString(rawSrv["ip"]))
	var ipAliasPtr, ipHintPtr *string
	var ipHint string
	if rawIP != "" && !strings.EqualFold(rawIP, "N/A") {
		ipHint = FormatIPHint(rawIP)
		ipAlias, err := ps.Register(ctx, "ip", rawIP, map[string]any{
			"label":        labelHint,
			"hint":         ipHint,
			"accountAlias": accountAlias,
		})
		if err != nil {
			return nil, "", err
		}
		if ipAlias != "" {
			ipAliasPtr = &ipAlias
		}
		if ipHint != "" {
			ipHintPtr = &ipHint
		}
	}

	hint := fmt.Sprintf("%s @ %s", model, dc)
	if ipHint != "" {
		hint += fmt.Sprintf(" (%s)", ipHint)
	}
	bmAlias, err := ps.Register(ctx, "baremetal", serviceName, map[string]any{
		"label":        labelHint,
		"hint":         hint,
		"accountAlias": accountAlias,
		"accountId":    accountID,
	})
	if err != nil {
		return nil, "", err
	}

	var profilePtr *string
	if accountAlias != "" {
		profilePtr = &accountAlias
	}

	state := asString(rawSrv["state"])
	if state == "" {
		state = "unknown"
	}
	status := asString(rawSrv["status"])
	if status == "" {
		status = "ok"
	}
	osStr := asString(rawSrv["os"])
	if osStr == "" {
		osStr = "N/A"
	}

	out := map[string]any{
		"server":          bmAlias,
		"label":           safeLabel,
		"profile":         profilePtr,
		"commercialModel": model,
		"datacenter":      dc,
		"state":           state,
		"status":          status,
		"os":              osStr,
		"monitoring":      asBool(rawSrv["monitoring"]),
		"ip": map[string]any{
			"alias":      ipAliasPtr,
			"maskedTail": ipHintPtr,
		},
	}
	if _, has := rawSrv["daysRemaining"]; has {
		out["daysRemaining"] = rawSrv["daysRemaining"]
	}
	if _, has := rawSrv["expiration"]; has {
		out["expiration"] = rawSrv["expiration"]
	}
	if _, has := rawSrv["renewMode"]; has {
		out["renewMode"] = rawSrv["renewMode"]
	}
	return out, bmAlias, nil
}

// RedactVPS produces an AI-safe VPS representation with persistent vps-xx and ip-xx aliases.
func (ps *PrivacyShield) RedactVPS(ctx context.Context, rawVPS map[string]any, accountAlias, accountID string) (map[string]any, string, error) {
	serviceName := strings.TrimSpace(asString(rawVPS["serviceName"]))
	safeLabel := SanitizeCustomLabel(asString(rawVPS["displayName"]), serviceName)

	zone := strings.TrimSpace(asString(rawVPS["zone"]))
	if zone == "" {
		zone = "N/A"
	}
	model := strings.TrimSpace(asString(rawVPS["modelName"]))
	if model == "" {
		model = strings.TrimSpace(asString(rawVPS["model"]))
	}
	if model == "" {
		model = "VPS"
	}
	labelHint := model
	if safeLabel != nil && *safeLabel != "" {
		labelHint = *safeLabel
	}

	redactedIPs := make([]map[string]any, 0)
	firstIPv4Hint := ""
	if rawIPs, ok := rawVPS["ips"].([]string); ok {
		for _, ipStr := range rawIPs {
			trimmed := strings.TrimSpace(ipStr)
			if trimmed == "" {
				continue
			}
			hint := FormatIPHint(trimmed)
			if firstIPv4Hint == "" && !strings.Contains(trimmed, ":") {
				firstIPv4Hint = hint
			}
			ipAlias, err := ps.Register(ctx, "ip", trimmed, map[string]any{
				"label":        labelHint,
				"hint":         hint,
				"accountAlias": accountAlias,
			})
			if err != nil {
				return nil, "", err
			}
			redactedIPs = append(redactedIPs, map[string]any{
				"alias":      ipAlias,
				"maskedTail": hint,
			})
		}
	}

	hint := fmt.Sprintf("%s @ %s", model, zone)
	if firstIPv4Hint != "" {
		hint += fmt.Sprintf(" (%s)", firstIPv4Hint)
	}
	vpsAlias, err := ps.Register(ctx, "vps", serviceName, map[string]any{
		"label":        labelHint,
		"hint":         hint,
		"accountAlias": accountAlias,
		"accountId":    accountID,
	})
	if err != nil {
		return nil, "", err
	}

	var profilePtr *string
	if accountAlias != "" {
		profilePtr = &accountAlias
	}
	state := asString(rawVPS["state"])
	if state == "" {
		state = "unknown"
	}
	memMB := rawVPS["memoryLimitMb"]
	if memMB == nil {
		memMB = rawVPS["memoryLimit"]
	}
	diskGB := rawVPS["diskGb"]
	if diskGB == nil {
		diskGB = rawVPS["disk"]
	}

	return map[string]any{
		"vps":           vpsAlias,
		"label":         safeLabel,
		"profile":       profilePtr,
		"model":         model,
		"zone":          zone,
		"state":         state,
		"vcore":         rawVPS["vcore"],
		"memoryMb":      memMB,
		"diskGb":        diskGB,
		"ips":           redactedIPs,
		"expiration":    rawVPS["expiration"],
		"daysRemaining": rawVPS["daysRemaining"],
		"renewMode":     rawVPS["renewMode"],
	}, vpsAlias, nil
}

// RedactOrder registers an ord-xx alias and strips raw payment/PDF URLs & orderPassword tokens.
func (ps *PrivacyShield) RedactOrder(ctx context.Context, rawOrder map[string]any, accountAlias string) (map[string]any, string, error) {
	rawOID := strings.TrimSpace(asString(rawOrder["orderId"]))
	status := strings.TrimSpace(asString(rawOrder["status"]))
	if status == "" {
		status = strings.TrimSpace(asString(rawOrder["orderStatus"]))
	}
	if status == "" {
		status = "unknown"
	}

	var ordAlias string
	var ordAliasPtr *string
	if rawOID != "" {
		dateStr := asString(rawOrder["date"])
		if len(dateStr) > 10 {
			dateStr = dateStr[:10]
		}
		var err error
		ordAlias, err = ps.Register(ctx, "order", rawOID, map[string]any{
			"label":        fmt.Sprintf("Order (%s)", status),
			"hint":         dateStr,
			"accountAlias": accountAlias,
		})
		if err != nil {
			return nil, "", err
		}
		if ordAlias != "" {
			ordAliasPtr = &ordAlias
		}
	}

	cleanFollowUp := make([]map[string]any, 0)
	if rawFup, ok := rawOrder["followUp"].([]any); ok {
		for _, item := range rawFup {
			stepMap := asMap(item)
			if len(stepMap) == 0 {
				continue
			}
			history := make([]map[string]any, 0)
			if rawHist, ok := stepMap["history"].([]any); ok {
				for _, hItem := range rawHist {
					hm := asMap(hItem)
					if len(hm) == 0 {
						continue
					}
					history = append(history, map[string]any{
						"date":        hm["date"],
						"label":       hm["label"],
						"description": ps.ScrubText(asString(hm["description"])),
					})
				}
			}
			cleanFollowUp = append(cleanFollowUp, map[string]any{
				"step":    stepMap["step"],
				"status":  stepMap["status"],
				"history": history,
			})
		}
	}

	var profilePtr *string
	if accountAlias != "" {
		profilePtr = &accountAlias
	}

	return map[string]any{
		"order":             ordAliasPtr,
		"profile":           profilePtr,
		"status":            status,
		"date":              rawOrder["date"],
		"expirationDate":    rawOrder["expirationDate"],
		"priceWithTax":      rawOrder["priceWithTax"],
		"priceWithoutTax":   rawOrder["priceWithoutTax"],
		"tax":               rawOrder["tax"],
		"followUp":          cleanFollowUp,
		"paymentLinkNotice": "Payment & invoice URLs are redacted from AI context for security. View or pay in the OVH Nexus Web Console [Orders & Delivery] tab or via your notification channel.",
	}, ordAlias, nil
}

// ScrubText scrubs registered identifiers, raw hostnames, IPs, emails, NIC handles,
// phone numbers, orderPassword tokens, and raw payment URLs from arbitrary text.
func (ps *PrivacyShield) ScrubText(text string) string {
	if text == "" {
		return ""
	}
	out := text

	type replacement struct {
		real  string
		alias string
	}
	var reps []replacement

	ps.mu.Lock()
	for _, cat := range []string{"baremetal", "vps", "ip", "order", "account"} {
		for realVal, alias := range ps.state.Forward[cat] {
			if len(realVal) >= 4 && alias != "" && strings.Contains(out, realVal) {
				reps = append(reps, replacement{real: realVal, alias: alias})
			}
		}
	}
	ps.mu.Unlock()

	sort.Slice(reps, func(i, j int) bool {
		return len(reps[i].real) > len(reps[j].real)
	})
	for _, r := range reps {
		out = strings.ReplaceAll(out, r.real, r.alias)
	}

	out = reOrderURL.ReplaceAllString(out, "[REDACTED_ORDER_URL]")
	out = reOrderPwdParam.ReplaceAllString(out, "${1}[REDACTED]")
	out = reOVHBMHost.ReplaceAllString(out, "[REDACTED_BM_HOST]")
	out = reOVHVPSHost.ReplaceAllString(out, "[REDACTED_VPS_HOST]")
	out = reEmail.ReplaceAllString(out, "[REDACTED_EMAIL]")
	out = reNICHandle.ReplaceAllString(out, "[REDACTED_NIC]")
	out = reIntlPhone.ReplaceAllString(out, "[REDACTED_PHONE]")
	out = reIPv4.ReplaceAllStringFunc(out, func(m string) string {
		sub := reIPv4.FindStringSubmatch(m)
		if len(sub) == 5 {
			return "*.*.*." + sub[4]
		}
		return "*.*.*.*"
	})
	out = reIPv6Candidate.ReplaceAllStringFunc(out, func(m string) string {
		addrPart := strings.Split(m, "/")[0]
		addr, err := netip.ParseAddr(addrPart)
		if err != nil || !addr.Is6() {
			return m
		}
		return FormatIPHint(m)
	})
	return out
}

func (ps *PrivacyShield) ensureCategoryMapsLocked(category string) {
	if ps.state.Forward[category] == nil {
		ps.state.Forward[category] = make(map[string]string)
	}
	if ps.state.Reverse[category] == nil {
		ps.state.Reverse[category] = make(map[string]string)
	}
	if ps.state.Meta[category] == nil {
		ps.state.Meta[category] = make(map[string]map[string]any)
	}
}
