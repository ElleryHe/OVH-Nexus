package fleet

import (
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"ovh-nexus/server/internal/logger"
)

// HandleAccountInfo handles GET /api/ovh/account/info.
func (s *Service) HandleAccountInfo(w http.ResponseWriter, r *http.Request) {
	client, _, ok := s.requireOVHClient(w, r)
	if !ok {
		return
	}

	var info map[string]any
	if err := client.GetWithContext(r.Context(), "/me", &info); err != nil {
		s.logError(logger.SourceOVH, "Failed to fetch OVH account info", "error", formatOVHError(err))
		writeStatusError(w, http.StatusInternalServerError, "Failed to fetch account info: "+formatOVHError(err))
		return
	}

	s.logInfo(logger.SourceOVH, "Fetched OVH account info")
	writeJSON(w, http.StatusOK, map[string]any{
		"status": "success",
		"data":   info,
	})
}

// HandleAccountOrders handles GET /api/ovh/account/orders?limit=.
func (s *Service) HandleAccountOrders(w http.ResponseWriter, r *http.Request) {
	client, _, ok := s.requireOVHClient(w, r)
	if !ok {
		return
	}

	limit := 12
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil {
			if parsed < 1 {
				parsed = 1
			} else if parsed > 30 {
				parsed = 30
			}
			limit = parsed
		}
	}

	ctx := r.Context()
	var orderIDs []int64
	if err := client.GetWithContext(ctx, "/me/order", &orderIDs); err != nil {
		s.logError(logger.SourceOVH, "Failed to list OVH orders", "error", formatOVHError(err))
		writeStatusError(w, http.StatusInternalServerError, formatOVHError(err))
		return
	}

	sort.Slice(orderIDs, func(i, j int) bool { return orderIDs[i] > orderIDs[j] })
	if len(orderIDs) > limit {
		orderIDs = orderIDs[:limit]
	}

	orders := make([]map[string]any, 0, len(orderIDs))
	for _, oid := range orderIDs {
		var detail map[string]any
		if err := client.GetWithContext(ctx, fmt.Sprintf("/me/order/%d", oid), &detail); err != nil {
			continue
		}
		var status string
		if err := client.GetWithContext(ctx, fmt.Sprintf("/me/order/%d/status", oid), &status); err != nil || status == "" {
			status = "unknown"
		}
		var followUp []any
		if err := client.GetWithContext(ctx, fmt.Sprintf("/me/order/%d/followUp", oid), &followUp); err != nil || followUp == nil {
			followUp = []any{}
		}
		orderIDVal := detail["orderId"]
		if orderIDVal == nil {
			orderIDVal = oid
		}
		orders = append(orders, map[string]any{
			"orderId":         orderIDVal,
			"date":            detail["date"],
			"expirationDate":  detail["expirationDate"],
			"url":             detail["url"],
			"pdfUrl":          detail["pdfUrl"],
			"priceWithTax":    detail["priceWithTax"],
			"priceWithoutTax": detail["priceWithoutTax"],
			"tax":             detail["tax"],
			"status":          status,
			"followUp":        followUp,
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status": "success",
		"data":   orders,
	})
}

// HandleOrderFollowUp handles GET /api/ovh/account/orders/{orderId}/follow-up.
func (s *Service) HandleOrderFollowUp(w http.ResponseWriter, r *http.Request) {
	client, _, ok := s.requireOVHClient(w, r)
	if !ok {
		return
	}

	rawID := strings.TrimSpace(chi.URLParam(r, "orderId"))
	orderID, err := strconv.ParseInt(rawID, 10, 64)
	if err != nil || orderID <= 0 {
		writeStatusError(w, http.StatusBadRequest, "Invalid orderId parameter")
		return
	}

	ctx := r.Context()
	detail := map[string]any{}
	_ = client.GetWithContext(ctx, fmt.Sprintf("/me/order/%d", orderID), &detail)

	var status string
	if err := client.GetWithContext(ctx, fmt.Sprintf("/me/order/%d/status", orderID), &status); err != nil || status == "" {
		status = "unknown"
	}
	var followUp []any
	if err := client.GetWithContext(ctx, fmt.Sprintf("/me/order/%d/followUp", orderID), &followUp); err != nil || followUp == nil {
		followUp = []any{}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status": "success",
		"data": map[string]any{
			"orderId":        orderID,
			"status":         status,
			"orderStatus":    status,
			"date":           detail["date"],
			"expirationDate": detail["expirationDate"],
			"url":            detail["url"],
			"pdfUrl":         detail["pdfUrl"],
			"priceWithTax":   detail["priceWithTax"],
			"followUp":       followUp,
		},
	})
}

// HandleServicesOverview handles GET /api/ovh/account/services-overview.
func (s *Service) HandleServicesOverview(w http.ResponseWriter, r *http.Request) {
	client, _, ok := s.requireOVHClient(w, r)
	if !ok {
		return
	}

	ctx := r.Context()
	services := make([]map[string]any, 0)

	var dedicatedNames []string
	if err := client.GetWithContext(ctx, "/dedicated/server", &dedicatedNames); err == nil {
		for _, srv := range dedicatedNames {
			var info map[string]any
			if err := client.GetWithContext(ctx, "/dedicated/server/"+url.PathEscape(srv)+"/serviceInfos", &info); err != nil {
				continue
			}
			exp := info["expiration"]
			renewObj := asMap(info["renew"])
			status := asString(info["status"])
			if status == "" {
				status = "ok"
			}
			services = append(services, map[string]any{
				"serviceName":    srv,
				"displayName":    srv,
				"type":           "dedicated",
				"serviceType":    "dedicated",
				"status":         status,
				"creation":       info["creation"],
				"expiration":     exp,
				"daysRemaining":  daysUntil(exp),
				"renew":          renewObj,
				"renewMode":      computeRenewMode(renewObj),
				"renewPeriod":    renewObj["period"],
				"engagedUpTo":    info["engagedUpTo"],
				"contactAdmin":   info["contactAdmin"],
				"contactBilling": info["contactBilling"],
			})
		}
	}

	var vpsNames []string
	if err := client.GetWithContext(ctx, "/vps", &vpsNames); err == nil {
		for _, vps := range vpsNames {
			var info map[string]any
			if err := client.GetWithContext(ctx, "/vps/"+url.PathEscape(vps)+"/serviceInfos", &info); err != nil {
				continue
			}
			exp := info["expiration"]
			renewObj := asMap(info["renew"])
			status := asString(info["status"])
			if status == "" {
				status = "ok"
			}
			services = append(services, map[string]any{
				"serviceName":    vps,
				"displayName":    vps,
				"type":           "vps",
				"serviceType":    "vps",
				"status":         status,
				"creation":       info["creation"],
				"expiration":     exp,
				"daysRemaining":  daysUntil(exp),
				"renew":          renewObj,
				"renewMode":      computeRenewMode(renewObj),
				"renewPeriod":    renewObj["period"],
				"engagedUpTo":    info["engagedUpTo"],
				"contactAdmin":   info["contactAdmin"],
				"contactBilling": info["contactBilling"],
			})
		}
	}

	sort.SliceStable(services, func(i, j int) bool {
		di, _ := services[i]["daysRemaining"].(*int)
		dj, _ := services[j]["daysRemaining"].(*int)
		if di == nil && dj == nil {
			return false
		}
		if di == nil {
			return false
		}
		if dj == nil {
			return true
		}
		return *di < *dj
	})

	writeJSON(w, http.StatusOK, map[string]any{
		"status": "success",
		"data":   services,
	})
}

// HandleListSSHKeys handles GET /api/ovh/account/ssh-keys.
func (s *Service) HandleListSSHKeys(w http.ResponseWriter, r *http.Request) {
	client, _, ok := s.requireOVHClient(w, r)
	if !ok {
		return
	}

	ctx := r.Context()
	var names []string
	if err := client.GetWithContext(ctx, "/me/sshKey", &names); err != nil {
		s.logError(logger.SourceOVH, "Failed to list SSH keys", "error", formatOVHError(err))
		writeStatusError(w, http.StatusInternalServerError, formatOVHError(err))
		return
	}

	type sshKeyItem struct {
		KeyName string `json:"keyName"`
		Key     string `json:"key"`
		Default bool   `json:"default"`
	}

	keys := make([]sshKeyItem, 0, len(names))
	for _, name := range names {
		var info map[string]any
		if err := client.GetWithContext(ctx, "/me/sshKey/"+url.PathEscape(name), &info); err != nil {
			continue
		}
		kName := asString(info["keyName"])
		if kName == "" {
			kName = name
		}
		keys = append(keys, sshKeyItem{
			KeyName: kName,
			Key:     asString(info["key"]),
			Default: asBool(info["default"]),
		})
	}

	sort.Slice(keys, func(i, j int) bool {
		if keys[i].Default != keys[j].Default {
			return keys[i].Default
		}
		return keys[i].KeyName < keys[j].KeyName
	})

	writeJSON(w, http.StatusOK, map[string]any{
		"status": "success",
		"data":   keys,
	})
}

// HandleAddSSHKey handles POST /api/ovh/account/ssh-keys.
func (s *Service) HandleAddSSHKey(w http.ResponseWriter, r *http.Request) {
	client, _, ok := s.requireOVHClient(w, r)
	if !ok {
		return
	}

	var body struct {
		KeyName string `json:"keyName"`
		Key     string `json:"key"`
	}
	if err := decodeJSONBody(r, &body); err != nil {
		writeStatusError(w, http.StatusBadRequest, "Invalid JSON payload")
		return
	}
	keyName := strings.TrimSpace(body.KeyName)
	keyVal := strings.TrimSpace(body.Key)
	if keyName == "" || keyVal == "" {
		writeStatusError(w, http.StatusBadRequest, "keyName and key are required")
		return
	}

	if err := client.PostWithContext(r.Context(), "/me/sshKey", map[string]string{
		"keyName": keyName,
		"key":     keyVal,
	}, nil); err != nil {
		s.logError(logger.SourceOVH, "Failed to add SSH key", "keyName", keyName, "error", formatOVHError(err))
		writeStatusError(w, http.StatusInternalServerError, formatOVHError(err))
		return
	}

	s.logInfo(logger.SourceOVH, "Added OVH SSH key", "keyName", keyName)
	writeJSON(w, http.StatusOK, map[string]any{
		"status":  "success",
		"message": "SSH key added",
	})
}

// HandleUpdateSSHKey handles PUT /api/ovh/account/ssh-keys/{keyName}.
func (s *Service) HandleUpdateSSHKey(w http.ResponseWriter, r *http.Request) {
	client, _, ok := s.requireOVHClient(w, r)
	if !ok {
		return
	}

	keyName := strings.TrimSpace(unescapeWildcardParam(r))
	if keyName == "" {
		writeStatusError(w, http.StatusBadRequest, "keyName is required")
		return
	}

	var body struct {
		Default *bool `json:"default"`
	}
	_ = decodeJSONBody(r, &body)
	defFlag := true
	if body.Default != nil {
		defFlag = *body.Default
	}

	if err := client.PutWithContext(r.Context(), "/me/sshKey/"+url.PathEscape(keyName), map[string]bool{
		"default": defFlag,
	}, nil); err != nil {
		s.logError(logger.SourceOVH, "Failed to update SSH key", "keyName", keyName, "error", formatOVHError(err))
		writeStatusError(w, http.StatusInternalServerError, formatOVHError(err))
		return
	}

	s.logInfo(logger.SourceOVH, "Updated OVH SSH key", "keyName", keyName, "default", defFlag)
	writeJSON(w, http.StatusOK, map[string]any{
		"status":  "success",
		"message": "SSH key updated",
	})
}

// HandleDeleteSSHKey handles DELETE /api/ovh/account/ssh-keys/{keyName}.
func (s *Service) HandleDeleteSSHKey(w http.ResponseWriter, r *http.Request) {
	client, _, ok := s.requireOVHClient(w, r)
	if !ok {
		return
	}

	keyName := strings.TrimSpace(unescapeWildcardParam(r))
	if keyName == "" {
		writeStatusError(w, http.StatusBadRequest, "keyName is required")
		return
	}

	if err := client.DeleteWithContext(r.Context(), "/me/sshKey/"+url.PathEscape(keyName), nil); err != nil {
		s.logError(logger.SourceOVH, "Failed to delete SSH key", "keyName", keyName, "error", formatOVHError(err))
		writeStatusError(w, http.StatusInternalServerError, formatOVHError(err))
		return
	}

	s.logInfo(logger.SourceOVH, "Deleted OVH SSH key", "keyName", keyName)
	writeJSON(w, http.StatusOK, map[string]any{
		"status":  "success",
		"message": "SSH key deleted",
	})
}

// HandleListVPS handles GET /api/ovh/vps/list.
func (s *Service) HandleListVPS(w http.ResponseWriter, r *http.Request) {
	client, _, ok := s.requireOVHClient(w, r)
	if !ok {
		return
	}

	ctx := r.Context()
	var vpsNames []string
	if err := client.GetWithContext(ctx, "/vps", &vpsNames); err != nil {
		s.logError(logger.SourceOVH, "Failed to list VPS instances", "error", formatOVHError(err))
		writeStatusError(w, http.StatusInternalServerError, formatOVHError(err))
		return
	}

	vpsList := make([]map[string]any, 0, len(vpsNames))
	for _, name := range vpsNames {
		enc := url.PathEscape(name)
		var detail map[string]any
		if err := client.GetWithContext(ctx, "/vps/"+enc, &detail); err != nil {
			continue
		}
		var ips []string
		if err := client.GetWithContext(ctx, "/vps/"+enc+"/ips", &ips); err != nil || ips == nil {
			ips = []string{}
		}
		var svc map[string]any
		_ = client.GetWithContext(ctx, "/vps/"+enc+"/serviceInfos", &svc)

		model := asMap(detail["model"])
		exp := svc["expiration"]
		renewObj := asMap(svc["renew"])

		modelStr := asString(model["offer"])
		if modelStr == "" {
			modelStr = asString(model["name"])
		}
		memMB := detail["memoryLimit"]
		if memMB == nil {
			memMB = model["memory"]
		}
		diskGB := model["disk"]
		vcore := detail["vcore"]
		if vcore == nil {
			vcore = model["vcore"]
		}
		displayName := asString(detail["displayName"])
		if displayName == "" {
			displayName = name
		}
		state := asString(detail["state"])
		if state == "" {
			state = "unknown"
		}

		vpsList = append(vpsList, map[string]any{
			"serviceName":   name,
			"displayName":   displayName,
			"state":         state,
			"zone":          asString(detail["zone"]),
			"vcore":         vcore,
			"memoryLimit":   memMB,
			"memoryLimitMb": memMB,
			"disk":          diskGB,
			"diskGb":        diskGB,
			"model":         modelStr,
			"modelName":     modelStr,
			"ips":           ips,
			"expiration":    exp,
			"daysRemaining": daysUntil(exp),
			"renew":         renewObj,
			"renewMode":     computeRenewMode(renewObj),
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status": "success",
		"data":   vpsList,
	})
}

// HandleRebootVPS handles POST /api/ovh/vps/{serviceName}/reboot.
func (s *Service) HandleRebootVPS(w http.ResponseWriter, r *http.Request) {
	client, _, ok := s.requireOVHClient(w, r)
	if !ok {
		return
	}

	serviceName := strings.TrimSpace(chi.URLParam(r, "serviceName"))
	if serviceName == "" {
		writeStatusError(w, http.StatusBadRequest, "serviceName is required")
		return
	}

	if err := client.PostWithContext(r.Context(), "/vps/"+url.PathEscape(serviceName)+"/reboot", nil, nil); err != nil {
		s.logError(logger.SourceOVH, "Failed to reboot VPS instance", "serviceName", serviceName, "error", formatOVHError(err))
		writeStatusError(w, http.StatusInternalServerError, formatOVHError(err))
		return
	}

	s.logInfo(logger.SourceOVH, "Triggered VPS reboot", "serviceName", serviceName)
	writeJSON(w, http.StatusOK, map[string]any{
		"status":  "success",
		"message": fmt.Sprintf("VPS %s reboot initiated", serviceName),
	})
}

// HandleAccountRefunds handles GET /api/ovh/account/refunds.
func (s *Service) HandleAccountRefunds(w http.ResponseWriter, r *http.Request) {
	client, _, ok := s.requireOVHClient(w, r)
	if !ok {
		return
	}

	ctx := r.Context()
	var refundIDs []string
	if err := client.GetWithContext(ctx, "/me/refund", &refundIDs); err != nil {
		s.logError(logger.SourceOVH, "Failed to fetch refunds", "error", formatOVHError(err))
		writeStatusError(w, http.StatusInternalServerError, "Failed to fetch refunds: "+formatOVHError(err))
		return
	}

	if len(refundIDs) > 20 {
		refundIDs = refundIDs[:20]
	}

	refunds := make([]map[string]any, 0, len(refundIDs))
	for _, rid := range refundIDs {
		var detail map[string]any
		if err := client.GetWithContext(ctx, "/me/refund/"+url.PathEscape(rid), &detail); err != nil {
			s.logWarn(logger.SourceOVH, "Failed to fetch refund detail", "refundId", rid, "error", formatOVHError(err))
			continue
		}
		refunds = append(refunds, detail)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status": "success",
		"data":   refunds,
	})
}

// HandleEmailHistory handles GET /api/ovh/account/email-history.
func (s *Service) HandleEmailHistory(w http.ResponseWriter, r *http.Request) {
	client, _, ok := s.requireOVHClient(w, r)
	if !ok {
		return
	}

	ctx := r.Context()
	var emailIDs []int64
	if err := client.GetWithContext(ctx, "/me/notification/email/history", &emailIDs); err != nil {
		s.logError(logger.SourceOVH, "Failed to fetch email history", "error", formatOVHError(err))
		writeStatusError(w, http.StatusInternalServerError, "Failed to fetch email history: "+formatOVHError(err))
		return
	}

	// Reverse so newest IDs come first, cap at 50.
	emails := make([]map[string]any, 0, min(len(emailIDs), 50))
	for i := len(emailIDs) - 1; i >= 0 && len(emails) < 50; i-- {
		eid := emailIDs[i]
		var detail map[string]any
		if err := client.GetWithContext(ctx, fmt.Sprintf("/me/notification/email/history/%d", eid), &detail); err != nil {
			s.logWarn(logger.SourceOVH, "Failed to fetch email detail", "emailId", eid, "error", formatOVHError(err))
			continue
		}
		emails = append(emails, detail)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status": "success",
		"data":   emails,
	})
}

// HandleCreditBalance handles GET /api/ovh/account/credit-balance.
func (s *Service) HandleCreditBalance(w http.ResponseWriter, r *http.Request) {
	client, _, ok := s.requireOVHClient(w, r)
	if !ok {
		return
	}

	ctx := r.Context()
	var names []string
	if err := client.GetWithContext(ctx, "/me/credit/balance", &names); err != nil {
		s.logError(logger.SourceOVH, "Failed to fetch credit balance", "error", formatOVHError(err))
		writeStatusError(w, http.StatusInternalServerError, "Failed to fetch credit balance: "+formatOVHError(err))
		return
	}

	balances := make([]map[string]any, 0, len(names))
	for _, name := range names {
		var detail map[string]any
		if err := client.GetWithContext(ctx, "/me/credit/balance/"+url.PathEscape(name), &detail); err != nil {
			s.logWarn(logger.SourceOVH, "Failed to fetch credit balance detail", "balance", name, "error", formatOVHError(err))
			continue
		}
		balances = append(balances, detail)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status": "success",
		"data":   balances,
	})
}

// HandleSubAccounts handles GET /api/ovh/account/sub-accounts.
func (s *Service) HandleSubAccounts(w http.ResponseWriter, r *http.Request) {
	client, _, ok := s.requireOVHClient(w, r)
	if !ok {
		return
	}

	ctx := r.Context()
	var subIDs []int64
	if err := client.GetWithContext(ctx, "/me/subAccount", &subIDs); err != nil {
		s.logError(logger.SourceOVH, "Failed to list sub-accounts", "error", formatOVHError(err))
		writeStatusError(w, http.StatusInternalServerError, "Failed to list sub-accounts: "+formatOVHError(err))
		return
	}

	subs := make([]map[string]any, 0, len(subIDs))
	for _, sid := range subIDs {
		var detail map[string]any
		if err := client.GetWithContext(ctx, fmt.Sprintf("/me/subAccount/%d", sid), &detail); err != nil {
			continue
		}
		subs = append(subs, detail)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status": "success",
		"data":   subs,
	})
}

// HandleAccountBills handles GET /api/ovh/account/bills.
func (s *Service) HandleAccountBills(w http.ResponseWriter, r *http.Request) {
	client, _, ok := s.requireOVHClient(w, r)
	if !ok {
		return
	}

	ctx := r.Context()
	var billIDs []string
	if err := client.GetWithContext(ctx, "/me/bill", &billIDs); err != nil {
		s.logError(logger.SourceOVH, "Failed to list bills", "error", formatOVHError(err))
		writeStatusError(w, http.StatusInternalServerError, "Failed to list bills: "+formatOVHError(err))
		return
	}

	if len(billIDs) > 20 {
		billIDs = billIDs[:20]
	}

	bills := make([]map[string]any, 0, len(billIDs))
	for _, bid := range billIDs {
		var detail map[string]any
		if err := client.GetWithContext(ctx, "/me/bill/"+url.PathEscape(bid), &detail); err != nil {
			continue
		}
		bills = append(bills, detail)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status": "success",
		"data":   bills,
	})
}

// HandleListContactChangeRequests handles GET /api/ovh/contact-change-requests.
func (s *Service) HandleListContactChangeRequests(w http.ResponseWriter, r *http.Request) {
	client, _, ok := s.requireOVHClient(w, r)
	if !ok {
		return
	}

	ctx := r.Context()
	var taskIDs []int64
	if err := client.GetWithContext(ctx, "/me/task/contactChange", &taskIDs); err != nil {
		s.logError(logger.SourceOVH, "Failed to list contact change requests", "error", formatOVHError(err))
		writeStatusError(w, http.StatusInternalServerError, "Failed to list contact change requests: "+formatOVHError(err))
		return
	}

	tasks := make([]map[string]any, 0, len(taskIDs))
	for _, tid := range taskIDs {
		var detail map[string]any
		if err := client.GetWithContext(ctx, fmt.Sprintf("/me/task/contactChange/%d", tid), &detail); err != nil {
			s.logWarn(logger.SourceOVH, "Failed to fetch contact change request detail", "taskId", tid, "error", formatOVHError(err))
			continue
		}
		tasks = append(tasks, detail)
	}

	sort.SliceStable(tasks, func(i, j int) bool {
		return asString(tasks[i]["dateRequest"]) > asString(tasks[j]["dateRequest"])
	})

	writeJSON(w, http.StatusOK, map[string]any{
		"status": "success",
		"data":   tasks,
	})
}

// HandleGetContactChangeRequest handles GET /api/ovh/contact-change-requests/{taskId}.
func (s *Service) HandleGetContactChangeRequest(w http.ResponseWriter, r *http.Request) {
	client, _, ok := s.requireOVHClient(w, r)
	if !ok {
		return
	}

	taskID, err := strconv.ParseInt(chi.URLParam(r, "taskId"), 10, 64)
	if err != nil {
		writeStatusError(w, http.StatusBadRequest, "Invalid taskId")
		return
	}

	var detail map[string]any
	if err := client.GetWithContext(r.Context(), fmt.Sprintf("/me/task/contactChange/%d", taskID), &detail); err != nil {
		s.logError(logger.SourceOVH, "Failed to get contact change request", "taskId", taskID, "error", formatOVHError(err))
		writeStatusError(w, http.StatusInternalServerError, "Failed to fetch contact change request: "+formatOVHError(err))
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status": "success",
		"data":   detail,
	})
}

// HandleAcceptContactChange handles POST /api/ovh/contact-change-requests/{taskId}/accept.
func (s *Service) HandleAcceptContactChange(w http.ResponseWriter, r *http.Request) {
	s.handleContactChangeAction(w, r, "accept", "Contact change request accepted")
}

// HandleRefuseContactChange handles POST /api/ovh/contact-change-requests/{taskId}/refuse.
func (s *Service) HandleRefuseContactChange(w http.ResponseWriter, r *http.Request) {
	s.handleContactChangeAction(w, r, "refuse", "Contact change request refused")
}

func (s *Service) handleContactChangeAction(w http.ResponseWriter, r *http.Request, action, successMsg string) {
	client, _, ok := s.requireOVHClient(w, r)
	if !ok {
		return
	}

	taskID, err := strconv.ParseInt(chi.URLParam(r, "taskId"), 10, 64)
	if err != nil {
		writeStatusError(w, http.StatusBadRequest, "Invalid taskId")
		return
	}

	var body struct {
		Token string `json:"token"`
	}
	if err := decodeJSONBody(r, &body); err != nil {
		writeStatusError(w, http.StatusBadRequest, "Invalid JSON payload")
		return
	}
	token := strings.TrimSpace(body.Token)
	if token == "" {
		writeStatusError(w, http.StatusBadRequest, "Confirmation token is required")
		return
	}

	if err := client.PostWithContext(r.Context(), fmt.Sprintf("/me/task/contactChange/%d/%s", taskID, action), map[string]string{
		"token": token,
	}, nil); err != nil {
		s.logError(logger.SourceOVH, "Contact change action failed", "taskId", taskID, "action", action, "error", formatOVHError(err))
		writeStatusError(w, http.StatusInternalServerError, formatOVHError(err))
		return
	}

	s.logInfo(logger.SourceOVH, "Processed contact change request", "taskId", taskID, "action", action)
	writeJSON(w, http.StatusOK, map[string]any{
		"status":  "success",
		"message": successMsg,
	})
}

// HandleResendContactChangeEmail handles POST /api/ovh/contact-change-requests/{taskId}/resend-email.
func (s *Service) HandleResendContactChangeEmail(w http.ResponseWriter, r *http.Request) {
	client, _, ok := s.requireOVHClient(w, r)
	if !ok {
		return
	}

	taskID, err := strconv.ParseInt(chi.URLParam(r, "taskId"), 10, 64)
	if err != nil {
		writeStatusError(w, http.StatusBadRequest, "Invalid taskId")
		return
	}

	if err := client.PostWithContext(r.Context(), fmt.Sprintf("/me/task/contactChange/%d/resendEmail", taskID), nil, nil); err != nil {
		s.logError(logger.SourceOVH, "Failed to resend contact change email", "taskId", taskID, "error", formatOVHError(err))
		writeStatusError(w, http.StatusInternalServerError, formatOVHError(err))
		return
	}

	s.logInfo(logger.SourceOVH, "Resent contact change confirmation email", "taskId", taskID)
	writeJSON(w, http.StatusOK, map[string]any{
		"status":  "success",
		"message": "Confirmation email resent",
	})
}
