"""
Telegram Bot Notification Channel
"""

from __future__ import annotations

import json
from typing import Any, Dict, Tuple
import urllib.request
import urllib.error

try:
    import requests as _requests
except ImportError:
    _requests = None

from .base import BaseNotifier
from .events import NotificationEvent


class TelegramNotifier(BaseNotifier):
    channel_type = "telegram"

    def send(self, event: NotificationEvent) -> Tuple[bool, str]:
        token = (self.channel_config.get("token") or "").strip()
        chat_id = str(self.channel_config.get("chatId") or "").strip()

        if not token:
            return False, "未配置 Telegram Bot Token"
        if not chat_id:
            return False, "未配置 Telegram Chat ID"

        url = f"https://api.telegram.org/bot{token}/sendMessage"
        payload: Dict[str, Any] = {
            "chat_id": chat_id,
            "text": event.plain_text,
        }
        if event.reply_markup:
            payload["reply_markup"] = event.reply_markup

        try:
            if _requests is not None:
                resp = _requests.post(
                    url,
                    json=payload,
                    headers={"Content-Type": "application/json"},
                    timeout=12,
                )
                status_code = resp.status_code
                body_text = resp.text
            else:
                req = urllib.request.Request(
                    url,
                    data=json.dumps(payload).encode("utf-8"),
                    headers={"Content-Type": "application/json"},
                    method="POST",
                )
                with urllib.request.urlopen(req, timeout=12) as r:
                    status_code = r.status
                    body_text = r.read().decode("utf-8")

            if status_code == 200:
                self.add_log("INFO", f"Telegram 推送成功 (chatId={chat_id})", "notify")
                return True, "ok"
            err_msg = f"HTTP {status_code}: {body_text[:200]}"
            self.add_log("ERROR", f"Telegram 推送失败: {err_msg}", "notify")
            return False, err_msg
        except Exception as e:
            err_msg = str(e)
            self.add_log("ERROR", f"Telegram 推送异常: {err_msg}", "notify")
            return False, err_msg
