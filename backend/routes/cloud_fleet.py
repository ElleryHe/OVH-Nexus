"""
OVH Nexus — Cloud Fleet, Orders, SSH Keys, Renewals, IP Security & VPS Control Blueprint.
"""

from datetime import datetime, timezone
from urllib.parse import quote
from flask import Blueprint, jsonify, request

cloud_fleet_bp = Blueprint("cloud_fleet", __name__)


def _get_app_module():
    import sys
    main_mod = sys.modules.get("__main__")
    if main_mod and hasattr(main_mod, "get_ovh_client"):
        return main_mod
    import app as A
    return A


def _days_until(date_str):
    if not date_str:
        return None
    try:
        raw = str(date_str).strip()
        if "T" in raw:
            dt = datetime.fromisoformat(raw.replace("Z", "+00:00"))
        else:
            dt = datetime.strptime(raw[:10], "%Y-%m-%d").replace(tzinfo=timezone.utc)
        if dt.tzinfo is None:
            dt = dt.replace(tzinfo=timezone.utc)
        now = datetime.now(timezone.utc)
        return int((dt - now).total_seconds() // 86400)
    except Exception:
        return None


# ==================== 1. Orders & Delivery Follow-Up (/me/order) ====================

@cloud_fleet_bp.route("/api/ovh/account/orders", methods=["GET"])
def get_account_orders():
    A = _get_app_module()
    client = A.get_ovh_client()
    if not client:
        return A.ovh_credentials_missing_response()

    try:
        limit = min(max(int(request.args.get("limit", 12)), 1), 30)
    except Exception:
        limit = 12

    try:
        order_ids = client.get("/me/order") or []
        sorted_ids = sorted(order_ids, reverse=True)[:limit]
        orders = []
        for oid in sorted_ids:
            try:
                detail = client.get(f"/me/order/{oid}") or {}
            except Exception:
                continue
            try:
                status = client.get(f"/me/order/{oid}/status")
            except Exception:
                status = "unknown"
            try:
                follow_up = client.get(f"/me/order/{oid}/followUp") or []
            except Exception:
                follow_up = []
            orders.append({
                "orderId": detail.get("orderId", oid),
                "date": detail.get("date"),
                "expirationDate": detail.get("expirationDate"),
                "url": detail.get("url"),
                "pdfUrl": detail.get("pdfUrl"),
                "priceWithTax": detail.get("priceWithTax"),
                "priceWithoutTax": detail.get("priceWithoutTax"),
                "tax": detail.get("tax"),
                "status": status,
                "followUp": follow_up,
            })
        return jsonify({"status": "success", "data": orders})
    except Exception as e:
        A.add_log("ERROR", f"Failed to fetch OVH orders: {str(e)}", "account")
        return jsonify({"status": "error", "message": str(e)}), 500


@cloud_fleet_bp.route("/api/ovh/account/orders/<int:order_id>/follow-up", methods=["GET"])
def get_order_follow_up(order_id):
    A = _get_app_module()
    client = A.get_ovh_client()
    if not client:
        return A.ovh_credentials_missing_response()

    try:
        detail = {}
        try:
            detail = client.get(f"/me/order/{order_id}") or {}
        except Exception:
            pass
        try:
            status = client.get(f"/me/order/{order_id}/status")
        except Exception:
            status = "unknown"
        try:
            follow_up = client.get(f"/me/order/{order_id}/followUp") or []
        except Exception:
            follow_up = []
        return jsonify({
            "status": "success",
            "data": {
                "orderId": order_id,
                "status": status,
                "orderStatus": status,
                "date": detail.get("date"),
                "expirationDate": detail.get("expirationDate"),
                "url": detail.get("url"),
                "pdfUrl": detail.get("pdfUrl"),
                "priceWithTax": detail.get("priceWithTax"),
                "followUp": follow_up,
            },
        })
    except Exception as e:
        A.add_log("ERROR", f"Failed to fetch order follow-up #{order_id}: {str(e)}", "account")
        return jsonify({"status": "error", "message": str(e)}), 500


# ==================== 2. SSH Public Keys (/me/sshKey) ====================

@cloud_fleet_bp.route("/api/ovh/account/ssh-keys", methods=["GET", "POST"])
def manage_ssh_keys():
    A = _get_app_module()
    client = A.get_ovh_client()
    if not client:
        return A.ovh_credentials_missing_response()

    if request.method == "GET":
        try:
            names = client.get("/me/sshKey") or []
            keys = []
            for name in names:
                try:
                    enc = quote(str(name), safe="")
                    info = client.get(f"/me/sshKey/{enc}") or {}
                    keys.append({
                        "keyName": info.get("keyName", name),
                        "key": info.get("key", ""),
                        "default": bool(info.get("default", False)),
                    })
                except Exception:
                    continue
            keys.sort(key=lambda k: (not k["default"], k["keyName"]))
            return jsonify({"status": "success", "data": keys})
        except Exception as e:
            A.add_log("ERROR", f"Failed to list SSH keys: {str(e)}", "account")
            return jsonify({"status": "error", "message": str(e)}), 500

    body = request.get_json(silent=True) or {}
    key_name = str(body.get("keyName") or "").strip()
    key_val = str(body.get("key") or "").strip()
    if not key_name or not key_val:
        return jsonify({"status": "error", "message": "keyName and key are required"}), 400

    try:
        client.post("/me/sshKey", keyName=key_name, key=key_val)
        A.add_log("INFO", f"Added SSH key: {key_name}", "account")
        return jsonify({"status": "success", "message": "SSH key added"})
    except Exception as e:
        A.add_log("ERROR", f"Failed to add SSH key {key_name}: {str(e)}", "account")
        return jsonify({"status": "error", "message": str(e)}), 500


@cloud_fleet_bp.route("/api/ovh/account/ssh-keys/<path:key_name>", methods=["PUT", "DELETE"])
def update_or_delete_ssh_key(key_name):
    A = _get_app_module()
    client = A.get_ovh_client()
    if not client:
        return A.ovh_credentials_missing_response()

    enc = quote(str(key_name), safe="")
    if request.method == "DELETE":
        try:
            client.delete(f"/me/sshKey/{enc}")
            A.add_log("INFO", f"Deleted SSH key: {key_name}", "account")
            return jsonify({"status": "success", "message": "SSH key deleted"})
        except Exception as e:
            A.add_log("ERROR", f"Failed to delete SSH key {key_name}: {str(e)}", "account")
            return jsonify({"status": "error", "message": str(e)}), 500

    body = request.get_json(silent=True) or {}
    default_flag = bool(body.get("default", True))
    try:
        client.put(f"/me/sshKey/{enc}", default=default_flag)
        A.add_log("INFO", f"Updated SSH key default={default_flag}: {key_name}", "account")
        return jsonify({"status": "success", "message": "SSH key updated"})
    except Exception as e:
        A.add_log("ERROR", f"Failed to update SSH key {key_name}: {str(e)}", "account")
        return jsonify({"status": "error", "message": str(e)}), 500


# ==================== 3. Services Expiration & Renewal Overview (/serviceInfos) ====================

def _compute_renew_mode(renew_obj):
    if not isinstance(renew_obj, dict):
        return "manual"
    if renew_obj.get("deleteAtExpiration"):
        return "deleteAtExpiration"
    if renew_obj.get("automatic"):
        return "auto"
    return "manual"


@cloud_fleet_bp.route("/api/ovh/account/services-overview", methods=["GET"])
def get_services_overview():
    A = _get_app_module()
    client = A.get_ovh_client()
    if not client:
        return A.ovh_credentials_missing_response()

    services = []
    try:
        try:
            dedicated_names = client.get("/dedicated/server") or []
        except Exception:
            dedicated_names = []

        for srv in dedicated_names:
            try:
                enc = quote(str(srv), safe="")
                info = client.get(f"/dedicated/server/{enc}/serviceInfos") or {}
                exp = info.get("expiration")
                renew_obj = info.get("renew") or {}
                services.append({
                    "serviceName": srv,
                    "displayName": srv,
                    "type": "dedicated",
                    "serviceType": "dedicated",
                    "status": info.get("status", "ok"),
                    "creation": info.get("creation"),
                    "expiration": exp,
                    "daysRemaining": _days_until(exp),
                    "renew": renew_obj,
                    "renewMode": _compute_renew_mode(renew_obj),
                    "renewPeriod": renew_obj.get("period"),
                    "engagedUpTo": info.get("engagedUpTo"),
                    "contactAdmin": info.get("contactAdmin"),
                    "contactBilling": info.get("contactBilling"),
                })
            except Exception:
                continue

        try:
            vps_names = client.get("/vps") or []
        except Exception:
            vps_names = []

        for vps in vps_names:
            try:
                enc = quote(str(vps), safe="")
                info = client.get(f"/vps/{enc}/serviceInfos") or {}
                exp = info.get("expiration")
                renew_obj = info.get("renew") or {}
                services.append({
                    "serviceName": vps,
                    "displayName": vps,
                    "type": "vps",
                    "serviceType": "vps",
                    "status": info.get("status", "ok"),
                    "creation": info.get("creation"),
                    "expiration": exp,
                    "daysRemaining": _days_until(exp),
                    "renew": renew_obj,
                    "renewMode": _compute_renew_mode(renew_obj),
                    "renewPeriod": renew_obj.get("period"),
                    "engagedUpTo": info.get("engagedUpTo"),
                    "contactAdmin": info.get("contactAdmin"),
                    "contactBilling": info.get("contactBilling"),
                })
            except Exception:
                continue

        services.sort(key=lambda s: (s["daysRemaining"] is None, s["daysRemaining"] if s["daysRemaining"] is not None else 999999))
        return jsonify({"status": "success", "data": services})
    except Exception as e:
        A.add_log("ERROR", f"Failed to fetch services overview: {str(e)}", "account")
        return jsonify({"status": "error", "message": str(e)}), 500


# ==================== 4. IP Edge Firewall Rules & Anti-DDoS Mitigation ====================

def _resolve_server_ipv4(client, service_name, requested_ip=None):
    enc_srv = quote(str(service_name), safe="")
    ipv4_blocks = []
    try:
        ips = client.get(f"/dedicated/server/{enc_srv}/ips") or []
        ipv4_blocks = [str(b) for b in ips if ":" not in str(b)]
    except Exception:
        pass
    if not ipv4_blocks:
        try:
            srv_info = client.get(f"/dedicated/server/{enc_srv}") or {}
            primary_ip = srv_info.get("ip")
            if primary_ip:
                ipv4_blocks = [str(primary_ip)]
        except Exception:
            pass
    if requested_ip:
        req_clean = str(requested_ip).split("/")[0].strip()
        for blk in ipv4_blocks:
            if blk.split("/")[0] == req_clean:
                return blk, req_clean
        return req_clean, req_clean
    if not ipv4_blocks:
        return None, None
    first_block = ipv4_blocks[0]
    return first_block, first_block.split("/")[0]


@cloud_fleet_bp.route("/api/server-control/<path:service_name>/ip-security", methods=["GET"])
def get_ip_security(service_name):
    A = _get_app_module()
    client = A.get_ovh_client()
    if not client:
        return A.ovh_credentials_missing_response()

    try:
        req_ip = request.args.get("ip")
        ip_block, ip_addr = _resolve_server_ipv4(client, service_name, req_ip)
        if not ip_block or not ip_addr:
            return jsonify({"success": False, "error": "No IPv4 address found for server"}), 404

        enc_block = quote(ip_block, safe="")
        enc_ip = quote(ip_addr, safe="")

        try:
            fw = client.get(f"/ip/{enc_block}/firewall/{enc_ip}") or {}
        except Exception:
            fw = {"ipOnFirewall": ip_addr, "enabled": False, "state": "not_configured"}

        rules = []
        try:
            seqs = client.get(f"/ip/{enc_block}/firewall/{enc_ip}/rule") or []
            for seq in sorted(seqs):
                try:
                    r = client.get(f"/ip/{enc_block}/firewall/{enc_ip}/rule/{seq}") or {}
                    rules.append(r)
                except Exception:
                    continue
        except Exception:
            rules = []

        try:
            mitigation = client.get(f"/ip/{enc_block}/mitigation/{enc_ip}") or {}
        except Exception:
            mitigation = {"ipOnMitigation": ip_addr, "permanent": False, "auto": False, "state": "auto"}

        payload = {
            "ipBlock": ip_block,
            "ip": ip_addr,
            "firewall": fw,
            "rules": rules,
            "mitigation": mitigation,
        }
        return jsonify({
            "status": "success",
            "success": True,
            "data": payload,
            **payload,
        })
    except Exception as e:
        A.add_log("ERROR", f"Failed to get IP security for {service_name}: {str(e)}", "server_control")
        return jsonify({"success": False, "error": str(e)}), 500


@cloud_fleet_bp.route("/api/server-control/<path:service_name>/ip-security/rules", methods=["POST"])
def create_ip_firewall_rule(service_name):
    A = _get_app_module()
    client = A.get_ovh_client()
    if not client:
        return A.ovh_credentials_missing_response()

    body = request.get_json(silent=True) or {}
    try:
        sequence = int(body.get("sequence", 0))
        if sequence < 0 or sequence > 19:
            return jsonify({"success": False, "error": "sequence must be between 0 and 19"}), 400
    except Exception:
        return jsonify({"success": False, "error": "Invalid sequence number"}), 400

    action = str(body.get("action") or "permit").strip()
    protocol = str(body.get("protocol") or "tcp").strip()
    if action not in ("permit", "deny"):
        return jsonify({"success": False, "error": "action must be permit or deny"}), 400

    try:
        ip_block, ip_addr = _resolve_server_ipv4(client, service_name, body.get("ip"))
        if not ip_block or not ip_addr:
            return jsonify({"success": False, "error": "No IPv4 address found for server"}), 404

        enc_block = quote(ip_block, safe="")
        enc_ip = quote(ip_addr, safe="")

        # Ensure IP is registered on the edge firewall
        try:
            client.get(f"/ip/{enc_block}/firewall/{enc_ip}")
        except Exception:
            try:
                client.post(f"/ip/{enc_block}/firewall", ipOnFirewall=ip_addr)
            except Exception:
                pass

        rule_payload = {
            "sequence": sequence,
            "action": action,
            "protocol": protocol,
        }
        source = str(body.get("source") or "").strip()
        if source and source.lower() != "any":
            rule_payload["source"] = source
        dest_port = body.get("destinationPort")
        if dest_port is not None and str(dest_port).strip() != "" and protocol in ("tcp", "udp"):
            rule_payload["destinationPort"] = int(str(dest_port).strip())
        if body.get("tcpOption") and protocol == "tcp":
            rule_payload["tcpOption"] = body["tcpOption"]

        client.post(f"/ip/{enc_block}/firewall/{enc_ip}/rule", **rule_payload)
        A.add_log("INFO", f"Added IP firewall rule #{sequence} ({action} {protocol}) on {ip_addr}", "server_control")
        return jsonify({"status": "success", "success": True, "message": "Firewall rule created"})
    except Exception as e:
        A.add_log("ERROR", f"Failed to create IP firewall rule for {service_name}: {str(e)}", "server_control")
        return jsonify({"success": False, "error": str(e)}), 500


@cloud_fleet_bp.route("/api/server-control/<path:service_name>/ip-security/rules/<int:sequence>", methods=["DELETE"])
def delete_ip_firewall_rule(service_name, sequence):
    A = _get_app_module()
    client = A.get_ovh_client()
    if not client:
        return A.ovh_credentials_missing_response()

    try:
        ip_block, ip_addr = _resolve_server_ipv4(client, service_name, request.args.get("ip"))
        if not ip_block or not ip_addr:
            return jsonify({"success": False, "error": "No IPv4 address found for server"}), 404

        enc_block = quote(ip_block, safe="")
        enc_ip = quote(ip_addr, safe="")
        client.delete(f"/ip/{enc_block}/firewall/{enc_ip}/rule/{sequence}")
        A.add_log("INFO", f"Deleted IP firewall rule #{sequence} on {ip_addr}", "server_control")
        return jsonify({"status": "success", "success": True, "message": "Firewall rule deleted"})
    except Exception as e:
        A.add_log("ERROR", f"Failed to delete IP firewall rule #{sequence} for {service_name}: {str(e)}", "server_control")
        return jsonify({"success": False, "error": str(e)}), 500


@cloud_fleet_bp.route("/api/server-control/<path:service_name>/ip-security/mitigation", methods=["POST"])
def update_ip_mitigation(service_name):
    A = _get_app_module()
    client = A.get_ovh_client()
    if not client:
        return A.ovh_credentials_missing_response()

    body = request.get_json(silent=True) or {}
    permanent = bool(body.get("permanent", True))

    try:
        ip_block, ip_addr = _resolve_server_ipv4(client, service_name, body.get("ip"))
        if not ip_block or not ip_addr:
            return jsonify({"success": False, "error": "No IPv4 address found for server"}), 404

        enc_block = quote(ip_block, safe="")
        enc_ip = quote(ip_addr, safe="")

        try:
            client.get(f"/ip/{enc_block}/mitigation/{enc_ip}")
            client.put(f"/ip/{enc_block}/mitigation/{enc_ip}", permanent=permanent)
        except Exception:
            if permanent:
                client.post(f"/ip/{enc_block}/mitigation", ipOnMitigation=ip_addr)
                try:
                    client.put(f"/ip/{enc_block}/mitigation/{enc_ip}", permanent=True)
                except Exception:
                    pass

        A.add_log("INFO", f"Updated Anti-DDoS permanent mitigation={permanent} on {ip_addr}", "server_control")
        return jsonify({"status": "success", "success": True, "permanent": permanent})
    except Exception as e:
        A.add_log("ERROR", f"Failed to update mitigation for {service_name}: {str(e)}", "server_control")
        return jsonify({"success": False, "error": str(e)}), 500


# ==================== 5. Purchased VPS Fleet Control (/vps) ====================

@cloud_fleet_bp.route("/api/ovh/vps/list", methods=["GET"])
def list_purchased_vps():
    A = _get_app_module()
    client = A.get_ovh_client()
    if not client:
        return A.ovh_credentials_missing_response()

    try:
        vps_names = client.get("/vps") or []
        vps_list = []
        for name in vps_names:
            try:
                enc = quote(str(name), safe="")
                detail = client.get(f"/vps/{enc}") or {}
                try:
                    ips = client.get(f"/vps/{enc}/ips") or []
                except Exception:
                    ips = []
                try:
                    svc = client.get(f"/vps/{enc}/serviceInfos") or {}
                except Exception:
                    svc = {}
                model = detail.get("model") or {}
                exp = svc.get("expiration")
                renew_obj = svc.get("renew") or {}
                model_str = model.get("offer") or model.get("name") or ""
                mem_mb = detail.get("memoryLimit") or model.get("memory")
                disk_gb = model.get("disk")
                vps_list.append({
                    "serviceName": name,
                    "displayName": detail.get("displayName") or name,
                    "state": detail.get("state", "unknown"),
                    "zone": detail.get("zone") or "",
                    "vcore": detail.get("vcore") or model.get("vcore"),
                    "memoryLimit": mem_mb,
                    "memoryLimitMb": mem_mb,
                    "disk": disk_gb,
                    "diskGb": disk_gb,
                    "model": model_str,
                    "modelName": model_str,
                    "ips": ips,
                    "expiration": exp,
                    "daysRemaining": _days_until(exp),
                    "renew": renew_obj,
                    "renewMode": _compute_renew_mode(renew_obj),
                })
            except Exception:
                continue
        return jsonify({"status": "success", "data": vps_list})
    except Exception as e:
        A.add_log("ERROR", f"Failed to list VPS instances: {str(e)}", "account")
        return jsonify({"status": "error", "message": str(e)}), 500


@cloud_fleet_bp.route("/api/ovh/vps/<path:service_name>/reboot", methods=["POST"])
def reboot_vps_instance(service_name):
    A = _get_app_module()
    client = A.get_ovh_client()
    if not client:
        return A.ovh_credentials_missing_response()

    try:
        enc = quote(str(service_name), safe="")
        client.post(f"/vps/{enc}/reboot")
        A.add_log("INFO", f"Triggered reboot for VPS: {service_name}", "server_control")
        return jsonify({"status": "success", "message": f"VPS {service_name} reboot initiated"})
    except Exception as e:
        A.add_log("ERROR", f"Failed to reboot VPS {service_name}: {str(e)}", "server_control")
        return jsonify({"status": "error", "message": str(e)}), 500
