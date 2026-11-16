package mcp

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	"ovh-nexus/server/internal/store"
)

// PermissionError indicates a tool call was denied by MCP scope, TOTP lock, whitelist,
// hourly order cap, or hardcoded L4 security redline.
type PermissionError struct {
	Message string
}

func (e *PermissionError) Error() string {
	return e.Message
}

// ToolDefinition describes an MCP tool's metadata and JSON schema.
type ToolDefinition struct {
	Name        string         `json:"name"`
	Scope       string         `json:"-"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

// ToolCatalogEntry summarizes a tool for WebUI inspection.
type ToolCatalogEntry struct {
	Name        string `json:"name"`
	Scope       string `json:"scope"`
	Description string `json:"description"`
}

var toolDefinitions = []ToolDefinition{
	// --- L1: Read-Only Insight (11 tools, scope: read) ---
	{
		Name:        "profile_list",
		Scope:       "read",
		Description: "List OVHcloud account profiles accessible to MCP (similar to `ovhcloud config profile list`). Returns obfuscated profile aliases (e.g. acct-01), region, and subsidiary.",
		InputSchema: map[string]any{
			"type":                 "object",
			"properties":           map[string]any{},
			"additionalProperties": false,
		},
	},
	{
		Name:        "catalog_availability",
		Scope:       "read",
		Description: "Query real-time global OVHcloud bare-metal server stock matrix and commercial specs (KS-A, RISE-1, SCALE, etc.) without requiring an API key.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"plan_code":      map[string]any{"type": "string", "description": "Optional exact or partial planCode / model name filter (e.g. '24ska01', 'KS-A', 'rise')."},
				"datacenter":     map[string]any{"type": "string", "description": "Optional datacenter filter (e.g. 'gra', 'rbx', 'sbg', 'bhs', 'fra', 'vin')."},
				"only_available": map[string]any{"type": "boolean", "description": "If true, return only configurations currently in stock."},
				"limit":          map[string]any{"type": "integer", "description": "Max plans to return (1-50, default 20)."},
			},
			"additionalProperties": false,
		},
	},
	{
		Name:        "catalog_sku_changes",
		Scope:       "read",
		Description: "List recent public OVHcloud shelf SKU additions and removals detected by the SKU Watcher.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"limit":       map[string]any{"type": "integer", "description": "Maximum change events to return (1-50, default 20)."},
				"change_type": map[string]any{"type": "string", "enum": []string{"ALL", "ADDED", "REMOVED"}, "description": "Filter by change type."},
			},
			"additionalProperties": false,
		},
	},
	{
		Name:        "baremetal_list",
		Scope:       "read",
		Description: "List purchased OVHcloud dedicated / bare-metal servers (similar to `ovhcloud baremetal list`). Sensitive hostnames and full IPs are replaced with persistent aliases (e.g. bm-01, ip-01).",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"profile": map[string]any{"type": "string", "description": "Optional account profile alias (e.g. 'acct-01'). Omit to list across all whitelisted profiles."},
			},
			"additionalProperties": false,
		},
	},
	{
		Name:        "baremetal_get",
		Scope:       "read",
		Description: "Get detailed hardware specs, boot mode, and ongoing tasks for a specific bare-metal server alias (similar to `ovhcloud baremetal get <server>`).",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"server": map[string]any{"type": "string", "description": "Bare-metal server alias (e.g. 'bm-01') or custom label."},
			},
			"required":             []string{"server"},
			"additionalProperties": false,
		},
	},
	{
		Name:        "baremetal_firewall_get",
		Scope:       "read",
		Description: "Inspect OVHcloud IP Edge Firewall status, Anti-DDoS mitigation mode, and active firewall rules for a bare-metal server (`bm-xx`).",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"server": map[string]any{"type": "string", "description": "Bare-metal server alias (e.g. 'bm-01')."},
			},
			"required":             []string{"server"},
			"additionalProperties": false,
		},
	},
	{
		Name:        "vps_list",
		Scope:       "read",
		Description: "List purchased OVHcloud VPS instances (similar to `ovhcloud vps list`) with obfuscated aliases (vps-01), vCPU/RAM/disk specs, state, and expiration countdown.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"profile": map[string]any{"type": "string", "description": "Optional account profile alias (e.g. 'acct-01')."},
			},
			"additionalProperties": false,
		},
	},
	{
		Name:        "order_list",
		Scope:       "read",
		Description: "List recent OVHcloud purchase orders (`ord-01`), prices, and delivery status. Secret payment/invoice URLs are stripped for security.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"profile": map[string]any{"type": "string", "description": "Optional account profile alias (e.g. 'acct-01')."},
				"limit":   map[string]any{"type": "integer", "description": "Number of orders per profile (1-20, default 10)."},
			},
			"additionalProperties": false,
		},
	},
	{
		Name:        "order_follow_up",
		Scope:       "read",
		Description: "Inspect the 4-stage physical hardware delivery pipeline (CREATING -> VALIDATING -> DELIVERING -> INSTALLATION) for an order alias (`ord-01`).",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"order":   map[string]any{"type": "string", "description": "Order alias (e.g. 'ord-01')."},
				"profile": map[string]any{"type": "string", "description": "Optional account profile alias (e.g. 'acct-01')."},
			},
			"required":             []string{"order"},
			"additionalProperties": false,
		},
	},
	{
		Name:        "service_renewals",
		Scope:       "read",
		Description: "List upcoming expiration dates and renewal modes across dedicated servers and VPS instances.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"profile":     map[string]any{"type": "string", "description": "Optional account profile alias (e.g. 'acct-01')."},
				"days_within": map[string]any{"type": "integer", "description": "Optional filter: only return services expiring within N days."},
			},
			"additionalProperties": false,
		},
	},
	{
		Name:        "procurement_status",
		Scope:       "read",
		Description: "Get current status of OVH Nexus Auto-Order Queue tasks, stock monitor subscriptions, and recent purchase history.",
		InputSchema: map[string]any{
			"type":                 "object",
			"properties":           map[string]any{},
			"additionalProperties": false,
		},
	},

	// --- L2: Monitoring & Unpaid Procurement (5 tools, scope: procurement) ---
	{
		Name:        "order_quote",
		Scope:       "procurement",
		Description: "Step 1 of 2 for placing an unpaid server order: query exact tax-inclusive/exclusive monthly pricing for a server configuration and generate a 5-minute confirmation `quote_id`. Does NOT place an order or charge money.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"plan_code":  map[string]any{"type": "string", "description": "Server planCode (e.g. '24ska01')."},
				"datacenter": map[string]any{"type": "string", "description": "Target datacenter code (e.g. 'gra', 'rbx', 'bhs')."},
				"options": map[string]any{
					"type":        "array",
					"items":       map[string]any{"type": "string"},
					"description": "Optional hardware addon planCodes (e.g. ['ram-64g-ecc-2133', 'softraid-2x450nvme']).",
				},
				"profile": map[string]any{"type": "string", "description": "Optional account profile alias (e.g. 'acct-01')."},
			},
			"required":             []string{"plan_code", "datacenter"},
			"additionalProperties": false,
		},
	},
	{
		Name:        "order_create_unpaid",
		Scope:       "procurement",
		Description: "Step 2 of 2: place an UNPAID purchase order using a valid `quote_id` from `order_quote`. Hardcoded to `auto_pay=false` and `quantity=1` — never charges any payment method. The payment link is sent directly to the owner's notification channel and Web Console.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"quote_id": map[string]any{"type": "string", "description": "Confirmation quoteId returned by `order_quote` within the last 5 minutes."},
			},
			"required":             []string{"quote_id"},
			"additionalProperties": false,
		},
	},
	{
		Name:        "queue_add",
		Scope:       "procurement",
		Description: "Add a server target to the background Auto-Order Queue to automatically lock in an UNPAID order (`auto_pay=false`, `quantity=1` hardcoded) when stock appears.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"plan_code": map[string]any{"type": "string", "description": "Server planCode (e.g. '24ska01')."},
				"datacenters": map[string]any{
					"type":        "array",
					"items":       map[string]any{"type": "string"},
					"description": "Ordered list of preferred datacenters (e.g. ['gra', 'rbx']).",
				},
				"options": map[string]any{
					"type":        "array",
					"items":       map[string]any{"type": "string"},
					"description": "Optional hardware option codes.",
				},
				"profile": map[string]any{"type": "string", "description": "Optional account profile alias (e.g. 'acct-01')."},
			},
			"required":             []string{"plan_code", "datacenters"},
			"additionalProperties": false,
		},
	},
	{
		Name:        "queue_manage",
		Scope:       "procurement",
		Description: "Pause, resume, or delete an existing task in the Auto-Order Queue.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"task_id": map[string]any{"type": "string", "description": "Queue task ID from `procurement_status`."},
				"action":  map[string]any{"type": "string", "enum": []string{"pause", "resume", "delete"}, "description": "Action to perform on the queue task."},
			},
			"required":             []string{"task_id", "action"},
			"additionalProperties": false,
		},
	},
	{
		Name:        "stock_subscription_manage",
		Scope:       "procurement",
		Description: "Add or remove a server/VPS planCode in the real-time stock restock alert monitor.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"target":    map[string]any{"type": "string", "enum": []string{"baremetal", "vps"}, "description": "Monitor type ('baremetal' or 'vps')."},
				"action":    map[string]any{"type": "string", "enum": []string{"add", "remove"}, "description": "Whether to add or remove the subscription."},
				"plan_code": map[string]any{"type": "string", "description": "Plan code to subscribe/unsubscribe (e.g. '24ska01' or 'vps-le-1')."},
			},
			"required":             []string{"target", "action", "plan_code"},
			"additionalProperties": false,
		},
	},

	// --- L3: Reversible Day-2 Operations (4 tools, scope: operations) ---
	{
		Name:        "baremetal_reboot",
		Scope:       "operations",
		Description: "Trigger a hardware reboot on a purchased dedicated server (`bm-xx`, similar to `ovhcloud baremetal reboot`).",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"server": map[string]any{"type": "string", "description": "Bare-metal server alias (e.g. 'bm-01')."},
			},
			"required":             []string{"server"},
			"additionalProperties": false,
		},
	},
	{
		Name:        "vps_reboot",
		Scope:       "operations",
		Description: "Reboot a purchased VPS instance (`vps-xx`, similar to `ovhcloud vps reboot`).",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"vps": map[string]any{"type": "string", "description": "VPS alias (e.g. 'vps-01')."},
			},
			"required":             []string{"vps"},
			"additionalProperties": false,
		},
	},
	{
		Name:        "baremetal_ipmi_reset",
		Scope:       "operations",
		Description: "Reset stuck IPMI/KVM interface and sessions on a dedicated server (`bm-xx`, similar to `ovhcloud baremetal ipmi reset-sessions`). Does not expose raw IPMI console URLs.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"server": map[string]any{"type": "string", "description": "Bare-metal server alias (e.g. 'bm-01')."},
			},
			"required":             []string{"server"},
			"additionalProperties": false,
		},
	},
	{
		Name:        "baremetal_firewall_update",
		Scope:       "operations",
		Description: "Manage OVHcloud IP Edge Firewall or permanent Anti-DDoS mitigation on a dedicated server (`bm-xx`).",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"server":               map[string]any{"type": "string", "description": "Bare-metal server alias (e.g. 'bm-01')."},
				"operation":            map[string]any{"type": "string", "enum": []string{"set_mitigation", "add_rule", "delete_rule"}, "description": "Operation to perform on the server's primary IPv4."},
				"permanent_mitigation": map[string]any{"type": "boolean", "description": "For 'set_mitigation': enable or disable permanent Anti-DDoS mitigation."},
				"sequence":             map[string]any{"type": "integer", "description": "For 'add_rule' / 'delete_rule': rule priority sequence (0-19)."},
				"rule_action":          map[string]any{"type": "string", "enum": []string{"permit", "deny"}, "description": "For 'add_rule': permit or deny traffic."},
				"protocol":             map[string]any{"type": "string", "enum": []string{"tcp", "udp", "icmp", "ipv4"}, "description": "For 'add_rule': network protocol."},
				"destination_port":     map[string]any{"type": "integer", "description": "For 'add_rule' (tcp/udp): destination port (1-65535)."},
				"source":               map[string]any{"type": "string", "description": "For 'add_rule': optional source CIDR (default 'any')."},
			},
			"required":             []string{"server", "operation"},
			"additionalProperties": false,
		},
	},
}

var toolsByName = func() map[string]ToolDefinition {
	m := make(map[string]ToolDefinition, len(toolDefinitions))
	for _, t := range toolDefinitions {
		m[t.Name] = t
	}
	return m
}()

// AllToolCatalog returns the full 20-tool catalog metadata for WebUI rendering.
func AllToolCatalog() []ToolCatalogEntry {
	out := make([]ToolCatalogEntry, 0, len(toolDefinitions))
	for _, t := range toolDefinitions {
		out = append(out, ToolCatalogEntry{
			Name:        t.Name,
			Scope:       t.Scope,
			Description: t.Description,
		})
	}
	return out
}

func scopeEnabled(scopes store.MCPScopes, scope string, totpEnabled bool) bool {
	switch scope {
	case "read":
		return scopes.Read
	case "procurement":
		return scopes.Procurement
	case "operations":
		return scopes.Operations && totpEnabled
	default:
		return false
	}
}

func (s *Service) enabledToolsForMCP(ctx context.Context) ([]map[string]any, error) {
	cfg, totpEnabled, err := s.loadEffectiveConfig(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]map[string]any, 0, len(toolDefinitions))
	for _, t := range toolDefinitions {
		if scopeEnabled(cfg.Scopes, t.Scope, totpEnabled) {
			out = append(out, map[string]any{
				"name":        t.Name,
				"description": t.Description,
				"inputSchema": t.InputSchema,
			})
		}
	}
	return out, nil
}

func (s *Service) executeTool(ctx context.Context, toolName string, args map[string]any) (map[string]any, string, string, error) {
	toolDef, exists := toolsByName[toolName]
	if !exists {
		return nil, "blocked", "", &PermissionError{
			Message: fmt.Sprintf("Tool '%s' is not available or is permanently blocked by OVH Nexus security policy (L4 destructive operations and payment execution are prohibited).", toolName),
		}
	}

	cfg, totpEnabled, err := s.loadEffectiveConfig(ctx)
	if err != nil {
		return nil, toolDef.Scope, "", err
	}
	if !scopeEnabled(cfg.Scopes, toolDef.Scope, totpEnabled) {
		reason := fmt.Sprintf("Tool '%s' requires the '%s' permission scope, which is currently disabled in OVH Nexus MCP Gateway settings.", toolName, toolDef.Scope)
		if toolDef.Scope == "operations" && !totpEnabled {
			reason = fmt.Sprintf("Tool '%s' requires the 'operations' (L3) scope, which is locked because panel Two-Factor Authentication (2FA) is not enabled.", toolName)
		}
		return nil, toolDef.Scope, "", &PermissionError{Message: reason}
	}

	if args == nil {
		args = map[string]any{}
	}

	var result map[string]any
	var summary string

	switch toolName {
	// L1
	case "profile_list":
		result, summary, err = s.toolProfileList(ctx, cfg)
	case "catalog_availability":
		result, summary, err = s.toolCatalogAvailability(ctx, args)
	case "catalog_sku_changes":
		result, summary, err = s.toolCatalogSKUChanges(ctx, args)
	case "baremetal_list":
		result, summary, err = s.toolBaremetalList(ctx, cfg, args)
	case "baremetal_get":
		result, summary, err = s.toolBaremetalGet(ctx, cfg, args)
	case "baremetal_firewall_get":
		result, summary, err = s.toolBaremetalFirewallGet(ctx, cfg, args)
	case "vps_list":
		result, summary, err = s.toolVPSList(ctx, cfg, args)
	case "order_list":
		result, summary, err = s.toolOrderList(ctx, cfg, args)
	case "order_follow_up":
		result, summary, err = s.toolOrderFollowUp(ctx, cfg, args)
	case "service_renewals":
		result, summary, err = s.toolServiceRenewals(ctx, cfg, args)
	case "procurement_status":
		result, summary, err = s.toolProcurementStatus(ctx, cfg)
	// L2
	case "order_quote":
		result, summary, err = s.toolOrderQuote(ctx, cfg, args)
	case "order_create_unpaid":
		result, summary, err = s.toolOrderCreateUnpaid(ctx, cfg, args)
	case "queue_add":
		result, summary, err = s.toolQueueAdd(ctx, cfg, args)
	case "queue_manage":
		result, summary, err = s.toolQueueManage(ctx, cfg, args)
	case "stock_subscription_manage":
		result, summary, err = s.toolStockSubscriptionManage(ctx, cfg, args)
	// L3
	case "baremetal_reboot":
		result, summary, err = s.toolBaremetalReboot(ctx, cfg, args)
	case "vps_reboot":
		result, summary, err = s.toolVPSReboot(ctx, cfg, args)
	case "baremetal_ipmi_reset":
		result, summary, err = s.toolBaremetalIPMIReset(ctx, cfg, args)
	case "baremetal_firewall_update":
		result, summary, err = s.toolBaremetalFirewallUpdate(ctx, cfg, args)
	default:
		return nil, "blocked", "", &PermissionError{
			Message: fmt.Sprintf("Tool '%s' is permanently blocked by OVH Nexus security policy.", toolName),
		}
	}

	return result, toolDef.Scope, summary, err
}

// ==================== Account Whitelist & Server Resolution Helpers ====================

type resolvedAccount struct {
	Account  store.OVHAccount
	Alias    string
	Redacted map[string]any
}

func (s *Service) resolveTargetAccounts(ctx context.Context, cfg *store.MCPConfig, profileArg string) ([]resolvedAccount, error) {
	allAccounts, err := s.store.ListAccounts(ctx)
	if err != nil {
		return nil, err
	}

	allowedSet := make(map[string]struct{}, len(cfg.AllowedAccountIDs))
	for _, id := range cfg.AllowedAccountIDs {
		if trimmed := strings.TrimSpace(id); trimmed != "" {
			allowedSet[trimmed] = struct{}{}
		}
	}

	// Filter whitelisted accounts (ListAccounts already puts default first).
	var permitted []store.OVHAccount
	for _, acc := range allAccounts {
		if cfg.AllowAllAccounts {
			permitted = append(permitted, acc)
		} else if _, ok := allowedSet[acc.ID]; ok {
			permitted = append(permitted, acc)
		}
	}
	if len(permitted) == 0 {
		if strings.TrimSpace(profileArg) != "" {
			return nil, fmt.Errorf("profile '%s' is not found or not permitted by the MCP account whitelist", strings.TrimSpace(profileArg))
		}
		return nil, nil
	}

	profiles := make([]resolvedAccount, 0, len(permitted))
	for _, acc := range permitted {
		isDef := acc.IsDefault || len(permitted) == 1
		redacted, alias, err := s.shield.RedactAccount(ctx, acc, isDef)
		if err != nil {
			return nil, err
		}
		profiles = append(profiles, resolvedAccount{
			Account:  acc,
			Alias:    alias,
			Redacted: redacted,
		})
	}

	wanted := strings.TrimSpace(profileArg)
	if wanted == "" {
		return profiles, nil
	}

	resolvedID, _ := s.shield.Resolve("account", wanted)
	for _, p := range profiles {
		if (resolvedID != "" && p.Account.ID == resolvedID) ||
			strings.EqualFold(p.Alias, wanted) ||
			strings.EqualFold(asString(p.Redacted["label"]), wanted) {
			return []resolvedAccount{p}, nil
		}
	}
	return nil, fmt.Errorf("profile '%s' is not found or not permitted by the MCP account whitelist", wanted)
}

func (s *Service) findServerOwnerAccount(ctx context.Context, cfg *store.MCPConfig, category, aliasOrLabel string) (string, string, string, OVHClient, error) {
	realName, ok := s.shield.Resolve(category, aliasOrLabel)
	if !ok || realName == "" {
		example := "bm-01"
		if category == "vps" {
			example = "vps-01"
		}
		return "", "", "", nil, fmt.Errorf("unknown %s alias '%s'. Call %s_list first to obtain valid aliases (e.g. '%s')", category, aliasOrLabel, category, example)
	}

	canonicalAlias, err := s.shield.Register(ctx, category, realName, nil)
	if err != nil {
		return "", "", "", nil, err
	}
	meta := s.shield.GetMeta(category, canonicalAlias)
	recordedAID := asString(meta["accountId"])

	allowedProfiles, err := s.resolveTargetAccounts(ctx, cfg, "")
	if err != nil {
		return "", "", "", nil, err
	}
	if len(allowedProfiles) == 0 {
		return "", "", "", nil, errors.New("no whitelisted OVH account is available for MCP")
	}

	isAllowed := make(map[string]bool, len(allowedProfiles))
	for _, p := range allowedProfiles {
		isAllowed[p.Account.ID] = true
	}

	if recordedAID != "" && isAllowed[recordedAID] {
		if client, _, err := s.clientForAccountFn(ctx, recordedAID); err == nil && client != nil {
			return realName, canonicalAlias, recordedAID, client, nil
		}
	}

	apiPrefix := "/dedicated/server"
	if category == "vps" {
		apiPrefix = "/vps"
	}
	for _, p := range allowedProfiles {
		client, _, err := s.clientForAccountFn(ctx, p.Account.ID)
		if err != nil || client == nil {
			continue
		}
		var names []string
		if err := client.GetWithContext(ctx, apiPrefix, &names); err != nil {
			continue
		}
		for _, n := range names {
			if n == realName {
				_, _ = s.shield.Register(ctx, category, realName, map[string]any{
					"accountId":    p.Account.ID,
					"accountAlias": p.Alias,
				})
				return realName, canonicalAlias, p.Account.ID, client, nil
			}
		}
	}

	return "", "", "", nil, fmt.Errorf("resource '%s' is not accessible under any MCP-whitelisted OVH account", canonicalAlias)
}

func resolveServerPrimaryIPv4(ctx context.Context, client OVHClient, realServiceName string) (string, string) {
	encSrv := "/dedicated/server/" + url.PathEscape(realServiceName)
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
	if len(ipv4Blocks) == 0 {
		return "", ""
	}
	first := ipv4Blocks[0]
	return first, strings.Split(first, "/")[0]
}

// ==================== L1 Tool Handlers ====================

func (s *Service) toolProfileList(ctx context.Context, cfg *store.MCPConfig) (map[string]any, string, error) {
	targets, err := s.resolveTargetAccounts(ctx, cfg, "")
	if err != nil {
		return nil, "", err
	}
	profiles := make([]map[string]any, 0, len(targets))
	for _, t := range targets {
		profiles = append(profiles, t.Redacted)
	}
	return map[string]any{
		"profiles": profiles,
		"total":    len(profiles),
	}, fmt.Sprintf("Listed %d whitelisted OVH account profile(s).", len(profiles)), nil
}

func (s *Service) toolCatalogAvailability(ctx context.Context, args map[string]any) (map[string]any, string, error) {
	planFilter := strings.TrimSpace(asString(args["plan_code"]))
	dcFilter := strings.TrimSpace(asString(args["datacenter"]))
	onlyAvail := asBool(args["only_available"])
	limit := asInt(args["limit"], 20)
	if limit < 1 {
		limit = 1
	} else if limit > 50 {
		limit = 50
	}

	if s.deps.Catalog != nil {
		items, totalMatched, err := s.deps.Catalog.QueryAvailability(ctx, planFilter, dcFilter, onlyAvail, limit)
		if err != nil {
			return nil, "", err
		}
		if items == nil {
			items = []CatalogEntry{}
		}
		return map[string]any{
			"items":        items,
			"returned":     len(items),
			"totalMatched": totalMatched,
		}, fmt.Sprintf("Queried bare-metal stock matrix (matched %d plan(s)).", totalMatched), nil
	}

	// Fallback to public OVH availability endpoint if CatalogReader is not wired.
	if s.deps.OVHPool == nil {
		return nil, "", errors.New("catalog reader is not configured")
	}
	var rawAvail []map[string]any
	if err := s.deps.OVHPool.GetPublicJSON(ctx, "ovh-eu", "/dedicated/server/datacenter/availabilities", &rawAvail); err != nil {
		return nil, "", err
	}

	planLow := strings.ToLower(planFilter)
	dcLow := strings.ToLower(dcFilter)

	grouped := make(map[string]*CatalogEntry)
	order := make([]string, 0)
	for _, entry := range rawAvail {
		pcode := strings.TrimSpace(asString(entry["planCode"]))
		if pcode == "" {
			continue
		}
		if planLow != "" && !strings.Contains(strings.ToLower(pcode), planLow) {
			continue
		}
		var inStock []string
		rawDCs, _ := entry["datacenters"].([]any)
		for _, dcRaw := range rawDCs {
			dcMap := asMap(dcRaw)
			dcCode := strings.ToLower(strings.TrimSpace(asString(dcMap["datacenter"])))
			availVal := strings.TrimSpace(asString(dcMap["availability"]))
			if availVal == "" {
				availVal = "unavailable"
			}
			if dcLow != "" && !strings.Contains(dcLow, "") && !strings.Contains(dcCode, dcLow) {
				continue
			}
			if dcLow != "" && !strings.Contains(dcCode, dcLow) {
				continue
			}
			if availVal != "unavailable" && availVal != "unknown" {
				inStock = append(inStock, fmt.Sprintf("%s (%s)", strings.ToUpper(dcCode), availVal))
			}
		}
		if onlyAvail && len(inStock) == 0 {
			continue
		}
		rec, exists := grouped[pcode]
		if !exists {
			rec = &CatalogEntry{
				PlanCode:             pcode,
				ModelName:            pcode,
				CPU:                  asString(entry["server"]),
				AvailableDatacenters: []string{},
			}
			grouped[pcode] = rec
			order = append(order, pcode)
		}
		rec.MatchingConfigurations++
		for _, tag := range inStock {
			found := false
			for _, existing := range rec.AvailableDatacenters {
				if existing == tag {
					found = true
					break
				}
			}
			if !found {
				rec.AvailableDatacenters = append(rec.AvailableDatacenters, tag)
			}
		}
	}

	items := make([]CatalogEntry, 0, min(limit, len(order)))
	for _, pcode := range order {
		if len(items) >= limit {
			break
		}
		items = append(items, *grouped[pcode])
	}
	return map[string]any{
		"items":        items,
		"returned":     len(items),
		"totalMatched": len(grouped),
	}, fmt.Sprintf("Queried bare-metal stock matrix (matched %d plan(s)).", len(grouped)), nil
}

func (s *Service) toolCatalogSKUChanges(ctx context.Context, args map[string]any) (map[string]any, string, error) {
	limit := asInt(args["limit"], 20)
	if limit < 1 {
		limit = 1
	} else if limit > 50 {
		limit = 50
	}
	changeType := strings.ToUpper(strings.TrimSpace(asString(args["change_type"])))
	if changeType == "" {
		changeType = "ALL"
	}

	if s.deps.Catalog != nil {
		status, changes, err := s.deps.Catalog.GetSKUChanges(ctx, limit, changeType)
		if err != nil {
			return nil, "", err
		}
		if status == nil {
			status = map[string]any{}
		}
		if changes == nil {
			changes = []map[string]any{}
		}
		return map[string]any{
			"status":  status,
			"changes": changes,
		}, fmt.Sprintf("Retrieved %d SKU watcher change event(s).", len(changes)), nil
	}

	if s.store == nil {
		return map[string]any{"status": map[string]any{}, "changes": []any{}}, "Retrieved 0 SKU watcher change event(s).", nil
	}
	st, err := s.store.LoadSKUWatcherState(ctx)
	if err != nil {
		return nil, "", err
	}
	var allChanges []map[string]any
	_ = json.Unmarshal(st.ChangesJSON, &allChanges)
	filtered := make([]map[string]any, 0)
	for _, ch := range allChanges {
		if changeType != "ALL" && !strings.EqualFold(asString(ch["changeType"]), changeType) {
			continue
		}
		filtered = append(filtered, ch)
		if len(filtered) >= limit {
			break
		}
	}
	return map[string]any{
		"status": map[string]any{
			"initialized": st.Initialized,
			"endpoint":    st.Endpoint,
			"updatedAt":   st.UpdatedAt,
		},
		"changes": filtered,
	}, fmt.Sprintf("Retrieved %d SKU watcher change event(s).", len(filtered)), nil
}

func (s *Service) toolBaremetalList(ctx context.Context, cfg *store.MCPConfig, args map[string]any) (map[string]any, string, error) {
	targets, err := s.resolveTargetAccounts(ctx, cfg, asString(args["profile"]))
	if err != nil {
		return nil, "", err
	}

	servers := make([]map[string]any, 0)
	for _, t := range targets {
		client, _, err := s.clientForAccountFn(ctx, t.Account.ID)
		if err != nil || client == nil {
			continue
		}
		var names []string
		if err := client.GetWithContext(ctx, "/dedicated/server", &names); err != nil {
			continue
		}
		for _, srvName := range names {
			enc := "/dedicated/server/" + url.PathEscape(srvName)
			var info map[string]any
			if err := client.GetWithContext(ctx, enc, &info); err != nil {
				continue
			}
			var svc map[string]any
			_ = client.GetWithContext(ctx, enc+"/serviceInfos", &svc)

			exp := svc["expiration"]
			renewObj := asMap(svc["renew"])
			iamDisplay := asString(asMap(info["iam"])["displayName"])
			if iamDisplay == "" {
				iamDisplay = asString(info["reverse"])
			}

			rawSrv := map[string]any{
				"serviceName":     srvName,
				"displayName":     iamDisplay,
				"commercialRange": info["commercialRange"],
				"datacenter":      info["datacenter"],
				"state":           info["state"],
				"status":          svc["status"],
				"os":              info["os"],
				"monitoring":      info["monitoring"],
				"ip":              info["ip"],
				"expiration":      exp,
				"daysRemaining":   daysUntil(exp),
				"renewMode":       computeRenewMode(renewObj),
			}
			redacted, _, err := s.shield.RedactBaremetal(ctx, rawSrv, t.Alias, t.Account.ID)
			if err == nil {
				servers = append(servers, redacted)
			}
		}
	}

	return map[string]any{
		"servers": servers,
		"total":   len(servers),
	}, fmt.Sprintf("Listed %d obfuscated bare-metal server(s).", len(servers)), nil
}

func (s *Service) toolBaremetalGet(ctx context.Context, cfg *store.MCPConfig, args map[string]any) (map[string]any, string, error) {
	srvArg := strings.TrimSpace(asString(args["server"]))
	realName, bmAlias, aid, client, err := s.findServerOwnerAccount(ctx, cfg, "baremetal", srvArg)
	if err != nil {
		return nil, "", err
	}

	enc := "/dedicated/server/" + url.PathEscape(realName)
	var info map[string]any
	if err := client.GetWithContext(ctx, enc, &info); err != nil {
		return nil, "", err
	}
	var svc map[string]any
	_ = client.GetWithContext(ctx, enc+"/serviceInfos", &svc)
	var hw map[string]any
	_ = client.GetWithContext(ctx, enc+"/specifications/hardware", &hw)
	var tasks []any
	_ = client.GetWithContext(ctx, enc+"/task", &tasks)

	palias, _ := s.shield.Register(ctx, "account", aid, nil)
	exp := svc["expiration"]
	renewObj := asMap(svc["renew"])
	iamDisplay := asString(asMap(info["iam"])["displayName"])
	if iamDisplay == "" {
		iamDisplay = asString(info["reverse"])
	}
	commercial := info["commercialRange"]
	if asString(commercial) == "" {
		commercial = hw["commercialRange"]
	}

	baseRedacted, _, err := s.shield.RedactBaremetal(ctx, map[string]any{
		"serviceName":     realName,
		"displayName":     iamDisplay,
		"commercialRange": commercial,
		"datacenter":      info["datacenter"],
		"state":           info["state"],
		"status":          svc["status"],
		"os":              info["os"],
		"monitoring":      info["monitoring"],
		"ip":              info["ip"],
		"expiration":      exp,
		"daysRemaining":   daysUntil(exp),
		"renewMode":       computeRenewMode(renewObj),
	}, palias, aid)
	if err != nil {
		return nil, "", err
	}

	diskGroups := make([]map[string]any, 0)
	if rawDGs, ok := hw["diskGroups"].([]any); ok {
		for _, dgRaw := range rawDGs {
			dg := asMap(dgRaw)
			if len(dg) == 0 {
				continue
			}
			diskGroups = append(diskGroups, map[string]any{
				"description":    dg["description"],
				"numberOfDisks":  dg["numberOfDisks"],
				"diskType":       dg["diskType"],
				"diskSize":       dg["diskSize"],
				"raidController": dg["raidController"],
			})
		}
	}

	baseRedacted["hardware"] = map[string]any{
		"processorName":         hw["processorName"],
		"processorArchitecture": hw["processorArchitecture"],
		"coresPerProcessor":     hw["coresPerProcessor"],
		"threadsPerProcessor":   hw["threadsPerProcessor"],
		"memorySize":            hw["memorySize"],
		"bootMode":              hw["bootMode"],
		"diskGroups":            diskGroups,
	}
	baseRedacted["recentTaskCount"] = len(tasks)
	return baseRedacted, fmt.Sprintf("Inspected bare-metal server %s.", bmAlias), nil
}

func (s *Service) toolBaremetalFirewallGet(ctx context.Context, cfg *store.MCPConfig, args map[string]any) (map[string]any, string, error) {
	srvArg := strings.TrimSpace(asString(args["server"]))
	realName, bmAlias, _, client, err := s.findServerOwnerAccount(ctx, cfg, "baremetal", srvArg)
	if err != nil {
		return nil, "", err
	}

	ipBlock, ipAddr := resolveServerPrimaryIPv4(ctx, client, realName)
	if ipBlock == "" || ipAddr == "" {
		return nil, "", fmt.Errorf("no primary IPv4 found for server %s", bmAlias)
	}

	encBlock := url.PathEscape(ipBlock)
	encIP := url.PathEscape(ipAddr)
	ipAlias, err := s.shield.Register(ctx, "ip", ipAddr, map[string]any{"hint": FormatIPHint(ipAddr)})
	if err != nil {
		return nil, "", err
	}

	var fw map[string]any
	if err := client.GetWithContext(ctx, fmt.Sprintf("/ip/%s/firewall/%s", encBlock, encIP), &fw); err != nil {
		fw = map[string]any{"enabled": false, "state": "not_configured"}
	}

	rules := make([]map[string]any, 0)
	var seqs []int
	if err := client.GetWithContext(ctx, fmt.Sprintf("/ip/%s/firewall/%s/rule", encBlock, encIP), &seqs); err == nil {
		sort.Ints(seqs)
		for _, seq := range seqs {
			var rule map[string]any
			if err := client.GetWithContext(ctx, fmt.Sprintf("/ip/%s/firewall/%s/rule/%d", encBlock, encIP, seq), &rule); err != nil {
				continue
			}
			src := asString(rule["source"])
			if src == "" {
				src = "any"
			}
			seqVal := rule["sequence"]
			if seqVal == nil {
				seqVal = seq
			}
			rules = append(rules, map[string]any{
				"sequence":        seqVal,
				"action":          rule["action"],
				"protocol":        rule["protocol"],
				"source":          s.shield.ScrubText(src),
				"destinationPort": rule["destinationPort"],
				"state":           rule["state"],
			})
		}
	}

	var mitigation map[string]any
	if err := client.GetWithContext(ctx, fmt.Sprintf("/ip/%s/mitigation/%s", encBlock, encIP), &mitigation); err != nil {
		mitigation = map[string]any{"permanent": false, "auto": false, "state": "auto"}
	}

	fwState := asString(fw["state"])
	if fwState == "" {
		fwState = "not_configured"
	}
	mitState := asString(mitigation["state"])
	if mitState == "" {
		mitState = "auto"
	}

	return map[string]any{
		"server": bmAlias,
		"ip": map[string]any{
			"alias":      ipAlias,
			"maskedTail": FormatIPHint(ipAddr),
		},
		"firewall": map[string]any{
			"enabled": asBool(fw["enabled"]),
			"state":   fwState,
		},
		"mitigation": map[string]any{
			"permanent": asBool(mitigation["permanent"]),
			"auto":      asBool(mitigation["auto"]),
			"state":     mitState,
		},
		"rules": rules,
	}, fmt.Sprintf("Inspected IP Edge Firewall & Anti-DDoS state for %s (%s).", bmAlias, ipAlias), nil
}

func (s *Service) toolVPSList(ctx context.Context, cfg *store.MCPConfig, args map[string]any) (map[string]any, string, error) {
	targets, err := s.resolveTargetAccounts(ctx, cfg, asString(args["profile"]))
	if err != nil {
		return nil, "", err
	}

	items := make([]map[string]any, 0)
	for _, t := range targets {
		client, _, err := s.clientForAccountFn(ctx, t.Account.ID)
		if err != nil || client == nil {
			continue
		}
		var names []string
		if err := client.GetWithContext(ctx, "/vps", &names); err != nil {
			continue
		}
		for _, vname := range names {
			enc := "/vps/" + url.PathEscape(vname)
			var detail map[string]any
			if err := client.GetWithContext(ctx, enc, &detail); err != nil {
				continue
			}
			var ips []string
			_ = client.GetWithContext(ctx, enc+"/ips", &ips)
			var svc map[string]any
			_ = client.GetWithContext(ctx, enc+"/serviceInfos", &svc)

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
			vcore := detail["vcore"]
			if vcore == nil {
				vcore = model["vcore"]
			}

			redacted, _, err := s.shield.RedactVPS(ctx, map[string]any{
				"serviceName":   vname,
				"displayName":   detail["displayName"],
				"state":         detail["state"],
				"zone":          detail["zone"],
				"vcore":         vcore,
				"memoryLimitMb": memMB,
				"diskGb":        model["disk"],
				"modelName":     modelStr,
				"ips":           ips,
				"expiration":    exp,
				"daysRemaining": daysUntil(exp),
				"renewMode":     computeRenewMode(renewObj),
			}, t.Alias, t.Account.ID)
			if err == nil {
				items = append(items, redacted)
			}
		}
	}

	return map[string]any{
		"vps":   items,
		"total": len(items),
	}, fmt.Sprintf("Listed %d obfuscated VPS instance(s).", len(items)), nil
}

func (s *Service) toolOrderList(ctx context.Context, cfg *store.MCPConfig, args map[string]any) (map[string]any, string, error) {
	targets, err := s.resolveTargetAccounts(ctx, cfg, asString(args["profile"]))
	if err != nil {
		return nil, "", err
	}
	limit := asInt(args["limit"], 10)
	if limit < 1 {
		limit = 1
	} else if limit > 20 {
		limit = 20
	}

	orders := make([]map[string]any, 0)
	for _, t := range targets {
		client, _, err := s.clientForAccountFn(ctx, t.Account.ID)
		if err != nil || client == nil {
			continue
		}
		var orderIDs []int64
		if err := client.GetWithContext(ctx, "/me/order", &orderIDs); err != nil {
			continue
		}
		sort.Slice(orderIDs, func(i, j int) bool { return orderIDs[i] > orderIDs[j] })
		if len(orderIDs) > limit {
			orderIDs = orderIDs[:limit]
		}

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
			_ = client.GetWithContext(ctx, fmt.Sprintf("/me/order/%d/followUp", oid), &followUp)

			oidVal := detail["orderId"]
			if oidVal == nil {
				oidVal = oid
			}
			redacted, _, err := s.shield.RedactOrder(ctx, map[string]any{
				"orderId":         oidVal,
				"date":            detail["date"],
				"expirationDate":  detail["expirationDate"],
				"priceWithTax":    detail["priceWithTax"],
				"priceWithoutTax": detail["priceWithoutTax"],
				"tax":             detail["tax"],
				"status":          status,
				"followUp":        followUp,
			}, t.Alias)
			if err == nil {
				orders = append(orders, redacted)
			}
		}
	}

	return map[string]any{
		"orders": orders,
		"total":  len(orders),
	}, fmt.Sprintf("Listed %d recent order(s) with payment URLs redacted.", len(orders)), nil
}

func (s *Service) toolOrderFollowUp(ctx context.Context, cfg *store.MCPConfig, args map[string]any) (map[string]any, string, error) {
	ordArg := strings.TrimSpace(asString(args["order"]))
	realOID, ok := s.shield.Resolve("order", ordArg)
	if !ok || realOID == "" {
		return nil, "", fmt.Errorf("unknown order alias '%s'. Call `order_list` first to obtain valid order aliases (e.g. 'ord-01')", ordArg)
	}

	targets, err := s.resolveTargetAccounts(ctx, cfg, asString(args["profile"]))
	if err != nil {
		return nil, "", err
	}

	for _, t := range targets {
		client, _, err := s.clientForAccountFn(ctx, t.Account.ID)
		if err != nil || client == nil {
			continue
		}
		var detail map[string]any
		if err := client.GetWithContext(ctx, "/me/order/"+url.PathEscape(realOID), &detail); err != nil {
			continue
		}
		var status string
		if err := client.GetWithContext(ctx, "/me/order/"+url.PathEscape(realOID)+"/status", &status); err != nil || status == "" {
			status = "unknown"
		}
		var followUp []any
		_ = client.GetWithContext(ctx, "/me/order/"+url.PathEscape(realOID)+"/followUp", &followUp)

		redacted, ordAlias, err := s.shield.RedactOrder(ctx, map[string]any{
			"orderId":         realOID,
			"date":            detail["date"],
			"expirationDate":  detail["expirationDate"],
			"priceWithTax":    detail["priceWithTax"],
			"priceWithoutTax": detail["priceWithoutTax"],
			"tax":             detail["tax"],
			"status":          status,
			"followUp":        followUp,
		}, t.Alias)
		if err != nil {
			return nil, "", err
		}
		return redacted, fmt.Sprintf("Checked 4-stage delivery follow-up for %s.", ordAlias), nil
	}

	return nil, "", fmt.Errorf("order '%s' was not found under any whitelisted profile", ordArg)
}

func (s *Service) toolServiceRenewals(ctx context.Context, cfg *store.MCPConfig, args map[string]any) (map[string]any, string, error) {
	targets, err := s.resolveTargetAccounts(ctx, cfg, asString(args["profile"]))
	if err != nil {
		return nil, "", err
	}

	var hasMaxDays bool
	var maxDays int
	if rawDays, exists := args["days_within"]; exists && rawDays != nil {
		hasMaxDays = true
		maxDays = asInt(rawDays, 0)
	}

	services := make([]map[string]any, 0)
	for _, t := range targets {
		client, _, err := s.clientForAccountFn(ctx, t.Account.ID)
		if err != nil || client == nil {
			continue
		}

		var dedicatedNames []string
		if err := client.GetWithContext(ctx, "/dedicated/server", &dedicatedNames); err == nil {
			for _, srv := range dedicatedNames {
				var info map[string]any
				if err := client.GetWithContext(ctx, "/dedicated/server/"+url.PathEscape(srv)+"/serviceInfos", &info); err != nil {
					continue
				}
				exp := info["expiration"]
				daysRem := daysUntil(exp)
				if hasMaxDays && (daysRem == nil || *daysRem > maxDays) {
					continue
				}
				renewObj := asMap(info["renew"])
				bmAlias, err := s.shield.Register(ctx, "baremetal", srv, map[string]any{
					"accountAlias": t.Alias,
					"accountId":    t.Account.ID,
				})
				if err != nil {
					continue
				}
				status := asString(info["status"])
				if status == "" {
					status = "ok"
				}
				services = append(services, map[string]any{
					"service":       bmAlias,
					"type":          "baremetal",
					"profile":       t.Alias,
					"status":        status,
					"expiration":    exp,
					"daysRemaining": daysRem,
					"renewMode":     computeRenewMode(renewObj),
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
				daysRem := daysUntil(exp)
				if hasMaxDays && (daysRem == nil || *daysRem > maxDays) {
					continue
				}
				renewObj := asMap(info["renew"])
				vpsAlias, err := s.shield.Register(ctx, "vps", vps, map[string]any{
					"accountAlias": t.Alias,
					"accountId":    t.Account.ID,
				})
				if err != nil {
					continue
				}
				status := asString(info["status"])
				if status == "" {
					status = "ok"
				}
				services = append(services, map[string]any{
					"service":       vpsAlias,
					"type":          "vps",
					"profile":       t.Alias,
					"status":        status,
					"expiration":    exp,
					"daysRemaining": daysRem,
					"renewMode":     computeRenewMode(renewObj),
				})
			}
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

	return map[string]any{
		"services": services,
		"total":    len(services),
	}, fmt.Sprintf("Checked renewal matrix (%d service(s)).", len(services)), nil
}

func (s *Service) toolProcurementStatus(ctx context.Context, cfg *store.MCPConfig) (map[string]any, string, error) {
	targets, err := s.resolveTargetAccounts(ctx, cfg, "")
	if err != nil {
		return nil, "", err
	}
	allowedIDs := make(map[string]bool, len(targets))
	aliasByID := make(map[string]string, len(targets))
	for _, t := range targets {
		allowedIDs[t.Account.ID] = true
		aliasByID[t.Account.ID] = t.Alias
	}

	rawQueue, err := s.store.ListQueue(ctx, "")
	if err != nil {
		return nil, "", err
	}
	safeQueue := make([]map[string]any, 0, len(rawQueue))
	for _, q := range rawQueue {
		if q.AccountID != "" && len(allowedIDs) > 0 && !allowedIDs[q.AccountID] {
			continue
		}
		var profilePtr *string
		if pal := aliasByID[q.AccountID]; pal != "" {
			profilePtr = &pal
		}
		safeQueue = append(safeQueue, map[string]any{
			"id":          q.ID,
			"planCode":    q.PlanCode,
			"datacenters": q.Datacenters,
			"options":     q.Options,
			"status":      q.Status,
			"retryCount":  q.RetryCount,
			"quantity":    q.Quantity,
			"purchased":   q.Purchased,
			"autoPay":     false, // Never expose or allow autoPay via MCP
			"profile":     profilePtr,
			"createdAt":   q.CreatedAt,
		})
	}

	rawHistory, err := s.store.ListHistory(ctx, "")
	if err != nil {
		return nil, "", err
	}
	if len(rawHistory) > 25 {
		rawHistory = rawHistory[:25]
	}
	safeHistory := make([]map[string]any, 0, len(rawHistory))
	for _, h := range rawHistory {
		if h.AccountID != "" && len(allowedIDs) > 0 && !allowedIDs[h.AccountID] {
			continue
		}
		var ordPtr *string
		if strings.TrimSpace(h.OrderID) != "" {
			if alias, regErr := s.shield.Register(ctx, "order", h.OrderID, nil); regErr == nil && alias != "" {
				ordPtr = &alias
			}
		}
		var profilePtr *string
		if pal := aliasByID[h.AccountID]; pal != "" {
			profilePtr = &pal
		}
		var errPtr *string
		if h.ErrorMessage != "" {
			scrubbed := s.shield.ScrubText(h.ErrorMessage)
			errPtr = &scrubbed
		}
		safeHistory = append(safeHistory, map[string]any{
			"planCode":     h.PlanCode,
			"datacenter":   h.Datacenter,
			"options":      h.Options,
			"status":       h.Status,
			"order":        ordPtr,
			"price":        h.Price,
			"purchaseTime": h.Timestamp,
			"profile":      profilePtr,
			"errorMessage": errPtr,
		})
	}

	rawSubs, err := s.store.ListStockSubs(ctx, "")
	if err != nil {
		return nil, "", err
	}
	bmSubs := make([]map[string]any, 0, len(rawSubs))
	for _, sub := range rawSubs {
		if sub.AccountID != "" && len(allowedIDs) > 0 && !allowedIDs[sub.AccountID] {
			continue
		}
		bmSubs = append(bmSubs, map[string]any{
			"planCode":        sub.PlanCode,
			"datacenters":     sub.Datacenters,
			"notifyAvailable": sub.NotifyAvailable,
		})
	}

	return map[string]any{
		"queue":                  safeQueue,
		"baremetalSubscriptions": bmSubs,
		"recentPurchaseHistory":  safeHistory,
	}, fmt.Sprintf("Inspected procurement status (%d queue task(s), %d subscription(s)).", len(safeQueue), len(bmSubs)), nil
}

// ==================== L2 Tool Handlers (Hardcoded auto_pay=false & quantity=1) ====================

func (s *Service) toolOrderQuote(ctx context.Context, cfg *store.MCPConfig, args map[string]any) (map[string]any, string, error) {
	if s.deps.Procurement == nil {
		return nil, "", errors.New("procurement service is not configured")
	}
	planCode := strings.TrimSpace(asString(args["plan_code"]))
	if planCode == "" {
		return nil, "", errors.New("`plan_code` is required")
	}
	datacenter := strings.ToLower(strings.TrimSpace(asString(args["datacenter"])))
	if datacenter == "" {
		datacenter = "gra"
	}
	options := extractStringSlice(args["options"])

	targets, err := s.resolveTargetAccounts(ctx, cfg, asString(args["profile"]))
	if err != nil {
		return nil, "", err
	}
	if len(targets) == 0 {
		return nil, "", errors.New("no whitelisted OVH account is available to request a price quote")
	}
	target := targets[0]

	quoteRes, err := s.deps.Procurement.QuoteOrder(ctx, target.Account.ID, planCode, datacenter, options)
	if err != nil {
		return nil, "", fmt.Errorf("failed to retrieve OVH cart quote for %s: %w", planCode, err)
	}
	priceInfo := map[string]any{}
	if quoteRes != nil && quoteRes.Price != nil {
		priceInfo = quoteRes.Price
	}

	q := s.quotes.create(planCode, datacenter, options, target.Account.ID, target.Alias, priceInfo)
	modelName := planCode
	if s.deps.Catalog != nil {
		if resolved, rErr := s.deps.Catalog.ResolveModelName(ctx, planCode); rErr == nil && strings.TrimSpace(resolved) != "" {
			modelName = resolved
		}
	}

	return map[string]any{
		"quoteId":          q.QuoteID,
		"expiresInSeconds": QuoteTTLSeconds,
		"planCode":         planCode,
		"modelName":        modelName,
		"datacenter":       strings.ToUpper(datacenter),
		"options":          options,
		"profile":          target.Alias,
		"quantity":         1,
		"autoPay":          false,
		"price":            priceInfo,
		"nextStep":         "Confirm with the user, then call `order_create_unpaid` with this `quote_id` within 5 minutes to generate an unpaid order.",
	}, fmt.Sprintf("Generated 5-min price quote %s for %s @ %s (profile %s).", q.QuoteID, planCode, strings.ToUpper(datacenter), target.Alias), nil
}

func (s *Service) toolOrderCreateUnpaid(ctx context.Context, cfg *store.MCPConfig, args map[string]any) (map[string]any, string, error) {
	if s.deps.Procurement == nil {
		return nil, "", errors.New("procurement service is not configured")
	}
	quoteID := strings.TrimSpace(asString(args["quote_id"]))
	if quoteID == "" {
		return nil, "", errors.New("`quote_id` is required. Call `order_quote` first to get a valid quote_id")
	}

	allowed, remaining := s.orderLimiter.check(cfg.HourlyOrderLimit)
	if !allowed {
		return nil, "", &PermissionError{
			Message: fmt.Sprintf("Hourly MCP unpaid order limit (%d/hour) reached. Please wait or adjust the limit in the OVH Nexus MCP Gateway settings.", cfg.HourlyOrderLimit),
		}
	}

	quote, ok := s.quotes.consume(quoteID)
	if !ok {
		return nil, "", errors.New("invalid or expired `quote_id`. Quotes expire after 5 minutes and can only be used once. Call `order_quote` again")
	}

	// Re-verify account is still permitted by the whitelist.
	if _, err := s.resolveTargetAccounts(ctx, cfg, quote.ProfileAlias); err != nil {
		return nil, "", err
	}

	// SECURITY REDLINE: auto_pay and waive_retractation_period are unconditionally false and quantity is unconditionally 1.
	req := UnpaidOrderRequest{
		TaskID:                  "mcp-order-" + randomHex(4),
		AccountID:               quote.AccountID,
		PlanCode:                quote.PlanCode,
		Datacenter:              quote.Datacenter,
		Options:                 append([]string(nil), quote.Options...),
		Quantity:                1,
		AutoPay:                 false,
		WaiveRetractationPeriod: false,
	}

	mu := s.checkoutMutexForAccount(quote.AccountID)
	mu.Lock()
	orderRes, err := s.deps.Procurement.CreateUnpaidOrder(ctx, req)
	mu.Unlock()
	if err != nil {
		return nil, "", fmt.Errorf("could not place unpaid order for %s @ %s: %w. Consider using `queue_add` to watch for restock", quote.PlanCode, strings.ToUpper(quote.Datacenter), err)
	}

	s.orderLimiter.record()

	ordAlias := "ord-new"
	if orderRes != nil && strings.TrimSpace(orderRes.OrderID) != "" {
		if registered, regErr := s.shield.Register(ctx, "order", orderRes.OrderID, map[string]any{
			"accountAlias": quote.ProfileAlias,
		}); regErr == nil && registered != "" {
			ordAlias = registered
		}
	}

	price := quote.Price
	if orderRes != nil && len(orderRes.Price) > 0 {
		price = orderRes.Price
	}

	remAfter := remaining - 1
	if remAfter < 0 {
		remAfter = 0
	}

	return map[string]any{
		"status":               "unpaid_order_created",
		"order":                ordAlias,
		"planCode":             quote.PlanCode,
		"datacenter":           strings.ToUpper(quote.Datacenter),
		"options":              quote.Options,
		"profile":              quote.ProfileAlias,
		"quantity":             1,
		"autoPay":              false,
		"price":                price,
		"remainingHourlyQuota": remAfter,
		"paymentInstructions":  "Unpaid order locked in! For security, the payment link is NOT exposed to the AI. It has been dispatched to your notification channel and is available in the OVH Nexus Web Console -> [Orders & Delivery].",
	}, fmt.Sprintf("Created UNPAID order %s for %s @ %s (auto_pay=false, profile %s).", ordAlias, quote.PlanCode, strings.ToUpper(quote.Datacenter), quote.ProfileAlias), nil
}

func (s *Service) toolQueueAdd(ctx context.Context, cfg *store.MCPConfig, args map[string]any) (map[string]any, string, error) {
	planCode := strings.TrimSpace(asString(args["plan_code"]))
	rawDCs := extractStringSlice(args["datacenters"])
	dcs := make([]string, 0, len(rawDCs))
	for _, dc := range rawDCs {
		if low := strings.ToLower(strings.TrimSpace(dc)); low != "" {
			dcs = append(dcs, low)
		}
	}
	options := extractStringSlice(args["options"])
	if planCode == "" || len(dcs) == 0 {
		return nil, "", errors.New("both `plan_code` and a non-empty `datacenters` list are required")
	}

	targets, err := s.resolveTargetAccounts(ctx, cfg, asString(args["profile"]))
	if err != nil {
		return nil, "", err
	}
	if len(targets) == 0 {
		return nil, "", errors.New("no whitelisted OVH account is available for queueing")
	}
	target := targets[0]

	// SECURITY REDLINE: Hardcode AutoPay=false, WaiveRetractationPeriod=false, Quantity=1.
	created, err := s.store.CreateQueueItem(ctx, store.QueueItem{
		AccountID:               target.Account.ID,
		PlanCode:                planCode,
		Datacenters:             dcs,
		Options:                 options,
		Quantity:                1,
		MaxRetryCount:           50,
		Status:                  "running",
		RetryInterval:           30,
		AutoPay:                 false,
		WaiveRetractationPeriod: false,
	})
	if err != nil {
		return nil, "", err
	}

	return map[string]any{
		"taskId":      created.ID,
		"planCode":    planCode,
		"datacenters": dcs,
		"options":     options,
		"profile":     target.Alias,
		"quantity":    1,
		"autoPay":     false,
		"status":      "running",
	}, fmt.Sprintf("Added Auto-Order Queue task %s for %s @ %s (auto_pay=false, quantity=1).", created.ID, planCode, strings.ToUpper(strings.Join(dcs, ","))), nil
}

func (s *Service) toolQueueManage(ctx context.Context, cfg *store.MCPConfig, args map[string]any) (map[string]any, string, error) {
	taskID := strings.TrimSpace(asString(args["task_id"]))
	action := strings.ToLower(strings.TrimSpace(asString(args["action"])))
	if taskID == "" || (action != "pause" && action != "resume" && action != "delete") {
		return nil, "", errors.New("valid `task_id` and `action` ('pause', 'resume', or 'delete') are required")
	}

	targets, err := s.resolveTargetAccounts(ctx, cfg, "")
	if err != nil {
		return nil, "", err
	}
	allowedIDs := make(map[string]bool, len(targets))
	for _, t := range targets {
		allowedIDs[t.Account.ID] = true
	}

	item, err := s.store.GetQueueItem(ctx, taskID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, "", fmt.Errorf("queue task '%s' not found", taskID)
		}
		return nil, "", err
	}
	if item.AccountID != "" && len(allowedIDs) > 0 && !allowedIDs[item.AccountID] {
		return nil, "", &PermissionError{
			Message: "Queue task belongs to an account outside the MCP whitelist.",
		}
	}

	resultStatus := item.Status
	switch action {
	case "delete":
		if err := s.store.DeleteQueueItem(ctx, taskID); err != nil {
			return nil, "", err
		}
		resultStatus = "deleted"
	case "pause":
		item.Status = "paused"
		if _, err := s.store.UpdateQueueItem(ctx, *item); err != nil {
			return nil, "", err
		}
		resultStatus = "paused"
	case "resume":
		item.Status = "running"
		if _, err := s.store.UpdateQueueItem(ctx, *item); err != nil {
			return nil, "", err
		}
		resultStatus = "running"
	}

	return map[string]any{
		"taskId": taskID,
		"action": action,
		"status": resultStatus,
	}, fmt.Sprintf("Performed '%s' on Auto-Order Queue task %s.", action, taskID), nil
}

func (s *Service) toolStockSubscriptionManage(ctx context.Context, cfg *store.MCPConfig, args map[string]any) (map[string]any, string, error) {
	target := strings.ToLower(strings.TrimSpace(asString(args["target"])))
	action := strings.ToLower(strings.TrimSpace(asString(args["action"])))
	planCode := strings.TrimSpace(asString(args["plan_code"]))
	if planCode == "" || (target != "baremetal" && target != "vps") || (action != "add" && action != "remove") {
		return nil, "", errors.New("valid `target` ('baremetal'|'vps'), `action` ('add'|'remove'), and `plan_code` are required")
	}

	targets, err := s.resolveTargetAccounts(ctx, cfg, "")
	if err != nil {
		return nil, "", err
	}
	accountID := ""
	if len(targets) > 0 {
		accountID = targets[0].Account.ID
	}

	if target == "baremetal" {
		serverName := planCode
		if s.deps.Catalog != nil {
			if resolved, rErr := s.deps.Catalog.ResolveModelName(ctx, planCode); rErr == nil && strings.TrimSpace(resolved) != "" {
				serverName = resolved
			}
		}
		if s.deps.Monitor != nil {
			if action == "add" {
				err = s.deps.Monitor.AddStockSubscription(ctx, accountID, planCode, []string{}, serverName)
			} else {
				err = s.deps.Monitor.RemoveStockSubscription(ctx, accountID, planCode)
			}
		} else {
			if action == "add" {
				_, err = s.store.UpsertStockSub(ctx, store.StockSubscription{
					AccountID:       accountID,
					PlanCode:        planCode,
					ServerName:      serverName,
					Datacenters:     []string{},
					NotifyAvailable: true,
				})
			} else {
				err = s.store.DeleteStockSub(ctx, accountID, planCode)
				if errors.Is(err, store.ErrNotFound) {
					err = nil
				}
			}
		}
	} else {
		if s.deps.Monitor != nil {
			if action == "add" {
				err = s.deps.Monitor.AddVPSSubscription(ctx, planCode)
			} else {
				err = s.deps.Monitor.RemoveVPSSubscription(ctx, planCode)
			}
		} else {
			subs, listErr := s.store.ListVPSSubs(ctx)
			if listErr != nil {
				return nil, "", listErr
			}
			if action == "add" {
				found := false
				for _, sub := range subs {
					if sub.PlanCode == planCode {
						found = true
						break
					}
				}
				if !found {
					_, err = s.store.UpsertVPSSub(ctx, store.VPSSubscription{
						PlanCode:        planCode,
						OVHSubsidiary:   "IE",
						Datacenters:     []string{},
						MonitorLinux:    true,
						NotifyAvailable: true,
					})
				}
			} else {
				for _, sub := range subs {
					if sub.PlanCode == planCode || sub.ID == planCode {
						if delErr := s.store.DeleteVPSSub(ctx, sub.ID); delErr != nil && !errors.Is(delErr, store.ErrNotFound) {
							err = delErr
						}
					}
				}
			}
		}
	}
	if err != nil {
		return nil, "", err
	}

	return map[string]any{
		"target":   target,
		"action":   action,
		"planCode": planCode,
		"success":  true,
	}, fmt.Sprintf("Updated %s stock subscription (%s: %s).", target, action, planCode), nil
}

// ==================== L3 Tool Handlers (Reversible Operations, requires TOTP + operations scope) ====================

func (s *Service) toolBaremetalReboot(ctx context.Context, cfg *store.MCPConfig, args map[string]any) (map[string]any, string, error) {
	srvArg := strings.TrimSpace(asString(args["server"]))
	realName, bmAlias, _, client, err := s.findServerOwnerAccount(ctx, cfg, "baremetal", srvArg)
	if err != nil {
		return nil, "", err
	}

	var res map[string]any
	if err := client.PostWithContext(ctx, "/dedicated/server/"+url.PathEscape(realName)+"/reboot", nil, &res); err != nil {
		return nil, "", err
	}
	return map[string]any{
		"server": bmAlias,
		"action": "reboot",
		"status": "initiated",
		"taskId": res["taskId"],
	}, fmt.Sprintf("Initiated hardware reboot for bare-metal server %s.", bmAlias), nil
}

func (s *Service) toolVPSReboot(ctx context.Context, cfg *store.MCPConfig, args map[string]any) (map[string]any, string, error) {
	vpsArg := strings.TrimSpace(asString(args["vps"]))
	realName, vpsAlias, _, client, err := s.findServerOwnerAccount(ctx, cfg, "vps", vpsArg)
	if err != nil {
		return nil, "", err
	}

	if err := client.PostWithContext(ctx, "/vps/"+url.PathEscape(realName)+"/reboot", nil, nil); err != nil {
		return nil, "", err
	}
	return map[string]any{
		"vps":    vpsAlias,
		"action": "reboot",
		"status": "initiated",
	}, fmt.Sprintf("Initiated reboot for VPS %s.", vpsAlias), nil
}

func (s *Service) toolBaremetalIPMIReset(ctx context.Context, cfg *store.MCPConfig, args map[string]any) (map[string]any, string, error) {
	srvArg := strings.TrimSpace(asString(args["server"]))
	realName, bmAlias, _, client, err := s.findServerOwnerAccount(ctx, cfg, "baremetal", srvArg)
	if err != nil {
		return nil, "", err
	}

	enc := "/dedicated/server/" + url.PathEscape(realName)
	taskIDs := make([]any, 0, 2)
	for _, sub := range []string{"resetInterface", "resetSessions"} {
		var res map[string]any
		if err := client.PostWithContext(ctx, enc+"/features/ipmi/"+sub, nil, &res); err == nil {
			if res["taskId"] != nil {
				taskIDs = append(taskIDs, res["taskId"])
			}
		}
	}
	return map[string]any{
		"server":  bmAlias,
		"action":  "ipmi_reset",
		"status":  "initiated",
		"taskIds": taskIDs,
	}, fmt.Sprintf("Reset IPMI interface & sessions for bare-metal server %s.", bmAlias), nil
}

func (s *Service) toolBaremetalFirewallUpdate(ctx context.Context, cfg *store.MCPConfig, args map[string]any) (map[string]any, string, error) {
	srvArg := strings.TrimSpace(asString(args["server"]))
	op := strings.TrimSpace(asString(args["operation"]))
	realName, bmAlias, _, client, err := s.findServerOwnerAccount(ctx, cfg, "baremetal", srvArg)
	if err != nil {
		return nil, "", err
	}

	ipBlock, ipAddr := resolveServerPrimaryIPv4(ctx, client, realName)
	if ipBlock == "" || ipAddr == "" {
		return nil, "", fmt.Errorf("no primary IPv4 found for server %s", bmAlias)
	}

	encBlock := url.PathEscape(ipBlock)
	encIP := url.PathEscape(ipAddr)
	ipAlias, err := s.shield.Register(ctx, "ip", ipAddr, map[string]any{"hint": FormatIPHint(ipAddr)})
	if err != nil {
		return nil, "", err
	}

	switch op {
	case "set_mitigation":
		permanent := true
		if rawPerm, exists := args["permanent_mitigation"]; exists {
			permanent = asBool(rawPerm)
		}
		var mitCheck map[string]any
		if err := client.GetWithContext(ctx, fmt.Sprintf("/ip/%s/mitigation/%s", encBlock, encIP), &mitCheck); err == nil {
			if putErr := client.PutWithContext(ctx, fmt.Sprintf("/ip/%s/mitigation/%s", encBlock, encIP), map[string]bool{
				"permanent": permanent,
			}, nil); putErr != nil {
				return nil, "", putErr
			}
		} else if permanent {
			if postErr := client.PostWithContext(ctx, fmt.Sprintf("/ip/%s/mitigation", encBlock), map[string]string{
				"ipOnMitigation": ipAddr,
			}, nil); postErr != nil {
				return nil, "", postErr
			}
			_ = client.PutWithContext(ctx, fmt.Sprintf("/ip/%s/mitigation/%s", encBlock, encIP), map[string]bool{
				"permanent": true,
			}, nil)
		}
		return map[string]any{
			"server":              bmAlias,
			"ip":                  ipAlias,
			"operation":           op,
			"permanentMitigation": permanent,
		}, fmt.Sprintf("Updated permanent Anti-DDoS mitigation=%t on %s (%s).", permanent, bmAlias, ipAlias), nil

	case "add_rule":
		seq := asInt(args["sequence"], 0)
		if seq < 0 || seq > 19 {
			return nil, "", errors.New("`sequence` must be between 0 and 19")
		}
		ruleAction := strings.TrimSpace(asString(args["rule_action"]))
		if ruleAction == "" {
			ruleAction = "permit"
		}
		if ruleAction != "permit" && ruleAction != "deny" {
			return nil, "", errors.New("`rule_action` must be 'permit' or 'deny'")
		}
		protocol := strings.TrimSpace(asString(args["protocol"]))
		if protocol == "" {
			protocol = "tcp"
		}

		var fwCheck map[string]any
		if err := client.GetWithContext(ctx, fmt.Sprintf("/ip/%s/firewall/%s", encBlock, encIP), &fwCheck); err != nil {
			_ = client.PostWithContext(ctx, fmt.Sprintf("/ip/%s/firewall", encBlock), map[string]string{
				"ipOnFirewall": ipAddr,
			}, nil)
		}

		payload := map[string]any{
			"sequence": seq,
			"action":   ruleAction,
			"protocol": protocol,
		}
		if src := strings.TrimSpace(asString(args["source"])); src != "" && strings.ToLower(src) != "any" {
			payload["source"] = src
		}
		if destPort, has := args["destination_port"]; has && destPort != nil && (protocol == "tcp" || protocol == "udp") {
			if p := asInt(destPort, 0); p > 0 {
				payload["destinationPort"] = p
			}
		}

		if err := client.PostWithContext(ctx, fmt.Sprintf("/ip/%s/firewall/%s/rule", encBlock, encIP), payload, nil); err != nil {
			return nil, "", err
		}
		return map[string]any{
			"server":     bmAlias,
			"ip":         ipAlias,
			"operation":  op,
			"sequence":   seq,
			"ruleAction": ruleAction,
			"protocol":   protocol,
		}, fmt.Sprintf("Added IP Edge Firewall rule #%d (%s %s) on %s (%s).", seq, ruleAction, protocol, bmAlias, ipAlias), nil

	case "delete_rule":
		seq := asInt(args["sequence"], 0)
		if err := client.DeleteWithContext(ctx, fmt.Sprintf("/ip/%s/firewall/%s/rule/%d", encBlock, encIP, seq), nil); err != nil {
			return nil, "", err
		}
		return map[string]any{
			"server":    bmAlias,
			"ip":        ipAlias,
			"operation": op,
			"sequence":  seq,
			"deleted":   true,
		}, fmt.Sprintf("Deleted IP Edge Firewall rule #%d on %s (%s).", seq, bmAlias, ipAlias), nil

	default:
		return nil, "", fmt.Errorf("unsupported firewall operation: %s", op)
	}
}

func extractStringSlice(v any) []string {
	if v == nil {
		return []string{}
	}
	switch t := v.(type) {
	case []string:
		out := make([]string, 0, len(t))
		for _, item := range t {
			if s := strings.TrimSpace(item); s != "" {
				out = append(out, s)
			}
		}
		return out
	case []any:
		out := make([]string, 0, len(t))
		for _, item := range t {
			if s := strings.TrimSpace(asString(item)); s != "" {
				out = append(out, s)
			}
		}
		return out
	default:
		return []string{}
	}
}

func randomHex(numBytes int) string {
	b := make([]byte, numBytes)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func daysUntil(dateStr any) *int {
	raw := strings.TrimSpace(asString(dateStr))
	if raw == "" {
		return nil
	}
	var parsed time.Time
	var err error
	if strings.Contains(raw, "T") {
		parsed, err = time.Parse(time.RFC3339, raw)
		if err != nil {
			parsed, err = time.Parse("2006-01-02T15:04:05-07:00", raw)
		}
		if err != nil && len(raw) >= 19 {
			parsed, err = time.ParseInLocation("2006-01-02T15:04:05", raw[:19], time.UTC)
		}
	} else if len(raw) >= 10 {
		parsed, err = time.ParseInLocation("2006-01-02", raw[:10], time.UTC)
	} else {
		return nil
	}
	if err != nil {
		return nil
	}
	days := int(parsed.UTC().Sub(time.Now().UTC()).Seconds() / 86400)
	return &days
}

func computeRenewMode(renewObj map[string]any) string {
	if len(renewObj) == 0 {
		return "manual"
	}
	if asBool(renewObj["deleteAtExpiration"]) {
		return "deleteAtExpiration"
	}
	if asBool(renewObj["automatic"]) {
		return "auto"
	}
	return "manual"
}
