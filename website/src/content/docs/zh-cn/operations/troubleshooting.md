---
title: 日志查看与故障排查
description: 如何查看容器与后端日志、排查常见启动报错及刷新缓存。
---

## 查看运行日志

OVH Nexus 提供两种查看运行日志的途径：

1. **Web 控制台日志中心 (`/logs`)**：
   - 在线查看结构化业务日志（库存轮询、抢购下单、SKU 变动、通知推送结果），支持按日志级别（`INFO`、`WARNING`、`ERROR`）和分类筛选。
2. **宿主机与容器命令行日志**：
   - 实时查看容器标准输出：
     ```bash
     docker compose logs -f --tail=100
     ```
   - 查看宿主机持久化日志文件：
     ```bash
     tail -f ./logs/app.log
     ```

## 常见问题与解决方法

### 1. 容器启动后立即退出，报错 `RuntimeError: API_SECRET_KEY ...`

**原因**：`.env` 中的 `API_SECRET_KEY` 未填写、长度不足 12 位，或直接使用了文档中的示例占位符（如 `请替换为您自己的高强度密码`、`replace-with-a-strong-random-secret`、`123456`）。

**解决方法**：重新生成一个强随机密钥写入 `.env` 并启动：

```bash
sed -i.bak "s|^API_SECRET_KEY=.*|API_SECRET_KEY=$(openssl rand -hex 24)|" .env
docker compose up -d
```

### 2. 通过 HTTP 访问时输入密码提示成功，但页面一刷新又跳回登录页

**原因**：`.env` 中开启了 `COOKIE_SECURE=true`，但您当前是通过未加密的 `http://<IP>:20000` 访问。浏览器安全策略会拒绝在明文 HTTP 连接上保存或发送带有 `Secure` 标记的 Cookie。

**解决方法**：请改用配置好 TLS 证书的 `https://` 域名访问，或者在局域网纯 HTTP 测试环境下将 `.env` 中的 `COOKIE_SECURE` 设为 `false` 后重启容器。

### 3. 登录时提示 `429 Too Many Requests`（登录尝试过多）

**原因**：同一客户端 IP 在 300 秒内连续输错密码满 5 次，触发了防暴力破解保护。

**解决方法**：等待 `Retry-After` 倒计时（最多 5 分钟）结束，或核对 `.env` 中的 `API_SECRET_KEY` 后执行 `docker compose restart` 重置内存限流计数器。若部署在反向代理后，请确保反代配置正确传递了 `X-Real-IP` 请求头。

### 4. OVHcloud API 报错 `INVALID_CREDENTIAL` 或 `This call has not been granted`

**原因**：`Consumer Key` 已过期，或在申请 Token 时未勾选对应的请求方法与路径权限（例如漏掉了 `GET /vps*` 或 `POST /order/*`）。

**解决方法**：前往对应区域的 `/createToken/` 页面重新生成包含 `GET /*`、`POST /*`、`PUT /*`、`DELETE /*` 权限的凭据（参见 [OVHcloud API 凭据申请](/zh-cn/configuration/ovh-api-credentials/)），并在 **API 账户管理**中更新保存。

### 5. 如何强制刷新公开目录与价格缓存？

OVH Nexus 会将公开 Eco 与 Baremetal 目录缓存在 `./cache` 目录下 12 小时（`CATALOG_TTL_SECONDS = 43200`）。如需立即强制重新拉取最新价格：

- 在**实时库存矩阵**页面点击**刷新**按钮；或
- 清理宿主机 `./cache/*.json` 文件并重启容器：
  ```bash
  rm -f ./cache/*.json && docker compose restart
  ```
