"""
MCP Gateway Configuration & Token Management
============================================
- Persists configuration in `backend/data/mcp_config.json` (`0600` permissions).
- Stores MCP Bearer Token strictly as an irreversible SHA-256 digest (`tokenHash`)
  plus a redacted display preview (`maskedToken`). Plaintext `nxs_mcp_...` tokens
  are never written to disk (and any legacy v1.2.0 plaintext `token` field is
  automatically migrated to `tokenHash` and wiped on startup).
- Enforces 2FA linkage: when panel TOTP 2FA is not enabled, the `operations` (L3)
  scope is forcibly locked to `False` at runtime and cannot be enabled.
"""

from __future__ import annotations

from copy import deepcopy
from datetime import datetime, timezone
import json
import os
import secrets
import stat
import threading
from typing import Any, Dict, List, Optional

from crypto_vault import hash_secret_token, verify_secret_token_hash
from totp_service import is_totp_enabled

BASE_DIR = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
DATA_DIR = os.path.join(BASE_DIR, "data")
MCP_CONFIG_FILE = os.path.join(DATA_DIR, "mcp_config.json")

_config_lock = threading.RLock()

DEFAULT_MCP_CONFIG: Dict[str, Any] = {
    "enabled": False,
    "tokenHash": "",
    "maskedToken": "",
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
        needs_migration = False
        if os.path.exists(MCP_CONFIG_FILE):
            try:
                with open(MCP_CONFIG_FILE, "r", encoding="utf-8") as f:
                    raw = json.load(f) or {}
                if isinstance(raw, dict):
                    cfg["enabled"] = bool(raw.get("enabled", False))

                    # Migrate legacy v1.2.0 plaintext `token` field to irreversible SHA-256 `tokenHash`
                    legacy_plain_token = str(raw.get("token") or "").strip()
                    stored_hash = str(raw.get("tokenHash") or "").strip()
                    stored_mask = str(raw.get("maskedToken") or "").strip()
                    if legacy_plain_token:
                        stored_hash = hash_secret_token(legacy_plain_token)
                        stored_mask = mask_mcp_token(legacy_plain_token)
                        needs_migration = True
                    elif "token" in raw:
                        needs_migration = True

                    cfg["tokenHash"] = stored_hash
                    cfg["maskedToken"] = stored_mask if stored_hash else ""

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

                    if needs_migration:
                        save_mcp_config(cfg)
            except Exception:
                pass

        # Runtime security guardrail: L3 `operations` is forcibly disabled if 2FA is not enabled
        if not is_totp_enabled():
            cfg["scopes"]["operations"] = False

        return cfg


def save_mcp_config(cfg: Dict[str, Any]) -> Dict[str, Any]:
    with _config_lock:
        now = _now_iso()
        if not cfg.get("createdAt"):
            cfg["createdAt"] = now
        cfg["updatedAt"] = now

        # Ensure plaintext `token` is never persisted to disk
        disk_cfg = deepcopy(cfg)
        plain_tok = str(disk_cfg.pop("token", "") or "").strip()
        disk_cfg.pop("_newlyGeneratedPlaintextToken", None)
        if plain_tok:
            disk_cfg["tokenHash"] = hash_secret_token(plain_tok)
            disk_cfg["maskedToken"] = mask_mcp_token(plain_tok)

        _write_secure_json(MCP_CONFIG_FILE, disk_cfg)
        return cfg


def get_public_mcp_config() -> Dict[str, Any]:
    cfg = load_mcp_config()
    has_token = bool(cfg.get("tokenHash"))
    totp_active = is_totp_enabled()
    return {
        "enabled": bool(cfg.get("enabled") and has_token),
        "hasToken": has_token,
        "maskedToken": cfg.get("maskedToken") or "",
        "scopes": deepcopy(cfg.get("scopes") or DEFAULT_MCP_CONFIG["scopes"]),
        "totpEnabled": totp_active,
        "operationsLockedByTotp": not totp_active,
        "allowAllAccounts": bool(cfg.get("allowAllAccounts", True)),
        "allowedAccountIds": list(cfg.get("allowedAccountIds") or []),
        "hourlyOrderLimit": int(cfg.get("hourlyOrderLimit", 5)),
        "notifyOnMutation": bool(cfg.get("notifyOnMutation", True)),
        "tokenRotatedAt": cfg.get("tokenRotatedAt"),
        "updatedAt": cfg.get("updatedAt"),
        "hardBlocked": [
            "auto_pay",
            "waive_retractation_period",
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
        newly_minted_token: Optional[str] = None
        if "enabled" in patch:
            want_enable = bool(patch["enabled"])
            if want_enable and not cfg.get("tokenHash"):
                # Automatically generate initial token when enabling for the first time
                newly_minted_token = f"nxs_mcp_{secrets.token_urlsafe(32)}"
                cfg["tokenHash"] = hash_secret_token(newly_minted_token)
                cfg["maskedToken"] = mask_mcp_token(newly_minted_token)
                cfg["tokenRotatedAt"] = _now_iso()
            cfg["enabled"] = want_enable

        if "scopes" in patch and isinstance(patch["scopes"], dict):
            s = patch["scopes"]
            want_ops = bool(s.get("operations", cfg["scopes"].get("operations", False)))
            if want_ops and not is_totp_enabled():
                want_ops = False
            cfg["scopes"] = {
                "read": bool(s.get("read", cfg["scopes"].get("read", True))),
                "procurement": bool(s.get("procurement", cfg["scopes"].get("procurement", False))),
                "operations": want_ops,
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
        if newly_minted_token:
            cfg["_newlyGeneratedPlaintextToken"] = newly_minted_token
        return cfg


def rotate_mcp_token() -> str:
    with _config_lock:
        cfg = load_mcp_config()
        new_token = f"nxs_mcp_{secrets.token_urlsafe(32)}"
        cfg["tokenHash"] = hash_secret_token(new_token)
        cfg["maskedToken"] = mask_mcp_token(new_token)
        cfg["tokenRotatedAt"] = _now_iso()
        save_mcp_config(cfg)
        return new_token


def revoke_mcp_token() -> Dict[str, Any]:
    with _config_lock:
        cfg = load_mcp_config()
        cfg["tokenHash"] = ""
        cfg["maskedToken"] = ""
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
    stored_hash = (cfg.get("tokenHash") or "").strip()
    if not stored_hash:
        return False
    clean_candidate = str(candidate).strip()
    if clean_candidate.lower().startswith("bearer "):
        clean_candidate = clean_candidate[7:].strip()
    if len(clean_candidate) < 20 or not clean_candidate.startswith("nxs_mcp_"):
        return False
    return verify_secret_token_hash(clean_candidate, stored_hash)


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
