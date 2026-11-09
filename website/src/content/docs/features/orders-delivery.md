---
title: Orders & Live Delivery Pipeline
description: Track OVHcloud orders, retrieve payment links, and monitor the 4-stage hardware delivery pipeline.
---

After an order is placed (either automatically by OVH Nexus's queue sniper or manually in the OVHcloud Manager), the **History & Orders** page (`/history`) lets you track the real-time status of your OVHcloud orders directly via `/me/order`.

## Syncing Official OVHcloud Orders

Click **Sync OVH Orders** in the **Official OVH Orders** tab to fetch your most recent orders from the active OVHcloud account:

- **Order Metadata**: Order ID, creation timestamp, total price with tax (`priceWithTax.text`), and currency.
- **Payment & Invoice Links**: Direct links to the official OVHcloud payment portal / PDF invoice (`url`) so you can complete payment immediately when an unpaid order (`unpaid` / `notPaid`) is generated.

## 4-Stage Hardware Delivery Pipeline (`/followUp`)

For each order, OVH Nexus queries `/me/order/{orderId}/followUp` and `/me/order/{orderId}/status` to render OVHcloud's 4-stage physical delivery pipeline:

| Stage | OVH Step Code | Description |
| :--- | :--- | :--- |
| **1. Creating** | `CREATING` | Order is generated and awaiting payment or initial validation. |
| **2. Validating** | `VALIDATING` | Payment and anti-fraud checks are being verified by OVHcloud. |
| **3. Delivering** | `DELIVERING` | Datacenter technicians / automation are allocating and cabling the chassis. |
| **4. Installation** | `INSTALLATION` | Initial OS/firmware check is completing and the server is handed over. |

Each step displays its live status indicator (`DONE`, `DOING`, `TODO`, or `ERROR`) along with the detailed history timestamps returned by OVHcloud.

## Order Delivery Notifications (`order.delivered`)

When subscribed to the `order.delivered` event in the **Notification Center**, OVH Nexus notifies your Telegram or SMTP Email channels when an order's delivery pipeline reaches completion.
