package fleet

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"ovh-nexus/server/internal/logger"
)

// HandleGetNetworkSpecs handles GET /api/server-control/{serviceName}/network-specs.
func (s *Service) HandleGetNetworkSpecs(w http.ResponseWriter, r *http.Request) {
	client, _, ok := s.requireOVHClient(w, r)
	if !ok {
		return
	}
	srv, ok := requireServiceName(w, r)
	if !ok {
		return
	}

	var netInfo map[string]any
	if err := client.GetWithContext(r.Context(), servicePath(srv)+"/specifications/network", &netInfo); err != nil {
		writeSuccessError(w, http.StatusInternalServerError, formatOVHError(err))
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"network": map[string]any{
			"bandwidth":  asMap(netInfo["bandwidth"]),
			"connection": asMap(netInfo["connection"]),
			"ola":        asMap(netInfo["ola"]),
			"routing":    asMap(netInfo["routing"]),
			"traffic":    asMap(netInfo["traffic"]),
			"switching":  asMap(netInfo["switching"]),
			"vmac":       asMap(netInfo["vmac"]),
			"vrack":      asMap(netInfo["vrack"]),
		},
	})
}

// HandleGetNetworkInterfaces handles GET /api/server-control/{serviceName}/network-interfaces.
func (s *Service) HandleGetNetworkInterfaces(w http.ResponseWriter, r *http.Request) {
	client, _, ok := s.requireOVHClient(w, r)
	if !ok {
		return
	}
	srv, ok := requireServiceName(w, r)
	if !ok {
		return
	}

	ctx := r.Context()
	enc := servicePath(srv)
	var macs []string
	if err := client.GetWithContext(ctx, enc+"/networkInterfaceController", &macs); err != nil {
		if isOVHNotFound(err) {
			writeJSON(w, http.StatusOK, map[string]any{
				"success":    true,
				"interfaces": []any{},
				"count":      0,
				"message":    "No network interfaces found for this server",
			})
			return
		}
		writeSuccessError(w, http.StatusInternalServerError, formatOVHError(err))
		return
	}

	interfaces := make([]map[string]any, 0, len(macs))
	for _, mac := range macs {
		var detail map[string]any
		if err := client.GetWithContext(ctx, enc+"/networkInterfaceController/"+url.PathEscape(mac), &detail); err != nil {
			interfaces = append(interfaces, map[string]any{
				"mac":      mac,
				"linkType": "unknown",
				"error":    formatOVHError(err),
			})
			continue
		}
		interfaces = append(interfaces, map[string]any{
			"mac":                     mac,
			"linkType":                detail["linkType"],
			"virtualNetworkInterface": detail["virtualNetworkInterface"],
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success":    true,
		"interfaces": interfaces,
		"count":      len(interfaces),
	})
}

// HandleGetNetworkStats handles GET /api/server-control/{serviceName}/network-stats.
func (s *Service) HandleGetNetworkStats(w http.ResponseWriter, r *http.Request) {
	client, _, ok := s.requireOVHClient(w, r)
	if !ok {
		return
	}
	srv, ok := requireServiceName(w, r)
	if !ok {
		return
	}

	ctx := r.Context()
	enc := servicePath(srv)
	var macs []string
	if err := client.GetWithContext(ctx, enc+"/networkInterfaceController", &macs); err != nil {
		writeSuccessError(w, http.StatusInternalServerError, formatOVHError(err))
		return
	}

	interfaces := make([]map[string]any, 0, len(macs))
	for _, mac := range macs {
		var detail map[string]any
		if err := client.GetWithContext(ctx, enc+"/networkInterfaceController/"+url.PathEscape(mac), &detail); err == nil {
			interfaces = append(interfaces, detail)
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success":    true,
		"interfaces": interfaces,
	})
}

// HandleGetMRTG handles GET /api/server-control/{serviceName}/mrtg.
func (s *Service) HandleGetMRTG(w http.ResponseWriter, r *http.Request) {
	client, _, ok := s.requireOVHClient(w, r)
	if !ok {
		return
	}
	srv, ok := requireServiceName(w, r)
	if !ok {
		return
	}

	period := strings.TrimSpace(r.URL.Query().Get("period"))
	if period == "" {
		period = "daily"
	}
	trafficType := strings.TrimSpace(r.URL.Query().Get("type"))
	if trafficType == "" {
		trafficType = "traffic:download"
	}

	q := url.Values{}
	q.Set("period", period)
	q.Set("type", trafficType)
	qs := q.Encode()

	ctx := r.Context()
	enc := servicePath(srv)
	var macs []string
	if err := client.GetWithContext(ctx, enc+"/networkInterfaceController", &macs); err != nil {
		var legacyData any
		if legErr := client.GetWithContext(ctx, enc+"/mrtg?"+qs, &legacyData); legErr != nil {
			writeSuccessError(w, http.StatusInternalServerError, formatOVHError(legErr))
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"success":    true,
			"data":       legacyData,
			"period":     period,
			"type":       trafficType,
			"interfaces": []any{},
		})
		return
	}

	allData := make([]map[string]any, 0, len(macs))
	for _, mac := range macs {
		var data any
		if err := client.GetWithContext(ctx, enc+"/networkInterfaceController/"+url.PathEscape(mac)+"/mrtg?"+qs, &data); err != nil {
			allData = append(allData, map[string]any{
				"mac":   mac,
				"data":  []any{},
				"error": formatOVHError(err),
			})
			continue
		}
		allData = append(allData, map[string]any{
			"mac":  mac,
			"data": data,
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success":    true,
		"interfaces": allData,
		"period":     period,
		"type":       trafficType,
		"server":     srv,
	})
}

// HandleGetStatistics handles GET /api/server-control/{serviceName}/statistics.
func (s *Service) HandleGetStatistics(w http.ResponseWriter, r *http.Request) {
	client, _, ok := s.requireOVHClient(w, r)
	if !ok {
		return
	}
	srv, ok := requireServiceName(w, r)
	if !ok {
		return
	}

	period := strings.TrimSpace(r.URL.Query().Get("period"))
	if period == "" {
		period = "lastday"
	}
	statType := strings.TrimSpace(r.URL.Query().Get("type"))
	if statType == "" {
		statType = "traffic:download"
	}
	q := url.Values{}
	q.Set("period", period)
	q.Set("type", statType)

	var stats any
	if err := client.GetWithContext(r.Context(), servicePath(srv)+"/statistics?"+q.Encode(), &stats); err != nil {
		writeSuccessError(w, http.StatusInternalServerError, "Statistics API is unavailable for this server: "+formatOVHError(err))
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success":    true,
		"statistics": stats,
		"period":     period,
		"type":       statType,
	})
}

// HandleGetServerIPs handles GET /api/server-control/{serviceName}/ips.
func (s *Service) HandleGetServerIPs(w http.ResponseWriter, r *http.Request) {
	client, _, ok := s.requireOVHClient(w, r)
	if !ok {
		return
	}
	srv, ok := requireServiceName(w, r)
	if !ok {
		return
	}

	ctx := r.Context()
	var ipList []string
	if err := client.GetWithContext(ctx, servicePath(srv)+"/ips", &ipList); err != nil {
		writeSuccessError(w, http.StatusInternalServerError, formatOVHError(err))
		return
	}

	ips := make([]map[string]any, 0, len(ipList))
	for _, ipBlock := range ipList {
		var detail map[string]any
		if err := client.GetWithContext(ctx, "/ip/"+url.PathEscape(ipBlock), &detail); err != nil {
			ips = append(ips, map[string]any{"ip": ipBlock, "type": "unknown"})
			continue
		}
		ipType := asString(detail["type"])
		if ipType == "" {
			ipType = "N/A"
		}
		routed := asMap(detail["routedTo"])
		ips = append(ips, map[string]any{
			"ip":          ipBlock,
			"type":        ipType,
			"description": asString(detail["description"]),
			"routedTo":    asString(routed["serviceName"]),
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"ips":     ips,
		"total":   len(ips),
	})
}

// HandleGetIPSpecs handles GET /api/server-control/{serviceName}/ip-specs.
func (s *Service) HandleGetIPSpecs(w http.ResponseWriter, r *http.Request) {
	client, _, ok := s.requireOVHClient(w, r)
	if !ok {
		return
	}
	srv, ok := requireServiceName(w, r)
	if !ok {
		return
	}

	var ipSpecs any
	if err := client.GetWithContext(r.Context(), servicePath(srv)+"/specifications/ip", &ipSpecs); err != nil {
		writeSuccessError(w, http.StatusInternalServerError, formatOVHError(err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"ipSpecs": ipSpecs,
	})
}

// HandleGetReverseDNS handles GET /api/server-control/{serviceName}/reverse.
func (s *Service) HandleGetReverseDNS(w http.ResponseWriter, r *http.Request) {
	client, _, ok := s.requireOVHClient(w, r)
	if !ok {
		return
	}
	srv, ok := requireServiceName(w, r)
	if !ok {
		return
	}

	ctx := r.Context()
	enc := servicePath(srv)
	var serverInfo map[string]any
	if err := client.GetWithContext(ctx, enc, &serverInfo); err != nil {
		writeSuccessError(w, http.StatusInternalServerError, formatOVHError(err))
		return
	}

	reverses := make([]map[string]any, 0)
	if asString(serverInfo["ip"]) != "" {
		var revIPs []string
		if err := client.GetWithContext(ctx, enc+"/reverse", &revIPs); err == nil {
			for _, revIP := range revIPs {
				var detail map[string]any
				if err := client.GetWithContext(ctx, enc+"/reverse/"+url.PathEscape(revIP), &detail); err == nil {
					reverses = append(reverses, map[string]any{
						"ipReverse": revIP,
						"reverse":   asString(detail["reverse"]),
					})
				}
			}
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success":  true,
		"reverses": reverses,
	})
}

// HandleSetReverseDNS handles POST /api/server-control/{serviceName}/reverse.
func (s *Service) HandleSetReverseDNS(w http.ResponseWriter, r *http.Request) {
	client, _, ok := s.requireOVHClient(w, r)
	if !ok {
		return
	}
	srv, ok := requireServiceName(w, r)
	if !ok {
		return
	}

	var body struct {
		IP      string `json:"ip"`
		Reverse string `json:"reverse"`
	}
	if err := decodeJSONBody(r, &body); err != nil {
		writeSuccessError(w, http.StatusBadRequest, "Invalid JSON payload")
		return
	}
	if strings.TrimSpace(body.IP) == "" || strings.TrimSpace(body.Reverse) == "" {
		writeSuccessError(w, http.StatusBadRequest, "ip and reverse are required")
		return
	}

	if err := client.PostWithContext(r.Context(), servicePath(srv)+"/reverse", map[string]string{
		"ipReverse": strings.TrimSpace(body.IP),
		"reverse":   strings.TrimSpace(body.Reverse),
	}, nil); err != nil {
		writeSuccessError(w, http.StatusInternalServerError, formatOVHError(err))
		return
	}

	s.logInfo(logger.SourceOVH, "Updated reverse DNS", "serviceName", srv, "ip", body.IP, "reverse", body.Reverse)
	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"message": "Reverse DNS updated",
	})
}

// HandleIPCanBeMovedTo handles GET /api/server-control/{serviceName}/ip/can-be-moved-to.
func (s *Service) HandleIPCanBeMovedTo(w http.ResponseWriter, r *http.Request) {
	client, _, ok := s.requireOVHClient(w, r)
	if !ok {
		return
	}
	srv, ok := requireServiceName(w, r)
	if !ok {
		return
	}
	var targets any
	if err := client.GetWithContext(r.Context(), servicePath(srv)+"/ipCanBeMovedTo", &targets); err != nil {
		writeSuccessError(w, http.StatusInternalServerError, formatOVHError(err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "targets": targets})
}

// HandleIPCountryAvailable handles GET /api/server-control/{serviceName}/ip/country-available.
func (s *Service) HandleIPCountryAvailable(w http.ResponseWriter, r *http.Request) {
	client, _, ok := s.requireOVHClient(w, r)
	if !ok {
		return
	}
	srv, ok := requireServiceName(w, r)
	if !ok {
		return
	}
	var countries any
	if err := client.GetWithContext(r.Context(), servicePath(srv)+"/ipCountryAvailable", &countries); err != nil {
		writeSuccessError(w, http.StatusInternalServerError, formatOVHError(err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "countries": countries})
}

// HandleMoveIP handles POST /api/server-control/{serviceName}/ip/move.
func (s *Service) HandleMoveIP(w http.ResponseWriter, r *http.Request) {
	client, _, ok := s.requireOVHClient(w, r)
	if !ok {
		return
	}
	srv, ok := requireServiceName(w, r)
	if !ok {
		return
	}

	var body struct {
		IP string `json:"ip"`
		To string `json:"to"`
	}
	if err := decodeJSONBody(r, &body); err != nil || strings.TrimSpace(body.IP) == "" || strings.TrimSpace(body.To) == "" {
		writeSuccessError(w, http.StatusBadRequest, "ip and to are required")
		return
	}

	var result any
	if err := client.PostWithContext(r.Context(), servicePath(srv)+"/ipMove", map[string]string{
		"ip": strings.TrimSpace(body.IP),
		"to": strings.TrimSpace(body.To),
	}, &result); err != nil {
		writeSuccessError(w, http.StatusInternalServerError, formatOVHError(err))
		return
	}

	s.logInfo(logger.SourceOVH, "Initiated IP migration task", "serviceName", srv, "ip", body.IP, "to", body.To)
	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"message": "IP migration task created",
		"result":  result,
	})
}

// HandleGetBurst handles GET /api/server-control/{serviceName}/burst.
func (s *Service) HandleGetBurst(w http.ResponseWriter, r *http.Request) {
	client, _, ok := s.requireOVHClient(w, r)
	if !ok {
		return
	}
	srv, ok := requireServiceName(w, r)
	if !ok {
		return
	}

	var burst any
	if err := client.GetWithContext(r.Context(), servicePath(srv)+"/burst", &burst); err != nil {
		if isOVHNotFound(err) {
			writeJSON(w, http.StatusNotFound, map[string]any{
				"success":      false,
				"error":        "Burst bandwidth is not supported on this server",
				"notAvailable": true,
			})
			return
		}
		writeSuccessError(w, http.StatusInternalServerError, formatOVHError(err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "burst": burst})
}

// HandleSetBurst handles PUT /api/server-control/{serviceName}/burst.
func (s *Service) HandleSetBurst(w http.ResponseWriter, r *http.Request) {
	client, _, ok := s.requireOVHClient(w, r)
	if !ok {
		return
	}
	srv, ok := requireServiceName(w, r)
	if !ok {
		return
	}

	var body struct {
		Status string `json:"status"`
	}
	if err := decodeJSONBody(r, &body); err != nil || strings.TrimSpace(body.Status) == "" {
		writeSuccessError(w, http.StatusBadRequest, "status is required")
		return
	}

	var result any
	if err := client.PutWithContext(r.Context(), servicePath(srv)+"/burst", map[string]string{
		"status": strings.TrimSpace(body.Status),
	}, &result); err != nil {
		writeSuccessError(w, http.StatusInternalServerError, formatOVHError(err))
		return
	}

	s.logInfo(logger.SourceOVH, "Updated burst bandwidth status", "serviceName", srv, "status", body.Status)
	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"message": "Burst bandwidth status updated",
		"result":  result,
	})
}

// HandleGetFirewall handles GET /api/server-control/{serviceName}/firewall.
func (s *Service) HandleGetFirewall(w http.ResponseWriter, r *http.Request) {
	client, _, ok := s.requireOVHClient(w, r)
	if !ok {
		return
	}
	srv, ok := requireServiceName(w, r)
	if !ok {
		return
	}

	var fw any
	if err := client.GetWithContext(r.Context(), servicePath(srv)+"/features/firewall", &fw); err != nil {
		if isOVHNotFound(err) {
			writeJSON(w, http.StatusNotFound, map[string]any{
				"success":      false,
				"error":        "Server firewall feature is not available",
				"notAvailable": true,
			})
			return
		}
		writeSuccessError(w, http.StatusInternalServerError, formatOVHError(err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "firewall": fw})
}

// HandleSetFirewall handles PUT /api/server-control/{serviceName}/firewall.
func (s *Service) HandleSetFirewall(w http.ResponseWriter, r *http.Request) {
	client, _, ok := s.requireOVHClient(w, r)
	if !ok {
		return
	}
	srv, ok := requireServiceName(w, r)
	if !ok {
		return
	}

	var body struct {
		Enabled *bool `json:"enabled"`
	}
	if err := decodeJSONBody(r, &body); err != nil || body.Enabled == nil {
		writeSuccessError(w, http.StatusBadRequest, "enabled is required")
		return
	}

	var result any
	if err := client.PutWithContext(r.Context(), servicePath(srv)+"/features/firewall", map[string]bool{
		"enabled": *body.Enabled,
	}, &result); err != nil {
		writeSuccessError(w, http.StatusInternalServerError, formatOVHError(err))
		return
	}

	stateWord := "disabled"
	if *body.Enabled {
		stateWord = "enabled"
	}
	s.logInfo(logger.SourceOVH, "Updated dedicated server firewall", "serviceName", srv, "enabled", *body.Enabled)
	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"message": "Firewall " + stateWord,
		"result":  result,
	})
}

// ==================== IP Security (Edge Firewall Rules & Anti-DDoS Mitigation) ====================

func resolveServerIPv4(ctx context.Context, client OVHClient, serviceName, requestedIP string) (string, string) {
	encSrv := servicePath(serviceName)
	var ipv4Blocks []string
	var ips []string
	if err := client.GetWithContext(ctx, encSrv+"/ips", &ips); err == nil {
		for _, b := range ips {
			if !strings.Contains(b, ":") {
				ipv4Blocks = append(ipv4Blocks, b)
			}
		}
	}
	if len(ipv4Blocks) == 0 {
		var srvInfo map[string]any
		if err := client.GetWithContext(ctx, encSrv, &srvInfo); err == nil {
			if primary := strings.TrimSpace(asString(srvInfo["ip"])); primary != "" {
				ipv4Blocks = append(ipv4Blocks, primary)
			}
		}
	}

	if req := strings.TrimSpace(requestedIP); req != "" {
		reqClean := strings.Split(req, "/")[0]
		for _, blk := range ipv4Blocks {
			if strings.Split(blk, "/")[0] == reqClean {
				return blk, reqClean
			}
		}
		return reqClean, reqClean
	}

	if len(ipv4Blocks) == 0 {
		return "", ""
	}
	first := ipv4Blocks[0]
	return first, strings.Split(first, "/")[0]
}

// HandleGetIPSecurity handles GET /api/server-control/{serviceName}/ip-security.
func (s *Service) HandleGetIPSecurity(w http.ResponseWriter, r *http.Request) {
	client, _, ok := s.requireOVHClient(w, r)
	if !ok {
		return
	}
	srv, ok := requireServiceName(w, r)
	if !ok {
		return
	}

	ctx := r.Context()
	ipBlock, ipAddr := resolveServerIPv4(ctx, client, srv, r.URL.Query().Get("ip"))
	if ipBlock == "" || ipAddr == "" {
		writeSuccessError(w, http.StatusNotFound, "No IPv4 address found for server")
		return
	}

	encBlock := url.PathEscape(ipBlock)
	encIP := url.PathEscape(ipAddr)

	var fw map[string]any
	if err := client.GetWithContext(ctx, fmt.Sprintf("/ip/%s/firewall/%s", encBlock, encIP), &fw); err != nil {
		fw = map[string]any{
			"ipOnFirewall": ipAddr,
			"enabled":      false,
			"state":        "not_configured",
		}
	}

	rules := make([]map[string]any, 0)
	var seqs []int
	if err := client.GetWithContext(ctx, fmt.Sprintf("/ip/%s/firewall/%s/rule", encBlock, encIP), &seqs); err == nil {
		sort.Ints(seqs)
		for _, seq := range seqs {
			var rule map[string]any
			if err := client.GetWithContext(ctx, fmt.Sprintf("/ip/%s/firewall/%s/rule/%d", encBlock, encIP, seq), &rule); err == nil {
				rules = append(rules, rule)
			}
		}
	}

	var mitigation map[string]any
	if err := client.GetWithContext(ctx, fmt.Sprintf("/ip/%s/mitigation/%s", encBlock, encIP), &mitigation); err != nil {
		mitigation = map[string]any{
			"ipOnMitigation": ipAddr,
			"permanent":      false,
			"auto":           false,
			"state":          "auto",
		}
	}

	payload := map[string]any{
		"ipBlock":    ipBlock,
		"ip":         ipAddr,
		"firewall":   fw,
		"rules":      rules,
		"mitigation": mitigation,
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":     "success",
		"success":    true,
		"data":       payload,
		"ipBlock":    ipBlock,
		"ip":         ipAddr,
		"firewall":   fw,
		"rules":      rules,
		"mitigation": mitigation,
	})
}

// HandleCreateIPFirewallRule handles POST /api/server-control/{serviceName}/ip-security/rules.
func (s *Service) HandleCreateIPFirewallRule(w http.ResponseWriter, r *http.Request) {
	client, _, ok := s.requireOVHClient(w, r)
	if !ok {
		return
	}
	srv, ok := requireServiceName(w, r)
	if !ok {
		return
	}

	var body struct {
		Sequence        any    `json:"sequence"`
		Action          string `json:"action"`
		Protocol        string `json:"protocol"`
		Source          string `json:"source"`
		DestinationPort any    `json:"destinationPort"`
		TCPOption       any    `json:"tcpOption"`
		IP              string `json:"ip"`
	}
	if err := decodeJSONBody(r, &body); err != nil {
		writeSuccessError(w, http.StatusBadRequest, "Invalid JSON payload")
		return
	}

	seq := asInt(body.Sequence, 0)
	if seq < 0 || seq > 19 {
		writeSuccessError(w, http.StatusBadRequest, "sequence must be between 0 and 19")
		return
	}
	action := strings.TrimSpace(body.Action)
	if action == "" {
		action = "permit"
	}
	if action != "permit" && action != "deny" {
		writeSuccessError(w, http.StatusBadRequest, "action must be permit or deny")
		return
	}
	protocol := strings.TrimSpace(body.Protocol)
	if protocol == "" {
		protocol = "tcp"
	}

	ctx := r.Context()
	ipBlock, ipAddr := resolveServerIPv4(ctx, client, srv, body.IP)
	if ipBlock == "" || ipAddr == "" {
		writeSuccessError(w, http.StatusNotFound, "No IPv4 address found for server")
		return
	}

	encBlock := url.PathEscape(ipBlock)
	encIP := url.PathEscape(ipAddr)

	var fwCheck map[string]any
	if err := client.GetWithContext(ctx, fmt.Sprintf("/ip/%s/firewall/%s", encBlock, encIP), &fwCheck); err != nil {
		_ = client.PostWithContext(ctx, fmt.Sprintf("/ip/%s/firewall", encBlock), map[string]string{
			"ipOnFirewall": ipAddr,
		}, nil)
	}

	rulePayload := map[string]any{
		"sequence": seq,
		"action":   action,
		"protocol": protocol,
	}
	if src := strings.TrimSpace(body.Source); src != "" && strings.ToLower(src) != "any" {
		rulePayload["source"] = src
	}
	if body.DestinationPort != nil && strings.TrimSpace(asString(body.DestinationPort)) != "" && (protocol == "tcp" || protocol == "udp") {
		if port := asInt(body.DestinationPort, 0); port > 0 {
			rulePayload["destinationPort"] = port
		}
	}
	if body.TCPOption != nil && protocol == "tcp" {
		rulePayload["tcpOption"] = body.TCPOption
	}

	if err := client.PostWithContext(ctx, fmt.Sprintf("/ip/%s/firewall/%s/rule", encBlock, encIP), rulePayload, nil); err != nil {
		s.logError(logger.SourceOVH, "Failed to create IP firewall rule", "serviceName", srv, "ip", ipAddr, "sequence", seq, "error", formatOVHError(err))
		writeSuccessError(w, http.StatusInternalServerError, formatOVHError(err))
		return
	}

	s.logInfo(logger.SourceOVH, "Created IP edge firewall rule", "serviceName", srv, "ip", ipAddr, "sequence", seq, "action", action, "protocol", protocol)
	writeJSON(w, http.StatusOK, map[string]any{
		"status":  "success",
		"success": true,
		"message": "Firewall rule created",
	})
}

// HandleDeleteIPFirewallRule handles DELETE /api/server-control/{serviceName}/ip-security/rules/{ruleId}.
func (s *Service) HandleDeleteIPFirewallRule(w http.ResponseWriter, r *http.Request) {
	client, _, ok := s.requireOVHClient(w, r)
	if !ok {
		return
	}
	srv, ok := requireServiceName(w, r)
	if !ok {
		return
	}

	seq, err := strconv.Atoi(strings.TrimSpace(chi.URLParam(r, "ruleId")))
	if err != nil {
		writeSuccessError(w, http.StatusBadRequest, "Invalid firewall rule sequence ID")
		return
	}

	ctx := r.Context()
	ipBlock, ipAddr := resolveServerIPv4(ctx, client, srv, r.URL.Query().Get("ip"))
	if ipBlock == "" || ipAddr == "" {
		writeSuccessError(w, http.StatusNotFound, "No IPv4 address found for server")
		return
	}

	encBlock := url.PathEscape(ipBlock)
	encIP := url.PathEscape(ipAddr)
	if err := client.DeleteWithContext(ctx, fmt.Sprintf("/ip/%s/firewall/%s/rule/%d", encBlock, encIP, seq), nil); err != nil {
		s.logError(logger.SourceOVH, "Failed to delete IP firewall rule", "serviceName", srv, "ip", ipAddr, "sequence", seq, "error", formatOVHError(err))
		writeSuccessError(w, http.StatusInternalServerError, formatOVHError(err))
		return
	}

	s.logInfo(logger.SourceOVH, "Deleted IP edge firewall rule", "serviceName", srv, "ip", ipAddr, "sequence", seq)
	writeJSON(w, http.StatusOK, map[string]any{
		"status":  "success",
		"success": true,
		"message": "Firewall rule deleted",
	})
}

// HandleUpdateIPMitigation handles POST /api/server-control/{serviceName}/ip-security/mitigation.
func (s *Service) HandleUpdateIPMitigation(w http.ResponseWriter, r *http.Request) {
	client, _, ok := s.requireOVHClient(w, r)
	if !ok {
		return
	}
	srv, ok := requireServiceName(w, r)
	if !ok {
		return
	}

	var body struct {
		Permanent *bool  `json:"permanent"`
		IP        string `json:"ip"`
	}
	_ = decodeJSONBody(r, &body)
	permanent := true
	if body.Permanent != nil {
		permanent = *body.Permanent
	}

	ctx := r.Context()
	ipBlock, ipAddr := resolveServerIPv4(ctx, client, srv, body.IP)
	if ipBlock == "" || ipAddr == "" {
		writeSuccessError(w, http.StatusNotFound, "No IPv4 address found for server")
		return
	}

	encBlock := url.PathEscape(ipBlock)
	encIP := url.PathEscape(ipAddr)

	var mitCheck map[string]any
	if err := client.GetWithContext(ctx, fmt.Sprintf("/ip/%s/mitigation/%s", encBlock, encIP), &mitCheck); err == nil {
		if putErr := client.PutWithContext(ctx, fmt.Sprintf("/ip/%s/mitigation/%s", encBlock, encIP), map[string]bool{
			"permanent": permanent,
		}, nil); putErr != nil {
			writeSuccessError(w, http.StatusInternalServerError, formatOVHError(putErr))
			return
		}
	} else if permanent {
		if postErr := client.PostWithContext(ctx, fmt.Sprintf("/ip/%s/mitigation", encBlock), map[string]string{
			"ipOnMitigation": ipAddr,
		}, nil); postErr != nil {
			writeSuccessError(w, http.StatusInternalServerError, formatOVHError(postErr))
			return
		}
		_ = client.PutWithContext(ctx, fmt.Sprintf("/ip/%s/mitigation/%s", encBlock, encIP), map[string]bool{
			"permanent": true,
		}, nil)
	}

	s.logInfo(logger.SourceOVH, "Updated Anti-DDoS permanent mitigation", "serviceName", srv, "ip", ipAddr, "permanent", permanent)
	writeJSON(w, http.StatusOK, map[string]any{
		"status":    "success",
		"success":   true,
		"permanent": permanent,
	})
}

// ==================== IPMI / KVM Console ====================

// HandleGetIPMIConsole handles GET /api/server-control/{serviceName}/console.
func (s *Service) HandleGetIPMIConsole(w http.ResponseWriter, r *http.Request) {
	client, _, ok := s.requireOVHClient(w, r)
	if !ok {
		return
	}
	srv, ok := requireServiceName(w, r)
	if !ok {
		return
	}

	ctx := r.Context()
	enc := servicePath(srv)

	var ipmiInfo map[string]any
	if err := client.GetWithContext(ctx, enc+"/features/ipmi", &ipmiInfo); err != nil {
		s.logError(logger.SourceOVH, "Failed to query server IPMI capabilities", "serviceName", srv, "error", formatOVHError(err))
		writeSuccessError(w, http.StatusInternalServerError, formatOVHError(err))
		return
	}

	supported := asMap(ipmiInfo["supportedFeatures"])
	var accessType string
	switch {
	case asBool(supported["kvmipHtml5URL"]):
		accessType = "kvmipHtml5URL"
	case asBool(supported["kvmipJnlp"]):
		accessType = "kvmipJnlp"
	case asBool(supported["serialOverLanURL"]):
		accessType = "serialOverLanURL"
	default:
		writeSuccessError(w, http.StatusBadRequest, "Server does not support KVM console access")
		return
	}

	clientIP := r.RemoteAddr
	if s.auth != nil {
		clientIP = s.auth.ClientIP(r)
	}
	taskParams := map[string]any{
		"type": accessType,
		"ttl":  15,
	}
	if isPublicIP(clientIP) {
		taskParams["ipToAllow"] = clientIP
	}

	var task map[string]any
	if err := client.PostWithContext(ctx, enc+"/features/ipmi/access", taskParams, &task); err != nil {
		s.logError(logger.SourceOVH, "Failed to request IPMI console access", "serviceName", srv, "error", formatOVHError(err))
		writeSuccessError(w, http.StatusInternalServerError, formatOVHError(err))
		return
	}

	taskID := asInt(task["taskId"], 0)
	pollInterval := s.ipmiPollInterval
	if pollInterval <= 0 {
		pollInterval = 2 * time.Second
	}

	completed := false
	for attempt := 0; attempt < 10; attempt++ {
		timer := time.NewTimer(pollInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			writeSuccessError(w, http.StatusRequestTimeout, "IPMI access request cancelled")
			return
		case <-timer.C:
		}

		var taskStatus map[string]any
		if err := client.GetWithContext(ctx, fmt.Sprintf("%s/task/%d", enc, taskID), &taskStatus); err != nil {
			continue
		}
		st := asString(taskStatus["status"])
		if st == "done" {
			completed = true
			break
		}
		if st == "cancelled" || st == "customerError" || st == "ovhError" {
			writeSuccessError(w, http.StatusInternalServerError, "IPMI access task failed: "+st)
			return
		}
	}

	if !completed {
		writeSuccessError(w, http.StatusInternalServerError, "IPMI access task timed out")
		return
	}

	var consoleAccess any
	if err := client.GetWithContext(ctx, fmt.Sprintf("%s/features/ipmi/access?type=%s", enc, url.QueryEscape(accessType)), &consoleAccess); err != nil {
		writeSuccessError(w, http.StatusInternalServerError, formatOVHError(err))
		return
	}

	s.logInfo(logger.SourceOVH, "Generated IPMI console access", "serviceName", srv, "accessType", accessType)
	writeJSON(w, http.StatusOK, map[string]any{
		"success":    true,
		"ipmi":       ipmiInfo,
		"console":    consoleAccess,
		"accessType": accessType,
	})
}

// ==================== Backup FTP & Backup Cloud ====================

// HandleGetBackupFTP handles GET /api/server-control/{serviceName}/backup-ftp.
func (s *Service) HandleGetBackupFTP(w http.ResponseWriter, r *http.Request) {
	client, _, ok := s.requireOVHClient(w, r)
	if !ok {
		return
	}
	srv, ok := requireServiceName(w, r)
	if !ok {
		return
	}

	var info any
	if err := client.GetWithContext(r.Context(), servicePath(srv)+"/features/backupFTP", &info); err != nil {
		if isOVHNotFound(err) {
			writeJSON(w, http.StatusNotFound, map[string]any{
				"success":      false,
				"error":        "Backup FTP is not activated",
				"notActivated": true,
			})
			return
		}
		writeSuccessError(w, http.StatusInternalServerError, formatOVHError(err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "backupFtp": info})
}

// HandleActivateBackupFTP handles POST /api/server-control/{serviceName}/backup-ftp.
func (s *Service) HandleActivateBackupFTP(w http.ResponseWriter, r *http.Request) {
	client, _, ok := s.requireOVHClient(w, r)
	if !ok {
		return
	}
	srv, ok := requireServiceName(w, r)
	if !ok {
		return
	}

	var result any
	if err := client.PostWithContext(r.Context(), servicePath(srv)+"/features/backupFTP", nil, &result); err != nil {
		low := strings.ToLower(err.Error())
		if strings.Contains(low, "cannot benefit") || strings.Contains(low, "not available") {
			writeJSON(w, http.StatusBadRequest, map[string]any{
				"success":      false,
				"error":        "Backup FTP is not available for this server",
				"notAvailable": true,
				"reason":       formatOVHError(err),
			})
			return
		}
		writeSuccessError(w, http.StatusInternalServerError, formatOVHError(err))
		return
	}
	s.logInfo(logger.SourceOVH, "Activated backup FTP", "serviceName", srv)
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "message": "Backup FTP activated", "result": result})
}

// HandleDeleteBackupFTP handles DELETE /api/server-control/{serviceName}/backup-ftp.
func (s *Service) HandleDeleteBackupFTP(w http.ResponseWriter, r *http.Request) {
	client, _, ok := s.requireOVHClient(w, r)
	if !ok {
		return
	}
	srv, ok := requireServiceName(w, r)
	if !ok {
		return
	}

	var result any
	if err := client.DeleteWithContext(r.Context(), servicePath(srv)+"/features/backupFTP", &result); err != nil {
		writeSuccessError(w, http.StatusInternalServerError, formatOVHError(err))
		return
	}
	s.logWarn(logger.SourceOVH, "Deleted backup FTP", "serviceName", srv)
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "message": "Backup FTP deleted", "result": result})
}

// HandleGetBackupFTPAccess handles GET /api/server-control/{serviceName}/backup-ftp/access.
func (s *Service) HandleGetBackupFTPAccess(w http.ResponseWriter, r *http.Request) {
	client, _, ok := s.requireOVHClient(w, r)
	if !ok {
		return
	}
	srv, ok := requireServiceName(w, r)
	if !ok {
		return
	}

	ctx := r.Context()
	enc := servicePath(srv)
	var ipBlocks []string
	if err := client.GetWithContext(ctx, enc+"/features/backupFTP/access", &ipBlocks); err != nil {
		writeSuccessError(w, http.StatusInternalServerError, formatOVHError(err))
		return
	}

	accessList := make([]map[string]any, 0, len(ipBlocks))
	for _, blk := range ipBlocks {
		var detail map[string]any
		if err := client.GetWithContext(ctx, enc+"/features/backupFTP/access/"+url.PathEscape(blk), &detail); err != nil {
			accessList = append(accessList, map[string]any{"ipBlock": blk, "error": formatOVHError(err)})
			continue
		}
		accessList = append(accessList, detail)
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "accessList": accessList})
}

// HandleAddBackupFTPAccess handles POST /api/server-control/{serviceName}/backup-ftp/access.
func (s *Service) HandleAddBackupFTPAccess(w http.ResponseWriter, r *http.Request) {
	client, _, ok := s.requireOVHClient(w, r)
	if !ok {
		return
	}
	srv, ok := requireServiceName(w, r)
	if !ok {
		return
	}

	var body struct {
		IPBlock string `json:"ipBlock"`
		FTP     *bool  `json:"ftp"`
		NFS     bool   `json:"nfs"`
		CIFS    bool   `json:"cifs"`
	}
	if err := decodeJSONBody(r, &body); err != nil || strings.TrimSpace(body.IPBlock) == "" {
		writeSuccessError(w, http.StatusBadRequest, "ipBlock is required")
		return
	}
	ftp := true
	if body.FTP != nil {
		ftp = *body.FTP
	}

	var result any
	if err := client.PostWithContext(r.Context(), servicePath(srv)+"/features/backupFTP/access", map[string]any{
		"ipBlock": strings.TrimSpace(body.IPBlock),
		"ftp":     ftp,
		"nfs":     body.NFS,
		"cifs":    body.CIFS,
	}, &result); err != nil {
		writeSuccessError(w, http.StatusInternalServerError, formatOVHError(err))
		return
	}

	s.logInfo(logger.SourceOVH, "Added backup FTP access rule", "serviceName", srv, "ipBlock", body.IPBlock)
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "message": "Access IP block added", "result": result})
}

// HandleDeleteBackupFTPAccess handles DELETE /api/server-control/{serviceName}/backup-ftp/access/{ipBlock}.
func (s *Service) HandleDeleteBackupFTPAccess(w http.ResponseWriter, r *http.Request) {
	client, _, ok := s.requireOVHClient(w, r)
	if !ok {
		return
	}
	srv, ok := requireServiceName(w, r)
	if !ok {
		return
	}

	ipBlock := strings.TrimSpace(unescapeWildcardParam(r))
	if ipBlock == "" {
		writeSuccessError(w, http.StatusBadRequest, "ipBlock is required")
		return
	}

	if err := client.DeleteWithContext(r.Context(), servicePath(srv)+"/features/backupFTP/access/"+url.PathEscape(ipBlock), nil); err != nil {
		writeSuccessError(w, http.StatusInternalServerError, formatOVHError(err))
		return
	}

	s.logInfo(logger.SourceOVH, "Deleted backup FTP access rule", "serviceName", srv, "ipBlock", ipBlock)
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "message": "Access IP block deleted"})
}

// HandleResetBackupFTPPassword handles POST /api/server-control/{serviceName}/backup-ftp/password.
func (s *Service) HandleResetBackupFTPPassword(w http.ResponseWriter, r *http.Request) {
	client, _, ok := s.requireOVHClient(w, r)
	if !ok {
		return
	}
	srv, ok := requireServiceName(w, r)
	if !ok {
		return
	}

	var result any
	if err := client.PostWithContext(r.Context(), servicePath(srv)+"/features/backupFTP/password", nil, &result); err != nil {
		writeSuccessError(w, http.StatusInternalServerError, formatOVHError(err))
		return
	}

	s.logInfo(logger.SourceOVH, "Reset backup FTP password", "serviceName", srv)
	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"message": "Password reset requested; new password sent to account email",
		"result":  result,
	})
}

// HandleGetBackupFTPAuthorizableBlocks handles GET /api/server-control/{serviceName}/backup-ftp/authorizable-blocks.
func (s *Service) HandleGetBackupFTPAuthorizableBlocks(w http.ResponseWriter, r *http.Request) {
	client, _, ok := s.requireOVHClient(w, r)
	if !ok {
		return
	}
	srv, ok := requireServiceName(w, r)
	if !ok {
		return
	}

	var blocks any
	if err := client.GetWithContext(r.Context(), servicePath(srv)+"/features/backupFTP/authorizableBlocks", &blocks); err != nil {
		writeSuccessError(w, http.StatusInternalServerError, formatOVHError(err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "blocks": blocks})
}

// HandleGetBackupCloud handles GET /api/server-control/{serviceName}/backup-cloud.
func (s *Service) HandleGetBackupCloud(w http.ResponseWriter, r *http.Request) {
	client, _, ok := s.requireOVHClient(w, r)
	if !ok {
		return
	}
	srv, ok := requireServiceName(w, r)
	if !ok {
		return
	}

	var info any
	if err := client.GetWithContext(r.Context(), servicePath(srv)+"/features/backupCloud", &info); err != nil {
		if isOVHNotFound(err) {
			writeJSON(w, http.StatusNotFound, map[string]any{
				"success":      false,
				"error":        "Backup Cloud is not activated",
				"notActivated": true,
			})
			return
		}
		writeSuccessError(w, http.StatusInternalServerError, formatOVHError(err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "backupCloud": info})
}

// HandleGetBackupCloudOfferDetails handles GET /api/server-control/{serviceName}/backup-cloud/offer-details.
func (s *Service) HandleGetBackupCloudOfferDetails(w http.ResponseWriter, r *http.Request) {
	client, _, ok := s.requireOVHClient(w, r)
	if !ok {
		return
	}
	srv, ok := requireServiceName(w, r)
	if !ok {
		return
	}

	var details any
	if err := client.GetWithContext(r.Context(), servicePath(srv)+"/backupCloudOfferDetails", &details); err != nil {
		writeSuccessError(w, http.StatusInternalServerError, formatOVHError(err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "offerDetails": details})
}

// ==================== Secondary DNS, Virtual MAC, vNIC, OLA, vRack & Orderables ====================

// HandleGetSecondaryDNS handles GET /api/server-control/{serviceName}/secondary-dns.
func (s *Service) HandleGetSecondaryDNS(w http.ResponseWriter, r *http.Request) {
	client, _, ok := s.requireOVHClient(w, r)
	if !ok {
		return
	}
	srv, ok := requireServiceName(w, r)
	if !ok {
		return
	}

	ctx := r.Context()
	enc := servicePath(srv)
	var domains []string
	if err := client.GetWithContext(ctx, enc+"/secondaryDnsDomains", &domains); err != nil {
		writeSuccessError(w, http.StatusInternalServerError, formatOVHError(err))
		return
	}

	list := make([]map[string]any, 0, len(domains))
	for _, dom := range domains {
		var detail map[string]any
		if err := client.GetWithContext(ctx, enc+"/secondaryDnsDomains/"+url.PathEscape(dom), &detail); err != nil {
			list = append(list, map[string]any{"domain": dom})
			continue
		}
		detail["domain"] = dom
		list = append(list, detail)
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "domains": list})
}

// HandleAddSecondaryDNS handles POST /api/server-control/{serviceName}/secondary-dns.
func (s *Service) HandleAddSecondaryDNS(w http.ResponseWriter, r *http.Request) {
	client, _, ok := s.requireOVHClient(w, r)
	if !ok {
		return
	}
	srv, ok := requireServiceName(w, r)
	if !ok {
		return
	}

	var body struct {
		Domain string `json:"domain"`
	}
	if err := decodeJSONBody(r, &body); err != nil || strings.TrimSpace(body.Domain) == "" {
		writeSuccessError(w, http.StatusBadRequest, "domain is required")
		return
	}

	if err := client.PostWithContext(r.Context(), servicePath(srv)+"/secondaryDnsDomains", map[string]string{
		"domain": strings.TrimSpace(body.Domain),
	}, nil); err != nil {
		writeSuccessError(w, http.StatusInternalServerError, formatOVHError(err))
		return
	}
	s.logInfo(logger.SourceOVH, "Added secondary DNS domain", "serviceName", srv, "domain", body.Domain)
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "message": "Secondary DNS domain added"})
}

// HandleDeleteSecondaryDNS handles DELETE /api/server-control/{serviceName}/secondary-dns/{domain}.
func (s *Service) HandleDeleteSecondaryDNS(w http.ResponseWriter, r *http.Request) {
	client, _, ok := s.requireOVHClient(w, r)
	if !ok {
		return
	}
	srv, ok := requireServiceName(w, r)
	if !ok {
		return
	}
	domain := strings.TrimSpace(unescapeWildcardParam(r))
	if domain == "" {
		writeSuccessError(w, http.StatusBadRequest, "domain is required")
		return
	}

	if err := client.DeleteWithContext(r.Context(), servicePath(srv)+"/secondaryDnsDomains/"+url.PathEscape(domain), nil); err != nil {
		writeSuccessError(w, http.StatusInternalServerError, formatOVHError(err))
		return
	}
	s.logInfo(logger.SourceOVH, "Deleted secondary DNS domain", "serviceName", srv, "domain", domain)
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "message": "Secondary DNS domain deleted"})
}

// HandleGetVirtualMACs handles GET /api/server-control/{serviceName}/virtual-mac.
func (s *Service) HandleGetVirtualMACs(w http.ResponseWriter, r *http.Request) {
	client, _, ok := s.requireOVHClient(w, r)
	if !ok {
		return
	}
	srv, ok := requireServiceName(w, r)
	if !ok {
		return
	}

	ctx := r.Context()
	enc := servicePath(srv)
	var macs []string
	if err := client.GetWithContext(ctx, enc+"/virtualMac", &macs); err != nil {
		writeSuccessError(w, http.StatusInternalServerError, formatOVHError(err))
		return
	}

	list := make([]map[string]any, 0, len(macs))
	for _, mac := range macs {
		var detail map[string]any
		if err := client.GetWithContext(ctx, enc+"/virtualMac/"+url.PathEscape(mac), &detail); err != nil {
			list = append(list, map[string]any{"macAddress": mac})
			continue
		}
		detail["macAddress"] = mac
		list = append(list, detail)
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "virtualMacs": list})
}

// HandleCreateVirtualMAC handles POST /api/server-control/{serviceName}/virtual-mac.
func (s *Service) HandleCreateVirtualMAC(w http.ResponseWriter, r *http.Request) {
	client, _, ok := s.requireOVHClient(w, r)
	if !ok {
		return
	}
	srv, ok := requireServiceName(w, r)
	if !ok {
		return
	}

	var body struct {
		IPAddress          string `json:"ipAddress"`
		Type               string `json:"type"`
		VirtualMachineName string `json:"virtualMachineName"`
	}
	if err := decodeJSONBody(r, &body); err != nil || strings.TrimSpace(body.IPAddress) == "" || strings.TrimSpace(body.Type) == "" {
		writeSuccessError(w, http.StatusBadRequest, "ipAddress and type are required")
		return
	}

	payload := map[string]any{
		"ipAddress": strings.TrimSpace(body.IPAddress),
		"type":      strings.TrimSpace(body.Type),
	}
	if strings.TrimSpace(body.VirtualMachineName) != "" {
		payload["virtualMachineName"] = strings.TrimSpace(body.VirtualMachineName)
	}

	var result any
	if err := client.PostWithContext(r.Context(), servicePath(srv)+"/virtualMac", payload, &result); err != nil {
		writeSuccessError(w, http.StatusInternalServerError, formatOVHError(err))
		return
	}
	s.logInfo(logger.SourceOVH, "Created virtual MAC", "serviceName", srv, "ipAddress", body.IPAddress)
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "message": "Virtual MAC created", "result": result})
}

// HandleGetVirtualNetworkInterfaces handles GET /api/server-control/{serviceName}/virtual-network-interface.
func (s *Service) HandleGetVirtualNetworkInterfaces(w http.ResponseWriter, r *http.Request) {
	client, _, ok := s.requireOVHClient(w, r)
	if !ok {
		return
	}
	srv, ok := requireServiceName(w, r)
	if !ok {
		return
	}

	ctx := r.Context()
	enc := servicePath(srv)
	var uuids []string
	if err := client.GetWithContext(ctx, enc+"/virtualNetworkInterface", &uuids); err != nil {
		writeSuccessError(w, http.StatusInternalServerError, formatOVHError(err))
		return
	}

	list := make([]map[string]any, 0, len(uuids))
	for _, id := range uuids {
		var detail map[string]any
		if err := client.GetWithContext(ctx, enc+"/virtualNetworkInterface/"+url.PathEscape(id), &detail); err != nil {
			list = append(list, map[string]any{"uuid": id})
			continue
		}
		detail["uuid"] = id
		list = append(list, detail)
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "interfaces": list})
}

// HandleEnableVirtualNetworkInterface handles POST /api/server-control/{serviceName}/virtual-network-interface/{uuid}/enable.
func (s *Service) HandleEnableVirtualNetworkInterface(w http.ResponseWriter, r *http.Request) {
	s.toggleVNIC(w, r, "enable", "Virtual network interface enabled")
}

// HandleDisableVirtualNetworkInterface handles POST /api/server-control/{serviceName}/virtual-network-interface/{uuid}/disable.
func (s *Service) HandleDisableVirtualNetworkInterface(w http.ResponseWriter, r *http.Request) {
	s.toggleVNIC(w, r, "disable", "Virtual network interface disabled")
}

func (s *Service) toggleVNIC(w http.ResponseWriter, r *http.Request, action, msg string) {
	client, _, ok := s.requireOVHClient(w, r)
	if !ok {
		return
	}
	srv, ok := requireServiceName(w, r)
	if !ok {
		return
	}
	uid := strings.TrimSpace(chi.URLParam(r, "uuid"))
	if uid == "" {
		writeSuccessError(w, http.StatusBadRequest, "uuid is required")
		return
	}

	if err := client.PostWithContext(r.Context(), fmt.Sprintf("%s/virtualNetworkInterface/%s/%s", servicePath(srv), url.PathEscape(uid), action), nil, nil); err != nil {
		writeSuccessError(w, http.StatusInternalServerError, formatOVHError(err))
		return
	}
	s.logInfo(logger.SourceOVH, "Updated virtual network interface", "serviceName", srv, "uuid", uid, "action", action)
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "message": msg})
}

// HandleOLAAggregation handles POST /api/server-control/{serviceName}/ola/aggregation.
func (s *Service) HandleOLAAggregation(w http.ResponseWriter, r *http.Request) {
	client, _, ok := s.requireOVHClient(w, r)
	if !ok {
		return
	}
	srv, ok := requireServiceName(w, r)
	if !ok {
		return
	}

	var body struct {
		Name                     string   `json:"name"`
		VirtualNetworkInterfaces []string `json:"virtualNetworkInterfaces"`
	}
	if err := decodeJSONBody(r, &body); err != nil {
		writeSuccessError(w, http.StatusBadRequest, "Invalid JSON payload")
		return
	}
	if strings.TrimSpace(body.Name) == "" {
		writeSuccessError(w, http.StatusBadRequest, "name is required")
		return
	}
	if len(body.VirtualNetworkInterfaces) < 2 {
		writeSuccessError(w, http.StatusBadRequest, "At least 2 virtual network interfaces are required for OLA aggregation")
		return
	}

	var task any
	if err := client.PostWithContext(r.Context(), servicePath(srv)+"/ola/aggregation", map[string]any{
		"name":                     strings.TrimSpace(body.Name),
		"virtualNetworkInterfaces": body.VirtualNetworkInterfaces,
	}, &task); err != nil {
		writeSuccessError(w, http.StatusInternalServerError, formatOVHError(err))
		return
	}

	s.logInfo(logger.SourceOVH, "Created OLA aggregation task", "serviceName", srv, "name", body.Name)
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "message": "OLA aggregation task created", "task": task})
}

// HandleOLAReset handles POST /api/server-control/{serviceName}/ola/reset.
func (s *Service) HandleOLAReset(w http.ResponseWriter, r *http.Request) {
	client, _, ok := s.requireOVHClient(w, r)
	if !ok {
		return
	}
	srv, ok := requireServiceName(w, r)
	if !ok {
		return
	}

	var body struct {
		VirtualNetworkInterface string `json:"virtualNetworkInterface"`
	}
	if err := decodeJSONBody(r, &body); err != nil || strings.TrimSpace(body.VirtualNetworkInterface) == "" {
		writeSuccessError(w, http.StatusBadRequest, "virtualNetworkInterface is required")
		return
	}

	var task any
	if err := client.PostWithContext(r.Context(), servicePath(srv)+"/ola/reset", map[string]string{
		"virtualNetworkInterface": strings.TrimSpace(body.VirtualNetworkInterface),
	}, &task); err != nil {
		writeSuccessError(w, http.StatusInternalServerError, formatOVHError(err))
		return
	}

	s.logInfo(logger.SourceOVH, "Created OLA reset task", "serviceName", srv, "vnic", body.VirtualNetworkInterface)
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "message": "OLA reset task created", "task": task})
}

// HandleOLAGroup handles POST /api/server-control/{serviceName}/ola/group.
func (s *Service) HandleOLAGroup(w http.ResponseWriter, r *http.Request) {
	s.handleSimpleOLAPost(w, r, "/ola/group", "OLA group created")
}

// HandleOLAUngroup handles POST /api/server-control/{serviceName}/ola/ungroup.
func (s *Service) HandleOLAUngroup(w http.ResponseWriter, r *http.Request) {
	s.handleSimpleOLAPost(w, r, "/ola/ungroup", "OLA group dissolved")
}

func (s *Service) handleSimpleOLAPost(w http.ResponseWriter, r *http.Request, suffix, msg string) {
	client, _, ok := s.requireOVHClient(w, r)
	if !ok {
		return
	}
	srv, ok := requireServiceName(w, r)
	if !ok {
		return
	}

	var result any
	if err := client.PostWithContext(r.Context(), servicePath(srv)+suffix, nil, &result); err != nil {
		writeSuccessError(w, http.StatusInternalServerError, formatOVHError(err))
		return
	}
	s.logInfo(logger.SourceOVH, msg, "serviceName", srv)
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "message": msg, "result": result})
}

// HandleGetVRack handles GET /api/server-control/{serviceName}/vrack.
func (s *Service) HandleGetVRack(w http.ResponseWriter, r *http.Request) {
	client, _, ok := s.requireOVHClient(w, r)
	if !ok {
		return
	}
	srv, ok := requireServiceName(w, r)
	if !ok {
		return
	}

	ctx := r.Context()
	enc := servicePath(srv)
	var vracks []string
	if err := client.GetWithContext(ctx, enc+"/vrack", &vracks); err != nil {
		writeSuccessError(w, http.StatusInternalServerError, formatOVHError(err))
		return
	}

	list := make([]map[string]any, 0, len(vracks))
	for _, vr := range vracks {
		var detail map[string]any
		if err := client.GetWithContext(ctx, enc+"/vrack/"+url.PathEscape(vr), &detail); err != nil {
			list = append(list, map[string]any{"vrackName": vr})
			continue
		}
		detail["vrackName"] = vr
		list = append(list, detail)
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "vracks": list})
}

// HandleRemoveFromVRack handles DELETE /api/server-control/{serviceName}/vrack/{vrackName}.
func (s *Service) HandleRemoveFromVRack(w http.ResponseWriter, r *http.Request) {
	client, _, ok := s.requireOVHClient(w, r)
	if !ok {
		return
	}
	srv, ok := requireServiceName(w, r)
	if !ok {
		return
	}
	vrack := strings.TrimSpace(unescapeWildcardParam(r))
	if vrack == "" {
		writeSuccessError(w, http.StatusBadRequest, "vrackName is required")
		return
	}

	if err := client.DeleteWithContext(r.Context(), servicePath(srv)+"/vrack/"+url.PathEscape(vrack), nil); err != nil {
		writeSuccessError(w, http.StatusInternalServerError, formatOVHError(err))
		return
	}
	s.logWarn(logger.SourceOVH, "Removed dedicated server from vRack", "serviceName", srv, "vrack", vrack)
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "message": "Server removed from vRack"})
}

// HandleGetOrderableBandwidth handles GET /api/server-control/{serviceName}/orderable/bandwidth.
func (s *Service) HandleGetOrderableBandwidth(w http.ResponseWriter, r *http.Request) {
	s.handleOrderableQuery(w, r, "/orderable/bandwidth")
}

// HandleGetOrderableTraffic handles GET /api/server-control/{serviceName}/orderable/traffic.
func (s *Service) HandleGetOrderableTraffic(w http.ResponseWriter, r *http.Request) {
	s.handleOrderableQuery(w, r, "/orderable/traffic")
}

// HandleGetOrderableIP handles GET /api/server-control/{serviceName}/orderable/ip.
func (s *Service) HandleGetOrderableIP(w http.ResponseWriter, r *http.Request) {
	s.handleOrderableQuery(w, r, "/orderable/ip")
}

func (s *Service) handleOrderableQuery(w http.ResponseWriter, r *http.Request, suffix string) {
	client, _, ok := s.requireOVHClient(w, r)
	if !ok {
		return
	}
	srv, ok := requireServiceName(w, r)
	if !ok {
		return
	}
	var orderable any
	if err := client.GetWithContext(r.Context(), servicePath(srv)+suffix, &orderable); err != nil {
		writeSuccessError(w, http.StatusInternalServerError, formatOVHError(err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "orderable": orderable})
}

// HandleGetServerOptions handles GET /api/server-control/{serviceName}/options.
func (s *Service) HandleGetServerOptions(w http.ResponseWriter, r *http.Request) {
	client, _, ok := s.requireOVHClient(w, r)
	if !ok {
		return
	}
	srv, ok := requireServiceName(w, r)
	if !ok {
		return
	}

	ctx := r.Context()
	enc := servicePath(srv)
	var options []string
	if err := client.GetWithContext(ctx, enc+"/option", &options); err != nil {
		writeSuccessError(w, http.StatusInternalServerError, formatOVHError(err))
		return
	}

	list := make([]map[string]any, 0, len(options))
	for _, opt := range options {
		var detail map[string]any
		if err := client.GetWithContext(ctx, enc+"/option/"+url.PathEscape(opt), &detail); err != nil {
			list = append(list, map[string]any{"option": opt})
			continue
		}
		detail["option"] = opt
		list = append(list, detail)
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "options": list})
}
