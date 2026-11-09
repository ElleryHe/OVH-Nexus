---
title: 多账户管理与切换
description: 在单一控制台内集中管理跨欧洲区、加拿大/亚太区与美国区的多个 OVHcloud 账户。
---

OVH Nexus 原生支持注册并随时切换多个不同区域（`ovh-eu`、`ovh-ca`、`ovh-us`）或不同结算子站点（`IE`、`FR`、`CA`、`SG`、`US` 等）的 OVHcloud 账户，无需部署多个容器实例。

## 多账户隔离机制

- **持久化账户注册表 (`data/accounts.json`)**：保存每个账户的别名、Endpoint、所属结算区域（`zone`）与 API 密钥，磁盘文件权限强制设为 `0600`。
- **请求级上下文切换 (`X-OVH-Account` 请求头)**：当您在顶部状态栏或**API 账户管理**页面切换当前活动账户时，前端会自动在请求中附加 `X-OVH-Account: <account-id>`，确保独服列表、VPS 管理、订单同步与手动下单均针对所选账户执行。
- **按账户隔离的缓存空间**：服务器资产缓存与配置查询均以账户 ID 作为命名空间隔离，切换账户时绝不会出现跨账户资产混用或串号。

## 添加与管理多个账户

1. 点击左侧导航栏的 **API 账户管理**。
2. 点击**添加账户**，填写：
   - **账户名称 / 备注**（例如 `欧区主号 (IE)`、`亚太新加坡 (CA-SG)`、`美区主力 (US)`）。
   - **API 端点 (`Endpoint`)**：`ovh-eu`、`ovh-ca` 或 `ovh-us`。
   - **App Key**、**App Secret** 与 **Consumer Key**。
3. 点击**测试连接**，后端会自动调用 `/me` 校验权限并拉取您的 `nichandle`、邮箱与结算子区域代码（`ovhSubsidiary`）。
4. 您可以将任意账户设为**默认账户（Default）**，未指定特定账户 ID 的后台轮询任务将默认使用该账户。

## 结算子区域 (`zone`) 与价格币种

每个 OVHcloud 账户都归属于一个结算子区域（`ovhSubsidiary`，在配置中存为 `zone`，如 `IE`、`FR`、`DE`、`GB`、`CA`、`SG`、`AU`、`US`）。OVH Nexus 在以下场景中使用该字段：

- 自动下单创建购物车（`POST /order/cart`）时传入正确的 `ovhSubsidiary`。
- 从公开目录（`/order/catalog/public/eco` 与 `/baremetal`）拉取与您账户结算币种一致的精确月付价格（如 `€`、`$`、`£`、`S$`、`A$`）。
