---
title: 日志查看与故障排查
description: 如何通过 SSE 实时日志中心查看/导出日志、排查启动报错及刷新目录缓存。
---

## 查看与导出运行日志

OVH Nexus 提供两种查看运行日志的途径（所有后端日志均由 `log/slog` 以标准化英文输出并自动脱敏）：

1. **Web 控制台 SSE 实时日志中心 (`/logs`)**：
   - 通过 **Server-Sent Events (`GET /api/logs/stream`)** 实时推送系统运行事件，底层由 `/data/nexus.db` 中的 **10,000 条滚动环形缓冲区**支持。
   - 支持按**模块 (`source`)**（`server`、`http`、`auth`、`totp`、`vault`、`ovh`、`catalog`、`queue`、`stock-monitor`、`vps-monitor`、`sku-watcher`、`fleet`、`notify`、`mcp`）、**日志级别**（`DEBUG`、`INFO`、`WARNING`、`ERROR`）和**关键词**筛选，展示结构化属性标签，按浏览器本地时区渲染时间，并支持一键导出 **`.jsonl`** 或 **`.log`** 文件。
2. **容器标准输出日志**：
   - 实时查看容器输出（支持 `LOG_FORMAT=text` 或 `json`）：
     ```bash
     docker compose logs -f --tail=100
     ```

## 常见问题与解决方法

### 1. 容器启动后立即退出，提示 `API_SECRET_KEY` 或 `DATA_ENCRYPTION_KEY` 错误

**原因**：`.env` 中的 `API_SECRET_KEY`（需不少于 12 位）或 `DATA_ENCRYPTION_KEY`（需不少于 16 位）未填写、两者相同或使用了文档中的示例占位符。

**解决方法**：重新生成两把独立的强随机密钥写入 `.env` 并启动：

```bash
sed -i.bak "s|^API_SECRET_KEY=.*|API_SECRET_KEY=$(openssl rand -hex 24)|" .env
sed -i.bak "s|^DATA_ENCRYPTION_KEY=.*|DATA_ENCRYPTION_KEY=$(openssl rand -hex 32)|" .env
docker compose up -d
```

### 2. 打开 `/data/nexus.db` 时提示 `permission denied` 或 `PUID`/`PGID` 校验失败

**原因**：在标准 Docker 部署下，容器入口会自动初始化 `/data`（`0700`）并通过 `su-exec` 降权至 `PUID:PGID`（默认 `65532:65532`）。但如果您通过 Docker Compose `user:`、`docker run --user` 或 Kubernetes `securityContext.runAsNonRoot: true` 强制以非 Root 身份启动（`id -u != 0`），而宿主机挂载目录仍归 `root` 所有，入口脚本便无法对 `/data` 执行 `chown`。此外，若传入非纯数字的 `PUID`/`PGID`（例如用户名），启动时会报错 `[entrypoint] ERROR: PUID/PGID must be a numeric ...` 并退出。

**解决方法**：任选以下两种方式之一：

1. **交由容器入口通过 `su-exec` 自动降权（Docker Compose 推荐）**：移除强制指定的 `user:` 配置，让入口脚本先完成 `/data` 目录初始化再通过 `su-exec` 降权运行 `ovh-nexus`（如需指定宿主机非 Root 用户属主，可在 `.env` 中配置 `id -u` / `id -g` 获取的纯数字 `PUID` 和 `PGID`）；**或者**
2. **手动对齐宿主机目录或 Kubernetes `fsGroup` 权限**：若必须保留 `user:` 或 Kubernetes `runAsNonRoot: true`，请确保宿主机挂载目录或 Pod `fsGroup` 对该强制运行 UID 具备读写权限：

```bash
chown -R <RUNTIME_UID>:<RUNTIME_GID> ./data
docker compose up -d
```

### 3. 通过 HTTP 访问时输入密码提示成功，但页面一刷新又跳回登录页

**原因**：`.env` 中开启了 `COOKIE_SECURE=true`，但您当前是通过未加密的 `http://<IP>:20000` 访问。浏览器安全策略会拒绝在明文 HTTP 连接上保存或发送带有 `Secure` 标记的 Cookie。

**解决方法**：请改用配置好 TLS 证书的 `https://` 域名访问，或者在局域网纯 HTTP 测试环境下将 `.env` 中的 `COOKIE_SECURE` 设为 `false` 后重启容器。

### 4. 登录时提示 `429 Too Many Requests`（登录尝试过多）

**原因**：同一客户端 IP 在 300 秒内连续输错密码满 5 次，或反向代理部署未配置 `TRUSTED_PROXIES` 导致所有客户端共用代理 IP 的限流计数。

**解决方法**：等待 `Retry-After` 倒计时（最多 5 分钟）结束，或核对 `.env` 中的 `API_SECRET_KEY` 后执行 `docker compose restart`。若部署在反向代理后，请确保反代传递了 `X-Real-IP` / `X-Forwarded-For` 并将反代 IP/CIDR 写入 `.env` 的 `TRUSTED_PROXIES`。

### 5. OVHcloud API 报错 `INVALID_CREDENTIAL` 或 `This call has not been granted`

**原因**：`Consumer Key` 已过期，或在申请 Token 时未勾选对应的请求方法与路径权限（例如漏掉了 `GET /vps*` 或 `POST /order/*`）。

**解决方法**：前往对应区域的 `/createToken/` 页面重新生成包含 `GET /*`、`POST /*`、`PUT /*`、`DELETE /*` 权限的凭据（参见 [OVHcloud API 凭据申请](/zh-cn/configuration/ovh-api-credentials/)），并在 **API 账户管理**中更新保存。

### 6. 如何强制刷新公开目录与价格缓存？

OVH Nexus 会将公开 Eco 与 Baremetal 目录缓存 12 小时。如需立即强制重新拉取最新价格：

- 在**实时库存矩阵（`/ovh-availability`）**页面点击**刷新**按钮，或执行 `docker compose restart`。
