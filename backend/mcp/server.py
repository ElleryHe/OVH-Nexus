"""
OVH Nexus MCP Streamable HTTP Server & WebUI Management Blueprint
=================================================================
1. Streamable HTTP Endpoint (`/api/mcp`):
   - Authenticated via dedicated `Authorization: Bearer <MCP_TOKEN>` (or `X-MCP-Token`)
   - Protected by brute-force rate limiting (5 failed attempts / 5 min per IP)
   - Implements MCP JSON-RPC 2.0 (`initialize`, `notifications/initialized`, `ping`, `tools/list`, `tools/call`)
2. WebUI Management Endpoints (`/api/mcp/settings`, `/api/mcp/token/rotate`, etc.):
   - Protected by standard OVH Nexus WebUI session / loopback authentication
"""

from __future__ import annotations

from collections import defaultdict, deque
import json
import threading
import time
from typing import Any, Dict, Optional, Tuple

from flask import Blueprint, Response, jsonify, request

from .audit import clear_audit_logs, load_audit_logs, record_mcp_audit
from .config import (
    get_public_mcp_config,
    load_mcp_config,
    revoke_mcp_token,
    rotate_mcp_token,
    update_mcp_config,
    verify_mcp_token,
)
from .privacy_shield import privacy_shield
from .tools import TOOL_DEFINITIONS, execute_mcp_tool, get_enabled_tools_for_mcp

mcp_bp = Blueprint("mcp_gateway", __name__)

MCP_PROTOCOL_VERSION = "2025-03-26"
MCP_AUTH_MAX_ATTEMPTS = 5
MCP_AUTH_WINDOW_SECONDS = 300

_mcp_attempts = defaultdict(deque)
_mcp_attempts_lock = threading.Lock()


def _get_app_module():
    import sys
    main_mod = sys.modules.get("__main__")
    if main_mod and hasattr(main_mod, "get_ovh_client"):
        return main_mod
    import app as A
    return A


def _client_ip() -> str:
    remote = request.remote_addr or "unknown"
    forwarded = request.headers.get("X-Real-IP")
    if remote in ("127.0.0.1", "::1") and forwarded:
        return forwarded
    return remote


def _prune_attempts(ip: str, now: float) -> deque:
    for k in list(_mcp_attempts.keys()):
        b = _mcp_attempts[k]
        while b and now - b[0] > MCP_AUTH_WINDOW_SECONDS:
            b.popleft()
        if not b:
            _mcp_attempts.pop(k, None)
    return _mcp_attempts.get(ip, deque())


def _extract_mcp_bearer_token() -> str:
    auth_header = (request.headers.get("Authorization") or "").strip()
    if auth_header.lower().startswith("bearer "):
        return auth_header[7:].strip()
    return (request.headers.get("X-MCP-Token") or "").strip()


def _jsonrpc_response(req_id: Any, result: Any) -> Dict[str, Any]:
    return {
        "jsonrpc": "2.0",
        "id": req_id,
        "result": result,
    }


def _jsonrpc_error(req_id: Any, code: int, message: str, data: Any = None) -> Dict[str, Any]:
    err: Dict[str, Any] = {"code": code, "message": message}
    if data is not None:
        err["data"] = data
    return {
        "jsonrpc": "2.0",
        "id": req_id,
        "error": err,
    }


def _handle_single_jsonrpc(msg: Dict[str, Any], ip: str) -> Optional[Dict[str, Any]]:
    """Process a single JSON-RPC 2.0 message. Returns None for notifications."""
    if not isinstance(msg, dict) or msg.get("jsonrpc") != "2.0":
        return _jsonrpc_error(msg.get("id") if isinstance(msg, dict) else None, -32600, "Invalid JSON-RPC 2.0 request")

    method = str(msg.get("method") or "").strip()
    req_id = msg.get("id")
    params = msg.get("params") if isinstance(msg.get("params"), dict) else {}

    # JSON-RPC notifications (no 'id' field) do not produce a response object
    is_notification = "id" not in msg

    if method == "initialize":
        client_proto = str(params.get("protocolVersion") or MCP_PROTOCOL_VERSION)
        supported = {"2024-11-05", "2025-03-26", "2025-06-18"}
        negotiated = client_proto if client_proto in supported else MCP_PROTOCOL_VERSION
        return _jsonrpc_response(
            req_id,
            {
                "protocolVersion": negotiated,
                "capabilities": {
                    "tools": {"listChanged": False},
                },
                "serverInfo": {
                    "name": "ovh-nexus-mcp",
                    "version": "1.2.3",
                },
                "instructions": (
                    "OVH Nexus MCP Gateway (Privacy-Shielded & No-Auto-Pay Enforced). "
                    "All server hostnames, IPs, accounts, and orders use persistent aliases (acct-01, bm-01, vps-01, ip-01, ord-01). "
                    "Pass these aliases directly to tool parameters. Direct ordering requires calling `order_quote` first, "
                    "confirming the price with the user, and then calling `order_create_unpaid` (which never charges any payment method)."
                ),
            },
        )

    if method.startswith("notifications/") or is_notification:
        return None

    if method == "ping":
        return _jsonrpc_response(req_id, {})

    if method == "resources/list":
        return _jsonrpc_response(req_id, {"resources": []})

    if method == "prompts/list":
        return _jsonrpc_response(req_id, {"prompts": []})

    if method == "tools/list":
        return _jsonrpc_response(req_id, {"tools": get_enabled_tools_for_mcp()})

    if method == "tools/call":
        tool_name = str(params.get("name") or "").strip()
        tool_args = params.get("arguments") if isinstance(params.get("arguments"), dict) else {}
        if not tool_name:
            return _jsonrpc_error(req_id, -32602, "Missing tool `name` in tools/call")

        cfg = load_mcp_config()
        A = _get_app_module()
        notify_hub = getattr(A, "notification_hub", None)

        try:
            result_data, scope_name, summary = execute_mcp_tool(tool_name, tool_args)
            safe_text = privacy_shield.scrub_text(json.dumps(result_data, ensure_ascii=False, indent=2))
            record_mcp_audit(
                tool_name=tool_name,
                scope=scope_name,
                arguments=tool_args,
                status="success",
                summary=privacy_shield.scrub_text(summary),
                client_ip=ip,
                notify_hub=notify_hub,
                notify_enabled=bool(cfg.get("notifyOnMutation", True)),
            )
            return _jsonrpc_response(
                req_id,
                {
                    "content": [
                        {
                            "type": "text",
                            "text": safe_text,
                        }
                    ],
                    "isError": False,
                },
            )
        except PermissionError as pe:
            err_msg = privacy_shield.scrub_text(str(pe))
            record_mcp_audit(
                tool_name=tool_name,
                scope="blocked",
                arguments=tool_args,
                status="blocked",
                summary=err_msg,
                client_ip=ip,
                notify_hub=None,
                notify_enabled=False,
            )
            return _jsonrpc_response(
                req_id,
                {
                    "content": [{"type": "text", "text": json.dumps({"error": "PERMISSION_DENIED", "message": err_msg}, ensure_ascii=False)}],
                    "isError": True,
                },
            )
        except Exception as e:
            err_msg = privacy_shield.scrub_text(str(e))
            record_mcp_audit(
                tool_name=tool_name,
                scope="error",
                arguments=tool_args,
                status="error",
                summary=err_msg,
                client_ip=ip,
                notify_hub=None,
                notify_enabled=False,
            )
            return _jsonrpc_response(
                req_id,
                {
                    "content": [{"type": "text", "text": json.dumps({"error": "TOOL_EXECUTION_ERROR", "message": err_msg}, ensure_ascii=False)}],
                    "isError": True,
                },
            )

    return _jsonrpc_error(req_id, -32601, f"Method not found: {method}")


# ==================== 1. Streamable HTTP MCP Endpoint (/api/mcp) ====================

@mcp_bp.route("/api/mcp", methods=["POST", "GET", "DELETE", "OPTIONS"])
def handle_mcp_streamable_http():
    if request.method == "OPTIONS":
        return jsonify({}), 200

    cfg = load_mcp_config()
    if not cfg.get("enabled") or not cfg.get("tokenHash"):
        return (
            jsonify({
                "jsonrpc": "2.0",
                "id": None,
                "error": {
                    "code": -32003,
                    "message": "MCP Gateway is disabled. Enable it and generate a Bearer token in the OVH Nexus Web Console (/mcp).",
                },
            }),
            403,
        )

    ip = _client_ip()
    now = time.time()
    with _mcp_attempts_lock:
        bucket = _prune_attempts(ip, now)
        if len(bucket) >= MCP_AUTH_MAX_ATTEMPTS:
            retry_after = max(1, int(MCP_AUTH_WINDOW_SECONDS - (now - bucket[0])))
            resp = jsonify({
                "jsonrpc": "2.0",
                "id": None,
                "error": {
                    "code": -32029,
                    "message": f"Too many failed MCP authentication attempts. Retry in {retry_after}s.",
                },
            })
            resp.status_code = 429
            resp.headers["Retry-After"] = str(retry_after)
            return resp

    candidate_token = _extract_mcp_bearer_token()
    if not verify_mcp_token(candidate_token):
        with _mcp_attempts_lock:
            _mcp_attempts[ip].append(now)
        return (
            jsonify({
                "jsonrpc": "2.0",
                "id": None,
                "error": {
                    "code": -32001,
                    "message": "Unauthorized: valid `Authorization: Bearer <MCP_TOKEN>` header required.",
                },
            }),
            401,
        )

    with _mcp_attempts_lock:
        _mcp_attempts.pop(ip, None)

    # Stateless Streamable HTTP: GET / DELETE return 405 Method Not Allowed per MCP spec
    if request.method in ("GET", "DELETE"):
        return (
            jsonify({
                "jsonrpc": "2.0",
                "id": None,
                "error": {"code": -32600, "message": "Stateless Streamable HTTP endpoint accepts POST requests."},
            }),
            405,
        )

    payload = request.get_json(silent=True)
    if payload is None:
        return jsonify(_jsonrpc_error(None, -32700, "Parse error: invalid JSON body")), 400

    if isinstance(payload, list):
        if not payload:
            return jsonify(_jsonrpc_error(None, -32600, "Empty JSON-RPC batch")), 400
        responses = []
        for item in payload:
            r = _handle_single_jsonrpc(item, ip)
            if r is not None:
                responses.append(r)
        if not responses:
            return Response(status=202)
        return jsonify(responses), 200

    resp_obj = _handle_single_jsonrpc(payload, ip)
    if resp_obj is None:
        return Response(status=202)
    return jsonify(resp_obj), 200


# ==================== 2. WebUI MCP Management API (/api/mcp/*) ====================

@mcp_bp.route("/api/mcp/settings", methods=["GET", "PUT", "POST"])
def manage_mcp_settings():
    if request.method == "GET":
        pub = get_public_mcp_config()
        pub["toolCatalog"] = [
            {"name": t["name"], "scope": t["scope"], "description": t["description"]}
            for t in TOOL_DEFINITIONS
        ]
        return jsonify({"status": "success", "data": pub})

    body = request.get_json(silent=True) or {}
    updated = update_mcp_config(body)
    pub = get_public_mcp_config()
    # If enabling for the first time auto-created a token, reveal it once so the user can copy it
    if updated.get("_newlyGeneratedPlaintextToken"):
        pub["revealedToken"] = updated["_newlyGeneratedPlaintextToken"]
    return jsonify({"status": "success", "data": pub})


@mcp_bp.route("/api/mcp/token/rotate", methods=["POST"])
def rotate_token_endpoint():
    new_token = rotate_mcp_token()
    pub = get_public_mcp_config()
    pub["revealedToken"] = new_token
    return jsonify({"status": "success", "data": pub})


@mcp_bp.route("/api/mcp/token/revoke", methods=["POST"])
def revoke_token_endpoint():
    revoke_mcp_token()
    return jsonify({"status": "success", "data": get_public_mcp_config()})


@mcp_bp.route("/api/mcp/aliases", methods=["GET", "DELETE"])
def list_mcp_aliases():
    if request.method == "DELETE":
        privacy_shield.clear_all()
        return jsonify({"status": "success", "data": []})
    # Ensure currently bound accounts are registered so the user sees them immediately
    A = _get_app_module()
    accounts_dict = getattr(A, "accounts", {}) or {}
    default_id = getattr(A, "default_account_id", None)
    for aid, acc in accounts_dict.items():
        privacy_shield.redact_account(aid, acc, is_default=(aid == default_id))
    return jsonify({"status": "success", "data": privacy_shield.list_mappings_for_ui()})


@mcp_bp.route("/api/mcp/audit", methods=["GET", "DELETE"])
def manage_mcp_audit():
    if request.method == "DELETE":
        clear_audit_logs()
        return jsonify({"status": "success", "data": []})
    try:
        limit = min(max(int(request.args.get("limit", 100)), 1), 300)
    except Exception:
        limit = 100
    return jsonify({"status": "success", "data": load_audit_logs(limit=limit)})
