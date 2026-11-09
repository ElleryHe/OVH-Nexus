---
title: VPS Fleet & Stock Monitor
description: Manage purchased OVHcloud VPS instances and monitor new VPS stock availability.
---

OVH Nexus covers the complete lifecycle of OVHcloud Virtual Private Servers (VPS)—both **monitoring public VPS stock** and **managing your purchased VPS fleet**.

## Purchased VPS Fleet Management

In the **VPS Monitor** page (`/vps-monitor`), switch to the **My Purchased VPS** tab (requires a configured OVHcloud API key) to inspect all VPS instances returned by `/vps`:

- **Instance Overview**: Service name (`vps-xxxx.vps.ovh.net`), custom display name, commercial offer (`VPS-1`, `VPS-2`, `Elite`, `Comfort`, `Essential`, `Starter`), and datacenter zone (`GRA`, `SBG`, `BHS`, `WAW`, `FRA`, `LON`, `SGP`, `SYD`).
- **Resource Allocation**: vCore count, RAM capacity (GB), and NVMe/SSD disk allocation.
- **Network & Lifecycle Info**: Primary IPv4 and IPv6 addresses (`/vps/{serviceName}/ips`), current operational state (`running`, `stopped`, `rebooting`, `installing`, `maintenance`), creation date, expiration date, and auto-renewal status.
- **One-Click Reboot**: Trigger an immediate hypervisor-level reboot via `POST /vps/{serviceName}/reboot` with confirmation.

## Public VPS Datacenter Stock Monitor

In the **VPS Stock Monitor** tab, you can track availability across OVHcloud's public VPS catalog (`/order/catalog/public/vps` and datacenter availability endpoints):

- Inspect which datacenters currently have stock (`available` vs `out-of-stock`) for each VPS plan code.
- Create recurring stock subscriptions (`data/vps_subscriptions.json`) so OVH Nexus polls your selected VPS plan codes in the background and dispatches notifications via the **Notification Center** as soon as target zones restock.
