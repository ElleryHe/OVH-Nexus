---
title: 全球实时库存矩阵与配置级价格
description: 免 API Key 查看 OVHcloud 全球机房实时库存、消费者友好型号、可读化硬件规格与精确到子配置的月付价格。
---

**实时库存矩阵（OVH Live Availability Matrix，`/ovh-availability`）** 直接拉取 OVHcloud 公开库存接口 `/dedicated/server/datacenter/availabilities`，并由 `internal/catalog` 自动关联公开 Eco 目录（`/order/catalog/public/eco`）与 Baremetal 目录（`/order/catalog/public/baremetal`）。

:::tip[无需配置 OVH API Key]
实时库存接口与公开价格目录均为 OVHcloud 公共接口。部署 OVH Nexus 后无需绑定任何 API 密钥即可直接使用本页面全部功能。
:::

## 消费者友好型号与可读化硬件规格

OVHcloud 原始库存接口仅返回内部简写代码（如 `24ska01`、`memory: ram-64g-ecc-3200`、`storage: raid-r1-2x480ssd-s10-2x6000sa`）。OVH Nexus 会自动将其翻译为清晰易读的商业规格：

1. **商业型号自动映射**：
   - 将每个 `planCode` 与本地缓存（12 小时 TTL）的 Eco（`Kimsufi`、`So you Start`、`Rise`）及 Baremetal（`Advance`、`Scale`、`High Grade`）目录进行匹配。
   - 对于刚上机架、尚未写入公开目录的预发布型号（如 `27skle01-v1`、`27risele01-v1`），自动启用确定性规则引擎推导商业名称（如 `KS-LE-1`、`KS-A`、`RISE-1`、`SCALE-a1`）。
   - 自动识别并归并亚太区域后缀（`-sgp`、`-syd`、`-mum`、`-apac`）与版本后缀（`-v1`、`-v2`）。
2. **三语本地化硬件规格解析 (`en` / `fr` / `zh-CN`)**：
   - **内存规格**：将 `ram-64g-ecc-3200` 解析为 `64 GB DDR4 ECC 3200 MHz`。
   - **存储与系统盘**：将 `softraid-2x512nvme` 解析为 `2 × 512 GB NVMe (软 RAID)`；将复合盘代码 `raid-r1-2x480ssd-s10-2x6000sa` 拆解为清晰的系统盘（`2 × 480 GB SSD, 硬 RAID 1`）与数据盘（`2 × 6 TB SATA, 硬 RAID 10`）。

## 配置级不含税预估月价与紧凑双视图

普通库存工具往往只能展示某个 `planCode` 的最低起步价，而同一型号下的高配内存或大容量 NVMe 子配置往往存在加价。OVH Nexus 通过 `/api/catalog/availability`、`/api/catalog/plans` 以及 `GET /api/catalog/plans/{planCode}/configs` 为**每一个具体的硬件子配置（`configKey`：`内存+硬盘`）计算不含税预估月价（`estimatedMonthlyPrice`）**：

```text
配置预估月价（不含税） = 基础型号月付价格 (Base Plan P1M)
                       + 该内存代码附加月费 (Memory Addon P1M)
                       + 该存储代码附加月费 (Storage Addon P1M)
```

- **紧凑单行实时库存矩阵 (`/availability`)**：
  - 将同一 `planCode` 的多套配置合并为**一机一行**（`-sgp`、`-syd`、`-mum` 等独立区域变体仍各自单独成行）。
  - 每行内置**配置下拉选择器**：
    - **全部配置 (`N`)** *(默认)*：每个机房单元格显示该型号所有配置中**最好的库存状态**以及有货配置数量角标。
    - **单个具体配置 (`内存 + 硬盘 — €XX.XX/月 · K 个机房`)**：选中某套具体配置后，该行的价格与所有机房单元格立即切换为该配置专属的不含税月价与实时库存状态。
- **紧凑可折叠服务器目录卡片 (`/servers`)**：
  - 桌面端采用每行 3～4 张的**默认折叠卡片网格**，折叠态仅展示：商业型号与 `planCode`、CPU、单行标配摘要、起始月价（`€XX.XX/月 起`）、有货机房紧凑国旗徽标与状态徽章。
  - 展开卡片后，原本分离的「默认配置」与「可选配置」合并为**单一可选配置列表**（标配带 `标配` 徽章且默认选中，每项显示不含税预估月价与机房库存）；默认仅列出有货配置与机房，支持勾选**「显示全部库存（含无货）」**查看完整列表，并将带宽、vRack 等附加包收纳在默认折叠的**「高级选项」**中。
- 支持按**价格从低到高**、**价格从高到低**一键排序，或按产品线（`KS`、`SYS`、`RISE`、`ADV`、`SCALE`、`HGR`）、目标机房（`GRA`、`SBG`、`RBX`、`FRA`、`BHS`、`TOR`、`SGP`、`SYD` 等）与上架状态（`全部`、`正式在售`、`已公布未开售`、`预售`、`新上架`）快速筛选。

## 三态生命周期 (`listed` / `announced` / `presale`) 与一键多目标监控

OVH Nexus 在 `/availability` 与 `/servers` 中精准区分三种机型状态：

- **正式在售 (`listingState: "listed"`)**：同时存在于公开价格目录（`eco` / `baremetalServers`）与实时机房库存接口中。
- **已公布未开售 (`listingState: "announced"`)**：已写入 OVHcloud 公开价格目录（含基础定价与默认规格），但尚未在实时库存接口开放任何配置（例如大促前提前写入目录的 `KS-LE`、`RISE-LE`、`SYS-LE` 限量版机型）。一旦库存接口开放，系统会自动发出**正式开售（`saleStarted: true`）**提醒。
- **预售 / 预上机架 (`listingState: "presale"`)**：机房已将新一代（`2024`–`2027` 代际）`planCode` 注册到实时库存接口，但尚未写入公开价格目录。
- **一键多目标快速监控弹窗 (`QuickMonitorDialog`)**：在 `/availability` 或 `/servers` 中点击**快速监控**或**加入采购队列**时，会自动带入当前选中的配置（`configKey`、`options`、预估月价）与机房；在弹窗中可添加多个配置级监控目标，并为每个自动下单目标独立设定采购数量与不含税**最高月价上限（`maxMonthlyPrice`）**。
- **全局三语机房地理位置标签**：控制台所有页面与通知模板均采用统一的三语机房地理字典（`en` / `fr` / `zh-CN`），在宽裕位置展示 `BHS（博阿努瓦，加拿大 🇨🇦）`、`TOR（多伦多，加拿大 🇨🇦）` 等完整地理信息，在紧凑位置展示带悬停全名提示的 `🇨🇦 BHS`、`🇨🇦 TOR` 国旗徽标。
