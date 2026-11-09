---
title: VPS 实例管控与库存监控
description: 一屏管理已购 OVHcloud VPS 实例并监控全球 VPS 机房补货动态。
---

OVH Nexus 完整覆盖了 OVHcloud 虚拟私有服务器（VPS）的两大核心场景：**已购 VPS 实例日常管控**与**全球 VPS 机房库存监控**。

## 已购 VPS 实例管控 (`/vps`)

进入 **VPS 监控（`/vps-monitor`）** 页面并切换至 **我的已购 VPS（My Purchased VPS）** 标签页（需配置 OVHcloud API 密钥），即可一屏查看当前账户名下的所有 VPS 实例：

- **实例概览**：服务内部名称（`vps-xxxx.vps.ovh.net`）、自定义显示备注、商业套餐型号（`VPS-1`、`VPS-2`、`Elite`、`Comfort`、`Essential`、`Starter`）及所在数据中心区域（`GRA`、`SBG`、`BHS`、`WAW`、`FRA`、`LON`、`SGP`、`SYD`）。
- **硬件配额**：vCore 核心数、内存容量（GB）与 NVMe/SSD 磁盘空间。
- **网络与生命周期**：主 IPv4 与 IPv6 地址（`/vps/{serviceName}/ips`）、当前运行状态（`running` 运行中、`stopped` 已停止、`rebooting` 重启中、`installing` 装机中、`maintenance` 维护中）、创建时间、到期时间与续费模式。
- **一键重启实例**：支持通过 `POST /vps/{serviceName}/reboot` 对指定 VPS 执行一键宿主机级重启。

## 全球 VPS 库存监控

在 **VPS 库存监控（VPS Stock Monitor）** 标签页中，您可以免 API Key 查询 OVHcloud 公开 VPS 目录（`/order/catalog/public/vps`）在各区域数据中心的实时库存状态：

- 直观查看各档位 VPS 套餐在全球各机房的当前可购状态（有货 vs 售罄）。
- 创建 VPS 库存订阅任务（保存在 `data/vps_subscriptions.json`），后台线程将定期轮询目标型号与机房，并在补货瞬间通过**通知中心**向您的 Telegram 或邮箱推送补货提醒。
