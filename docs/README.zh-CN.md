# OVH Nexus (简体中文)

<p align="center">
  <img src="../public/logo.png" width="76" height="76" alt="OVH Nexus Logo" />
</p>

<p align="center">
  <strong>OVHcloud 云资产控制与自动化采购控制台</strong><br/>
  集独服/VPS 日常运维管控、实时订单交付追踪、到期续费大盘、全球机房库存监控采购与 MCP AI 网关于一体的单静态二进制自托管平台。
</p>

<p align="center">
  <a href="../README.md">English</a> ·
  <a href="./README.fr.md">Français</a> ·
  <a href="./README.zh-CN.md"><strong>简体中文</strong></a>
</p>

<p align="center">
  <a href="https://ovh-nexus.ellery.me/zh-cn/" target="_blank" rel="noopener noreferrer"><img src="https://img.shields.io/badge/文档-ovh--nexus.ellery.me%2Fzh--cn-0050D7?logo=read-the-docs&logoColor=white" alt="Documentation" /></a>
  <a href="https://hub.docker.com/r/elleryhe/ovh-nexus" target="_blank" rel="noopener noreferrer"><img src="https://img.shields.io/badge/Docker-elleryhe%2Fovh--nexus-2496ED?logo=docker&logoColor=white" alt="Docker Hub" /></a>
  <a href="https://ovh-nexus.ellery.me/zh-cn/rss.xml" target="_blank" rel="noopener noreferrer"><img src="https://img.shields.io/badge/%E6%9B%B4%E6%96%B0%E6%97%A5%E5%BF%97-RSS-FFA500?logo=rss&logoColor=white" alt="更新日志 RSS 订阅" /></a>
  <a href="../LICENSE"><img src="https://img.shields.io/badge/License-AGPL%20v3.0-blue.svg" alt="License: AGPL v3.0" /></a>
</p>

---

## 项目简介

**OVH Nexus** 是一款面向 OVHcloud 用户（支持 `ovh-eu` 欧洲区、`ovh-ca` 加拿大/亚太区、`ovh-us` 美国区）的多账户自托管 Web 控制台。项目采用 **Go 1.27 单静态二进制架构**（基于 `alpine:3.24` 的约 `20 MB` 非 Root 容器，内嵌 React SPA 与纯 Go SQLite WAL 数据库），将**已购云资产日常运维管理**与**全球机房实时库存监控、公开货架 SKU 变动追踪及自动化采购**整合于单一进程中。

- **独立服务器与 VPS 管控**：硬件资产清单、MRTG 流量图表、IPMI/KVM 远程控制台、自动化系统重装（支持软硬 RAID / Proxmox ZFS 及一键注入 SSH 公钥）、IP 边缘防火墙与永久 Anti-DDoS 缓解、VPS 实例一键重启。
- **订单交付与续费到期总览**：实时追踪 OVH 官方 4 步硬件交付流水线（`CREATING` → `VALIDATING` → `DELIVERING` → `INSTALLATION`）与全账户服务到期倒计时预警。
- **全球实时库存矩阵、预售识别与自动采购队列**：免 API Key 拉取全球机房（`🇨🇦 BHS`、`🇨🇦 TOR`、`🇫🇷 GRA`、`🇩🇪 FRA`、`🇸🇬 SGP` 等三语地理标签）实时库存，自动展示商业型号（`KS-A`、`RISE-1`、`SCALE-a1`）、可读化硬件规格、精确到子配置的月付价格，支持**预售/待上架机型识别**（`2024`–`2027` 代数标签与“仅看预售”筛选）及与采购队列双向联动的**一键快速监控**弹窗。
- **公开货架 SKU 变动监控与纯出站通知中心**：免 API Key 追踪预上机架型号与 `presale -> listed` 正式上架变动（内置首轮存量基线静默 `baseline: true` 与防抖熔断机制），支持多实例 **Telegram 机器人**与 **SMTP 邮件**按需订阅 8 类事件，并支持 `en` / `fr` / `zh-CN` 通知模板语言切换。
- **实时可观测性与 SSE 日志控制台 (`/logs`)**：全标准化英文 `log/slog` 结构化日志，内置 SQLite 10,000 条滚动环形缓冲区、Server-Sent Events 实时推流、按模块/级别/关键词筛选、敏感凭据自动脱敏及 `.jsonl` / `.log` 一键导出。
- **MCP AI 网关与隐私安全沙盒 (`/api/mcp`)**：支持 Claude Code、Claude Desktop、Cursor 与 Antigravity 通过 Streamable HTTP (JSON-RPC 2.0) 接入，内置双向资产混淆引擎（`bm-01`、`vps-01`、`ord-01` 与掩码尾号 IP `*.*.*.42`）及**“只允许两步确认生成未付款订单、硬性禁止自动扣款”**（`auto_pay=false` 代码级锁死）财务防线。
- **单二进制安全加固**：原生抗 Slowloris 超时防护与 `2 MB` 请求体限制、`/data/nexus.db` 字段级 `HKDF-SHA256` + `AES-256-GCM` 静态加密、RFC 6238 TOTP 双因素分级门禁（`strict` / `sudo`），以及基于 `TRUSTED_PROXIES` 的可信代理真实 IP 提取。

📚 **完整中文文档**：<a href="https://ovh-nexus.ellery.me/zh-cn/" target="_blank" rel="noopener noreferrer"><strong>https://ovh-nexus.ellery.me/zh-cn/</strong></a>

---

## 快速开始 (Docker Compose)

OVH Nexus 通过 Docker Hub 分发多架构安全加固单容器镜像 `elleryhe/ovh-nexus:latest`（基于 `alpine:3.24`，监听 `8080` 端口；容器启动时自动初始化 `/data` 目录权限并通过 `su-exec` 降权至非 Root UID/GID `PUID:PGID`，默认 `65532:65532`），无需克隆代码仓库即可直接部署运行。

### 1. 创建工作目录与 `.env` 配置文件

```bash
mkdir -p ovh-nexus/data && cd ovh-nexus

cat > .env << EOF
# 必填：Web 控制台登录密码 / 本机 API 密钥（至少 12 位随机字符）
API_SECRET_KEY=$(openssl rand -hex 24)

# 必填：独立的静态数据加密主密钥（AES-256-GCM 落盘加密，至少 16 位字符，且不得与 API_SECRET_KEY 相同）
DATA_ENCRYPTION_KEY=$(openssl rand -hex 32)

# 可选：Web 控制台登录用户名，兼容浏览器与密码管理器自动填充（默认 Nexus，严格区分大小写）
ADMIN_USERNAME=Nexus

# 可选：若通过 HTTPS 反向代理访问请设为 true
COOKIE_SECURE=false

# 可选：默认界面语言 (en | fr | zh-CN)
DEFAULT_LOCALE=zh-CN

# 可选：容器时区，用于定时任务与通知时间戳（默认 UTC）
TZ=UTC

# 可选：后端日志级别 (DEBUG | INFO | WARNING | ERROR)
LOG_LEVEL=INFO

# 可选：受信反向代理 IP/CIDR（逗号分隔），用于解析 X-Forwarded-For / X-Real-IP
TRUSTED_PROXIES=

# 可选：用于 /data 目录属主初始化与 su-exec 降权运行的非 Root UID/GID（默认 65532:65532）
PUID=65532
PGID=65532
EOF
```

> **提示**：无需在宿主机手动执行 `chown` 修改 `./data` 权限。容器启动入口脚本会自动初始化 `/data`（权限 `0700`）并通过 `su-exec` 就地降权至 `PUID:PGID`（默认 `65532:65532`，Linux 非 Root 用户亦可按需调整 `.env` 中的 `PUID`/`PGID`）；同时原生兼容 Kubernetes `runAsNonRoot: true`。

### 2. 创建 `docker-compose.yml`

```yaml
services:
  ovh-nexus:
    image: ${DOCKER_IMAGE:-elleryhe/ovh-nexus:latest}
    container_name: ovh-nexus
    ports:
      - "20000:8080"
    environment:
      - API_SECRET_KEY=${API_SECRET_KEY:?请在 .env 中设置至少 12 位的强口令 API_SECRET_KEY}
      - DATA_ENCRYPTION_KEY=${DATA_ENCRYPTION_KEY:?请在 .env 中设置至少 16 位的独立加密密钥 DATA_ENCRYPTION_KEY}
      - ADMIN_USERNAME=${ADMIN_USERNAME:-Nexus}
      - COOKIE_SECURE=${COOKIE_SECURE:-false}
      - DEFAULT_LOCALE=${DEFAULT_LOCALE:-zh-CN}
      - TZ=${TZ:-UTC}
      - LOG_LEVEL=${LOG_LEVEL:-INFO}
      - TRUSTED_PROXIES=${TRUSTED_PROXIES:-}
      - PUID=${PUID:-65532}
      - PGID=${PGID:-65532}
    volumes:
      - ./data:/data
    restart: unless-stopped
```

### 3. 启动容器

```bash
docker compose up -d
```

浏览器访问 `http://localhost:20000`，输入登录用户名 `Nexus`（或自定义的 `ADMIN_USERNAME`，严格区分大小写）以及 `.env` 中生成的 `API_SECRET_KEY`（可通过 `grep API_SECRET_KEY .env` 查看）即可登录控制台。

---

## 文档导航

| 指南章节 | 链接 |
| :--- | :--- |
| **快速开始与安装升级** | <a href="https://ovh-nexus.ellery.me/zh-cn/getting-started/quick-start/" target="_blank" rel="noopener noreferrer">ovh-nexus.ellery.me/zh-cn/getting-started/quick-start/</a> |
| **反向代理与 HTTPS (`COOKIE_SECURE`)** | <a href="https://ovh-nexus.ellery.me/zh-cn/getting-started/reverse-proxy/" target="_blank" rel="noopener noreferrer">ovh-nexus.ellery.me/zh-cn/getting-started/reverse-proxy/</a> |
| **环境变量与 OVH API 凭据申请** | <a href="https://ovh-nexus.ellery.me/zh-cn/configuration/environment-variables/" target="_blank" rel="noopener noreferrer">ovh-nexus.ellery.me/zh-cn/configuration/environment-variables/</a> |
| **功能指南（独服、VPS、库存、SKU 监控、通知中心）** | <a href="https://ovh-nexus.ellery.me/zh-cn/features/dedicated-servers/" target="_blank" rel="noopener noreferrer">ovh-nexus.ellery.me/zh-cn/features/dedicated-servers/</a> |
| **安全加固建议与故障排查** | <a href="https://ovh-nexus.ellery.me/zh-cn/operations/security/" target="_blank" rel="noopener noreferrer">ovh-nexus.ellery.me/zh-cn/operations/security/</a> |

---

## 开源协议与免责声明

本项目基于 **GNU Affero General Public License v3.0 (AGPL-3.0)** 协议开源，详见 [LICENSE](../LICENSE)、[版本更新日志](https://ovh-nexus.ellery.me/zh-cn/about/changelog/)（[RSS 订阅](https://ovh-nexus.ellery.me/zh-cn/rss.xml)）、[贡献指南](https://ovh-nexus.ellery.me/zh-cn/about/contributing/) 与 [安全加固与漏洞披露策略](https://ovh-nexus.ellery.me/zh-cn/operations/security/)。

> **OVH Nexus** 是独立的第三方开源工具，**与 OVH SAS / OVHcloud 官方无任何隶属、合作或背书关系**。“OVH”与“OVHcloud”均为 OVH SAS 的注册商标。
