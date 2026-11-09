---
title: Public SKU Change Watcher
description: Track newly racked or removed OVHcloud server models and configurations without an API key.
---

Before OVHcloud launches a new server generation or flash-sale model (such as `27skle01-v1` or `27risele01-v1`), technicians typically rack the hardware and register the SKU in the public `/dedicated/server/datacenter/availabilities` endpoint with `availability: "unavailable"` hours or days before the product appears on the website.

The **Public SKU Change Watcher** (`backend/sku_watcher.py`) monitors the entire public availability shelf in the background and alerts you whenever SKUs or hardware configurations are added or removed.

## Anti-False-Positive & Circuit-Breaker Engine

Public availability APIs occasionally experience transient CDN cache glitches or partial responses. To prevent false removal storms, `SkuWatcher` implements a 4-layer protection engine:

1. **Silent Baseline Initialization (`initialized: false → true`)**:
   - On the very first scan (or after changing the monitored OVH endpoint), `SkuWatcher` records all current SKUs into `data/sku_snapshot.json` as the baseline **without sending any notifications**.
2. **2-Round Consecutive Removal Confirmation (`missingCounts >= 2`)**:
   - When a previously known SKU disappears in a check round, it is **not** immediately reported as removed. Instead, its `missingCounts` counter is incremented to `1`.
   - Only if the SKU remains absent in the **next consecutive round** (`missingCounts >= 2`) is a `sku.changed` (`SKU Removed`) event emitted. If it reappears on the next check, the counter resets to `0`.
3. **>30% Single-Round Drop Circuit Breaker**:
   - If the total number of returned SKUs drops by **more than 30%** compared to the previous snapshot (when the baseline has `>= 5` SKUs), the entire check round is aborted and logged as a transient OVH API anomaly.
4. **Regional Variant Deduplication (`skuMergeRegional`)**:
   - When enabled (default `true`), regional plan variants such as `24ska01-sgp` and `24ska01-syd` are normalized into their base SKU or tagged cleanly so APAC catalog syncs do not produce duplicate alerts.

## Configurable Watcher Rules

In **Notification Center (`/notifications`) → SKU Watcher Rules**, you can customize:

| Setting | Key | Default | Description |
| :--- | :--- | :--- | :--- |
| **Enable SKU Watcher** | `skuEnabled` | `true` | Enables background periodic SKU checks. |
| **Check Interval (s)** | `skuInterval` | `600` (10m) | Polling interval in seconds (minimum `60` seconds). |
| **Product Lines** | `skuRanges` | `["KS","RISE","SYS","ADV","SCALE","HGR"]` | Filter notifications to specific OVHcloud product families. |
| **Track Sub-Configurations** | `skuTrackConfigs` | `false` | When `true`, also alerts when an existing SKU gains or loses a specific `memory + storage` FQN variant (`CONFIG_ADDED` / `CONFIG_REMOVED`). |
| **Merge Regional Suffixes** | `skuMergeRegional` | `true` | Deduplicates `-sgp` / `-syd` / `-mum` regional variants. |

## Viewing the SKU Change Timeline

Both the **Notification Center (`/notifications`)** and the **Live Availability Matrix (`/ovh-availability`)** display the **Recent SKU Change Timeline** (`data/sku_changes.json`, retaining up to 200 events) with timestamps, badge types (`SKU Added`, `SKU Removed`, `Config Added`, `Config Removed`), commercial model names, readable RAM/storage specs, and monthly pricing.
