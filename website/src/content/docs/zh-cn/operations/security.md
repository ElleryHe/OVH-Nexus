---
title: 安全架构与加固建议
description: OVH Nexus 的内置纵深防御机制与生产环境自托管安全加固清单。
---

OVH Nexus 涉及高价值云基础设施管理与自动下单权限，因此其 Go 单静态二进制后端（`internal/config`、`internal/auth`、`internal/vault`、`internal/httpx`）与非 Root Alpine 容器在架构层面默认启用了纵深防御机制。

## 内置安全防护机制

1. **启动期 Fail-Fast 强密钥校验 (`internal/config`)**：
   - 进程启动时立即校验 `API_SECRET_KEY`（不少于 12 位）与 `DATA_ENCRYPTION_KEY`（不少于 16 位且严禁与前者相同）。若缺失、过短或命中常见弱口令/文档示例占位符，服务直接拒绝启动。
2. **原生 Slowloris 与 HTTP 洪水防护 (`internal/httpx`)**：
   - Go `net/http` 服务端直接实施严格的连接超时（`ReadHeaderTimeout: 5s`、`ReadTimeout: 15s`、`WriteTimeout: 120s`、`IdleTimeout: 60s`）、`2 MB` 请求体上限及严格的 CSP 安全响应头。
3. **HttpOnly 服务端签名会话 Cookie**：
   - Web 浏览器登录后使用 HMAC-SHA256 签名的 `ovh_session` Cookie（`HttpOnly=true`、`SameSite=Lax`、有效期 12 小时）。
   - `API_SECRET_KEY` 绝不会明文驻留在浏览器 `localStorage` 中，彻底杜绝 XSS 窃取持久化口令的风险。
4. **基于可信代理的防暴力破解限流 (`429 Too Many Requests`)**：
   - `/api/auth/login` 与 TOTP 验证按客户端 IP 统计失败次数（**300 秒内连续失败 5 次**触发 HTTP `429` 与 `Retry-After` 限流）。
   - 仅当直接 TCP 来源位于 `TRUSTED_PROXIES` 指定的 IP/CIDR 列表内时，系统才会采信 `X-Forwarded-For` 与 `X-Real-IP` 请求头，防止外部伪造 IP 绕过限流。
5. **`X-API-Key` 严格本机回环限制**：
   - 请求头 `X-API-Key` 认证方式**仅允许未携带任何代理转发头的本机直接回环连接（`127.0.0.1` / `::1`）**使用；任何经反向代理转发的外部请求均必须通过登录会话 Cookie 认证。
6. **恒定时间密钥比对 (`crypto/subtle.ConstantTimeCompare`)**：
   - 所有登录密码、内部进程令牌、API 密钥与令牌哈希校验均采用恒定时间比对，防御时序侧信道攻击。
7. **非 Root 降权隔离 (`su-exec`、`PUID`/`PGID`)、`0600` SQLite WAL 与 AES-256-GCM 静态加密金库 (`internal/vault`)**：
   - 容器启动时，Alpine 入口脚本（`/usr/local/bin/docker-entrypoint.sh`）自动将 `/data` 权限收紧为 `0700`，并通过 `su-exec` 就地降权至非 Root 用户 `PUID:PGID`（默认 `65532:65532`，`nexus:nexus`）再拉起 `ovh-nexus`，后者以 `0600` 权限打开 `/data/nexus.db`；同时原生支持 Kubernetes `runAsNonRoot: true`（`id -u != 0`）。
   - 所有落盘敏感凭据（`app_key`、`app_secret`、`consumer_key`、Telegram Token、SMTP 密码、TOTP 种子与 `session_secret`）均通过独立的 `DATA_ENCRYPTION_KEY`（`HKDF-SHA256` + `AES-256-GCM`）加密为 `enc:v1:<nonce>:<ciphertext>` 密文，并在初始化完成后清除内存中的原始主密钥；MCP 网关令牌与 TOTP 应急恢复码仅存储不可逆的 `SHA-256` 哈希。
8. **RFC 6238 TOTP 双因素认证与分级高危操作门禁 (`internal/auth`)**：
   - **未启用 2FA 时强制锁定高危操作**：在「系统设置 → 双因素认证」完成绑定前，所有高危操作（独服/VPS 重启、系统重装、退订销毁、过户转移、IPMI 控制台、防火墙变更、API 密钥增删改、开启自动扣款或放弃撤回权、MCP L3 运维工具）均被安全门禁拦截（`403 TOTP_SETUP_REQUIRED`）。
   - **Strict / Sudo 双层 Step-Up 验证**：
     - **`strict` 级（每次必验）**：重装系统、服务销毁、过户转移、修改 API 密钥、关闭 2FA 等不可逆操作，每次请求均须输入最新 6 位动态码（内置 30 秒窗口防重放）或应急恢复码。
     - **`sudo` 级（5 分钟免重复输入）**：硬件重启、救援模式、IPMI 控制台、边缘防火墙、MCP 配置变更以及开启自动扣款/放弃撤回权，验证一次后授予当前会话 **5 分钟 Sudo 提权窗口**。
9. **欧盟 14 天消费者撤回权保护 (`waive_retractation_period = false`)**：
   - 采购引擎默认关闭 `auto_pay` 与 `waive_retractation_period`（默认保留欧盟消费者 14 天法定撤回权），仅当用户在具体任务上显式勾选并通过 2FA Sudo 验证后才会生效。
10. **日志自动脱敏与防搜索引擎收录 (`robots.txt`)**：
    - `log/slog` 日志中心自动对敏感凭据进行脱敏处理，内置 `robots.txt` 返回 `Disallow: /` 防止搜索引擎收录。

## 生产环境自托管加固清单

- [ ] 分别生成至少 24 字节的强随机 `API_SECRET_KEY`（`openssl rand -hex 24`）与独立的 32 字节 `DATA_ENCRYPTION_KEY`（`openssl rand -hex 32`）。
- [ ] 首次登录面板后，立即前往「API 与系统设置」绑定 **2FA 双因素认证**，并将 8 个一次性应急恢复码离线保存在密码管理器中。
- [ ] 在 `docker-compose.yml` 中将容器端口仅绑定在回环地址（`127.0.0.1:20000:8080`），并通过 Caddy、Nginx 或 Cloudflare Tunnel 提供 HTTPS 访问。
- [ ] 通过 HTTPS 反向代理访问时，在 `.env` 中设置 `COOKIE_SECURE=true` 并正确配置 `TRUSTED_PROXIES`。
- [ ] 收紧宿主机上的 `.env` 文件权限（`chmod 600 .env`）；容器启动脚本会自动将 `/data` 设为 `0700`（`PUID:PGID`，默认 `65532:65532`），`ovh-nexus` 会将 `/data/nexus.db` 设为 `0600`。
