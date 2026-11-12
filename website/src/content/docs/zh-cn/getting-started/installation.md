---
title: 安装、版本与平滑升级
description: Docker 镜像版本标签说明、多架构支持、平滑升级与源码构建指南。
---

## 官方 Docker Hub 镜像与版本标签

官方镜像由 GitHub Actions 自动构建并发布至 [`elleryhe/ovh-nexus`](https://hub.docker.com/r/elleryhe/ovh-nexus)，同时支持 `linux/amd64` 与 `linux/arm64` 架构。

### 镜像标签策略

| 标签格式 | 示例 | 说明 |
| :--- | :--- | :--- |
| `latest` | `elleryhe/ovh-nexus:latest` | `main` 分支最新发布的稳定版镜像。 |
| `MAJOR.MINOR.PATCH` | `elleryhe/ovh-nexus:1.1.1` | 固定语义化版本号镜像，适合对稳定性要求极高的生产环境锁定版本。 |
| `MAJOR.MINOR` | `elleryhe/ovh-nexus:1.1` | 自动跟随 `1.1.x` 系列的最新补丁版本。 |

如需固定镜像版本，只需在 `.env` 中指定 `DOCKER_IMAGE`：

```ini
DOCKER_IMAGE=elleryhe/ovh-nexus:1.1.1
```

## 平滑升级容器

由于所有持久化数据均保存在宿主机的 `./data`、`./logs` 和 `./cache` 挂载目录中，升级镜像不会丢失任何账户凭据、通知渠道、监控队列或历史记录：

```bash
# 1. 拉取最新镜像
docker compose pull

# 2. 重新创建并启动容器
docker compose up -d
```

:::note[自动配置结构迁移]
当从旧版本升级时，`NotificationHub` 会在启动时自动检测旧版字段（例如 `config.json` 中的 `tgToken` 与 `tgChatId`），并将其无损迁移为标准的多通道 Telegram 通知实例，无需手动重填。
:::

## 从源码本地构建

如果您希望自行编译镜像或进行二次开发：

```bash
git clone https://github.com/ElleryHe/OVH-Nexus.git
cd OVH-Nexus

# 复制并配置环境变量
cp .env.example .env
# 编辑 .env 并填入高强度 API_SECRET_KEY

# 本地构建并启动
docker build -t elleryhe/ovh-nexus:local .
DOCKER_IMAGE=elleryhe/ovh-nexus:local docker compose up -d
```

## 仅监听本机回环地址

如果 OVH Nexus 部署在公网服务器上并配合本机反向代理（Nginx 或 Caddy）使用，建议在 `docker-compose.yml` 中将端口绑定限制在 `127.0.0.1`，避免 `20000` 端口直接暴露到公网：

```yaml
    ports:
      - "127.0.0.1:20000:80"
```
