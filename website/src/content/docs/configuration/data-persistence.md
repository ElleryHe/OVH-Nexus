---
title: Data Persistence & Backup
description: Single-volume SQLite WAL architecture, column-level encryption, and backup procedures.
---

OVH Nexus consolidates all runtime configuration, encrypted secrets, queue state, monitoring baselines, and structured logs into a **single persistent volume mount (`./data:/data`)** backed by pure-Go embedded **SQLite in WAL mode** (`/data/nexus.db`).

## Volume Mount Overview

| Host Path | Container Path | Purpose |
| :--- | :--- | :--- |
| `./data` | `/data` | Stores `nexus.db` (plus SQLite `-wal` and `-shm` sidecar files) containing all persistent state. |

:::note[Automatic Non-Root Ownership (`su-exec`, `PUID`/`PGID`)]
On container boot, `/usr/local/bin/docker-entrypoint.sh` automatically initializes `/data` with `0700` permissions owned by `PUID:PGID` (default `65532:65532`, `nexus:nexus`) and drops privileges in-place via `su-exec` before `ovh-nexus` creates `/data/nexus.db` with `0600` (`rw-------`) permissions—so no manual host `chown` is needed. Non-root Linux host users can optionally set `PUID`/`PGID` in `.env`. When started already as a non-root UID (`id -u != 0`, such as Kubernetes `runAsNonRoot: true`), the entrypoint executes `ovh-nexus` directly; ensure your volume or `fsGroup` grants write access to that UID.
:::

## SQLite Tables in `/data/nexus.db`

All tables use `PRAGMA journal_mode=WAL; PRAGMA synchronous=NORMAL; PRAGMA busy_timeout=5000; PRAGMA foreign_keys=ON;`:

| Table | Contents & Security Protection |
| :--- | :--- |
| `kv_settings` | System preferences (`default_account_id`, notification `locale`, `panel_url`, `vps_check_interval`) and encrypted `session_secret`. |
| `ovh_accounts` | Multi-account OVHcloud registry (`endpoint`, `zone`, `is_default`) with `AES-256-GCM` (`enc:v1:...`) encrypted `app_key`, `app_secret`, and `consumer_key`. |
| `totp_state` | RFC 6238 TOTP 2FA singleton (`enabled`, encrypted `secret` & `pending_secret`, `SHA-256` hashed single-use recovery codes, `last_used_step`). |
| `order_queue` | Automated procurement queue items (`plan_code`, datacenters, hardware options, `auto_pay`, `waive_retractation_period`). |
| `purchase_history` | Historical log of automated and manual orders (Order IDs, payment URLs, pricing, status). |
| `stock_subscriptions` & `vps_subscriptions` | Dedicated server and VPS availability subscriptions plus availability state snapshots. |
| `sku_watcher_state` | Public SKU baseline snapshot, missing-round counters, and recent SKU change timeline. |
| `notification_channels` & `notified_markers` | Outbound Telegram Bot & SMTP Email channels (with encrypted tokens/passwords) plus deduplication markers for delivery/expiry alerts. |
| `mcp_config`, `mcp_aliases`, `mcp_audit_logs` | Irreversible `SHA-256` `token_hash` + `masked_token`, L1–L3 scopes, persistent bidirectional privacy aliases (`acct-01`, `bm-01`, `vps-01`, `ip-01`, `ord-01`), and MCP audit logs. |
| `system_logs` | Standardized English structured logs (`id`, UTC `timestamp`, `level`, `source`, `message`, `attrs_json`), automatically pruned to a **10,000-row rolling ring buffer**. |

## Backing Up & Restoring

To back up your entire OVH Nexus instance safely while preserving WAL consistency, briefly stop the container (or run SQLite's `.backup` command) and archive `.env` alongside `./data/`:

```bash
# Consistent cold backup
docker compose stop
tar -czf ovh-nexus-backup-$(date +%F).tar.gz .env data/
chmod 600 ovh-nexus-backup-*.tar.gz
docker compose start
```

:::danger[Protect Your Backup Archives & `DATA_ENCRYPTION_KEY`]
`/data/nexus.db` contains encrypted OVHcloud API keys, Telegram Bot tokens, SMTP passwords, and 2FA seeds that can only be decrypted with the `DATA_ENCRYPTION_KEY` stored in your `.env` file. Always back up `.env` and `./data/` together, keep backups offline or encrypted, and never commit either to a public Git repository.
:::
