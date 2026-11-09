---
title: 常见问题解答 (FAQ)
description: 关于 OVH Nexus、API 密钥要求、自动抢购扣款、预上架 SKU 价格与多区域支持的常见问题。
---

### 使用 OVH Nexus 必须绑定 OVHcloud API 密钥吗？

**公共监控功能完全不需要。** 部署容器并登录后，无需配置任何 OVHcloud API Key，即可直接使用**全球实时库存矩阵**（含友好商业型号、可读化硬件规格与精确到子配置的月付价格）、**公开货架 SKU 变动监控**，以及配置 **Telegram / SMTP 邮件**接收补货（`stock.available`）和货架变动（`sku.changed`）通知。

仅当您需要操作名下资产（**独立服务器管控**、**已购 VPS 管控**、**订单交付追踪**、**到期续费矩阵**）或执行**自动下单抢购**时，才需要绑定 OVHcloud API 密钥。

---

### 自动抢购成功时会自动从我的信用卡或 PayPal 扣款吗？

默认情况下，OVH Nexus 调用 `/order/cart/{cartId}/checkout` 时使用安全结算模式（生成正式订单以锁死库存配额，而不会未经确认直接静默扣刷您的信用卡）。下单成功后，系统会立即通过**通知中心**向您推送 `order.success` 通知，内含 **OVHcloud 官方一键付款链接**，您只需点开链接确认无误后完成支付即可。

---

### 为什么 SKU 变动监控里抓到的新上机架型号有时暂时不显示价格？

当 OVHcloud 机房工程师首次将新机型（例如 `27skle01-v1`）注册进 `/dedicated/server/datacenter/availabilities` 接口时，往往比官网公开价格目录（`/order/catalog/public/eco` 或 `/baremetal`）早几个小时甚至数天。

在这个“预上架未定价格”窗口期内，OVH Nexus 会通过内置的确定性推导引擎（`infer_plan_from_code`）自动解析出商业家族名称（如 `KS-LE-1`）及内存/硬盘可读规格；一旦 OVHcloud 官方将该型号写入公开价格目录，面板会在下一次目录刷新时自动补齐月付价格。

---

### 可以同时监控和管理欧区 (`ovh-eu`)、加区 (`ovh-ca`) 和美区 (`ovh-us`) 账户吗？

完全支持。您可以在 **API 账户管理**中添加任意数量的 `ovh-eu`、`ovh-ca` 与 `ovh-us` 账户，并随时在顶部导航栏一键切换当前活动账户上下文。

---

### 如何修改控制台的默认语言？

在 `.env` 文件中将 `DEFAULT_LOCALE` 设为 `zh-CN`（简体中文）、`en`（English）或 `fr`（Français），或者随时点击登录页右上角及左侧边栏底部的 `EN | FR | 中文` 切换按钮（浏览器会自动记住您的选择）。
