---
title: 自动采购队列与配置监控
description: 配置无人值守的独立服务器自动采购队列与精确硬件子配置（FQN）库存监控任务。
---

针对 OVHcloud 热门机型（如 `KS-LE` 限量版、`KS-A` 或大促闪购机型）补货往往仅持续数秒的场景，OVH Nexus 提供了两套互补的自动化采购引擎：

1. **采购队列（Purchase Queue，`/queue`）**：按目标 `planCode`、优先机房列表与可选附加组件进行持续轮询下单，并清晰标记每条任务的创建来源（`source: "manual" | "stock-monitor" | "mcp"`）。
2. **多目标配置级库存监控（Stock Monitor，`/monitor`、`/availability`、`/servers`）**：每个库存订阅可通过**快速监控（`QuickMonitorDialog`）**弹窗添加并管理一个或多个独立的**配置监控目标（`targets[]`）**：
   - **仅通知 vs. 自动下单配置强校验**：仅通知目标可选择「任意配置」（`configKey: ""`）；而开启 **`autoOrder: true` 自动下单必须明确选定具体硬件配置（`configKey` + `options`）**（否则返回 `400 CONFIG_REQUIRED_FOR_AUTO_ORDER`）。系统已移除未指定配置时自动回退选择第一个有货配置的兜底逻辑，杜绝盲目买错高价配置。
   - **按目标设定数量 (`quantity`) 与不含税最高月价上限 (`maxMonthlyPrice`)**：每个自动下单目标均可设定采购数量（`quantity >= 1`，默认 `1`）及**不含税最高月价保护上限（`maxMonthlyPrice`）**（默认自动填入所选配置的预估月价，支持手动修改；一次性安装费单独展示、不计入月价上限）。
   - **旧版自动下单订阅安全迁移 (`needsConfigSelection`)**：未绑定具体配置的旧版订阅会自动迁移为「任意配置 + 原机房」的通知目标；若原订阅曾开启自动下单，升级后会自动暂停其自动下单并标记 `needsConfigSelection: true`，在 `/monitor` 页面展示醒目琥珀色提示条，待您选择具体配置与最高月价后重新启用。

## 自动下单执行流程

当后台 Goroutine 监控协程检测到目标配置与机房出现可用库存（`1H-low`、`1H-high`、`24H`、`72H` 或 `available`）时，会自动按顺序执行以下 OVHcloud API 调用：

1. **创建并绑定购物车**：使用当前账户的结算区域（`zone`，如 `IE`、`FR`、`CA`、`US`）调用 `POST /order/cart`，并通过 `POST /order/cart/{cartId}/assign` 将购物车绑定至您的账户。
2. **添加基础机型与数量**：调用 `POST /order/cart/{cartId}/eco`（或 `/baremetal`）将目标 `planCode`（租期 `P1M`、数量 `quantity`）加入购物车。
3. **写入机房与硬件附加项（Addons）**：
   - 设置目标数据中心配置项（`datacenter`）。
   - 挂载该配置目标绑定的精确内存（`memory`）、存储（`storage`）、系统盘（`system-storage`）与带宽附加包代码（`options`）。
4. **购物车实时不含税月价上限校验 (`maxMonthlyPrice`)**：
   - 读取购物车实时报价汇总（`GET /order/cart/{cartId}/summary`），将实时**不含税月维护费**与目标设定的 `maxMonthlyPrice` 进行比对。
   - 若实时月价超过 `maxMonthlyPrice`，系统立即中止结算并返回 `PRICE_ABOVE_LIMIT`，记录一条 `WARNING` 日志并通过通知中心推送 `order.failed` 告警。
5. **提交结算生成订单**：调用 `POST /order/cart/{cartId}/checkout` 生成正式订单（默认 `auto_pay=false` 且 `waive_retractation_period=false`，保留欧盟 14 天法定撤回权并生成待支付订单及付款链接，避免未经确认自动扣刷信用卡）。
6. **推送下单结果通知**：通过**通知中心**立即分发 `order.success`（附带订单号、配置明细、含税总价与官方一键付款链接）或 `order.failed` 告警。

## 一键创建采购任务与配置监控目标

在 **服务器目录（`/servers`）** 或 **实时库存矩阵（`/availability`）** 中：

1. 在矩阵行内配置下拉框或展开的服务器卡片中选择目标硬件配置（`内存 + 硬盘`）。
2. 点击**加入采购队列**或**快速监控**（自动带入已选配置与机房）。
3. 在 `QuickMonitorDialog` 弹窗中添加一个或多个监控目标，勾选目标机房（留空则监控全部机房）；若开启**自动下单**，确认具体配置、采购数量 `quantity` 与不含税**最高月价（`maxMonthlyPrice`）**。
4. 下单成功后，任务状态会自动标记为已完成，并将订单详情写入 **历史与订单（`/history`）**。
