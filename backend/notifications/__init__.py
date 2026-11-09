"""
NotificationHub: Multi-channel Pluggable Notification Dispatcher
================================================================
- Manages notification channels (Telegram, SMTP, and future extensions)
- Automatically migrates legacy tgToken / tgChatId into a Telegram channel
- Masks secret fields (token, password) when returning configuration to the UI
- Dispatches events to all enabled channels subscribed to the event type
"""

from __future__ import annotations

from copy import deepcopy
from typing import Any, Callable, Dict, List, Optional, Tuple
import uuid

from .base import BaseNotifier
from .events import (
    ALL_EVENTS,
    DEFAULT_CHANNEL_EVENTS,
    EVENT_ORDER_DELIVERED,
    EVENT_ORDER_FAILED,
    EVENT_ORDER_SUCCESS,
    EVENT_SERVICE_EXPIRING,
    EVENT_SKU_CHANGED,
    EVENT_STOCK_AVAILABLE,
    EVENT_STOCK_UNAVAILABLE,
    NotificationEvent,
)
from .smtp import SmtpNotifier
from .telegram import TelegramNotifier

NOTIFIER_REGISTRY = {
    "telegram": TelegramNotifier,
    "smtp": SmtpNotifier,
}

SECRET_KEYS_BY_TYPE = {
    "telegram": ("token",),
    "smtp": ("password",),
}

DEFAULT_RULES = {
    "skuEnabled": True,
    "skuInterval": 600,
    "skuTrackConfigs": False,
    "skuRanges": ["KS", "RISE", "SYS", "ADV", "SCALE", "HGR"],
    "skuMergeRegional": True,
    "expiryDays": 7,
}


def mask_secret_value(val: str) -> str:
    if not val:
        return ""
    s = str(val)
    if len(s) <= 8:
        return "****"
    return f"{s[:4]}****{s[-4:]}"


def is_masked_secret(val: Any) -> bool:
    return isinstance(val, str) and "****" in val


class NotificationHub:
    """Central notification manager and dispatcher."""

    def __init__(
        self,
        get_config_func: Callable[[], Dict[str, Any]],
        save_config_func: Callable[[Dict[str, Any]], None],
        add_log_func: Callable[[str, str, str], None] = lambda l, m, c="notify": None,
    ):
        self._get_config = get_config_func
        self._save_config = save_config_func
        self._add_log = add_log_func

    def ensure_migrated(self, cfg: Optional[Dict[str, Any]] = None) -> Dict[str, Any]:
        """
        Ensure cfg['notifications'] exists with channels + rules, and migrate legacy
        tgToken / tgChatId into a Telegram channel if not yet migrated.
        """
        root = cfg if cfg is not None else self._get_config()
        notif = root.get("notifications")
        dirty = False

        if not isinstance(notif, dict):
            notif = {"channels": [], "panelUrl": "", "rules": deepcopy(DEFAULT_RULES)}
            root["notifications"] = notif
            dirty = True

        if "channels" not in notif or not isinstance(notif["channels"], list):
            notif["channels"] = []
            dirty = True

        if "panelUrl" not in notif:
            notif["panelUrl"] = ""
            dirty = True

        rules = notif.get("rules")
        if not isinstance(rules, dict):
            notif["rules"] = deepcopy(DEFAULT_RULES)
            dirty = True
        else:
            for k, v in DEFAULT_RULES.items():
                if k not in rules:
                    rules[k] = deepcopy(v)
                    dirty = True

        # Migrate legacy tgToken / tgChatId if present and no telegram channel exists yet
        legacy_token = (root.get("tgToken") or "").strip()
        legacy_chat = str(root.get("tgChatId") or "").strip()
        has_tg_channel = any(c.get("type") == "telegram" for c in notif["channels"])

        if (legacy_token or legacy_chat) and not has_tg_channel and not notif.get("migratedLegacyTg"):
            notif["channels"].append(
                {
                    "id": "tg-default",
                    "type": "telegram",
                    "name": "默认 Telegram 机器人",
                    "enabled": bool(legacy_token and legacy_chat),
                    "events": list(DEFAULT_CHANNEL_EVENTS),
                    "config": {
                        "token": legacy_token,
                        "chatId": legacy_chat,
                    },
                }
            )
            notif["migratedLegacyTg"] = True
            dirty = True

        if dirty and cfg is None:
            self._save_config(root)

        return notif

    def get_public_state(self) -> Dict[str, Any]:
        """Return notification state with secret fields masked for frontend display."""
        root = self._get_config()
        notif = deepcopy(self.ensure_migrated(root))

        for ch in notif.get("channels", []):
            ch_type = ch.get("type", "")
            secret_keys = SECRET_KEYS_BY_TYPE.get(ch_type, ())
            ch_cfg = ch.get("config") or {}
            has_secrets = {}
            for sk in secret_keys:
                raw_val = ch_cfg.get(sk) or ""
                has_secrets[sk] = bool(raw_val)
                ch_cfg[sk] = mask_secret_value(raw_val) if raw_val else ""
            ch["hasSecrets"] = has_secrets

        notif["supportedEvents"] = ALL_EVENTS
        return notif

    def update_state(self, payload: Dict[str, Any]) -> Dict[str, Any]:
        """
        Update channels, panelUrl, and rules while preserving existing secret values
        whenever the incoming value is empty or masked ('****').
        Also syncs the primary Telegram channel back to legacy tgToken/tgChatId for
        1-version backward compatibility.
        """
        root = self._get_config()
        current = self.ensure_migrated(root)
        existing_by_id = {c.get("id"): c for c in current.get("channels", []) if c.get("id")}

        if "panelUrl" in payload:
            current["panelUrl"] = str(payload.get("panelUrl") or "").strip()

        if "rules" in payload and isinstance(payload["rules"], dict):
            r_in = payload["rules"]
            rules = current["rules"]
            if "skuEnabled" in r_in:
                rules["skuEnabled"] = bool(r_in["skuEnabled"])
            if "skuInterval" in r_in:
                rules["skuInterval"] = max(60, int(r_in["skuInterval"] or 600))
            if "skuTrackConfigs" in r_in:
                rules["skuTrackConfigs"] = bool(r_in["skuTrackConfigs"])
            if "skuRanges" in r_in and isinstance(r_in["skuRanges"], list):
                rules["skuRanges"] = [str(x).upper() for x in r_in["skuRanges"]]
            if "skuMergeRegional" in r_in:
                rules["skuMergeRegional"] = bool(r_in["skuMergeRegional"])
            if "expiryDays" in r_in:
                rules["expiryDays"] = max(1, min(90, int(r_in["expiryDays"] or 7)))

        if "channels" in payload and isinstance(payload["channels"], list):
            new_channels = []
            for item in payload["channels"]:
                if not isinstance(item, dict):
                    continue
                ch_type = (item.get("type") or "").strip().lower()
                if ch_type not in NOTIFIER_REGISTRY:
                    continue
                ch_id = (item.get("id") or "").strip() or f"{ch_type}-{uuid.uuid4().hex[:8]}"
                old_ch = existing_by_id.get(ch_id) or {}
                old_cfg = (old_ch.get("config") or {}) if isinstance(old_ch, dict) else {}
                in_cfg = item.get("config") if isinstance(item.get("config"), dict) else {}

                merged_cfg = deepcopy(old_cfg)
                secret_keys = SECRET_KEYS_BY_TYPE.get(ch_type, ())
                for k, v in in_cfg.items():
                    if k in secret_keys:
                        if v and not is_masked_secret(v):
                            merged_cfg[k] = str(v).strip()
                    else:
                        merged_cfg[k] = v

                valid_event_keys = {e["key"] for e in ALL_EVENTS}
                events = [
                    e for e in (item.get("events") or []) if isinstance(e, str) and e in valid_event_keys
                ]

                new_channels.append(
                    {
                        "id": ch_id,
                        "type": ch_type,
                        "name": (item.get("name") or ch_type.upper()).strip(),
                        "enabled": bool(item.get("enabled", True)),
                        "events": events,
                        "config": merged_cfg,
                    }
                )
            current["channels"] = new_channels
            current["migratedLegacyTg"] = True

            # Keep legacy tgToken / tgChatId synced with the first Telegram channel for 1-version compatibility
            first_tg = next((c for c in new_channels if c["type"] == "telegram"), None)
            if first_tg:
                root["tgToken"] = (first_tg["config"].get("token") or "").strip()
                root["tgChatId"] = str(first_tg["config"].get("chatId") or "").strip()
            else:
                root["tgToken"] = ""
                root["tgChatId"] = ""

        root["notifications"] = current
        self._save_config(root)
        return self.get_public_state()

    def build_notifier(self, channel: Dict[str, Any], panel_url: str = "") -> Optional[BaseNotifier]:
        ch_type = (channel.get("type") or "").lower()
        cls = NOTIFIER_REGISTRY.get(ch_type)
        if not cls:
            return None
        return cls(channel.get("config") or {}, panel_url=panel_url, add_log=self._add_log)

    def test_channel(self, channel_id: str, override_channel: Optional[Dict[str, Any]] = None) -> Tuple[bool, str]:
        """Test a specific channel by ID (or with unsaved form values merged with stored secrets)."""
        root = self._get_config()
        notif = self.ensure_migrated(root)
        panel_url = notif.get("panelUrl") or ""
        stored = next((c for c in notif.get("channels", []) if c.get("id") == channel_id), None)

        if override_channel and isinstance(override_channel, dict):
            ch_type = (override_channel.get("type") or (stored or {}).get("type") or "").lower()
            merged_cfg = deepcopy((stored or {}).get("config") or {})
            in_cfg = override_channel.get("config") or {}
            secret_keys = SECRET_KEYS_BY_TYPE.get(ch_type, ())
            for k, v in in_cfg.items():
                if k in secret_keys:
                    if v and not is_masked_secret(v):
                        merged_cfg[k] = str(v).strip()
                else:
                    merged_cfg[k] = v
            target = {"id": channel_id, "type": ch_type, "config": merged_cfg}
        else:
            target = stored

        if not target:
            return False, "未找到对应的通知渠道"

        notifier = self.build_notifier(target, panel_url=panel_url)
        if not notifier:
            return False, f"不支持的渠道类型: {target.get('type')}"
        return notifier.test()

    def dispatch(self, event: NotificationEvent) -> bool:
        """
        Dispatch an event to all enabled channels subscribed to event.event_type.
        Returns True if at least one channel succeeded.
        """
        root = self._get_config()
        notif = self.ensure_migrated(root)
        panel_url = notif.get("panelUrl") or ""
        channels = notif.get("channels") or []

        any_success = False
        matched = 0
        for ch in channels:
            if not ch.get("enabled", True):
                continue
            subscribed = ch.get("events") or []
            if event.event_type not in subscribed:
                continue
            notifier = self.build_notifier(ch, panel_url=panel_url)
            if not notifier:
                continue
            matched += 1
            ok, _ = notifier.send(event)
            if ok:
                any_success = True

        if matched == 0:
            self._add_log("INFO", f"事件 {event.event_type} 无启用中的订阅渠道，已跳过推送", "notify")
        return any_success


__all__ = [
    "ALL_EVENTS",
    "DEFAULT_CHANNEL_EVENTS",
    "DEFAULT_RULES",
    "EVENT_ORDER_DELIVERED",
    "EVENT_ORDER_FAILED",
    "EVENT_ORDER_SUCCESS",
    "EVENT_SERVICE_EXPIRING",
    "EVENT_SKU_CHANGED",
    "EVENT_STOCK_AVAILABLE",
    "EVENT_STOCK_UNAVAILABLE",
    "NotificationEvent",
    "NotificationHub",
    "SmtpNotifier",
    "TelegramNotifier",
]
