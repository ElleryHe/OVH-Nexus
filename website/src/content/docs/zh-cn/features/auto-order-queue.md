---
title: 自动采购队列与配置监控
description: 配置无人值守的独立服务器自动采购队列与精确硬件子配置（FQN）库存监控任务。
---

针对 OVHcloud 热门机型（如 `KS-LE` 限量版、`KS-A` 或大促闪购机型）补货往往仅持续数秒的场景，OVH Nexus 提供了两套互补的自动化采购引擎：

1. **采购队列（Purchase Queue，`/queue`）**：按目标 `planCode`、优先机房列表、可选附加组件、付款周期模式（`pricingMode` & `duration`）及可选自定义优惠码（`coupon`）进行持续轮询下单，并清晰标记每条任务的创建来源（`source: "manual" | "stock-monitor" | "mcp"`）。
2. **多目标配置级库存监控（Stock Monitor，`/monitor`、`/availability`、`/servers`）**：每个库存订阅可通过**快速监控（`QuickMonitorDialog`）**弹窗添加并管理一个或多个独立的**配置监控目标（`targets[]`）**：
   - **仅通知 vs. 自动下单配置强校验**：仅通知目标可选择「任意配置」（`configKey: ""`）；而开启 **`autoOrder: true` 自动下单必须明确选定具体硬件配置（`configKey` + `options`）**（否则返回 `400 CONFIG_REQUIRED_FOR_AUTO_ORDER`）。系统已移除未指定配置时自动回退选择第一个有货配置的兜底逻辑，杜绝盲目买错高价配置。
   - **多周期合约付款模式 (`pricingMode` & `duration`)**：在配置下单弹窗、快速监控目标与采购队列中均支持切换月付无合约（`default` / `P1M`）、12 个月一次性年付（`upfront12` / `P12M`）、24 个月一次性两年付（`upfront24` / `P24M`）及合约月付（`degressivity12` / `degressivity24`）。切换付款方式时实时更新周期总价、折合月均不含税价及免安装费状态（如 `upfront12`/`upfront24` 自动变为免安装费 `€0.00`）。
   - **按目标设定数量 (`quantity`) 与折合月均不含税上限 (`maxMonthlyPrice`)**：每个自动下单目标均可设定采购数量（`quantity >= 1`，默认 `1`）及**不含税最高月价保护上限（`maxMonthlyPrice`）**（默认自动填入所选付款周期的折后折合月均不含税价 `monthlyEquivalentPrice`，支持手动修改；一次性安装费单独展示、不计入月价上限）。
   - **自定义优惠码 (`coupon`) 与失效立停保护 (`400 INVALID_COUPON`)**：支持在直接下单、快速监控目标或采购队列中填入可选优惠码（`coupon`）。下单时若 OVHcloud 拒绝该优惠码，系统立即中止结算并返回 `400 INVALID_COUPON`，绝不按原价继续下单。
   - **旧版自动下单订阅安全迁移 (`needsConfigSelection`)**：未绑定具体配置的旧版订阅会自动迁移为「任意配置 + 原机房」的通知目标；若原订阅曾开启自动下单，升级后会自动暂停其自动下单并标记 `needsConfigSelection: true`，在 `/monitor` 页面展示醒目琥珀色提示条，待您选择具体配置与最高月价后重新启用。

## 自动下单执行流程

当后台 Goroutine 监控协程检测到目标配置与机房出现可用库存（`1H-low`、`1H-high`、`24H`、`72H` 或 `available`）时，会自动按顺序执行以下 OVHcloud API 调用：

1. **创建并绑定购物车**：使用当前账户的结算区域（`zone`，如 `IE`、`FR`、`CA`、`US`）调用 `POST /order/cart`，并通过 `POST /order/cart/{cartId}/assign` 将购物车绑定至您的账户。
2. **添加基础机型、合约周期与数量**：调用 `POST /order/cart/{cartId}/eco`（或 `/baremetal`）将目标 `planCode`、付款模式 `pricingMode`（`default`、`upfront12`、`upfront24` 等）、租期 `duration`（`P1M`、`P12M`/`P1Y`、`P24M`/`P2Y`）及采购数量 `quantity` 加入购物车。
3. **写入机房与硬件附加项（Addons）**：
   - 设置目标数据中心配置项（`datacenter`）。
   - 优先以与主商品一致的 `pricingMode` 与 `duration` 挂载该配置目标绑定的精确内存（`memory`）、存储（`storage`）、系统盘（`system-storage`）与带宽附加包代码（`options`）。
4. **应用并强校验自定义优惠码 (`coupon`)**：
   - 若配置了非空 `coupon`，调用 `POST /order/cart/{cartId}/coupons`（`{"coupon": coupon}`）。
   - 若优惠码被 OVHcloud 接口拒绝，系统立即删除临时购物车、记录 `WARNING` 日志、推送 `order.failed` 告警并返回 `400 INVALID_COUPON`，绝不回退按原价下单。
5. **购物车实时折合月均不含税价上限校验 (`maxMonthlyPrice`)**：
   - 读取购物车实时报价汇总（`GET /order/cart/{cartId}/summary`），提取不含税周期费并除以对应承诺月数（`upfront12` 除以 `12`，`upfront24` 除以 `24`，月付除以 `1`）得到实时折合月均价 `liveMonthlyExVAT`，与目标设定的 `maxMonthlyPrice` 比对。
   - 若 `liveMonthlyExVAT` 超过 `maxMonthlyPrice`，系统立即中止结算并返回 `PRICE_ABOVE_LIMIT`，记录一条 `WARNING` 日志并通过通知中心推送 `order.failed` 告警。
6. **提交结算生成订单**：调用 `POST /order/cart/{cartId}/checkout` 生成正式订单（默认 `auto_pay=false` 且 `waive_retractation_period=false`，保留欧盟 14 天法定撤回权并生成待支付订单及付款链接，避免未经确认自动扣刷信用卡）。
7. **推送下单结果通知**：通过**通知中心**立即分发 `order.success`（附带订单号、配置明细、含税总价与官方一键付款链接）或 `order.failed` 告警。

## 一键创建采购任务与配置监控目标

在 **服务器目录（`/servers`）** 或 **实时库存矩阵（`/availability`）** 中：

1. 在矩阵行内配置下拉框或**服务器配置与下单独立弹窗**中选择目标硬件配置（`内存 + 硬盘`）。
2. 点击**加入采购队列**或**快速监控**（自动带入已选配置、付款周期与机房）。
3. 选择付款周期（`default` 月付、`upfront12` 年付 12 个月或 `upfront24` 两年付 24 个月），按需填写可选**优惠码（`coupon`）**；若开启**自动下单**，确认具体配置、采购数量 `quantity` 与不含税**最高月价（`maxMonthlyPrice`）**。
4. 下单成功后，任务状态会自动标记为已完成，并将订单详情写入 **历史与订单（`/history`）**。
