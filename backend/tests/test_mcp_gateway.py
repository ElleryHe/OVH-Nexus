import json
import os
import sys

import pytest

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

import app as A  # noqa: E402
from mcp import audit as mcp_audit  # noqa: E402
from mcp import config as mcp_cfg  # noqa: E402
from mcp import privacy_shield as mcp_shield  # noqa: E402
from mcp import server as mcp_srv  # noqa: E402

PASSWORD = os.environ["API_SECRET_KEY"]


@pytest.fixture()
def client():
    return A.app.test_client()


@pytest.fixture(autouse=True)
def reset_mcp_and_app_state(tmp_path, monkeypatch):
    # Isolate MCP config, aliases, and audit files per test in tmp_path
    cfg_file = str(tmp_path / "mcp_config.json")
    aliases_file = str(tmp_path / "mcp_aliases.json")
    audit_file = str(tmp_path / "mcp_audit.json")

    monkeypatch.setattr(mcp_cfg, "MCP_CONFIG_FILE", cfg_file)
    monkeypatch.setattr(mcp_audit, "AUDIT_FILE", audit_file)
    monkeypatch.setattr(mcp_shield.privacy_shield, "_storage_path", aliases_file)
    mcp_shield.privacy_shield.reload()
    mcp_audit.reset_quotes_and_rate_limits_for_test()
    mcp_srv._mcp_attempts.clear()

    A.config.update({
        "appKey": "", "appSecret": "", "consumerKey": "",
        "endpoint": "ovh-eu", "tgToken": "", "tgChatId": "",
        "iam": "go-ovh-ie", "zone": "IE",
    })
    A.accounts.clear()
    A.default_account_id = None
    A.queue.clear()
    A.purchase_history.clear()
    monkeypatch.setattr(A, "save_data", lambda: None)
    monkeypatch.setattr(A, "update_stats", lambda: None)
    monkeypatch.setattr(
        A.catalog_service,
        "get_dictionary",
        lambda *a, **k: {"updatedAt": 9999999999, "plans": {}, "options": {}},
    )
    yield


def login(client, ip="127.0.0.1"):
    return client.post(
        "/api/auth/login",
        json={"password": PASSWORD},
        environ_base={"REMOTE_ADDR": ip},
    )


def mcp_rpc(client, token, method, params=None, req_id=1, ip="198.51.100.10"):
    payload = {"jsonrpc": "2.0", "id": req_id, "method": method}
    if params is not None:
        payload["params"] = params
    headers = {}
    if token:
        headers["Authorization"] = f"Bearer {token}"
    return client.post(
        "/api/mcp",
        json=payload,
        headers=headers,
        environ_base={"REMOTE_ADDR": ip},
    )


def test_mcp_disabled_by_default_and_management_requires_web_session(client):
    # 1. Management API (/api/mcp/settings) must require panel login
    assert client.get("/api/mcp/settings").status_code == 401
    assert client.post("/api/mcp/token/rotate").status_code == 401

    # 2. Streamable HTTP (/api/mcp) returns 403 when disabled
    r = mcp_rpc(client, "any-token", "initialize")
    assert r.status_code == 403
    assert "disabled" in r.get_json()["error"]["message"].lower()


def test_mcp_token_isolation_and_brute_force_rate_limit(client):
    login(client)
    # Enable MCP -> auto-generates dedicated nxs_mcp_ token
    r_en = client.put("/api/mcp/settings", json={"enabled": True})
    assert r_en.status_code == 200
    data = r_en.get_json()["data"]
    assert data["enabled"] is True
    token = data["revealedToken"]
    assert token.startswith("nxs_mcp_")

    # Subsequent GET masks the token
    r_get = client.get("/api/mcp/settings").get_json()["data"]
    assert "revealedToken" not in r_get
    assert "****" in r_get["maskedToken"]

    # Panel master password API_SECRET_KEY MUST NOT be accepted as MCP Bearer token
    r_bad_master = mcp_rpc(client, PASSWORD, "ping", ip="198.51.100.50")
    assert r_bad_master.status_code == 401

    # 5 failed attempts trigger 429 rate limit
    for _ in range(4):
        assert mcp_rpc(client, "wrong-token", "ping", ip="198.51.100.50").status_code == 401
    r_limited = mcp_rpc(client, "wrong-token", "ping", ip="198.51.100.50")
    assert r_limited.status_code == 429
    assert "Retry-After" in r_limited.headers

    # Valid token from another IP succeeds
    r_ok = mcp_rpc(client, token, "initialize", {"protocolVersion": "2025-03-26"}, ip="198.51.100.51")
    assert r_ok.status_code == 200
    assert r_ok.get_json()["result"]["serverInfo"]["name"] == "ovh-nexus-mcp"


def test_mcp_privacy_shield_bidirectional_aliases_and_account_whitelist(client, monkeypatch):
    login(client)
    token = client.put(
        "/api/mcp/settings",
        json={
            "enabled": True,
            "scopes": {"read": True, "procurement": True, "operations": True},
            "allowAllAccounts": False,
            "allowedAccountIds": ["acc-eu"],
        },
    ).get_json()["data"]["revealedToken"]

    A.accounts["acc-eu"] = {
        "id": "acc-eu",
        "alias": "EU-Prod",
        "appKey": "SECRET-AK-EU",
        "appSecret": "SECRET-AS-EU",
        "consumerKey": "SECRET-CK-EU",
        "endpoint": "ovh-eu",
        "zone": "IE",
    }
    A.accounts["acc-us-secret"] = {
        "id": "acc-us-secret",
        "alias": "US-Hidden",
        "appKey": "SECRET-AK-US",
        "appSecret": "SECRET-AS-US",
        "consumerKey": "SECRET-CK-US",
        "endpoint": "ovh-us",
        "zone": "US",
    }
    A.default_account_id = "acc-eu"

    rebooted_servers = []

    class FakeEUClient:
        def get(self, path):
            if path == "/dedicated/server":
                return ["ns3198452.ip-51-210-88.eu"]
            if path == "/dedicated/server/ns3198452.ip-51-210-88.eu":
                return {
                    "name": "ns3198452.ip-51-210-88.eu",
                    "commercialRange": "KS-A",
                    "datacenter": "gra3",
                    "state": "ok",
                    "os": "debian12_64",
                    "monitoring": True,
                    "ip": "51.210.88.42",
                    "reverse": "ns3198452.ip-51-210-88.eu",
                    "iam": {"displayName": "My-GRA-Node"},
                }
            if path == "/dedicated/server/ns3198452.ip-51-210-88.eu/serviceInfos":
                return {
                    "status": "ok",
                    "expiration": "2027-01-15",
                    "renew": {"automatic": True, "deleteAtExpiration": False},
                    "contactAdmin": "ab12345-ovh",
                }
            if path == "/dedicated/server/ns3198452.ip-51-210-88.eu/specifications/hardware":
                return {"processorName": "Intel Xeon E3", "memorySize": {"value": 32, "unit": "GB"}, "diskGroups": []}
            if path == "/dedicated/server/ns3198452.ip-51-210-88.eu/task":
                return []
            if path == "/me/order":
                return [238910452]
            if path == "/me/order/238910452":
                return {
                    "orderId": 238910452,
                    "date": "2026-11-15T10:00:00Z",
                    "expirationDate": "2026-11-22T10:00:00Z",
                    "url": "https://www.ovh.com/cgi-bin/order/displayOrder.cgi?orderId=238910452&orderPassword=SUPER_SECRET_ORDER_TOKEN",
                    "pdfUrl": "https://www.ovh.com/cgi-bin/order/displayOrder.cgi?orderId=238910452&orderPassword=SUPER_SECRET_ORDER_TOKEN&pdf=1",
                    "priceWithTax": {"value": 29.99, "currencyCode": "EUR", "text": "29.99 €"},
                }
            if path == "/me/order/238910452/status":
                return "notPaid"
            if path == "/me/order/238910452/followUp":
                return [{"step": "VALIDATING", "status": "DOING", "history": []}]
            raise AssertionError(f"Unexpected path: {path}")

        def post(self, path, **kwargs):
            if path == "/dedicated/server/ns3198452.ip-51-210-88.eu/reboot":
                rebooted_servers.append("ns3198452.ip-51-210-88.eu")
                return {"taskId": 777}
            return {}

    monkeypatch.setattr(A, "get_ovh_client", lambda aid=None: FakeEUClient() if aid in (None, "acc-eu") else None)

    # 1. profile_list only exposes whitelisted 'acc-eu' as 'acct-01', hiding 'acc-us-secret' and all API keys
    r_prof = mcp_rpc(client, token, "tools/call", {"name": "profile_list", "arguments": {}})
    prof_text = r_prof.get_json()["result"]["content"][0]["text"]
    assert "acct-01" in prof_text
    assert "EU-Prod" in prof_text
    assert "US-Hidden" not in prof_text
    assert "SECRET-AK-EU" not in prof_text

    # 2. baremetal_list returns 'bm-01', custom label 'My-GRA-Node', masked tail '*.*.*.42', and NEVER leaks 'ns3198452' or '51.210.88.42'
    r_bm = mcp_rpc(client, token, "tools/call", {"name": "baremetal_list", "arguments": {}})
    bm_text = r_bm.get_json()["result"]["content"][0]["text"]
    assert "bm-01" in bm_text
    assert "My-GRA-Node" in bm_text
    assert "*.*.*.42" in bm_text
    assert "ns3198452" not in bm_text
    assert "51.210.88.42" not in bm_text
    assert "ab12345-ovh" not in bm_text

    # 3. Bidirectional resolution: calling baremetal_reboot(server='bm-01') reboots real 'ns3198452.ip-51-210-88.eu'
    r_reb = mcp_rpc(client, token, "tools/call", {"name": "baremetal_reboot", "arguments": {"server": "bm-01"}})
    reb_text = r_reb.get_json()["result"]["content"][0]["text"]
    assert r_reb.get_json()["result"]["isError"] is False
    assert rebooted_servers == ["ns3198452.ip-51-210-88.eu"]
    assert "ns3198452" not in reb_text

    # 4. order_list strips orderPassword URLs and maps 238910452 -> 'ord-01'
    r_ord = mcp_rpc(client, token, "tools/call", {"name": "order_list", "arguments": {}})
    ord_text = r_ord.get_json()["result"]["content"][0]["text"]
    assert "ord-01" in ord_text
    assert "238910452" not in ord_text
    assert "SUPER_SECRET_ORDER_TOKEN" not in ord_text
    assert "displayOrder.cgi" not in ord_text


def test_mcp_two_step_unpaid_ordering_and_hardcoded_no_autopay(client, monkeypatch):
    login(client)
    # Initially only L1 'read' is enabled -> L2 'order_quote' must be blocked and hidden from tools/list
    token = client.put(
        "/api/mcp/settings",
        json={"enabled": True, "scopes": {"read": True, "procurement": False, "operations": False}},
    ).get_json()["data"]["revealedToken"]

    tools_l1 = mcp_rpc(client, token, "tools/list").get_json()["result"]["tools"]
    tool_names_l1 = {t["name"] for t in tools_l1}
    assert "baremetal_list" in tool_names_l1
    assert "order_quote" not in tool_names_l1
    assert "baremetal_reboot" not in tool_names_l1
    # L4 tools must never exist
    assert "baremetal_reinstall" not in tool_names_l1
    assert "baremetal_terminate" not in tool_names_l1

    # Calling order_quote while procurement scope is disabled returns isError=True
    r_denied = mcp_rpc(
        client,
        token,
        "tools/call",
        {"name": "order_quote", "arguments": {"plan_code": "24ska01", "datacenter": "gra"}},
    )
    assert r_denied.get_json()["result"]["isError"] is True
    assert "PERMISSION_DENIED" in r_denied.get_json()["result"]["content"][0]["text"]

    # Enable L2 procurement with hourlyOrderLimit=1
    client.put(
        "/api/mcp/settings",
        json={"scopes": {"read": True, "procurement": True, "operations": False}, "hourlyOrderLimit": 1},
    )
    A.accounts["acc-eu"] = {
        "id": "acc-eu", "alias": "EU", "appKey": "AK", "appSecret": "AS", "consumerKey": "CK", "endpoint": "ovh-eu", "zone": "IE",
    }
    A.default_account_id = "acc-eu"

    monkeypatch.setattr(
        A,
        "_get_server_price_internal",
        lambda plan, dc, opts, aid: {
            "success": True,
            "price": {"prices": {"withTax": 24.99, "withoutTax": 20.99, "currencyCode": "EUR"}},
        },
    )

    captured_purchases = []

    def fake_purchase_server(task_item):
        captured_purchases.append(dict(task_item))
        A.purchase_history.append({
            "id": "h1",
            "taskId": task_item["id"],
            "planCode": task_item["planCode"],
            "datacenter": task_item["datacenters"][0],
            "options": task_item["options"],
            "status": "success",
            "orderId": "99887766",
            "orderUrl": "https://www.ovh.com/order?orderPassword=LEAK_TEST_SECRET",
            "price": {"withTax": 24.99, "currencyCode": "EUR"},
            "accountId": task_item["accountId"],
        })
        return True

    monkeypatch.setattr(A, "purchase_server", fake_purchase_server)

    # Step 1: order_quote
    r_quote = mcp_rpc(
        client,
        token,
        "tools/call",
        {"name": "order_quote", "arguments": {"plan_code": "24ska01", "datacenter": "gra"}},
    )
    quote_payload = json.loads(r_quote.get_json()["result"]["content"][0]["text"])
    assert quote_payload["autoPay"] is False
    assert quote_payload["quantity"] == 1
    quote_id = quote_payload["quoteId"]

    # Step 2: order_create_unpaid with valid quote_id
    r_order = mcp_rpc(
        client,
        token,
        "tools/call",
        {"name": "order_create_unpaid", "arguments": {"quote_id": quote_id}},
    )
    order_text = r_order.get_json()["result"]["content"][0]["text"]
    order_payload = json.loads(order_text)
    assert order_payload["status"] == "unpaid_order_created"
    assert order_payload["autoPay"] is False
    assert order_payload["quantity"] == 1
    assert "LEAK_TEST_SECRET" not in order_text
    assert "99887766" not in order_text
    assert len(captured_purchases) == 1
    assert captured_purchases[0]["auto_pay"] is False
    assert captured_purchases[0]["quantity"] == 1

    # Re-using the same quote_id or exceeding hourlyOrderLimit=1 is blocked
    r_rate_limited = mcp_rpc(
        client,
        token,
        "tools/call",
        {"name": "order_create_unpaid", "arguments": {"quote_id": quote_id}},
    )
    assert r_rate_limited.get_json()["result"]["isError"] is True
    assert "Hourly MCP unpaid order limit" in r_rate_limited.get_json()["result"]["content"][0]["text"]

    # queue_add also forces auto_pay=False and quantity=1
    r_q = mcp_rpc(
        client,
        token,
        "tools/call",
        {"name": "queue_add", "arguments": {"plan_code": "24ska01", "datacenters": ["gra", "rbx"]}},
    )
    assert r_q.get_json()["result"]["isError"] is False
    assert len(A.queue) == 1
    assert A.queue[0]["auto_pay"] is False
    assert A.queue[0]["quantity"] == 1

    # DELETE /api/mcp/aliases clears alias mappings
    r_del_aliases = client.delete("/api/mcp/aliases")
    assert r_del_aliases.status_code == 200
    assert r_del_aliases.get_json()["data"] == []
