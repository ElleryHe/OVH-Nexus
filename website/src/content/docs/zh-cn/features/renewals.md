---
title: 服务到期与自动续费矩阵
description: 汇总名下所有独立服务器与 VPS 的到期倒计时预警及自动/手动续费策略。
---

当您在多个 OVHcloud 账户下持有数十台独立服务器与 VPS 时，一旦遗漏手动续费账单，机器被机房回收将导致不可逆的数据丢失。

位于**总览大盘（Dashboard）** 的 **服务到期与续费总览（Services Expiration & Renewal Overview）** 模块（后端接口 `/api/ovh/services/renewals`）会自动聚合所有 `/dedicated/server/{name}/serviceInfos` 与 `/vps/{name}/serviceInfos` 数据，按剩余天数由近及远排序展示。

## 三级到期预警颜色

系统根据距离到期日的剩余天数（`daysLeft`）自动划分为三个健康等级：

| 等级 | 触发条件 | 视觉徽标 | 处理建议 |
| :--- | :--- | :--- | :--- |
| **紧急告警 (Critical)** | `daysLeft <= 7` | 红色高亮 | 若为手动续费或到期销毁且仍需保留，请立即前往续费。 |
| **临期提醒 (Warning)** | `8 <= daysLeft <= 30` | 黄色提醒 | 建议核对绑定支付方式是否有效及下期账单计划。 |
| **状态正常 (Healthy)** | `daysLeft > 30` | 绿色正常 | 租期充裕，无需立即处理。 |

## 续费策略分类

OVH Nexus 会解析每台服务的 `serviceInfos.renew` 结构，并标注为以下三种策略之一：

- **自动续费 (`automatic: true`)**：到期时 OVHcloud 将按续费周期（`period` 月数）自动从您的默认支付方式扣款。
- **手动续费 (`manualPayment: true`)**：需在到期日（`expiration`）前手动支付续费账单。
- **到期销毁 (`deleteAtExpiration: true`)**：已设置到期不续费，当前计费周期结束时机器将自动退订回收。

## 到期预警推送 (`service.expiring`)

在**通知中心（`/notifications`）** 中启用 `service.expiring` 事件，并设置**到期预警天数阈值（`expiryDays`，默认 `7` 天，支持 `1–90` 天）**。当任何独服或 VPS 剩余天数进入该阈值窗口时，系统将自动发送提醒。
