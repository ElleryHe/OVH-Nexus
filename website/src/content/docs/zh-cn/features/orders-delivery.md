---
title: 订单追踪与实时交付流水线
description: 同步 OVHcloud 官方订单、获取付款链接与 PDF 账单，并实时追踪 4 步硬件交付进度。
---

当抢购队列自动下单成功或您在 OVHcloud 官网手动下单后，**历史与订单（History & Orders，`/history`）** 页面可通过 `/me/order` 接口实时同步并追踪订单从付款到机器交付上架的全过程。

## 同步 OVHcloud 官方订单

切换到 **OVH 官方订单（Official OVH Orders）** 标签页并点击**同步订单**，系统将拉取当前活动账户下的最新订单列表：

- **订单基础信息**：订单号（Order ID）、创建时间、含税总价（`priceWithTax.text`）及结算币种。
- **官方付款链接与 PDF 账单**：直接提供 OVHcloud 官方付款页面或账单链接（`url`）。对于自动抢购生成的待支付订单，点击即可跳转至官网完成支付。

## 4 步硬件交付流水线 (`/followUp`)

针对每一笔订单，OVH Nexus 会调用 `/me/order/{orderId}/followUp` 与 `/me/order/{orderId}/status`，将 OVHcloud 内部装机进度可视化为标准的 4 阶段流水线：

| 阶段 | OVH 阶段代码 | 含义说明 |
| :--- | :--- | :--- |
| **1. 订单创建** | `CREATING` | 订单已生成，等待完成支付或初步校验。 |
| **2. 订单审核** | `VALIDATING` | OVHcloud 正在确认收款并执行风控审核。 |
| **3. 硬件交付** | `DELIVERING` | 机房自动化系统或工程师正在分配机架、核对内存/磁盘硬件。 |
| **4. 装机就绪** | `INSTALLATION` | 固件自检与初始环境准备完成，服务器正式交付至您的账户名下。 |

每个阶段均会实时展示状态徽标（`DONE` 已完成、`DOING` 进行中、`TODO` 待执行、`ERROR` 异常）以及每一步的具体时间戳详情。

## 订单交付完成通知 (`order.delivered`)

在**通知中心**中勾选订阅 `order.delivered` 事件后，一旦您的新机器完成 `INSTALLATION` 阶段正式交付，OVH Nexus 将第一时间通过 Telegram 机器人或 SMTP 邮件通知您上机。
