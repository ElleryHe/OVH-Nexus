---
title: 环境变量参考
description: OVH Nexus 支持的所有环境变量完整说明与安全配置规范。
---

OVH Nexus 在容器启动时通过 `.env` 文件或 `docker-compose.yml` 中的 `environment` 读取环境变量，并由 `server/internal/config` 在监听端口前执行严格的 Fail-Fast 启动校验。

## 环境变量一览表

| 变量名 | 必填 | 默认值 | 说明 |
| :--- | :---: | :--- | :--- |
| `API_SECRET_KEY` | **是** | *(无)* | Web 控制台登录密码及本机回环 API 密钥。**长度必须不少于 12 位**，且不得使用文档中的示例占位符或常见弱口令。 |
| `DATA_ENCRYPTION_KEY` | **是** | *(无)* | 独立的静态数据加密主密钥（`HKDF-SHA256` + `AES-256-GCM`），用于加密 `/data/nexus.db` 中的所有 OVH API 密钥、Telegram Token、SMTP 密码与 TOTP 种子。**长度必须不少于 16 位**，且**严禁与 `API_SECRET_KEY` 相同**。 |
| `ADMIN_USERNAME` | 否 | `Nexus` | Web 控制台登录用户名，配合标准 `username` + `current-password` 表单实现浏览器与密码管理器（1Password、Bitwarden、Apple 密码）自动保存与填充。与 `API_SECRET_KEY` 一同执行恒定时间比对，**严格区分大小写**（兼容别名 `NEXUS_USERNAME`）。 |
| `PORT` | 否 | `8080` | 容器内 Go HTTP 服务监听的 TCP 端口（`docker-compose.yml` 默认映射为宿主机 `20000:8080`）。 |
| `DATA_DIR` | 否 | `/data` | SQLite WAL 数据库（`nexus.db`）的存储目录（目录权限 `0700`，数据库文件权限 `0600`）。 |
| `TZ` | 否 | `UTC` | 容器时区（IANA 名称，通过 `time.LoadLocation` 作用于后台调度任务与出站通知时间戳，如 `UTC`、`Europe/Paris`、`Asia/Shanghai`）。 |
| `COOKIE_SECURE` | 否 | `false` | 是否为 `ovh_session` 会话 Cookie 启用 `Secure` 属性。通过 HTTPS 反向代理访问时请设为 `true`。 |
| `DEFAULT_LOCALE` | 否 | `en` | 首次访问 Web 控制台时的默认界面语言（可选 `en`、`fr`、`zh-CN`）。用户也可随时在界面内手动切换。 |
| `LOG_LEVEL` | 否 | `INFO` | 后端最小日志输出级别（`DEBUG`、`INFO`、`WARNING` 或 `ERROR`）。 |
| `LOG_FORMAT` | 否 | `text` | 容器标准输出日志格式（`text` 或 `json`）。无论标准输出格式如何，数据库与 SSE 控制台均提供完整结构化检索。 |
| `TRUSTED_PROXIES` | 否 | *(空)* | 逗号分隔的受信反向代理 IP 或 CIDR 列表（如 `127.0.0.1,172.16.0.0/12`），仅来自受信代理的请求才会采信 `X-Forwarded-For` 与 `X-Real-IP`。 |
| `PUID` | 否 | `65532` | 容器启动入口在初始化 `/data`（`0700`）后通过 `su-exec` 降权切换的目标数字 UID。可按需设为 Linux 非 Root 宿主机用户 UID（`id -u`）；若容器本身已以非 Root 启动（`id -u != 0`，如 Kubernetes `runAsNonRoot: true`）则直接保留当前运行身份。 |
| `PGID` | 否 | `65532` | 容器启动入口在初始化 `/data`（`0700`）后通过 `su-exec` 降权切换的目标数字 GID。可按需设为与宿主机用户一致的 GID（`id -g`）。 |
| `SECRET_KEY` | 否 | *(自动生成)* | 可选的 `ovh_session` HMAC-SHA256 会话签名覆盖密钥。若省略，系统首次启动时自动生成随机密钥并加密持久化在 `/data/nexus.db`（`kv_settings`）。 |
| `CORS_ORIGINS` | 否 | `http://localhost:19999,http://127.0.0.1:19999` | 前后端分离本地开发时的跨域白名单（逗号分隔）。生产单二进制部署下前后端同源，无需设置。 |
| `DOCKER_IMAGE` | 否 | `elleryhe/ovh-nexus:latest` | 供 `docker-compose.yml` 覆盖镜像版本标签（如 `elleryhe/ovh-nexus:1.3.0`）。 |

## 生成高强度密钥

使用 `openssl` 生成两把相互独立的高熵随机密钥：

```bash
# API_SECRET_KEY（48 位十六进制随机字符串）
openssl rand -hex 24

# DATA_ENCRYPTION_KEY（64 位十六进制随机字符串，严禁与 API_SECRET_KEY 相同）
openssl rand -hex 32
```

## 内置拦截的弱密钥与示例占位符

为防止直接复制示例值上线造成安全风险，`server/internal/config` 会拦截过短、两者相同或命中以下常见占位符的配置并拒绝启动：

- `123456`、`123456789012`、`1234567890123456`、`password`、`changeme`、`secret`、`admin123456`
- `ovh-nexus-default-secret-key`、`ovh-nexus-default-encryption-key`
- `replace-with-a-strong-random-secret`、`replace-with-a-strong-encryption-key`
- `votre-cle-secrete-robuste`、`encryption-key-example`
- `请替换为您自己的高强度密码`、`请替换为您自己的高强度加密密钥`、`你得替换为你自己的密钥`
