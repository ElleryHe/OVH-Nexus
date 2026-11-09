---
title: 环境变量参考
description: OVH Nexus 支持的所有环境变量完整说明与安全配置规范。
---

OVH Nexus 在容器启动时通过 `.env` 文件或 `docker-compose.yml` 中的 `environment` 读取环境变量配置。

## 环境变量一览表

| 变量名 | 必填 | 默认值 | 说明 |
| :--- | :---: | :--- | :--- |
| `API_SECRET_KEY` | **是** | *(无)* | Web 控制台登录密码及本机回环脚本 API 密钥。**长度必须不少于 12 位**，且不得使用文档中的示例占位符或常见弱口令。 |
| `DEFAULT_LOCALE` | 否 | `en` | 首次访问 Web 控制台时的默认界面语言（可选 `en`、`fr`、`zh-CN`）。用户也可随时在界面内手动切换。 |
| `COOKIE_SECURE` | 否 | `false` | 是否为 `ovh_session` 会话 Cookie 启用 `Secure` 属性。通过 HTTPS 反向代理访问时请设为 `true`。 |
| `SECRET_KEY` | 否 | *(自动生成)* | Flask 会话签名密钥。若未显式设置，首次启动时会自动生成 32 字节强随机密钥并持久化保存在 `/app/backend/data/session_secret`（权限 `0600`）。 |
| `ENABLE_API_KEY_AUTH` | 否 | `true` | 是否启用身份认证。注意：在生产模式（`DEBUG=false`）下，即使设为 `false` 也会被忽略并强制启用认证。 |
| `DEBUG` | 否 | `false` | 是否开启 Flask 调试模式。**生产环境请始终保持 `false`。** |
| `BIND_HOST` | 否 | `127.0.0.1` | 容器内部 Python 后端监听的网卡地址（默认仅监听 `127.0.0.1`，仅供容器内 Nginx 反代调用）。 |
| `PORT` | 否 | `19998` | 容器内 Python Flask 后端监听端口（由容器内 Nginx 在 80 端口统一代理）。 |
| `CORS_ORIGINS` | 否 | `http://localhost:19999,http://127.0.0.1:19999` | 前后端分离本地开发时的跨域白名单（逗号分隔）。Docker 生产部署下前后端同源，无需修改。 |
| `DOCKER_IMAGE` | 否 | `elleryhe/ovh-nexus:latest` | 供 `docker-compose.yml` 覆盖镜像版本标签（如 `elleryhe/ovh-nexus:1.0.1`）。 |

## 生成高强度 `API_SECRET_KEY`

您可以使用以下任一命令生成高熵随机密钥：

```bash
# 使用 OpenSSL 生成 48 位十六进制随机字符串
openssl rand -hex 24

# 使用 Python 生成 URL 安全随机字符串
python3 -c "import secrets; print(secrets.token_urlsafe(24))"
```

## 内置拦截的弱密钥与示例占位符

为防止用户直接复制 README 示例上线导致未授权访问，`backend/api_key_config.py` 会拦截长度不足 12 位或命中以下列表的密钥并拒绝启动：

- `123456`、`password`、`changeme`
- `ovh-nexus-default-secret-key`
- `replace-with-a-strong-random-secret`
- `votre-cle-secrete-robuste`
- `请替换为您自己的高强度密码`、`你得替换为你自己的密钥`
