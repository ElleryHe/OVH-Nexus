"""
API 认证中间件。

规则：
1. 浏览器使用服务端会话 Cookie（HttpOnly + SameSite=Lax）；
2. 同进程监控器使用 X-Internal-Token；
3. X-API-Key 仅允许来自本机直连（无反向代理头）的脚本；
4. 其余 /api/* 请求一律 401。
"""

import hmac
import os
import threading
import time
from collections import defaultdict, deque
from datetime import timedelta
from functools import wraps

from flask import current_app, jsonify, request, session

from api_key_config import (
    API_SECRET_KEY,
    ENABLE_API_KEY_AUTH,
    INTERNAL_TOKEN,
    WHITELIST_PATHS,
    get_session_secret,
    validate_password,
)

LOGIN_MAX_ATTEMPTS = 5
LOGIN_WINDOW_SECONDS = 300

_attempts = defaultdict(deque)
_attempts_lock = threading.Lock()


def _client_ip():
    remote = request.remote_addr or 'unknown'
    forwarded = request.headers.get('X-Real-IP')
    if remote in ('127.0.0.1', '::1') and forwarded:
        return forwarded
    return remote


def _prune(ip, now):
    for k in list(_attempts.keys()):
        b = _attempts[k]
        while b and now - b[0] > LOGIN_WINDOW_SECONDS:
            b.popleft()
        if not b:
            _attempts.pop(k, None)
    return _attempts.get(ip, deque())


def _is_direct_loopback():
    """只有真正的本机直连才允许使用 X-API-Key。"""
    if request.remote_addr not in ('127.0.0.1', '::1'):
        return False
    # 经 nginx 转发的外部请求一定会带这两个头
    if request.headers.get('X-Forwarded-For') or request.headers.get('X-Real-IP'):
        return False
    return True


def _unauthorized(code='AUTH_REQUIRED', message='请先登录'):
    return jsonify({'error': 'Unauthorized', 'message': message, 'code': code}), 401


def require_api_key(f):
    """兼容旧装饰器：内部复用统一校验逻辑。"""
    @wraps(f)
    def decorated_function(*args, **kwargs):
        if not ENABLE_API_KEY_AUTH:
            if current_app.debug:
                return f(*args, **kwargs)
            current_app.logger.warning('ENABLE_API_KEY_AUTH=false 被忽略，认证仍强制启用')
        if session.get('auth'):
            return f(*args, **kwargs)
        token = request.headers.get('X-Internal-Token', '')
        if token and hmac.compare_digest(token.encode('utf-8'), INTERNAL_TOKEN.encode('utf-8')):
            return f(*args, **kwargs)
        key = request.headers.get('X-API-Key', '')
        if key and _is_direct_loopback() and hmac.compare_digest(key.encode('utf-8'), API_SECRET_KEY.encode('utf-8')):
            return f(*args, **kwargs)
        return _unauthorized()
    return decorated_function


def init_api_auth(app):
    """配置会话、登录端点与全局 /api 门禁。"""
    validate_password()

    app.config.update(
        SECRET_KEY=get_session_secret(),
        SESSION_COOKIE_NAME='ovh_session',
        SESSION_COOKIE_HTTPONLY=True,
        SESSION_COOKIE_SAMESITE='Lax',
        SESSION_COOKIE_SECURE=os.getenv('COOKIE_SECURE', 'false').lower() == 'true',
        PERMANENT_SESSION_LIFETIME=timedelta(hours=12),
    )

    @app.route('/api/auth/login', methods=['POST'])
    def auth_login():
        if not ENABLE_API_KEY_AUTH and app.debug:
            session.permanent = True
            session['auth'] = True
            session['login_at'] = int(time.time())
            return jsonify({'authenticated': True})

        ip = _client_ip()
        now = time.time()
        with _attempts_lock:
            bucket = _prune(ip, now)
            if len(bucket) >= LOGIN_MAX_ATTEMPTS:
                retry_after = max(1, int(LOGIN_WINDOW_SECONDS - (now - bucket[0])))
                resp = jsonify({
                    'error': 'Too Many Requests',
                    'message': f'登录尝试过多，请在 {retry_after} 秒后重试',
                    'code': 'RATE_LIMITED',
                })
                resp.status_code = 429
                resp.headers['Retry-After'] = str(retry_after)
                return resp

        payload = request.get_json(silent=True) or {}
        password = payload.get('password') or ''
        if not hmac.compare_digest(str(password).encode('utf-8'), API_SECRET_KEY.encode('utf-8')):
            with _attempts_lock:
                _attempts[ip].append(now)
            return jsonify({
                'error': 'Invalid credentials',
                'message': '密码错误',
                'code': 'INVALID_PASSWORD',
            }), 401

        with _attempts_lock:
            _attempts.pop(ip, None)

        session.permanent = True
        session['auth'] = True
        session['login_at'] = int(time.time())
        return jsonify({'authenticated': True})

    @app.route('/api/auth/status', methods=['GET'])
    def auth_status():
        return jsonify({'authenticated': bool(session.get('auth'))})

    @app.route('/api/auth/logout', methods=['POST'])
    def auth_logout():
        session.clear()
        return jsonify({'authenticated': False})

    @app.before_request
    def verify_api_key():
        if not request.path.startswith('/api/'):
            return None
        if request.method == 'OPTIONS':
            return None
        if request.path in WHITELIST_PATHS:
            return None

        if not ENABLE_API_KEY_AUTH:
            if app.debug:
                return None
            app.logger.warning('ENABLE_API_KEY_AUTH=false 被忽略，认证仍强制启用')

        if session.get('auth'):
            return None

        token = request.headers.get('X-Internal-Token', '')
        if token and hmac.compare_digest(token.encode('utf-8'), INTERNAL_TOKEN.encode('utf-8')):
            return None

        key = request.headers.get('X-API-Key', '')
        if key and _is_direct_loopback() and hmac.compare_digest(key.encode('utf-8'), API_SECRET_KEY.encode('utf-8')):
            return None

        return _unauthorized()
