---
title: Multi-Account Management
description: Manage multiple OVHcloud accounts across Europe, Canada/APAC, and US regions in one console.
---

OVH Nexus supports registering and switching between multiple OVHcloud accounts (`ovh-eu`, `ovh-ca`, `ovh-us`) without restarting the container or running separate instances.

## How Multi-Account Isolation Works

- **Persistent Account Registry (`data/accounts.json`)**: Stores each account's display label, endpoint (`ovh-eu`, `ovh-ca`, `ovh-us`), subsidiary zone (`IE`, `FR`, `CA`, `US`, etc.), and API credentials with `0600` file permissions on disk.
- **Request-Scoped Account Selection (`X-OVH-Account` Header)**: When you switch the active account in the WebUI top bar or **API Accounts** page, the frontend attaches `X-OVH-Account: <account-id>` to API requests so fleet queries, OS installations, and manual orders run against the selected account.
- **Per-Account Server Cache Isolation**: Dedicated server lists, hardware specifications, and catalog lookups are cached per account ID so switching between accounts never leaks or mixes server lists across accounts.

## Adding & Switching Accounts

1. Open **API Accounts** in the sidebar.
2. Click **Add Account** and provide:
   - **Account Name / Alias** (e.g., `EU Production (IE)`, `CA APAC (SG)`, `US Hillsboro`).
   - **Endpoint**: `ovh-eu`, `ovh-ca`, or `ovh-us`.
   - **App Key**, **App Secret**, and **Consumer Key**.
3. Use **Test Connection** to validate the credentials against `/me` and automatically populate your OVH `nichandle`, email, and subsidiary code (`ovhSubsidiary`).
4. Set any account as the **Default Account**—background tasks that do not specify an explicit account ID will use the default account.

## Subsidiary (`zone`) and Pricing Currency

Each OVHcloud account belongs to an `ovhSubsidiary` (stored as `zone`, such as `FR`, `IE`, `DE`, `GB`, `CA`, `SG`, `AU`, `US`). OVH Nexus uses this subsidiary code when:

- Creating shopping carts (`/order/cart`) for automated or manual server purchases.
- Fetching exact configuration-level monthly pricing from `/order/catalog/public/eco` and `/order/catalog/public/baremetal` in your account's native currency (`€`, `$`, `£`, `A$`, `S$`).
