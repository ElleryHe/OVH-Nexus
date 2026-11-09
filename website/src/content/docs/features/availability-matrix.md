---
title: Live Availability Matrix & Pricing
description: Inspect real-time OVHcloud bare-metal stock, human-readable hardware specs, and exact configuration-level monthly pricing.
---

The **OVH Live Availability Matrix** (`/ovh-availability`) queries OVHcloud's public `/dedicated/server/datacenter/availabilities` API and enriches every result with `CatalogService` (`/order/catalog/public/eco` and `/order/catalog/public/baremetal`).

:::tip[No API Key Required]
Both the availability endpoint and the public Eco/Baremetal catalogs are public OVHcloud endpoints. You can browse live stock and prices across all datacenters immediately after deploying OVH Nexus.
:::

## Consumer-Friendly Model Names & Hardware Formatting

Raw OVHcloud availability records only expose internal codes such as `24ska01`, `memory: ram-64g-ecc-3200`, and `storage: raid-r1-2x480ssd-s10-2x6000sa`. OVH Nexus automatically translates these into readable specifications:

1. **Commercial Model Name Resolution**:
   - Matches each `planCode` against the cached Eco (`Kimsufi`, `So you Start`, `Rise`) and Baremetal (`Advance`, `Scale`, `High Grade`) catalogs (12-hour disk cache in `cache/`).
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
- Sort the entire availability matrix by **Price: Low to High** or **Price: High to Low**, or filter by product line (`KS`, `SYS`, `RISE`, `ADV`, `SCALE`, `HGR`), datacenter (`GRA`, `SBG`, `RBX`, `FRA`, `BHS`, `SGP`, `SYD`, etc.), and stock status (`Available Only` / `1H High` / `72H`).
