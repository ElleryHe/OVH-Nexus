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

## 精确到子配置 (`FQN`) 的月付价格计算

普通库存工具往往只能展示某个 `planCode` 的最低起步价，而同一型号下的高配内存或大容量 NVMe 子配置往往存在加价。OVH Nexus 会为**每一个具体的硬件子配置（`FQN`）精确计算总月付价格**：

```text
子配置月付总价 = 基础型号月付价格 (Base Plan P1M)
               + 该内存代码附加月费 (Memory Addon P1M)
               + 该存储代码附加月费 (Storage Addon P1M)
               + 系统盘附加月费 (System Storage Addon P1M)
```

- 当同一型号下不同子配置存在差价时，外层型号卡片会显示起步价（如 `€XX.XX/月 起`），而展开后的每一张子配置卡片上则会直接展示该配置专属的精确月付价格徽标（及初装费）。
- 支持按**价格从低到高**、**价格从高到低**一键排序，或按产品线（`KS`、`SYS`、`RISE`、`ADV`、`SCALE`、`HGR`）、目标机房（`GRA`、`SBG`、`RBX`、`FRA`、`BHS`、`TOR`、`SGP`、`SYD` 等）与有货状态快速筛选。

## 预售 / 待上架机型识别与一键快速监控

OVHcloud 机房常在新一代服务器（`2024`–`2027` 代际）正式写入公开价格目录前数小时至数天，先行将 `planCode` 注册到实时库存接口中：

- **预售徽标与硬件代数推断 (`listingState: "presale"`)**：**实时库存矩阵（`/availability`）** 与 **服务器目录（`/servers`）** 会统一识别尚未进入公开目录的预售机型，醒目展示**预售（Presale）**徽标与硬件代数标签（`2024`–`2027`），并提供**仅看预售（Presale Only）**一键筛选开关。
- **一键快速监控弹窗 (`QuickMonitorDialog`)**：在 `/availability` 或 `/servers` 的任意机型行点击**快速监控（Quick Monitor）**，即可在同一弹窗内完成多机房勾选（实时显示各机房当前库存状态）、到货/断货通知开关、内存/硬盘子配置精确匹配以及自动下单配置（针对预售机型，上架前可正常触发库存通知，待公开价格目录正式上架后自动激活下单）。
- **全局三语机房地理位置标签**：控制台所有页面与通知模板均采用统一的三语机房地理字典（`en` / `fr` / `zh-CN`），在宽裕位置展示 `BHS（博阿努瓦，加拿大 🇨🇦）`、`TOR（多伦多，加拿大 🇨🇦）` 等完整地理信息，在紧凑位置展示带悬停全名提示的 `🇨🇦 BHS`、`🇨🇦 TOR` 国旗徽标。
