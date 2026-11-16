package procurement

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"ovh-nexus/server/internal/store"
)

// MountRoutes registers queue, quick-order, history, and stats routes on an /api chi.Router.
func (s *Service) MountRoutes(r chi.Router) {
	r.Get("/queue", s.HandleListQueue)
	r.Get("/queue/all", s.HandleListAllQueue)
	r.Post("/queue", s.HandleCreateQueue)
	r.Delete("/queue/clear", s.HandleClearQueue)
	r.Post("/queue/clear", s.HandleClearQueue)
	r.Put("/queue/{id}", s.HandleUpdateQueue)
	r.Put("/queue/{id}/status", s.HandleUpdateQueueStatus)
	r.Delete("/queue/{id}", s.HandleDeleteQueue)

	r.Post("/orders/quick-order", s.HandleQuickOrder)

	r.Get("/history", s.HandleListHistory)
	r.Delete("/history", s.HandleClearHistory)
	r.Get("/purchase-history", s.HandleListHistory)
	r.Delete("/purchase-history", s.HandleClearHistory)

	r.Get("/stats", s.HandleGetStats)
	r.Get("/stats/accounts", s.HandleGetAccountStats)
}

// HandleListQueue handles GET /api/queue.
func (s *Service) HandleListQueue(w http.ResponseWriter, r *http.Request) {
	accountID := strings.TrimSpace(r.Header.Get("X-OVH-Account"))
	items, err := s.store.ListQueue(r.Context(), accountID)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "QUEUE_LIST_FAILED", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, items)
}

// HandleListAllQueue handles GET /api/queue/all.
func (s *Service) HandleListAllQueue(w http.ResponseWriter, r *http.Request) {
	items, err := s.store.ListQueue(r.Context(), "")
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "QUEUE_LIST_FAILED", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, items)
}

// HandleCreateQueue handles POST /api/queue. auto_pay and waive_retractation_period default to false.
func (s *Service) HandleCreateQueue(w http.ResponseWriter, r *http.Request) {
	var raw map[string]any
	if err := json.NewDecoder(io.LimitReader(r.Body, 512<<10)).Decode(&raw); err != nil {
		writeJSONError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid queue item JSON payload.")
		return
	}

	accountID := strings.TrimSpace(r.Header.Get("X-OVH-Account"))
	if accountID == "" {
		accountID = stringFromAny(raw["accountId"])
	}

	req := CreateQueueRequest{
		AccountID:               accountID,
		PlanCode:                stringFromAny(raw["planCode"]),
		Datacenters:             stringsFromAny(raw["datacenters"]),
		Options:                 stringsFromAny(raw["options"]),
		Quantity:                intFromAny(raw["quantity"], 1),
		MaxRetryCount:           intFromAny(raw["maxRetryCount"], 50),
		RetryInterval:           intFromAny(raw["retryInterval"], 30),
		AutoPay:                 boolFromAnyKeys(raw, "auto_pay", "autoPay"),
		WaiveRetractationPeriod: boolFromAnyKeys(raw, "waive_retractation_period", "waiveRetractationPeriod"),
		Source:                  stringFromAny(raw["source"]),
		SourceRef:               stringFromAny(raw["sourceRef"]),
	}
	if len(req.Datacenters) == 0 {
		if dc := stringFromAny(raw["datacenter"]); dc != "" {
			req.Datacenters = []string{dc}
		}
	}

	created, err := s.CreateQueueTask(r.Context(), req)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "QUEUE_CREATE_FAILED", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status": "success",
		"id":     created.ID,
		"item":   created,
	})
}

// HandleUpdateQueue handles PUT /api/queue/{id}.
func (s *Service) HandleUpdateQueue(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	var raw map[string]any
	if err := json.NewDecoder(io.LimitReader(r.Body, 512<<10)).Decode(&raw); err != nil {
		writeJSONError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid queue update JSON payload.")
		return
	}

	var req UpdateQueueRequest
	if v, ok := raw["planCode"]; ok {
		str := stringFromAny(v)
		req.PlanCode = &str
	}
	if v, ok := raw["datacenters"]; ok {
		req.Datacenters = stringsFromAny(v)
		req.HasDatacenters = true
	}
	if v, ok := raw["options"]; ok {
		req.Options = stringsFromAny(v)
		req.HasOptions = true
	}
	if v, ok := raw["retryInterval"]; ok {
		n := intFromAny(v, 0)
		req.RetryInterval = &n
	}
	if v, ok := raw["quantity"]; ok {
		n := intFromAny(v, 1)
		req.Quantity = &n
	}
	if v, ok := firstPresent(raw, "auto_pay", "autoPay"); ok {
		b := asBool(v)
		req.AutoPay = &b
	}
	if v, ok := firstPresent(raw, "waive_retractation_period", "waiveRetractationPeriod"); ok {
		b := asBool(v)
		req.WaiveRetractationPeriod = &b
	}

	updated, err := s.UpdateQueueTask(r.Context(), id, req)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeJSONError(w, http.StatusNotFound, "QUEUE_NOT_FOUND", "Queue task not found.")
			return
		}
		writeJSONError(w, http.StatusBadRequest, "QUEUE_UPDATE_FAILED", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status": "success",
		"item":   updated,
	})
}

// HandleUpdateQueueStatus handles PUT /api/queue/{id}/status.
func (s *Service) HandleUpdateQueueStatus(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	var body struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 64<<10)).Decode(&body); err != nil {
		writeJSONError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid status payload.")
		return
	}

	item, err := s.store.GetQueueItem(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeJSONError(w, http.StatusNotFound, "QUEUE_NOT_FOUND", "Queue task not found.")
			return
		}
		writeJSONError(w, http.StatusInternalServerError, "QUEUE_LOAD_FAILED", err.Error())
		return
	}

	nextStatus := strings.ToLower(strings.TrimSpace(body.Status))
	if nextStatus == "" {
		nextStatus = "pending"
	}
	item.Status = nextStatus
	if _, err := s.store.UpdateQueueItem(r.Context(), *item); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "QUEUE_STATUS_FAILED", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status": "success",
	})
}

// HandleDeleteQueue handles DELETE /api/queue/{id}.
func (s *Service) HandleDeleteQueue(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	err := s.store.DeleteQueueItem(r.Context(), id)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		writeJSONError(w, http.StatusInternalServerError, "QUEUE_DELETE_FAILED", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status": "success",
	})
}

// HandleClearQueue handles DELETE|POST /api/queue/clear.
func (s *Service) HandleClearQueue(w http.ResponseWriter, r *http.Request) {
	accountID := strings.TrimSpace(r.Header.Get("X-OVH-Account"))
	count, err := s.store.ClearQueue(r.Context(), accountID)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "QUEUE_CLEAR_FAILED", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status": "success",
		"count":  count,
	})
}

// HandleQuickOrder handles POST /api/orders/quick-order.
func (s *Service) HandleQuickOrder(w http.ResponseWriter, r *http.Request) {
	var raw map[string]any
	if err := json.NewDecoder(io.LimitReader(r.Body, 512<<10)).Decode(&raw); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"success": false,
			"code":    "INVALID_JSON",
			"error":   "Invalid quick-order JSON payload",
			"message": "Invalid quick-order JSON payload",
		})
		return
	}

	accountID := strings.TrimSpace(r.Header.Get("X-OVH-Account"))
	if accountID == "" {
		accountID = stringFromAny(raw["accountId"])
	}

	res, statusCode, _ := s.QuickOrder(r.Context(), QuickOrderRequest{
		AccountID:               accountID,
		PlanCode:                stringFromAny(raw["planCode"]),
		Datacenter:              stringFromAny(raw["datacenter"]),
		Options:                 stringsFromAny(raw["options"]),
		SkipPriceCheck:          boolFromAnyKeys(raw, "skipPriceCheck"),
		SkipDuplicateCheck:      boolFromAnyKeys(raw, "skipDuplicateCheck"),
		AutoPay:                 boolFromAnyKeys(raw, "auto_pay", "autoPay"),
		WaiveRetractationPeriod: boolFromAnyKeys(raw, "waive_retractation_period", "waiveRetractationPeriod"),
		Source:                  stringFromAny(raw["source"]),
		SourceRef:               stringFromAny(raw["sourceRef"]),
	})

	if !res.Success {
		writeJSON(w, statusCode, map[string]any{
			"success": false,
			"code":    res.Code,
			"error":   res.Error,
			"message": res.Error,
		})
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// HandleListHistory handles GET /api/history and GET /api/purchase-history.
func (s *Service) HandleListHistory(w http.ResponseWriter, r *http.Request) {
	accountID := strings.TrimSpace(r.Header.Get("X-OVH-Account"))
	items, err := s.store.ListHistory(r.Context(), accountID)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "HISTORY_LIST_FAILED", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, items)
}

// HandleClearHistory handles DELETE /api/history and DELETE /api/purchase-history.
func (s *Service) HandleClearHistory(w http.ResponseWriter, r *http.Request) {
	accountID := strings.TrimSpace(r.Header.Get("X-OVH-Account"))
	count, err := s.store.ClearHistory(r.Context(), accountID)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "HISTORY_CLEAR_FAILED", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status": "success",
		"count":  count,
	})
}

// HandleGetStats handles GET /api/stats.
func (s *Service) HandleGetStats(w http.ResponseWriter, r *http.Request) {
	stats, err := s.store.ComputeStats(r.Context(), "")
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "STATS_FAILED", err.Error())
		return
	}
	if s.catalog != nil {
		total, avail := s.catalog.GetCachedServerCounts()
		stats.TotalServers = total
		stats.AvailableServers = avail
	}
	stats.QueueProcessorRunning = s.IsRunning()
	stats.MonitorRunning = s.isMonitorRunning()
	writeJSON(w, http.StatusOK, stats)
}

// HandleGetAccountStats handles GET /api/stats/accounts.
func (s *Service) HandleGetAccountStats(w http.ResponseWriter, r *http.Request) {
	accounts, err := s.store.ListAccounts(r.Context())
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "ACCOUNT_STATS_FAILED", err.Error())
		return
	}
	var totalSrv, availSrv int
	if s.catalog != nil {
		totalSrv, availSrv = s.catalog.GetCachedServerCounts()
	}

	out := make(map[string]*store.Stats, len(accounts)+1)
	if len(accounts) == 0 {
		st, _ := s.store.ComputeStats(r.Context(), "")
		if st != nil {
			st.TotalServers = totalSrv
			st.AvailableServers = availSrv
			st.QueueProcessorRunning = s.IsRunning()
			st.MonitorRunning = s.isMonitorRunning()
			out["default"] = st
		}
	} else {
		for _, acc := range accounts {
			st, _ := s.store.ComputeStats(r.Context(), acc.ID)
			if st != nil {
				st.TotalServers = totalSrv
				st.AvailableServers = availSrv
				st.QueueProcessorRunning = s.IsRunning()
				st.MonitorRunning = s.isMonitorRunning()
				out[acc.ID] = st
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"accounts": out})
}

func stringFromAny(v any) string {
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(s)
}

func stringsFromAny(v any) []string {
	switch t := v.(type) {
	case []string:
		return cleanStringList(t)
	case []any:
		out := make([]string, 0, len(t))
		for _, item := range t {
			if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
				out = append(out, strings.TrimSpace(s))
			}
		}
		return out
	case string:
		var out []string
		for _, part := range strings.Split(t, ",") {
			if trimmed := strings.TrimSpace(part); trimmed != "" {
				out = append(out, trimmed)
			}
		}
		return out
	default:
		return nil
	}
}

func intFromAny(v any, fallback int) int {
	switch t := v.(type) {
	case int:
		return t
	case int64:
		return int(t)
	case float64:
		return int(t)
	default:
		return fallback
	}
}

func firstPresent(m map[string]any, keys ...string) (any, bool) {
	for _, k := range keys {
		if val, ok := m[k]; ok {
			return val, true
		}
	}
	return nil, false
}

func boolFromAnyKeys(m map[string]any, keys ...string) bool {
	if val, ok := firstPresent(m, keys...); ok {
		return asBool(val)
	}
	return false
}

func asBool(v any) bool {
	switch b := v.(type) {
	case bool:
		return b
	case string:
		low := strings.ToLower(strings.TrimSpace(b))
		return low == "true" || low == "1" || low == "yes"
	case float64:
		return b != 0
	case int:
		return b != 0
	default:
		return false
	}
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeJSONError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{
		"success": false,
		"error":   http.StatusText(status),
		"code":    code,
		"message": message,
	})
}
