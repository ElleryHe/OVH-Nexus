---
title: 数据持久化与备份恢复
description: 单挂载卷 SQLite WAL 持久化架构、字段级静态加密与备份恢复指南。
---

OVH Nexus 将所有运行期配置、加密凭据、采购队列、监控状态与结构化日志统一整合在**单个持久化挂载卷（`./data:/data`）**中，由纯 Go 内嵌 **SQLite WAL 模式数据库**（`/data/nexus.db`）驱动。

## 挂载卷概览

| 宿主机路径 | 容器内路径 | 用途说明 |
| :--- | :--- | :--- |
| `./data` | `/data` | 保存核心数据库 `nexus.db`（及 SQLite `-wal`、`-shm` 事务文件），涵盖全部持久化数据。 |

:::note[自动非 Root 目录归属初始化 (`su-exec`、`PUID`/`PGID`)]
容器启动时，`/usr/local/bin/docker-entrypoint.sh` 会自动确保 `/data` 目录存在、将权限收敛为 `0700` 并设为 `PUID:PGID`（默认 `65532:65532`，即 `nexus:nexus`）所有，随后通过 `su-exec` 就地降权运行 `ovh-nexus`（后者以 `0600` 权限创建 `/data/nexus.db`），无需在宿主机手动执行 `chown`。Linux 非 Root 宿主机用户可在 `.env` 中按需设置 `PUID`/`PGID`。若容器本身以非 Root 身份直接启动（`id -u != 0`，例如 Kubernetes `runAsNonRoot: true`），入口脚本将跳过 `chown` 直接拉起 `ovh-nexus`，此时需确保持久化卷或 `fsGroup` 对该运行 UID 可写。
:::

## `/data/nexus.db` 数据表结构

数据库默认启用 `PRAGMA journal_mode=WAL; PRAGMA synchronous=NORMAL; PRAGMA busy_timeout=5000; PRAGMA foreign_keys=ON;`：

| 数据表 | 存储内容与加密保护 |
| :--- | :--- |
| `kv_settings` | 系统级设置（`default_account_id`、通知语言 `locale`、`panel_url`、`vps_check_interval`）及加密存储的 `session_secret`。 |
| `ovh_accounts` | 多账户 OVHcloud 注册表（`endpoint`、`zone`、`is_default`），`app_key`、`app_secret`、`consumer_key` 均经 `AES-256-GCM`（`enc:v1:...`）字段级加密。 |
| `totp_state` | RFC 6238 TOTP 双因素认证单例状态（`enabled`、加密 `secret` 与 `pending_secret`、`SHA-256` 哈希应急恢复码、`last_used_step`）。 |
| `order_queue` | 自动化采购队列任务（`plan_code`、目标机房、硬件选项、`auto_pay`、`waive_retractation_period`）。 |
| `purchase_history` | 自动与手动下单历史记录（含订单号、官方付款链接、含税价格与状态）。 |
| `stock_subscriptions` & `vps_subscriptions` | 独立服务器与 VPS 库存变动订阅任务及状态快照。 |
| `sku_watcher_state` | 公开货架 SKU 基线快照、缺失确认计数器及近期 SKU 变动时间线。 |
| `notification_channels` & `notified_markers` | 纯出站 Telegram 机器人与 SMTP 邮件通道配置（Token 与密码加密存储）及交付/到期去重标记。 |
| `mcp_config`、`mcp_aliases`、`mcp_audit_logs` | 不可逆 `SHA-256` `token_hash` 与脱敏预览 `masked_token`、L1–L3 权限开关、持久化双向隐私代号表（`acct-01`、`bm-01`、`vps-01`、`ip-01`、`ord-01`）与 MCP 审计日志。 |
| `system_logs` | 全标准化英文结构化运行日志（`id`、UTC `timestamp`、`level`、`source`、`message`、`attrs_json`），自动滚动裁剪保留最新 **10,000 条**。 |

## 备份与恢复

为确保 WAL 事务日志完整落盘，建议短暂停止容器后将 `.env` 与 `./data/` 一并打包备份：

```bash
docker compose stop
tar -czf ovh-nexus-backup-$(date +%F).tar.gz .env data/
chmod 600 ovh-nexus-backup-*.tar.gz
docker compose start
```

:::danger[妥善保管备份文件与 `DATA_ENCRYPTION_KEY`]
`/data/nexus.db` 内的所有敏感凭据（OVHcloud API 密钥、Telegram Token、SMTP 密码与 2FA 种子）均依赖 `.env` 中的 `DATA_ENCRYPTION_KEY` 进行解密。请务必同时备份 `.env` 与 `./data/`，离线妥善保存，切勿上传至任何公开仓库。
:::
