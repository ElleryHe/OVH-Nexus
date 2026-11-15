"""
MCP Audit Log, Two-Step Quote Store & Hourly Order Rate Limiter
===============================================================
- Persists MCP tool invocation logs in backend/data/mcp_audit.json (0600, max 300 entries)
- Enforces hourly unpaid order rate limits (anti-runaway guardrail)
- Manages short-lived (5-minute) price confirmation quotes for two-step ordering
- Dispatches EVENT_MCP_ACTION notifications via NotificationHub for L2/L3 mutations
"""

from __future__ import annotations

from collections import deque
from datetime import datetime, timezone
import json
import os
import secrets
import stat
import threading
import time
from typing import Any, Dict, List, Optional, Tuple

from notifications import EVENT_MCP_ACTION, NotificationEvent

BASE_DIR = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
DATA_DIR = os.path.join(BASE_DIR, "data")
AUDIT_FILE = os.path.join(DATA_DIR, "mcp_audit.json")
MAX_AUDIT_ENTRIES = 300
QUOTE_TTL_SECONDS = 300  # 5 minutes

_audit_lock = threading.RLock()
_order_timestamps: deque = deque()
_quotes: Dict[str, Dict[str, Any]] = {}


def _write_secure_json(path: str, payload: Any) -> None:
    os.makedirs(os.path.dirname(path), exist_ok=True)
    tmp_path = f"{path}.tmp"
    fd = os.open(tmp_path, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, stat.S_IRUSR | stat.S_IWUSR)
    with os.fdopen(fd, "w", encoding="utf-8") as f:
        json.dump(payload, f, ensure_ascii=False, indent=2)
    os.chmod(tmp_path, stat.S_IRUSR | stat.S_IWUSR)
    os.replace(tmp_path, path)
    os.chmod(path, stat.S_IRUSR | stat.S_IWUSR)


def load_audit_logs(limit: int = 100) -> List[Dict[str, Any]]:
    with _audit_lock:
        if not os.path.exists(AUDIT_FILE):
            return []
        try:
            with open(AUDIT_FILE, "r", encoding="utf-8") as f:
                entries = json.load(f) or []
            if isinstance(entries, list):
                return entries[: max(1, min(limit, MAX_AUDIT_ENTRIES))]
        except Exception:
            pass
        return []


def clear_audit_logs() -> None:
    with _audit_lock:
        _write_secure_json(AUDIT_FILE, [])


def record_mcp_audit(
    tool_name: str,
    scope: str,
    arguments: Dict[str, Any],
    status: str,
    summary: str,
    client_ip: str = "unknown",
    notify_hub: Any = None,
    notify_enabled: bool = True,
) -> Dict[str, Any]:
    """Append an audit log entry and optionally notify NotificationHub on L2/L3 mutations."""
    entry = {
        "id": f"mcp_{int(time.time() * 1000)}_{secrets.token_hex(3)}",
        "timestamp": datetime.now(timezone.utc).isoformat(),
        "tool": tool_name,
        "scope": scope,
        "arguments": arguments or {},
        "status": status,
        "summary": summary,
        "clientIp": client_ip,
    }
    with _audit_lock:
        existing = load_audit_logs(limit=MAX_AUDIT_ENTRIES)
        updated = [entry] + existing[: MAX_AUDIT_ENTRIES - 1]
        try:
            _write_secure_json(AUDIT_FILE, updated)
        except Exception:
            pass

    if (
        notify_enabled
        and notify_hub is not None
        and scope in ("procurement", "operations")
        and status == "success"
    ):
        try:
            scope_label = "L2 免付采购/队列" if scope == "procurement" else "L3 运维操作"
            event = NotificationEvent(
                event_type=EVENT_MCP_ACTION,
                title=f"🤖 MCP AI 操作审计：{tool_name}",
                plain_text=(
                    f"AI 客户端通过 MCP 网关执行了 {scope_label} 操作：\n"
                    f"{summary}"
                ),
                fields=[
                    {"label": "MCP 工具", "value": tool_name},
                    {"label": "权限层级", "value": scope_label},
                    {"label": "请求参数", "value": json.dumps(arguments or {}, ensure_ascii=False)},
                    {"label": "来源 IP", "value": client_ip},
                ],
                badge_color="#0050D7",
            )
            notify_hub.dispatch(event)
        except Exception:
            pass

    return entry


# ==================== Two-Step Quote Store ====================

def _prune_quotes(now: float) -> None:
    expired = [qid for qid, q in _quotes.items() if now > q.get("expiresAt", 0)]
    for qid in expired:
        _quotes.pop(qid, None)


def create_order_quote(
    plan_code: str,
    datacenter: str,
    options: List[str],
    account_id: str,
    account_alias: str,
    price_data: Dict[str, Any],
) -> Dict[str, Any]:
    with _audit_lock:
        now = time.time()
        _prune_quotes(now)
        quote_id = f"q_{secrets.token_urlsafe(12)}"
        quote_obj = {
            "quoteId": quote_id,
            "planCode": plan_code,
            "datacenter": datacenter,
            "options": list(options or []),
            "accountId": account_id,
            "profile": account_alias,
            "price": price_data,
            "autoPay": False,
            "quantity": 1,
            "createdAt": now,
            "expiresAt": now + QUOTE_TTL_SECONDS,
            "ttlSeconds": QUOTE_TTL_SECONDS,
        }
        _quotes[quote_id] = quote_obj
        return quote_obj


def consume_order_quote(quote_id: str) -> Optional[Dict[str, Any]]:
    with _audit_lock:
        now = time.time()
        _prune_quotes(now)
        return _quotes.pop(str(quote_id or "").strip(), None)


def reset_quotes_and_rate_limits_for_test() -> None:
    with _audit_lock:
        _quotes.clear()
        _order_timestamps.clear()


# ==================== Hourly Unpaid Order Rate Limiter ====================

def check_order_rate_limit(hourly_limit: int) -> Tuple[bool, int]:
    """
    Check if another direct unpaid order is allowed within the rolling 1-hour window.
    Returns (allowed, remaining_quota).
    """
    with _audit_lock:
        now = time.time()
        while _order_timestamps and now - _order_timestamps[0] > 3600:
            _order_timestamps.popleft()
        used = len(_order_timestamps)
        if used >= max(1, hourly_limit):
            return False, 0
        return True, max(0, hourly_limit - used)


def record_order_execution() -> None:
    with _audit_lock:
        _order_timestamps.append(time.time())
