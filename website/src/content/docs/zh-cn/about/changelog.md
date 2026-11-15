---
title: 版本更新日志 (Changelog)
description: OVH Nexus 的版本发布历史与功能演进记录。
---

:::note[当前最新版本：v1.2.3]
您可以在 [GitHub Releases](https://github.com/ElleryHe/OVH-Nexus/releases) 与 [Docker Hub (`elleryhe/ovh-nexus`)](https://hub.docker.com/r/elleryhe/ovh-nexus) 查看所有正式版发布包与多架构容器镜像。
:::

本页面遵循 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.1.0/) 规范与 [语义化版本 (SemVer)](https://semver.org/lang/zh-CN/) 记录 **OVH Nexus** 的重要变更。

## [1.2.3] - 响应式侧边栏布局修复、首次部署 OVH API 引导闭环、2FA 状态同步与文档站 SEO 增强

### 问题修复 (Fixed)

- **Tailwind CSS v3 响应式选择器转义异常与侧边栏消失修复**：
  - 将 `package.json` `overrides` 中的 `postcss-selector-parser` 锁定为 `^6.1.6`（修复误解析至 `7.2.0` 导致 `.lg\:block` 等响应式类名被三重转义失效、桌面端左侧导航栏被隐藏的问题）。
  - 在 `Layout.tsx` 中补齐 `< 1024px` 紧凑视口顶部导航栏与菜单按钮，彻底消除 `768px–1023px` 视口盲区。
- **首次部署 OVH API 填写入口闭环与顶部提示条优化**：
  - 在左侧主导航新增 **「OVH API 账户」(`/api-accounts`)** 入口，并在「系统与安全设置」(`/settings`) 中增加多账户管理卡片跳转。
  - 将顶部「立即配置 API」按钮直连 `/api-accounts` 填写表单，在配置页自动隐藏重复提示条并支持一键收起关闭，同时补齐首次绑定 2FA 的前置引导提示。
- **2FA (TOTP) 绑定后状态同步修复**：
  - 统一后端 `/api/auth/totp/status` 与 `/api/auth/totp/activate` 返回的 `totpEnabled` / `enabled` 字段及中文错误提示字段（`message`），修复绑定验证成功后前端仍显示“未启用”的问题。

### 新增功能 (Added)

- **文档门户 SEO 与搜索引擎抓取优化 (`website/`)**：
  - 新增构建期 `/sitemap.xml` 直出生成（与 `/sitemap-index.xml` 同步输出），注入 `<lastmod>` 更新日期与 `hreflang="x-default"` 多语言互链；完善 `robots.txt` 搜索引擎放行规则、`_headers` MIME 响应头与 OpenGraph 社交预览图（`og-image.jpg`）。

---

## [1.2.2] - 依赖基线升级与 Dependabot 跨大版本治理加固

### 变更与升级 (Changed)

- 升级后端依赖基线（`backend/requirements.txt` 中 `cryptography>=50.0.2`、`backend/requirements-dev.txt` 中 `pytest>=9.2.0`）与在线文档门户框架（`website/package.json` 中 `@astrojs/starlight@^0.42.11`）。
- 完善 `.github/dependabot.yml` 中前端（`/`）与文档站（`/website`）的 `ignore` 策略，显式锁定 `react` / `react-dom`（`<19.0.0`）、`tailwindcss`（`<4.0.0`）与 `recharts`（`<3.0.0`）大版本边界，并全局屏蔽非安全类的 `semver-major` 跨大版本自动 PR。

---

## [1.2.1] - AES-256-GCM 磁盘静态加密、TOTP 2FA 分级安全门禁、欧盟 14 天撤回权保护与 Node 24 / Python 3.12 LTS 升级

### 安全加固 (Security)

- **本地磁盘敏感凭证静态加密 (`DATA_ENCRYPTION_KEY`, `backend/crypto_vault.py`)**：
  - 新增必填且独立的环境变量 `DATA_ENCRYPTION_KEY`（长度不少于 16 位，缺失、弱密钥或与 `API_SECRET_KEY` 相同时启动期直接阻断报错）。
  - 基于 `HKDF-SHA256` 密钥派生与 `AES-256-GCM` 认证加密算法（`enc:v1:<nonce>:<ciphertext>`），对 `./data/config.json`（`appKey`、`appSecret`、`consumerKey`、`tgToken`、`notifications.channels[*].config.token/password`）、`./data/accounts.json`、`./data/totp_config.json` 以及 `./data/session_secret` / `./data/webhook_secret` 全面施行落盘加密，并在启动时自动将历史明文配置无缝平滑迁移为密文。
  - 升级 `./data/mcp_config.json` 存储结构，仅持久化不可逆的 `SHA-256` 哈希摘要（`tokenHash`）与脱敏预览（`maskedToken`），彻底消除磁盘明文 MCP Token。
- **TOTP 双因素认证 (RFC 6238) 与分级高危操作安全门禁 (`backend/totp_service.py`)**：
  - 原生实现 RFC 6238 TOTP 动态口令认证，支持离线二维码绑定、30 秒窗口防重放（`lastUsedStep`）以及 8 个一次性应急恢复码（`XXXX-XXXX`）。
  - 全局强制高危门禁：**未绑定启用 2FA 时，所有高危操作路由一律返回 `403 TOTP_SETUP_REQUIRED` 拦截，且 MCP 网关 `L3`（可逆运维）被底层强制锁定关闭**。
  - 双层 Step-Up 验证体验：`strict` 级（重装系统、服务退订销毁、过户转移、新增/编辑/删除 API 账户密钥、关闭 2FA）要求每次请求均附带最新动态码；`sudo` 级（独服/VPS 重启、救援模式、IPMI 控制台、边缘防火墙/网络变更、MCP 配置变更、开启自动扣款或放弃撤回权）验证一次后授予当前会话 **5 分钟免重复输入窗口**。
- **欧盟 14 天消费者撤回权保护与支付风控 (`waiveRetractationPeriod` & `auto_pay`)**：
  - 将下单引擎 `purchase_server()` 与抢购队列 `/api/queue` 中的 `waiveRetractationPeriod` 由原先的强制 `True` 改为**默认 `False`**（默认保留欧盟 14 天法定无理由撤回权），并在队列与服务器目录页提供任务级双开关（`auto_pay` + `waive_retractation_period`），开启任意一项均需通过 2FA Sudo 验证；MCP 工具层硬编码锁死两者为 `False`。

### 变更与升级 (Changed)

- 将容器构建基础镜像与 CI 运行时升级至 **Node 24 LTS (`node:24-alpine`)** 与 **Python 3.12 (`python:3.12-slim`)**。
- 升级 GitHub Actions（`actions/setup-node@v7`、`actions/setup-python@v7`、`docker/setup-buildx-action@v4`、`peter-evans/dockerhub-description@v5`）与前端依赖（`lucide-react@^1.72.0`、`sonner@^2.0.8`、`autoprefixer@^10.6.2`、`@radix-ui/react-alert-dialog@^1.2.1`、`@radix-ui/react-tooltip@^1.4.1`、`wrangler@^4.160.1`、`qrcode@^1.5.4`），并将 `.github/dependabot.yml` 目标分支切换为 `develop` 及开启分组更新策略。

---

## [1.2.0] - MCP AI 网关、双向隐私混淆护盾与“免付款下单”安全沙盒

### 新增功能 (Added)

- **内置 Model Context Protocol (MCP) Streamable HTTP 网关 (`/api/mcp`)**：
  - 对标官方 `ovh/ovhcloud-cli` 交互范式，内置遵循 JSON-RPC 2.0 标准的无状态 Streamable HTTP 端点；默认安全关闭，采用与 WebUI 登录密码彻底隔离的专属高熵令牌（`nxs_mcp_...`）鉴权，并内置按 IP 防爆破限流。
  - **三级可配置权限沙盒（共 20 个 MCP 工具）**：
    - `L1 只读查询`（11 个工具）：脱敏后的多账户画像、独服列表与详情、VPS 列表、边缘防火墙状态、服务到期倒计时、订单 4 步交付进度、全球机房实时库存与 SKU 上新变动。
    - `L2 免付采购`（5 个工具）：两步确认生成【未付款订单】（`order_quote` 签发 5 分钟报价单 → 用户确认后调用 `order_create_unpaid`）、抢购队列管理与库存监控订阅。
    - `L3 可逆运维`（4 个工具）：独服/VPS 硬件重启、重置卡死的 IPMI 会话、边缘防火墙规则与永久防洪开关。
  - **L4 代码级永久财务与高危红线**：
    - 在后端代码层无条件锁死 `auto_pay = False` 与 `quantity = 1`（绝不触发自动扣款），内置每小时下单速率限制，并永久封禁重装系统、退订销毁服务、读取带令牌的 KVM 远程控制台链接及修改 SSH/API 密钥。
- **双向隐私混淆护盾 (`Balanced Shield`, `backend/mcp/privacy_shield.py`)**：
  - 自动将 OVH 账户（`acct-01`）、包含 IP 段的独服主机名（`bm-01`）、VPS（`vps-01`）、公网 IP（`ip-01`，仅保留末段尾号 `*.*.*.42`）与订单号（`ord-01`）映射为持久化代号，并彻底剥离含 `orderPassword` 的付款链接与个人敏感信息。
- **WebUI「MCP AI 网关」控制台 (`/mcp`) 与通知中心审计事件 (`mcp.action`)**：
  - 在侧边栏「账户与系统」分组新增 **MCP AI 网关** 页面，支持令牌轮换/吊销、L1–L3 权限开关、多账户可见性白名单、一键复制客户端配置（Claude Code、Claude Desktop、Cursor、Antigravity）、隐私混淆映射表查阅，以及 L2/L3 写操作实时触发 `mcp.action` 通知告警。

---

## [1.1.1] - 后端测试依赖安全补丁

### 安全修复 (Security)

- **后端测试依赖升级 (`CVE-2025-71176` / `GHSA-6w46-j5rx-g56g`)**：
  - 将 `backend/requirements-dev.txt` 中的 `pytest` 由 `8.4.2` 升级至 `>=9.0.3`，修复 UNIX 临时目录处理安全漏洞（`CVE-2025-71176`），实现全仓 npm 与 pip 依赖的 GitHub Dependabot 告警彻底清零（0 Open Alerts）。

---

## [1.1.0] - 依赖链安全加固清零、三语系统架构图与文档体验升级

### 安全修复 (Security)

- **全仓依赖安全告警清零 (`0 vulnerabilities`)**：
  - 将主项目中的 `react-router-dom` 升级至 `>= 7.18.5`、`vite` 升级至 `>= 8.3.6`，并将文档站 `website/` 中的 `astro`（`>= 7.4.7`）、`@astrojs/starlight`（`>= 0.42.10`）与 `sharp`（`>= 0.35.5`）升级至最新安全版本。
  - 在主目录与 `website/` 的 `package.json` 中通过 `overrides` 强制锁定构建期子依赖安全版本（`esbuild >= 0.28.1`、`glob >= 14.0.2`、`minimatch >= 11.0.1`、`postcss-selector-parser >= 6.1.5`、`zod >= 4.6.6`），彻底消除 GitHub Dependabot 报出的全部 31 条安全告警。

### 新增功能 (Added)

- **三语高清系统架构图**：
  - 为英、法、中三语《项目简介》页面新增本地化高清系统架构图（`architecture-en.jpg`、`architecture-fr.jpg`、`architecture-zh.jpg`）。
- **文档站首页版权底栏与外链新窗口跳转**：
  - 在文档站首页底部新增年份自动更新的版权与协议底栏（`© <年份> OVH-Nexus · AGPL-3.0`），并为所有外站链接自动启用新窗口打开（`target="_blank"`）。

### 变更与优化 (Changed)

- **Docker Node 22 LTS 构建升级与 README 开箱即用配置**：
  - 将 `Dockerfile` 第一阶段 `frontend-builder` 基础镜像由 `node:18-alpine` 升级至长期支持版 `node:22-alpine` (LTS)。
  - 完善 `README.md`、`docs/README.fr.md` 与 `docs/README.zh-CN.md` 的快速开始章节，提供完整的独立 `docker-compose.yml` 与 `.env` 配置示例，以及外站链接新窗口跳转属性。
  - 修复文档站首页卡片图标键名（`telegram` 与 `padlock`）。

---

## [1.0.1] - 官方文档站上线与 CI/CD 构建优化

### 新增功能 (Added)

- **官方三语文档站点 (`website/`)**：
  - 基于 **Astro Starlight** 与 **Pagefind** 离线全文检索引擎构建全量三语（`en`、`fr`、`zh-CN`）官方文档站，覆盖 5 大章节共 69 个页面（`https://ovh-nexus.ellery.me`）。
  - 配置基于 **Cloudflare Workers Static Assets** 的纯静态资产部署架构（`wrangler.jsonc`，关闭 `workers_dev` 与 `preview_urls`），并配置严格的 CSP 与缓存响应头（`_headers`）。

### 变更与优化 (Changed)

- **仓库 README 精简与 CI 触发隔离**：
  - 精简 `README.md`、`docs/README.fr.md` 与 `docs/README.zh-CN.md`，提供 30 秒快速开始与文档站直达导航。
  - 在 `.github/workflows/dockerhub.yml` 中配置 `paths-ignore` 忽略规则，并在 `.dockerignore` 中排除 `website/`，避免纯文档变更触发不必要的 Docker 镜像重编译。

---

## [1.0.0] - 首个公开正式版发布

### 新增功能 (Added)

- **独立服务器与云资产深度管控**：
  - 独立服务器硬件详情、MRTG 实时网卡流量图表、IPMI/KVM 远程控制台（`SOL`、`KVM`、`xterm.js`）及电源/救援模式控制。
  - 自动化系统重装（`/dedicated/server/{serviceName}/install/start`），支持软/硬 RAID 自定义分区、Proxmox VE ZFS 根文件系统模板，以及一键注入账户 SSH 公钥（`/me/sshKey`）。
  - IP 边缘防火墙（`/ip/{ip}/firewall`）规则编排与永久 VAC Anti-DDoS 缓解开关（`/ip/{ip}/mitigation`）。
- **已购 VPS 实例管控**：
  - 一屏查看名下所有已购 VPS（`/vps`）的机型配额、IPv4/IPv6 地址、运行状态与到期日，并支持一键重启（`/vps/{serviceName}/reboot`）。
- **订单实时交付流水线与到期续费矩阵**：
  - 同步 OVHcloud 官方订单（`/me/order`）、提取付款链接与 PDF 账单，并实时展示 4 步硬件交付流水线（`CREATING` → `VALIDATING` → `DELIVERING` → `INSTALLATION`）。
  - 汇总全账户独服与 VPS 到期倒计时（`/serviceInfos`），提供 `<= 7 天` 紧急红色告警与 `<= 30 天` 临期黄色提醒。
- **全球实时库存矩阵、目录服务与配置级定价**：
  - 免 API Key 拉取并缓存公开 Eco + Baremetal 目录（12 小时 TTL），内置未开售新机型确定性名称推导引擎。
  - 内存与存储规格本地化可读格式转换（`DDR4/DDR5 ECC`、`NVMe/SSD/SATA`、`软/硬 RAID`）以及精确到硬件子配置（`FQN`）的月付价格计算与价格排序。
- **公开货架 SKU 变动监控**：
  - 免 API Key 后台货架扫描引擎（`SkuWatcher`），内置首轮基线静默、连续 2 轮缺失确认下架、单轮骤降 >30% 熔断防抖及区域后缀去重。
- **可插拔多通道通知中心**：
  - 支持同时配置多个 **Telegram 机器人**与 **SMTP 邮件**（`SSL/TLS`、`STARTTLS`、`Plain` 及响应式 HTML 邮件模板），按需独立订阅 7 类细粒度事件。
- **多账户架构、安全加固与三语自适应主题**：
  - 多账户注册表（`ovh-eu`、`ovh-ca`、`ovh-us`）与 `X-OVH-Account` 请求级隔离。
  - 启动期强制校验 `API_SECRET_KEY`、`HttpOnly` 会话 Cookie、防暴力破解限流、敏感凭据掩码（`****`）及 `0600` 数据权限收紧。
  - 内置 **简体中文 (`zh-CN`)**、**English (`en`)**、**Français (`fr`)** 三语无缝切换与纯粹白/中性碳黑 + OVHcloud 品牌蓝自适应主题。
