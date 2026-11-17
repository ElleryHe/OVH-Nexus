---
title: 30 秒快速开始
description: 使用 Docker Compose 在 30 秒内部署并启动 OVH Nexus。
---

OVH Nexus 在 Docker Hub（`elleryhe/ovh-nexus`）上提供预构建的多架构安全加固镜像（约 `20 MB` 的 `alpine:3.24` 单二进制容器，监听 `8080` 端口；容器启动入口自动初始化 `/data` 目录权限并通过 `su-exec` 降权至非 Root UID/GID `PUID:PGID`，默认 `65532:65532`），原生支持 `linux/amd64`（x86_64）与 `linux/arm64`（Apple Silicon、AWS Graviton、Oracle Ampere、树莓派 4/5）。

## 环境要求

- **Docker Engine** 20.10+ 与 **Docker Compose** v2（`docker compose`）。
- 至少 **128 MB 内存**与 **256 MB 磁盘空间**。
- 宿主机可正常出站访问 OVHcloud API（`*.api.ovh.com`、`api.us.ovhcloud.com`），如需使用出站通知功能还需可访问 `api.telegram.org` 或您的 SMTP 邮件服务器。

## 1. 创建部署目录与 `.env` 配置文件

新建目录并生成高强度的 `API_SECRET_KEY` 与 `DATA_ENCRYPTION_KEY`：

```bash
mkdir -p ovh-nexus/data && cd ovh-nexus

cat > .env << EOF
# 必填：Web 控制台登录密码与本机 API 密钥（至少 12 位随机字符）
API_SECRET_KEY=$(openssl rand -hex 24)

# 必填：独立的静态数据加密主密钥（AES-256-GCM 落盘加密，至少 16 位字符，且不得与 API_SECRET_KEY 相同）
DATA_ENCRYPTION_KEY=$(openssl rand -hex 32)

# 可选：Web 控制台登录用户名，兼容浏览器与密码管理器自动填充（默认 Nexus，严格区分大小写）
ADMIN_USERNAME=Nexus

# 可选：若通过 HTTPS 反向代理访问请设为 true
COOKIE_SECURE=false

# 可选：默认界面语言 (en | fr | zh-CN)
DEFAULT_LOCALE=zh-CN

# 可选：容器时区（IANA 时区名称，默认 UTC）
TZ=UTC

# 可选：后端日志级别 (DEBUG | INFO | WARNING | ERROR)
LOG_LEVEL=INFO

# 可选：受信反向代理 IP/CIDR（逗号分隔），用于解析 X-Forwarded-For / X-Real-IP
TRUSTED_PROXIES=

# 可选：用于 /data 属主初始化与 su-exec 降权运行的非 Root UID/GID（默认 65532:65532）
PUID=65532
PGID=65532
EOF
```

:::caution[强口令、独立加密主密钥与自动目录权限初始化]
- OVH Nexus 在启动时会对 `API_SECRET_KEY`（不少于 12 位）与 `DATA_ENCRYPTION_KEY`（不少于 16 位，且不得与登录密码相同）进行严格校验。若未配置或使用示例占位符，进程将拒绝启动并提示错误。
- 无需在宿主机手动执行 `chown`：容器启动入口会自动初始化 `/data`（权限 `0700`）并经 `su-exec` 就地降权至 `PUID:PGID`（默认 `65532:65532`，Linux 非 Root 用户亦可按需调整 `.env` 中的 `PUID`/`PGID`）；同时兼容 Kubernetes `runAsNonRoot: true`。
:::

## 2. 创建 `docker-compose.yml`

```yaml
services:
  ovh-nexus:
    image: ${DOCKER_IMAGE:-elleryhe/ovh-nexus:latest}
    container_name: ovh-nexus
    ports:
      - "20000:8080"
    environment:
      - API_SECRET_KEY=${API_SECRET_KEY:?请在 .env 中设置至少 12 位的强口令 API_SECRET_KEY}
      - DATA_ENCRYPTION_KEY=${DATA_ENCRYPTION_KEY:?请在 .env 中设置至少 16 位的独立加密密钥 DATA_ENCRYPTION_KEY}
      - ADMIN_USERNAME=${ADMIN_USERNAME:-Nexus}
      - COOKIE_SECURE=${COOKIE_SECURE:-false}
      - DEFAULT_LOCALE=${DEFAULT_LOCALE:-zh-CN}
      - TZ=${TZ:-UTC}
      - LOG_LEVEL=${LOG_LEVEL:-INFO}
      - TRUSTED_PROXIES=${TRUSTED_PROXIES:-}
      - PUID=${PUID:-65532}
      - PGID=${PGID:-65532}
    volumes:
      - ./data:/data
    restart: unless-stopped
```

## 3. 启动容器

```bash
docker compose up -d
```

验证服务健康状态：

```bash
curl -s http://localhost:20000/api/health
# 预期输出: {"status":"ok"}
```

## 4. 登录 Web 控制台

1. 浏览器打开 `http://localhost:20000`。
2. 输入登录用户名 `Nexus`（或自定义的 `ADMIN_USERNAME`，严格区分大小写）与 `.env` 文件中的 `API_SECRET_KEY`（可通过 `grep API_SECRET_KEY .env` 查看）。
3. 您随时可以在登录页右上角或左侧导航栏底部切换界面语言（`EN` / `FR` / `中文`）与主题模式（`跟随系统` / `浅色` / `深色`）。

:::tip[免 API Key 即可使用公共监控]
登录后，即使尚未配置 OVHcloud API 密钥，您也可以立即使用**全球实时库存矩阵**、**公开货架 SKU 变动监控**与**多通道通知中心**。若需管理已购独服/VPS、查看订单交付进度或开启自动下单，请参阅 [OVHcloud API 凭据申请](/zh-cn/configuration/ovh-api-credentials/)。
:::
