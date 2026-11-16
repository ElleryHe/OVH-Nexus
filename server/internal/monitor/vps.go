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
	"ovh-nexus/server/internal/ovhclient"
	"ovh-nexus/server/internal/store"
)

// IsVPSRunning reports whether the VPS availability monitor goroutine is active.
func (s *Service) IsVPSRunning() bool {
	return s.vpsRunning.Load()
}

// StartVPSMonitor starts the periodic VPS availability monitoring loop.
func (s *Service) StartVPSMonitor(ctx context.Context) {
	s.vpsMu.Lock()
	defer s.vpsMu.Unlock()

	if s.vpsRunning.Load() {
		return
	}
	parent := ctx
	if parent == nil {
		parent = s.getRootCtx()
	}
	runCtx, cancel := context.WithCancel(parent)
	s.vpsCancel = cancel
	s.vpsRunning.Store(true)

	intervalSec := s.vpsIntervalSec.Load()
	if intervalSec < defaultVPSIntervalSec {
		intervalSec = defaultVPSIntervalSec
	}

	if s.log != nil {
		s.log.Info(logger.SourceVPSMonitor, "VPS stock monitor started", "intervalSeconds", intervalSec)
	}

	go func() {
		defer s.vpsRunning.Store(false)
		curInterval := intervalSec
		ticker := time.NewTicker(time.Duration(curInterval) * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-runCtx.Done():
				if s.log != nil {
					s.log.Info(logger.SourceVPSMonitor, "VPS stock monitor stopped")
				}
				return
			case <-ticker.C:
				_, _ = s.CheckVPSOnce(runCtx, "")
				latest := s.vpsIntervalSec.Load()
				if latest >= defaultVPSIntervalSec && latest != curInterval {
					curInterval = latest
					ticker.Reset(time.Duration(curInterval) * time.Second)
				}
			}
		}
	}()
}

// StopVPSMonitor stops the background VPS monitor goroutine.
func (s *Service) StopVPSMonitor() {
	s.vpsMu.Lock()
	defer s.vpsMu.Unlock()
	if s.vpsCancel != nil {
		s.vpsCancel()
		s.vpsCancel = nil
	}
	s.vpsRunning.Store(false)
}

// CheckVPSOnce evaluates all VPS monitor subscriptions (or a specific planCode if non-empty).
func (s *Service) CheckVPSOnce(ctx context.Context, filterPlanCode string) (int, error) {
	if s.store == nil || s.catalog == nil {
		return 0, errors.New("monitor store or catalog service is unavailable")
	}
	s.vpsCheckMu.Lock()
	defer s.vpsCheckMu.Unlock()

	subs, err := s.store.ListVPSSubs(ctx)
	if err != nil {
		return 0, err
	}
	if len(subs) == 0 {
		return 0, nil
	}

	type vpsQueryKey struct {
		ep  string
		pc  string
		sub string
	}
	availCache := make(map[vpsQueryKey]*catalog.VPSAvailabilityResponse)
	transitions := 0
	filterPlanCode = strings.TrimSpace(filterPlanCode)

	for _, sub := range subs {
		if ctx.Err() != nil {
			return transitions, ctx.Err()
		}
		if filterPlanCode != "" && !strings.EqualFold(sub.PlanCode, filterPlanCode) {
			continue
		}

		subsidiary := strings.ToUpper(strings.TrimSpace(sub.OVHSubsidiary))
		if subsidiary == "" {
			subsidiary = "IE"
		}
		ep := endpointForSubsidiary(subsidiary)
		qk := vpsQueryKey{ep: ep, pc: sub.PlanCode, sub: subsidiary}

		resp, cached := availCache[qk]
		if !cached {
			fetched, fetchErr := s.catalog.GetVPSDatacenterAvailability(ctx, ep, sub.PlanCode, subsidiary)
			if fetchErr != nil {
				if s.log != nil {
					s.log.Warn(logger.SourceVPSMonitor, "Failed to check VPS availability",
						"planCode", sub.PlanCode,
						"subsidiary", subsidiary,
						"error", fetchErr.Error(),
					)
				}
				availCache[qk] = nil
				continue
			}
			availCache[qk] = fetched
			resp = fetched
		}
		if resp == nil {
			continue
		}

		if sub.LastStatus == nil {
			sub.LastStatus = make(map[string]string)
		}

		wantAll := len(sub.Datacenters) == 0
		dirty := false

		for _, dc := range resp.Datacenters {
			dcCode := strings.ToUpper(strings.TrimSpace(dc.Code))
			if dcCode == "" {
				dcCode = strings.ToUpper(strings.TrimSpace(dc.Datacenter))
			}
			if dcCode == "" {
				continue
			}
			if !wantAll && !vpsTargetsMatch(sub.Datacenters, dcCode, dc.Datacenter) {
				continue
			}

			dcName := strings.TrimSpace(dc.Datacenter)
			if dcName == "" {
				dcName = dcCode
			}
			curStatus := strings.TrimSpace(dc.Status)
			if curStatus == "" {
				curStatus = "outOfStock"
			}

			oldStatus := sub.LastStatus[dcCode]
			wasAvail := strings.EqualFold(oldStatus, "available")
			curAvail := strings.EqualFold(curStatus, "available")

			switch {
			case oldStatus == "":
				sub.LastStatus[dcCode] = curStatus
				dirty = true

			case !wasAvail && curAvail:
				transitions++
				sub.LastStatus[dcCode] = curStatus
				_, _ = s.store.UpsertVPSSub(ctx, sub)
				dirty = false

				_ = s.store.AppendVPSHistory(ctx, sub.ID, store.VPSHistoryEntry{
					SubscriptionID: sub.ID,
					Datacenter:     dcName,
					DatacenterCode: dcCode,
					Status:         curStatus,
					ChangeType:     "available",
					OldStatus:      oldStatus,
				})

				if s.log != nil {
					s.log.Info(logger.SourceVPSMonitor, "VPS stock transitioned to available",
						"planCode", sub.PlanCode,
						"subsidiary", subsidiary,
						"datacenter", dcCode,
					)
				}
				if sub.NotifyAvailable && s.notify != nil {
					_ = s.notify.EmitEvent(ctx, notify.EventStockAvailable, "", "", nil, notify.WithTemplateData(map[string]any{
						"preferTemplate": true,
						"planCode":       sub.PlanCode,
						"modelName":      fmt.Sprintf("%s (%s)", sub.PlanCode, subsidiary),
						"datacenter":     fmt.Sprintf("%s (%s)", dcName, dcCode),
					}))
				}

			case wasAvail && !curAvail:
				transitions++
				sub.LastStatus[dcCode] = curStatus
				_, _ = s.store.UpsertVPSSub(ctx, sub)
				dirty = false

				_ = s.store.AppendVPSHistory(ctx, sub.ID, store.VPSHistoryEntry{
					SubscriptionID: sub.ID,
					Datacenter:     dcName,
					DatacenterCode: dcCode,
					Status:         curStatus,
					ChangeType:     "unavailable",
					OldStatus:      oldStatus,
				})

				if s.log != nil {
					s.log.Info(logger.SourceVPSMonitor, "VPS stock transitioned to unavailable",
						"planCode", sub.PlanCode,
						"subsidiary", subsidiary,
						"datacenter", dcCode,
					)
				}
				if sub.NotifyUnavailable && s.notify != nil {
					_ = s.notify.EmitEvent(ctx, notify.EventStockUnavailable, "", "", nil, notify.WithTemplateData(map[string]any{
						"preferTemplate": true,
						"planCode":       sub.PlanCode,
						"modelName":      fmt.Sprintf("%s (%s)", sub.PlanCode, subsidiary),
						"datacenter":     fmt.Sprintf("%s (%s)", dcName, dcCode),
					}))
				}

			default:
				if oldStatus != curStatus {
					sub.LastStatus[dcCode] = curStatus
					dirty = true
				}
			}
		}

		if dirty {
			_, _ = s.store.UpsertVPSSub(ctx, sub)
		}
	}

	return transitions, nil
}

func vpsTargetsMatch(targets []string, code, name string) bool {
	return slices.ContainsFunc(targets, func(t string) bool {
		return strings.EqualFold(strings.TrimSpace(t), code) || strings.EqualFold(strings.TrimSpace(t), name)
	})
}

func endpointForSubsidiary(sub string) string {
	switch strings.ToUpper(strings.TrimSpace(sub)) {
	case "US":
		return ovhclient.EndpointUS
	case "CA", "QC", "WE", "WS", "SG", "AU", "IN":
		return ovhclient.EndpointCA
	default:
		return ovhclient.EndpointEU
	}
}

// ==================== VPS Monitor HTTP Handlers ====================

// HandleVPSStatus handles GET /api/vps-monitor/status.
func (s *Service) HandleVPSStatus(w http.ResponseWriter, r *http.Request) {
	subs, err := s.store.ListVPSSubs(r.Context())
	count := 0
	if err == nil {
		count = len(subs)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"running":             s.IsVPSRunning(),
		"subscriptions_count": count,
		"check_interval":      s.vpsIntervalSec.Load(),
	})
}

// HandleVPSStart handles POST /api/vps-monitor/start.
func (s *Service) HandleVPSStart(w http.ResponseWriter, _ *http.Request) {
	s.StartVPSMonitor(s.getRootCtx())
	writeJSON(w, http.StatusOK, map[string]any{
		"status":  "success",
		"running": true,
		"message": "VPS monitor started",
	})
}

// HandleVPSStop handles POST /api/vps-monitor/stop.
func (s *Service) HandleVPSStop(w http.ResponseWriter, _ *http.Request) {
	s.StopVPSMonitor()
	writeJSON(w, http.StatusOK, map[string]any{
		"status":  "success",
		"running": false,
		"message": "VPS monitor stopped",
	})
}

// HandleSetVPSInterval handles PUT|POST /api/vps-monitor/interval (minimum 60 seconds).
func (s *Service) HandleSetVPSInterval(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Interval      int `json:"interval"`
		CheckInterval int `json:"check_interval"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 32<<10)).Decode(&body); err != nil {
		writeJSONError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid interval JSON payload.")
		return
	}
	sec := body.Interval
	if sec == 0 {
		sec = body.CheckInterval
	}
	if sec < defaultVPSIntervalSec {
		writeJSONError(w, http.StatusBadRequest, "INVALID_INTERVAL", "Check interval must be at least 60 seconds.")
		return
	}
	s.vpsIntervalSec.Store(int64(sec))
	writeJSON(w, http.StatusOK, map[string]any{
		"status":         "success",
		"check_interval": sec,
	})
}

// HandleListVPSSubs handles GET /api/vps-monitor/subscriptions.
func (s *Service) HandleListVPSSubs(w http.ResponseWriter, r *http.Request) {
	subs, err := s.store.ListVPSSubs(r.Context())
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "VPS_SUBS_LIST_FAILED", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, subs)
}

// HandleAddVPSSub handles POST /api/vps-monitor/subscriptions and auto-starts the VPS monitor.
func (s *Service) HandleAddVPSSub(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID                string   `json:"id"`
		PlanCode          string   `json:"planCode"`
		OVHSubsidiary     string   `json:"ovhSubsidiary"`
		Datacenters       []string `json:"datacenters"`
		MonitorLinux      *bool    `json:"monitorLinux"`
		MonitorWindows    *bool    `json:"monitorWindows"`
		NotifyAvailable   *bool    `json:"notifyAvailable"`
		NotifyUnavailable *bool    `json:"notifyUnavailable"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 256<<10)).Decode(&body); err != nil {
		writeJSONError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid VPS subscription JSON payload.")
		return
	}
	planCode := strings.TrimSpace(body.PlanCode)
	if planCode == "" {
		writeJSONError(w, http.StatusBadRequest, "MISSING_PLAN_CODE", "planCode is required.")
		return
	}
	subsidiary := strings.ToUpper(strings.TrimSpace(body.OVHSubsidiary))
	if subsidiary == "" {
		subsidiary = "IE"
	}

	monLinux := true
	if body.MonitorLinux != nil {
		monLinux = *body.MonitorLinux
	}
	monWin := false
	if body.MonitorWindows != nil {
		monWin = *body.MonitorWindows
	}
	notifyAvail := true
	if body.NotifyAvailable != nil {
		notifyAvail = *body.NotifyAvailable
	}
	notifyUnavail := false
	if body.NotifyUnavailable != nil {
		notifyUnavail = *body.NotifyUnavailable
	}

	dcs := cleanStrings(body.Datacenters)
	sub := store.VPSSubscription{
		ID:                strings.TrimSpace(body.ID),
		PlanCode:          planCode,
		OVHSubsidiary:     subsidiary,
		Datacenters:       dcs,
		MonitorLinux:      monLinux,
		MonitorWindows:    monWin,
		NotifyAvailable:   notifyAvail,
		NotifyUnavailable: notifyUnavail,
		LastStatus:        map[string]string{},
	}
	if sub.ID != "" {
		if existing, err := s.store.GetVPSSub(r.Context(), sub.ID); err == nil && existing != nil {
			sub.LastStatus = existing.LastStatus
			sub.CreatedAt = existing.CreatedAt
		}
	}

	saved, err := s.store.UpsertVPSSub(r.Context(), sub)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "VPS_SUB_SAVE_FAILED", err.Error())
		return
	}

	s.StartVPSMonitor(s.getRootCtx())
	writeJSON(w, http.StatusOK, map[string]any{
		"status":       "success",
		"subscription": saved,
	})
}

// HandleDeleteVPSSub handles DELETE /api/vps-monitor/subscriptions/{id} and auto-stops at 0 subs.
func (s *Service) HandleDeleteVPSSub(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	s.deleteVPSSubByID(w, r, id)
}

// HandleDeleteVPSSubFromBodyOrQuery handles DELETE /api/vps-monitor/subscriptions?id=... or JSON body {"id":...}.
func (s *Service) HandleDeleteVPSSubFromBodyOrQuery(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.URL.Query().Get("id"))
	if id == "" && r.Body != nil {
		var body struct {
			ID string `json:"id"`
		}
		_ = json.NewDecoder(io.LimitReader(r.Body, 32<<10)).Decode(&body)
		id = strings.TrimSpace(body.ID)
	}
	if id == "" {
		writeJSONError(w, http.StatusBadRequest, "MISSING_ID", "Subscription id is required.")
		return
	}
	s.deleteVPSSubByID(w, r, id)
}

func (s *Service) deleteVPSSubByID(w http.ResponseWriter, r *http.Request, id string) {
	err := s.store.DeleteVPSSub(r.Context(), id)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		writeJSONError(w, http.StatusInternalServerError, "VPS_SUB_DELETE_FAILED", err.Error())
		return
	}
	if subs, listErr := s.store.ListVPSSubs(r.Context()); listErr == nil && len(subs) == 0 {
		s.StopVPSMonitor()
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "success"})
}

// HandleClearVPSSubs handles DELETE|POST /api/vps-monitor/subscriptions/clear and stops the monitor.
func (s *Service) HandleClearVPSSubs(w http.ResponseWriter, r *http.Request) {
	cleared, err := s.store.ClearVPSSubs(r.Context())
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "VPS_SUBS_CLEAR_FAILED", err.Error())
		return
	}
	s.StopVPSMonitor()
	writeJSON(w, http.StatusOK, map[string]any{
		"status":  "success",
		"count":   cleared,
		"cleared": cleared,
	})
}

// HandleGetVPSHistory handles GET /api/vps-monitor/subscriptions/{id}/history.
func (s *Service) HandleGetVPSHistory(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	entries, err := s.store.GetVPSHistory(r.Context(), id)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "VPS_HISTORY_FAILED", err.Error())
		return
	}
	if entries == nil {
		entries = []store.VPSHistoryEntry{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id":      id,
		"history": entries,
	})
}

// HandleVPSCheckNow handles POST /api/vps-monitor/check/{planCode} and POST /api/vps-monitor/check-now.
func (s *Service) HandleVPSCheckNow(w http.ResponseWriter, r *http.Request) {
	planCode := strings.TrimSpace(chi.URLParam(r, "planCode"))
	transitions, err := s.CheckVPSOnce(r.Context(), planCode)
	if err != nil {
		writeJSONError(w, http.StatusBadGateway, "VPS_CHECK_FAILED", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":      "success",
		"transitions": transitions,
		"message":     "VPS check completed",
	})
}
