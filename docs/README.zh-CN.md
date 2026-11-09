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

---

## 项目简介

**OVH Nexus** 是一款面向 OVHcloud 用户（支持 `OVH-EU` 欧洲区、`OVH-CA` 加拿大/亚太区、`OVH-US` 美国区）的多账户自托管 Web 控制台。项目将**已购云资产日常运维管理**与**热门机房库存监控及自动化采购**整合在同一个轻量级 Docker 容器中。

### 核心功能

1. **独立服务器深度管控 (`/dedicated/server`)**
   - 硬件配置详情、网络带宽规格、MRTG 实时网卡流量图表、IPMI/KVM 远程控制台（SOL 串口终端、KVM、xterm.js）。
   - 自动化系统重装：支持软/硬 RAID 自定义分区、Proxmox VE ZFS 根文件系统，以及**重装时一键注入账户 SSH 公钥 (`/me/sshKey`)**。
   - **IP 边缘防火墙与 Anti-DDoS 缓解 (`/ip/{ip}/firewall` & `/mitigation`)**：直接在面板内管理 OVH 硬件边缘防火墙规则（允许/拒绝指定端口与源 CIDR），并支持一键开启永久 Anti-DDoS 缓解。
2. **已购 VPS 实例管控 (`/vps`)**
   - 一屏查看名下所有已购 VPS 的运行状态、机型规格（vCore / 内存 / 磁盘）、公网 IPv4/IPv6 地址、所在区域与到期时间，并支持一键重启 VPS (`/vps/{serviceName}/reboot`)。
3. **订单与实时交付进度追踪 (`/me/order` & `/followUp`)**
   - 直接拉取账户历史订单、待付款订单支付链接与 PDF 账单，并实时展示 OVH 官方 4 步硬件交付流水线（`CREATING` 创建 → `VALIDATING` 审核 → `DELIVERING` 交付中 → `INSTALLATION` 装机完成）。
4. **服务到期与自动续费总览 (`/serviceInfos`)**
   - 汇总名下所有独服与 VPS 的到期日、剩余天数颜色预警（`<=7天` 红色告警、`<=30天` 黄色提醒、`>30天` 绿色正常）以及续费策略（自动续费 / 手动续费 / 到期销毁）。
5. **全球机房实时库存、SKU 变动监控与多通道通知中心**
   - 免 API Key 直接拉取 OVH 全球机房实时库存，自动关联公开 Eco / Baremetal 目录展示**消费者友好商业型号（如 `KS-A`、`RISE-1`）、可读化硬件规格（内存代数/ECC/频率、软硬 RAID 存储）与配置级月付价格**。
   - **全量货架 SKU 变动监控**：免 API Key 轮询追踪所有在架（含未开售预上机架）SKU 的上新、下架与配置增减，内置首轮基线静默、连续 2 轮缺失确认下架与单轮锐减熔断防误报机制。
   - **可插拔多通道通知中心**：支持配置多个 **Telegram 机器人**与 **SMTP 邮件推送**（支持 `SSL/TLS`、`STARTTLS`、`Plain` 与 HTML 邮件模板），按需订阅 7 类库存、SKU 变动与生命周期事件。
6. **内置三语界面 (`en` / `fr` / `zh-CN`)**
   - 登录页与侧边栏均内置 `EN | FR | 中文` 实时切换器，也可通过环境变量 `DEFAULT_LOCALE=zh-CN` 指定默认语言。

---

## 快速部署 (Docker Compose)

本项目通过 Docker Hub 分发单容器镜像 `elleryhe/ovh-nexus:latest`。

### 1. 创建 `.env` 配置文件

```bash
cat > .env << 'EOF'
# 必填：Web 控制台登录密码 / API 访问密钥
API_SECRET_KEY=请替换为您自己的高强度密码

# 可选：默认界面语言 (en | fr | zh-CN)
DEFAULT_LOCALE=zh-CN

# 可选：若仅通过 HTTPS 反代访问可设为 true
COOKIE_SECURE=false
EOF
```

### 2. 启动容器

```bash
docker compose up -d
```

浏览器访问 `http://localhost:20000`，输入 `.env` 中配置的 `API_SECRET_KEY` 即可进入控制台。

---

## 开源协议与免责声明

- **开源协议**：本项目基于 **GNU Affero General Public License v3.0 (AGPL-3.0)** 协议开源（详见 [LICENSE](../LICENSE)）。任何基于本项目的修改或通过网络提供服务的部署均须遵循 AGPL-3.0 协议开放完整源代码。
- **非官方声明**：**OVH Nexus** 是独立的第三方开源工具，**与 OVH SAS / OVHcloud 官方无任何隶属、合作或背书关系**。“OVH”与“OVHcloud”均为 OVH SAS 的注册商标。请合理合规使用本工具并遵守 OVHcloud 服务条款与 API 速率限制。
