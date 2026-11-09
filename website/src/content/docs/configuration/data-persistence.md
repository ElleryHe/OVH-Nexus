---
title: Data Persistence & Backup
description: Directory structure, persisted JSON files, cache TTLs, and backup procedures.
---

OVH Nexus stores all runtime state on the local filesystem inside three container directories. Mounting these three directories in `docker-compose.yml` ensures zero data loss across container restarts or image upgrades.

## Volume Mounts Overview

| Host Path | Container Path | Purpose |
| :--- | :--- | :--- |
| `./data` | `/app/backend/data` | Accounts, settings, notification channels, queue tasks, purchase history, and SKU snapshots. |
| `./logs` | `/app/backend/logs` | Rotating application log (`app.log`). |
| `./cache` | `/app/backend/cache` | Cached OVHcloud Eco/Baremetal catalogs and server metadata (safe to delete anytime). |

## Files Inside `/app/backend/data`

All files created inside `/app/backend/data` are automatically assigned `0600` (`rw-------`) permissions at startup (`os.umask(0o077)`).

| Filename | Description |
| :--- | :--- |
| `config.json` | Active default account settings, notification channels (`notifications.channels`), and SKU watcher rules (`notifications.rules`). |
| `accounts.json` | Multi-account OVHcloud API credential registry and default account pointer. |
| `session_secret` | Auto-generated 32-byte key used to sign `ovh_session` cookies (persisted so sessions survive container restarts). |
| `webhook_secret` | Auto-generated secret token used to verify incoming Telegram webhook requests (`/api/telegram/webhook`). |
| `queue.json` | Active and queued automated server procurement tasks. |
| `config_sniper_tasks.json` | Exact hardware configuration (`planCode + memory + storage + datacenter`) sniper tasks. |
| `subscriptions.json` | Dedicated server stock monitoring subscriptions. |
| `vps_subscriptions.json` | VPS stock monitoring subscriptions. |
| `history.json` | Historical log of automated and manual order attempts (with order IDs and payment URLs). |
| `sku_snapshot.json` | Baseline state of public OVHcloud server SKUs and missing-check counters. |
| `sku_changes.json` | Timeline of the most recent 200 public SKU additions, removals, and configuration changes. |

## Backing Up & Restoring

To back up your entire OVH Nexus state, create an encrypted or access-restricted archive of the `./data` directory and `.env` file:

```bash
tar -czf ovh-nexus-backup-$(date +%F).tar.gz .env data/
chmod 600 ovh-nexus-backup-*.tar.gz
```

:::danger[Protect Your Backup Archives]
`data/config.json` and `data/accounts.json` contain your OVHcloud API keys, Telegram Bot tokens, and SMTP passwords. Never upload `data/` or `.env` to a public Git repository.
:::
