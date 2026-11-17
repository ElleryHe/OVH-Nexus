---
title: Live Availability Matrix & Pricing
description: Inspect real-time OVHcloud bare-metal stock, human-readable hardware specs, and exact configuration-level monthly pricing.
---

The **OVH Live Availability Matrix** (`/ovh-availability`) queries OVHcloud's public `/dedicated/server/datacenter/availabilities` API and enriches every result with `internal/catalog` (`/order/catalog/public/eco` and `/order/catalog/public/baremetal`).

:::tip[No API Key Required]
Both the availability endpoint and the public Eco/Baremetal catalogs are public OVHcloud endpoints. You can browse live stock and prices across all datacenters immediately after deploying OVH Nexus.
:::

## Consumer-Friendly Model Names & Hardware Formatting

Raw OVHcloud availability records only expose internal codes such as `24ska01`, `memory: ram-64g-ecc-3200`, and `storage: raid-r1-2x480ssd-s10-2x6000sa`. OVH Nexus automatically translates these into readable specifications:

1. **Commercial Model Name Resolution**:
   - Matches each `planCode` against the cached Eco (`Kimsufi`, `So you Start`, `Rise`) and Baremetal (`Advance`, `Scale`, `High Grade`) catalogs (12-hour TTL cache).
   - For newly racked or unreleased SKUs not yet present in the public catalog (e.g., `27skle01-v1`), falls back to a deterministic rule parser (`KS-LE-1`, `KS-A`, `RISE-1`, `SCALE-a1`).
   - Automatically merges APAC regional suffix variants (`-sgp`, `-syd`, `-mum`, `-apac`) and version suffixes (`-v1`, `-v2`).
2. **Localized Hardware Spec Translation (`en` / `fr` / `zh-CN`)**:
   - **Memory**: Converts `ram-64g-ecc-3200` into `64 GB DDR4 ECC 3200 MHz`.
   - **Storage & System Disks**: Converts `softraid-2x512nvme` into `2 × 512 GB NVMe (Soft RAID)` and `raid-r1-2x480ssd-s10-2x6000sa` into separate boot (`2 × 480 GB SSD, HW RAID 1`) and data (`2 × 6 TB SATA, HW RAID 10`) descriptions.

## Exact Configuration-Level Monthly Pricing

Unlike basic stock checkers that only show a plan's starting base price, OVH Nexus computes the **exact monthly price for each specific hardware configuration (`FQN`)**:

```text
Total Monthly Price = Base Plan Monthly Price (P1M)
                    + Memory Addon Monthly Surcharge (if > 0)
                    + Storage Addon Monthly Surcharge (if > 0)
                    + System Storage Addon Surcharge (if > 0)
```

- When a `planCode` has multiple memory/storage configurations with different prices, the matrix displays the starting price (`From €XX.XX/mo`) on the plan summary card and the **exact monthly price badge** (`€XX.XX/mo` + setup fee if any) on each individual configuration card.
- Sort the entire availability matrix by **Price: Low to High** or **Price: High to Low**, or filter by product line (`KS`, `SYS`, `RISE`, `ADV`, `SCALE`, `HGR`), datacenter (`GRA`, `SBG`, `RBX`, `FRA`, `BHS`, `TOR`, `SGP`, `SYD`, etc.), and stock status (`Available Only` / `1H High` / `72H`).

## Presale / Unlisted Server Detection & Quick Monitor

OVHcloud technicians often rack new server generations (`2024`–`2027`) and register their `planCode`s in the live availability feed hours or days before publishing commercial pricing in the public catalog:

- **Presale Badges & Generation Tags (`listingState: "presale"`)**: Both the **Live Availability Matrix (`/availability`)** and **Server Catalog (`/servers`)** automatically detect unlisted/presale SKUs, display a **Presale** badge alongside the inferred hardware generation (`2024`–`2027`), and provide a **Presale Only** filter toggle.
- **One-Click Quick Monitor (`QuickMonitorDialog`)**: Click **Quick Monitor** on any row in `/availability` or `/servers` to open a unified modal where you can select target datacenters (with live per-datacenter stock badges), toggle restock/out-of-stock notifications, filter specific `memory` / `storage` configurations, and pre-arm automated ordering (for presale models, notifications fire immediately while auto-ordering activates automatically once public catalog pricing goes live).
- **Tri-Lingual Datacenter Geolocation Labels**: All datacenter badges and filters across the console and notification templates render localized city, country, and flag emoji labels (`en` / `fr` / `zh-CN`), such as `BHS (Beauharnois, Canada 🇨🇦)` and `TOR (Toronto, Canada 🇨🇦)` in full views or compact `🇨🇦 BHS` / `🇨🇦 TOR` pills with hover tooltips.
