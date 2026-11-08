"""
SMTP Email Notification Channel
Supports SSL/TLS (465), STARTTLS (587), and plain SMTP (25/custom),
sending multipart/alternative (plain text + OVHcloud Blue HTML).
"""

from __future__ import annotations

from email.mime.multipart import MIMEMultipart
from email.mime.text import MIMEText
from email.utils import formataddr, formatdate, make_msgid
import smtplib
import ssl
from typing import List, Tuple

from .base import BaseNotifier
from .events import NotificationEvent


class SmtpNotifier(BaseNotifier):
    channel_type = "smtp"

    def _parse_recipients(self) -> List[str]:
        raw = self.channel_config.get("recipients") or []
        if isinstance(raw, str):
            items = [x.strip() for x in raw.replace(";", ",").split(",")]
        elif isinstance(raw, list):
            items = [str(x).strip() for x in raw]
        else:
            items = []
        return [x for x in items if x and "@" in x]

    def send(self, event: NotificationEvent) -> Tuple[bool, str]:
        host = (self.channel_config.get("host") or "").strip()
        port = int(self.channel_config.get("port") or 465)
        security = (self.channel_config.get("security") or "ssl").strip().lower()
        username = (self.channel_config.get("username") or "").strip()
        password = self.channel_config.get("password") or ""
        from_name = (self.channel_config.get("fromName") or "OVH Nexus").strip()
        from_address = (self.channel_config.get("fromAddress") or username).strip()
        recipients = self._parse_recipients()

        if not host:
            return False, "未配置 SMTP 服务器地址 (host)"
        if not from_address:
            return False, "未配置发件人邮箱地址 (fromAddress)"
        if not recipients:
            return False, "未配置有效的收件人邮箱 (recipients)"

        msg = MIMEMultipart("alternative")
        msg["Subject"] = f"[OVH Nexus] {event.title}"
        msg["From"] = formataddr((from_name, from_address))
        msg["To"] = ", ".join(recipients)
        msg["Date"] = formatdate(localtime=True)
        msg["Message-ID"] = make_msgid(domain=from_address.split("@")[-1] if "@" in from_address else "ovh-nexus.local")

        plain_body = event.plain_text
        if self.panel_url and self.panel_url.startswith(("http://", "https://")):
            plain_body = f"{plain_body}\n\n---\n打开 OVH Nexus: {self.panel_url}"

        html_body = event.render_html(panel_url=self.panel_url)

        msg.attach(MIMEText(plain_body, "plain", "utf-8"))
        msg.attach(MIMEText(html_body, "html", "utf-8"))

        try:
            if security == "ssl":
                ctx = ssl.create_default_context()
                with smtplib.SMTP_SSL(host, port, timeout=15, context=ctx) as server:
                    if username and password:
                        server.login(username, password)
                    server.sendmail(from_address, recipients, msg.as_string())
            elif security == "starttls":
                ctx = ssl.create_default_context()
                with smtplib.SMTP(host, port, timeout=15) as server:
                    server.ehlo()
                    server.starttls(context=ctx)
                    server.ehlo()
                    if username and password:
                        server.login(username, password)
                    server.sendmail(from_address, recipients, msg.as_string())
            else:
                with smtplib.SMTP(host, port, timeout=15) as server:
                    server.ehlo()
                    if username and password:
                        server.login(username, password)
                    server.sendmail(from_address, recipients, msg.as_string())

            self.add_log("INFO", f"SMTP 邮件推送成功 -> {', '.join(recipients)}", "notify")
            return True, "ok"
        except Exception as e:
            err_msg = str(e)
            self.add_log("ERROR", f"SMTP 邮件推送失败: {err_msg}", "notify")
            return False, err_msg
