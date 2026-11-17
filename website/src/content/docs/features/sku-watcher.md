---
title: Public SKU Change Watcher
description: Track newly racked or removed OVHcloud server models and configurations without an API key.
---

Before OVHcloud launches a new server generation or flash-sale model (such as `27skle01-v1` or `27risele01-v1`), technicians typically rack the hardware and register the SKU in the public `/dedicated/server/datacenter/availabilities` endpoint with `availability: "unavailable"` hours or days before the product appears on the website.

The **Public SKU Change Watcher** (`internal/monitor`) monitors the entire public availability shelf in the background and alerts you whenever SKUs or hardware configurations are added or removed.

## Anti-False-Positive & Lifecycle Tracking Engine

Public availability APIs occasionally experience transient CDN cache glitches or partial responses. To ensure high-precision tracking without false removal storms, the watcher implements a multi-layer engine:

1. **Silent Baseline Initialization (`baseline: true`) & Three-State Lifecycle (`presale` / `announced` / `listed`)**:
   - On initial deployment (or after changing the monitored OVH endpoint), all existing SKUs are recorded in `/data/nexus.db` (`sku_watcher_state`) with `baseline: true` **without sending notifications or marking existing inventory as newly listed**.
   - The watcher cross-references only exact bare-metal `planCode`s (`RealPlans` from the `eco` and `baremetalServers` catalogs, isolated from the VPS catalog and free of `baseCode`/`product` lookup alias pollution) against live availability rows:
     - **`presale`**: Present in live datacenter availability feeds before appearing in the public catalog (records `presaleSince`).
     - **`announced`**: Published with pricing in the public catalog (`eco` / `baremetalServers`) before any configuration appears in `/dedicated/server/datacenter/availabilities` (such as Limited Edition `KS-LE`, `RISE-LE`, and `SYS-LE` series).
     - **`listed`**: Active in both the public catalog and the live availability feed (records `listedAt`). When an `announced` SKU first enters the live availability feed, the watcher emits an `added` history record with `listingState: "listed"` and `saleStarted: true` (**Sale Started** notification).
2. **Product-Line Version Refresh (`replaced`) Detection & Grouped Notifications**:
   - When an older `planCode` disappears and a newer `planCode` sharing the same product line (`Name + CPU`, e.g., `24sys022 -> 24sys02-v3`) appears in the same check cycle, the watcher merges the pair into a single **`replaced`** timeline entry (`replacedFrom: "24sys022"`) rather than emitting separate add and remove alerts.
   - Outbound `sku.changed` notifications are grouped cleanly by **`🆕 New`**, **`🚀 Sale Started`**, **`🔄 Replaced`**, and **`➖ Discontinued`** under each product line.
3. **Fast ~20-Second Confirmation Recheck (`missingCounts >= 2`)**:
   - When a previously known SKU disappears in a check round (`missingCounts == 1`), it is **not** immediately reported as discontinued. Instead, the watcher schedules a dedicated **~20-second confirmation recheck** (without waiting for the next full polling interval).
   - Only if the SKU remains absent on the confirmation recheck (`missingCounts >= 2`) is it marked as `removed` (`Discontinued`). If it reappears, the counter resets to `0`.
4. **>30% Single-Round Drop Circuit Breaker & Regional Deduplication (`skuMergeRegional`)**:
   - If the total number of returned SKUs drops by **more than 30%** in a single round (when baseline `>= 5` SKUs), the check round is aborted as a transient upstream API anomaly.
   - When `skuMergeRegional` is enabled (default `true`), regional plan variants such as `-sgp`, `-syd`, and `-mum` are normalized into their base SKU so APAC catalog syncs do not produce duplicate alerts.

## Configurable Watcher Rules

In **Stock & SKU Monitor (`/monitor`)** or **Notification Center (`/notifications`)**, you can customize:

| Setting | Key | Default | Description |
| :--- | :--- | :--- | :--- |
| **Enable SKU Watcher** | `skuEnabled` | `true` | Enables background periodic bare-metal SKU checks. |
| **Check Interval (s)** | `skuInterval` | `120` (2m) | Polling interval in seconds (minimum `60` seconds; legacy `600`s default is automatically migrated to `120`s). |
| **Product Lines** | `skuRanges` | `["KS","RISE","SYS","ADV","SCALE","HGR"]` | Filter notifications to specific OVHcloud product families. |
| **Track Sub-Configurations** | `skuTrackConfigs` | `false` | When `true`, also alerts when an existing SKU gains or loses a specific `memory + storage` configuration. |
| **Merge Regional Suffixes** | `skuMergeRegional` | `true` | Deduplicates `-sgp` / `-syd` / `-mum` regional variants. |

## Viewing the SKU Change Timeline

Both the **Stock & SKU Monitor (`/monitor`)** and **Live Availability Matrix (`/availability`)** display the **SKU Change Timeline** (persisted in `/data/nexus.db` and exposed to AI clients via the MCP `catalog_sku_changes` tool) with timestamps, lifecycle badges (**New**, **Announced**, **Presale**, **Sale Started**, **Replaced (`oldCode → newCode`)**, **Discontinued**), commercial model names, CPU/RAM/storage specs, and ex-VAT monthly pricing.
