"""
认证与密钥配置。

- Web 登录口令复用 API_SECRET_KEY（最短 12 位，禁止弱默认值）
- 会话签名密钥优先 SECRET_KEY，否则持久化到 data/session_secret
- Telegram Webhook secret 持久化到 data/webhook_secret
- 内部进程使用进程级随机 INTERNAL_TOKEN，不落盘
"""

import os
import secrets
import stat
from dotenv import load_dotenv

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
    'ovh-phantom-sniper-2024-secret-key',
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
]


def validate_password():
    """启动时校验登录口令；不满足条件直接拒绝启动。"""
    if not API_SECRET_KEY:
        raise RuntimeError(
            'API_SECRET_KEY 未配置。请在 backend/.env 中设置至少 12 位的强随机口令，'
            '例如: python -c "import secrets; print(secrets.token_urlsafe(24))"'
        )
    if API_SECRET_KEY in WEAK_KEYS or len(API_SECRET_KEY) < 12:
        raise RuntimeError(
            'API_SECRET_KEY 过弱。请改为至少 12 位的随机字符串，且不要使用仓库示例值。'
        )


def _read_or_create_secret(filename, length=32):
    """读取或生成 0600 权限的密钥文件。"""
    os.makedirs(DATA_DIR, exist_ok=True)
    path = os.path.join(DATA_DIR, filename)
    if os.path.exists(path):
        with open(path, 'r', encoding='utf-8') as f:
            value = f.read().strip()
        if value:
            return value

    value = secrets.token_urlsafe(length)
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, stat.S_IRUSR | stat.S_IWUSR)
    with os.fdopen(fd, 'w', encoding='utf-8') as f:
        f.write(value)
    os.chmod(path, stat.S_IRUSR | stat.S_IWUSR)
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
