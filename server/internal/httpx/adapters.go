package httpx

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"ovh-nexus/server/internal/catalog"
	"ovh-nexus/server/internal/logger"
	"ovh-nexus/server/internal/mcp"
	"ovh-nexus/server/internal/monitor"
	"ovh-nexus/server/internal/notify"
	"ovh-nexus/server/internal/ovhclient"
	"ovh-nexus/server/internal/procurement"
	"ovh-nexus/server/internal/store"
)

type catalogAdapter struct {
	cat   *catalog.Service
	store *store.Store
	mon   *monitor.Service
}

func (a *catalogAdapter) resolveEndpointAndZone(ctx context.Context) (string, string) {
	if a.store != nil {
		if acc, err := a.store.GetActiveAccount(ctx, ""); err == nil && acc != nil {
			ep := ovhclient.NormalizeEndpoint(acc.Endpoint)
			return ep, ovhclient.NormalizeZone(acc.Zone, ep)
		}
	}
	return ovhclient.EndpointEU, "IE"
}

func (a *catalogAdapter) QueryAvailability(
	ctx context.Context,
	planFilter, dcFilter string,
	onlyAvailable, presaleOnly bool,
	offset, limit int,
) ([]mcp.CatalogEntry, int, error) {
	if a.cat == nil {
		return nil, 0, errors.New("catalog service is not configured")
	}
	if offset < 0 {
		offset = 0
	}
	if limit < 1 {
		limit = 50
	} else if limit > 200 {
		limit = 200
	}

	ep, zn := a.resolveEndpointAndZone(ctx)
	rawItems, err := a.cat.GetRawAvailabilities(ctx, ep, "")
	if err != nil {
		return nil, 0, err
	}
	dict, _ := a.cat.GetDictionary(ctx, ep, zn, false)
	hasDict := dict != nil && len(dict.Plans) > 0

	planLow := strings.ToLower(strings.TrimSpace(planFilter))
	dcLow := strings.ToLower(strings.TrimSpace(dcFilter))
	dcDispLow := ""
	if dcLow != "" {
		dcDispLow = strings.ToLower(catalog.NormalizeDisplayDatacenter(dcLow))
	}

	grouped := make(map[string]*mcp.CatalogEntry)
	order := make([]string, 0)

	for _, item := range rawItems {
		pcode := strings.TrimSpace(item.PlanCode)
		if pcode == "" || catalog.IsNonServerAddonCode(pcode) {
			continue
		}
		meta := catalog.ResolvePlanWithDict(pcode, item.Server, dict)
		if strings.EqualFold(meta.Range, "VPS") {
			continue
		}

		isListed := !hasDict || catalog.IsPlanListedInDict(pcode, dict)
		listingState := "listed"
		orderable := true
		if !isListed {
			listingState = "presale"
			orderable = false
		}
		if presaleOnly && (listingState != "presale" || orderable) {
			continue
		}

		if planLow != "" {
			if !strings.Contains(strings.ToLower(pcode), planLow) &&
				!strings.Contains(strings.ToLower(meta.Name), planLow) &&
				!strings.Contains(strings.ToLower(item.Server), planLow) {
				continue
			}
		}

		matchedDC := dcLow == ""
		var inStock []string
		for _, dc := range item.Datacenters {
			dispDC := catalog.NormalizeDisplayDatacenter(dc.Datacenter)
			if dispDC == "" {
				continue
			}
			if dcLow != "" {
				rawLow := strings.ToLower(strings.TrimSpace(dc.Datacenter))
				if !strings.EqualFold(dispDC, dcDispLow) &&
					!strings.Contains(rawLow, dcLow) &&
					!strings.Contains(strings.ToLower(dispDC), dcLow) {
					continue
				}
				matchedDC = true
			}

			avail := strings.TrimSpace(dc.Availability)
			if avail != "" && !strings.EqualFold(avail, "unavailable") && !strings.EqualFold(avail, "unknown") {
				inStock = append(inStock, fmt.Sprintf("%s (%s)", strings.ToUpper(dispDC), avail))
			}
		}

		if !matchedDC {
			continue
		}
		if onlyAvailable && len(inStock) == 0 {
			continue
		}

		rec, exists := grouped[pcode]
		if !exists {
			modelName := meta.Name
			if modelName == "" {
				modelName = pcode
			}
			cpuStr := catalog.FormatCPU(meta.CPU, "en")
			if cpuStr == "" || cpuStr == "-" {
				cpuStr = item.Server
			}
			gen := meta.Generation
			if gen == 0 {
				gen = catalog.InferPlanGeneration(pcode)
			}
			rec = &mcp.CatalogEntry{
				PlanCode:             pcode,
				ModelName:            modelName,
				Range:                meta.Range,
				CPU:                  cpuStr,
				ListingState:         listingState,
				Orderable:            orderable,
				Generation:           gen,
				AvailableDatacenters: []string{},
			}
			grouped[pcode] = rec
			order = append(order, pcode)
		}
		rec.MatchingConfigurations++
		for _, tag := range inStock {
			if !slices.Contains(rec.AvailableDatacenters, tag) {
				rec.AvailableDatacenters = append(rec.AvailableDatacenters, tag)
			}
		}
	}

	totalMatched := len(order)
	if offset > totalMatched {
		offset = totalMatched
	}
	end := min(offset+limit, totalMatched)
	out := make([]mcp.CatalogEntry, 0, end-offset)
	for _, pcode := range order[offset:end] {
		out = append(out, *grouped[pcode])
	}
	return out, totalMatched, nil
}

func (a *catalogAdapter) ResolveModelName(ctx context.Context, planCode string) (string, error) {
	if a.cat == nil {
		return planCode, nil
	}
	ep, zn := a.resolveEndpointAndZone(ctx)
	meta := a.cat.ResolvePlan(ctx, planCode, "", ep, zn)
	if strings.TrimSpace(meta.Name) != "" {
		return meta.Name, nil
	}
	return planCode, nil
}

func (a *catalogAdapter) GetSKUChanges(ctx context.Context, limit int, changeType string) (map[string]any, []map[string]any, error) {
	return (&monitorAdapter{
		mon:   a.mon,
		store: a.store,
		cat:   a.cat,
	}).GetSKUChanges(ctx, limit, changeType)
}

type procurementAdapter struct {
	proc *procurement.Service
}

func (a *procurementAdapter) QuoteOrder(
	ctx context.Context,
	accountID, planCode, datacenter string,
	options []string,
) (*mcp.OrderQuoteResult, error) {
	if a.proc == nil {
		return nil, errors.New("procurement service is not configured")
	}
	res, err := a.proc.QuoteOrder(ctx, procurement.QuoteRequest{
		AccountID:  accountID,
		PlanCode:   planCode,
		Datacenter: datacenter,
		Options:    options,
	})
	if err != nil {
		return nil, err
	}
	var priceMap map[string]any
	if res != nil && res.Price != nil {
		priceMap = structToMap(res.Price)
	}
	return &mcp.OrderQuoteResult{Price: priceMap}, nil
}

func (a *procurementAdapter) CreateUnpaidOrder(ctx context.Context, req mcp.UnpaidOrderRequest) (*mcp.UnpaidOrderResult, error) {
	if a.proc == nil {
		return nil, errors.New("procurement service is not configured")
	}
	res, err := a.proc.CreateUnpaidOrder(ctx, procurement.UnpaidOrderRequest{
		TaskID:     req.TaskID,
		AccountID:  req.AccountID,
		PlanCode:   req.PlanCode,
		Datacenter: req.Datacenter,
		Options:    req.Options,
	})
	if err != nil {
		return nil, err
	}
	var priceMap map[string]any
	if res.Price != nil {
		priceMap = structToMap(res.Price)
	}
	return &mcp.UnpaidOrderResult{
		OrderID: res.OrderID,
		Price:   priceMap,
	}, nil
}

type monitorAdapter struct {
	mon   *monitor.Service
	store *store.Store
	cat   *catalog.Service
}

func (a *monitorAdapter) GetSKUChanges(ctx context.Context, limit int, changeType string) (map[string]any, []map[string]any, error) {
	if limit < 1 {
		limit = 20
	} else if limit > 50 {
		limit = 50
	}

	filter := strings.ToUpper(strings.TrimSpace(changeType))
	if filter == "" {
		filter = "ALL"
	}

	if a.mon != nil {
		skuStatus, allChanges, err := a.mon.GetSKUStatusAndChanges(ctx)
		if err != nil {
			return nil, nil, err
		}
		presaleSkus := skuStatus.PresaleSKUs
		if presaleSkus == nil {
			presaleSkus = map[string]string{}
		}
		recentFirstSeen := skuStatus.RecentFirstSeen
		if recentFirstSeen == nil {
			recentFirstSeen = map[string]string{}
		}

		filtered := make([]map[string]any, 0, min(limit, len(allChanges)))
		for _, rec := range allChanges {
			kind := strings.ToUpper(strings.TrimSpace(rec.Type))
			switch filter {
			case "ALL":
			case "ADDED":
				if kind != "ADDED" && kind != "CONFIG_ADDED" {
					continue
				}
			case "REMOVED":
				if kind != "REMOVED" && kind != "CONFIG_REMOVED" {
					continue
				}
			default:
				if kind != filter {
					continue
				}
			}

			ch := structToMap(rec)
			skuKey := strings.TrimSpace(rec.SKUKey)
			if skuKey == "" {
				skuKey = rec.PlanCode
			}
			modelName := strings.TrimSpace(rec.Name)
			if modelName == "" {
				modelName = rec.PlanCode
			}
			listingState := strings.TrimSpace(rec.ListingState)
			if listingState == "" {
				listingState = "listed"
			}
			gen := rec.Generation
			if gen == 0 {
				gen = catalog.InferPlanGeneration(rec.PlanCode)
			}

			ch["skuKey"] = skuKey
			ch["planCode"] = rec.PlanCode
			ch["name"] = modelName
			ch["modelName"] = modelName
			ch["range"] = rec.Range
			ch["type"] = rec.Type
			ch["changeType"] = kind
			ch["listingState"] = listingState
			if gen > 0 {
				ch["generation"] = gen
			}
			ch["timestamp"] = rec.Timestamp
			ch["detectedAt"] = rec.Timestamp

			filtered = append(filtered, ch)
			if len(filtered) >= limit {
				break
			}
		}

		status := map[string]any{
			"running":            skuStatus.Running,
			"initialized":        skuStatus.Initialized,
			"endpoint":           skuStatus.Endpoint,
			"totalSkus":          skuStatus.TotalSKUs,
			"skuCount":           skuStatus.SKUCount,
			"totalTracked":       skuStatus.TotalTracked,
			"presaleCount":       len(presaleSkus),
			"presaleSkus":        presaleSkus,
			"lastCheckTime":      skuStatus.LastCheckTime,
			"lastCheck":          skuStatus.LastCheck,
			"lastError":          skuStatus.LastError,
			"updatedAt":          skuStatus.UpdatedAt,
			"recentChangesCount": skuStatus.RecentChangesCount,
			"recentFirstSeen":    recentFirstSeen,
			"total":              len(allChanges),
		}
		return status, filtered, nil
	}

	if a.store == nil {
		return map[string]any{}, []map[string]any{}, nil
	}
	st, err := a.store.LoadSKUWatcherState(ctx)
	if err != nil {
		return nil, nil, err
	}

	var rawChanges []map[string]any
	_ = json.Unmarshal(st.ChangesJSON, &rawChanges)

	filtered := make([]map[string]any, 0, min(limit, len(rawChanges)))
	for _, ch := range rawChanges {
		kind := strings.ToUpper(strings.TrimSpace(stringVal(ch["type"])))
		if kind == "" {
			kind = strings.ToUpper(strings.TrimSpace(stringVal(ch["changeType"])))
		}
		switch filter {
		case "ALL":
		case "ADDED":
			if kind != "ADDED" && kind != "CONFIG_ADDED" {
				continue
			}
		case "REMOVED":
			if kind != "REMOVED" && kind != "CONFIG_REMOVED" {
				continue
			}
		default:
			if kind != filter {
				continue
			}
		}
		filtered = append(filtered, ch)
		if len(filtered) >= limit {
			break
		}
	}

	status := map[string]any{
		"initialized": st.Initialized,
		"endpoint":    st.Endpoint,
		"updatedAt":   st.UpdatedAt,
		"total":       len(rawChanges),
	}
	return status, filtered, nil
}

func (a *monitorAdapter) AddStockSubscription(
	ctx context.Context,
	accountID, planCode string,
	datacenters []string,
	serverName string,
) error {
	if a.store == nil {
		return errors.New("store is not configured")
	}
	planCode = strings.TrimSpace(planCode)
	if planCode == "" {
		return errors.New("planCode is required")
	}

	var existing *store.StockSubscription
	if subs, err := a.store.ListStockSubs(ctx, accountID); err == nil {
		for _, it := range subs {
			if it.PlanCode == planCode {
				cp := it
				existing = &cp
				break
			}
		}
	}

	dcs := make([]string, 0, len(datacenters))
	for _, dc := range datacenters {
		if norm := catalog.NormalizeDisplayDatacenter(dc); norm != "" && !slices.Contains(dcs, norm) {
			dcs = append(dcs, norm)
		}
	}

	name := strings.TrimSpace(serverName)
	if name == "" && a.cat != nil {
		name = a.cat.ResolvePlan(ctx, planCode, "", "", "").Name
	}

	sub := store.StockSubscription{
		AccountID:         strings.TrimSpace(accountID),
		PlanCode:          planCode,
		ServerName:        name,
		Datacenters:       dcs,
		NotifyAvailable:   true,
		AutoOrder:         false,
		AutoOrderQuantity: 1,
		LastStatus:        map[string]string{},
		History:           []store.StockHistoryEntry{},
	}
	if existing != nil {
		sub.LastStatus = existing.LastStatus
		sub.History = existing.History
		sub.CreatedAt = existing.CreatedAt
		sub.NotifyAvailable = existing.NotifyAvailable
		sub.NotifyUnavailable = existing.NotifyUnavailable
		sub.AutoOrder = existing.AutoOrder
		sub.AutoOrderQuantity = existing.AutoOrderQuantity
		if len(sub.Datacenters) == 0 {
			sub.Datacenters = existing.Datacenters
		}
		if sub.ServerName == "" {
			sub.ServerName = existing.ServerName
		}
	}

	if _, err := a.store.UpsertStockSub(ctx, sub); err != nil {
		return err
	}
	if a.mon != nil {
		// Pass nil so StartStockMonitor uses the service's long-lived rootCtx rather than the HTTP request ctx.
		a.mon.StartStockMonitor(nil)
	}
	return nil
}

func (a *monitorAdapter) RemoveStockSubscription(ctx context.Context, accountID, planCode string) error {
	if a.store == nil {
		return errors.New("store is not configured")
	}
	err := a.store.DeleteStockSub(ctx, strings.TrimSpace(accountID), strings.TrimSpace(planCode))
	if errors.Is(err, store.ErrNotFound) {
		return nil
	}
	return err
}

func (a *monitorAdapter) AddVPSSubscription(ctx context.Context, planCode string) error {
	if a.store == nil {
		return errors.New("store is not configured")
	}
	planCode = strings.TrimSpace(planCode)
	if planCode == "" {
		return errors.New("planCode is required")
	}
	subs, err := a.store.ListVPSSubs(ctx)
	if err != nil {
		return err
	}
	found := false
	for _, s := range subs {
		if strings.EqualFold(s.PlanCode, planCode) {
			found = true
			break
		}
	}
	if !found {
		if _, err := a.store.UpsertVPSSub(ctx, store.VPSSubscription{
			PlanCode:        planCode,
			OVHSubsidiary:   "IE",
			Datacenters:     []string{},
			MonitorLinux:    true,
			NotifyAvailable: true,
			LastStatus:      map[string]string{},
		}); err != nil {
			return err
		}
	}
	if a.mon != nil {
		a.mon.StartVPSMonitor(nil)
	}
	return nil
}

func (a *monitorAdapter) RemoveVPSSubscription(ctx context.Context, planCodeOrID string) error {
	if a.store == nil {
		return errors.New("store is not configured")
	}
	target := strings.TrimSpace(planCodeOrID)
	subs, err := a.store.ListVPSSubs(ctx)
	if err != nil {
		return err
	}
	for _, s := range subs {
		if s.ID == target || strings.EqualFold(s.PlanCode, target) {
			if delErr := a.store.DeleteVPSSub(ctx, s.ID); delErr != nil && !errors.Is(delErr, store.ErrNotFound) {
				return delErr
			}
		}
	}
	if a.mon != nil {
		if remaining, listErr := a.store.ListVPSSubs(ctx); listErr == nil && len(remaining) == 0 {
			a.mon.StopVPSMonitor()
		}
	}
	return nil
}

type notifyAdapter struct {
	disp *notify.Dispatcher
}

func (a *notifyAdapter) EmitMCPAction(ctx context.Context, evt mcp.MCPActionEvent) {
	if a.disp == nil {
		return
	}
	opts := []notify.EventOption{
		notify.WithTemplateData(map[string]any{
			"preferTemplate": true,
			"tool":           evt.Tool,
			"scope":          evt.Scope,
			"summary":        evt.Summary,
			"clientIp":       evt.ClientIP,
		}),
	}
	if ts, err := time.Parse(time.RFC3339, evt.Timestamp); err == nil {
		opts = append(opts, notify.WithTimestamp(ts))
	}
	_ = a.disp.EmitEvent(ctx, notify.EventMCPAction, "", "", nil, opts...)
}

// RegisterChannelSecrets loads stored notification channels and registers sensitive
// tokens/passwords in logHub so they are scrubbed from stdout, SQLite, and SSE logs.
func RegisterChannelSecrets(ctx context.Context, st *store.Store, logHub *logger.Hub) error {
	if st == nil || logHub == nil {
		return nil
	}
	channels, err := st.ListChannels(ctx)
	if err != nil {
		return err
	}
	for _, ch := range channels {
		if ch.Config == nil {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(ch.Type)) {
		case "telegram":
			if tok := strings.TrimSpace(stringVal(ch.Config, "token")); tok != "" {
				logHub.AddSensitiveValue(tok)
			}
		case "smtp":
			if pwd := stringVal(ch.Config, "password"); pwd != "" {
				logHub.AddSensitiveValue(pwd)
			}
		}
	}
	return nil
}

func pruneDeletedAccountFromMCP(ctx context.Context, st *store.Store, accountID string) {
	if st == nil || strings.TrimSpace(accountID) == "" {
		return
	}
	cfg, err := st.LoadMCPConfig(ctx)
	if err != nil || cfg == nil || len(cfg.AllowedAccountIDs) == 0 {
		return
	}
	filtered := make([]string, 0, len(cfg.AllowedAccountIDs))
	changed := false
	for _, id := range cfg.AllowedAccountIDs {
		if id == accountID {
			changed = true
			continue
		}
		filtered = append(filtered, id)
	}
	if changed {
		cfg.AllowedAccountIDs = filtered
		_ = st.SaveMCPConfig(ctx, cfg)
	}
}

func structToMap(v any) map[string]any {
	b, err := json.Marshal(v)
	if err != nil {
		return map[string]any{}
	}
	var out map[string]any
	if err := json.Unmarshal(b, &out); err != nil || out == nil {
		return map[string]any{}
	}
	return out
}

func stringVal(args ...any) string {
	switch len(args) {
	case 1:
		s, _ := args[0].(string)
		return s
	case 2:
		m, _ := args[0].(map[string]any)
		k, _ := args[1].(string)
		if m == nil {
			return ""
		}
		s, _ := m[k].(string)
		return s
	default:
		return ""
	}
}
