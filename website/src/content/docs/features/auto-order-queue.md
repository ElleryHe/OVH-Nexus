---
title: Automated Procurement & Stock Monitor
description: Configure automated bare-metal server ordering tasks and configuration-level stock monitoring.
---

OVH Nexus provides two complementary engines for automated bare-metal procurement when high-demand OVHcloud servers (such as `KS-LE`, `KS-A`, or Flash Sale models) restock for only a few seconds:

1. **Purchase Queue (`/queue`)**: Polls for a target `planCode` across one or more preferred datacenters and optional addon codes, recording each task's origin (`source: "manual" | "stock-monitor" | "mcp"`).
2. **Multi-Target Configuration & Stock Monitor (`/monitor`, `/availability`, `/servers`)**: Each stock subscription manages one or more independent **Configuration Targets (`targets[]`)** via the **Quick Monitor (`QuickMonitorDialog`)** modal:
   - **Notify-Only vs. Auto-Order Binding**: A notify-only target can watch `Any Configuration` (`configKey: ""`), whereas enabling **`autoOrder: true` strictly requires selecting a specific hardware configuration (`configKey` + addon `options`)** (`400 CONFIG_REQUIRED_FOR_AUTO_ORDER`). Automated stock-monitor procurement never blindly falls back to the first available configuration.
   - **Per-Target Quantity & Ex-VAT Price Cap (`maxMonthlyPrice`)**: Each auto-order target defines its order `quantity` (default `1`) and an ex-VAT **Max Monthly Price (`maxMonthlyPrice`)** cap (pre-filled from the configuration's `estimatedMonthlyPrice` and editable by the user; one-time setup fees are excluded from the monthly cap).
   - **Safe Legacy Migration (`needsConfigSelection`)**: Legacy subscriptions created without an explicit configuration are automatically migrated to a notify-only `Any Configuration` target; if a legacy subscription had `autoOrder` enabled, auto-ordering is safely paused (`needsConfigSelection: true`) with an amber warning banner on `/monitor` until you select a specific configuration and price cap.

## How the Procurement Flow Works

When the background goroutine worker detects that your target server configuration and datacenter report available stock (`1H-low`, `1H-high`, `24H`, `72H`, or `available`—anything other than `unavailable` or `unknown`):

1. **Create Cart**: Calls `POST /order/cart` with your account's `ovhSubsidiary` (`zone`, e.g., `IE`, `FR`, `CA`, `US`) and assigns the cart to your authenticated account (`POST /order/cart/{cartId}/assign`).
2. **Add Server Plan**: Calls `POST /order/cart/{cartId}/eco` (or `/baremetal`) with `planCode`, `duration: P1M`, `pricingMode: default`, and the target's configured `quantity` (default `1`).
3. **Configure Datacenter & Hardware Addons**:
   - Sets the target `datacenter` configuration item on the cart item.
   - Attaches the exact `memory`, `storage`, `system-storage`, and bandwidth addon `options` bound to the target configuration.
4. **Verify Live Ex-VAT Monthly Quote (`maxMonthlyPrice`)**:
   - Inspects the live OVHcloud cart summary (`GET /order/cart/{cartId}/summary`) and compares the ex-VAT recurring monthly fee against the target's `maxMonthlyPrice` (if `> 0`).
   - If the live monthly price exceeds `maxMonthlyPrice`, checkout is aborted (`PRICE_ABOVE_LIMIT`), a `WARNING` log is recorded, and an `order.failed` alert is dispatched.
5. **Checkout / Generate Order**: Calls `POST /order/cart/{cartId}/checkout` with `auto_pay=false` and `waive_retractation_period=false` by default (preserving the EU 14-day statutory right of withdrawal and generating an unpaid order + payment link safely unless explicitly opted in with 2FA Sudo verification).
6. **Dispatch Notification**: Emits `order.success` (with the Order ID, configuration details, price, and direct OVH payment URL) or `order.failed` through the **Notification Center**.

## Creating a Task or Configuration Target in One Click

From either **Servers Catalog (`/servers`)** or **Live Availability Matrix (`/availability`)**:

1. Select the desired model and specific hardware configuration (`memory` + `storage`) from the inline row dropdown or expanded catalog card.
2. Click **Add to Queue** or **Quick Monitor**.
3. In `QuickMonitorDialog`, add one or more configuration targets, choose the target datacenters (or leave empty to monitor all datacenters), and—if enabling **Auto-Order**—confirm the specific configuration, `quantity`, and ex-VAT **Max Monthly Price (`maxMonthlyPrice`)**.
4. Once an order succeeds, the task automatically marks itself completed and records the order in **History & Orders (`/history`)**.
