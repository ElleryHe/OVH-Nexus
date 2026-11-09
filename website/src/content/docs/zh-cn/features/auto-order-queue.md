---
title: 自动抢购队列与配置狙击
description: 配置无人值守的独立服务器抢购队列与精确硬件子配置（FQN）秒级狙击任务。
---

针对 OVHcloud 热门机型（如 `KS-LE` 限量版、`KS-A` 或大促闪购机型）补货往往仅持续数秒的场景，OVH Nexus 提供了两套互补的自动化采购引擎：

1. **采购队列（Purchase Queue，`/queue`）**：按目标 `planCode`、优先机房列表与可选附加组件进行持续轮询抢购。
2. **精确配置狙击（Configuration Sniper，`/monitor` & `/ovh-availability`）**：直接从实时库存矩阵锁定某一个特定的 `planCode + 内存代码 + 存储代码 + 机房` 组合（`FQN`）进行定点监控与自动下单。

## 自动下单执行流程

当后台守护线程检测到目标机型与机房出现可用库存（`1H-low`、`1H-high`、`24H`、`72H` 或 `available`）时，会自动按顺序执行以下 OVHcloud API 调用：

1. **创建并绑定购物车**：使用当前账户的结算区域（`zone`，如 `IE`、`FR`、`CA`、`US`）调用 `POST /order/cart`，并通过 `POST /order/cart/{cartId}/assign` 将购物车绑定至您的账户。
2. **添加基础机型**：调用 `POST /order/cart/{cartId}/eco`（或 `/baremetal`）将目标 `planCode`（租期 `P1M`、数量 `1`）加入购物车。
3. **写入机房与硬件附加项（Addons）**：
   - 设置目标数据中心配置项（`datacenter`）。
   - 自动挂载该子配置所需的内存（`memory`）、存储（`storage`）、系统盘（`system-storage`）与带宽附加包代码。
4. **提交结算生成订单**：调用 `POST /order/cart/{cartId}/checkout` 生成正式订单（默认生成待支付订单以锁死库存配额，避免未经确认自动扣刷信用卡）。
5. **推送抢购结果通知**：通过**通知中心**立即分发 `order.success`（附带订单号、配置明细、含税总价与官方一键付款链接）或 `order.failed` 告警。

## 一键创建抢购任务

在 **服务器目录（`/servers`）** 或 **实时库存矩阵（`/ovh-availability`）** 中：

1. 找到心仪的服务器型号与具体内存/硬盘组合。
2. 点击**加入抢购队列**或**监控此配置**。
3. 选择目标机房（可多选或设为 `any` 不限机房）及轮询间隔（如 `15–30` 秒）。
4. 下单成功后，任务状态会自动标记为已完成，并将订单详情写入 **历史与订单（`/history`）**。
