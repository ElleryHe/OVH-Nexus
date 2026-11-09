# OVH Nexus (简体中文)

<p align="center">
  <img src="../public/favicon.svg" width="72" height="72" alt="OVH Nexus Logo" />
</p>

<p align="center">
  <strong>OVHcloud 云资产控制与自动化采购控制台</strong><br/>
  集独服/VPS 日常运维管控、实时订单交付追踪、到期续费大盘与全球机房库存监控抢购于一体的自托管平台。
</p>

<p align="center">
  <a href="../README.md">English</a> ·
  <a href="./README.fr.md">Français</a> ·
  <a href="./README.zh-CN.md"><strong>简体中文</strong></a>
</p>

<p align="center">
  <a href="https://ovh-nexus.ellery.me/zh-cn/"><img src="https://img.shields.io/badge/文档-ovh--nexus.ellery.me%2Fzh--cn-0050D7?logo=read-the-docs&logoColor=white" alt="Documentation" /></a>
  <a href="https://hub.docker.com/r/elleryhe/ovh-nexus"><img src="https://img.shields.io/badge/Docker-elleryhe%2Fovh--nexus-2496ED?logo=docker&logoColor=white" alt="Docker Hub" /></a>
  <img src="https://img.shields.io/badge/License-AGPL%20v3.0-blue.svg" alt="License: AGPL v3.0" />
</p>

---

## 项目简介

**OVH Nexus** 是一款面向 OVHcloud 用户（支持 `ovh-eu` 欧洲区、`ovh-ca` 加拿大/亚太区、`ovh-us` 美国区）的多账户自托管 Web 控制台。项目将**已购云资产日常运维管理**与**全球机房实时库存监控、公开货架 SKU 变动追踪及自动化采购**整合在同一个轻量级 Docker 容器中。

- **独立服务器与 VPS 管控**：硬件资产清单、MRTG 流量图表、IPMI/KVM 远程控制台、自动化系统重装（支持软硬 RAID / Proxmox ZFS 及一键注入 SSH 公钥）、IP 边缘防火墙与永久 Anti-DDoS 缓解、VPS 实例一键重启。
- **订单交付与续费到期总览**：实时追踪 OVH 官方 4 步硬件交付流水线（`CREATING` → `VALIDATING` → `DELIVERING` → `INSTALLATION`）与全账户服务到期倒计时预警。
- **全球实时库存矩阵与自动抢购队列**：免 API Key 拉取全球机房实时库存，自动展示商业型号（`KS-A`、`RISE-1`、`SCALE-a1`）、可读化硬件规格与精确到子配置的月付价格。
- **公开货架 SKU 变动监控与通知中心**：免 API Key 追踪所有在架/预上机架型号上新与下架（内置防抖熔断机制），支持多实例 **Telegram 机器人**与 **SMTP 邮件**按需订阅 7 类事件。

📚 **完整中文文档**：**[https://ovh-nexus.ellery.me/zh-cn/](https://ovh-nexus.ellery.me/zh-cn/)**

---

## 30 秒快速开始

```bash
# 1. 创建环境变量配置文件
cp .env.example .env
# 编辑 .env 并设置至少 12 位的高强度 API_SECRET_KEY（例如 openssl rand -hex 24）

# 2. 启动容器
docker compose up -d
```

浏览器访问 `http://localhost:20000`，使用 `.env` 中配置的 `API_SECRET_KEY` 登录即可。

---

## 文档导航

| 指南章节 | 链接 |
| :--- | :--- |
| **快速开始与安装升级** | [ovh-nexus.ellery.me/zh-cn/getting-started/quick-start/](https://ovh-nexus.ellery.me/zh-cn/getting-started/quick-start/) |
| **反向代理与 HTTPS (`COOKIE_SECURE`)** | [ovh-nexus.ellery.me/zh-cn/getting-started/reverse-proxy/](https://ovh-nexus.ellery.me/zh-cn/getting-started/reverse-proxy/) |
| **环境变量与 OVH API 凭据申请** | [ovh-nexus.ellery.me/zh-cn/configuration/environment-variables/](https://ovh-nexus.ellery.me/zh-cn/configuration/environment-variables/) |
| **功能指南（独服、VPS、库存、SKU 监控、通知中心）** | [ovh-nexus.ellery.me/zh-cn/features/dedicated-servers/](https://ovh-nexus.ellery.me/zh-cn/features/dedicated-servers/) |
| **安全加固建议与故障排查** | [ovh-nexus.ellery.me/zh-cn/operations/security/](https://ovh-nexus.ellery.me/zh-cn/operations/security/) |

---

## 开源协议与免责声明

本项目基于 **GNU Affero General Public License v3.0 (AGPL-3.0)** 协议开源，详见 [LICENSE](../LICENSE)、[CONTRIBUTING.md](../CONTRIBUTING.md) 与 [SECURITY.md](../SECURITY.md)。

> **OVH Nexus** 是独立的第三方开源工具，**与 OVH SAS / OVHcloud 官方无任何隶属、合作或背书关系**。“OVH”与“OVHcloud”均为 OVH SAS 的注册商标。
