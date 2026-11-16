---
title: MCP AI 网关与隐私安全沙盒
description: 通过内置 Model Context Protocol (MCP) 网关将 Claude Code、Claude Desktop、Cursor 或 Antigravity 接入 OVH Nexus，兼具双向资产混淆脱敏与“只允许下单、硬性禁止付款”财务防线。
---

**MCP AI 网关**（`/mcp`，由 `internal/mcp` 驱动）内置遵循 **Model Context Protocol (MCP) Streamable HTTP** 标准（`/api/mcp`，JSON-RPC 2.0）的服务端，工具命名与交互体验对齐官方 [OVHcloud CLI (`ovh/ovhcloud-cli`)](https://github.com/ovh/ovhcloud-cli)。

鉴于 OVH Nexus 管控的是真实云资产与账户资金，MCP 网关内置了**双向隐私混淆护盾（Privacy Shield）**与**四级权限及财务安全红线（L1–L4）**，让 AI 助手能够流畅协助清点资产、查询全球机房实时库存并生成待支付订单，同时绝不泄露真实主机名/完整 IP，也绝不会触发任何自动扣款。

---

## 核心安全架构

### 1. 独立高熵令牌 (`nxs_mcp_...`) 与默认关闭策略

- **默认安全关闭**：除非管理员在 **WebUI → MCP AI 网关 (`/mcp`)** 中主动启用并生成令牌，否则 `/api/mcp` 始终返回 `403 Forbidden`。
- **凭证彻底隔离**：MCP 网关使用独立签发的 Bearer Token（`Authorization: Bearer nxs_mcp_...`），在 `/data/nexus.db`（`mcp_config` 表）中仅持久化不可逆的 `SHA-256` 哈希（`token_hash`）与脱敏预览（`masked_token`）。即使将该令牌配置给外部 AI 客户端，对方也**无法**登录 WebUI 控制台，更无法读取任何原始 OVHcloud API 密钥（`appKey`、`appSecret`、`consumerKey`）。
- **防爆破速率限制**：`/api/mcp` 内置按来源 IP 的鉴权防爆破保护（`5 次失败 / 5 分钟`，超限返回 `429 Too Many Requests` 与 `Retry-After`）。

### 2. 双向隐私混淆护盾（均衡模式 `Balanced Shield`）

AI 模型在上下文中永远不会接触到原始 OVH 主机名、完整公网 IP、NIC Handle（`xx12345-ovh`）、账单邮箱或带密码的付款链接：

| 资产类型 | 真实 OVHcloud 标识（对 AI 隐藏） | AI 可见代号 | AI 可见安全摘要（均衡模式） |
| :--- | :--- | :---: | :--- |
| **OVH 账户** | `ovh-eu` / `xx12345-ovh` / `admin@example.com` | `acct-01` | 自定义备注名（`EU-Main`）+ `ovh-eu (IE)` |
| **独立服务器** | `ns3198452.ip-51-210-88.eu`（内嵌真实 IP！） | `bm-01` | 安全自定义备注 + `KS-A @ GRA (*.*.*.42)` |
| **VPS 实例** | `vps-98a7bc12.vps.ovh.net` | `vps-01` | 安全自定义备注 + `VPS-1 @ GRA (*.*.*.88)` |
| **公网 IP** | `51.210.88.42` / `2001:41d0:...:1` | `ip-01` | 仅保留末段尾号（`*.*.*.42` / `*::0001`） |
| **采购订单** | `238910421` + `orderUrl?orderPassword=...` | `ord-01` | 含税总价与 4 步装机进度（**彻底剥离付款/PDF 链接**） |

当 AI 在对话中使用代号调用工具时（例如 `baremetal_get(server="bm-01")` 或 `order_follow_up(order="ord-01")`），网关会在后端自动从 `/data/nexus.db`（`mcp_aliases`）将代号还原为真实 OVH 标识并校验多账户白名单权限。

### 3. 四级权限沙盒与永久安全红线（`L1` – `L4`）

| 级别 | 权限键 | 默认状态 | 包含工具与硬性边界 |
| :--- | :---: | :---: | :--- |
| **L1 只读查询** | `read` | **开启** | **11 个工具**：查询脱敏后的账户画像、独服列表与硬件详情、VPS 列表、边缘防火墙状态、服务到期倒计时、订单交付进度、全球机房实时库存与 SKU 上新变动。 |
| **L2 免付采购** | `procurement` | **关闭** | **5 个工具**：两步确认生成【未付款订单】（`order_quote` → `order_create_unpaid`）、采购队列任务管理（`queue_add`、`queue_manage`）与库存订阅管理。**底层代码硬性锁死 `auto_pay=false` 且单次数量固定为 `1`。** |
| **L3 可逆运维** | `operations` | **关闭** | **4 个工具**：独服/VPS 硬件重启（`baremetal_reboot`、`vps_reboot`）、重置卡死的 IPMI 会话（`baremetal_ipmi_reset`）、边缘防火墙规则与永久防洪开关（`baremetal_firewall_update`）。需启用 2FA 后方可解锁。 |
| **L4 高危与财务** | *代码级封禁* | **永久熔断** | **0 个工具（不提供任何开启开关）**：自动扣款结账（`autoPay`）、重装系统/格式化磁盘、退订销毁服务、读取带一次性令牌的 KVM 远程控制台链接、增删 SSH 公钥或 API 密钥均被永久排除。 |

---

## 两步确认生成【未付款订单】流程

开启 **L2 免付采购**（`procurement`）后，AI 下单受到严格的两步确认与速率配额保护：

1. **第一步：试算配置报价 (`order_quote`)**
   AI 调用 `order_quote(plan_code="24ska01", datacenter="gra", options=[...])`。系统向 OVH 询价并签发一个 **5 分钟有效的一次性 `quote_id`**，此时不会产生任何订单。
2. **第二步：人工确认并生成待支付订单 (`order_create_unpaid`)**
   您在聊天窗口确认价格后，AI 传入 `quote_id` 调用 `order_create_unpaid`：
   - 后端 Go 代码无条件锁死 `auto_pay = false` 与 `quantity = 1`；
   - 受每小时下单上限（`hourlyOrderLimit`，默认 `5 单/小时`）保护；
   - 订单付款链接**绝不返回给 AI**，而是通过您的**通知中心**（Telegram / 邮件）私密推送，也可在 **WebUI → 历史与订单** 页面查看并手动完成支付。

---

## 一键接入主流 AI 客户端

前往 **WebUI → MCP AI 网关 (`/mcp`)** 启用网关并复制 `nxs_mcp_...` 令牌后，可在任一支持 MCP 的客户端中配置（远程访问请将 `http://localhost:20000` 替换为您的 HTTPS 反代域名）：

### Claude Code (CLI)

```bash
claude mcp add --transport http ovh-nexus "http://localhost:20000/api/mcp" \
  --header "Authorization: Bearer nxs_mcp_YOUR_TOKEN"
```

### Claude Desktop / Cursor (`mcp.json`)

```json
{
  "mcpServers": {
    "ovh-nexus": {
      "url": "http://localhost:20000/api/mcp",
      "headers": {
        "Authorization": "Bearer nxs_mcp_YOUR_TOKEN"
      }
    }
  }
}
```

### Antigravity / Windsurf (`mcp_config.json`)

```json
{
  "mcpServers": {
    "ovh-nexus": {
      "serverUrl": "http://localhost:20000/api/mcp",
      "headers": {
        "Authorization": "Bearer nxs_mcp_YOUR_TOKEN"
      }
    }
  }
}
```
