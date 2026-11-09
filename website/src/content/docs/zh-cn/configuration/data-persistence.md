---
title: 数据持久化与备份恢复
description: 容器持久化目录结构、JSON 状态文件说明与数据备份恢复方案。
---

OVH Nexus 将所有运行期配置与状态保存在容器内的三个目录中。在 `docker-compose.yml` 中挂载这三个目录后，重启容器或升级镜像不会丢失任何配置。

## 挂载卷概览

| 宿主机路径 | 容器内路径 | 用途说明 |
| :--- | :--- | :--- |
| `./data` | `/app/backend/data` | 保存所有账户凭据、通知中心配置、抢购队列、订单历史与 SKU 快照。 |
| `./logs` | `/app/backend/logs` | 后端应用运行日志文件（`app.log`）。 |
| `./cache` | `/app/backend/cache` | OVHcloud 公开目录（Eco/Baremetal）12 小时磁盘缓存（可随时安全清理）。 |

## `/app/backend/data` 数据文件详解

容器启动时会自动设置 `os.umask(0o077)` 并将 `/app/backend/data` 下所有文件的权限收紧为 `0600`（仅属主可读写）。

| 文件名 | 说明 |
| :--- | :--- |
| `config.json` | 默认账户配置、多通道通知中心配置（`notifications.channels`）与 SKU 监控规则（`notifications.rules`）。 |
| `accounts.json` | 多账户 OVHcloud API 凭据注册表及当前默认账户 ID。 |
| `session_secret` | 自动生成的 32 字节 Flask 会话签名密钥（持久化后重启容器不会导致已登录浏览器掉线）。 |
| `webhook_secret` | 自动生成的 Telegram Webhook 校验密钥（用于校验 `/api/telegram/webhook` 请求头）。 |
| `queue.json` | 自动抢购队列任务列表。 |
| `config_sniper_tasks.json` | 精确硬件子配置（`planCode + 内存 + 存储 + 机房`）狙击任务列表。 |
| `subscriptions.json` | 独立服务器库存变动订阅列表。 |
| `vps_subscriptions.json` | VPS 库存变动订阅列表。 |
| `history.json` | 自动与手动下单历史记录（含订单号与付款链接）。 |
| `sku_snapshot.json` | 公开货架 SKU 基线快照与缺失轮次计数器。 |
| `sku_changes.json` | 最近 200 条公开货架 SKU 上新、下架与子配置变更时间线记录。 |

## 备份与恢复

只需打包宿主机上的 `.env` 与 `./data/` 目录即可完成全量备份：

```bash
tar -czf ovh-nexus-backup-$(date +%F).tar.gz .env data/
chmod 600 ovh-nexus-backup-*.tar.gz
```

:::danger[妥善保管备份文件]
`data/config.json` 与 `data/accounts.json` 内含您的 OVHcloud API 密钥、Telegram Bot Token 与 SMTP 邮箱密码。请勿将 `data/` 或 `.env` 上传至公开代码仓库。
:::
