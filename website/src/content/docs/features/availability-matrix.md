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

## Exact Configuration-Level Monthly Pricing & Compact Views

Unlike basic stock checkers that only show a plan's starting base price, OVH Nexus computes the **estimated ex-VAT monthly price for each specific hardware configuration (`configKey`: `memory+storage`)** via `/api/catalog/availability`, `/api/catalog/plans`, and `GET /api/catalog/plans/{planCode}/configs`:

```text
Estimated Monthly Price (ex-VAT) = Base Plan Monthly Price (P1M)
                                 + Memory Addon Monthly Surcharge (if > 0)
                                 + Storage Addon Monthly Surcharge (if > 0)
```

- **Compact Single-Row Availability Matrix (`/availability`)**:
  - Consolidates identical `planCode` configurations into **1 row per `planCode`** (while keeping distinct regional `planCode`s such as `-sgp`, `-syd`, and `-mum` on their own rows).
  - Each row includes an **inline configuration dropdown**:
    - **All Configurations (`N`)** *(default)*: Each datacenter cell displays the **best availability status** across all configurations of that `planCode` along with an in-stock configuration counter.
    - **Specific Configuration (`Memory + Storage — €XX.XX/mo · K DCs`)**: Selecting a specific configuration immediately switches the row's price display and datacenter cells to that exact configuration's stock status.
- **Compact Collapsible Server Catalog Cards (`/servers`)**:
  - Displays a dense 3–4 column desktop card grid **collapsed by default** (showing model name, `planCode`, CPU, 1-line default spec summary, starting monthly price `from €XX.XX/mo`, compact in-stock datacenter flag pills, and status badges).
  - Expanding a card reveals a **single unified selectable configuration list** (with the default configuration marked `Default` and pre-selected, displaying ex-VAT monthly pricing and per-datacenter stock pills). By default, only in-stock configurations and datacenters are shown; enable **Show all inventory (including out-of-stock)** to inspect unavailable configurations, or open the collapsible **Advanced Options** section for bandwidth and vRack addons.
- Sort by **Price: Low to High** or **Price: High to Low**, or filter by product line (`KS`, `SYS`, `RISE`, `ADV`, `SCALE`, `HGR`), datacenter (`GRA`, `SBG`, `RBX`, `FRA`, `BHS`, `TOR`, `SGP`, `SYD`, etc.), and listing state (`All`, `Listed`, `Announced`, `Presale`, `New`).

## Listing States (`listed`, `announced`, `presale`) & Quick Monitor

OVH Nexus tracks three distinct catalog lifecycle states across `/availability` and `/servers`:

- **Listed (`listingState: "listed"`)**: Present in the public catalog (`eco` / `baremetalServers`) **and** active in the live datacenter availability feed.
- **Announced (`listingState: "announced"`)**: Published with official pricing and default specs in the OVHcloud public catalog, but **not yet active** in `/dedicated/server/datacenter/availabilities` (such as newly announced `KS-LE`, `RISE-LE`, and `SYS-LE` Limited Edition models prior to flash-sale activation). When availability goes live, OVH Nexus automatically emits a **Sale Started (`saleStarted: true`)** alert.
- **Presale (`listingState: "presale"`)**: Newly racked `planCode`s (`2024`–`2027` generations) already registered in the live availability feed hours or days before appearing in the public catalog.
- **One-Click Multi-Target Quick Monitor (`QuickMonitorDialog`)**: Click **Quick Monitor** on any row in `/availability` or card in `/servers` to pass the selected `planCode`, configuration (`configKey` + addon `options`), and datacenters directly into the configuration-level monitor dialog—where you can configure one or more monitoring targets with per-target quantity and an ex-VAT **Max Monthly Price (`maxMonthlyPrice`)** cap.
- **Tri-Lingual Datacenter Geolocation Labels**: All datacenter badges and filters across the console and notification templates render localized city, country, and flag emoji labels (`en` / `fr` / `zh-CN`), such as `BHS (Beauharnois, Canada 🇨🇦)` and `TOR (Toronto, Canada 🇨🇦)` in full views or compact `🇨🇦 BHS` / `🇨🇦 TOR` pills with hover tooltips.
