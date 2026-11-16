package monitor

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/ovh/go-ovh/ovh"

	"ovh-nexus/server/internal/logger"
	"ovh-nexus/server/internal/notify"
	"ovh-nexus/server/internal/store"
)

type ovhServiceInfos struct {
	Expiration string `json:"expiration"`
	Status     string `json:"status"`
	Renew      *struct {
		Automatic          bool `json:"automatic"`
		DeleteAtExpiration bool `json:"deleteAtExpiration"`
		Forced             bool `json:"forced"`
		ManualPayment      bool `json:"manualPayment"`
	} `json:"renew"`
}

// StartLifecycleWatcher runs the 30-minute background checker for delivered orders and expiring services.
func (s *Service) StartLifecycleWatcher(ctx context.Context) {
	s.lifecycleMu.Lock()
	defer s.lifecycleMu.Unlock()

	if s.lifecycleRunning.Load() {
		return
	}
	parent := ctx
	if parent == nil {
		parent = s.getRootCtx()
	}
	runCtx, cancel := context.WithCancel(parent)
	s.lifecycleCancel = cancel
	s.lifecycleRunning.Store(true)

	if s.log != nil {
		s.log.Info(logger.SourceNotify, "Lifecycle watcher started", "intervalMinutes", int(defaultLifecycleInterval.Minutes()))
	}

	go func() {
		defer s.lifecycleRunning.Store(false)

		select {
		case <-runCtx.Done():
			return
		case <-time.After(15 * time.Second):
			_ = s.CheckLifecycleOnce(runCtx)
		}

		ticker := time.NewTicker(defaultLifecycleInterval)
		defer ticker.Stop()

		for {
			select {
			case <-runCtx.Done():
				if s.log != nil {
					s.log.Info(logger.SourceNotify, "Lifecycle watcher stopped")
				}
				return
			case <-ticker.C:
				_ = s.CheckLifecycleOnce(runCtx)
			}
		}
	}()
}

// StopLifecycleWatcher stops the 30-minute lifecycle watcher goroutine.
func (s *Service) StopLifecycleWatcher() {
	s.lifecycleMu.Lock()
	defer s.lifecycleMu.Unlock()
	if s.lifecycleCancel != nil {
		s.lifecycleCancel()
		s.lifecycleCancel = nil
	}
	s.lifecycleRunning.Store(false)
}

// CheckLifecycleOnce checks all accounts for newly delivered OVH orders and services expiring within rules.ExpiryDays.
func (s *Service) CheckLifecycleOnce(ctx context.Context) error {
	if s.store == nil {
		return nil
	}
	s.lifecycleCheckMu.Lock()
	defer s.lifecycleCheckMu.Unlock()

	accounts, err := s.store.ListAccounts(ctx)
	if err != nil {
		return err
	}
	if len(accounts) == 0 {
		accounts = []store.OVHAccount{{ID: ""}}
	}

	expiryDays := 7
	if settings, sErr := s.store.GetNotificationSettings(ctx); sErr == nil && settings != nil {
		expiryDays = settings.Rules.ExpiryDays
	}

	allHistory, _ := s.store.ListHistory(ctx, "")
	singleAccount := len(accounts) <= 1

	for _, acc := range accounts {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		client, resolvedAcc, cErr := s.getAccountClient(ctx, acc.ID)
		if cErr != nil || client == nil {
			continue
		}
		accKey := acc.ID
		if accKey == "" && resolvedAcc != nil {
			accKey = resolvedAcc.ID
		}

		s.checkDeliveredOrdersForAccount(ctx, client, accKey, singleAccount, allHistory)
		if expiryDays > 0 {
			s.checkExpiringServicesForAccount(ctx, client, accKey, expiryDays)
		}
	}

	return nil
}

func (s *Service) checkDeliveredOrdersForAccount(
	ctx context.Context,
	client *ovh.Client,
	accountID string,
	singleAccount bool,
	allHistory []store.PurchaseHistory,
) {
	checked := 0
	for _, h := range allHistory {
		if ctx.Err() != nil || checked >= 30 {
			break
		}
		if h.Status != "success" || strings.TrimSpace(h.OrderID) == "" || h.DeliveredNotified {
			continue
		}
		if h.AccountID != "" && accountID != "" && h.AccountID != accountID {
			continue
		}
		if h.AccountID == "" && !singleAccount {
			continue
		}

		checked++
		var raw json.RawMessage
		apiPath := fmt.Sprintf("/me/order/%s/status", url.PathEscape(h.OrderID))
		if err := client.GetWithContext(ctx, apiPath, &raw); err != nil {
			continue
		}

		statusStr := parseOrderStatus(raw)
		if !strings.EqualFold(statusStr, "delivered") {
			continue
		}

		if err := s.store.MarkOrderDeliveredNotified(ctx, h.ID); err != nil {
			continue
		}

		modelName := h.PlanCode
		if s.catalog != nil {
			ep, zn := s.resolveEndpointAndZone(ctx, accountID)
			if meta := s.catalog.ResolvePlan(ctx, h.PlanCode, "", ep, zn); meta.Name != "" {
				modelName = meta.Name
			}
		}

		if s.log != nil {
			s.log.Info(logger.SourceNotify, "Order marked as delivered",
				"orderId", h.OrderID,
				"planCode", h.PlanCode,
				"datacenter", strings.ToUpper(h.Datacenter),
			)
		}
		if s.notify != nil {
			_ = s.notify.EmitEvent(ctx, notify.EventOrderDelivered, "", "", nil, notify.WithTemplateData(map[string]any{
				"preferTemplate": true,
				"orderId":        h.OrderID,
				"planCode":       h.PlanCode,
				"modelName":      modelName,
				"datacenter":     strings.ToUpper(h.Datacenter),
			}))
		}
	}
}

func (s *Service) checkExpiringServicesForAccount(
	ctx context.Context,
	client *ovh.Client,
	accountID string,
	expiryDays int,
) {
	type serviceSpec struct {
		serviceType string
		listPath    string
	}
	specs := []serviceSpec{
		{serviceType: "dedicated", listPath: "/dedicated/server"},
		{serviceType: "vps", listPath: "/vps"},
	}

	nowLocal := time.Now().In(time.Local)
	todayLocal := time.Date(nowLocal.Year(), nowLocal.Month(), nowLocal.Day(), 0, 0, 0, 0, time.Local)

	for _, sp := range specs {
		var names []string
		if err := client.GetWithContext(ctx, sp.listPath, &names); err != nil {
			continue
		}
		for _, svcName := range names {
			if ctx.Err() != nil {
				return
			}
			var info ovhServiceInfos
			infoPath := fmt.Sprintf("%s/%s/serviceInfos", sp.listPath, url.PathEscape(svcName))
			if err := client.GetWithContext(ctx, infoPath, &info); err != nil {
				continue
			}

			expTime, ok := parseExpirationTime(info.Expiration)
			if !ok {
				continue
			}
			expLocal := expTime.In(time.Local)
			expDay := time.Date(expLocal.Year(), expLocal.Month(), expLocal.Day(), 0, 0, 0, 0, time.Local)
			daysLeft := int(expDay.Sub(todayLocal).Hours() / 24)
			if daysLeft < 0 || daysLeft > expiryDays {
				continue
			}

			expDateStr := expLocal.Format("2006-01-02")
			markerKey := fmt.Sprintf("expiring:default:%s:%s:%s", sp.serviceType, svcName, expDateStr)
			if accountID != "" {
				markerKey = fmt.Sprintf("expiring:%s:%s:%s:%s", accountID, sp.serviceType, svcName, expDateStr)
			}

			seen, err := s.store.HasNotifiedMarker(ctx, markerKey)
			if err != nil || seen {
				continue
			}

			renewMode := "manual"
			if info.Renew != nil {
				if info.Renew.Automatic {
					renewMode = "automatic"
				} else if info.Renew.DeleteAtExpiration {
					renewMode = "deleteAtExpiration"
				}
			}

			if err := s.store.SetNotifiedMarker(ctx, markerKey, "service_expiring"); err != nil {
				continue
			}

			if s.log != nil {
				s.log.Info(logger.SourceNotify, "Service expiration warning emitted",
					"serviceType", sp.serviceType,
					"serviceName", svcName,
					"expiration", expDateStr,
					"daysRemaining", daysLeft,
				)
			}

			if s.notify != nil {
				_ = s.notify.EmitEvent(ctx, notify.EventServiceExpiring, "", "", nil, notify.WithTemplateData(map[string]any{
					"preferTemplate": true,
					"serviceName":    svcName,
					"serviceType":    sp.serviceType,
					"expiration":     expDateStr,
					"daysRemaining":  strconv.Itoa(daysLeft),
					"renewMode":      renewMode,
				}))
			}
		}
	}
}

func parseOrderStatus(raw json.RawMessage) string {
	var str string
	if err := json.Unmarshal(raw, &str); err == nil {
		return strings.TrimSpace(str)
	}
	var obj struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(raw, &obj); err == nil {
		return strings.TrimSpace(obj.Status)
	}
	return ""
}

func parseExpirationTime(raw string) (time.Time, bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return time.Time{}, false
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02"} {
		if t, err := time.Parse(layout, trimmed); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}
