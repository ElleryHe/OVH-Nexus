---
title: 可插拔纯出站通知中心
description: 配置纯出站 Telegram 机器人与 SMTP 邮件推送通道，支持三语模板与 8 类细粒度事件订阅。
---

**通知中心（Notification Center，`/notifications`）** 基于 `internal/notify` 构建，采用**纯出站推送**架构，支持同时配置多个 **Telegram 机器人**与 **SMTP 邮件**通道，并为每个通道独立勾选需要订阅的事件类型。

## 三语通知模板与容器时区

- **通知消息语言 (`locale`)**：可在**通知中心**独立选择 **`en`**（English，默认）、**`fr`**（Français）或 **`zh-CN`**（简体中文），使推送的 Telegram 消息与 HTML/纯文本邮件按您习惯的语言渲染，不受 WebUI 当前界面语言限制。
- **容器时区 (`TZ`)**：通知消息中的时间戳自动遵循容器环境变量 `TZ`（默认 `UTC`，可设为 `Asia/Shanghai`、`Europe/Paris` 等）。

## 支持的 8 类通知事件

每个通知通道均可独立订阅以下 8 类系统事件的任意组合：

| 事件标识 (Key) | 默认开启 | 是否需 OVH API Key | 触发时机 |
| :--- | :---: | :---: | :--- |
| `stock.available` | 是 | 否 | 监控中的独服或 VPS 配置从无货变为有货（补货）。 |
| `stock.unavailable` | 否 | 否 | 原本有货的监控配置变为售罄状态。 |
| `sku.changed` | 是 | 否 | 公开货架 SKU 监控检测到新上机架、已公布机型正式开售（`saleStarted`）、同产品线换代（`replaced`）或确认下架（按产品线分组聚合）。 |
| `order.success` | 是 | 是 | 自动采购队列或快捷下单成功生成 OVHcloud 订单（含付款链接）。 |
| `order.failed` | 是 | 是 | 自动下单在创建购物车、挂载配置、结算阶段失败，或购物车实时不含税月价超出目标设定的 `maxMonthlyPrice` 上限（`PRICE_ABOVE_LIMIT`）。 |
| `order.delivered` | 是 | 是 | `/me/order` 中追踪的订单完成全部 4 步硬件交付（`INSTALLATION` 完成）。 |
| `service.expiring` | 否 | 是 | 账户名下的独服或 VPS 剩余有效期小于等于 `expiryDays`（默认 `7` 天）。 |
| `mcp.action` | 是 | 否 | AI 客户端通过 `/api/mcp` 触发 L2 免付下单/队列变更或 L3 可逆运维操作。 |

## 通道类型 1：纯出站 Telegram 机器人

1. 在 Telegram 中联系 [@BotFather](https://t.me/BotFather) 发送 `/newbot` 创建机器人，获取 **Bot Token**（形如 `123456789:ABCdef...`）。
2. 获取接收消息的 **Chat ID**（个人账号 ID、群组 ID `-100...` 或频道 ID）。
3. 在 **通知中心 → 添加通知渠道** 中选择 **Telegram Bot**，填写渠道名称、Bot Token 与 Chat ID，勾选订阅事件后点击**测试发送**并**保存配置**。

:::note[纯出站推送与面板跳转链接]
OVH Nexus 的 Telegram 集成严格采用**纯出站推送**（调用 `https://api.telegram.org`），无需向公网暴露任何入站回调接口。您可在通知中心填写可选的 **Panel URL**，使告警卡片附带返回控制台的直达链接。
:::

## 通道类型 2：SMTP 邮件推送

OVH Nexus 内置标准 SMTP 推送引擎（`internal/notify`），自动发送包含**纯文本降级正文**与**OVHcloud 品牌蓝响应式 HTML 邮件卡片**的多部分邮件（`UTF-8`，完美支持中文与特殊符号）。

| 配置项 | 示例值 | 说明 |
| :--- | :--- | :--- |
| **SMTP 服务器 (`host`)** | `smtp.qq.com` / `smtp.gmail.com` / `smtp.feishu.cn` | 发件服务器地址。 |
| **端口 (`port`)** | `465` (SSL/TLS) 或 `587` (STARTTLS) | SMTP 端口号。 |
| **加密方式 (`security`)** | `ssl`（465）、`starttls`（587）或 `none`（25） | 传输层加密模式（依托容器内置 `ca-certificates` 校验证书）。 |
| **用户名 (`username`)** | `alerts@example.com` | SMTP 登录账号（若为内网免密中继可留空）。 |
| **密码 / 授权码 (`password`)** | `xxxx-xxxx-xxxx` | 邮箱密码或客户端专用授权码（落盘经 `AES-256-GCM` 加密，接口返回自动掩码为 `****`）。 |
| **发件人地址 (`fromAddress`)** | `alerts@example.com` | 邮件 `From` 发件地址。 |
| **收件人列表 (`recipients`)** | `ops@example.com, me@example.com` | 收件人邮箱，支持多个地址以英文逗号分隔。 |

## 敏感凭据掩码保护 (`****`)

每当前端读取通知配置（`GET /api/notifications`）时，后端会自动将所有敏感密钥（Telegram 的 `token`、SMTP 的 `password`）脱敏为 `abcd****wxyz`。当您修改渠道名称、语言或订阅事件并点击**保存**或**测试**时，若密码框保持掩码或留空，后端会自动沿用 `/data/nexus.db` 中已加密保存的真实密钥，无需重复输入。
