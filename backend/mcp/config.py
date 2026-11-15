"""
MCP Gateway Configuration & Token Management
============================================
Persists configuration in backend/data/mcp_config.json (0600 permissions).
"""

from __future__ import annotations

from copy import deepcopy
from datetime import datetime, timezone
import hmac
import json
import os
import secrets
import stat
import threading
from typing import Any, Dict, List, Optional

BASE_DIR = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
DATA_DIR = os.path.join(BASE_DIR, "data")
MCP_CONFIG_FILE = os.path.join(DATA_DIR, "mcp_config.json")

_config_lock = threading.RLock()

DEFAULT_MCP_CONFIG: Dict[str, Any] = {
    "enabled": False,
    "token": "",
    "scopes": {
        "read": True,
        "procurement": False,
        "operations": False,
    },
    "allowAllAccounts": True,
    "allowedAccountIds": [],
    "hourlyOrderLimit": 5,
    "notifyOnMutation": True,
    "createdAt": None,
    "updatedAt": None,
    "tokenRotatedAt": None,
}


def _now_iso() -> str:
    return datetime.now(timezone.utc).isoformat()


def _write_secure_json(path: str, payload: Any) -> None:
    os.makedirs(os.path.dirname(path), exist_ok=True)
    tmp_path = f"{path}.tmp"
    fd = os.open(tmp_path, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, stat.S_IRUSR | stat.S_IWUSR)
    with os.fdopen(fd, "w", encoding="utf-8") as f:
        json.dump(payload, f, ensure_ascii=False, indent=2)
    os.chmod(tmp_path, stat.S_IRUSR | stat.S_IWUSR)
    os.replace(tmp_path, path)
    os.chmod(path, stat.S_IRUSR | stat.S_IWUSR)


def mask_mcp_token(token: str) -> str:
    if not token:
        return ""
    s = str(token).strip()
    if len(s) <= 14:
        return "****"
    return f"{s[:10]}****{s[-4:]}"


def load_mcp_config() -> Dict[str, Any]:
    with _config_lock:
        cfg = deepcopy(DEFAULT_MCP_CONFIG)
        if os.path.exists(MCP_CONFIG_FILE):
            try:
                with open(MCP_CONFIG_FILE, "r", encoding="utf-8") as f:
                    raw = json.load(f) or {}
                if isinstance(raw, dict):
                    cfg["enabled"] = bool(raw.get("enabled", False))
                    cfg["token"] = str(raw.get("token") or "").strip()
                    raw_scopes = raw.get("scopes") or {}
                    if isinstance(raw_scopes, dict):
                        cfg["scopes"] = {
                            "read": bool(raw_scopes.get("read", True)),
                            "procurement": bool(raw_scopes.get("procurement", False)),
                            "operations": bool(raw_scopes.get("operations", False)),
                        }
                    cfg["allowAllAccounts"] = bool(raw.get("allowAllAccounts", True))
                    allowed_ids = raw.get("allowedAccountIds")
                    if isinstance(allowed_ids, list):
                        cfg["allowedAccountIds"] = [str(x) for x in allowed_ids if x]
                    try:
                        limit = int(raw.get("hourlyOrderLimit", 5))
                        cfg["hourlyOrderLimit"] = min(max(limit, 1), 20)
                    except Exception:
                        cfg["hourlyOrderLimit"] = 5
                    cfg["notifyOnMutation"] = bool(raw.get("notifyOnMutation", True))
                    cfg["createdAt"] = raw.get("createdAt")
                    cfg["updatedAt"] = raw.get("updatedAt")
                    cfg["tokenRotatedAt"] = raw.get("tokenRotatedAt")
            except Exception:
                pass
        return cfg


def save_mcp_config(cfg: Dict[str, Any]) -> Dict[str, Any]:
    with _config_lock:
        now = _now_iso()
        if not cfg.get("createdAt"):
            cfg["createdAt"] = now
        cfg["updatedAt"] = now
        _write_secure_json(MCP_CONFIG_FILE, cfg)
        return cfg


def get_public_mcp_config() -> Dict[str, Any]:
    cfg = load_mcp_config()
    token = cfg.get("token") or ""
    return {
        "enabled": bool(cfg.get("enabled") and bool(token)),
        "hasToken": bool(token),
        "maskedToken": mask_mcp_token(token),
        "scopes": deepcopy(cfg.get("scopes") or DEFAULT_MCP_CONFIG["scopes"]),
        "allowAllAccounts": bool(cfg.get("allowAllAccounts", True)),
        "allowedAccountIds": list(cfg.get("allowedAccountIds") or []),
        "hourlyOrderLimit": int(cfg.get("hourlyOrderLimit", 5)),
        "notifyOnMutation": bool(cfg.get("notifyOnMutation", True)),
        "tokenRotatedAt": cfg.get("tokenRotatedAt"),
        "updatedAt": cfg.get("updatedAt"),
        "hardBlocked": [
            "auto_pay",
            "baremetal_reinstall",
            "vps_reinstall",
            "service_terminate",
            "ipmi_raw_console",
            "credentials_mutation",
        ],
    }


def update_mcp_config(patch: Dict[str, Any]) -> Dict[str, Any]:
    with _config_lock:
        cfg = load_mcp_config()
        if "enabled" in patch:
            want_enable = bool(patch["enabled"])
            if want_enable and not cfg.get("token"):
                # Automatically generate initial token when enabling for the first time
                cfg["token"] = f"nxs_mcp_{secrets.token_urlsafe(32)}"
                cfg["tokenRotatedAt"] = _now_iso()
            cfg["enabled"] = want_enable

        if "scopes" in patch and isinstance(patch["scopes"], dict):
            s = patch["scopes"]
            cfg["scopes"] = {
                "read": bool(s.get("read", cfg["scopes"].get("read", True))),
                "procurement": bool(s.get("procurement", cfg["scopes"].get("procurement", False))),
                "operations": bool(s.get("operations", cfg["scopes"].get("operations", False))),
            }

        if "allowAllAccounts" in patch:
            cfg["allowAllAccounts"] = bool(patch["allowAllAccounts"])

        if "allowedAccountIds" in patch and isinstance(patch["allowedAccountIds"], list):
            cfg["allowedAccountIds"] = [str(x).strip() for x in patch["allowedAccountIds"] if str(x).strip()]

        if "hourlyOrderLimit" in patch:
            try:
                cfg["hourlyOrderLimit"] = min(max(int(patch["hourlyOrderLimit"]), 1), 20)
            except Exception:
                pass

        if "notifyOnMutation" in patch:
            cfg["notifyOnMutation"] = bool(patch["notifyOnMutation"])

        save_mcp_config(cfg)
        return cfg


def rotate_mcp_token() -> str:
    with _config_lock:
        cfg = load_mcp_config()
        new_token = f"nxs_mcp_{secrets.token_urlsafe(32)}"
        cfg["token"] = new_token
        cfg["tokenRotatedAt"] = _now_iso()
        save_mcp_config(cfg)
        return new_token


def revoke_mcp_token() -> Dict[str, Any]:
    with _config_lock:
        cfg = load_mcp_config()
        cfg["token"] = ""
        cfg["enabled"] = False
        cfg["tokenRotatedAt"] = _now_iso()
        save_mcp_config(cfg)
        return cfg


def verify_mcp_token(candidate: Optional[str]) -> bool:
    if not candidate:
        return False
    cfg = load_mcp_config()
    if not cfg.get("enabled"):
        return False
    stored = (cfg.get("token") or "").strip()
    if not stored or len(stored) < 20:
        return False
    clean_candidate = str(candidate).strip()
    if clean_candidate.lower().startswith("bearer "):
        clean_candidate = clean_candidate[7:].strip()
    return hmac.compare_digest(clean_candidate.encode("utf-8"), stored.encode("utf-8"))


def get_whitelisted_account_ids(
    accounts_dict: Dict[str, Dict[str, Any]],
    default_account_id: Optional[str] = None,
    legacy_config: Optional[Dict[str, Any]] = None,
) -> List[str]:
    """
    Return the ordered list of account IDs that MCP is permitted to access.
    If no multi-account entries exist but legacy_config has credentials, uses 'default'.
    """
    cfg = load_mcp_config()
    all_ids = list(accounts_dict.keys())
    if not all_ids and legacy_config and legacy_config.get("appKey") and legacy_config.get("consumerKey"):
        all_ids = ["default"]

    # Put default_account_id first if present
    if default_account_id and default_account_id in all_ids:
        all_ids = [default_account_id] + [aid for aid in all_ids if aid != default_account_id]

    if cfg.get("allowAllAccounts", True):
        return all_ids

    allowed_set = set(cfg.get("allowedAccountIds") or [])
    return [aid for aid in all_ids if aid in allowed_set]
