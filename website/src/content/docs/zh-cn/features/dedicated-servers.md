---
title: 独立服务器深度管控
description: 管理 OVHcloud 独立服务器硬件资产、MRTG 流量图表、IPMI/KVM 控制台、自动化重装系统、SSH 公钥注入与 IP 边缘防火墙。
---

**独立服务器管控（Server Control，`/server-control`）** 模块让您无需登录繁琐的 OVHcloud 官方后台，即可对当前账户名下的所有独立服务器（`Kimsufi`、`So you Start`、`Rise`、`Advance`、`Scale`、`High Grade`）执行全套日常运维操作。

## 硬件资产清单与 MRTG 实时流量监控

选中任意一台独立服务器后，面板将实时展示：

- **硬件与机架信息**：商业型号名称、CPU 型号与核心数、内存容量、磁盘阵列布局、所在机房代码及机柜编号（Rack ID）。
- **网络与 MRTG 流量遥测**：公网 IPv4/IPv6 地址、反向解析（rDNS），以及直接拉取自 `/dedicated/server/{serviceName}/statistics` 的 **MRTG 实时上行/下行带宽图表**（支持按小时、天、周、月、年切换时间窗口）。
- **电源与救援模式（Rescue）**：支持一键硬件重启（`/reboot`）或切换启动模式进入 `rescue64-pro` 救援系统。

## IPMI / KVM 带外远程控制台

针对配备 BMC / IPMI 模块的独立服务器：

- **SOL 串口终端与 xterm.js**：在浏览器内直接启动交互式终端会话，或获取 SSH SOL 连接凭据。
- **KVM over IP 远程控制**：自动将您当前的公网 IP 加入白名单并调用 `/dedicated/server/{serviceName}/features/ipmi/access` 申请临时 KVM 控制台访问权限，实时轮询任务就绪状态。

## 自动化系统重装与一键 SSH 公钥注入

OVH Nexus 对裸金属系统重装流程（`/dedicated/server/{serviceName}/install/start`）进行了深度优化：

1. **官方系统镜像发现**：自动拉取当前机器兼容的所有 OVHcloud 官方模板（Debian、Ubuntu、Rocky Linux、AlmaLinux、Proxmox VE、Windows Server 等）。
2. **自定义分区与软/硬 RAID 及 ZFS**：
   - 支持配置硬件或软件 RAID 级别（`RAID 0`、`RAID 1`、`RAID 5`、`RAID 10` 或 `JBOD` 单盘模式）。
   - 支持一键生成 **Proxmox VE ZFS** 根文件系统分区配置。
   - 支持自定义根分区/数据分区容量与主机名（Hostname）。
3. **账户级 SSH 公钥管理 (`/me/sshKey`)**：
   - 在重装弹窗或工具栏中打开 **SSH 公钥管理**，直接查看、上传或删除保存在您 OVHcloud 账户下的 SSH 公钥。
   - 重装系统时只需勾选一个 `sshKeyName`，OVHcloud 装机流水线便会自动将该公钥写入新系统的 `authorized_keys`，实现装机完成后免密安全登录。

## IP 边缘防火墙与 Anti-DDoS 缓解

直接在面板内管控 OVHcloud 骨干网级别的硬件边缘防火墙（`/ip/{ip}/firewall`）与 VAC Anti-DDoS 防护：

- **边缘防火墙开关**：一键启用或停用绑定在服务器 IPv4 地址上的 OVH 硬件边缘防火墙。
- **规则编排（优先级 `0–19`）**：添加或删除允许（`permit`）或拒绝（`deny`）规则，支持指定协议（`tcp`、`udp`、`icmp`、`ipv4`）、源 CIDR 网段、目标端口以及 TCP 标志位（`syn`、`established`）。
- **永久 Anti-DDoS 缓解模式**：查看当前 IP 的缓解状态（`auto` 自动感知 vs `permanent` 永久开启），并支持针对游戏服或高敏感业务一键开启永久 VAC 流量清洗（`/ip/{ip}/mitigation`）。
