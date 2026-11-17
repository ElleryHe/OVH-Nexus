---
title: MCP AI Gateway & Privacy Sandbox
description: Connect Claude Code, Claude Desktop, Cursor, or Antigravity to OVH Nexus via Model Context Protocol (MCP) with bidirectional privacy obfuscation and strict unpaid-order guardrails.
---

The **MCP AI Gateway** (`/mcp`, powered by `internal/mcp`) exposes a built-in **Model Context Protocol (MCP) Streamable HTTP** endpoint (`/api/mcp`, JSON-RPC 2.0) inspired by the [official OVHcloud CLI (`ovh/ovhcloud-cli`)](https://github.com/ovh/ovhcloud-cli) command taxonomy.

Because OVH Nexus manages real cloud infrastructure and billing accounts, the MCP Gateway enforces a **Bidirectional Privacy Shield** and **4-Tier Permission & Financial Guardrails** so AI assistants can inspect inventory, check global datacenter stock, and prepare unpaid orders without ever seeing sensitive identifiers or triggering automatic payments.

---

## Core Security Architecture

### 1. Isolated Bearer Token (`nxs_mcp_...`) & Default-Off Switch

- **Disabled by Default**: `/api/mcp` returns `403 Forbidden` until explicitly enabled in **WebUI → MCP AI Gateway (`/mcp`)**.
- **Isolated Credentials**: The MCP Gateway authenticates requests using a dedicated high-entropy Bearer Token (`Authorization: Bearer nxs_mcp_...`), stored strictly as an irreversible `SHA-256` `token_hash` with a redacted `masked_token` preview inside `/data/nexus.db` (`mcp_config`). Even if shared with an external AI client, this token **cannot** log into the WebUI console or read raw OVHcloud API credentials (`appKey`, `appSecret`, `consumerKey`).
- **Brute-Force Protection**: Failed authentication attempts against `/api/mcp` are rate-limited per client IP (`5 failed attempts / 5 minutes`, returning `429 Too Many Requests` with `Retry-After`).

### 2. Bidirectional Privacy Shield (`Balanced Mode`)

AI models never receive raw OVHcloud resource identifiers, full IPv4/IPv6 addresses, NIC handles (`xx12345-ovh`), billing emails, or unauthenticated payment URLs:

| Resource Type | Raw OVHcloud Value (Hidden from AI) | AI-Visible Alias | Safe Context Hint (`Balanced Mode`) |
| :--- | :--- | :---: | :--- |
| **OVH Account** | `ovh-eu` / `xx12345-ovh` / `admin@example.com` | `acct-01` | Custom alias (`EU-Main`) + `ovh-eu (IE)` |
| **Dedicated Server** | `ns3198452.ip-51-210-88.eu` (embeds IP!) | `bm-01` | Safe custom label + `KS-A @ GRA (*.*.*.42)` |
| **VPS Instance** | `vps-98a7bc12.vps.ovh.net` | `vps-01` | Safe custom label + `VPS-1 @ GRA (*.*.*.88)` |
| **Public IP** | `51.210.88.42` / `2001:41d0:...:1` | `ip-01` | Masked tail (`*.*.*.42` / `*::0001`) |
| **Order & Invoice** | `238910421` + `orderUrl?orderPassword=...` | `ord-01` | Price, 4-step delivery status (**payment URL stripped**) |

When the AI invokes a tool using an alias (for example `baremetal_get(server="bm-01")` or `order_follow_up(order="ord-01")`), the gateway transparently resolves the persistent alias from `/data/nexus.db` (`mcp_aliases`) back to the real OVHcloud identifier inside the backend.

### 3. Four-Tier Permission Sandbox & Hardcoded Redlines (`L1` – `L4`)

| Tier | Scope Key | Default | Capabilities & Redlines |
| :--- | :---: | :---: | :--- |
| **L1 Read-Only** | `read` | **ON** | **11 Tools**: Inspect obfuscated accounts, dedicated servers, VPS instances, IP firewall rules, service expiration countdowns, order delivery progress, live datacenter stock (`catalog_availability` with `offset` pagination, `nextOffset`, unified `listingState` (`listed` / `announced` / `presale`), `generation`, `presale_only` filtering, one-time `setupFee` / `originalSetupFee`, promotional pricing `hasPromo` / `promoBadge` / `originalMonthlyPrice`, and multi-period `pricingModes` such as `default`, `upfront12`, and `upfront24`), and SKU watcher timeline changes (`catalog_sku_changes` with `listingState`, `saleStarted`, and `replaced` / `replacedFrom` version refresh tracking). |
| **L2 Unpaid Procurement** | `procurement` | **OFF** | **5 Tools**: Two-step unpaid order creation (`order_quote` → `order_create_unpaid`), auto-order queue management (`queue_add`, `queue_manage`), and stock monitor subscriptions (`pricingMode` / `duration` commitment support and fail-fast `coupon` validation returning `400 INVALID_COUPON` on rejected vouchers). **`auto_pay=false` and `quantity=1` are hardcoded.** |
| **L3 Reversible Operations** | `operations` | **OFF** | **4 Tools**: Hardware reboot (`baremetal_reboot`, `vps_reboot`), stuck IPMI session reset (`baremetal_ipmi_reset`), and IP Edge Firewall / Anti-DDoS mitigation updates (`baremetal_firewall_update`). Requires 2FA enabled to unlock. |
| **L4 Destructive & Financial** | *Hard-Blocked* | **LOCKED** | **0 Tools (Permanently Blocked in Code)**: Automatic payment (`autoPayWithPreferredPaymentMethod`), OS reinstallation / disk wipe, service termination, raw IPMI KVM URLs, and SSH/API key mutations are never exposed. |

---

## Two-Step Unpaid Ordering Flow

When **L2 Unpaid Procurement** (`procurement`) is enabled:

1. **Step 1 — Price Quote (`order_quote`)**:
   The AI calls `order_quote(plan_code="24ska01", datacenter="gra", options=[...])`. OVH Nexus queries the real OVH cart price and issues a single-use **`quote_id` valid for 5 minutes** without placing an order.
2. **Step 2 — User Confirmation & Unpaid Order (`order_create_unpaid`)**:
   After you confirm the quoted price in chat, the AI calls `order_create_unpaid(quote_id="q_...")`.
   - `auto_pay` is **unconditionally hardcoded to `false`** in Go code.
   - `quantity` is **unconditionally hardcoded to `1`**.
   - Rolling hourly rate limit (`hourlyOrderLimit`, default `5 orders/hour`) prevents runaway loops.
   - The resulting payment URL is **stripped from the AI response** and sent only to your **Notification Center** (`Telegram` / `Email`) and **WebUI → History & Orders** tab.

---

## Connecting AI Clients

1. Open **WebUI → MCP AI Gateway (`/mcp`)**, toggle **Enable Gateway**, and copy your `nxs_mcp_...` Bearer Token.
2. In the **Client Configuration Generator**, choose between **Full Config File** (creates a standalone `mcp.json` / `mcp_config.json`) or **Server Entry Only** (copies only the `"ovh-nexus": { ... }` object with a reminder to add a trailing comma `,` after the preceding entry when pasting into an existing `mcpServers` block).
3. Add the endpoint to your preferred AI client (replace `http://localhost:20000` with your HTTPS reverse proxy domain if accessing remotely):

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
