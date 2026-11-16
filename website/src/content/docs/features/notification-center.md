---
title: Multi-Channel Notification Center
description: Configure outbound Telegram Bot and SMTP Email notification channels with tri-lingual templates and 8 event subscriptions.
---

The **Notification Center** (`/notifications`, powered by `internal/notify`) provides a purely **outbound** multi-channel dispatcher supporting multiple simultaneous **Telegram Bots** and **SMTP Email** accounts.

## Tri-Lingual Templates & Timezone Formatting

- **Notification Language (`locale`)**: Choose **`en`** (English, default), **`fr`** (Français), or **`zh-CN`** (简体中文) directly in **Notification Center** so outbound Telegram messages and HTML/text emails render in your preferred language independently of browser UI sessions.
- **Timezone (`TZ`)**: Notification timestamps automatically use your container's `TZ` environment variable (default `UTC`, e.g., `Europe/Paris` or `Asia/Shanghai`).

## Supported Event Types (8 Events)

Each notification channel can subscribe independently to any combination of the 8 system events:

| Event Key | Default On | Requires OVH API Key | Trigger Condition |
| :--- | :---: | :---: | :--- |
| `stock.available` | Yes | No | A monitored server/VPS configuration transitions from out-of-stock to available. |
| `stock.unavailable` | No | No | A previously available monitored configuration goes out of stock. |
| `sku.changed` | Yes | No | The Public SKU Watcher detects a newly racked SKU, confirmed SKU removal, or sub-configuration change. |
| `order.success` | Yes | Yes | Automated purchase queue or quick-order generates an OVHcloud order (includes payment link). |
| `order.failed` | Yes | Yes | An automated order attempt fails during cart creation, configuration, or checkout. |
| `order.delivered` | Yes | Yes | A tracked order in `/me/order` completes physical delivery (`INSTALLATION` done). |
| `service.expiring` | No | Yes | A Dedicated Server or VPS is within `expiryDays` (default `7` days) of expiration. |
| `mcp.action` | Yes | No | An AI client executes an L2 procurement/queue mutation or L3 operation via `/api/mcp`. |

## Channel Type 1: Outbound Telegram Bot

1. Message [@BotFather](https://t.me/BotFather) on Telegram and run `/newbot` to obtain a **Bot Token** (`123456789:ABCdef...`).
2. Obtain your **Chat ID** (personal user ID, group ID `-100...`, or channel ID).
3. In **Notification Center → Add Channel**, select **Telegram Bot**, enter a channel name, paste your **Bot Token** and **Chat ID**, choose your subscribed events, and click **Test** then **Save**.

:::note[Outbound-Only Push & Panel URL]
Telegram integration in OVH Nexus is strictly **outbound-only** (`https://api.telegram.org`), so no inbound public callback endpoint is required. Optionally set **Panel URL** in Notification Center so alert messages include a direct link back to your console.
:::

## Channel Type 2: SMTP Email

OVH Nexus includes a built-in SMTP dispatcher (`internal/notify`) that sends multipart emails containing both a **plain-text fallback** and a **responsive OVHcloud Blue HTML template** (`UTF-8`).

| Field | Example | Description |
| :--- | :--- | :--- |
| **SMTP Host** | `smtp.gmail.com` / `smtp.qq.com` / `mail.example.com` | Outbound SMTP hostname. |
| **SMTP Port** | `465` (SSL/TLS) or `587` (STARTTLS) | Server port. |
| **Encryption Mode** | `ssl` (`465`), `starttls` (`587`), or `none` (`25`) | Transport security mode verified against system CA certificates (`ca-certificates`). |
| **Username** | `alerts@example.com` | SMTP login username (optional for IP-whitelisted local relays). |
| **Password** | `app-specific-password` | SMTP password or app password (encrypted at rest via `AES-256-GCM` and masked as `****` in API responses). |
| **From Address** | `OVH Nexus <alerts@example.com>` | Sender address (`From` header). |
| **To Addresses** | `ops@example.com, admin@example.com` | Comma-separated recipient email addresses. |

## Secret Masking (`****`)

Whenever the WebUI fetches notification settings (`GET /api/notifications`), OVH Nexus masks sensitive credentials (`token` for Telegram, `password` for SMTP) as `abcd****wxyz`. When you edit channel names, events, or rules and click **Save** (or **Test**), the backend automatically preserves the encrypted secret stored in `/data/nexus.db` so you never have to re-type your password.
