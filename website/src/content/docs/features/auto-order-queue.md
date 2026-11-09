---
title: Automated Procurement & Queue Sniper
description: Configure automated bare-metal server ordering tasks and configuration-level stock snipers.
---

OVH Nexus provides two complementary engines for automated bare-metal procurement when high-demand OVHcloud servers (such as `KS-LE`, `KS-A`, or Flash Sale models) restock for only a few seconds:

1. **Purchase Queue (`/queue`)**: Polls for a target `planCode` across one or more preferred datacenters and optional addon codes.
2. **Configuration Sniper (`/monitor` & `/ovh-availability`)**: Targets a specific hardware configuration (`planCode + memory + storage + datacenter`) directly from the Live Availability Matrix.

## How the Procurement Flow Works

When the background worker detects that your target server and datacenter report available stock (`1H-low`, `1H-high`, `24H`, `72H`, or `available`—anything other than `unavailable` or `unknown`):

1. **Create Cart**: Calls `POST /order/cart` with your account's `ovhSubsidiary` (`zone`, e.g., `IE`, `FR`, `CA`, `US`) and assigns the cart to your authenticated account (`POST /order/cart/{cartId}/assign`).
2. **Add Server Plan**: Calls `POST /order/cart/{cartId}/eco` (or `/baremetal`) with `planCode`, `duration: P1M`, `pricingMode: default`, and `quantity: 1`.
3. **Configure Datacenter & Hardware Addons**:
   - Sets the target `datacenter` configuration item on the cart item.
   - Attaches the exact `memory`, `storage`, `system-storage`, and bandwidth addon planCodes required by the FQN.
4. **Checkout / Generate Order**: Calls `POST /order/cart/{cartId}/checkout` (`autoPayWithPreferredPaymentMethod: false` by default so an unpaid order and payment link are generated safely without unexpected credit card charges, or according to your checkout settings).
5. **Dispatch Notification**: Emits `order.success` (with the Order ID, configuration details, price, and direct OVH payment URL) or `order.failed` through the **Notification Center**.

## Creating a Task in One Click

From either **Servers Catalog (`/servers`)** or **Live Availability Matrix (`/ovh-availability`)**:

1. Locate the desired model and hardware configuration (`memory` + `storage`).
2. Click **Add to Queue** or **Monitor Config**.
3. Select the target datacenters (or leave as `any` to accept the first datacenter that comes in stock) and check Interval (`retryInterval`, e.g., `15–30` seconds).
4. Once an order succeeds, the task automatically marks itself completed and records the order in **History & Orders (`/history`)**.
