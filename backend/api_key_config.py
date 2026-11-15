"""
认证与密钥配置。

- Web 登录口令复用 API_SECRET_KEY（最短 12 位，禁止弱默认值）
- 静态落盘加密主密钥使用独立的 DATA_ENCRYPTION_KEY（最短 16 位，严禁与登录口令相同）
- 会话签名密钥优先 SECRET_KEY，否则加密持久化到 data/session_secret
- Telegram Webhook secret 加密持久化到 data/webhook_secret
- 内部进程使用进程级随机 INTERNAL_TOKEN，不落盘
"""

import os
import secrets
import stat
from dotenv import load_dotenv

from crypto_vault import decrypt_value, encrypt_value, validate_encryption_key

BASE_DIR = os.path.dirname(os.path.abspath(__file__))
DATA_DIR = os.path.join(BASE_DIR, 'data')

# 加载 .env 文件（优先 backend/.env）
ENV_PATH = os.path.join(BASE_DIR, '.env')
if os.path.exists(ENV_PATH):
    load_dotenv(dotenv_path=ENV_PATH)
else:
    load_dotenv()

WEAK_KEYS = {
    '',
    '123456',
    'ovh-nexus-default-secret-key',
    'replace-with-a-strong-random-secret',
    'votre-cle-secrete-robuste',
    '请替换为您自己的高强度密码',
    '你得替换为你自己的密钥',
    'password',
    'changeme',
}

API_SECRET_KEY = os.getenv('API_SECRET_KEY', '').strip()
ENABLE_API_KEY_AUTH = os.getenv('ENABLE_API_KEY_AUTH', 'True').lower() == 'true'

# 仅供同进程的监控器调用内部接口（不落盘）
INTERNAL_TOKEN = secrets.token_urlsafe(32)

# 免登录路径（登录相关 + 健康检查）
WHITELIST_PATHS = [
    '/health',
    '/api/health',
    '/api/auth/login',
    '/api/auth/status',
    '/api/auth/logout',
    # Telegram 服务器无法携带会话；该端点由处理函数校验 secret_token + chat_id
    '/api/telegram/webhook',
    # MCP Streamable HTTP 端点由 mcp.server 独立校验专用 Bearer Token 与限流（不含 /api/mcp/* 管理接口）
    '/api/mcp',
]


def validate_password():
    """启动时校验登录口令与静态落盘加密密钥；不满足条件直接拒绝启动。"""
    if not API_SECRET_KEY:
        raise RuntimeError(
            'API_SECRET_KEY 未配置。请在 backend/.env 中设置至少 12 位的强随机口令，'
            '例如: python -c "import secrets; print(secrets.token_urlsafe(24))"'
        )
    if API_SECRET_KEY in WEAK_KEYS or len(API_SECRET_KEY) < 12:
        raise RuntimeError(
            'API_SECRET_KEY 过弱。请改为至少 12 位的随机字符串，且不要使用仓库示例值。'
        )
    validate_encryption_key(API_SECRET_KEY)


def _write_encrypted_secret_file(path: str, plaintext_value: str) -> None:
    os.makedirs(os.path.dirname(path), exist_ok=True)
    encrypted = encrypt_value(plaintext_value)
    tmp_path = f"{path}.tmp"
    fd = os.open(tmp_path, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, stat.S_IRUSR | stat.S_IWUSR)
    with os.fdopen(fd, 'w', encoding='utf-8') as f:
        f.write(encrypted)
    os.chmod(tmp_path, stat.S_IRUSR | stat.S_IWUSR)
    os.replace(tmp_path, path)
    os.chmod(path, stat.S_IRUSR | stat.S_IWUSR)


def _read_or_create_secret(filename, length=32):
    """读取或生成 0600 权限的 AES-256-GCM 加密密钥文件（自动迁移旧明文）。"""
    os.makedirs(DATA_DIR, exist_ok=True)
    path = os.path.join(DATA_DIR, filename)
    if os.path.exists(path):
        with open(path, 'r', encoding='utf-8') as f:
            raw_value = f.read().strip()
        if raw_value:
            plaintext, was_legacy = decrypt_value(raw_value)
            if was_legacy and plaintext:
                _write_encrypted_secret_file(path, plaintext)
            return plaintext

    value = secrets.token_urlsafe(length)
    _write_encrypted_secret_file(path, value)
    return value


def get_session_secret():
    """Flask session 签名密钥。"""
    env_secret = os.getenv('SECRET_KEY', '').strip()
    if env_secret:
        return env_secret
    return _read_or_create_secret('session_secret')


def get_webhook_secret():
    """Telegram Webhook 的 secret_token。"""
    return _read_or_create_secret('webhook_secret')
