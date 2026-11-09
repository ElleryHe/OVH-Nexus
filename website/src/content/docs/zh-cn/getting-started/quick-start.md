---
title: 30 秒快速开始
description: 使用 Docker Compose 在 30 秒内部署并启动 OVH Nexus。
---

OVH Nexus 在 Docker Hub（`elleryhe/ovh-nexus`）上提供预构建的多架构镜像，原生支持 `linux/amd64`（x86_64）与 `linux/arm64`（Apple Silicon、AWS Graviton、Oracle Ampere、树莓派 4/5）。

## 环境要求

- **Docker Engine** 20.10+ 与 **Docker Compose** v2（`docker compose`）。
- 至少 **256 MB 内存**与 **500 MB 磁盘空间**。
- 宿主机可正常出站访问 OVHcloud API（`*.api.ovh.com`、`api.us.ovhcloud.com`），如需使用通知功能还需可访问 `api.telegram.org` 或您的 SMTP 邮件服务器。

## 1. 创建部署目录与 `.env` 配置文件

新建目录并生成一个高强度的 `API_SECRET_KEY`（至少 12 位字符），该密钥即为您登录 Web 控制台的管理员密码：

```bash
mkdir -p ovh-nexus && cd ovh-nexus

cat > .env << EOF
# 必填：Web 控制台登录密码（至少 12 位随机字符）
API_SECRET_KEY=$(openssl rand -hex 24)

# 可选：默认界面语言 (en | fr | zh-CN)
DEFAULT_LOCALE=zh-CN

# 可选：若通过 HTTPS 反向代理访问请设为 true
COOKIE_SECURE=false
EOF
```

:::caution[强制强口令校验]
OVH Nexus 在启动时会对 `API_SECRET_KEY` 进行严格校验。如果未配置、长度不足 12 位，或者直接使用了文档中的示例占位符（如 `请替换为您自己的高强度密码`、`replace-with-a-strong-random-secret`、`123456`），容器将拒绝启动并提示错误。
:::

## 2. 创建 `docker-compose.yml`

```yaml
services:
  ovh-nexus:
    image: ${DOCKER_IMAGE:-elleryhe/ovh-nexus:latest}
    container_name: ovh-nexus
    ports:
      - "20000:80"
    environment:
      - DEBUG=false
      - ENABLE_API_KEY_AUTH=true
      - API_SECRET_KEY=${API_SECRET_KEY:?请在 .env 中设置至少 12 位的强口令 API_SECRET_KEY}
      - COOKIE_SECURE=${COOKIE_SECURE:-false}
      - DEFAULT_LOCALE=${DEFAULT_LOCALE:-zh-CN}
      - BIND_HOST=127.0.0.1
    volumes:
      - ./data:/app/backend/data
      - ./logs:/app/backend/logs
      - ./cache:/app/backend/cache
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
2. 输入 `.env` 文件中的 `API_SECRET_KEY`（可通过 `grep API_SECRET_KEY .env` 查看）。
3. 您随时可以在登录页右上角或左侧导航栏底部切换界面语言（`EN` / `FR` / `中文`）与主题模式（`跟随系统` / `浅色` / `深色`）。

:::tip[免 API Key 即可使用公共监控]
登录后，即使尚未配置 OVHcloud API 密钥，您也可以立即使用**全球实时库存矩阵**、**公开货架 SKU 变动监控**与**多通道通知中心**。若需管理已购独服/VPS、查看订单交付进度或开启自动抢购，请参阅 [OVHcloud API 凭据申请](/zh-cn/configuration/ovh-api-credentials/)。
:::
