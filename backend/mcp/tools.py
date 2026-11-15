"""
OVH Nexus MCP Tool Registry & Execution Engine
==============================================
Implements 20 tools across 3 configurable permission tiers (inspired by ovh/ovhcloud-cli):
  - L1 (read):        11 read-only inspection tools
  - L2 (procurement):  5 monitoring & unpaid ordering tools (auto_pay=False & quantity=1 hardcoded)
  - L3 (operations):   4 reversible Day-2 infrastructure tools (reboot, IPMI reset, IP firewall)
  - L4 (destructive):  0 tools (reinstall, terminate, auto-pay, raw IPMI URL, secret edits permanently excluded)
"""

from __future__ import annotations

from datetime import datetime, timezone
import threading
from typing import Any, Dict, List, Optional, Tuple
from urllib.parse import quote
import uuid

from catalog_service import (
    catalog_service,
    format_cpu,
    format_memory,
    format_storage,
    parse_memory_code,
    parse_storage_code,
)
from .audit import (
    check_order_rate_limit,
    consume_order_quote,
    create_order_quote,
    record_order_execution,
)
from .config import get_whitelisted_account_ids, load_mcp_config
from .privacy_shield import format_ip_hint, privacy_shield


def _get_app_module():
    import sys
    main_mod = sys.modules.get("__main__")
    if main_mod and hasattr(main_mod, "get_ovh_client"):
        return main_mod
    import app as A
    return A


def _days_until(date_str: Any) -> Optional[int]:
    if not date_str:
        return None
    try:
        raw = str(date_str).strip()
        if "T" in raw:
            dt = datetime.fromisoformat(raw.replace("Z", "+00:00"))
        else:
            dt = datetime.strptime(raw[:10], "%Y-%m-%d").replace(tzinfo=timezone.utc)
        if dt.tzinfo is None:
            dt = dt.replace(tzinfo=timezone.utc)
        now = datetime.now(timezone.utc)
        return int((dt - now).total_seconds() // 86400)
    except Exception:
        return None


def _compute_renew_mode(renew_obj: Any) -> str:
    if not isinstance(renew_obj, dict):
        return "manual"
    if renew_obj.get("deleteAtExpiration"):
        return "deleteAtExpiration"
    if renew_obj.get("automatic"):
        return "auto"
    return "manual"


def _resolve_target_accounts(profile_arg: Optional[str] = None) -> List[Tuple[str, str, Dict[str, Any]]]:
    """
    Return list of (account_id, account_alias, account_dict) permitted by the MCP whitelist.
    If profile_arg is specified ('acct-01' or custom label), filters to that single profile
    and raises ValueError if not found or not whitelisted.
    """
    A = _get_app_module()
    accounts_dict = getattr(A, "accounts", {}) or {}
    default_id = getattr(A, "default_account_id", None)
    legacy_cfg = getattr(A, "config", {}) or {}

    allowed_ids = get_whitelisted_account_ids(accounts_dict, default_id, legacy_cfg)
    if not allowed_ids:
        return []

    profiles: List[Tuple[str, str, Dict[str, Any]]] = []
    for aid in allowed_ids:
        if aid == "default" and aid not in accounts_dict:
            acc_obj = {
                "id": "default",
                "alias": "Default",
                "endpoint": legacy_cfg.get("endpoint", "ovh-eu"),
                "zone": legacy_cfg.get("zone", "IE"),
            }
        else:
            acc_obj = accounts_dict.get(aid) or {}
        redacted = privacy_shield.redact_account(aid, acc_obj, is_default=(aid == default_id or len(allowed_ids) == 1))
        profiles.append((aid, redacted["profile"], redacted))

    if not profile_arg:
        return profiles

    wanted = str(profile_arg).strip()
    resolved_id = privacy_shield.resolve("account", wanted)
    for aid, palias, pmeta in profiles:
        if (
            aid == resolved_id
            or palias.lower() == wanted.lower()
            or str(pmeta.get("label") or "").lower() == wanted.lower()
        ):
            return [(aid, palias, pmeta)]

    raise ValueError(f"Profile '{wanted}' is not found or not permitted by the MCP account whitelist.")


def _find_server_owner_account(category: str, alias_or_label: str) -> Tuple[str, str, str, Any]:
    """
    Resolve a baremetal ('bm-xx') or vps ('vps-xx') alias to:
    (real_service_name, canonical_alias, account_id, ovh_client)
    while enforcing the MCP account whitelist.
    """
    A = _get_app_module()
    real_name = privacy_shield.resolve(category, alias_or_label)
    if not real_name:
        raise ValueError(
            f"Unknown {category} alias '{alias_or_label}'. Call {category}_list first to obtain valid aliases (e.g. '{'bm-01' if category == 'baremetal' else 'vps-01'}')."
        )

    canonical_alias = privacy_shield.register(category, real_name)
    meta = privacy_shield.get_meta(category, canonical_alias)
    recorded_account_id = meta.get("accountId")

    allowed_profiles = _resolve_target_accounts(None)
    allowed_ids = [aid for aid, _, _ in allowed_profiles]
    if not allowed_ids:
        raise ValueError("No whitelisted OVH account is available for MCP.")

    if recorded_account_id and recorded_account_id in allowed_ids:
        client = A.get_ovh_client(None if recorded_account_id == "default" else recorded_account_id)
        if client:
            return real_name, canonical_alias, recorded_account_id, client

    # Search across whitelisted accounts
    api_prefix = "/dedicated/server" if category == "baremetal" else "/vps"
    for aid, palias, _ in allowed_profiles:
        client = A.get_ovh_client(None if aid == "default" else aid)
        if not client:
            continue
        try:
            names = client.get(api_prefix) or []
            if real_name in names:
                privacy_shield.register(category, real_name, meta={"accountId": aid, "accountAlias": palias})
                return real_name, canonical_alias, aid, client
        except Exception:
            continue

    raise ValueError(f"Resource '{canonical_alias}' is not accessible under any MCP-whitelisted OVH account.")


# ==================== Tool Definitions (JSON Schema) ====================

TOOL_DEFINITIONS: List[Dict[str, Any]] = [
    # --- L1: Read-Only Insight (scope: read) ---
    {
        "name": "profile_list",
        "scope": "read",
        "description": "List OVHcloud account profiles accessible to MCP (similar to `ovhcloud config profile list`). Returns obfuscated profile aliases (e.g. acct-01), region, and subsidiary.",
        "inputSchema": {
            "type": "object",
            "properties": {},
            "additionalProperties": False,
        },
    },
    {
        "name": "catalog_availability",
        "scope": "read",
        "description": "Query real-time global OVHcloud bare-metal server stock matrix and commercial specs (KS-A, RISE-1, SCALE, etc.) without requiring an API key.",
        "inputSchema": {
            "type": "object",
            "properties": {
                "plan_code": {"type": "string", "description": "Optional exact or partial planCode / model name filter (e.g. '24ska01', 'KS-A', 'rise')."},
                "datacenter": {"type": "string", "description": "Optional datacenter filter (e.g. 'gra', 'rbx', 'sbg', 'bhs', 'fra', 'vin')."},
                "only_available": {"type": "boolean", "description": "If true, return only configurations currently in stock."},
                "limit": {"type": "integer", "description": "Max plans to return (1-50, default 20)."},
            },
            "additionalProperties": False,
        },
    },
    {
        "name": "catalog_sku_changes",
        "scope": "read",
        "description": "List recent public OVHcloud shelf SKU additions and removals detected by the SKU Watcher.",
        "inputSchema": {
            "type": "object",
            "properties": {
                "limit": {"type": "integer", "description": "Maximum change events to return (1-50, default 20)."},
                "change_type": {"type": "string", "enum": ["ALL", "ADDED", "REMOVED"], "description": "Filter by change type."},
            },
            "additionalProperties": False,
        },
    },
    {
        "name": "baremetal_list",
        "scope": "read",
        "description": "List purchased OVHcloud dedicated / bare-metal servers (similar to `ovhcloud baremetal list`). Sensitive hostnames and full IPs are replaced with persistent aliases (e.g. bm-01, ip-01).",
        "inputSchema": {
            "type": "object",
            "properties": {
                "profile": {"type": "string", "description": "Optional account profile alias (e.g. 'acct-01'). Omit to list across all whitelisted profiles."},
            },
            "additionalProperties": False,
        },
    },
    {
        "name": "baremetal_get",
        "scope": "read",
        "description": "Get detailed hardware specs, boot mode, and ongoing tasks for a specific bare-metal server alias (similar to `ovhcloud baremetal get <server>`).",
        "inputSchema": {
            "type": "object",
            "properties": {
                "server": {"type": "string", "description": "Bare-metal server alias (e.g. 'bm-01') or custom label."},
            },
            "required": ["server"],
            "additionalProperties": False,
        },
    },
    {
        "name": "baremetal_firewall_get",
        "scope": "read",
        "description": "Inspect OVHcloud IP Edge Firewall status, Anti-DDoS mitigation mode, and active firewall rules for a bare-metal server (`bm-xx`).",
        "inputSchema": {
            "type": "object",
            "properties": {
                "server": {"type": "string", "description": "Bare-metal server alias (e.g. 'bm-01')."},
            },
            "required": ["server"],
            "additionalProperties": False,
        },
    },
    {
        "name": "vps_list",
        "scope": "read",
        "description": "List purchased OVHcloud VPS instances (similar to `ovhcloud vps list`) with obfuscated aliases (vps-01), vCPU/RAM/disk specs, state, and expiration countdown.",
        "inputSchema": {
            "type": "object",
            "properties": {
                "profile": {"type": "string", "description": "Optional account profile alias (e.g. 'acct-01')."},
            },
            "additionalProperties": False,
        },
    },
    {
        "name": "order_list",
        "scope": "read",
        "description": "List recent OVHcloud purchase orders (`ord-01`), prices, and delivery status. Secret payment/invoice URLs are stripped for security.",
        "inputSchema": {
            "type": "object",
            "properties": {
                "profile": {"type": "string", "description": "Optional account profile alias (e.g. 'acct-01')."},
                "limit": {"type": "integer", "description": "Number of orders per profile (1-20, default 10)."},
            },
            "additionalProperties": False,
        },
    },
    {
        "name": "order_follow_up",
        "scope": "read",
        "description": "Inspect the 4-stage physical hardware delivery pipeline (CREATING -> VALIDATING -> DELIVERING -> INSTALLATION) for an order alias (`ord-01`).",
        "inputSchema": {
            "type": "object",
            "properties": {
                "order": {"type": "string", "description": "Order alias (e.g. 'ord-01')."},
                "profile": {"type": "string", "description": "Optional account profile alias (e.g. 'acct-01')."},
            },
            "required": ["order"],
            "additionalProperties": False,
        },
    },
    {
        "name": "service_renewals",
        "scope": "read",
        "description": "List upcoming expiration dates and renewal modes across dedicated servers and VPS instances.",
        "inputSchema": {
            "type": "object",
            "properties": {
                "profile": {"type": "string", "description": "Optional account profile alias (e.g. 'acct-01')."},
                "days_within": {"type": "integer", "description": "Optional filter: only return services expiring within N days."},
            },
            "additionalProperties": False,
        },
    },
    {
        "name": "procurement_status",
        "scope": "read",
        "description": "Get current status of OVH Nexus Auto-Order Queue tasks, stock monitor subscriptions, and recent purchase history.",
        "inputSchema": {
            "type": "object",
            "properties": {},
            "additionalProperties": False,
        },
    },

    # --- L2: Monitoring & Unpaid Procurement (scope: procurement) ---
    {
        "name": "order_quote",
        "scope": "procurement",
        "description": "Step 1 of 2 for placing an unpaid server order: query exact tax-inclusive/exclusive monthly pricing for a server configuration and generate a 5-minute confirmation `quote_id`. Does NOT place an order or charge money.",
        "inputSchema": {
            "type": "object",
            "properties": {
                "plan_code": {"type": "string", "description": "Server planCode (e.g. '24ska01')."},
                "datacenter": {"type": "string", "description": "Target datacenter code (e.g. 'gra', 'rbx', 'bhs')."},
                "options": {
                    "type": "array",
                    "items": {"type": "string"},
                    "description": "Optional hardware addon planCodes (e.g. ['ram-64g-ecc-2133', 'softraid-2x450nvme']).",
                },
                "profile": {"type": "string", "description": "Optional account profile alias (e.g. 'acct-01')."},
            },
            "required": ["plan_code", "datacenter"],
            "additionalProperties": False,
        },
    },
    {
        "name": "order_create_unpaid",
        "scope": "procurement",
        "description": "Step 2 of 2: place an UNPAID purchase order using a valid `quote_id` from `order_quote`. Hardcoded to `auto_pay=False` and `quantity=1` — never charges any payment method. The payment link is sent directly to the owner's Telegram/Email and Web Console.",
        "inputSchema": {
            "type": "object",
            "properties": {
                "quote_id": {"type": "string", "description": "Confirmation quoteId returned by `order_quote` within the last 5 minutes."},
            },
            "required": ["quote_id"],
            "additionalProperties": False,
        },
    },
    {
        "name": "queue_add",
        "scope": "procurement",
        "description": "Add a server target to the background Auto-Order Queue to automatically lock in an UNPAID order (`auto_pay=False`, `quantity=1` hardcoded) when stock appears.",
        "inputSchema": {
            "type": "object",
            "properties": {
                "plan_code": {"type": "string", "description": "Server planCode (e.g. '24ska01')."},
                "datacenters": {
                    "type": "array",
                    "items": {"type": "string"},
                    "description": "Ordered list of preferred datacenters (e.g. ['gra', 'rbx']).",
                },
                "options": {
                    "type": "array",
                    "items": {"type": "string"},
                    "description": "Optional hardware option codes.",
                },
                "profile": {"type": "string", "description": "Optional account profile alias (e.g. 'acct-01')."},
            },
            "required": ["plan_code", "datacenters"],
            "additionalProperties": False,
        },
    },
    {
        "name": "queue_manage",
        "scope": "procurement",
        "description": "Pause, resume, or delete an existing task in the Auto-Order Queue.",
        "inputSchema": {
            "type": "object",
            "properties": {
                "task_id": {"type": "string", "description": "Queue task ID from `procurement_status`."},
                "action": {"type": "string", "enum": ["pause", "resume", "delete"], "description": "Action to perform on the queue task."},
            },
            "required": ["task_id", "action"],
            "additionalProperties": False,
        },
    },
    {
        "name": "stock_subscription_manage",
        "scope": "procurement",
        "description": "Add or remove a server/VPS planCode in the real-time stock restock alert monitor.",
        "inputSchema": {
            "type": "object",
            "properties": {
                "target": {"type": "string", "enum": ["baremetal", "vps"], "description": "Monitor type ('baremetal' or 'vps')."},
                "action": {"type": "string", "enum": ["add", "remove"], "description": "Whether to add or remove the subscription."},
                "plan_code": {"type": "string", "description": "Plan code to subscribe/unsubscribe (e.g. '24ska01' or 'vps-le-1')."},
            },
            "required": ["target", "action", "plan_code"],
            "additionalProperties": False,
        },
    },

    # --- L3: Reversible Day-2 Operations (scope: operations) ---
    {
        "name": "baremetal_reboot",
        "scope": "operations",
        "description": "Trigger a hardware reboot on a purchased dedicated server (`bm-xx`, similar to `ovhcloud baremetal reboot`).",
        "inputSchema": {
            "type": "object",
            "properties": {
                "server": {"type": "string", "description": "Bare-metal server alias (e.g. 'bm-01')."},
            },
            "required": ["server"],
            "additionalProperties": False,
        },
    },
    {
        "name": "vps_reboot",
        "scope": "operations",
        "description": "Reboot a purchased VPS instance (`vps-xx`, similar to `ovhcloud vps reboot`).",
        "inputSchema": {
            "type": "object",
            "properties": {
                "vps": {"type": "string", "description": "VPS alias (e.g. 'vps-01')."},
            },
            "required": ["vps"],
            "additionalProperties": False,
        },
    },
    {
        "name": "baremetal_ipmi_reset",
        "scope": "operations",
        "description": "Reset stuck IPMI/KVM interface and sessions on a dedicated server (`bm-xx`, similar to `ovhcloud baremetal ipmi reset-sessions`). Does not expose raw IPMI console URLs.",
        "inputSchema": {
            "type": "object",
            "properties": {
                "server": {"type": "string", "description": "Bare-metal server alias (e.g. 'bm-01')."},
            },
            "required": ["server"],
            "additionalProperties": False,
        },
    },
    {
        "name": "baremetal_firewall_update",
        "scope": "operations",
        "description": "Manage OVHcloud IP Edge Firewall or permanent Anti-DDoS mitigation on a dedicated server (`bm-xx`).",
        "inputSchema": {
            "type": "object",
            "properties": {
                "server": {"type": "string", "description": "Bare-metal server alias (e.g. 'bm-01')."},
                "operation": {
                    "type": "string",
                    "enum": ["set_mitigation", "add_rule", "delete_rule"],
                    "description": "Operation to perform on the server's primary IPv4.",
                },
                "permanent_mitigation": {"type": "boolean", "description": "For 'set_mitigation': enable or disable permanent Anti-DDoS mitigation."},
                "sequence": {"type": "integer", "description": "For 'add_rule' / 'delete_rule': rule priority sequence (0-19)."},
                "rule_action": {"type": "string", "enum": ["permit", "deny"], "description": "For 'add_rule': permit or deny traffic."},
                "protocol": {"type": "string", "enum": ["tcp", "udp", "icmp", "ipv4"], "description": "For 'add_rule': network protocol."},
                "destination_port": {"type": "integer", "description": "For 'add_rule' (tcp/udp): destination port (1-65535)."},
                "source": {"type": "string", "description": "For 'add_rule': optional source CIDR (default 'any')."},
            },
            "required": ["server", "operation"],
            "additionalProperties": False,
        },
    },
]

TOOLS_BY_NAME: Dict[str, Dict[str, Any]] = {t["name"]: t for t in TOOL_DEFINITIONS}


def get_enabled_tools_for_mcp() -> List[Dict[str, Any]]:
    """Return MCP tool definitions filtered by currently enabled permission scopes."""
    cfg = load_mcp_config()
    scopes = cfg.get("scopes") or {}
    enabled_list = []
    for t in TOOL_DEFINITIONS:
        scope_key = t.get("scope", "read")
        if scopes.get(scope_key, False):
            enabled_list.append({
                "name": t["name"],
                "description": t["description"],
                "inputSchema": t["inputSchema"],
            })
    return enabled_list


# ==================== Tool Execution Handlers ====================

def execute_mcp_tool(tool_name: str, arguments: Optional[Dict[str, Any]] = None) -> Tuple[Dict[str, Any], str, str]:
    """
    Validate scope and execute an MCP tool.
    Returns (result_payload, scope_name, human_audit_summary).
    Raises PermissionError if scope is disabled or tool is blocked, or ValueError on invalid arguments.
    """
    args = dict(arguments or {})
    tool_def = TOOLS_BY_NAME.get(tool_name)
    if not tool_def:
        raise PermissionError(
            f"Tool '{tool_name}' is not available or is permanently blocked by OVH Nexus security policy (L4 destructive operations and payment execution are prohibited)."
        )

    scope = tool_def["scope"]
    cfg = load_mcp_config()
    if not (cfg.get("scopes") or {}).get(scope, False):
        raise PermissionError(
            f"Tool '{tool_name}' requires the '{scope}' permission scope, which is currently disabled in the OVH Nexus MCP Gateway settings."
        )

    handler = _HANDLERS.get(tool_name)
    if not handler:
        raise ValueError(f"No handler registered for tool '{tool_name}'.")

    result, summary = handler(args, cfg)
    return result, scope, summary


# --- L1 Handlers ---

def _handle_profile_list(args: Dict[str, Any], cfg: Dict[str, Any]) -> Tuple[Dict[str, Any], str]:
    profiles = [pmeta for _, _, pmeta in _resolve_target_accounts(None)]
    return (
        {"profiles": profiles, "total": len(profiles)},
        f"Listed {len(profiles)} whitelisted OVH account profile(s).",
    )


def _handle_catalog_availability(args: Dict[str, Any], cfg: Dict[str, Any]) -> Tuple[Dict[str, Any], str]:
    A = _get_app_module()
    plan_filter = str(args.get("plan_code") or "").strip().lower()
    dc_filter = str(args.get("datacenter") or "").strip().lower()
    only_avail = bool(args.get("only_available", False))
    try:
        limit = min(max(int(args.get("limit", 20)), 1), 50)
    except Exception:
        limit = 20

    helper = A.get_global_helper(None, max_calls_per_second=5)
    params = {}
    if plan_filter and "-" not in plan_filter and len(plan_filter) <= 16:
        # Could be exact planCode or substring; fetch all or filtered
        pass
    raw_avail = helper.get("https://eu.api.ovh.com/1.0/dedicated/server/datacenter/availabilities", **params) or []

    grouped: Dict[str, Dict[str, Any]] = {}
    for entry in raw_avail:
        if not isinstance(entry, dict):
            continue
        pcode = str(entry.get("planCode") or "").strip()
        if not pcode:
            continue
        meta = catalog_service.resolve_plan(pcode) or {}
        friendly_name = str(meta.get("name") or pcode)
        if plan_filter and plan_filter not in pcode.lower() and plan_filter not in friendly_name.lower():
            continue

        dcs_in_stock = []
        all_dcs = {}
        for dc_obj in entry.get("datacenters") or []:
            dc_code = str(dc_obj.get("datacenter") or "").lower()
            avail_val = str(dc_obj.get("availability") or "unavailable")
            if dc_filter and dc_filter not in dc_code:
                continue
            all_dcs[dc_code] = avail_val
            if avail_val not in ("unavailable", "unknown", ""):
                dcs_in_stock.append(f"{dc_code.upper()} ({avail_val})")

        if only_avail and not dcs_in_stock:
            continue

        rec = grouped.get(pcode)
        if not rec:
            rec = {
                "planCode": pcode,
                "modelName": friendly_name,
                "range": meta.get("range") or "",
                "cpu": format_cpu(meta.get("cpu"), "en") or entry.get("server") or "",
                "availableDatacenters": [],
                "matchingConfigurations": 0,
            }
            grouped[pcode] = rec

        rec["matchingConfigurations"] += 1
        for dc_tag in dcs_in_stock:
            if dc_tag not in rec["availableDatacenters"]:
                rec["availableDatacenters"].append(dc_tag)

    items = list(grouped.values())[:limit]
    return (
        {"items": items, "returned": len(items), "totalMatched": len(grouped)},
        f"Queried bare-metal stock matrix (matched {len(grouped)} plan(s)).",
    )


def _handle_catalog_sku_changes(args: Dict[str, Any], cfg: Dict[str, Any]) -> Tuple[Dict[str, Any], str]:
    A = _get_app_module()
    watcher = getattr(A, "sku_watcher", None)
    try:
        limit = min(max(int(args.get("limit", 20)), 1), 50)
    except Exception:
        limit = 20
    change_type = str(args.get("change_type") or "ALL").upper()
    changes = watcher.get_changes(limit=limit, change_type=None if change_type == "ALL" else change_type) if watcher else []
    status = watcher.get_status() if watcher else {}
    return (
        {"status": status, "changes": changes},
        f"Retrieved {len(changes)} SKU watcher change event(s).",
    )


def _handle_baremetal_list(args: Dict[str, Any], cfg: Dict[str, Any]) -> Tuple[Dict[str, Any], str]:
    A = _get_app_module()
    targets = _resolve_target_accounts(args.get("profile"))
    servers: List[Dict[str, Any]] = []

    for aid, palias, _ in targets:
        client = A.get_ovh_client(None if aid == "default" else aid)
        if not client:
            continue
        try:
            names = client.get("/dedicated/server") or []
        except Exception:
            continue
        for srv_name in names:
            try:
                enc = quote(str(srv_name), safe="")
                info = client.get(f"/dedicated/server/{enc}") or {}
                try:
                    svc = client.get(f"/dedicated/server/{enc}/serviceInfos") or {}
                except Exception:
                    svc = {}
                exp = svc.get("expiration")
                renew_obj = svc.get("renew") or {}
                iam_display = ((info.get("iam") or {}).get("displayName")) or info.get("reverse") or ""
                raw_srv = {
                    "serviceName": srv_name,
                    "displayName": iam_display,
                    "commercialRange": info.get("commercialRange") or "N/A",
                    "datacenter": info.get("datacenter") or "N/A",
                    "state": info.get("state") or "unknown",
                    "status": svc.get("status") or "ok",
                    "os": info.get("os") or "N/A",
                    "monitoring": bool(info.get("monitoring", False)),
                    "ip": info.get("ip") or "N/A",
                    "expiration": exp,
                    "daysRemaining": _days_until(exp),
                    "renewMode": _compute_renew_mode(renew_obj),
                }
                servers.append(privacy_shield.redact_baremetal(raw_srv, account_alias=palias, account_id=aid))
            except Exception:
                continue

    return (
        {"servers": servers, "total": len(servers)},
        f"Listed {len(servers)} obfuscated bare-metal server(s).",
    )


def _handle_baremetal_get(args: Dict[str, Any], cfg: Dict[str, Any]) -> Tuple[Dict[str, Any], str]:
    srv_arg = str(args.get("server") or "").strip()
    real_name, bm_alias, aid, client = _find_server_owner_account("baremetal", srv_arg)
    enc = quote(real_name, safe="")

    info = client.get(f"/dedicated/server/{enc}") or {}
    try:
        svc = client.get(f"/dedicated/server/{enc}/serviceInfos") or {}
    except Exception:
        svc = {}
    try:
        hw = client.get(f"/dedicated/server/{enc}/specifications/hardware") or {}
    except Exception:
        hw = {}
    try:
        tasks = client.get(f"/dedicated/server/{enc}/task") or []
    except Exception:
        tasks = []

    palias = privacy_shield.register("account", aid)
    exp = svc.get("expiration")
    renew_obj = svc.get("renew") or {}
    iam_display = ((info.get("iam") or {}).get("displayName")) or info.get("reverse") or ""
    base_redacted = privacy_shield.redact_baremetal(
        {
            "serviceName": real_name,
            "displayName": iam_display,
            "commercialRange": info.get("commercialRange") or hw.get("commercialRange") or "N/A",
            "datacenter": info.get("datacenter") or "N/A",
            "state": info.get("state") or "unknown",
            "status": svc.get("status") or "ok",
            "os": info.get("os") or "N/A",
            "monitoring": bool(info.get("monitoring", False)),
            "ip": info.get("ip") or "N/A",
            "expiration": exp,
            "daysRemaining": _days_until(exp),
            "renewMode": _compute_renew_mode(renew_obj),
        },
        account_alias=palias,
        account_id=aid,
    )

    disk_groups = []
    for dg in hw.get("diskGroups") or []:
        if isinstance(dg, dict):
            disk_groups.append({
                "description": dg.get("description"),
                "numberOfDisks": dg.get("numberOfDisks"),
                "diskType": dg.get("diskType"),
                "diskSize": dg.get("diskSize"),
                "raidController": dg.get("raidController"),
            })

    base_redacted["hardware"] = {
        "processorName": hw.get("processorName"),
        "processorArchitecture": hw.get("processorArchitecture"),
        "coresPerProcessor": hw.get("coresPerProcessor"),
        "threadsPerProcessor": hw.get("threadsPerProcessor"),
        "memorySize": hw.get("memorySize"),
        "bootMode": hw.get("bootMode"),
        "diskGroups": disk_groups,
    }
    base_redacted["recentTaskCount"] = len(tasks)
    return base_redacted, f"Inspected bare-metal server {bm_alias}."


def _resolve_server_primary_ipv4(client: Any, real_service_name: str) -> Tuple[Optional[str], Optional[str]]:
    enc_srv = quote(str(real_service_name), safe="")
    ipv4_blocks = []
    try:
        ips = client.get(f"/dedicated/server/{enc_srv}/ips") or []
        ipv4_blocks = [str(b) for b in ips if ":" not in str(b)]
    except Exception:
        pass
    if not ipv4_blocks:
        try:
            srv_info = client.get(f"/dedicated/server/{enc_srv}") or {}
            primary_ip = srv_info.get("ip")
            if primary_ip:
                ipv4_blocks = [str(primary_ip)]
        except Exception:
            pass
    if not ipv4_blocks:
        return None, None
    first_block = ipv4_blocks[0]
    return first_block, first_block.split("/")[0]


def _handle_baremetal_firewall_get(args: Dict[str, Any], cfg: Dict[str, Any]) -> Tuple[Dict[str, Any], str]:
    srv_arg = str(args.get("server") or "").strip()
    real_name, bm_alias, aid, client = _find_server_owner_account("baremetal", srv_arg)
    ip_block, ip_addr = _resolve_server_primary_ipv4(client, real_name)
    if not ip_block or not ip_addr:
        raise ValueError(f"No primary IPv4 found for server {bm_alias}.")

    enc_block = quote(ip_block, safe="")
    enc_ip = quote(ip_addr, safe="")
    ip_alias = privacy_shield.register("ip", ip_addr, meta={"hint": format_ip_hint(ip_addr)})

    try:
        fw = client.get(f"/ip/{enc_block}/firewall/{enc_ip}") or {}
    except Exception:
        fw = {"enabled": False, "state": "not_configured"}

    rules = []
    try:
        seqs = client.get(f"/ip/{enc_block}/firewall/{enc_ip}/rule") or []
        for seq in sorted(seqs):
            try:
                r = client.get(f"/ip/{enc_block}/firewall/{enc_ip}/rule/{seq}") or {}
                rules.append({
                    "sequence": r.get("sequence", seq),
                    "action": r.get("action"),
                    "protocol": r.get("protocol"),
                    "source": privacy_shield.scrub_text(str(r.get("source") or "any")),
                    "destinationPort": r.get("destinationPort"),
                    "state": r.get("state"),
                })
            except Exception:
                continue
    except Exception:
        rules = []

    try:
        mitigation = client.get(f"/ip/{enc_block}/mitigation/{enc_ip}") or {}
    except Exception:
        mitigation = {"permanent": False, "auto": False, "state": "auto"}

    return (
        {
            "server": bm_alias,
            "ip": {"alias": ip_alias, "maskedTail": format_ip_hint(ip_addr)},
            "firewall": {
                "enabled": bool(fw.get("enabled", False)),
                "state": fw.get("state", "not_configured"),
            },
            "mitigation": {
                "permanent": bool(mitigation.get("permanent", False)),
                "auto": bool(mitigation.get("auto", False)),
                "state": mitigation.get("state", "auto"),
            },
            "rules": rules,
        },
        f"Inspected IP Edge Firewall & Anti-DDoS state for {bm_alias} ({ip_alias}).",
    )


def _handle_vps_list(args: Dict[str, Any], cfg: Dict[str, Any]) -> Tuple[Dict[str, Any], str]:
    A = _get_app_module()
    targets = _resolve_target_accounts(args.get("profile"))
    vps_items: List[Dict[str, Any]] = []

    for aid, palias, _ in targets:
        client = A.get_ovh_client(None if aid == "default" else aid)
        if not client:
            continue
        try:
            names = client.get("/vps") or []
        except Exception:
            continue
        for vname in names:
            try:
                enc = quote(str(vname), safe="")
                detail = client.get(f"/vps/{enc}") or {}
                try:
                    ips = client.get(f"/vps/{enc}/ips") or []
                except Exception:
                    ips = []
                try:
                    svc = client.get(f"/vps/{enc}/serviceInfos") or {}
                except Exception:
                    svc = {}
                model = detail.get("model") or {}
                exp = svc.get("expiration")
                renew_obj = svc.get("renew") or {}
                model_str = model.get("offer") or model.get("name") or ""
                raw_vps = {
                    "serviceName": vname,
                    "displayName": detail.get("displayName") or "",
                    "state": detail.get("state", "unknown"),
                    "zone": detail.get("zone") or "",
                    "vcore": detail.get("vcore") or model.get("vcore"),
                    "memoryLimitMb": detail.get("memoryLimit") or model.get("memory"),
                    "diskGb": model.get("disk"),
                    "modelName": model_str,
                    "ips": ips,
                    "expiration": exp,
                    "daysRemaining": _days_until(exp),
                    "renewMode": _compute_renew_mode(renew_obj),
                }
                vps_items.append(privacy_shield.redact_vps(raw_vps, account_alias=palias, account_id=aid))
            except Exception:
                continue

    return (
        {"vps": vps_items, "total": len(vps_items)},
        f"Listed {len(vps_items)} obfuscated VPS instance(s).",
    )


def _handle_order_list(args: Dict[str, Any], cfg: Dict[str, Any]) -> Tuple[Dict[str, Any], str]:
    A = _get_app_module()
    targets = _resolve_target_accounts(args.get("profile"))
    try:
        limit = min(max(int(args.get("limit", 10)), 1), 20)
    except Exception:
        limit = 10

    orders: List[Dict[str, Any]] = []
    for aid, palias, _ in targets:
        client = A.get_ovh_client(None if aid == "default" else aid)
        if not client:
            continue
        try:
            order_ids = client.get("/me/order") or []
        except Exception:
            continue
        for oid in sorted(order_ids, reverse=True)[:limit]:
            try:
                detail = client.get(f"/me/order/{oid}") or {}
            except Exception:
                continue
            try:
                status = client.get(f"/me/order/{oid}/status")
            except Exception:
                status = "unknown"
            try:
                follow_up = client.get(f"/me/order/{oid}/followUp") or []
            except Exception:
                follow_up = []
            orders.append(
                privacy_shield.redact_order(
                    {
                        "orderId": detail.get("orderId", oid),
                        "date": detail.get("date"),
                        "expirationDate": detail.get("expirationDate"),
                        "priceWithTax": detail.get("priceWithTax"),
                        "priceWithoutTax": detail.get("priceWithoutTax"),
                        "tax": detail.get("tax"),
                        "status": status,
                        "followUp": follow_up,
                    },
                    account_alias=palias,
                )
            )

    return (
        {"orders": orders, "total": len(orders)},
        f"Listed {len(orders)} recent order(s) with payment URLs redacted.",
    )


def _handle_order_follow_up(args: Dict[str, Any], cfg: Dict[str, Any]) -> Tuple[Dict[str, Any], str]:
    A = _get_app_module()
    ord_arg = str(args.get("order") or "").strip()
    real_oid = privacy_shield.resolve("order", ord_arg)
    if not real_oid:
        raise ValueError(f"Unknown order alias '{ord_arg}'. Call `order_list` first to obtain valid order aliases (e.g. 'ord-01').")

    targets = _resolve_target_accounts(args.get("profile"))
    for aid, palias, _ in targets:
        client = A.get_ovh_client(None if aid == "default" else aid)
        if not client:
            continue
        try:
            detail = client.get(f"/me/order/{real_oid}") or {}
            status = client.get(f"/me/order/{real_oid}/status")
            follow_up = client.get(f"/me/order/{real_oid}/followUp") or []
            redacted = privacy_shield.redact_order(
                {
                    "orderId": real_oid,
                    "date": detail.get("date"),
                    "expirationDate": detail.get("expirationDate"),
                    "priceWithTax": detail.get("priceWithTax"),
                    "priceWithoutTax": detail.get("priceWithoutTax"),
                    "tax": detail.get("tax"),
                    "status": status,
                    "followUp": follow_up,
                },
                account_alias=palias,
            )
            return redacted, f"Checked 4-stage delivery follow-up for {redacted.get('order')}."
        except Exception:
            continue

    raise ValueError(f"Order '{ord_arg}' was not found under any whitelisted profile.")


def _handle_service_renewals(args: Dict[str, Any], cfg: Dict[str, Any]) -> Tuple[Dict[str, Any], str]:
    A = _get_app_module()
    targets = _resolve_target_accounts(args.get("profile"))
    days_within = args.get("days_within")
    max_days = int(days_within) if days_within is not None else None

    services: List[Dict[str, Any]] = []
    for aid, palias, _ in targets:
        client = A.get_ovh_client(None if aid == "default" else aid)
        if not client:
            continue

        try:
            dedicated_names = client.get("/dedicated/server") or []
        except Exception:
            dedicated_names = []
        for srv in dedicated_names:
            try:
                enc = quote(str(srv), safe="")
                info = client.get(f"/dedicated/server/{enc}/serviceInfos") or {}
                exp = info.get("expiration")
                days_rem = _days_until(exp)
                if max_days is not None and (days_rem is None or days_rem > max_days):
                    continue
                renew_obj = info.get("renew") or {}
                bm_alias = privacy_shield.register("baremetal", srv, meta={"accountAlias": palias, "accountId": aid})
                services.append({
                    "service": bm_alias,
                    "type": "baremetal",
                    "profile": palias,
                    "status": info.get("status", "ok"),
                    "expiration": exp,
                    "daysRemaining": days_rem,
                    "renewMode": _compute_renew_mode(renew_obj),
                })
            except Exception:
                continue

        try:
            vps_names = client.get("/vps") or []
        except Exception:
            vps_names = []
        for vps in vps_names:
            try:
                enc = quote(str(vps), safe="")
                info = client.get(f"/vps/{enc}/serviceInfos") or {}
                exp = info.get("expiration")
                days_rem = _days_until(exp)
                if max_days is not None and (days_rem is None or days_rem > max_days):
                    continue
                renew_obj = info.get("renew") or {}
                vps_alias = privacy_shield.register("vps", vps, meta={"accountAlias": palias, "accountId": aid})
                services.append({
                    "service": vps_alias,
                    "type": "vps",
                    "profile": palias,
                    "status": info.get("status", "ok"),
                    "expiration": exp,
                    "daysRemaining": days_rem,
                    "renewMode": _compute_renew_mode(renew_obj),
                })
            except Exception:
                continue

    services.sort(key=lambda s: (s["daysRemaining"] is None, s["daysRemaining"] if s["daysRemaining"] is not None else 999999))
    return (
        {"services": services, "total": len(services)},
        f"Checked renewal matrix ({len(services)} service(s)).",
    )


def _handle_procurement_status(args: Dict[str, Any], cfg: Dict[str, Any]) -> Tuple[Dict[str, Any], str]:
    A = _get_app_module()
    allowed_profiles = _resolve_target_accounts(None)
    allowed_ids = {aid for aid, _, _ in allowed_profiles}
    alias_by_aid = {aid: palias for aid, palias, _ in allowed_profiles}

    raw_queue = getattr(A, "queue", []) or []
    safe_queue = []
    for q in raw_queue:
        q_aid = q.get("accountId") or "default"
        if allowed_ids and q_aid not in allowed_ids and q.get("accountId") is not None:
            continue
        safe_queue.append({
            "id": q.get("id"),
            "planCode": q.get("planCode"),
            "datacenters": q.get("datacenters") or ([q.get("datacenter")] if q.get("datacenter") else []),
            "options": q.get("options") or [],
            "status": q.get("status"),
            "retryCount": q.get("retryCount", 0),
            "quantity": q.get("quantity", 1),
            "purchased": q.get("purchased", 0),
            "autoPay": False,  # Never expose or allow autoPay via MCP
            "profile": alias_by_aid.get(q_aid),
            "createdAt": q.get("createdAt"),
        })

    raw_history = getattr(A, "purchase_history", []) or []
    safe_history = []
    for h in reversed(raw_history[-25:]):
        h_aid = h.get("accountId") or "default"
        if allowed_ids and h_aid not in allowed_ids and h.get("accountId") is not None:
            continue
        raw_oid = h.get("orderId")
        ord_alias = privacy_shield.register("order", raw_oid) if raw_oid else None
        safe_history.append({
            "planCode": h.get("planCode"),
            "datacenter": h.get("datacenter"),
            "options": h.get("options") or [],
            "status": h.get("status"),
            "order": ord_alias,
            "price": h.get("price"),
            "purchaseTime": h.get("purchaseTime"),
            "profile": alias_by_aid.get(h_aid),
            "errorMessage": privacy_shield.scrub_text(str(h.get("errorMessage") or "")) if h.get("errorMessage") else None,
        })

    bm_subs = []
    monitor_obj = getattr(A, "monitor", None)
    if monitor_obj and hasattr(monitor_obj, "get_subscriptions"):
        try:
            for s in monitor_obj.get_subscriptions() or []:
                bm_subs.append({
                    "planCode": s.get("planCode"),
                    "datacenters": s.get("datacenters") or [],
                    "notifyAvailable": s.get("notifyAvailable", True),
                })
        except Exception:
            pass

    return (
        {
            "queue": safe_queue,
            "baremetalSubscriptions": bm_subs,
            "recentPurchaseHistory": safe_history,
        },
        f"Inspected procurement status ({len(safe_queue)} queue task(s), {len(bm_subs)} subscription(s)).",
    )


# --- L2 Handlers (Procurement — Hardcoded auto_pay=False & quantity=1) ---

def _handle_order_quote(args: Dict[str, Any], cfg: Dict[str, Any]) -> Tuple[Dict[str, Any], str]:
    A = _get_app_module()
    plan_code = str(args.get("plan_code") or "").strip()
    datacenter = str(args.get("datacenter") or "gra").strip().lower()
    options = [str(o).strip() for o in (args.get("options") or []) if str(o).strip()]
    if not plan_code:
        raise ValueError("`plan_code` is required.")

    targets = _resolve_target_accounts(args.get("profile"))
    if not targets:
        raise ValueError("No whitelisted OVH account is available to request a price quote.")
    aid, palias, _ = targets[0]

    price_res = A._get_server_price_internal(plan_code, datacenter, options, None if aid == "default" else aid)
    if not price_res.get("success"):
        raise ValueError(f"Failed to retrieve OVH cart quote for {plan_code}: {price_res.get('error') or 'Unknown error'}")

    price_info = price_res.get("price") or {}
    quote_obj = create_order_quote(
        plan_code=plan_code,
        datacenter=datacenter,
        options=options,
        account_id=aid,
        account_alias=palias,
        price_data=price_info,
    )
    meta = catalog_service.resolve_plan(plan_code) or {}
    return (
        {
            "quoteId": quote_obj["quoteId"],
            "expiresInSeconds": quote_obj["ttlSeconds"],
            "planCode": plan_code,
            "modelName": meta.get("name") or plan_code,
            "datacenter": datacenter.upper(),
            "options": options,
            "profile": palias,
            "quantity": 1,
            "autoPay": False,
            "price": price_info,
            "nextStep": "Confirm with the user, then call `order_create_unpaid` with this `quote_id` within 5 minutes to generate an unpaid order.",
        },
        f"Generated 5-min price quote {quote_obj['quoteId']} for {plan_code} @ {datacenter.upper()} (profile {palias}).",
    )


def _handle_order_create_unpaid(args: Dict[str, Any], cfg: Dict[str, Any]) -> Tuple[Dict[str, Any], str]:
    A = _get_app_module()
    quote_id = str(args.get("quote_id") or "").strip()
    if not quote_id:
        raise ValueError("`quote_id` is required. Call `order_quote` first to get a valid quote_id.")

    hourly_limit = int(cfg.get("hourlyOrderLimit", 5))
    allowed, remaining = check_order_rate_limit(hourly_limit)
    if not allowed:
        raise PermissionError(
            f"Hourly MCP unpaid order limit ({hourly_limit}/hour) reached. Please wait or adjust the limit in the OVH Nexus MCP Gateway settings."
        )

    quote_obj = consume_order_quote(quote_id)
    if not quote_obj:
        raise ValueError("Invalid or expired `quote_id`. Quotes expire after 5 minutes and can only be used once. Call `order_quote` again.")

    aid = quote_obj["accountId"]
    palias = quote_obj["profile"]
    plan_code = quote_obj["planCode"]
    datacenter = quote_obj["datacenter"]
    options = quote_obj.get("options") or []

    # Re-verify account is still whitelisted
    _resolve_target_accounts(palias)

    # SECURITY REDLINE: auto_pay and waive_retractation_period are unconditionally False and quantity is unconditionally 1
    synthetic_task = {
        "id": f"mcp-order-{uuid.uuid4().hex[:8]}",
        "planCode": plan_code,
        "datacenters": [datacenter],
        "options": options,
        "accountId": None if aid == "default" else aid,
        "auto_pay": False,
        "waive_retractation_period": False,
        "quantity": 1,
        "purchased": 0,
        "retryCount": 1,
    }

    sem_map = getattr(A, "account_checkout_semaphores", {})
    acct_key = synthetic_task["accountId"]
    sem = sem_map.setdefault(acct_key, threading.Semaphore(1))
    with sem:
        ok = A.purchase_server(synthetic_task)

    if not ok:
        raise ValueError(
            f"Could not place unpaid order for {plan_code} @ {datacenter.upper()} (item may be out of stock or OVH API rejected the cart). Consider using `queue_add` to watch for restock."
        )

    record_order_execution()

    # Locate the newly created history entry and redact orderId/orderUrl
    latest_entry = None
    for h in reversed(getattr(A, "purchase_history", []) or []):
        if h.get("taskId") == synthetic_task["id"] and h.get("status") == "success":
            latest_entry = h
            break

    raw_oid = (latest_entry or {}).get("orderId")
    ord_alias = privacy_shield.register("order", raw_oid, meta={"accountAlias": palias}) if raw_oid else "ord-new"

    return (
        {
            "status": "unpaid_order_created",
            "order": ord_alias,
            "planCode": plan_code,
            "datacenter": datacenter.upper(),
            "options": options,
            "profile": palias,
            "quantity": 1,
            "autoPay": False,
            "price": (latest_entry or {}).get("price") or quote_obj.get("price"),
            "remainingHourlyQuota": max(0, remaining - 1),
            "paymentInstructions": "Unpaid order locked in! For security, the payment link is NOT exposed to the AI. It has been dispatched to your Notification Center (Telegram/Email) and is available in the OVH Nexus Web Console -> [Orders & Delivery].",
        },
        f"Created UNPAID order {ord_alias} for {plan_code} @ {datacenter.upper()} (auto_pay=False, profile {palias}).",
    )


def _handle_queue_add(args: Dict[str, Any], cfg: Dict[str, Any]) -> Tuple[Dict[str, Any], str]:
    A = _get_app_module()
    plan_code = str(args.get("plan_code") or "").strip()
    raw_dcs = args.get("datacenters") or []
    dcs = [str(d).strip().lower() for d in raw_dcs if str(d).strip()]
    options = [str(o).strip() for o in (args.get("options") or []) if str(o).strip()]

    if not plan_code or not dcs:
        raise ValueError("Both `plan_code` and a non-empty `datacenters` list are required.")

    targets = _resolve_target_accounts(args.get("profile"))
    if not targets:
        raise ValueError("No whitelisted OVH account is available for queueing.")
    aid, palias, _ = targets[0]

    # SECURITY REDLINE: Hardcode auto_pay=False, waive_retractation_period=False, and quantity=1
    new_item = {
        "id": str(uuid.uuid4()),
        "planCode": plan_code,
        "datacenters": dcs,
        "options": options,
        "status": "running",
        "createdAt": datetime.now().isoformat(),
        "retryInterval": 30,
        "retryCount": 0,
        "accountId": None if aid == "default" else aid,
        "auto_pay": False,
        "waive_retractation_period": False,
        "quantity": 1,
        "purchased": 0,
        "source": "mcp",
    }
    A.queue.append(new_item)
    A.save_data()
    A.update_stats()

    return (
        {
            "taskId": new_item["id"],
            "planCode": plan_code,
            "datacenters": dcs,
            "options": options,
            "profile": palias,
            "quantity": 1,
            "autoPay": False,
            "status": "running",
        },
        f"Added Auto-Order Queue task {new_item['id']} for {plan_code} @ {','.join(dcs).upper()} (auto_pay=False, quantity=1).",
    )


def _handle_queue_manage(args: Dict[str, Any], cfg: Dict[str, Any]) -> Tuple[Dict[str, Any], str]:
    A = _get_app_module()
    task_id = str(args.get("task_id") or "").strip()
    action = str(args.get("action") or "").strip().lower()
    if not task_id or action not in ("pause", "resume", "delete"):
        raise ValueError("Valid `task_id` and `action` ('pause', 'resume', or 'delete') are required.")

    allowed_ids = {aid for aid, _, _ in _resolve_target_accounts(None)}
    target_item = None
    for item in A.queue:
        if item.get("id") == task_id:
            item_aid = item.get("accountId") or "default"
            if allowed_ids and item_aid not in allowed_ids and item.get("accountId") is not None:
                raise PermissionError("Queue task belongs to an account outside the MCP whitelist.")
            target_item = item
            break

    if not target_item:
        raise ValueError(f"Queue task '{task_id}' not found.")

    if action == "delete":
        A.deleted_task_ids.add(task_id)
        A.queue = [q for q in A.queue if q.get("id") != task_id]
    elif action == "pause":
        target_item["status"] = "paused"
    elif action == "resume":
        target_item["status"] = "running"

    A.save_data()
    A.update_stats()
    return (
        {"taskId": task_id, "action": action, "status": "deleted" if action == "delete" else target_item["status"]},
        f"Performed '{action}' on Auto-Order Queue task {task_id}.",
    )


def _handle_stock_subscription_manage(args: Dict[str, Any], cfg: Dict[str, Any]) -> Tuple[Dict[str, Any], str]:
    A = _get_app_module()
    target = str(args.get("target") or "baremetal").strip().lower()
    action = str(args.get("action") or "add").strip().lower()
    plan_code = str(args.get("plan_code") or "").strip()
    if not plan_code or target not in ("baremetal", "vps") or action not in ("add", "remove"):
        raise ValueError("Valid `target`, `action`, and `plan_code` are required.")

    if target == "baremetal":
        monitor_obj = getattr(A, "monitor", None)
        if not monitor_obj:
            raise ValueError("Bare-metal stock monitor is not initialized.")
        if action == "add":
            meta = catalog_service.resolve_plan(plan_code) or {}
            monitor_obj.add_subscription(plan_code, datacenters=[], server_name=meta.get("name") or plan_code)
        else:
            monitor_obj.remove_subscription(plan_code)
        A.save_subscriptions()
    else:
        vps_subs = getattr(A, "vps_subscriptions", [])
        if action == "add":
            if not any(s.get("planCode") == plan_code for s in vps_subs):
                vps_subs.append({
                    "id": str(uuid.uuid4()),
                    "planCode": plan_code,
                    "name": plan_code,
                    "notifyAvailable": True,
                    "notifyUnavailable": False,
                    "createdAt": datetime.now().isoformat(),
                    "history": [],
                })
                A.save_vps_subscriptions()
        else:
            A.vps_subscriptions = [s for s in vps_subs if s.get("planCode") != plan_code and s.get("id") != plan_code]
            A.save_vps_subscriptions()

    return (
        {"target": target, "action": action, "planCode": plan_code, "success": True},
        f"Updated {target} stock subscription ({action}: {plan_code}).",
    )


# --- L3 Handlers (Reversible Day-2 Operations) ---

def _handle_baremetal_reboot(args: Dict[str, Any], cfg: Dict[str, Any]) -> Tuple[Dict[str, Any], str]:
    srv_arg = str(args.get("server") or "").strip()
    real_name, bm_alias, _, client = _find_server_owner_account("baremetal", srv_arg)
    enc = quote(real_name, safe="")
    res = client.post(f"/dedicated/server/{enc}/reboot") or {}
    return (
        {
            "server": bm_alias,
            "action": "reboot",
            "status": "initiated",
            "taskId": res.get("taskId") if isinstance(res, dict) else None,
        },
        f"Initiated hardware reboot for bare-metal server {bm_alias}.",
    )


def _handle_vps_reboot(args: Dict[str, Any], cfg: Dict[str, Any]) -> Tuple[Dict[str, Any], str]:
    vps_arg = str(args.get("vps") or "").strip()
    real_name, vps_alias, _, client = _find_server_owner_account("vps", vps_arg)
    enc = quote(real_name, safe="")
    client.post(f"/vps/{enc}/reboot")
    return (
        {"vps": vps_alias, "action": "reboot", "status": "initiated"},
        f"Initiated reboot for VPS {vps_alias}.",
    )


def _handle_baremetal_ipmi_reset(args: Dict[str, Any], cfg: Dict[str, Any]) -> Tuple[Dict[str, Any], str]:
    srv_arg = str(args.get("server") or "").strip()
    real_name, bm_alias, _, client = _find_server_owner_account("baremetal", srv_arg)
    enc = quote(real_name, safe="")
    reset_tasks = []
    for sub in ("resetInterface", "resetSessions"):
        try:
            r = client.post(f"/dedicated/server/{enc}/features/ipmi/{sub}") or {}
            if isinstance(r, dict) and r.get("taskId"):
                reset_tasks.append(r.get("taskId"))
        except Exception:
            pass
    return (
        {"server": bm_alias, "action": "ipmi_reset", "status": "initiated", "taskIds": reset_tasks},
        f"Reset IPMI interface & sessions for bare-metal server {bm_alias}.",
    )


def _handle_baremetal_firewall_update(args: Dict[str, Any], cfg: Dict[str, Any]) -> Tuple[Dict[str, Any], str]:
    srv_arg = str(args.get("server") or "").strip()
    op = str(args.get("operation") or "").strip()
    real_name, bm_alias, _, client = _find_server_owner_account("baremetal", srv_arg)
    ip_block, ip_addr = _resolve_server_primary_ipv4(client, real_name)
    if not ip_block or not ip_addr:
        raise ValueError(f"No primary IPv4 found for server {bm_alias}.")

    enc_block = quote(ip_block, safe="")
    enc_ip = quote(ip_addr, safe="")
    ip_alias = privacy_shield.register("ip", ip_addr, meta={"hint": format_ip_hint(ip_addr)})

    if op == "set_mitigation":
        permanent = bool(args.get("permanent_mitigation", True))
        try:
            client.get(f"/ip/{enc_block}/mitigation/{enc_ip}")
            client.put(f"/ip/{enc_block}/mitigation/{enc_ip}", permanent=permanent)
        except Exception:
            if permanent:
                client.post(f"/ip/{enc_block}/mitigation", ipOnMitigation=ip_addr)
                try:
                    client.put(f"/ip/{enc_block}/mitigation/{enc_ip}", permanent=True)
                except Exception:
                    pass
        return (
            {"server": bm_alias, "ip": ip_alias, "operation": op, "permanentMitigation": permanent},
            f"Updated permanent Anti-DDoS mitigation={permanent} on {bm_alias} ({ip_alias}).",
        )

    if op == "add_rule":
        sequence = int(args.get("sequence", 0))
        if sequence < 0 or sequence > 19:
            raise ValueError("`sequence` must be between 0 and 19.")
        rule_action = str(args.get("rule_action") or "permit").strip()
        protocol = str(args.get("protocol") or "tcp").strip()
        if rule_action not in ("permit", "deny"):
            raise ValueError("`rule_action` must be 'permit' or 'deny'.")
        try:
            client.get(f"/ip/{enc_block}/firewall/{enc_ip}")
        except Exception:
            try:
                client.post(f"/ip/{enc_block}/firewall", ipOnFirewall=ip_addr)
            except Exception:
                pass
        payload: Dict[str, Any] = {
            "sequence": sequence,
            "action": rule_action,
            "protocol": protocol,
        }
        source = str(args.get("source") or "").strip()
        if source and source.lower() != "any":
            payload["source"] = source
        dest_port = args.get("destination_port")
        if dest_port is not None and protocol in ("tcp", "udp"):
            payload["destinationPort"] = int(dest_port)
        client.post(f"/ip/{enc_block}/firewall/{enc_ip}/rule", **payload)
        return (
            {"server": bm_alias, "ip": ip_alias, "operation": op, "sequence": sequence, "ruleAction": rule_action, "protocol": protocol},
            f"Added IP Edge Firewall rule #{sequence} ({rule_action} {protocol}) on {bm_alias} ({ip_alias}).",
        )

    if op == "delete_rule":
        sequence = int(args.get("sequence", 0))
        client.delete(f"/ip/{enc_block}/firewall/{enc_ip}/rule/{sequence}")
        return (
            {"server": bm_alias, "ip": ip_alias, "operation": op, "sequence": sequence, "deleted": True},
            f"Deleted IP Edge Firewall rule #{sequence} on {bm_alias} ({ip_alias}).",
        )

    raise ValueError(f"Unsupported firewall operation: {op}")


_HANDLERS = {
    "profile_list": _handle_profile_list,
    "catalog_availability": _handle_catalog_availability,
    "catalog_sku_changes": _handle_catalog_sku_changes,
    "baremetal_list": _handle_baremetal_list,
    "baremetal_get": _handle_baremetal_get,
    "baremetal_firewall_get": _handle_baremetal_firewall_get,
    "vps_list": _handle_vps_list,
    "order_list": _handle_order_list,
    "order_follow_up": _handle_order_follow_up,
    "service_renewals": _handle_service_renewals,
    "procurement_status": _handle_procurement_status,
    "order_quote": _handle_order_quote,
    "order_create_unpaid": _handle_order_create_unpaid,
    "queue_add": _handle_queue_add,
    "queue_manage": _handle_queue_manage,
    "stock_subscription_manage": _handle_stock_subscription_manage,
    "baremetal_reboot": _handle_baremetal_reboot,
    "vps_reboot": _handle_vps_reboot,
    "baremetal_ipmi_reset": _handle_baremetal_ipmi_reset,
    "baremetal_firewall_update": _handle_baremetal_firewall_update,
}
