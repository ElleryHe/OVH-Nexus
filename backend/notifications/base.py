"""
Abstract Base Class for Notification Channels
"""

from __future__ import annotations

from abc import ABC, abstractmethod
from typing import Any, Callable, Dict, Tuple

from .events import NotificationEvent


class BaseNotifier(ABC):
    """Pluggable notification channel interface."""

    channel_type: str = "base"

    def __init__(
        self,
        channel_config: Dict[str, Any],
        panel_url: str = "",
        add_log: Callable[[str, str, str], None] = lambda l, m, c="notify": None,
    ):
        self.channel_config = channel_config or {}
        self.panel_url = panel_url or ""
        self.add_log = add_log

    @abstractmethod
    def send(self, event: NotificationEvent) -> Tuple[bool, str]:
        """Deliver an event. Returns (success, error_or_status_message)."""
        raise NotImplementedError

    def test(self) -> Tuple[bool, str]:
        """Send a test notification to verify channel credentials."""
        test_event = NotificationEvent(
            event_type="system.test",
            title="OVH Nexus 通知渠道测试成功",
            plain_text=(
                "✅ OVH Nexus 通知渠道连接测试成功！\n"
                f"渠道类型: {self.channel_type.upper()}\n"
                "当您订阅的事件触发时，消息将通过此渠道推送。"
            ),
            fields=[
                {"label": "渠道类型", "value": self.channel_type.upper()},
                {"label": "状态", "value": "连接正常"},
            ],
        )
        return self.send(test_event)
