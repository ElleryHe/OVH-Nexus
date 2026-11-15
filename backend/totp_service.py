"""
RFC 6238 TOTP Two-Factor Authentication (2FA) & Tiered Sudo Gatekeeper
======================================================================
- Implements standard RFC 6238 TOTP (SHA-1, 6 digits, 30s period, ±1 window)
  using Python's standard library + `crypto_vault` AES-256-GCM at-rest encryption.
- Generates 8 single-use emergency Recovery Codes (`XXXX-XXXX`), storing only
  their SHA-256 digests on disk.
- Enforces anti-replay tracking (`lastUsedStep`) so a 6-digit code cannot be
  replayed within the same 30-second window.
- Classifies all OVH Nexus API routes into:
    * `strict` — Irreversible / credential / security-downgrade operations
      that require a fresh TOTP code on every single invocation.
    * `sudo`   — Reversible Day-2 server/network operations, auto-pay toggles,
      and MCP governance that grant a 5-minute elevated session window (`300s`).
    * `None`   — Normal read / monitoring / unpaid-queue operations.
- When 2FA is not yet bound/enabled, all `strict` and `sudo` operations are
  unconditionally blocked with HTTP 403 (`TOTP_SETUP_REQUIRED`).
"""

from __future__ import annotations

import base64
from copy import deepcopy
from datetime import datetime, timezone
import hashlib
import hmac
import json
import os
import re
import secrets
import stat
import struct
import threading
import time
import urllib.parse
from typing import Any, Dict, List, Optional, Tuple

from crypto_vault import (
    decrypt_value,
    encrypt_value,
    hash_secret_token,
    verify_secret_token_hash,
)

BASE_DIR = os.path.dirname(os.path.abspath(__file__))
DATA_DIR = os.path.join(BASE_DIR, "data")
TOTP_CONFIG_FILE = os.path.join(DATA_DIR, "totp_config.json")

TOTP_DIGITS = 6
TOTP_PERIOD = 30
TOTP_WINDOW = 1
SUDO_DURATION_SECONDS = 300  # 5-minute elevated window for `sudo` tier ops
RECOVERY_CODE_COUNT = 8
RECOVERY_ALPHABET = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"

_totp_lock = threading.RLock()

# Rate limiting for TOTP verification failures (per IP / session)
TOTP_MAX_FAILURES = 6
TOTP_LOCKOUT_SECONDS = 300
_totp_failures: Dict[str, List[float]] = {}
_totp_failures_lock = threading.Lock()

DEFAULT_TOTP_STATE: Dict[str, Any] = {
    "enabled": False,
    "secret": "",
    "pendingSecret": "",
    "recoveryCodeHashes": [],
    "lastUsedStep": 0,
    "enabledAt": None,
    "updatedAt": None,
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


def load_totp_state() -> Dict[str, Any]:
    """Load TOTP configuration from disk and decrypt secrets into memory."""
    with _totp_lock:
        state = deepcopy(DEFAULT_TOTP_STATE)
        if not os.path.exists(TOTP_CONFIG_FILE):
            return state
        try:
            with open(TOTP_CONFIG_FILE, "r", encoding="utf-8") as f:
                raw = json.load(f) or {}
            if not isinstance(raw, dict):
                return state
            needs_migration = False
            secret_pt, was_plain_sec = decrypt_value(raw.get("secret") or "")
            pending_pt, was_plain_pend = decrypt_value(raw.get("pendingSecret") or "")
            if was_plain_sec or was_plain_pend:
                needs_migration = True

            state["enabled"] = bool(raw.get("enabled", False) and bool(secret_pt))
            state["secret"] = secret_pt
            state["pendingSecret"] = pending_pt
            hashes = raw.get("recoveryCodeHashes")
            state["recoveryCodeHashes"] = (
                [str(x) for x in hashes if x] if isinstance(hashes, list) else []
            )
            try:
                state["lastUsedStep"] = int(raw.get("lastUsedStep") or 0)
            except Exception:
                state["lastUsedStep"] = 0
            state["enabledAt"] = raw.get("enabledAt")
            state["updatedAt"] = raw.get("updatedAt")

            if needs_migration:
                save_totp_state(state)
        except Exception:
            return deepcopy(DEFAULT_TOTP_STATE)
        return state


def save_totp_state(state: Dict[str, Any]) -> Dict[str, Any]:
    """Encrypt TOTP secrets and persist state to `data/totp_config.json`."""
    with _totp_lock:
        state["updatedAt"] = _now_iso()
        disk_payload = {
            "enabled": bool(state.get("enabled", False) and bool(state.get("secret"))),
            "secret": encrypt_value(state.get("secret") or ""),
            "pendingSecret": encrypt_value(state.get("pendingSecret") or ""),
            "recoveryCodeHashes": list(state.get("recoveryCodeHashes") or []),
            "lastUsedStep": int(state.get("lastUsedStep") or 0),
            "enabledAt": state.get("enabledAt"),
            "updatedAt": state["updatedAt"],
        }
        _write_secure_json(TOTP_CONFIG_FILE, disk_payload)
        return state


def is_totp_enabled() -> bool:
    """Return True if 2FA TOTP is actively bound and enabled."""
    st = load_totp_state()
    return bool(st.get("enabled") and st.get("secret"))


def generate_totp_secret() -> str:
    """Generate a 160-bit (32-char Base32) TOTP secret."""
    raw = secrets.token_bytes(20)
    return base64.b32encode(raw).decode("ascii").rstrip("=")


def build_otpauth_uri(
    secret: str,
    account_name: str = "Admin",
    issuer: str = "OVH Nexus",
) -> str:
    """Build standard `otpauth://totp/...` URI for QR code scanners."""
    label = f"{issuer}:{account_name}"
    params = urllib.parse.urlencode(
        {
            "secret": secret,
            "issuer": issuer,
            "algorithm": "SHA1",
            "digits": str(TOTP_DIGITS),
            "period": str(TOTP_PERIOD),
        }
    )
    return f"otpauth://totp/{urllib.parse.quote(label)}?{params}"


def _decode_base32_secret(secret: str) -> bytes:
    cleaned = re.sub(r"[\s\-]+", "", (secret or "")).upper()
    if not cleaned:
        raise ValueError("Empty TOTP secret")
    padding = (8 - (len(cleaned) % 8)) % 8
    return base64.b32decode(cleaned + ("=" * padding), casefold=True)


def compute_totp_at_step(secret: str, step: int) -> str:
    """Compute 6-digit RFC 6238 TOTP code for a given 30-second time step."""
    key = _decode_base32_secret(secret)
    msg = struct.pack(">Q", int(step))
    digest = hmac.new(key, msg, hashlib.sha1).digest()
    offset = digest[-1] & 0x0F
    code_int = (
        struct.unpack(">I", digest[offset : offset + 4])[0] & 0x7FFFFFFF
    ) % (10**TOTP_DIGITS)
    return f"{code_int:0{TOTP_DIGITS}d}"


def compute_current_totp(secret: str, for_time: Optional[float] = None) -> Tuple[str, int]:
    """Return `(6_digit_code, current_step)` for `for_time` (defaults to `time.time()`)."""
    ts = for_time if for_time is not None else time.time()
    step = int(ts // TOTP_PERIOD)
    return compute_totp_at_step(secret, step), step


def _normalize_recovery_code(code: str) -> str:
    return re.sub(r"[\s\-]+", "", (code or "")).upper()


def generate_recovery_codes(count: int = RECOVERY_CODE_COUNT) -> Tuple[List[str], List[str]]:
    """
    Generate `count` human-friendly recovery codes (`XXXX-XXXX`) and their SHA-256 hashes.
    Returns `(plaintext_codes, sha256_hashes)`.
    """
    plain_codes: List[str] = []
    hashes: List[str] = []
    for _ in range(count):
        part1 = "".join(secrets.choice(RECOVERY_ALPHABET) for _ in range(4))
        part2 = "".join(secrets.choice(RECOVERY_ALPHABET) for _ in range(4))
        formatted = f"{part1}-{part2}"
        plain_codes.append(formatted)
        hashes.append(hash_secret_token(_normalize_recovery_code(formatted)))
    return plain_codes, hashes


def begin_totp_setup(account_name: str = "Admin") -> Dict[str, Any]:
    """
    Create a pending TOTP secret for enrollment.
    Does not overwrite an already-enabled 2FA configuration until activated,
    and refuses if 2FA is already enabled.
    """
    with _totp_lock:
        state = load_totp_state()
        if state.get("enabled") and state.get("secret"):
            raise ValueError("2FA 双因素认证已处于启用状态，如需重新绑定请先验证并关闭当前 2FA。")
        secret = generate_totp_secret()
        state["pendingSecret"] = secret
        save_totp_state(state)
        return {
            "secret": secret,
            "otpauthUri": build_otpauth_uri(secret, account_name=account_name),
            "period": TOTP_PERIOD,
            "digits": TOTP_DIGITS,
        }


def activate_totp_setup(code: str) -> Dict[str, Any]:
    """
    Verify the initial 6-digit code against `pendingSecret`, activate 2FA,
    and return 8 one-time recovery codes.
    """
    cleaned = re.sub(r"\s+", "", str(code or ""))
    if not re.fullmatch(r"\d{6}", cleaned):
        raise ValueError("请输入验证器 App 显示的 6 位数字动态码。")

    with _totp_lock:
        state = load_totp_state()
        pending = state.get("pendingSecret") or ""
        if not pending:
            raise ValueError("未找到待激活的 2FA 密钥，请重新点击生成绑定二维码。")

        now_step = int(time.time() // TOTP_PERIOD)
        matched_step: Optional[int] = None
        for offset in (0, -1, 1):
            cand_step = now_step + offset
            cand_code = compute_totp_at_step(pending, cand_step)
            if hmac.compare_digest(cleaned.encode("ascii"), cand_code.encode("ascii")):
                matched_step = cand_step
                break

        if matched_step is None:
            raise ValueError("验证码校验失败，请确认验证器时间同步并输入最新 6 位动态码。")

        recovery_plain, recovery_hashes = generate_recovery_codes()
        state["enabled"] = True
        state["secret"] = pending
        state["pendingSecret"] = ""
        state["recoveryCodeHashes"] = recovery_hashes
        state["lastUsedStep"] = matched_step
        state["enabledAt"] = _now_iso()
        save_totp_state(state)

        return {
            "enabled": True,
            "recoveryCodes": recovery_plain,
            "recoveryCodesRemaining": len(recovery_hashes),
            "enabledAt": state["enabledAt"],
        }


def _check_rate_limit(actor_key: str) -> Optional[int]:
    now = time.time()
    with _totp_failures_lock:
        bucket = [t for t in _totp_failures.get(actor_key, []) if now - t < TOTP_LOCKOUT_SECONDS]
        _totp_failures[actor_key] = bucket
        if len(bucket) >= TOTP_MAX_FAILURES:
            return max(1, int(TOTP_LOCKOUT_SECONDS - (now - bucket[0])))
    return None


def _record_failure(actor_key: str) -> None:
    now = time.time()
    with _totp_failures_lock:
        bucket = [t for t in _totp_failures.get(actor_key, []) if now - t < TOTP_LOCKOUT_SECONDS]
        bucket.append(now)
        _totp_failures[actor_key] = bucket


def _clear_failures(actor_key: str) -> None:
    with _totp_failures_lock:
        _totp_failures.pop(actor_key, None)


def verify_totp_or_recovery(
    code: str,
    actor_key: str = "global",
    for_time: Optional[float] = None,
) -> Tuple[bool, str, str]:
    """
    Verify a 6-digit TOTP code or an 8-char recovery code.
    Returns `(ok, status_code, message)`:
      - `status_code` on success: `"TOTP_OK"` or `"RECOVERY_OK"`
      - `status_code` on failure: `"TOTP_NOT_ENABLED"`, `"TOTP_RATE_LIMITED"`,
        `"TOTP_REPLAYED"`, or `"INVALID_TOTP_CODE"`
    """
    retry_after = _check_rate_limit(actor_key)
    if retry_after is not None:
        return (
            False,
            "TOTP_RATE_LIMITED",
            f"2FA 验证失败次数过多，请等待 {retry_after} 秒后再试。",
        )

    raw_input = str(code or "").strip()
    if not raw_input:
        return False, "INVALID_TOTP_CODE", "请输入 6 位 2FA 动态码或 8 位应急恢复码。"

    with _totp_lock:
        state = load_totp_state()
        secret = state.get("secret") or ""
        if not state.get("enabled") or not secret:
            return False, "TOTP_NOT_ENABLED", "尚未启用 2FA 双因素认证。"

        digits_only = re.sub(r"\s+", "", raw_input)
        if re.fullmatch(r"\d{6}", digits_only):
            ts = for_time if for_time is not None else time.time()
            now_step = int(ts // TOTP_PERIOD)
            last_used = int(state.get("lastUsedStep") or 0)

            for offset in (0, -1, 1):
                cand_step = now_step + offset
                cand_code = compute_totp_at_step(secret, cand_step)
                if hmac.compare_digest(digits_only.encode("ascii"), cand_code.encode("ascii")):
                    if cand_step <= last_used:
                        return (
                            False,
                            "TOTP_REPLAYED",
                            "该 6 位动态码在当前 30 秒周期内已被使用过，请等待下一个动态码刷新后再试。",
                        )
                    state["lastUsedStep"] = cand_step
                    save_totp_state(state)
                    _clear_failures(actor_key)
                    return True, "TOTP_OK", "2FA 动态码验证通过。"

            _record_failure(actor_key)
            return False, "INVALID_TOTP_CODE", "2FA 动态码错误或已过期。"

        # Otherwise check recovery codes (8 alphanumeric chars after stripping hyphens/spaces)
        norm_rec = _normalize_recovery_code(raw_input)
        if len(norm_rec) == 8:
            existing_hashes = list(state.get("recoveryCodeHashes") or [])
            matched_idx: Optional[int] = None
            for idx, h in enumerate(existing_hashes):
                if verify_secret_token_hash(norm_rec, h):
                    matched_idx = idx
                    break
            if matched_idx is not None:
                existing_hashes.pop(matched_idx)
                state["recoveryCodeHashes"] = existing_hashes
                save_totp_state(state)
                _clear_failures(actor_key)
                return (
                    True,
                    "RECOVERY_OK",
                    f"应急恢复码验证通过（剩余可用恢复码：{len(existing_hashes)} 个）。",
                )

        _record_failure(actor_key)
        return False, "INVALID_TOTP_CODE", "验证码或应急恢复码无效。"


def disable_totp() -> None:
    """Disable 2FA and wipe stored TOTP secrets and recovery codes."""
    with _totp_lock:
        state = deepcopy(DEFAULT_TOTP_STATE)
        save_totp_state(state)


def regenerate_recovery_codes() -> List[str]:
    """Generate a fresh set of 8 recovery codes for an already-enabled 2FA state."""
    with _totp_lock:
        state = load_totp_state()
        if not state.get("enabled") or not state.get("secret"):
            raise ValueError("尚未启用 2FA，无法重置恢复码。")
        plain_codes, hashes = generate_recovery_codes()
        state["recoveryCodeHashes"] = hashes
        save_totp_state(state)
        return plain_codes


# ==================== High-Risk Route Classification Matrix ====================

def _is_keep_secret_placeholder(val: Any) -> bool:
    if val is None or val == "":
        return True
    s = str(val).strip()
    return "****" in s or "•" in s


def classify_request_totp_tier(
    method: str,
    path: str,
    payload: Optional[Dict[str, Any]] = None,
) -> Optional[str]:
    """
    Determine whether an incoming HTTP request targets a high-risk operation:
      - Returns `"strict"` for irreversible / credential / 2FA-disable actions
        (requires a fresh TOTP code on every call).
      - Returns `"sudo"` for reversible Day-2 ops, console access, firewall/network
        changes, MCP config changes, or enabling `auto_pay` / `waiveRetractationPeriod`
        (grants and accepts a 5-minute Sudo window).
      - Returns `None` for non-high-risk endpoints.
    """
    m = (method or "GET").upper()
    p = (path or "").rstrip("/") or "/"
    body = payload if isinstance(payload, dict) else {}

    if m == "OPTIONS":
        return None

    # --- 1. 2FA Self-Management (strict) ---
    if p in ("/api/auth/totp/disable", "/api/auth/totp/recovery-codes/regenerate") and m == "POST":
        return "strict"

    # --- 2. OVH API Accounts & Credentials (strict) ---
    if p == "/api/accounts" and m == "POST":
        return "strict"
    if re.fullmatch(r"/api/accounts/[^/]+", p) and m == "DELETE":
        return "strict"
    if p == "/api/settings" and m == "POST":
        if body.get("clearCredentials"):
            return "strict"
        for field in ("appKey", "appSecret", "consumerKey"):
            if field in body and not _is_keep_secret_placeholder(body.get(field)):
                return "strict"
        return None

    # --- 3. Irreversible Bare-Metal Server Lifecycle & Ownership (strict) ---
    if re.fullmatch(r"/api/server-control/[^/]+/install", p) and m == "POST":
        return "strict"
    if re.fullmatch(r"/api/server-control/[^/]+/terminate", p) and m == "POST":
        return "strict"
    if re.fullmatch(r"/api/server-control/[^/]+/confirm-termination", p) and m == "POST":
        return "strict"
    if re.fullmatch(r"/api/server-control/.+/change-contact", p) and m == "POST":
        return "strict"
    if re.fullmatch(r"/api/ovh/contact-change-requests/\d+/accept", p) and m == "POST":
        return "strict"
    if re.fullmatch(r"/api/server-control/[^/]+/hardware/replace", p) and m == "POST":
        return "strict"
    if re.fullmatch(r"/api/server-control/[^/]+/backup-ftp", p) and m == "DELETE":
        return "strict"

    # --- 4. Reversible Server Power, Boot, Console, Network & Firewall (sudo) ---
    if re.fullmatch(r"/api/server-control/[^/]+/reboot", p) and m == "POST":
        return "sudo"
    if re.fullmatch(r"/api/ovh/vps/.+/reboot", p) and m == "POST":
        return "sudo"
    if re.fullmatch(r"/api/server-control/[^/]+/boot/\d+", p) and m == "PUT":
        return "sudo"
    if re.fullmatch(r"/api/server-control/[^/]+/boot-mode", p) and m == "PUT":
        return "sudo"
    if re.fullmatch(r"/api/server-control/[^/]+/console", p) and m == "GET":
        return "sudo"
    if re.fullmatch(r"/api/server-control/[^/]+/ip/move", p) and m == "POST":
        return "sudo"
    if re.fullmatch(r"/api/server-control/[^/]+/vrack/.+", p) and m == "DELETE":
        return "sudo"
    if re.fullmatch(r"/api/server-control/[^/]+/ola/(aggregation|reset|group|ungroup)", p) and m == "POST":
        return "sudo"
    if re.fullmatch(r"/api/server-control/[^/]+/virtual-network-interface/[^/]+/(enable|disable)", p) and m == "POST":
        return "sudo"
    if re.fullmatch(r"/api/server-control/[^/]+/virtual-mac", p) and m == "POST":
        return "sudo"
    if re.fullmatch(r"/api/server-control/[^/]+/firewall", p) and m == "PUT":
        return "sudo"
    if re.fullmatch(r"/api/server-control/.+/ip-security/rules", p) and m == "POST":
        return "sudo"
    if re.fullmatch(r"/api/server-control/.+/ip-security/rules/\d+", p) and m == "DELETE":
        return "sudo"
    if re.fullmatch(r"/api/server-control/.+/ip-security/mitigation", p) and m == "POST":
        return "sudo"
    if re.fullmatch(r"/api/server-control/[^/]+/backup-ftp", p) and m == "POST":
        return "sudo"
    if re.fullmatch(r"/api/server-control/[^/]+/backup-ftp/access.*", p) and m in ("POST", "DELETE"):
        return "sudo"
    if re.fullmatch(r"/api/server-control/[^/]+/backup-ftp/password", p) and m == "POST":
        return "sudo"
    if re.fullmatch(r"/api/ovh/account/ssh-keys.*", p) and m in ("POST", "PUT", "DELETE"):
        return "sudo"
    if re.fullmatch(r"/api/ovh/contact-change-requests/\d+/refuse", p) and m == "POST":
        return "sudo"

    # --- 5. MCP Gateway Governance (sudo) ---
    if p in ("/api/mcp/config", "/api/mcp/settings") and m in ("PUT", "POST"):
        return "sudo"
    if p in ("/api/mcp/token/rotate", "/api/mcp/token/revoke") and m == "POST":
        return "sudo"

    # --- 6. Auto-Pay & Waive EU Retraction Right in Queue / Quick-Order (sudo) ---
    if (
        (p == "/api/queue" and m == "POST")
        or (re.fullmatch(r"/api/queue/[^/]+", p) and p != "/api/queue/clear" and m == "PUT")
        or (p == "/api/config-sniper/quick-order" and m == "POST")
        or (p == "/api/config-sniper/tasks" and m == "POST")
    ):
        auto_pay_flag = bool(body.get("auto_pay") or body.get("autoPay"))
        waive_flag = bool(
            body.get("waive_retractation_period") or body.get("waiveRetractationPeriod")
        )
        if auto_pay_flag or waive_flag:
            return "sudo"

    return None
