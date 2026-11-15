import json
import os
import sys
import time

import pytest

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

import app as A  # noqa: E402
import crypto_vault as CV  # noqa: E402
from mcp import config as mcp_cfg  # noqa: E402
import totp_service as TOTP  # noqa: E402

PASSWORD = os.environ["API_SECRET_KEY"]


@pytest.fixture()
def client():
    return A.app.test_client()


@pytest.fixture(autouse=True)
def isolate_files(tmp_path, monkeypatch):
    cfg_file = str(tmp_path / "config.json")
    acc_file = str(tmp_path / "accounts.json")
    mcp_file = str(tmp_path / "mcp_config.json")
    totp_file = str(tmp_path / "totp_config.json")

    monkeypatch.setattr(A, "CONFIG_FILE", cfg_file)
    monkeypatch.setattr(A, "ACCOUNTS_FILE", acc_file)
    monkeypatch.setattr(mcp_cfg, "MCP_CONFIG_FILE", mcp_file)
    monkeypatch.setattr(TOTP, "TOTP_CONFIG_FILE", totp_file)
    TOTP._totp_failures.clear()

    A.config.update({
        "appKey": "", "appSecret": "", "consumerKey": "",
        "endpoint": "ovh-eu", "tgToken": "", "tgChatId": "",
        "iam": "go-ovh-ie", "zone": "IE",
    })
    A.accounts.clear()
    A.default_account_id = None
    A.queue.clear()
    A.purchase_history.clear()
    yield


def test_data_encryption_key_validation(monkeypatch):
    # 1. Missing key -> RuntimeError
    monkeypatch.setenv("DATA_ENCRYPTION_KEY", "")
    with pytest.raises(RuntimeError, match="DATA_ENCRYPTION_KEY 未配置"):
        CV.validate_encryption_key(PASSWORD)

    # 2. Too short / weak key -> RuntimeError
    monkeypatch.setenv("DATA_ENCRYPTION_KEY", "short-key")
    with pytest.raises(RuntimeError, match="DATA_ENCRYPTION_KEY 过弱"):
        CV.validate_encryption_key(PASSWORD)

    # 3. Identical to API_SECRET_KEY -> RuntimeError
    monkeypatch.setenv("DATA_ENCRYPTION_KEY", PASSWORD)
    with pytest.raises(RuntimeError, match="严禁与登录口令 API_SECRET_KEY 相同"):
        CV.validate_encryption_key(PASSWORD)


def test_at_rest_encryption_and_legacy_plaintext_migration(tmp_path, monkeypatch):
    # Write legacy plaintext config.json, accounts.json, and mcp_config.json
    legacy_cfg = {
        "appKey": "PLAIN-APP-KEY-1",
        "appSecret": "PLAIN-APP-SECRET-1",
        "consumerKey": "PLAIN-CONSUMER-KEY-1",
        "endpoint": "ovh-eu",
        "tgToken": "123456:PLAIN-TG-BOT-TOKEN",
        "tgChatId": "998877",
        "zone": "IE",
        "notifications": {
            "channels": [
                {
                    "id": "tg-1",
                    "type": "telegram",
                    "config": {"token": "123456:CHANNEL-TG-SECRET", "chatId": "998877"},
                },
                {
                    "id": "smtp-1",
                    "type": "smtp",
                    "config": {"host": "smtp.example.com", "password": "PLAIN-SMTP-PASSWORD"},
                },
            ]
        },
    }
    legacy_accounts = {
        "accounts": [
            {
                "id": "eu-main",
                "alias": "Main EU",
                "appKey": "ACC-PLAIN-AK",
                "appSecret": "ACC-PLAIN-AS",
                "consumerKey": "ACC-PLAIN-CK",
                "endpoint": "ovh-eu",
                "zone": "FR",
            }
        ],
        "defaultAccountId": "eu-main",
    }
    legacy_mcp = {
        "enabled": True,
        "token": "nxs_mcp_legacy_plaintext_bearer_token_1234567890",
        "scopes": {"read": True, "procurement": False, "operations": False},
    }

    with open(A.CONFIG_FILE, "w", encoding="utf-8") as f:
        json.dump(legacy_cfg, f)
    with open(A.ACCOUNTS_FILE, "w", encoding="utf-8") as f:
        json.dump(legacy_accounts, f)
    with open(mcp_cfg.MCP_CONFIG_FILE, "w", encoding="utf-8") as f:
        json.dump(legacy_mcp, f)

    # Trigger load_data() and load_mcp_config() -> should auto-migrate all 3 files on disk!
    A.load_data()
    mcp_loaded = mcp_cfg.load_mcp_config()

    # 1. In-memory runtime retains plaintext for OVH SDK & notifications
    assert A.config["appSecret"] == "PLAIN-APP-SECRET-1"
    assert A.config["tgToken"] == "123456:PLAIN-TG-BOT-TOKEN"
    assert A.config["notifications"]["channels"][0]["config"]["token"] == "123456:CHANNEL-TG-SECRET"
    assert A.config["notifications"]["channels"][1]["config"]["password"] == "PLAIN-SMTP-PASSWORD"
    assert A.accounts["eu-main"]["appSecret"] == "ACC-PLAIN-AS"

    # 2. Disk files are now encrypted with AES-256-GCM (`enc:v1:...`) and contain ZERO plaintext secrets
    disk_cfg_raw = open(A.CONFIG_FILE, "r", encoding="utf-8").read()
    assert "PLAIN-APP-SECRET-1" not in disk_cfg_raw
    assert "PLAIN-TG-BOT-TOKEN" not in disk_cfg_raw
    assert "CHANNEL-TG-SECRET" not in disk_cfg_raw
    assert "PLAIN-SMTP-PASSWORD" not in disk_cfg_raw
    disk_cfg_json = json.loads(disk_cfg_raw)
    assert disk_cfg_json["appSecret"].startswith("enc:v1:")
    assert disk_cfg_json["notifications"]["channels"][1]["config"]["password"].startswith("enc:v1:")

    disk_acc_raw = open(A.ACCOUNTS_FILE, "r", encoding="utf-8").read()
    assert "ACC-PLAIN-AK" not in disk_acc_raw
    assert "ACC-PLAIN-AS" not in disk_acc_raw
    assert "ACC-PLAIN-CK" not in disk_acc_raw
    disk_acc_json = json.loads(disk_acc_raw)
    assert disk_acc_json["accounts"][0]["appSecret"].startswith("enc:v1:")

    # 3. MCP config migrated plaintext `token` -> SHA-256 `tokenHash` + `maskedToken`
    disk_mcp_raw = open(mcp_cfg.MCP_CONFIG_FILE, "r", encoding="utf-8").read()
    assert "nxs_mcp_legacy_plaintext_bearer_token_1234567890" not in disk_mcp_raw
    disk_mcp_json = json.loads(disk_mcp_raw)
    assert "token" not in disk_mcp_json
    assert len(disk_mcp_json["tokenHash"]) == 64
    assert mcp_cfg.verify_mcp_token("nxs_mcp_legacy_plaintext_bearer_token_1234567890") is True
    assert mcp_cfg.verify_mcp_token("nxs_mcp_wrong_token_1234567890") is False

    # 4. Tampering with AES-256-GCM ciphertext raises ValueError
    tampered = disk_cfg_json["appSecret"][:-4] + "AAAA"
    with pytest.raises(ValueError, match="解密失败"):
        CV.decrypt_value(tampered)


def test_totp_2fa_enrollment_stepup_tiers_and_mcp_l3_lock(client, monkeypatch):
    # 1. Log in before 2FA is enabled
    r_login = client.post("/api/auth/login", json={"password": PASSWORD})
    assert r_login.status_code == 200
    assert r_login.get_json()["totpEnabled"] is False

    # 2. While 2FA is NOT enabled, all high-risk routes (strict & sudo) return 403 TOTP_SETUP_REQUIRED
    r_install = client.post("/api/server-control/ns123.ip-1-2-3.eu/install", json={"templateName": "debian12_64"})
    assert r_install.status_code == 403
    assert r_install.get_json()["code"] == "TOTP_SETUP_REQUIRED"
    assert r_install.get_json()["tier"] == "strict"

    r_reboot = client.post("/api/server-control/ns123.ip-1-2-3.eu/reboot")
    assert r_reboot.status_code == 403
    assert r_reboot.get_json()["code"] == "TOTP_SETUP_REQUIRED"
    assert r_reboot.get_json()["tier"] == "sudo"

    r_mcp_put = client.put("/api/mcp/settings", json={"enabled": True})
    assert r_mcp_put.status_code == 403
    assert r_mcp_put.get_json()["code"] == "TOTP_SETUP_REQUIRED"

    # Unpaid queue item with default flags (auto_pay=False, waive_retractation_period=False) is allowed without 2FA
    monkeypatch.setattr(A, "save_data", lambda: None)
    monkeypatch.setattr(A, "update_stats", lambda: None)
    r_q_safe = client.post("/api/queue", json={"planCode": "24ska01", "datacenters": ["gra"]})
    assert r_q_safe.status_code == 200
    assert A.queue[-1]["auto_pay"] is False
    assert A.queue[-1]["waive_retractation_period"] is False

    # Queue item with auto_pay=True or waive_retractation_period=True is blocked without 2FA
    r_q_autopay = client.post("/api/queue", json={"planCode": "24ska01", "datacenters": ["gra"], "auto_pay": True})
    assert r_q_autopay.status_code == 403
    assert r_q_autopay.get_json()["code"] == "TOTP_SETUP_REQUIRED"

    r_q_waive = client.post("/api/queue", json={"planCode": "24ska01", "datacenters": ["gra"], "waive_retractation_period": True})
    assert r_q_waive.status_code == 403
    assert r_q_waive.get_json()["code"] == "TOTP_SETUP_REQUIRED"

    # 3. Enroll & activate 2FA
    r_setup = client.post("/api/auth/totp/setup")
    assert r_setup.status_code == 200
    setup_data = r_setup.get_json()
    secret = setup_data["secret"]
    assert setup_data["otpauthUri"].startswith("otpauth://totp/")

    now_step = int(time.time() // 30)
    code_step0 = TOTP.compute_totp_at_step(secret, now_step)
    r_act = client.post("/api/auth/totp/activate", json={"code": code_step0})
    assert r_act.status_code == 200
    act_data = r_act.get_json()
    assert act_data["enabled"] is True
    recovery_codes = act_data["recoveryCodes"]
    assert len(recovery_codes) == 8

    # Verify totp_config.json on disk encrypts `secret` and hashes recovery codes
    disk_totp_raw = open(TOTP.TOTP_CONFIG_FILE, "r", encoding="utf-8").read()
    assert secret not in disk_totp_raw
    assert recovery_codes[0] not in disk_totp_raw

    # 4. Log out and verify 2-step login requirement
    client.post("/api/auth/logout")
    r_step1 = client.post("/api/auth/login", json={"password": PASSWORD})
    assert r_step1.status_code == 200
    assert r_step1.get_json()["authenticated"] is False
    assert r_step1.get_json()["requireTotp"] is True

    # Replaying the exact same `code_step0` used during activation is rejected by anti-replay!
    r_replay_login = client.post("/api/auth/login", json={"password": PASSWORD, "totpCode": code_step0})
    assert r_replay_login.status_code == 401
    assert r_replay_login.get_json()["code"] == "TOTP_REPLAYED"

    # Log in using a recovery code (single-use)
    rec0 = recovery_codes[0]
    r_rec_login = client.post("/api/auth/login", json={"password": PASSWORD, "totpCode": rec0})
    assert r_rec_login.status_code == 200
    assert r_rec_login.get_json()["authenticated"] is True
    assert r_rec_login.get_json()["sudoActive"] is True

    # Re-using the burned recovery code fails
    r_reuse_rec = client.post("/api/auth/totp/verify-sudo", json={"code": rec0})
    assert r_reuse_rec.status_code == 403

    # 5. Inside active 5-minute Sudo window:
    # - `sudo` tier (e.g. queue with auto_pay=True & waive_retractation_period=True) passes without extra prompt!
    r_q_elevated = client.post(
        "/api/queue",
        json={
            "planCode": "24ska01",
            "datacenters": ["rbx"],
            "auto_pay": True,
            "waive_retractation_period": True,
        },
    )
    assert r_q_elevated.status_code == 200
    assert A.queue[-1]["auto_pay"] is True
    assert A.queue[-1]["waive_retractation_period"] is True

    # - `strict` tier (e.g. DELETE /api/accounts/<id>) STILL demands a fresh X-TOTP-Code header!
    A.accounts["acc-del"] = {"id": "acc-del", "alias": "To Delete", "appKey": "k", "appSecret": "s", "consumerKey": "c"}
    monkeypatch.setattr(A, "save_accounts", lambda: None)
    r_del_no_code = client.delete("/api/accounts/acc-del")
    assert r_del_no_code.status_code == 403
    assert r_del_no_code.get_json()["code"] == "TOTP_STEP_UP_REQUIRED"
    assert r_del_no_code.get_json()["tier"] == "strict"

    # Provide next-step code (`now_step + 1`, within ±1 window) -> strict delete succeeds!
    code_step1 = TOTP.compute_totp_at_step(secret, now_step + 1)
    r_del_ok = client.delete("/api/accounts/acc-del", headers={"X-TOTP-Code": code_step1})
    assert r_del_ok.status_code == 200
    assert "acc-del" not in A.accounts


def test_purchase_server_waive_retractation_period_default_and_override(monkeypatch):
    captured_checkouts = []

    class FakeHelper:
        def get(self, path, **kwargs):
            if path == "/dedicated/server/datacenter/availabilities":
                return [{"datacenters": [{"datacenter": "gra", "availability": "available"}]}]
            if path.endswith("/summary"):
                return {"prices": {"withTax": {"value": 19.99, "currencyCode": "EUR"}}}
            return {}

        def post(self, path, **kwargs):
            if path == "/order/cart":
                return {"cartId": "cart-123"}
            if path == "/order/cart/cart-123/eco":
                return {"itemId": 1}
            if path == "/order/cart/cart-123/assign":
                return {}
            if path == "/order/cart/cart-123/checkout":
                captured_checkouts.append(dict(kwargs))
                return {"orderId": 555666, "url": "https://www.ovh.com/order/555666"}
            return {}

    monkeypatch.setattr(A, "get_ovh_client", lambda aid=None: object())
    monkeypatch.setattr(A, "get_global_helper", lambda client, max_calls_per_second=5: FakeHelper())
    monkeypatch.setattr(A, "save_data", lambda: None)
    monkeypatch.setattr(A, "update_stats", lambda: None)
    monkeypatch.setattr(A, "send_telegram_msg", lambda *a, **k: True)

    # 1. Default task (no waive_retractation_period specified) -> MUST default to False!
    task_default = {
        "id": "t-default",
        "planCode": "24ska01",
        "datacenters": ["gra"],
        "options": [],
        "retryCount": 1,
        "purchased": 0,
    }
    assert A.purchase_server(task_default) is True
    assert captured_checkouts[-1] == {
        "autoPayWithPreferredPaymentMethod": False,
        "waiveRetractationPeriod": False,
    }

    # 2. Explicit opt-in task -> passes True for both
    task_optin = {
        "id": "t-optin",
        "planCode": "24ska01",
        "datacenters": ["gra"],
        "options": [],
        "auto_pay": True,
        "waive_retractation_period": True,
        "retryCount": 1,
        "purchased": 0,
    }
    assert A.purchase_server(task_optin) is True
    assert captured_checkouts[-1] == {
        "autoPayWithPreferredPaymentMethod": True,
        "waiveRetractationPeriod": True,
    }
