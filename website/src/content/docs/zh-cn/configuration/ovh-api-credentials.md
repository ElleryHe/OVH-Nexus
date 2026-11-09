---
title: OVHcloud API 凭据申请
description: 如何在欧洲区、加拿大/亚太区与美国区申请 OVHcloud App Key、App Secret 与 Consumer Key。
---

虽然 OVH Nexus 的公共监控能力（**全球实时库存矩阵**与**公开货架 SKU 变动监控**）无需任何 OVHcloud 凭据即可运行，但如果您需要管控已购独服/VPS、追踪订单交付进度或执行自动抢购下单，则需要申请一组 OVHcloud API 密钥（`App Key`、`App Secret` 与 `Consumer Key`）。

## 1. 打开对应区域的 Token 创建页面

请根据您的 OVHcloud 账户注册区域，打开对应的官方 Token 生成页面：

| 账户区域 | Endpoint 标识 | 官方 Token 申请地址 |
| :--- | :--- | :--- |
| **欧洲区 (Europe)** | `ovh-eu` | `https://eu.api.ovh.com/createToken/` |
| **加拿大 / 亚太区 (CA / APAC)** | `ovh-ca` | `https://ca.api.ovh.com/createToken/` |
| **美国区 (United States)** | `ovh-us` | `https://api.us.ovhcloud.com/createToken/` |

## 2. 填写 Token 申请表单

1. **Account ID or email address** 与 **Password**：输入您的 OVHcloud 账号（NIC-handle 或邮箱）与密码（如开启了二次验证需完成验证）。
2. **Script Name**：填写 `OVH Nexus`
3. **Script Description**：填写 `Self-hosted OVH Nexus control and procurement console`
4. **Validity（有效期）**：建议选择 `Unlimited`（无期限，适合长期无人值守监控与续费提醒）或 `30 days`。
5. **Rights（权限规则）**：点击 `+` 号添加下方所需的 HTTP 方法与路径。

## 3. 推荐的 API 权限规则

### 方案 A：完整控制台功能权限（最简便）

如果您同时使用自动抢购和全部云资产运维功能（重装系统、SSH 公钥管理、IP 防火墙、VPS 重启等），添加以下 4 条规则即可：

| HTTP 方法 | 路径匹配 (Path) |
| :--- | :--- |
| `GET` | `/*` |
| `POST` | `/*` |
| `PUT` | `/*` |
| `DELETE` | `/*` |

### 方案 B：按功能最小权限收敛

如果您希望严格遵循最小权限原则，仅授予 OVH Nexus 实际调用的 API 前缀：

| 功能模块 | HTTP 方法 | 路径模式 |
| :--- | :--- | :--- |
| **账户信息、订单查询与 SSH 公钥** | `GET`, `POST`, `DELETE` | `/me`, `/me/*` |
| **购物车与自动下单采购** | `GET`, `POST`, `PUT`, `DELETE` | `/order/*` |
| **独立服务器管控与 IPMI** | `GET`, `POST`, `PUT` | `/dedicated/server`, `/dedicated/server/*` |
| **系统安装模板查询** | `GET` | `/dedicated/installationTemplate`, `/dedicated/installationTemplate/*` |
| **已购 VPS 管控与重启** | `GET`, `POST` | `/vps`, `/vps/*` |
| **IP 边缘防火墙与 Anti-DDoS** | `GET`, `POST`, `PUT`, `DELETE` | `/ip`, `/ip/*` |

## 4. 在 OVH Nexus 中绑定凭据

1. 登录 OVH Nexus 控制台。
2. 进入左侧导航栏的 **API 账户管理**（或 **系统设置**）。
3. 填入生成的 **App Key**、**App Secret**、**Consumer Key**，选择对应的 **Endpoint**（`ovh-eu`、`ovh-ca` 或 `ovh-us`），确认 **子区域 (`zone`)**（如 `IE`、`FR`、`DE`、`CA`、`SG`、`US`）。
4. 点击**测试连接 / 保存**，系统会调用 `/me` 验证凭据有效性，并自动显示您的 `nichandle` 与所属结算区域。
