---
title: 项目简介
description: 了解 OVH Nexus 的系统架构、核心能力与支持的 OVHcloud 区域。
---

**OVH Nexus** 是一款面向 OVHcloud 用户（全面支持 `OVH 欧洲区`、`OVH 加拿大/亚太区` 与 `OVH 美国区`）的多账户自托管 Web 控制台。项目将**已购云资产日常运维管理**与**全球机房实时库存监控、公开货架 SKU 变动追踪及自动化采购**整合在同一个轻量级 Docker 容器中。

## 为什么选择 OVH Nexus？

在管理跨区域（`ovh-eu`、`ovh-ca`、`ovh-us`）的多个 OVHcloud 账户，或者盯盘抢购热门高性价比机型（如 `Kimsufi`、`So you Start`、`Rise` 活动机或 `Advance`/`Scale` 独立服务器）时，传统方式往往需要在多个脚本、网页标签页和 API 工具之间来回切换。

OVH Nexus 在单一控制台内打通了完整生命周期：

1. **免 API Key 公共情报监控**：无需配置任何 OVHcloud API 密钥，即可直接查看全球机房实时库存、精确到子配置的月付价格、消费者友好型号（`KS-A`、`RISE-1`、`SCALE-a1`）以及公开货架 SKU 上新/下架变动时间线。
2. **毫秒级自动化采购**：支持按型号（`planCode` + 机房 + 附加组件）加入抢购队列，或直接从实时库存矩阵一键狙击指定硬件子配置（`FQN`），在补货瞬间自动创建购物车并生成订单。
3. **订单与装机交付可视化**：订单生成后，直接在面板内获取官方支付链接，并实时追踪 4 步硬件交付流水线（`CREATING` → `VALIDATING` → `DELIVERING` → `INSTALLATION`）。
4. **已购资产深度运维**：集中管控名下所有已购独立服务器与 VPS——支持自定义软硬 RAID / Proxmox ZFS 自动化重装系统、一键注入账户 SSH 公钥、打开 IPMI/KVM 远程终端、配置 IP 边缘防火墙与永久 Anti-DDoS 缓解，以及监控全账户到期续费状态。

## 系统架构

OVH Nexus 通过单容器镜像（`elleryhe/ovh-nexus`）分发，内部集成 Nginx 与 Python 后端：

```text
浏览器 (HTTPS / HTTP)
   │
   ▼
┌───────────────────────────────────────────────────────────┐
│ Docker 容器 (elleryhe/ovh-nexus)                          │
│                                                           │
│  ┌─────────────────────────────────────────────────────┐  │
│  │ Nginx (容器内 80 端口 -> 宿主机 20000 端口)         │  │
│  │  • 托管 React 前端静态资源 (/usr/share/nginx/html)  │  │
│  │  • 下发 CSP 与安全响应头                            │  │
│  │  • 将 /api/* 反向代理至 127.0.0.1:19998             │  │
│  └─────────────────────────┬───────────────────────────┘  │
│                            │                              │
│                            ▼                              │
│  ┌─────────────────────────────────────────────────────┐  │
│  │ Python Flask 后端 (127.0.0.1:19998)                 │  │
│  │  • 会话 Cookie 认证与登录防暴力破解限流             │  │
│  │  • 多账户 OVHcloud API 客户端注册表                 │  │
│  │  • CatalogService (Eco + Baremetal 12 小时磁盘缓存) │  │
│  │  • ServerMonitor 与自动抢购队列守护线程             │  │
│  │  • SkuWatcher (公开货架 SKU 差分与防抖熔断引擎)     │  │
│  │  • NotificationHub (Telegram 机器人与 SMTP 邮件分发)│  │
│  └─────────────────────────────────────────────────────┘  │
│       │                    │                    │         │
│       ▼                    ▼                    ▼         │
│  /app/backend/data   /app/backend/logs   /app/backend/cache
└───────────────────────────────────────────────────────────┘
```

## 支持的 OVHcloud 区域

| 区域代码 | API 端点 | 覆盖范围 |
| :--- | :--- | :--- |
| `ovh-eu` | `https://eu.api.ovh.com/1.0` | 欧洲、非洲、中东（法国、德国、英国、波兰等机房） |
| `ovh-ca` | `https://ca.api.ovh.com/1.0` | 加拿大及亚太区（魁北克 `BHS`、新加坡 `SGP`、悉尼 `SYD`、孟买 `MUM`） |
| `ovh-us` | `https://api.us.ovhcloud.com/1.0` | 美国区（弗吉尼亚 `VIN`、俄勒冈 `HIL`） |

## 下一步

- 阅读 [30 秒快速开始](/zh-cn/getting-started/quick-start/)，使用 Docker Compose 一键启动实例。
- 阅读 [反向代理与 HTTPS](/zh-cn/getting-started/reverse-proxy/)，配置安全的公网 HTTPS 访问。
- 阅读 [OVHcloud API 凭据申请](/zh-cn/configuration/ovh-api-credentials/)，绑定您的 OVHcloud 账户。
