---
title: Multi-Channel Notification Center
description: Configure Telegram Bot and SMTP Email notification channels with fine-grained event subscriptions.
---

The **Notification Center** (`/notifications`, powered by `backend/notifications/`) provides a pluggable multi-channel dispatcher supporting multiple simultaneous **Telegram Bots** and **SMTP Email** accounts.

## Supported Event Types (7 Events)

Each notification channel can subscribe independently to any combination of the 7 system events:

| Event Key | Default On | Requires OVH API Key | Trigger Condition |
| :--- | :---: | :---: | :--- |
| `stock.available` | Yes | No | A monitored server/VPS configuration transitions from out-of-stock to available. |
| `stock.unavailable` | No | No | A previously available monitored configuration goes out of stock. |
| `sku.changed` | Yes | No | `SkuWatcher` detects a newly racked SKU, confirmed SKU removal, or sub-configuration change. |
| `order.success` | Yes | Yes | Automated queue/sniper successfully generates an OVHcloud order (includes payment link). |
| `order.failed` | Yes | Yes | An automated order attempt fails during cart creation, configuration, or checkout. |
| `order.delivered` | Yes | Yes | A tracked order in `/me/order` completes physical delivery (`INSTALLATION` done). |
| `service.expiring` | No | Yes | A Dedicated Server or VPS is within `expiryDays` (default `7` days) of expiration. |

## Channel Type 1: Telegram Bot

1. Message [@BotFather](https://t.me/BotFather) on Telegram and run `/newbot` to obtain a **Bot Token** (`123456789:ABCdef...`).
2. Obtain your **Chat ID** (personal user ID, group ID `-100...`, or channel ID).
3. In **Notification Center → Add Channel**, select **Telegram Bot**, enter a channel name, paste your **Bot Token** and **Chat ID**, choose your subscribed events, and click **Test** then **Save**.

:::note[Interactive Telegram Webhook]
If you configure a public URL and Telegram webhook (`/api/telegram/webhook`), OVH Nexus verifies every incoming update using a constant-time check against the persisted `X-Telegram-Bot-Api-Secret-Token` (`data/webhook_secret`) and restricts command execution strictly to the configured `chatId`.
:::

## Channel Type 2: SMTP Email

OVH Nexus includes a built-in `SmtpNotifier` (`backend/notifications/smtp.py`) that sends multipart emails containing both a **plain-text fallback** and a **responsive OVHcloud Blue HTML template** (`MIME Base64/UTF-8`).

| Field | Example | Description |
| :--- | :--- | :--- |
| **SMTP Host** | `smtp.gmail.com` / `smtp.qq.com` / `mail.PrivateServer.com` | Outbound SMTP hostname. |
| **SMTP Port** | `465` (SSL/TLS) or `587` (STARTTLS) | Server port. |
| **Encryption Mode** | `ssl` (`465`), `starttls` (`587`), or `none` (`25`) | Transport security mode (`ssl` uses `smtplib.SMTP_SSL` with system CA bundle). |
| **Username** | `alerts@example.com` | SMTP login username (optional for IP-whitelisted local relays). |
| **Password** | `app-specific-password` | SMTP password or app password (masked as `****` once saved). |
| **From Address** | `OVH Nexus <alerts@example.com>` | Sender address (`From` header). |
| **To Addresses** | `ops@example.com, admin@example.com` | Comma-separated recipient email addresses. |

## Secret Masking (`****`)

Whenever the WebUI fetches notification settings (`GET /api/notifications/config`), `NotificationHub` masks all sensitive fields (`token` for Telegram, `password` for SMTP) as `abcd****wxyz`. When you edit channel names, events, or rules and click **Save** (or **Test**), the backend automatically merges your changes with the stored plaintext secret on disk so you never have to re-type your password.
