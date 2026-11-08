"""
Notification Event Definitions & HTML/Plain Templates
=====================================================
Supports all 7 event types:
  - stock.available   (库存补货)
  - stock.unavailable (库存售罄)
  - order.success     (抢购成功)
  - order.failed      (抢购失败)
  - sku.changed       (SKU 变动：上新 / 下架)
  - service.expiring  (服务即将到期)
  - order.delivered   (订单交付完成)
"""

from __future__ import annotations

from dataclasses import dataclass, field
from datetime import datetime, timezone, timedelta
import html
from typing import Any, Dict, List, Optional

EVENT_STOCK_AVAILABLE = "stock.available"
EVENT_STOCK_UNAVAILABLE = "stock.unavailable"
EVENT_ORDER_SUCCESS = "order.success"
EVENT_ORDER_FAILED = "order.failed"
EVENT_SKU_CHANGED = "sku.changed"
EVENT_SERVICE_EXPIRING = "service.expiring"
EVENT_ORDER_DELIVERED = "order.delivered"

ALL_EVENTS: List[Dict[str, Any]] = [
    {"key": EVENT_STOCK_AVAILABLE, "requiresApiKey": False, "defaultEnabled": True},
    {"key": EVENT_STOCK_UNAVAILABLE, "requiresApiKey": False, "defaultEnabled": False},
    {"key": EVENT_ORDER_SUCCESS, "requiresApiKey": True, "defaultEnabled": True},
    {"key": EVENT_ORDER_FAILED, "requiresApiKey": True, "defaultEnabled": True},
    {"key": EVENT_SKU_CHANGED, "requiresApiKey": False, "defaultEnabled": True},
    {"key": EVENT_SERVICE_EXPIRING, "requiresApiKey": True, "defaultEnabled": False},
    {"key": EVENT_ORDER_DELIVERED, "requiresApiKey": True, "defaultEnabled": True},
]

DEFAULT_CHANNEL_EVENTS = [
    EVENT_STOCK_AVAILABLE,
    EVENT_ORDER_SUCCESS,
    EVENT_ORDER_FAILED,
    EVENT_SKU_CHANGED,
    EVENT_ORDER_DELIVERED,
]


def now_beijing_str() -> str:
    tz = timezone(timedelta(hours=8))
    return datetime.now(tz).strftime("%Y-%m-%d %H:%M:%S")


@dataclass
class NotificationEvent:
    event_type: str
    title: str
    plain_text: str
    fields: List[Dict[str, str]] = field(default_factory=list)
    reply_markup: Optional[Dict[str, Any]] = None
    badge_color: str = "#0050D7"
    timestamp: str = field(default_factory=now_beijing_str)

    def render_html(self, panel_url: str = "") -> str:
        """Render clean OVHcloud Blue HTML email body."""
        safe_title = html.escape(self.title)
        safe_body = html.escape(self.plain_text).replace("\n", "<br/>")
        safe_time = html.escape(self.timestamp)

        rows_html = ""
        if self.fields:
            row_items = []
            for item in self.fields:
                k = html.escape(str(item.get("label", "")))
                v = html.escape(str(item.get("value", "")))
                row_items.append(
                    f'<tr>'
                    f'<td style="padding:8px 12px;border-bottom:1px solid #E2E8F0;color:#64748B;font-size:13px;width:32%;font-weight:600;">{k}</td>'
                    f'<td style="padding:8px 12px;border-bottom:1px solid #E2E8F0;color:#0F172A;font-size:13px;font-family:ui-monospace,SFMono-Regular,Menlo,monospace;">{v}</td>'
                    f'</tr>'
                )
            rows_html = (
                '<table width="100%" cellpadding="0" cellspacing="0" '
                'style="border:1px solid #E2E8F0;border-radius:8px;border-collapse:collapse;margin:16px 0;background:#F8FAFC;">'
                + "".join(row_items)
                + "</table>"
            )

        cta_html = ""
        clean_url = (panel_url or "").strip()
        if clean_url and clean_url.startswith(("http://", "https://")):
            escaped_url = html.escape(clean_url, quote=True)
            cta_html = (
                f'<div style="margin-top:22px;text-align:left;">'
                f'<a href="{escaped_url}" target="_blank" rel="noopener noreferrer" '
                f'style="display:inline-block;padding:10px 20px;background:#0050D7;color:#FFFFFF;'
                f'text-decoration:none;border-radius:6px;font-size:13px;font-weight:600;">'
                f'打开 OVH Nexus 控制台 →</a>'
                f'</div>'
            )

        return f"""<!DOCTYPE html>
<html lang="zh-CN">
<head><meta charset="utf-8"/></head>
<body style="margin:0;padding:24px;background:#F1F5F9;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,sans-serif;color:#0F172A;">
  <div style="max-width:600px;margin:0 auto;background:#FFFFFF;border-radius:12px;border:1px solid #E2E8F0;overflow:hidden;box-shadow:0 4px 12px rgba(15,23,42,0.06);">
    <div style="background:#0050D7;padding:16px 24px;color:#FFFFFF;display:flex;align-items:center;justify-content:space-between;">
      <div style="font-size:16px;font-weight:700;letter-spacing:0.3px;">OVH Nexus · Cloud Fleet Notification</div>
      <div style="font-size:12px;opacity:0.85;">{html.escape(self.event_type)}</div>
    </div>
    <div style="padding:24px;">
      <h2 style="margin:0 0 14px 0;font-size:18px;color:#0F172A;">{safe_title}</h2>
      {rows_html}
      <div style="font-size:14px;line-height:1.65;color:#1E293B;background:#F8FAFC;padding:14px 16px;border-radius:8px;border:1px solid #E2E8F0;">
        {safe_body}
      </div>
      {cta_html}
    </div>
    <div style="padding:12px 24px;background:#F8FAFC;border-top:1px solid #E2E8F0;font-size:12px;color:#64748B;">
      发送时间：{safe_time} · 由 OVH Nexus 自动推送
    </div>
  </div>
</body>
</html>"""
