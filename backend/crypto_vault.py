"""
At-Rest Cryptographic Vault (AES-256-GCM + HKDF-SHA256)
=======================================================
- Enforces an independent, high-entropy `DATA_ENCRYPTION_KEY` environment variable
  (strictly decoupled from `API_SECRET_KEY`).
- Encrypts sensitive credentials at rest in `./data/*.json` using authenticated
  AES-256-GCM envelopes (`enc:v1:<b64url_nonce>:<b64url_ciphertext_and_tag>`).
- Transparently detects legacy plaintext fields on startup and flags them for
  immediate atomic in-place migration to ciphertext.
- Provides SHA-256 one-way hashing for bearer tokens (MCP) and 2FA recovery codes.
"""

from __future__ import annotations

import base64
from copy import deepcopy
import hashlib
import hmac
import os
from typing import Any, Dict, List, Optional, Tuple

from cryptography.exceptions import InvalidTag
from cryptography.hazmat.primitives import hashes
from cryptography.hazmat.primitives.ciphers.aead import AESGCM
from cryptography.hazmat.primitives.kdf.hkdf import HKDF
from dotenv import load_dotenv

BASE_DIR = os.path.dirname(os.path.abspath(__file__))
ENV_PATH = os.path.join(BASE_DIR, ".env")
if os.path.exists(ENV_PATH):
    load_dotenv(dotenv_path=ENV_PATH)
else:
    load_dotenv()

ENC_PREFIX = "enc:v1:"
AAD_CONTEXT = b"ovh-nexus:at-rest:v1"
HKDF_SALT = b"ovh-nexus-vault-salt-v1"
HKDF_INFO = b"ovh-nexus-at-rest-aes256gcm-v1"

WEAK_ENCRYPTION_KEYS = {
    "",
    "123456",
    "1234567890123456",
    "ovh-nexus-default-secret-key",
    "ovh-nexus-default-encryption-key",
    "replace-with-a-strong-random-secret",
    "replace-with-a-strong-encryption-key",
    "votre-cle-secrete-robuste",
    "请替换为您自己的高强度密码",
    "请替换为您自己的高强度加密密钥",
    "password",
    "changeme",
}

CONFIG_SECRET_FIELDS = ("appKey", "appSecret", "consumerKey", "tgToken")
ACCOUNT_SECRET_FIELDS = ("appKey", "appSecret", "consumerKey")
NOTIFICATION_SECRET_KEYS_BY_TYPE: Dict[str, Tuple[str, ...]] = {
    "telegram": ("token",),
    "smtp": ("password",),
}


def get_raw_encryption_key() -> str:
    """Read DATA_ENCRYPTION_KEY dynamically from environment."""
    return os.getenv("DATA_ENCRYPTION_KEY", "").strip()


def validate_encryption_key(api_secret_key: Optional[str] = None) -> None:
    """
    Validate DATA_ENCRYPTION_KEY on startup.
    Fails fast if missing, weak (< 16 chars), or identical to API_SECRET_KEY.
    """
    raw_key = get_raw_encryption_key()
    if not raw_key:
        raise RuntimeError(
            "DATA_ENCRYPTION_KEY 未配置。请在环境变量或 backend/.env 中设置至少 16 位的独立加密主密钥，"
            '例如: python -c "import secrets; print(secrets.token_urlsafe(32))"'
        )
    if raw_key in WEAK_ENCRYPTION_KEYS or len(raw_key) < 16:
        raise RuntimeError(
            "DATA_ENCRYPTION_KEY 过弱。请改为至少 16 位的随机高熵字符串，且不要使用示例默认值。"
        )
    login_key = (
        api_secret_key
        if api_secret_key is not None
        else os.getenv("API_SECRET_KEY", "").strip()
    )
    if login_key and hmac.compare_digest(raw_key.encode("utf-8"), login_key.encode("utf-8")):
        raise RuntimeError(
            "DATA_ENCRYPTION_KEY 严禁与登录口令 API_SECRET_KEY 相同。"
            "请为 DATA_ENCRYPTION_KEY 生成独立的随机密钥，以免日后修改登录密码导致历史密文无法解密。"
        )


def _derive_aes_key(raw_key: Optional[str] = None) -> bytes:
    key_material = (raw_key if raw_key is not None else get_raw_encryption_key()).strip()
    if not key_material or len(key_material) < 16 or key_material in WEAK_ENCRYPTION_KEYS:
        validate_encryption_key()
    hkdf = HKDF(
        algorithm=hashes.SHA256(),
        length=32,
        salt=HKDF_SALT,
        info=HKDF_INFO,
    )
    return hkdf.derive(key_material.encode("utf-8"))


def is_encrypted(value: Any) -> bool:
    """Return True if value is an AES-256-GCM envelope (`enc:v1:...`)."""
    return isinstance(value, str) and value.startswith(ENC_PREFIX)


def encrypt_value(plaintext: Any, raw_key: Optional[str] = None) -> str:
    """
    Encrypt a string value into an `enc:v1:<nonce>:<ciphertext>` envelope.
    Empty strings stay empty; already-encrypted envelopes are returned unchanged.
    """
    if plaintext is None:
        return ""
    s = str(plaintext)
    if s == "":
        return ""
    if is_encrypted(s):
        return s

    aes_key = _derive_aes_key(raw_key)
    nonce = os.urandom(12)
    ct = AESGCM(aes_key).encrypt(nonce, s.encode("utf-8"), AAD_CONTEXT)
    b64_nonce = base64.urlsafe_b64encode(nonce).decode("ascii")
    b64_ct = base64.urlsafe_b64encode(ct).decode("ascii")
    return f"{ENC_PREFIX}{b64_nonce}:{b64_ct}"


def decrypt_value(token_or_plaintext: Any, raw_key: Optional[str] = None) -> Tuple[str, bool]:
    """
    Decrypt an `enc:v1:...` envelope or pass through legacy plaintext.
    Returns `(plaintext, was_legacy_plaintext)`:
      - `was_legacy_plaintext=True` indicates a non-empty legacy plaintext value
        that should be migrated to ciphertext on disk.
    """
    if token_or_plaintext is None:
        return "", False
    s = str(token_or_plaintext)
    if s == "":
        return "", False
    if not is_encrypted(s):
        return s, True

    parts = s.split(":")
    if len(parts) != 4 or parts[0] != "enc" or parts[1] != "v1":
        raise ValueError("密文信封格式非法 (Invalid enc:v1 envelope format)")

    try:
        nonce = base64.urlsafe_b64decode(parts[2].encode("ascii"))
        ct = base64.urlsafe_b64decode(parts[3].encode("ascii"))
        aes_key = _derive_aes_key(raw_key)
        pt_bytes = AESGCM(aes_key).decrypt(nonce, ct, AAD_CONTEXT)
        return pt_bytes.decode("utf-8"), False
    except (InvalidTag, ValueError, Exception) as exc:
        raise ValueError(
            "数据解密失败：DATA_ENCRYPTION_KEY 与已加密数据不匹配，或密文文件已被篡改。"
        ) from exc


def encrypt_config_dict(cfg: Dict[str, Any]) -> Dict[str, Any]:
    """Return a deep copy of `config` with all secret fields encrypted for disk storage."""
    out = deepcopy(cfg) if isinstance(cfg, dict) else {}
    for field in CONFIG_SECRET_FIELDS:
        if field in out and out[field]:
            out[field] = encrypt_value(out[field])

    notif = out.get("notifications")
    if isinstance(notif, dict) and isinstance(notif.get("channels"), list):
        for ch in notif["channels"]:
            if not isinstance(ch, dict):
                continue
            ch_type = str(ch.get("type") or "").lower()
            ch_cfg = ch.get("config")
            if isinstance(ch_cfg, dict):
                for skey in NOTIFICATION_SECRET_KEYS_BY_TYPE.get(ch_type, ()):
                    if skey in ch_cfg and ch_cfg[skey]:
                        ch_cfg[skey] = encrypt_value(ch_cfg[skey])
    return out


def decrypt_config_dict(raw_cfg: Dict[str, Any]) -> Tuple[Dict[str, Any], bool]:
    """
    Decrypt a loaded `config.json` dict into plaintext for in-memory runtime.
    Returns `(decrypted_dict, needs_migration)`.
    """
    out = deepcopy(raw_cfg) if isinstance(raw_cfg, dict) else {}
    needs_migration = False

    for field in CONFIG_SECRET_FIELDS:
        if field in out and out[field]:
            pt, was_plain = decrypt_value(out[field])
            out[field] = pt
            if was_plain:
                needs_migration = True

    notif = out.get("notifications")
    if isinstance(notif, dict) and isinstance(notif.get("channels"), list):
        for ch in notif["channels"]:
            if not isinstance(ch, dict):
                continue
            ch_type = str(ch.get("type") or "").lower()
            ch_cfg = ch.get("config")
            if isinstance(ch_cfg, dict):
                for skey in NOTIFICATION_SECRET_KEYS_BY_TYPE.get(ch_type, ()):
                    if skey in ch_cfg and ch_cfg[skey]:
                        pt, was_plain = decrypt_value(ch_cfg[skey])
                        ch_cfg[skey] = pt
                        if was_plain:
                            needs_migration = True

    return out, needs_migration


def encrypt_accounts_payload(
    accounts_dict: Dict[str, Dict[str, Any]],
    default_account_id: Optional[str],
) -> Dict[str, Any]:
    """Return an encrypted payload ready to serialize into `accounts.json`."""
    encrypted_list: List[Dict[str, Any]] = []
    for acc in (accounts_dict or {}).values():
        if not isinstance(acc, dict):
            continue
        item = deepcopy(acc)
        for field in ACCOUNT_SECRET_FIELDS:
            if field in item and item[field]:
                item[field] = encrypt_value(item[field])
        encrypted_list.append(item)
    return {
        "accounts": encrypted_list,
        "defaultAccountId": default_account_id,
    }


def decrypt_accounts_data(raw_accounts_data: Any) -> Tuple[Dict[str, Dict[str, Any]], bool]:
    """
    Decrypt `accounts` from `accounts.json` (supports list or dict representation).
    Returns `(accounts_by_id, needs_migration)`.
    """
    needs_migration = False
    result: Dict[str, Dict[str, Any]] = {}

    if isinstance(raw_accounts_data, list):
        items = [a for a in raw_accounts_data if isinstance(a, dict) and a.get("id")]
    elif isinstance(raw_accounts_data, dict):
        items = [a for a in raw_accounts_data.values() if isinstance(a, dict) and a.get("id")]
    else:
        items = []

    for raw_acc in items:
        acc = deepcopy(raw_acc)
        for field in ACCOUNT_SECRET_FIELDS:
            if field in acc and acc[field]:
                pt, was_plain = decrypt_value(acc[field])
                acc[field] = pt
                if was_plain:
                    needs_migration = True
        result[str(acc["id"])] = acc

    return result, needs_migration


def hash_secret_token(token: str) -> str:
    """Compute a deterministic SHA-256 hex digest for bearer tokens or recovery codes."""
    normalized = (token or "").strip()
    if not normalized:
        return ""
    return hashlib.sha256(normalized.encode("utf-8")).hexdigest()


def verify_secret_token_hash(candidate: str, expected_hash: str) -> bool:
    """Constant-time verification of a candidate token against a SHA-256 hex digest."""
    if not candidate or not expected_hash:
        return False
    cand_hash = hash_secret_token(candidate)
    return hmac.compare_digest(cand_hash.encode("ascii"), expected_hash.strip().encode("ascii"))
