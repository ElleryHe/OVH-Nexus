"""
API 认证与 2FA 高危操作门禁中间件。

规则：
1. 浏览器使用服务端会话 Cookie（HttpOnly + SameSite=Lax）；
2. 同进程监控器使用 X-Internal-Token；
3. X-API-Key 仅允许来自本机直连（无反向代理头）的脚本；
4. 其余 /api/* 请求一律 401；
5. 所有命中高危矩阵（strict / sudo）的请求：
   - 未绑定 2FA 时一律返回 403 (TOTP_SETUP_REQUIRED)；
   - strict 级要求每次附带有效 X-TOTP-Code（防 30s 重放）；
   - sudo 级验证一次授予当前会话 5 分钟 Sudo 提权窗口。
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
from totp_service import (
    SUDO_DURATION_SECONDS,
    activate_totp_setup,
    begin_totp_setup,
    classify_request_totp_tier,
    disable_totp,
    is_totp_enabled,
    load_totp_state,
    regenerate_recovery_codes,
    verify_totp_or_recovery,
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


def _extract_request_totp_code() -> str:
    header_code = (request.headers.get('X-TOTP-Code') or '').strip()
    if header_code:
        return header_code
    if request.is_json:
        body = request.get_json(silent=True) or {}
        if isinstance(body, dict):
            return str(body.get('totpCode') or body.get('totp_code') or '').strip()
    return ''


def _enforce_high_risk_totp_gate():
    """
    Evaluate whether the current authenticated request requires 2FA verification.
    Returns a Flask response tuple `(response, 403)` if blocked, or `None` if allowed.
    """
    payload = request.get_json(silent=True) if request.is_json else None
    tier = classify_request_totp_tier(request.method, request.path, payload)
    if not tier:
        return None

    if not is_totp_enabled():
        return jsonify({
            'error': '2FA Setup Required',
            'code': 'TOTP_SETUP_REQUIRED',
            'tier': tier,
            'message': '安全策略限制：未启用双因素认证 (2FA) 时禁止执行高危操作。请前往「系统设置 → 双因素认证」完成绑定。',
        }), 403

    candidate_code = _extract_request_totp_code()
    now_ts = int(time.time())
    sudo_until = int(session.get('sudo_until') or 0)

    if tier == 'sudo':
        if not candidate_code and sudo_until > now_ts:
            return None
        if not candidate_code:
            return jsonify({
                'error': '2FA Sudo Required',
                'code': 'TOTP_STEP_UP_REQUIRED',
                'tier': 'sudo',
                'message': '执行此高危运维操作需验证 2FA 动态码（验证通过后 5 分钟内免重复输入）。',
            }), 403

        ok, status_code, msg = verify_totp_or_recovery(
            candidate_code,
            actor_key=f"sudo:{_client_ip()}",
        )
        if not ok:
            return jsonify({
                'error': 'Invalid 2FA Code',
                'code': status_code,
                'tier': 'sudo',
                'message': msg,
            }), 403

        session['sudo_until'] = now_ts + SUDO_DURATION_SECONDS
        return None

    # tier == 'strict': always requires a fresh TOTP or recovery code on this request
    if not candidate_code:
        return jsonify({
            'error': '2FA Strict Verification Required',
            'code': 'TOTP_STEP_UP_REQUIRED',
            'tier': 'strict',
            'message': '该操作属于不可逆高危操作，必须输入当前最新的 6 位 2FA 动态码或应急恢复码进行二次确认。',
        }), 403

    ok, status_code, msg = verify_totp_or_recovery(
        candidate_code,
        actor_key=f"strict:{_client_ip()}",
    )
    if not ok:
        return jsonify({
            'error': 'Invalid 2FA Code',
            'code': status_code,
            'tier': 'strict',
            'message': msg,
        }), 403

    session['sudo_until'] = now_ts + SUDO_DURATION_SECONDS
    return None


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
    """配置会话、登录端点、2FA 管理端点与全局 /api 门禁。"""
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
            return jsonify({'authenticated': True, 'totpEnabled': is_totp_enabled()})

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

        # Password matched; if 2FA is enabled, require TOTP verification before granting session
        if is_totp_enabled():
            totp_code = str(payload.get('totpCode') or payload.get('totp_code') or '').strip()
            if not totp_code:
                return jsonify({
                    'authenticated': False,
                    'requireTotp': True,
                    'totpEnabled': True,
                    'message': '请输入 6 位 2FA 动态码或 8 位应急恢复码完成登录',
                })
            ok, status_code, msg = verify_totp_or_recovery(totp_code, actor_key=f"login:{ip}")
            if not ok:
                with _attempts_lock:
                    _attempts[ip].append(now)
                return jsonify({
                    'error': 'Invalid 2FA code',
                    'message': msg,
                    'code': status_code,
                    'requireTotp': True,
                }), 401

        with _attempts_lock:
            _attempts.pop(ip, None)

        now_int = int(time.time())
        session.permanent = True
        session['auth'] = True
        session['login_at'] = now_int
        if is_totp_enabled():
            session['sudo_until'] = now_int + SUDO_DURATION_SECONDS
        return jsonify({
            'authenticated': True,
            'totpEnabled': is_totp_enabled(),
            'sudoActive': is_totp_enabled(),
            'sudoExpiresAt': session.get('sudo_until'),
        })

    @app.route('/api/auth/status', methods=['GET'])
    def auth_status():
        now_int = int(time.time())
        sudo_until = int(session.get('sudo_until') or 0)
        is_authed = bool(session.get('auth'))
        return jsonify({
            'authenticated': is_authed,
            'totpEnabled': is_totp_enabled(),
            'sudoActive': bool(is_authed and sudo_until > now_int),
            'sudoExpiresAt': sudo_until if (is_authed and sudo_until > now_int) else None,
        })

    @app.route('/api/auth/logout', methods=['POST'])
    def auth_logout():
        session.clear()
        return jsonify({'authenticated': False})

    # ==================== 2FA (TOTP) Management Endpoints ====================

    @app.route('/api/auth/totp/status', methods=['GET'])
    def totp_status_endpoint():
        st = load_totp_state()
        now_int = int(time.time())
        sudo_until = int(session.get('sudo_until') or 0)
        enabled = bool(st.get('enabled') and st.get('secret'))
        return jsonify({
            'enabled': enabled,
            'hasPendingSetup': bool(st.get('pendingSecret') and not enabled),
            'recoveryCodesRemaining': len(st.get('recoveryCodeHashes') or []) if enabled else 0,
            'enabledAt': st.get('enabledAt') if enabled else None,
            'sudoActive': bool(enabled and sudo_until > now_int),
            'sudoExpiresAt': sudo_until if (enabled and sudo_until > now_int) else None,
        })

    @app.route('/api/auth/totp/setup', methods=['POST'])
    def totp_setup_endpoint():
        try:
            data = begin_totp_setup(account_name='Admin')
            return jsonify({'success': True, **data})
        except ValueError as exc:
            return jsonify({'success': False, 'error': str(exc)}), 400

    @app.route('/api/auth/totp/activate', methods=['POST'])
    def totp_activate_endpoint():
        payload = request.get_json(silent=True) or {}
        code = str(payload.get('code') or payload.get('totpCode') or '').strip()
        try:
            result = activate_totp_setup(code)
            now_int = int(time.time())
            session['sudo_until'] = now_int + SUDO_DURATION_SECONDS
            return jsonify({
                'success': True,
                **result,
                'sudoActive': True,
                'sudoExpiresAt': session['sudo_until'],
            })
        except ValueError as exc:
            return jsonify({'success': False, 'error': str(exc)}), 400

    @app.route('/api/auth/totp/verify-sudo', methods=['POST'])
    def totp_verify_sudo_endpoint():
        if not is_totp_enabled():
            return jsonify({
                'success': False,
                'code': 'TOTP_SETUP_REQUIRED',
                'error': '尚未启用 2FA 双因素认证',
            }), 400
        payload = request.get_json(silent=True) or {}
        code = str(payload.get('code') or payload.get('totpCode') or '').strip()
        ok, status_code, msg = verify_totp_or_recovery(code, actor_key=f"sudo:{_client_ip()}")
        if not ok:
            return jsonify({
                'success': False,
                'code': status_code,
                'error': msg,
            }), 403
        now_int = int(time.time())
        session['sudo_until'] = now_int + SUDO_DURATION_SECONDS
        return jsonify({
            'success': True,
            'message': msg,
            'sudoActive': True,
            'sudoExpiresAt': session['sudo_until'],
        })

    @app.route('/api/auth/totp/disable', methods=['POST'])
    def totp_disable_endpoint():
        # Note: `strict` TOTP verification is already enforced by `before_request`
        disable_totp()
        session.pop('sudo_until', None)
        return jsonify({'success': True, 'enabled': False, 'message': '已关闭 2FA 双因素认证'})

    @app.route('/api/auth/totp/recovery-codes/regenerate', methods=['POST'])
    def totp_regenerate_recovery_endpoint():
        # Note: `strict` TOTP verification is already enforced by `before_request`
        try:
            codes = regenerate_recovery_codes()
            return jsonify({
                'success': True,
                'recoveryCodes': codes,
                'recoveryCodesRemaining': len(codes),
            })
        except ValueError as exc:
            return jsonify({'success': False, 'error': str(exc)}), 400

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

        token = request.headers.get('X-Internal-Token', '')
        if token and hmac.compare_digest(token.encode('utf-8'), INTERNAL_TOKEN.encode('utf-8')):
            return None

        authenticated = False
        if session.get('auth'):
            authenticated = True
        else:
            key = request.headers.get('X-API-Key', '')
            if key and _is_direct_loopback() and hmac.compare_digest(key.encode('utf-8'), API_SECRET_KEY.encode('utf-8')):
                authenticated = True

        if not authenticated:
            return _unauthorized()

        return _enforce_high_risk_totp_gate()
