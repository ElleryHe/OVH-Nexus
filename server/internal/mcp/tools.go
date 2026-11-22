package mcp

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"ovh-nexus/server/internal/catalog"
	"ovh-nexus/server/internal/logger"
	"ovh-nexus/server/internal/procurement"
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
	// --- L1: Read-Only Insight (12 tools, scope: read) ---
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
		Description: "Query real-time global OVHcloud bare-metal server stock matrix, configuration keys (`configKey`), ex-VAT `estimatedMonthlyPrice`, `originalMonthlyPrice`, `setupFee`, promotions (`hasPromo`, `promoBadge`, `pricingModes`), and `listingState` ('listed', 'announced', 'presale', 'promo') without requiring an API key. Includes catalog-announced models (e.g. Limited Edition SKUs with 0 datacenter availability rows).",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"plan_code":      map[string]any{"type": "string", "description": "Optional exact or partial planCode / model name filter (e.g. '24ska01', '27skle', 'KS-A', 'rise')."},
				"datacenter":     map[string]any{"type": "string", "description": "Optional datacenter filter (e.g. 'gra', 'rbx', 'sbg', 'bhs', 'fra', 'vin')."},
				"only_available": map[string]any{"type": "boolean", "description": "If true, return only configurations currently in stock."},
				"listing_state":  map[string]any{"type": "string", "enum": []string{"all", "listed", "announced", "presale", "promo"}, "default": "all", "description": "Filter by listingState: 'all', 'listed' (orderable in catalog), 'announced' (in public catalog with 0 availability rows, not yet on sale), 'presale' (in availability feed but not yet in public catalog), or 'promo' (active recurring or setup fee promotion)."},
				"only_promo":     map[string]any{"type": "boolean", "default": false, "description": "If true, only return server plans with active recurring or setup fee promotions (hasPromo=true)."},
				"only_announced": map[string]any{"type": "boolean", "default": false, "description": "If true, only return 'announced' models (in public catalog with 0 availability rows, not yet on sale)."},
				"only_presale":   map[string]any{"type": "boolean", "default": false, "description": "If true, only return 'presale' models that appear in datacenter availability feeds but are not yet in the public catalog."},
				"presale_only":   map[string]any{"type": "boolean", "default": false, "description": "Alias for only_presale: if true, only return 'presale' / unlisted models."},
				"mode":           map[string]any{"type": "string", "enum": []string{"summary", "compact", "full"}, "default": "summary", "description": "Output detail mode ('summary', 'compact', or 'full'). All modes include listingState, configKey, isDefault, estimatedMonthlyPrice, setupFee, hasPromo, promoBadge, pricingModes, currency, and listedAt."},
				"offset":         map[string]any{"type": "integer", "default": 0, "description": "Pagination offset (0-based) for browsing beyond the first page."},
				"limit":          map[string]any{"type": "integer", "default": 50, "description": "Max plans to return (1-200, default 50)."},
			},
			"additionalProperties": false,
		},
	},
	{
		Name:        "catalog_promotions",
		Scope:       "read",
		Description: "Inspect active public OVHcloud bare-metal promotions, auto-applied campaign rules (context='standard' vs voucher codes), eligible billing/commitment modes (`default`, `upfront12`, `upfront24`), expiration countdowns, dual-promo stacking (recurring + setup fee), addon pricing notes, and real-time in-stock promoted models without requiring an API key.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"plan_code":     map[string]any{"type": "string", "description": "Optional exact or partial planCode / model name filter (e.g. '24sys02', 'GAME-1', 'scale')."},
				"range":         map[string]any{"type": "string", "description": "Optional product range filter (e.g. 'ECO', 'KS', 'SYS', 'RISE', 'ADV', 'SCALE', 'HGR')."},
				"promo_type":    map[string]any{"type": "string", "enum": []string{"all", "recurring", "setup"}, "default": "all", "description": "Filter by promotion scope: 'all', 'recurring' (monthly/upfront discount), or 'setup' (installation fee waiver/discount)."},
				"only_in_stock": map[string]any{"type": "boolean", "default": false, "description": "If true, return only promoted models currently in stock in at least one datacenter."},
				"subsidiary":    map[string]any{"type": "string", "description": "Optional OVH subsidiary code (e.g. 'IE', 'FR', 'DE', 'CA', 'US'). Defaults to the active account's zone or 'IE'."},
			},
			"additionalProperties": false,
		},
	},
	{
		Name:        "catalog_sku_changes",
		Scope:       "read",
		Description: "List recent public OVHcloud shelf SKU changes (added, saleStarted, replaced, removed), trackedSkus, and summary counts (totalSkus, listedSkus, announcedSkus, presaleSkus) detected by the SKU Watcher.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"limit":         map[string]any{"type": "integer", "description": "Maximum change events to return (1-50, default 20)."},
				"change_type":   map[string]any{"type": "string", "enum": []string{"ALL", "ADDED", "REMOVED", "REPLACED", "all", "added", "removed", "replaced"}, "description": "Filter by change type ('ALL', 'ADDED', 'REMOVED', 'REPLACED'). Note: 'ADDED' with saleStarted=true indicates an announced SKU that went on sale; 'REPLACED' indicates a same-product-line planCode replacement (with replacedFrom)."},
				"type":          map[string]any{"type": "string", "enum": []string{"all", "added", "removed", "replaced", "ALL", "ADDED", "REMOVED", "REPLACED"}, "description": "Alias for change_type ('all', 'added', 'removed', 'replaced')."},
				"listing_state": map[string]any{"type": "string", "enum": []string{"all", "listed", "announced", "presale"}, "default": "all", "description": "Filter changes and trackedSkus by listingState ('all', 'listed', 'announced', 'presale')."},
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
		Description: "Step 1 of 2 for placing an unpaid server order (and live cart verification for commitment modes or private coupons): query exact tax-inclusive/exclusive pricing and setup fees via an ephemeral OVH cart and generate a 5-minute confirmation `quote_id`. Does NOT place an order or charge money. IMPORTANT: auto-applied catalog promotions (autoApplied=true from `catalog_promotions`) apply automatically by `pricing_mode` — do NOT pass them in `coupon`.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"plan_code":  map[string]any{"type": "string", "description": "Server planCode (e.g. '24ska01', '24sys02-v3')."},
				"datacenter": map[string]any{"type": "string", "description": "Target datacenter code (e.g. 'gra', 'rbx', 'bhs')."},
				"options": map[string]any{
					"type":        "array",
					"items":       map[string]any{"type": "string"},
					"description": "Optional hardware addon planCodes (e.g. ['ram-64g-ecc-2133', 'softraid-2x450nvme']).",
				},
				"pricing_mode": map[string]any{"type": "string", "enum": []string{"default", "upfront12", "upfront24"}, "default": "default", "description": "Billing / commitment pricing mode ('default' monthly, 'upfront12' 12-month upfront, or 'upfront24' 24-month upfront)."},
				"coupon":       map[string]any{"type": "string", "description": "Optional private OVH cart voucher/coupon code to test & apply. Never pass auto-applied catalog campaign IDs (e.g. BF26_ECO_20OFF) here."},
				"profile":      map[string]any{"type": "string", "description": "Optional account profile alias (e.g. 'acct-01')."},
			},
			"required":             []string{"plan_code", "datacenter"},
			"additionalProperties": false,
		},
	},
	{
		Name:        "order_create_unpaid",
		Scope:       "procurement",
		Description: "Step 2 of 2: place an UNPAID purchase order using a valid `quote_id` from `order_quote` (preserving the quoted `pricing_mode` and `coupon`). Hardcoded to `auto_pay=false` and `quantity=1` — never charges any payment method. The payment link is sent directly to the owner's notification channel and Web Console.",
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
		Description: "Add a server target to the background Auto-Order Queue to automatically lock in an UNPAID order (`auto_pay=false`, `quantity=1` hardcoded) when stock appears, with optional commitment `pricing_mode`, private `coupon`, and `max_monthly_price` ex-VAT safeguard.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"plan_code": map[string]any{"type": "string", "description": "Server planCode (e.g. '24ska01', '24sys02-v3')."},
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
				"pricing_mode":      map[string]any{"type": "string", "enum": []string{"default", "upfront12", "upfront24"}, "default": "default", "description": "Billing / commitment pricing mode ('default', 'upfront12', or 'upfront24')."},
				"coupon":            map[string]any{"type": "string", "description": "Optional private OVH cart voucher/coupon code (fail-fast if rejected by OVH). Do NOT pass auto-applied catalog campaign IDs here."},
				"max_monthly_price": map[string]any{"type": "number", "description": "Optional maximum acceptable monthly equivalent price (ex-VAT, must be > 0). Aborts auto-order if live cart quote exceeds this cap."},
				"max_setup_fee":     map[string]any{"type": "number", "description": "Optional maximum acceptable installation/setup fee (ex-VAT, must be > 0). Aborts auto-order if live cart setup fee exceeds this cap."},
				"profile":           map[string]any{"type": "string", "description": "Optional account profile alias (e.g. 'acct-01')."},
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

// AllToolCatalog returns the full 21-tool catalog metadata for WebUI rendering.
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
	case "catalog_promotions":
		result, summary, err = s.toolCatalogPromotions(ctx, args)
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

func entryHasPromo(it CatalogEntry) bool {
	if it.HasPromo {
		return true
	}
	for _, m := range it.PricingModes {
		if m.HasPromo {
			return true
		}
	}
	return false
}

func (s *Service) toolCatalogAvailability(ctx context.Context, args map[string]any) (map[string]any, string, error) {
	planFilter := strings.TrimSpace(asString(args["plan_code"]))
	dcFilter := strings.TrimSpace(asString(args["datacenter"]))
	onlyAvail := asBool(args["only_available"])
	onlyPromo := asBool(args["only_promo"]) || asBool(args["promo_only"])
	presaleOnly := asBool(args["presale_only"]) || asBool(args["only_presale"])
	onlyAnnounced := asBool(args["only_announced"]) || asBool(args["announced_only"])
	listingState := strings.ToLower(strings.TrimSpace(asString(args["listing_state"])))
	if listingState == "promo" {
		onlyPromo = true
		listingState = "promo"
	} else if listingState == "" || listingState == "all" {
		if onlyPromo {
			listingState = "promo"
		} else if onlyAnnounced {
			listingState = "announced"
		} else if presaleOnly {
			listingState = "presale"
		} else {
			listingState = "all"
		}
	}

	offset := asInt(args["offset"], 0)
	if offset < 0 {
		offset = 0
	}
	limit := asInt(args["limit"], 50)
	if limit < 1 {
		limit = 1
	} else if limit > 200 {
		limit = 200
	}

	if s.deps.Catalog != nil {
		var (
			items        []CatalogEntry
			totalMatched int
			err          error
		)
		if filterReader, ok := s.deps.Catalog.(CatalogAvailabilityFilterReader); ok && (!onlyPromo || listingState == "promo") {
			items, totalMatched, err = filterReader.QueryAvailabilityFiltered(ctx, planFilter, dcFilter, listingState, onlyAvail, presaleOnly, onlyAnnounced, offset, limit)
		} else if !onlyPromo && (listingState == "all" || listingState == "presale") {
			items, totalMatched, err = s.deps.Catalog.QueryAvailability(ctx, planFilter, dcFilter, onlyAvail, listingState == "presale", offset, limit)
		} else {
			var allItems []CatalogEntry
			var qErr error
			if filterReader, ok := s.deps.Catalog.(CatalogAvailabilityFilterReader); ok {
				stateQuery := listingState
				if stateQuery == "promo" {
					stateQuery = "all"
				}
				allItems, _, qErr = filterReader.QueryAvailabilityFiltered(ctx, planFilter, dcFilter, stateQuery, onlyAvail, presaleOnly, onlyAnnounced, 0, 10000)
			} else {
				allItems, _, qErr = s.deps.Catalog.QueryAvailability(ctx, planFilter, dcFilter, onlyAvail, false, 0, 10000)
			}
			if qErr != nil {
				return nil, "", qErr
			}
			var matched []CatalogEntry
			for _, it := range allItems {
				st := strings.ToLower(strings.TrimSpace(it.ListingState))
				if st == "" {
					st = "listed"
				}
				if listingState != "all" && listingState != "promo" && st != listingState {
					continue
				}
				if (onlyPromo || listingState == "promo") && !entryHasPromo(it) {
					continue
				}
				matched = append(matched, it)
			}
			totalMatched = len(matched)
			off := min(offset, totalMatched)
			end := min(off+limit, totalMatched)
			items = matched[off:end]
		}
		if err != nil {
			return nil, "", err
		}
		if items == nil {
			items = []CatalogEntry{}
		}
		for i := range items {
			st := strings.ToLower(strings.TrimSpace(items[i].ListingState))
			switch st {
			case "":
				items[i].ListingState = "listed"
				items[i].Orderable = true
			case "listed":
				items[i].ListingState = "listed"
				items[i].Orderable = true
			case "presale":
				items[i].ListingState = "presale"
				items[i].Orderable = false
			case "announced":
				items[i].ListingState = "announced"
				items[i].Orderable = false
			default:
				items[i].ListingState = st
			}
			if strings.TrimSpace(items[i].ConfigKey) == "" {
				items[i].ConfigKey = "default"
			}
			if strings.TrimSpace(items[i].Currency) == "" {
				items[i].Currency = "EUR"
			}
			if items[i].OriginalMonthlyPrice == 0 && items[i].EstimatedMonthlyPrice > 0 {
				items[i].OriginalMonthlyPrice = items[i].EstimatedMonthlyPrice
			}
			items[i].FreeSetup = items[i].SetupFee <= 0
			if items[i].Generation == 0 {
				items[i].Generation = catalog.InferPlanGeneration(items[i].PlanCode)
			}
			if items[i].AvailableDatacenters == nil {
				items[i].AvailableDatacenters = []string{}
			}
		}
		if offset > totalMatched {
			offset = totalMatched
		}
		var nextOffset any
		if offset+len(items) < totalMatched {
			nextOffset = offset + len(items)
		}
		return map[string]any{
			"items":        items,
			"offset":       offset,
			"returned":     len(items),
			"totalMatched": totalMatched,
			"nextOffset":   nextOffset,
		}, fmt.Sprintf("Queried bare-metal stock matrix (matched %d plan(s), returned %d).", totalMatched, len(items)), nil
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
	dcDispLow := ""
	if dcLow != "" {
		dcDispLow = strings.ToLower(catalog.NormalizeDisplayDatacenter(dcLow))
	}

	grouped := make(map[string]*CatalogEntry)
	order := make([]string, 0)
	for _, entry := range rawAvail {
		pcode := strings.TrimSpace(asString(entry["planCode"]))
		if pcode == "" || catalog.IsNonServerAddonCode(pcode) {
			continue
		}
		if planLow != "" && !strings.Contains(strings.ToLower(pcode), planLow) {
			continue
		}
		st := strings.ToLower(strings.TrimSpace(asString(entry["listingState"])))
		if st != "presale" && st != "announced" {
			st = "listed"
		}
		orderable := st == "listed"
		if listingState != "all" && listingState != "promo" && st != listingState {
			continue
		}
		hasPromo := asBool(entry["hasPromo"])
		if (onlyPromo || listingState == "promo") && !hasPromo {
			continue
		}

		matchedDC := dcLow == ""
		var inStock []string
		rawDCs, _ := entry["datacenters"].([]any)
		for _, dcRaw := range rawDCs {
			dcMap := asMap(dcRaw)
			rawDC := strings.ToLower(strings.TrimSpace(asString(dcMap["datacenter"])))
			dispDC := catalog.NormalizeDisplayDatacenter(rawDC)
			if dispDC == "" {
				continue
			}
			availVal := strings.TrimSpace(asString(dcMap["availability"]))
			if availVal == "" {
				availVal = "unavailable"
			}
			if dcLow != "" {
				if !strings.EqualFold(dispDC, dcDispLow) && !strings.Contains(rawDC, dcLow) && !strings.Contains(strings.ToLower(dispDC), dcLow) {
					continue
				}
				matchedDC = true
			}
			if availVal != "unavailable" && availVal != "unknown" {
				inStock = append(inStock, fmt.Sprintf("%s (%s)", strings.ToUpper(dispDC), availVal))
			}
		}
		if !matchedDC {
			continue
		}
		if onlyAvail && len(inStock) == 0 {
			continue
		}
		rec, exists := grouped[pcode]
		if !exists {
			cfgKey := strings.TrimSpace(asString(entry["configKey"]))
			if cfgKey == "" {
				cfgKey = catalog.BuildConfigKey(asString(entry["memory"]), asString(entry["storage"]))
			}
			currency := strings.TrimSpace(asString(entry["currency"]))
			if currency == "" {
				currency = "EUR"
			}
			rec = &CatalogEntry{
				PlanCode:             pcode,
				ModelName:            pcode,
				CPU:                  asString(entry["server"]),
				ConfigKey:            cfgKey,
				IsDefault:            true,
				FreeSetup:            true,
				HasPromo:             hasPromo,
				PromoCode:            asString(entry["promoCode"]),
				PromoBadge:           asString(entry["promoBadge"]),
				PromoDescription:     asString(entry["promoDescription"]),
				Currency:             currency,
				ListingState:         st,
				ListedAt:             asString(entry["listedAt"]),
				Orderable:            orderable,
				Generation:           catalog.InferPlanGeneration(pcode),
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

	totalMatched := len(order)
	if offset > totalMatched {
		offset = totalMatched
	}
	end := min(offset+limit, totalMatched)
	items := make([]CatalogEntry, 0, end-offset)
	for _, pcode := range order[offset:end] {
		items = append(items, *grouped[pcode])
	}
	var nextOffset any
	if offset+len(items) < totalMatched {
		nextOffset = offset + len(items)
	}
	return map[string]any{
		"items":        items,
		"offset":       offset,
		"returned":     len(items),
		"totalMatched": totalMatched,
		"nextOffset":   nextOffset,
	}, fmt.Sprintf("Queried bare-metal stock matrix (matched %d plan(s), returned %d).", totalMatched, len(items)), nil
}

func (s *Service) toolCatalogSKUChanges(ctx context.Context, args map[string]any) (map[string]any, string, error) {
	limit := asInt(args["limit"], 20)
	if limit < 1 {
		limit = 1
	} else if limit > 50 {
		limit = 50
	}
	changeType := strings.TrimSpace(asString(args["change_type"]))
	if changeType == "" {
		changeType = strings.TrimSpace(asString(args["type"]))
	}
	if changeType == "" {
		changeType = "ALL"
	}
	changeType = strings.ToUpper(changeType)

	listingState := strings.ToLower(strings.TrimSpace(asString(args["listing_state"])))
	if listingState == "" {
		listingState = "all"
	}

	var (
		status      map[string]any
		changes     []map[string]any
		trackedSkus []map[string]any
		err         error
	)

	if monFilter, ok := s.deps.Monitor.(SKUChangesFilterReader); ok && monFilter != nil {
		status, changes, trackedSkus, err = monFilter.GetSKUChangesFiltered(ctx, limit, changeType, listingState)
		if err != nil {
			return nil, "", err
		}
	} else if monReader, ok := s.deps.Monitor.(SKUChangesReader); ok && monReader != nil {
		status, changes, err = monReader.GetSKUChanges(ctx, limit, changeType)
		if err != nil {
			return nil, "", err
		}
	} else if catFilter, ok := s.deps.Catalog.(SKUChangesFilterReader); ok && catFilter != nil {
		status, changes, trackedSkus, err = catFilter.GetSKUChangesFiltered(ctx, limit, changeType, listingState)
		if err != nil {
			return nil, "", err
		}
	} else if s.deps.Catalog != nil {
		status, changes, err = s.deps.Catalog.GetSKUChanges(ctx, limit, changeType)
		if err != nil {
			return nil, "", err
		}
	} else if s.store != nil {
		st, stErr := s.store.LoadSKUWatcherState(ctx)
		if stErr != nil {
			return nil, "", stErr
		}
		_ = json.Unmarshal(st.ChangesJSON, &changes)
		status = map[string]any{
			"initialized": st.Initialized,
			"endpoint":    st.Endpoint,
			"updatedAt":   st.UpdatedAt,
		}
	}

	if status == nil {
		status = map[string]any{}
	}

	// If trackedSkus were not provided by the reader, load and enrich from SQLite store when available.
	var storeSKUs map[string]map[string]any
	if s.store != nil {
		if st, stErr := s.store.LoadSKUWatcherState(ctx); stErr == nil && st != nil {
			if len(st.SKUsJSON) > 0 {
				_ = json.Unmarshal(st.SKUsJSON, &storeSKUs)
			}
			if len(changes) == 0 && len(st.ChangesJSON) > 2 {
				var rawCh []map[string]any
				if json.Unmarshal(st.ChangesJSON, &rawCh) == nil && len(rawCh) > 0 {
					changes = rawCh
				}
			}
		}
	}

	matchChangeKind := func(kind string) bool {
		switch changeType {
		case "ALL":
			return true
		case "ADDED":
			return kind == "ADDED" || kind == "CONFIG_ADDED"
		case "REMOVED":
			return kind == "REMOVED" || kind == "CONFIG_REMOVED"
		case "REPLACED":
			return kind == "REPLACED"
		default:
			return strings.EqualFold(kind, changeType)
		}
	}

	normalizedChanges := make([]map[string]any, 0, len(changes))
	for _, rawCh := range changes {
		ch := make(map[string]any, len(rawCh)+6)
		for k, v := range rawCh {
			ch[k] = v
		}
		kind := strings.ToUpper(strings.TrimSpace(asString(ch["type"])))
		if kind == "" {
			kind = strings.ToUpper(strings.TrimSpace(asString(ch["changeType"])))
		}
		if !matchChangeKind(kind) {
			continue
		}
		ls := strings.ToLower(strings.TrimSpace(asString(ch["listingState"])))
		if ls == "" {
			ls = "listed"
		}
		if listingState != "all" && ls != listingState {
			continue
		}
		ch["listingState"] = ls
		ch["saleStarted"] = asBool(ch["saleStarted"])
		ch["replacedFrom"] = strings.TrimSpace(asString(ch["replacedFrom"]))
		if asString(ch["type"]) == "" && kind != "" {
			ch["type"] = strings.ToLower(kind)
		}
		if asString(ch["changeType"]) == "" && kind != "" {
			ch["changeType"] = kind
		}
		normalizedChanges = append(normalizedChanges, ch)
		if len(normalizedChanges) >= limit {
			break
		}
	}

	listedCount := asInt(status["listedSkus"], -1)
	announcedCount := asInt(status["announcedSkus"], -1)
	presaleCount := asInt(status["presaleSkus"], -1)
	totalSkus := asInt(status["totalSkus"], -1)

	if trackedSkus == nil {
		if rawTS, ok := status["trackedSkus"].([]map[string]any); ok {
			trackedSkus = rawTS
		} else if rawAny, ok := status["trackedSkus"].([]any); ok {
			for _, it := range rawAny {
				if m, ok := it.(map[string]any); ok {
					trackedSkus = append(trackedSkus, m)
				}
			}
		}
	}

	if len(trackedSkus) == 0 && len(storeSKUs) > 0 {
		keys := make([]string, 0, len(storeSKUs))
		for k := range storeSKUs {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		lCnt, aCnt, pCnt := 0, 0, 0
		for _, k := range keys {
			entry := storeSKUs[k]
			ls := strings.ToLower(strings.TrimSpace(asString(entry["listingState"])))
			if ls == "" {
				ls = "listed"
			}
			switch ls {
			case "announced":
				aCnt++
			case "presale":
				pCnt++
			default:
				ls = "listed"
				lCnt++
			}
			cp := make(map[string]any, len(entry)+6)
			for ek, ev := range entry {
				cp[ek] = ev
			}
			pc := strings.TrimSpace(asString(cp["planCode"]))
			if pc == "" {
				pc = k
			}
			cp["key"] = k
			cp["skuKey"] = k
			cp["planCode"] = pc
			cp["listingState"] = ls
			cp["saleStarted"] = asBool(cp["saleStarted"])
			cp["replacedFrom"] = strings.TrimSpace(asString(cp["replacedFrom"]))
			trackedSkus = append(trackedSkus, cp)
		}
		if totalSkus < 0 {
			totalSkus = len(storeSKUs)
		}
		if listedCount < 0 {
			listedCount = lCnt
		}
		if announcedCount < 0 {
			announcedCount = aCnt
		}
		if presaleCount < 0 {
			presaleCount = pCnt
		}
	}

	filteredTracked := make([]map[string]any, 0, len(trackedSkus))
	lFromTracked, aFromTracked, pFromTracked := 0, 0, 0
	for _, rawT := range trackedSkus {
		tm := make(map[string]any, len(rawT)+4)
		for k, v := range rawT {
			tm[k] = v
		}
		ls := strings.ToLower(strings.TrimSpace(asString(tm["listingState"])))
		if ls == "" {
			ls = "listed"
		}
		switch ls {
		case "announced":
			aFromTracked++
		case "presale":
			pFromTracked++
		default:
			ls = "listed"
			lFromTracked++
		}
		if listingState != "all" && ls != listingState {
			continue
		}
		tm["listingState"] = ls
		tm["saleStarted"] = asBool(tm["saleStarted"])
		tm["replacedFrom"] = strings.TrimSpace(asString(tm["replacedFrom"]))
		filteredTracked = append(filteredTracked, tm)
	}

	if listedCount < 0 {
		listedCount = lFromTracked
	}
	if announcedCount < 0 {
		announcedCount = aFromTracked
	}
	if presaleCount < 0 {
		presaleCount = pFromTracked
	}
	if totalSkus < 0 {
		totalSkus = listedCount + announcedCount + presaleCount
	}

	status["totalSkus"] = totalSkus
	status["listedSkus"] = listedCount
	status["announcedSkus"] = announcedCount
	status["presaleSkus"] = presaleCount
	status["listedCount"] = listedCount
	status["announcedCount"] = announcedCount
	if _, hasPC := status["presaleCount"]; !hasPC {
		status["presaleCount"] = presaleCount
	}
	status["recentChanges"] = normalizedChanges
	status["trackedSkus"] = filteredTracked

	summaryCounts := map[string]any{
		"totalSkus":     totalSkus,
		"listedSkus":    listedCount,
		"announcedSkus": announcedCount,
		"presaleSkus":   presaleCount,
	}

	return map[string]any{
		"status":        status,
		"summary":       summaryCounts,
		"totalSkus":     totalSkus,
		"listedSkus":    listedCount,
		"announcedSkus": announcedCount,
		"presaleSkus":   presaleCount,
		"changes":       normalizedChanges,
		"recentChanges": normalizedChanges,
		"trackedSkus":   filteredTracked,
	}, fmt.Sprintf("Retrieved %d SKU watcher change event(s).", len(normalizedChanges)), nil
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
		source := strings.TrimSpace(q.Source)
		if source == "" {
			source = "manual"
		}
		pm := strings.TrimSpace(q.PricingMode)
		if pm == "" {
			pm = "default"
		}
		qMap := map[string]any{
			"id":          q.ID,
			"planCode":    q.PlanCode,
			"datacenters": q.Datacenters,
			"options":     q.Options,
			"pricingMode": pm,
			"status":      q.Status,
			"retryCount":  q.RetryCount,
			"quantity":    q.Quantity,
			"purchased":   q.Purchased,
			"autoPay":     false, // Never expose or allow autoPay via MCP
			"source":      source,
			"profile":     profilePtr,
			"createdAt":   q.CreatedAt,
		}
		if strings.TrimSpace(q.Coupon) != "" {
			qMap["coupon"] = strings.TrimSpace(q.Coupon)
		}
		if q.MaxMonthlyPrice > 0 {
			qMap["maxMonthlyPrice"] = q.MaxMonthlyPrice
		}
		if ref := strings.TrimSpace(q.SourceRef); ref != "" {
			qMap["sourceRef"] = ref
		}
		safeQueue = append(safeQueue, qMap)
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

func (s *Service) toolCatalogPromotions(ctx context.Context, args map[string]any) (map[string]any, string, error) {
	planFilter := strings.TrimSpace(asString(args["plan_code"]))
	rangeFilter := strings.TrimSpace(asString(args["range"]))
	promoType := strings.ToLower(strings.TrimSpace(asString(args["promo_type"])))
	if promoType == "" {
		promoType = "all"
	}
	onlyInStock := asBool(args["only_in_stock"])
	subsidiary := strings.ToUpper(strings.TrimSpace(asString(args["subsidiary"])))

	opts := catalog.PromotionsQueryOptions{
		Zone:        subsidiary,
		PlanCode:    planFilter,
		Range:       rangeFilter,
		PromoType:   promoType,
		OnlyInStock: onlyInStock,
	}

	var resp *catalog.CatalogPromotionsResponse
	var err error
	if promoReader, ok := s.deps.Catalog.(CatalogPromotionsReader); ok && promoReader != nil {
		resp, err = promoReader.GetPromotions(ctx, opts)
	} else {
		// Standalone fallback: reuse lazily-initialized singleton catalog service.
		resp, err = s.getFallbackCatalog().GetPromotions(ctx, opts)
	}
	if err != nil {
		return nil, "", fmt.Errorf("failed to inspect OVH catalog promotions: %w", err)
	}
	if resp == nil {
		resp = &catalog.CatalogPromotionsResponse{
			Currency:         "EUR",
			AutoAppliedCodes: []string{},
			Campaigns:        []catalog.CatalogPromoCampaign{},
			Plans:            []catalog.CatalogPromoPlan{},
		}
	}

	// Compact per-plan projection so agent contexts stay crisp and actionable.
	plansCompact := make([]map[string]any, 0, len(resp.Plans))
	for _, p := range resp.Plans {
		modes := make([]map[string]any, 0, len(p.PricingModes))
		for _, m := range p.PricingModes {
			entry := map[string]any{
				"mode":                   m.Mode,
				"duration":               m.Duration,
				"intervalMonths":         m.IntervalMonths,
				"commitmentMonths":       m.CommitmentMonths,
				"monthlyEquivalentPrice": m.MonthlyEquivalentPrice,
				"originalMonthlyPrice":   m.OriginalMonthlyPrice,
				"periodPrice":            m.PeriodPrice,
				"originalPeriodPrice":    m.OriginalPeriodPrice,
				"setupFee":               m.SetupFee,
				"originalSetupFee":       m.OriginalSetupFee,
				"freeSetup":              m.FreeSetup,
				"hasPromo":               m.HasPromo,
			}
			if m.PromoCode != "" {
				entry["promoCode"] = m.PromoCode
			}
			if m.PromoBadge != "" {
				entry["promoBadge"] = m.PromoBadge
			}
			if m.RenewPromo != nil {
				entry["renewPromo"] = m.RenewPromo
			}
			if m.SetupPromo != nil {
				entry["setupPromo"] = m.SetupPromo
			}
			modes = append(modes, entry)
		}
		plansCompact = append(plansCompact, map[string]any{
			"planCode":             p.PlanCode,
			"name":                 p.Name,
			"range":                p.Range,
			"cpu":                  p.CPU,
			"listingState":         p.ListingState,
			"currency":             p.Currency,
			"startingPrice":        p.StartingPrice,
			"originalMonthlyPrice": p.OriginalMonthlyPrice,
			"setupFee":             p.SetupFee,
			"originalSetupFee":     p.OriginalSetupFee,
			"monthlySavings":       p.MonthlySavings,
			"setupSavings":         p.SetupSavings,
			"bestPromoMode":        p.BestPromoMode,
			"hasRecurringPromo":    p.HasRecurringPromo,
			"hasSetupPromo":        p.HasSetupPromo,
			"autoApplied":          p.AutoApplied,
			"promoCodes":           p.PromoCodes,
			"promoBadge":           p.PromoBadge,
			"supportedPromoModes":  p.SupportedPromoModes,
			"missedPromoModes":     p.MissedPromoModes,
			"addonsDiscounted":     p.AddonsDiscounted,
			"inStockDatacenters":   p.InStockDatacenters,
			"inStockCount":         p.InStockCount,
			"pricingModes":         modes,
		})
	}

	guidance := []string{
		"Campaigns with autoApplied=true (context='standard') are automatically applied by OVH whenever a matching pricingMode is selected — NEVER pass those campaign IDs into the `coupon` parameter of `order_quote` or `queue_add` (OVH rejects them with INVALID_COUPON).",
		"Check `supportedPromoModes` vs `missedPromoModes` before recommending a commitment term: some discounts apply to `default` and `upfront12` but NOT `upfront24`.",
		"When `addonsDiscounted` is false, base server pricing is discounted while RAM/storage upgrade addons are added at list price.",
		"Use `order_quote(plan_code, datacenter, options, pricing_mode, coupon)` to verify exact tax-inclusive/exclusive live cart pricing or test private voucher codes.",
	}

	return map[string]any{
		"endpoint":           resp.Endpoint,
		"subsidiary":         resp.Zone,
		"currency":           resp.Currency,
		"updatedAt":          resp.UpdatedAt,
		"totalCampaigns":     resp.TotalCampaigns,
		"totalPromotedPlans": resp.TotalPromotedPlans,
		"inStockPlans":       resp.InStockPlans,
		"autoAppliedCodes":   resp.AutoAppliedCodes,
		"campaigns":          resp.Campaigns,
		"plans":              plansCompact,
		"agentGuidance":      guidance,
	}, fmt.Sprintf("Inspected %d active OVH promotion campaign(s) across %d promoted server plan(s) (%d in stock) for %s.", resp.TotalCampaigns, resp.TotalPromotedPlans, resp.InStockPlans, resp.Zone), nil
}

// ==================== L2 Tool Handlers (Hardcoded auto_pay=false & quantity=1) ====================

func validateMCPPricingMode(raw string) (string, string, int, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed != "" {
		switch strings.ToLower(trimmed) {
		case "default", "upfront12", "upfront24", "degressivity12", "degressivity24":
		default:
			return "", "", 0, fmt.Errorf("invalid `pricing_mode` %q: allowed values are 'default', 'upfront12', 'upfront24', 'degressivity12', 'degressivity24'", trimmed)
		}
	}
	mode, dur, interval := catalog.NormalizePricingModeAndDuration(trimmed, "")
	return mode, dur, interval, nil
}

func parseOptionalPositiveFloat(args map[string]any, key string) (float64, bool, error) {
	raw, exists := args[key]
	if !exists || raw == nil {
		return 0, false, nil
	}
	var val float64
	switch v := raw.(type) {
	case float64:
		val = v
	case float32:
		val = float64(v)
	case int:
		val = float64(v)
	case int64:
		val = float64(v)
	case json.Number:
		f, err := v.Float64()
		if err != nil {
			return 0, true, fmt.Errorf("`%s` must be a valid positive number", key)
		}
		val = f
	case string:
		trimmed := strings.TrimSpace(v)
		if trimmed == "" {
			return 0, false, nil
		}
		f, err := strconv.ParseFloat(trimmed, 64)
		if err != nil {
			return 0, true, fmt.Errorf("`%s` must be a valid positive number", key)
		}
		val = f
	default:
		return 0, true, fmt.Errorf("`%s` must be a positive number", key)
	}
	if math.IsNaN(val) || math.IsInf(val, 0) || val <= 0 {
		return 0, true, fmt.Errorf("`%s` must be greater than 0 (omit the parameter if no limit is desired)", key)
	}
	return val, true, nil
}

func numericFieldFromMap(m map[string]any, key string) (float64, bool) {
	if m == nil {
		return 0, false
	}
	raw, ok := m[key]
	if !ok || raw == nil {
		return 0, false
	}
	switch v := raw.(type) {
	case float64:
		return v, !math.IsNaN(v) && !math.IsInf(v, 0)
	case float32:
		return float64(v), true
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	case json.Number:
		f, err := v.Float64()
		return f, err == nil
	case map[string]any:
		return numericFieldFromMap(v, "value")
	default:
		return 0, false
	}
}

func extractQuotedPrices(priceMap map[string]any, intervalMonths int) (monthlyExVAT float64, setupFeeExVAT float64, ok bool) {
	if len(priceMap) == 0 {
		return 0, 0, false
	}
	if intervalMonths < 1 {
		intervalMonths = 1
	}
	if iv, hasIV := numericFieldFromMap(priceMap, "intervalMonths"); hasIV && iv >= 1 {
		intervalMonths = int(iv)
	}
	if sf, hasSF := numericFieldFromMap(priceMap, "setupFeeWithoutTax"); hasSF && sf >= 0 {
		setupFeeExVAT = math.Round(sf*100) / 100
	}
	if m, hasM := numericFieldFromMap(priceMap, "monthlyEquivalentWithoutTax"); hasM && m > 0 {
		return math.Round(m*100) / 100, setupFeeExVAT, true
	}
	if rec, hasRec := numericFieldFromMap(priceMap, "recurringWithoutTax"); hasRec && rec > 0 {
		periodEx := rec
		if pricesObj, _ := priceMap["prices"].(map[string]any); pricesObj != nil {
			if totalEx, hasTot := numericFieldFromMap(pricesObj, "withoutTax"); hasTot && totalEx > 0 && totalEx+0.02 < rec+setupFeeExVAT {
				if net := totalEx - setupFeeExVAT; net > 0 {
					periodEx = net
				}
			}
		}
		return math.Round((periodEx/float64(intervalMonths))*100) / 100, setupFeeExVAT, true
	}
	if pricesObj, _ := priceMap["prices"].(map[string]any); pricesObj != nil {
		if wt, hasWT := numericFieldFromMap(pricesObj, "withoutTax"); hasWT && wt > 0 {
			net := wt
			if setupFeeExVAT > 0 {
				if wt <= setupFeeExVAT {
					return 0, setupFeeExVAT, false
				}
				net = wt - setupFeeExVAT
			}
			return math.Round((net/float64(intervalMonths))*100) / 100, setupFeeExVAT, true
		}
	}
	if wt, hasWT := numericFieldFromMap(priceMap, "totalWithoutTax"); hasWT && wt > 0 {
		net := wt
		if setupFeeExVAT > 0 {
			if wt <= setupFeeExVAT {
				return 0, setupFeeExVAT, false
			}
			net = wt - setupFeeExVAT
		}
		return math.Round((net/float64(intervalMonths))*100) / 100, setupFeeExVAT, true
	}
	return 0, setupFeeExVAT, false
}

func (s *Service) stripAutoAppliedCouponForMCP(ctx context.Context, planCode, pricingMode, rawCoupon string) (cleanCoupon string, strippedCode string) {
	trimmed := strings.TrimSpace(rawCoupon)
	targetPlan := strings.TrimSpace(planCode)
	if trimmed == "" || targetPlan == "" {
		return trimmed, ""
	}
	targetMode := ""
	if strings.TrimSpace(pricingMode) != "" {
		targetMode, _, _ = catalog.NormalizePricingModeAndDuration(pricingMode, "")
	}
	promoReader, ok := s.deps.Catalog.(CatalogPromotionsReader)
	if !ok || promoReader == nil {
		return trimmed, ""
	}
	resp, err := promoReader.GetPromotions(ctx, catalog.PromotionsQueryOptions{
		PlanCode: targetPlan,
	})
	if err != nil || resp == nil {
		return trimmed, ""
	}

	containsFold := func(list []string, needle string) bool {
		for _, item := range list {
			if strings.EqualFold(strings.TrimSpace(item), needle) {
				return true
			}
		}
		return false
	}

	// 1. Check exact plan + pricingMode match in resp.Plans.
	for _, p := range resp.Plans {
		if !strings.EqualFold(strings.TrimSpace(p.PlanCode), targetPlan) {
			continue
		}
		if len(p.PricingModes) > 0 {
			for _, m := range p.PricingModes {
				if targetMode != "" && !strings.EqualFold(strings.TrimSpace(m.Mode), targetMode) {
					continue
				}
				for _, pd := range []*catalog.PromoDetail{m.RenewPromo, m.SetupPromo} {
					if pd != nil && pd.AutoApplied && strings.EqualFold(strings.TrimSpace(pd.Code), trimmed) {
						if s.log != nil {
							s.log.Warn(logger.SourceMCP, "Stripped auto-applied OVH catalog campaign code from MCP coupon argument (applied automatically by OVH)",
								"planCode", targetPlan, "pricingMode", targetMode, "coupon", trimmed, "campaign", pd.Code)
						}
						return "", strings.TrimSpace(pd.Code)
					}
				}
				if m.RenewPromo == nil && m.SetupPromo == nil && m.HasPromo &&
					strings.EqualFold(strings.TrimSpace(m.PromoCode), trimmed) &&
					(m.AutoApplied || m.PromoContext == "" || strings.EqualFold(m.PromoContext, "standard")) {
					if s.log != nil {
						s.log.Warn(logger.SourceMCP, "Stripped auto-applied OVH catalog campaign code from MCP coupon argument (applied automatically by OVH)",
							"planCode", targetPlan, "pricingMode", targetMode, "coupon", trimmed, "campaign", m.PromoCode)
					}
					return "", strings.TrimSpace(m.PromoCode)
				}
			}
		} else if p.AutoApplied && containsFold(p.PromoCodes, trimmed) {
			if targetMode == "" || len(p.SupportedPromoModes) == 0 || containsFold(p.SupportedPromoModes, targetMode) {
				for _, code := range p.PromoCodes {
					if strings.EqualFold(strings.TrimSpace(code), trimmed) {
						if s.log != nil {
							s.log.Warn(logger.SourceMCP, "Stripped auto-applied OVH catalog campaign code from MCP coupon argument (applied automatically by OVH)",
								"planCode", targetPlan, "pricingMode", targetMode, "coupon", trimmed, "campaign", code)
						}
						return "", strings.TrimSpace(code)
					}
				}
			}
		}
	}

	// 2. Check campaigns explicitly listing this planCode and pricingMode.
	for _, c := range resp.Campaigns {
		if !c.AutoApplied || !strings.EqualFold(strings.TrimSpace(c.PromoCode), trimmed) {
			continue
		}
		if !containsFold(c.PlanCodes, targetPlan) {
			continue
		}
		if targetMode != "" && len(c.SupportedModes) > 0 && !containsFold(c.SupportedModes, targetMode) {
			continue
		}
		if s.log != nil {
			s.log.Warn(logger.SourceMCP, "Stripped auto-applied OVH catalog campaign code from MCP coupon argument (applied automatically by OVH)",
				"planCode", targetPlan, "pricingMode", targetMode, "coupon", trimmed, "campaign", c.PromoCode)
		}
		return "", strings.TrimSpace(c.PromoCode)
	}

	return trimmed, ""
}

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
	pricingMode, duration, intervalMonths, modeErr := validateMCPPricingMode(asString(args["pricing_mode"]))
	if modeErr != nil {
		return nil, "", modeErr
	}
	coupon, strippedCode := s.stripAutoAppliedCouponForMCP(ctx, planCode, pricingMode, asString(args["coupon"]))

	targets, err := s.resolveTargetAccounts(ctx, cfg, asString(args["profile"]))
	if err != nil {
		return nil, "", err
	}
	if len(targets) == 0 {
		return nil, "", errors.New("no whitelisted OVH account is available to request a price quote")
	}
	target := targets[0]

	var quoteRes *OrderQuoteResult
	if modeQuoter, ok := s.deps.Procurement.(ProcurementModeQuoter); ok && modeQuoter != nil {
		quoteRes, err = modeQuoter.QuoteOrderWithMode(ctx, target.Account.ID, planCode, datacenter, options, pricingMode, duration, coupon)
	} else {
		quoteRes, err = s.deps.Procurement.QuoteOrder(ctx, target.Account.ID, planCode, datacenter, options)
	}
	if err != nil {
		return nil, "", fmt.Errorf("failed to retrieve OVH cart quote for %s: %w", planCode, err)
	}
	priceInfo := map[string]any{}
	if quoteRes != nil && quoteRes.Price != nil {
		priceInfo = quoteRes.Price
	}
	quotedMonthly, quotedSetup, hasQuotedPrice := extractQuotedPrices(priceInfo, intervalMonths)

	q := s.quotes.createWithQuotePrices(
		planCode, datacenter, options, pricingMode, duration, coupon,
		target.Account.ID, target.Alias, priceInfo,
		quotedMonthly, quotedSetup, hasQuotedPrice,
	)
	modelName := planCode
	if s.deps.Catalog != nil {
		if resolved, rErr := s.deps.Catalog.ResolveModelName(ctx, planCode); rErr == nil && strings.TrimSpace(resolved) != "" {
			modelName = resolved
		}
	}

	resMap := map[string]any{
		"quoteId":          q.QuoteID,
		"expiresInSeconds": QuoteTTLSeconds,
		"planCode":         planCode,
		"modelName":        modelName,
		"datacenter":       strings.ToUpper(datacenter),
		"options":          options,
		"pricingMode":      pricingMode,
		"duration":         duration,
		"profile":          target.Alias,
		"quantity":         1,
		"autoPay":          false,
		"price":            priceInfo,
		"nextStep":         "Confirm with the user, then call `order_create_unpaid` with this `quote_id` within 5 minutes to generate an unpaid order.",
	}
	if coupon != "" {
		resMap["coupon"] = coupon
	}
	if strippedCode != "" {
		resMap["couponStripped"] = true
		resMap["autoAppliedPromoCode"] = strippedCode
	}
	return resMap, fmt.Sprintf("Generated 5-min price quote %s for %s @ %s (%s, profile %s).", q.QuoteID, planCode, strings.ToUpper(datacenter), pricingMode, target.Alias), nil
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
	if !quote.HasQuotedPrice {
		return nil, "", errors.New("quote did not contain a verifiable price; call `order_quote` again before creating an unpaid order")
	}

	// Re-verify account is still permitted by the whitelist.
	if _, err := s.resolveTargetAccounts(ctx, cfg, quote.ProfileAlias); err != nil {
		return nil, "", err
	}

	// SECURITY REDLINE: auto_pay and waive_retractation_period are unconditionally false, quantity is unconditionally 1,
	// and live checkout price is strictly re-verified against the quoted ex-VAT monthly price and setup fee.
	req := UnpaidOrderRequest{
		TaskID:                  "mcp-order-" + randomHex(4),
		AccountID:               quote.AccountID,
		PlanCode:                quote.PlanCode,
		Datacenter:              quote.Datacenter,
		Options:                 append([]string(nil), quote.Options...),
		PricingMode:             quote.PricingMode,
		Duration:                quote.Duration,
		Coupon:                  quote.Coupon,
		MaxMonthlyPrice:         quote.QuotedMonthlyExVAT,
		MaxSetupFee:             quote.QuotedSetupFeeExVAT,
		EnforceQuoteCap:         true,
		Quantity:                1,
		AutoPay:                 false,
		WaiveRetractationPeriod: false,
	}

	mu := s.checkoutMutexForAccount(quote.AccountID)
	orderRes, err := func() (*UnpaidOrderResult, error) {
		mu.Lock()
		defer mu.Unlock()
		return s.deps.Procurement.CreateUnpaidOrder(ctx, req)
	}()
	if err != nil {
		if errors.Is(err, procurement.ErrPriceChangedSinceQuote) || strings.Contains(err.Error(), "PRICE_CHANGED_SINCE_QUOTE") {
			return nil, "", fmt.Errorf("PRICE_CHANGED_SINCE_QUOTE: live price for %s @ %s increased above quoted price (monthly<=%.2f, setup<=%.2f): %w. Call `order_quote` again to confirm the new price with the user", quote.PlanCode, strings.ToUpper(quote.Datacenter), quote.QuotedMonthlyExVAT, quote.QuotedSetupFeeExVAT, err)
		}
		if errors.Is(err, procurement.ErrPriceCheckFailed) || strings.Contains(err.Error(), "PRICE_CHECK_FAILED") {
			return nil, "", fmt.Errorf("PRICE_CHECK_FAILED: unable to verify live cart summary before checkout for %s @ %s: %w. Please retry with a fresh `order_quote`", quote.PlanCode, strings.ToUpper(quote.Datacenter), err)
		}
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

	pm := strings.TrimSpace(quote.PricingMode)
	if pm == "" {
		pm = "default"
	}
	resMap := map[string]any{
		"status":               "unpaid_order_created",
		"order":                ordAlias,
		"planCode":             quote.PlanCode,
		"datacenter":           strings.ToUpper(quote.Datacenter),
		"options":              quote.Options,
		"pricingMode":          pm,
		"duration":             quote.Duration,
		"profile":              quote.ProfileAlias,
		"quantity":             1,
		"autoPay":              false,
		"price":                price,
		"remainingHourlyQuota": remAfter,
		"paymentInstructions":  "Unpaid order locked in! For security, the payment link is NOT exposed to the AI. It has been dispatched to your notification channel and is available in the OVH Nexus Web Console -> [Orders & Delivery].",
	}
	if quote.Coupon != "" {
		resMap["coupon"] = quote.Coupon
	}
	return resMap, fmt.Sprintf("Created UNPAID order %s for %s @ %s (%s, auto_pay=false, profile %s).", ordAlias, quote.PlanCode, strings.ToUpper(quote.Datacenter), pm, quote.ProfileAlias), nil
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
	pricingMode, duration, _, modeErr := validateMCPPricingMode(asString(args["pricing_mode"]))
	if modeErr != nil {
		return nil, "", modeErr
	}
	maxMonthlyPrice, _, mErr := parseOptionalPositiveFloat(args, "max_monthly_price")
	if mErr != nil {
		return nil, "", mErr
	}
	maxSetupFee, _, sErr := parseOptionalPositiveFloat(args, "max_setup_fee")
	if sErr != nil {
		return nil, "", sErr
	}
	coupon, strippedCode := s.stripAutoAppliedCouponForMCP(ctx, planCode, pricingMode, asString(args["coupon"]))

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
		PricingMode:             pricingMode,
		Duration:                duration,
		Coupon:                  coupon,
		MaxMonthlyPrice:         maxMonthlyPrice,
		MaxSetupFee:             maxSetupFee,
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

	resMap := map[string]any{
		"taskId":      created.ID,
		"planCode":    planCode,
		"datacenters": dcs,
		"options":     options,
		"pricingMode": pricingMode,
		"duration":    duration,
		"profile":     target.Alias,
		"quantity":    1,
		"autoPay":     false,
		"status":      "running",
	}
	if coupon != "" {
		resMap["coupon"] = coupon
	}
	if strippedCode != "" {
		resMap["couponStripped"] = true
		resMap["autoAppliedPromoCode"] = strippedCode
	}
	if maxMonthlyPrice > 0 {
		resMap["maxMonthlyPrice"] = maxMonthlyPrice
	}
	if maxSetupFee > 0 {
		resMap["maxSetupFee"] = maxSetupFee
	}
	return resMap, fmt.Sprintf("Added Auto-Order Queue task %s for %s @ %s (%s, auto_pay=false, quantity=1).", created.ID, planCode, strings.ToUpper(strings.Join(dcs, ",")), pricingMode), nil
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
