import os
import sys

import pytest

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

import app as A  # noqa: E402  (环境变量由 conftest 注入)

PASSWORD = os.environ['API_SECRET_KEY']
WEBHOOK_SECRET = A.get_webhook_secret()


@pytest.fixture()
def client():
    return A.app.test_client()


@pytest.fixture(autouse=True)
def reset_state():
    A.config.update({
        'appKey': '', 'appSecret': '', 'consumerKey': '',
        'endpoint': 'ovh-eu', 'tgToken': '', 'tgChatId': '',
        'iam': 'go-ovh-ie', 'zone': 'IE',
    })
    A.accounts.clear()
    A.default_account_id = None
    A.monitors.clear()
    A.queue.clear()
    yield


def login(client, ip='127.0.0.1'):
    return client.post(
        '/api/auth/login',
        json={'password': PASSWORD},
        environ_base={'REMOTE_ADDR': ip},
    )


def test_unauthenticated_settings_blocked(client):
    r = client.get('/api/settings')
    assert r.status_code == 401
    assert r.get_json()['code'] == 'AUTH_REQUIRED'


def test_login_sets_httponly_cookie_and_allows_settings(client):
    r = login(client)
    assert r.status_code == 200
    cookie = r.headers.get('Set-Cookie', '')
    assert 'HttpOnly' in cookie

    r = client.get('/api/settings')
    assert r.status_code == 200


def test_wrong_password_and_rate_limit(client):
    ip = '198.51.100.77'
    for _ in range(5):
        r = client.post('/api/auth/login', json={'password': 'wrong'}, environ_base={'REMOTE_ADDR': ip})
        assert r.status_code == 401
    r = client.post('/api/auth/login', json={'password': 'wrong'}, environ_base={'REMOTE_ADDR': ip})
    assert r.status_code == 429
    assert 'Retry-After' in r.headers


def test_rate_limit_prunes_stale_buckets(client, monkeypatch):
    import api_auth_middleware as M
    base_time = 1_700_000_000.0
    monkeypatch.setattr(M.time, 'time', lambda: base_time)

    stale_ip = '198.51.100.200'
    r = client.post('/api/auth/login', json={'password': 'wrong'}, environ_base={'REMOTE_ADDR': stale_ip})
    assert r.status_code == 401
    assert stale_ip in M._attempts

    # Advance past LOGIN_WINDOW_SECONDS; another login check should prune stale_ip
    monkeypatch.setattr(M.time, 'time', lambda: base_time + M.LOGIN_WINDOW_SECONDS + 10)
    other_ip = '198.51.100.201'
    r = client.post('/api/auth/login', json={'password': PASSWORD}, environ_base={'REMOTE_ADDR': other_ip})
    assert r.status_code == 200
    assert stale_ip not in M._attempts
    assert other_ip not in M._attempts


def test_status_and_logout(client):
    assert client.get('/api/auth/status').get_json()['authenticated'] is False
    login(client)
    assert client.get('/api/auth/status').get_json()['authenticated'] is True
    client.post('/api/auth/logout')
    assert client.get('/api/auth/status').get_json()['authenticated'] is False
    assert client.get('/api/settings').status_code == 401


def test_disable_auth_ignored_outside_debug(client, monkeypatch):
    import api_auth_middleware as M
    monkeypatch.setattr(M, 'ENABLE_API_KEY_AUTH', False)
    monkeypatch.setattr(A.app, 'debug', False)

    r = client.get('/api/settings')
    assert r.status_code == 401

    r = client.post('/api/auth/login', json={'password': 'wrong'}, environ_base={'REMOTE_ADDR': '198.51.100.88'})
    assert r.status_code == 401
    assert client.get('/api/auth/status').get_json()['authenticated'] is False

    r = client.post('/api/auth/login', json={'password': PASSWORD}, environ_base={'REMOTE_ADDR': '198.51.100.88'})
    assert r.status_code == 200
    assert client.get('/api/settings').status_code == 200


def test_missing_ovh_credentials_do_not_invalidate_web_session(client):
    login(client)
    r = client.get('/api/server-control/list')
    assert r.status_code == 400
    assert r.get_json()['code'] == 'OVH_CREDENTIALS_MISSING'
    assert client.get('/api/auth/status').get_json()['authenticated'] is True
    assert client.get('/api/settings').status_code == 200


def test_server_control_still_requires_web_session(client):
    r = client.get('/api/server-control/list')
    assert r.status_code == 401
    assert r.get_json()['code'] == 'AUTH_REQUIRED'


def test_settings_are_masked(client):
    A.config['appKey'] = 'APPKEY-ABCD-1234'
    A.config['appSecret'] = 'SUPER-SECRET-VALUE'
    A.config['consumerKey'] = 'CONSUMER-9999'
    login(client)
    body = client.get('/api/settings').get_data(as_text=True)
    assert 'SUPER-SECRET-VALUE' not in body
    assert 'APPKEY-ABCD-1234' not in body
    data = client.get('/api/settings').get_json()
    assert data['appSecret'] == '****'
    assert data['hasAppSecret'] is True


def test_masked_or_empty_fields_keep_previous_value(client):
    A.config['appKey'] = 'KEEP-ME-123456'
    login(client)
    client.post('/api/settings', json={'appKey': '****3456', 'appSecret': '', 'consumerKey': '****'})
    assert A.config['appKey'] == 'KEEP-ME-123456'


def test_accounts_are_masked(client):
    A.accounts['acc1'] = {
        'id': 'acc1', 'alias': 'demo',
        'appKey': 'AK-12345678', 'appSecret': 'AS-87654321',
        'consumerKey': 'CK-00001111', 'endpoint': 'ovh-eu', 'zone': 'IE',
    }
    login(client)
    body = client.get('/api/accounts').get_data(as_text=True)
    assert 'AS-87654321' not in body
    assert 'AK-12345678' not in body
    acc = client.get('/api/accounts').get_json()['accounts'][0]
    assert acc['hasAppSecret'] is True
    assert acc['appSecret'] == '****'


def test_api_key_only_from_direct_loopback(client):
    r = client.get('/api/settings', headers={'X-API-Key': PASSWORD},
                   environ_base={'REMOTE_ADDR': '203.0.113.9'})
    assert r.status_code == 401

    r = client.get('/api/settings', headers={'X-API-Key': PASSWORD, 'X-Real-IP': '203.0.113.9'},
                   environ_base={'REMOTE_ADDR': '127.0.0.1'})
    assert r.status_code == 401

    r = client.get('/api/settings', headers={'X-API-Key': PASSWORD, 'X-Forwarded-For': '203.0.113.9'},
                   environ_base={'REMOTE_ADDR': '127.0.0.1'})
    assert r.status_code == 401

    r = client.get('/api/settings', headers={'X-API-Key': PASSWORD},
                   environ_base={'REMOTE_ADDR': '127.0.0.1'})
    assert r.status_code == 200


def test_internal_price_requires_internal_token(client, monkeypatch):
    monkeypatch.setattr(A, '_get_server_price_internal', lambda *a, **k: {'success': True, 'price': 1})

    r = client.post('/api/internal/monitor/price', json={'plan_code': 'x'},
                    environ_base={'REMOTE_ADDR': '127.0.0.1'})
    assert r.status_code == 401

    from api_key_config import INTERNAL_TOKEN
    r = client.post('/api/internal/monitor/price', json={'plan_code': 'x'},
                    headers={'X-Internal-Token': INTERNAL_TOKEN},
                    environ_base={'REMOTE_ADDR': '203.0.113.9'})
    assert r.status_code == 200


def test_webhook_requires_secret(client):
    A.config['tgChatId'] = '999'
    payload = {'message': {'text': '24sk202 rbx 1', 'chat': {'id': 999}}}
    assert client.post('/api/telegram/webhook', json=payload).status_code == 403


def test_webhook_requires_matching_chat(client):
    A.config['tgChatId'] = '999'
    bad = {'message': {'text': '24sk202 rbx 1', 'chat': {'id': 111}}}
    r = client.post('/api/telegram/webhook', json=bad,
                    headers={'X-Telegram-Bot-Api-Secret-Token': WEBHOOK_SECRET})
    assert r.status_code == 403


def test_webhook_valid_enqueues(client, monkeypatch):
    A.config['tgChatId'] = '999'
    monkeypatch.setattr(
        A, 'check_server_availability_with_configs',
        lambda plan: {'cfg': {'memory': '64G', 'storage': '2x1T', 'options': [], 'datacenters': {'rbx': 'available'}}},
    )
    monkeypatch.setattr(A, 'save_data', lambda: None)
    monkeypatch.setattr(A, 'update_stats', lambda: None)

    r = client.post(
        '/api/telegram/webhook',
        json={'message': {'text': '24sk202 rbx 1', 'chat': {'id': 999}}},
        headers={'X-Telegram-Bot-Api-Secret-Token': WEBHOOK_SECRET},
    )
    assert r.status_code == 200, r.get_data(as_text=True)
    assert len(A.queue) == 1
    assert A.queue[0]['planCode'] == '24sk202'


def test_default_account_and_headerless_fallback(client, monkeypatch):
    monkeypatch.setattr(A, 'save_accounts', lambda: None)
    monkeypatch.setattr(A, 'save_data', lambda: None)
    monkeypatch.setattr(A, 'update_stats', lambda: None)
    A.accounts['acc1'] = {
        'id': 'acc1', 'alias': 'first',
        'appKey': 'AK1', 'appSecret': 'AS1', 'consumerKey': 'CK1',
        'endpoint': 'ovh-eu', 'zone': 'IE',
    }
    A.accounts['acc2'] = {
        'id': 'acc2', 'alias': 'second',
        'appKey': 'AK2', 'appSecret': 'AS2', 'consumerKey': 'CK2',
        'endpoint': 'ovh-us', 'zone': 'US',
    }
    login(client)

    # Without explicit defaultAccountId, falls back to first account
    r = client.get('/api/accounts')
    assert r.status_code == 200
    assert r.get_json()['defaultAccountId'] == 'acc1'
    assert A.get_current_account_config()['id'] == 'acc1'

    # Set acc2 as default
    r = client.put('/api/accounts/default', json={'id': 'acc2'})
    assert r.status_code == 200
    assert r.get_json()['defaultAccountId'] == 'acc2'
    assert A.get_current_account_config()['id'] == 'acc2'

    # Explicit argument overrides default
    assert A.get_current_account_config('acc1')['id'] == 'acc1'

    # Deleting default account moves default to remaining first account
    r = client.delete('/api/accounts/acc2')
    assert r.status_code == 200
    assert r.get_json()['defaultAccountId'] == 'acc1'
    assert A.get_current_account_config()['id'] == 'acc1'


def test_webhook_uses_default_account_and_per_account_uuid_cache(client, monkeypatch):
    import time as _time
    A.config['tgChatId'] = '999'
    A.accounts['acc1'] = {
        'id': 'acc1', 'alias': 'first',
        'appKey': 'AK1', 'appSecret': 'AS1', 'consumerKey': 'CK1',
        'endpoint': 'ovh-eu', 'zone': 'IE',
    }
    A.accounts['acc2'] = {
        'id': 'acc2', 'alias': 'second',
        'appKey': 'AK2', 'appSecret': 'AS2', 'consumerKey': 'CK2',
        'endpoint': 'ovh-us', 'zone': 'US',
    }
    A.default_account_id = 'acc2'
    monkeypatch.setattr(
        A, 'check_server_availability_with_configs',
        lambda plan: {'cfg': {'memory': '64G', 'storage': '2x1T', 'options': [], 'datacenters': {'rbx': 'available'}}},
    )
    monkeypatch.setattr(A, 'save_data', lambda: None)
    monkeypatch.setattr(A, 'update_stats', lambda: None)

    # Text order uses default account (acc2)
    r = client.post(
        '/api/telegram/webhook',
        json={'message': {'text': '24sk202 rbx 1', 'chat': {'id': 999}}},
        headers={'X-Telegram-Bot-Api-Secret-Token': WEBHOOK_SECRET},
    )
    assert r.status_code == 200
    assert A.queue[-1]['accountId'] == 'acc2'

    # Inline button uses the account of the monitor that owns the UUID
    class DummyMonitor:
        account_id = 'acc1'
        message_uuid_cache_ttl = 3600
        message_uuid_cache = {
            'uuid-acc1': {
                'planCode': '24rise01',
                'datacenter': 'gra',
                'options': ['ram-64g'],
                'timestamp': _time.time(),
            }
        }

    A.monitors['acc1'] = DummyMonitor()
    r = client.post(
        '/api/telegram/webhook',
        json={
            'callback_query': {
                'id': 'cb1',
                'from': {'id': 999},
                'message': {'message_id': 1, 'chat': {'id': 999}},
                'data': '{"a":"add_to_queue","u":"uuid-acc1"}',
            }
        },
        headers={'X-Telegram-Bot-Api-Secret-Token': WEBHOOK_SECRET},
    )
    assert r.status_code == 200
    assert A.queue[-1]['planCode'] == '24rise01'
    assert A.queue[-1]['accountId'] == 'acc1'


def test_monitor_price_lookup_passes_account_and_port(monkeypatch):
    import requests
    from server_monitor import ServerMonitor

    monkeypatch.setenv('PORT', '29998')
    captured = {}

    class DummyResp:
        status_code = 200
        def raise_for_status(self):
            pass
        def json(self):
            return {'success': True, 'price': {'prices': {'withTax': 19.99, 'currencyCode': 'EUR'}}}

    def fake_post(url, json=None, headers=None, timeout=None):
        captured['url'] = url
        captured['json'] = json
        captured['headers'] = headers
        return DummyResp()

    monkeypatch.setattr(requests, 'post', fake_post)
    mon = ServerMonitor(lambda p: {}, lambda m: None, lambda *a, **k: None, account_id='acc-custom')
    price_str = mon._get_price_info('24sk202', 'rbx', {'options': ['ram-64g']})

    assert price_str == '€19.99/月'
    assert captured['url'] == 'http://127.0.0.1:29998/api/internal/monitor/price'
    assert captured['json']['accountId'] == 'acc-custom'
    assert captured['headers']['X-OVH-Account'] == 'acc-custom'
    assert 'X-Internal-Token' in captured['headers']


def test_edit_account_validates_credentials_and_rejects_mismatch(client, monkeypatch):
    monkeypatch.setattr(A, 'save_accounts', lambda: None)
    A.accounts['acc1'] = {
        'id': 'acc1', 'alias': 'orig-alias',
        'appKey': 'OLD-AK', 'appSecret': 'OLD-AS', 'consumerKey': 'OLD-CK',
        'endpoint': 'ovh-eu', 'zone': 'IE',
    }
    login(client)

    # Editing metadata without credentials skips /me and preserves credentials
    r = client.post('/api/accounts', json={'id': 'acc1', 'alias': 'new-alias', 'zone': 'FR'})
    assert r.status_code == 200
    assert A.accounts['acc1']['alias'] == 'new-alias'
    assert A.accounts['acc1']['zone'] == 'FR'
    assert A.accounts['acc1']['appKey'] == 'OLD-AK'

    # Editing credentials with mismatched customerCode is rejected
    class MismatchedClient:
        def __init__(self, **kwargs):
            pass
        def get(self, path):
            return {'customerCode': 'other-acc', 'nichandle': 'other-nic', 'email': 'other@example.com'}

    monkeypatch.setattr(A.ovh, 'Client', MismatchedClient)
    r = client.post('/api/accounts', json={
        'id': 'acc1',
        'appKey': 'NEW-AK', 'appSecret': 'NEW-AS', 'consumerKey': 'NEW-CK',
    })
    assert r.status_code == 400
    assert '另一个OVH账户' in r.get_json()['error']
    assert A.accounts['acc1']['appKey'] == 'OLD-AK'

    # Editing credentials with matching customerCode succeeds
    class MatchingClient:
        def __init__(self, **kwargs):
            pass
        def get(self, path):
            return {'customerCode': 'acc1', 'nichandle': 'acc1-nic', 'email': 'acc1@example.com'}

    monkeypatch.setattr(A.ovh, 'Client', MatchingClient)
    r = client.post('/api/accounts', json={
        'id': 'acc1',
        'appKey': 'NEW-AK', 'appSecret': 'NEW-AS', 'consumerKey': 'NEW-CK',
    })
    assert r.status_code == 200
    assert A.accounts['acc1']['appKey'] == 'NEW-AK'
    assert A.accounts['acc1']['alias'] == 'new-alias'


def test_telegram_settings_clear_chat_id_and_clear_telegram(client, monkeypatch):
    monkeypatch.setattr(A, 'save_data', lambda: None)
    monkeypatch.setattr(A, 'send_telegram_msg', lambda msg: True)
    A.config['tgToken'] = '123456:SECRET_BOT_TOKEN'
    A.config['tgChatId'] = '-100999888'
    login(client)

    # Empty string tgChatId clears tgChatId while keeping tgToken
    r = client.post('/api/settings', json={'tgChatId': ''})
    assert r.status_code == 200
    assert A.config['tgToken'] == '123456:SECRET_BOT_TOKEN'
    assert A.config['tgChatId'] == ''

    # clearTelegram clears both and calls deleteWebhook
    A.config['tgChatId'] = '-100999888'
    deleted_urls = []

    class DummyResp:
        status_code = 200

    monkeypatch.setattr(A.requests, 'post', lambda url, timeout=5: (deleted_urls.append(url), DummyResp())[1])
    r = client.post('/api/settings', json={'clearTelegram': True})
    assert r.status_code == 200
    assert A.config['tgToken'] == ''
    assert A.config['tgChatId'] == ''
    assert deleted_urls == ['https://api.telegram.org/bot123456:SECRET_BOT_TOKEN/deleteWebhook']


def test_cloud_fleet_endpoints_and_default_locale(client, monkeypatch):
    import routes.cloud_fleet as CF

    login(client)

    # 1. Verify defaultLocale is exposed in /api/endpoint-config and /api/settings
    r_cfg = client.get('/api/endpoint-config')
    assert r_cfg.status_code == 200
    assert r_cfg.get_json()['defaultLocale'] == 'en'

    # 2. Mock OVH client for cloud_fleet routes
    class FakeOVHClient:
        def __init__(self):
            self.created_keys = []
            self.deleted_keys = []
            self.rebooted_vps = []
            self.created_rules = []

        def get(self, path):
            if path == '/me/order':
                return [1001, 1002]
            if path in ('/me/order/1001', '/me/order/1002'):
                oid = int(path.split('/')[-1])
                return {
                    'orderId': oid,
                    'date': '2026-11-01T10:00:00Z',
                    'expirationDate': '2026-11-08T10:00:00Z',
                    'priceWithTax': {'currencyCode': 'EUR', 'text': '49.99 €', 'value': 49.99},
                    'url': f'https://www.ovh.com/order/{oid}',
                    'pdfUrl': f'https://www.ovh.com/order/{oid}.pdf',
                }
            if path.endswith('/status'):
                return 'delivering'
            if path.endswith('/followUp'):
                return [{'step': 'DELIVERING', 'status': 'DOING', 'history': []}]
            if path == '/me/sshKey':
                return ['prod-key']
            if path == '/me/sshKey/prod-key':
                return {'keyName': 'prod-key', 'key': 'ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAITest', 'default': True}
            if path == '/dedicated/server':
                return ['ns12345.ip-1-2-3.eu']
            if path == '/dedicated/server/ns12345.ip-1-2-3.eu':
                return {'name': 'ns12345.ip-1-2-3.eu', 'ip': '198.51.100.10', 'reverse': 'dedi-01.example.com'}
            if path == '/dedicated/server/ns12345.ip-1-2-3.eu/serviceInfos':
                return {
                    'status': 'ok',
                    'creation': '2026-01-01',
                    'expiration': '2026-12-31',
                    'renew': {'automatic': True, 'deleteAtExpiration': False, 'period': 1},
                }
            if path == '/vps':
                return ['vps-abc.ovh.net']
            if path == '/vps/vps-abc.ovh.net':
                return {
                    'name': 'vps-abc.ovh.net',
                    'displayName': 'edge-vps-01',
                    'state': 'running',
                    'zone': 'os-gra1',
                    'memoryLimit': 4096,
                    'vcore': 4,
                    'model': {'name': 'vps-le-4', 'vcore': 4, 'memory': 4096, 'disk': 80},
                }
            if path == '/vps/vps-abc.ovh.net/ips':
                return ['198.51.100.55']
            if path == '/vps/vps-abc.ovh.net/serviceInfos':
                return {
                    'status': 'ok',
                    'creation': '2026-02-01',
                    'expiration': '2026-11-20',
                    'renew': {'automatic': False, 'deleteAtExpiration': False, 'period': 1},
                }
            if path == '/ip/198.51.100.10/firewall/198.51.100.10':
                return {'ipOnFirewall': '198.51.100.10', 'enabled': True, 'state': 'ok'}
            if path == '/ip/198.51.100.10/firewall/198.51.100.10/rule':
                return [0]
            if path == '/ip/198.51.100.10/firewall/198.51.100.10/rule/0':
                return {
                    'sequence': 0,
                    'action': 'permit',
                    'protocol': 'tcp',
                    'destinationPort': 'eq 22',
                    'source': 'any',
                    'state': 'ok',
                }
            if path == '/ip/198.51.100.10/mitigation/198.51.100.10':
                return {'ipOnMitigation': '198.51.100.10', 'permanent': True, 'auto': False, 'state': 'ok'}
            raise AssertionError(f'Unexpected GET path: {path}')

        def post(self, path, **kwargs):
            if path == '/me/sshKey':
                self.created_keys.append(kwargs)
                return kwargs
            if path == '/vps/vps-abc.ovh.net/reboot':
                self.rebooted_vps.append(path)
                return {'id': 99}
            if path.endswith('/rule'):
                self.created_rules.append(kwargs)
                return kwargs
            return {}

        def put(self, path, **kwargs):
            return {}

        def delete(self, path):
            self.deleted_keys.append(path)
            return None

    fake_ovh = FakeOVHClient()
    monkeypatch.setattr(A, 'get_ovh_client', lambda: fake_ovh)

    # Orders & Follow-up
    r_orders = client.get('/api/ovh/account/orders')
    assert r_orders.status_code == 200
    assert len(r_orders.get_json()['data']) == 2

    r_fup = client.get('/api/ovh/account/orders/1001/follow-up')
    assert r_fup.status_code == 200
    assert r_fup.get_json()['data']['status'] == 'delivering'
    assert r_fup.get_json()['data']['followUp'][0]['step'] == 'DELIVERING'

    # SSH Keys
    r_ssh = client.get('/api/ovh/account/ssh-keys')
    assert r_ssh.status_code == 200
    assert r_ssh.get_json()['data'][0]['keyName'] == 'prod-key'

    r_ssh_add = client.post('/api/ovh/account/ssh-keys', json={
        'keyName': 'new-key',
        'key': 'ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAINewKey',
    })
    assert r_ssh_add.status_code == 200
    assert fake_ovh.created_keys[0]['keyName'] == 'new-key'

    # Services Overview
    r_svc = client.get('/api/ovh/account/services-overview')
    assert r_svc.status_code == 200
    svc_data = r_svc.get_json()['data']
    assert len(svc_data) == 2
    assert {s['type'] for s in svc_data} == {'dedicated', 'vps'}

    # IP Security (Firewall Rules & Anti-DDoS Mitigation)
    r_ipsec = client.get('/api/server-control/ns12345.ip-1-2-3.eu/ip-security')
    assert r_ipsec.status_code == 200
    ipsec_data = r_ipsec.get_json()['data']
    assert ipsec_data['ip'] == '198.51.100.10'
    assert ipsec_data['mitigation']['permanent'] is True
    assert len(ipsec_data['rules']) == 1

    # VPS Fleet & Reboot
    r_vps = client.get('/api/ovh/vps/list')
    assert r_vps.status_code == 200
    vps_data = r_vps.get_json()['data']
    assert len(vps_data) == 1
    assert vps_data[0]['displayName'] == 'edge-vps-01'

    r_reboot = client.post('/api/ovh/vps/vps-abc.ovh.net/reboot')
    assert r_reboot.status_code == 200
    assert fake_ovh.rebooted_vps == ['/vps/vps-abc.ovh.net/reboot']


# ══════════════════════════════════════════════════════════════════════════════
# 10. Catalog Service, Notification Hub (TG + SMTP) & Public SKU Change Watcher
# ══════════════════════════════════════════════════════════════════════════════

def test_catalog_service_and_hardware_parsers():
    from catalog_service import (
        CatalogService,
        format_cpu,
        format_gpu,
        format_memory,
        format_storage,
        parse_gpu_code,
        parse_memory_code,
        parse_storage_code,
    )

    # Memory formatting across locales
    m1 = parse_memory_code("ram-32g-noecc-2133")
    assert format_memory(m1, "zh-CN") == "32 GB DDR4 · 非 ECC · 2133 MHz"
    assert format_memory(m1, "en") == "32 GB DDR4 · Non-ECC · 2133 MHz"
    assert format_memory(m1, "fr") == "32 Go DDR4 · Sans ECC · 2133 MHz"

    m2 = parse_memory_code("ram-1024g-ecc-4800")
    assert "1024 GB (1 TB) DDR5 · ECC · 4800 MHz" == format_memory(m2, "zh-CN")

    # Storage & systemStorage formatting
    s1 = parse_storage_code("softraid-1x480ssd")
    assert format_storage(s1, "zh-CN") == "1× 480 GB SSD · 软 RAID"

    s2 = parse_storage_code("hardraid-12x3840sas")
    assert format_storage(s2, "zh-CN") == "12× 3.84 TB SAS SSD · 硬 RAID"

    s3 = parse_storage_code("hybridsoftraid-2x4000sa-2x450nvme")
    assert format_storage(s3, "zh-CN") == "2× 4 TB SATA HDD + 2× 450 GB NVMe SSD · 混合软 RAID"

    s_sys = parse_storage_code("softraid-2x480ssd-sata-system", is_system=True)
    assert format_storage(s_sys, "zh-CN") == "2× 480 GB SATA SSD"

    s_none = parse_storage_code("noraid-0")
    assert format_storage(s_none, "zh-CN") == "无数据盘"

    # GPU formatting
    g1 = parse_gpu_code("gpu-4xnvidia-l40s-48g")
    assert format_gpu(g1, "zh-CN") == "4× NVIDIA L40S 48 GB"

    # Catalog merging & fallback plan resolution
    svc = CatalogService()
    sample_eco = {
        "locale": {"currencyCode": "EUR"},
        "plans": [
            {
                "planCode": "24ska012-sgp",
                "invoiceName": "KS-A | Intel i7-6700k",
                "product": "24ska01",
                "blobs": {"commercial": {"name": "KS-A"}},
                "pricings": [
                    {
                        "phase": 1,
                        "capacities": ["renew"],
                        "mode": "default",
                        "price": 5499000000,
                    }
                ],
                "addonFamilies": [
                    {
                        "name": "storage",
                        "addons": ["softraid-2x960nvme-24ska01"],
                    }
                ],
            }
        ],
        "addons": [
            {
                "planCode": "softraid-2x960nvme-24ska01",
                "product": "softraid-2x960nvme",
                "pricings": [
                    {
                        "phase": 1,
                        "capacities": ["renew"],
                        "mode": "default",
                        "price": 1500000000,
                    }
                ],
            }
        ],
        "products": [
            {
                "name": "24ska01",
                "blobs": {
                    "technical": {
                        "server": {
                            "range": "kimsufi",
                            "cpu": {
                                "brand": "Intel",
                                "model": "i7-6700k",
                                "cores": 4,
                                "threads": 8,
                                "frequency": 4,
                                "boost": 4.2,
                            },
                        }
                    }
                },
            }
        ],
    }
    merged = svc.build_dictionary_from_catalogs(sample_eco, None)
    resolved_ksa = svc.resolve_plan("24ska01-v1", dictionary=merged)
    assert resolved_ksa["name"] == "KS-A"
    assert resolved_ksa["range"] == "KS"
    assert resolved_ksa["price"] == 54.99
    assert resolved_ksa["currency"] == "EUR"
    assert resolved_ksa["addonPrices"]["softraid-2x960nvme"] == 15.0
    assert "Intel i7-6700k · 4核/8线程 · 4/4.2 GHz" == format_cpu(resolved_ksa["cpu"], "zh-CN")

    # Fallback inference for newly racked uncataloged SKU
    fallback_le = svc.resolve_plan("27skle01-v1-sgp", dictionary=merged)
    assert fallback_le["name"] == "KS-LE-1"
    assert fallback_le["region"]["zh-CN"] == "新加坡"


def test_notification_hub_and_smtp(monkeypatch):
    import smtplib
    from notifications import (
        EVENT_ORDER_SUCCESS,
        EVENT_SKU_CHANGED,
        EVENT_STOCK_AVAILABLE,
        NotificationEvent,
        NotificationHub,
    )

    state_store = {
        "tgToken": "123456:ABCDEF_SECRET_TOKEN",
        "tgChatId": "987654321",
    }

    def get_cfg():
        return state_store

    def save_cfg(new_cfg):
        state_store.update(new_cfg)

    hub = NotificationHub(get_config_func=get_cfg, save_config_func=save_cfg)

    # 1. Legacy TG config auto-migration & secret masking
    pub = hub.get_public_state()
    assert len(pub["channels"]) == 1
    assert pub["channels"][0]["type"] == "telegram"
    assert "****" in pub["channels"][0]["config"]["token"]
    assert pub["channels"][0]["hasSecrets"]["token"] is True

    # 2. Add an SMTP channel while keeping masked TG token intact
    sent_smtp_messages = []

    class FakeSMTPSSL:
        def __init__(self, host, port, timeout=15, context=None):
            self.host = host
            self.port = port

        def __enter__(self):
            return self

        def __exit__(self, exc_type, exc, tb):
            pass

        def login(self, user, pwd):
            self.logged_in = (user, pwd)

        def sendmail(self, from_addr, to_addrs, msg_str):
            sent_smtp_messages.append((from_addr, to_addrs, msg_str))

    monkeypatch.setattr(smtplib, "SMTP_SSL", FakeSMTPSSL)

    updated = hub.update_state(
        {
            "panelUrl": "https://nexus.example.com",
            "channels": [
                pub["channels"][0],  # Contains masked token '1234****OKEN', must preserve real secret
                {
                    "id": "smtp-1",
                    "type": "smtp",
                    "name": "Ops Email",
                    "enabled": True,
                    "events": [EVENT_SKU_CHANGED, EVENT_ORDER_SUCCESS],
                    "config": {
                        "host": "smtp.example.com",
                        "port": 465,
                        "security": "ssl",
                        "username": "bot@example.com",
                        "password": "smtp-super-secret-pwd",
                        "fromName": "OVH Nexus",
                        "fromAddress": "bot@example.com",
                        "recipients": ["admin@example.com", "ops@example.com"],
                    },
                },
            ],
        }
    )
    assert len(updated["channels"]) == 2
    # Verify real TG token was preserved
    assert state_store["notifications"]["channels"][0]["config"]["token"] == "123456:ABCDEF_SECRET_TOKEN"

    # 3. Dispatch SKU changed event -> should route to SMTP (and TG if subscribed)
    # Disable TG channel so only SMTP is tested
    state_store["notifications"]["channels"][0]["enabled"] = False
    ev = NotificationEvent(
        event_type=EVENT_SKU_CHANGED,
        title="SKU 变动提醒：上新 1 款",
        plain_text="新增 KS-A (24ska01)",
        fields=[{"label": "型号", "value": "KS-A"}],
    )
    assert hub.dispatch(ev) is True
    assert len(sent_smtp_messages) == 1
    from_addr, to_addrs, raw_mime = sent_smtp_messages[0]
    assert to_addrs == ["admin@example.com", "ops@example.com"]
    assert "multipart/alternative" in raw_mime
    import email
    parsed_msg = email.message_from_string(raw_mime)
    decoded_parts = [
        part.get_payload(decode=True).decode("utf-8", errors="ignore")
        for part in parsed_msg.walk()
        if part.get_payload(decode=True)
    ]
    combined_body = "\n".join(decoded_parts)
    assert "https://nexus.example.com" in combined_body
    assert "KS-A" in combined_body


def test_sku_watcher_anti_false_positive(tmp_path):
    from sku_watcher import SkuWatcher

    dispatched_events = []
    cfg = {
        "endpoint": "ovh-eu",
        "zone": "IE",
        "notifications": {
            "rules": {
                "skuEnabled": True,
                "skuInterval": 600,
                "skuTrackConfigs": True,
                "skuRanges": ["KS", "RISE", "SYS", "ADV", "SCALE", "HGR"],
                "skuMergeRegional": True,
            }
        },
    }

    watcher = SkuWatcher(
        get_config_func=lambda: cfg,
        dispatch_event_func=lambda ev: (dispatched_events.append(ev) or True),
        data_dir=str(tmp_path),
    )

    base_items = [
        {
            "fqn": f"24sk{i}0.ram-32g-noecc-2133.softraid-1x480ssd",
            "planCode": f"24sk{i}0",
            "server": f"24sk{i}0",
            "memory": "ram-32g-noecc-2133",
            "storage": "softraid-1x480ssd",
            "datacenters": [{"datacenter": "gra", "availability": "unavailable"}],
        }
        for i in range(1, 12)
    ]

    # Round 1: Initial run only builds baseline, no notification dispatched
    r1 = watcher.check_once(raw_items=base_items, dictionary={"plans": {}, "options": {}})
    assert r1["status"] == "baseline_initialized"
    assert r1["totalSkus"] == 11
    assert len(dispatched_events) == 0

    # Round 2: Sudden drop (>30% missing) -> skipped as API glitch
    glitch_items = base_items[:3]
    r_glitch = watcher.check_once(raw_items=glitch_items, dictionary={"plans": {}, "options": {}})
    assert r_glitch["status"] == "skipped_glitch"
    assert len(dispatched_events) == 0

    # Round 3: Add 24ska01 (KS-A) and remove 24sk10 (1st missing check -> not yet confirmed removed)
    round3_items = base_items[1:] + [
        {
            "fqn": "24ska01.ram-32g-noecc-2133.softraid-1x480ssd",
            "planCode": "24ska01",
            "server": "24ska01",
            "memory": "ram-32g-noecc-2133",
            "storage": "softraid-1x480ssd",
            "datacenters": [{"datacenter": "sbg", "availability": "72H"}],
        }
    ]
    r3 = watcher.check_once(raw_items=round3_items, dictionary={"plans": {}, "options": {}})
    assert r3["status"] == "ok"
    assert len(r3["added"]) == 1
    assert r3["added"][0]["name"] == "KS-A"
    assert len(r3["removed"]) == 0  # Only 1st miss for 24sk10
    assert len(dispatched_events) == 1

    # Round 4: 24sk10 is still missing for 2nd consecutive check -> now confirmed removed!
    r4 = watcher.check_once(raw_items=round3_items, dictionary={"plans": {}, "options": {}})
    assert r4["status"] == "ok"
    assert len(r4["removed"]) == 1
    assert r4["removed"][0]["skuKey"] == "24sk10"
    assert len(dispatched_events) == 2
    assert len(watcher.get_changes()) == 2



