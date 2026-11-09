---
title: 多通道可插拔通知中心
description: 配置多实例 Telegram 机器人与 SMTP 邮件推送通道，按需订阅 7 类细粒度事件。
---

**通知中心（Notification Center，`/notifications`）** 基于可插拔的 `NotificationHub` 架构设计，支持同时添加多个 **Telegram 机器人**与 **SMTP 邮件**推送通道，并为每个通道独立勾选需要订阅的事件类型。

## 支持的 7 类通知事件

每个通知通道均可独立订阅以下 7 类系统事件的任意组合：

| 事件标识 (Key) | 默认开启 | 是否需 OVH API Key | 触发时机 |
| :--- | :---: | :---: | :--- |
| `stock.available` | 是 | 否 | 监控中的独服或 VPS 配置从无货变为有货（补货）。 |
| `stock.unavailable` | 否 | 否 | 原本有货的监控配置变为售罄状态。 |
| `sku.changed` | 是 | 否 | `SkuWatcher` 检测到货架新上机架型号、确认下架或子配置增减。 |
| `order.success` | 是 | 是 | 自动抢购队列或配置狙击成功生成 OVHcloud 订单（含付款链接）。 |
| `order.failed` | 是 | 是 | 自动下单尝试在创建购物车、挂载配置或结算阶段失败。 |
| `order.delivered` | 是 | 是 | `/me/order` 中追踪的订单完成全部 4 步硬件交付（`INSTALLATION` 完成）。 |
| `service.expiring` | 否 | 是 | 账户名下的独服或 VPS 剩余有效期小于等于 `expiryDays`（默认 `7` 天）。 |

## 通道类型 1：Telegram 机器人

1. 在 Telegram 中联系 [@BotFather](https://t.me/BotFather) 发送 `/newbot` 创建机器人，获取 **Bot Token**（形如 `123456789:ABCdef...`）。
2. 获取接收消息的 **Chat ID**（个人账号 ID、群组 ID `-100...` 或频道 ID）。
3. 在 **通知中心 → 添加通知渠道** 中选择 **Telegram Bot**，填写渠道名称、Bot Token 与 Chat ID，勾选订阅事件后点击**测试发送**并**保存配置**。

:::note[Telegram Webhook 安全校验]
如果您配置了交互式 Telegram Webhook（`/api/telegram/webhook`），OVH Nexus 会使用恒定时间算法校验请求头中的 `X-Telegram-Bot-Api-Secret-Token`（保存在 `data/webhook_secret`），且仅响应白名单 `chatId` 发送的指令。
:::

## 通道类型 2：SMTP 邮件推送

OVH Nexus 内置了标准 `SmtpNotifier`（`backend/notifications/smtp.py`），自动发送包含**纯文本降级正文**与**OVHcloud 品牌蓝响应式 HTML 邮件卡片**的多部分邮件（`MIME Base64/UTF-8`，完美支持中文与特殊符号）。

| 配置项 | 示例值 | 说明 |
| :--- | :--- | :--- |
| **SMTP 服务器 (`host`)** | `smtp.qq.com` / `smtp.gmail.com` / `smtp.feishu.cn` | 发件服务器地址。 |
| **端口 (`port`)** | `465` (SSL/TLS) 或 `587` (STARTTLS) | SMTP 端口号。 |
| **加密方式 (`encryption`)** | `ssl`（465）、`starttls`（587）或 `none`（25） | 传输层加密模式（`ssl` 使用系统 CA 证书链建立 `SMTP_SSL` 连接）。 |
| **用户名 (`username`)** | `alerts@example.com` | SMTP 登录账号（若为内网免密中继可留空）。 |
| **密码 / 授权码 (`password`)** | `xxxx-xxxx-xxxx` | 邮箱密码或客户端专用授权码（保存后前端自动掩码为 `****`）。 |
| **发件人展示 (`fromEmail`)** | `OVH Nexus <alerts@example.com>` | 邮件 `From` 头展示地址。 |
| **收件人列表 (`toEmails`)** | `ops@example.com, me@example.com` | 收件人邮箱，支持多个地址以英文逗号分隔。 |

## 敏感凭据掩码保护 (`****`)

每当前端读取通知配置（`GET /api/notifications/config`）时，后端会自动将所有敏感密钥（Telegram 的 `token`、SMTP 的 `password`）脱敏为 `abcd****wxyz`。当您修改渠道名称或订阅事件并点击**保存**或**测试**时，若密码框保持掩码或留空，后端会自动沿用磁盘上已保存的真实密钥，无需重复输入。
