"""
MCP Privacy Shield — Bidirectional Asset Alias & PII Redaction Layer
====================================================================
Implements Balanced Mode (平衡模式) obfuscation:
  1. Assigns deterministic, persistent aliases in backend/data/mcp_aliases.json (0600):
       - OVH Accounts      -> acct-01, acct-02, ...
       - Bare-Metal Servers-> bm-01, bm-02, ...
       - VPS Instances     -> vps-01, vps-02, ...
       - Public IPv4/IPv6  -> ip-01, ip-02, ...
       - Purchase Orders   -> ord-01, ord-02, ...
  2. Preserves safe human hints:
       - Account user-defined alias/label + region/subsidiary
       - Server custom displayName ONLY if it does not match OVH default hostname patterns
         (ns*.ip-*, vps-*, sd-*, raw IPv4/IPv6, or email)
       - Masked IP tail hint: *.*.*.42 (IPv4) or *::xxxx (IPv6)
  3. Unconditionally strips:
       - All credentials (appKey, appSecret, consumerKey, tgToken, SMTP passwords, SSH keys)
       - Account PII (NIC-handle, legal name, email, phone, address, VAT)
       - Order payment/PDF URLs containing unauthenticated orderPassword tokens
"""

from __future__ import annotations

import json
import os
import re
import stat
import threading
from typing import Any, Dict, List, Optional, Tuple

BASE_DIR = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
DATA_DIR = os.path.join(BASE_DIR, "data")
ALIASES_FILE = os.path.join(DATA_DIR, "mcp_aliases.json")

_alias_lock = threading.RLock()

CATEGORY_PREFIX = {
    "account": "acct",
    "baremetal": "bm",
    "vps": "vps",
    "ip": "ip",
    "order": "ord",
}

# Regex patterns for sensitive OVH identifiers and PII
_IPV4_RE = re.compile(r"\b(\d{1,3})\.(\d{1,3})\.(\d{1,3})\.(\d{1,3})(?:/\d{1,2})?\b")
_IPV6_RE = re.compile(r"\b(?:[0-9a-fA-F]{1,4}:){2,7}[0-9a-fA-F]{0,4}(?:/\d{1,3})?\b")
_OVH_BM_HOST_RE = re.compile(
    r"\b(?:ns\d+\.ip-[\d-]+(?:\.[a-z0-9-]+)+|ip-\d+-\d+-\d+(?:\.[a-z0-9-]+)*|sd-\d+(?:\.[a-z0-9-]+)*|ks\d+(?:\.[a-z0-9-]+)*)\b",
    re.IGNORECASE,
)
_OVH_VPS_HOST_RE = re.compile(
    r"\b(?:vps-[a-z0-9-]+\.vps\.ovh\.[a-z.]+|vps\d+\.ovh\.[a-z.]+)\b",
    re.IGNORECASE,
)
_EMAIL_RE = re.compile(r"\b[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}\b")
_NIC_HANDLE_RE = re.compile(r"\b[a-z]{2}\d{1,8}-ovh\b", re.IGNORECASE)
_ORDER_URL_RE = re.compile(r"https?://[^\s\"'<>]+(?:orderId|orderPassword|displayOrder)[^\s\"'<>]*", re.IGNORECASE)


def _write_secure_json(path: str, payload: Any) -> None:
    os.makedirs(os.path.dirname(path), exist_ok=True)
    tmp_path = f"{path}.tmp"
    fd = os.open(tmp_path, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, stat.S_IRUSR | stat.S_IWUSR)
    with os.fdopen(fd, "w", encoding="utf-8") as f:
        json.dump(payload, f, ensure_ascii=False, indent=2)
    os.chmod(tmp_path, stat.S_IRUSR | stat.S_IWUSR)
    os.replace(tmp_path, path)
    os.chmod(path, stat.S_IRUSR | stat.S_IWUSR)


def format_ip_hint(raw_ip: str) -> str:
    """Return masked IP tail hint such as '*.*.*.42' or '*::1'."""
    if not raw_ip:
        return ""
    clean = str(raw_ip).strip()
    addr_only = clean.split("/")[0].strip()
    m4 = _IPV4_RE.fullmatch(addr_only)
    if m4:
        return f"*.*.*.{m4.group(4)}"
    if ":" in addr_only:
        parts = [p for p in addr_only.split(":") if p]
        tail = parts[-1] if parts else "0"
        return f"*::{tail}"
    return "***"


def sanitize_custom_label(raw_label: Optional[str], real_service_name: str = "") -> Optional[str]:
    """
    Preserve user-defined custom server/VPS display names (e.g. 'Prod-EU-Node1'),
    but return None if the label is equal to the raw serviceName or contains
    OVH default hostnames (ns*.ip-*, vps-*), raw IPs, emails, or NIC handles.
    """
    if not raw_label:
        return None
    label = str(raw_label).strip()
    if not label:
        return None
    if real_service_name and label.lower() == str(real_service_name).strip().lower():
        return None
    if _OVH_BM_HOST_RE.search(label) or _OVH_VPS_HOST_RE.search(label):
        return None
    if _IPV4_RE.search(label) or _IPV6_RE.search(label):
        return None
    if _EMAIL_RE.search(label) or _NIC_HANDLE_RE.search(label):
        return None
    return label[:64]


class PrivacyShield:
    """Thread-safe bidirectional alias registry and data redactor."""

    def __init__(self, storage_path: str = ALIASES_FILE):
        self._storage_path = storage_path
        self._lock = _alias_lock
        self._state = self._load()

    def _empty_state(self) -> Dict[str, Any]:
        return {
            "forward": {k: {} for k in CATEGORY_PREFIX},
            "reverse": {k: {} for k in CATEGORY_PREFIX},
            "meta": {k: {} for k in CATEGORY_PREFIX},
            "counters": {k: 0 for k in CATEGORY_PREFIX},
        }

    def _load(self) -> Dict[str, Any]:
        with self._lock:
            state = self._empty_state()
            if os.path.exists(self._storage_path):
                try:
                    with open(self._storage_path, "r", encoding="utf-8") as f:
                        raw = json.load(f) or {}
                    for cat in CATEGORY_PREFIX:
                        fwd = (raw.get("forward") or {}).get(cat) or {}
                        rev = (raw.get("reverse") or {}).get(cat) or {}
                        meta = (raw.get("meta") or {}).get(cat) or {}
                        cnt = int((raw.get("counters") or {}).get(cat) or 0)
                        state["forward"][cat] = {str(k): str(v) for k, v in fwd.items()}
                        state["reverse"][cat] = {str(k): str(v) for k, v in rev.items()}
                        state["meta"][cat] = meta if isinstance(meta, dict) else {}
                        state["counters"][cat] = max(cnt, len(fwd))
                except Exception:
                    pass
            return state

    def _save(self) -> None:
        _write_secure_json(self._storage_path, self._state)

    def reload(self) -> None:
        self._state = self._load()

    def clear_all(self) -> None:
        with self._lock:
            self._state = self._empty_state()
            self._save()

    def register(self, category: str, real_value: Any, meta: Optional[Dict[str, Any]] = None) -> str:
        """Register or lookup the alias (e.g. 'bm-01') for a real resource identifier."""
        if category not in CATEGORY_PREFIX:
            raise ValueError(f"Unsupported alias category: {category}")
        raw_str = str(real_value or "").strip()
        if not raw_str:
            return ""
        if category == "ip":
            raw_str = raw_str.split("/")[0].strip()

        with self._lock:
            fwd = self._state["forward"][category]
            rev = self._state["reverse"][category]
            meta_map = self._state["meta"][category]

            alias = fwd.get(raw_str)
            if not alias:
                prefix = CATEGORY_PREFIX[category]
                next_num = int(self._state["counters"].get(category, 0)) + 1
                while True:
                    candidate = f"{prefix}-{next_num:02d}"
                    if candidate not in rev:
                        alias = candidate
                        break
                    next_num += 1
                self._state["counters"][category] = next_num
                fwd[raw_str] = alias
                rev[alias] = raw_str

            if meta and isinstance(meta, dict):
                existing = meta_map.get(alias) or {}
                existing.update({k: v for k, v in meta.items() if v is not None})
                meta_map[alias] = existing

            self._save()
            return alias

    def resolve(self, category: str, alias_or_hint: str) -> Optional[str]:
        """Resolve an alias ('bm-01', 'acct-01', or custom label) back to the real identifier."""
        if category not in CATEGORY_PREFIX or not alias_or_hint:
            return None
        key = str(alias_or_hint).strip()
        if not key:
            return None

        with self._lock:
            rev = self._state["reverse"][category]
            # 1. Exact alias match (case-insensitive)
            for alias_k, real_v in rev.items():
                if alias_k.lower() == key.lower():
                    return real_v

            # 2. Match by registered safe label in meta
            meta_map = self._state["meta"][category]
            for alias_k, m in meta_map.items():
                label = str((m or {}).get("label") or "").strip()
                if label and label.lower() == key.lower() and alias_k in rev:
                    return rev[alias_k]

            return None

    def get_meta(self, category: str, alias: str) -> Dict[str, Any]:
        with self._lock:
            return dict((self._state["meta"].get(category) or {}).get(alias) or {})

    def list_mappings_for_ui(self) -> List[Dict[str, Any]]:
        """
        Return alias mappings for the WebUI MCP Gateway reference table.
        Includes both alias, safe hint, and the real identifier (for authenticated panel owner).
        """
        with self._lock:
            items: List[Dict[str, Any]] = []
            for cat in ("account", "baremetal", "vps", "ip", "order"):
                rev = self._state["reverse"].get(cat) or {}
                meta_map = self._state["meta"].get(cat) or {}
                for alias in sorted(rev.keys()):
                    real_val = rev[alias]
                    m = meta_map.get(alias) or {}
                    masked_real = real_val
                    if cat == "ip":
                        masked_real = format_ip_hint(real_val)
                    elif len(real_val) > 10:
                        masked_real = f"{real_val[:4]}****{real_val[-4:]}"
                    items.append({
                        "category": cat,
                        "alias": alias,
                        "label": m.get("label") or "",
                        "hint": m.get("hint") or "",
                        "accountAlias": m.get("accountAlias") or "",
                        "maskedReal": masked_real,
                        "realValue": real_val,
                    })
            return items

    # ==================== High-Level Entity Redactors ====================

    def redact_account(self, account_id: str, account_obj: Dict[str, Any], is_default: bool = False) -> Dict[str, Any]:
        raw_alias = str(account_obj.get("alias") or "").strip()
        safe_label = sanitize_custom_label(raw_alias) or f"OVH-{str(account_obj.get('zone') or 'EU').upper()}"
        endpoint = str(account_obj.get("endpoint") or "ovh-eu")
        zone = str(account_obj.get("zone") or "IE")
        alias = self.register(
            "account",
            account_id,
            meta={"label": safe_label, "hint": f"{endpoint} ({zone})"},
        )
        return {
            "profile": alias,
            "label": safe_label,
            "endpoint": endpoint,
            "zone": zone,
            "isDefault": bool(is_default),
        }

    def redact_baremetal(
        self,
        raw_srv: Dict[str, Any],
        account_alias: str = "",
        account_id: str = "",
    ) -> Dict[str, Any]:
        service_name = str(raw_srv.get("serviceName") or raw_srv.get("name") or "").strip()
        raw_display = raw_srv.get("displayName") or raw_srv.get("reverse") or raw_srv.get("name") or ""
        safe_label = sanitize_custom_label(raw_display, real_service_name=service_name)
        dc = str(raw_srv.get("datacenter") or "N/A").upper()
        model = str(raw_srv.get("commercialRange") or raw_srv.get("model") or "baremetal")

        raw_ip = str(raw_srv.get("ip") or "").strip()
        ip_alias = ""
        ip_hint = ""
        if raw_ip and raw_ip.upper() != "N/A":
            ip_hint = format_ip_hint(raw_ip)
            ip_alias = self.register(
                "ip",
                raw_ip,
                meta={"label": safe_label or model, "hint": ip_hint, "accountAlias": account_alias},
            )

        bm_alias = self.register(
            "baremetal",
            service_name,
            meta={
                "label": safe_label or model,
                "hint": f"{model} @ {dc}" + (f" ({ip_hint})" if ip_hint else ""),
                "accountAlias": account_alias,
                "accountId": account_id,
            },
        )

        out: Dict[str, Any] = {
            "server": bm_alias,
            "label": safe_label,
            "profile": account_alias or None,
            "commercialModel": model,
            "datacenter": dc,
            "state": raw_srv.get("state", "unknown"),
            "status": raw_srv.get("status", "ok"),
            "os": raw_srv.get("os", "N/A"),
            "monitoring": bool(raw_srv.get("monitoring", False)),
            "ip": {
                "alias": ip_alias or None,
                "maskedTail": ip_hint or None,
            },
        }
        if "daysRemaining" in raw_srv:
            out["daysRemaining"] = raw_srv.get("daysRemaining")
        if "expiration" in raw_srv:
            out["expiration"] = raw_srv.get("expiration")
        if "renewMode" in raw_srv:
            out["renewMode"] = raw_srv.get("renewMode")
        return out

    def redact_vps(
        self,
        raw_vps: Dict[str, Any],
        account_alias: str = "",
        account_id: str = "",
    ) -> Dict[str, Any]:
        service_name = str(raw_vps.get("serviceName") or "").strip()
        raw_display = raw_vps.get("displayName") or ""
        safe_label = sanitize_custom_label(raw_display, real_service_name=service_name)
        zone = str(raw_vps.get("zone") or "N/A")
        model = str(raw_vps.get("modelName") or raw_vps.get("model") or "VPS")

        raw_ips = raw_vps.get("ips") or []
        redacted_ips = []
        first_ip_hint = ""
        for raw_ip in raw_ips:
            ip_str = str(raw_ip).strip()
            if not ip_str:
                continue
            hint = format_ip_hint(ip_str)
            if not first_ip_hint and ":" not in ip_str:
                first_ip_hint = hint
            ip_alias = self.register(
                "ip",
                ip_str,
                meta={"label": safe_label or model, "hint": hint, "accountAlias": account_alias},
            )
            redacted_ips.append({"alias": ip_alias, "maskedTail": hint})

        vps_alias = self.register(
            "vps",
            service_name,
            meta={
                "label": safe_label or model,
                "hint": f"{model} @ {zone}" + (f" ({first_ip_hint})" if first_ip_hint else ""),
                "accountAlias": account_alias,
                "accountId": account_id,
            },
        )

        return {
            "vps": vps_alias,
            "label": safe_label,
            "profile": account_alias or None,
            "model": model,
            "zone": zone,
            "state": raw_vps.get("state", "unknown"),
            "vcore": raw_vps.get("vcore"),
            "memoryMb": raw_vps.get("memoryLimitMb") or raw_vps.get("memoryLimit"),
            "diskGb": raw_vps.get("diskGb") or raw_vps.get("disk"),
            "ips": redacted_ips,
            "expiration": raw_vps.get("expiration"),
            "daysRemaining": raw_vps.get("daysRemaining"),
            "renewMode": raw_vps.get("renewMode"),
        }

    def redact_order(self, raw_order: Dict[str, Any], account_alias: str = "") -> Dict[str, Any]:
        raw_oid = str(raw_order.get("orderId") or "").strip()
        ord_alias = self.register(
            "order",
            raw_oid,
            meta={
                "label": f"Order ({raw_order.get('status') or 'unknown'})",
                "hint": str(raw_order.get("date") or "")[:10],
                "accountAlias": account_alias,
            },
        ) if raw_oid else ""

        # Explicitly strip url / pdfUrl (which contain orderPassword query tokens!)
        follow_up = raw_order.get("followUp") or []
        clean_follow_up = []
        if isinstance(follow_up, list):
            for step in follow_up:
                if not isinstance(step, dict):
                    continue
                history = []
                for h in step.get("history") or []:
                    if isinstance(h, dict):
                        history.append({
                            "date": h.get("date"),
                            "label": h.get("label"),
                            "description": self.scrub_text(str(h.get("description") or "")),
                        })
                clean_follow_up.append({
                    "step": step.get("step"),
                    "status": step.get("status"),
                    "history": history,
                })

        return {
            "order": ord_alias or None,
            "profile": account_alias or None,
            "status": raw_order.get("status") or raw_order.get("orderStatus") or "unknown",
            "date": raw_order.get("date"),
            "expirationDate": raw_order.get("expirationDate"),
            "priceWithTax": raw_order.get("priceWithTax"),
            "priceWithoutTax": raw_order.get("priceWithoutTax"),
            "tax": raw_order.get("tax"),
            "followUp": clean_follow_up,
            "paymentLinkNotice": "Payment & invoice URLs are redacted from AI context for security. View or pay in the OVH Nexus Web Console [Orders & Delivery] tab or via your Telegram/Email notification.",
        }

    def scrub_text(self, text: str) -> str:
        """Scrub any raw hostnames, IPs, emails, NIC handles, or order URLs from free-form text."""
        if not text:
            return ""
        out = str(text)
        with self._lock:
            for cat in ("baremetal", "vps", "ip", "order", "account"):
                for real_val, alias in self._state["forward"].get(cat, {}).items():
                    if real_val and len(real_val) >= 4 and real_val in out:
                        out = out.replace(real_val, alias)

        out = _ORDER_URL_RE.sub("[REDACTED_ORDER_URL]", out)
        out = _OVH_BM_HOST_RE.sub("[REDACTED_BM_HOST]", out)
        out = _OVH_VPS_HOST_RE.sub("[REDACTED_VPS_HOST]", out)
        out = _EMAIL_RE.sub("[REDACTED_EMAIL]", out)
        out = _NIC_HANDLE_RE.sub("[REDACTED_NIC]", out)
        out = _IPV4_RE.sub(lambda m: f"*.*.*.{m.group(4)}", out)
        out = _IPV6_RE.sub(lambda m: format_ip_hint(m.group(0)), out)
        return out


privacy_shield = PrivacyShield()
