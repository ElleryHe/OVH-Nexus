---
title: 安全架构与加固建议
description: OVH Nexus 的内置纵深防御机制与生产环境自托管安全加固清单。
---

OVH Nexus 涉及高价值云基础设施管理与自动下单权限，因此在后端认证（`backend/api_auth_middleware.py`、`backend/api_key_config.py`）和容器网络层均默认启用了纵深防御机制。

## 内置安全防护机制

1. **启动期强制强口令校验 (`validate_password`)**：
   - 容器启动时立即校验 `API_SECRET_KEY`。若未配置、长度不足 12 位，或命中任何常见弱口令及文档示例值，进程直接拒绝启动。
2. **HttpOnly 服务端签名会话 Cookie**：
   - Web 浏览器登录后使用服务端签名的 `ovh_session` Cookie（默认 `HttpOnly=True`、`SameSite=Lax`、有效期 12 小时）。
   - 登录口令不会明文驻留在浏览器 `localStorage` 中，彻底杜绝 XSS 窃取持久化口令的风险。
3. **防暴力破解速率限制 (`429 Too Many Requests`)**：
   - `/api/auth/login` 按客户端真实 IP（`X-Real-IP` / `remote_addr`）统计失败次数。若**在 300 秒窗口内连续失败满 5 次**，后续登录请求将直接返回 HTTP `429` 及 `Retry-After` 倒计时响应头。
4. **`X-API-Key` 严格本机回环限制 (`_is_direct_loopback`)**：
   - 请求头 `X-API-Key` 认证方式**仅允许未携带任何反向代理头（`X-Forwarded-For` / `X-Real-IP`）的本机回环连接（`127.0.0.1` / `::1`）**使用；任何经过 Nginx 转发的外部请求必须通过登录会话 Cookie 认证。
5. **恒定时间密钥比对 (`hmac.compare_digest`)**：
   - 所有登录密码、内部进程令牌（`INTERNAL_TOKEN`）、API 密钥及 Telegram Webhook 令牌校验均采用恒定时间比对函数，防御时序侧信道攻击。
6. **持久化文件权限强制收紧 (`0600`)**：
   - 启动时自动设置 `os.umask(0o077)` 并将 `/app/backend/data` 目录下所有存量文件权限修正为 `0600`。
7. **防搜索引擎收录 (`robots.txt`)**：
   - 控制台前端内置 `Disallow: /`，防止自托管面板地址被搜索引擎爬虫抓取索引。

## 生产环境自托管加固清单

- [ ] 使用至少 24 字节的强随机字符串作为 `API_SECRET_KEY`（例如 `openssl rand -hex 24`）。
- [ ] 在 `docker-compose.yml` 中将容器端口仅绑定在回环地址（`127.0.0.1:20000:80`），并通过 Caddy、Nginx 或 Cloudflare Tunnel 提供 HTTPS 访问。
- [ ] 通过 HTTPS 反向代理访问时，在 `.env` 中设置 `COOKIE_SECURE=true`。
- [ ] 生产环境始终保持 `DEBUG=false`（当 `DEBUG=false` 时，即便误设 `ENABLE_API_KEY_AUTH=false`，系统也会忽略该配置并强制开启认证）。
- [ ] 收紧宿主机上的 `.env` 与 `./data` 目录权限（`chmod 600 .env && chmod 700 data`）。
