---
title: 版本更新日志 (Changelog)
description: OVH Nexus 的版本发布历史与功能演进记录。
---

:::note[当前最新版本：v1.0.1]
您可以在 [GitHub Releases](https://github.com/ElleryHe/OVH-Nexus/releases) 与 [Docker Hub (`elleryhe/ovh-nexus`)](https://hub.docker.com/r/elleryhe/ovh-nexus) 查看所有正式版发布包与多架构容器镜像。
:::

本页面遵循 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.1.0/) 规范与 [语义化版本 (SemVer)](https://semver.org/lang/zh-CN/) 记录 **OVH Nexus** 的重要变更。

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
