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
| `MAJOR.MINOR.PATCH` | `elleryhe/ovh-nexus:1.3.0` | 固定语义化版本号镜像，适合对稳定性要求极高的生产环境锁定版本。 |
| `MAJOR.MINOR` | `elleryhe/ovh-nexus:1.3` | 自动跟随 `1.3.x` 系列的最新补丁版本。 |

如需固定镜像版本，只需在 `.env` 中指定 `DOCKER_IMAGE`：

```ini
DOCKER_IMAGE=elleryhe/ovh-nexus:1.3.0
```

## 平滑升级容器

由于所有持久化数据均集中存储在单一挂载卷 `./data:/data` 中的 SQLite WAL 数据库（`/data/nexus.db`）内，升级镜像仅需数秒，且完整保留您的账户凭据、2FA 配置、通知渠道、采购队列、日志与 SKU 历史记录：

```bash
# 1. 拉取最新镜像
docker compose pull

# 2. 重新创建并启动容器
docker compose up -d
```

:::note[自动非 Root 初始化 (`su-exec`、`PUID`/`PGID`)]
容器启动时，`/usr/local/bin/docker-entrypoint.sh` 会自动初始化 `/data` 目录（权限 `0700`）并通过 `su-exec` 就地降权至 `PUID:PGID`（默认 `65532:65532`），无需在宿主机手动执行 `chown` 修改 `./data` 权限。Linux 非 Root 宿主机用户可在 `.env` 中按需指定 `PUID`/`PGID`；在 Kubernetes `runAsNonRoot: true` 模式下亦可直接平滑运行。
:::

## 从源码本地构建

如果您希望自行编译三阶段静态 Go 镜像（`node:24-alpine` + `golang:1.27-alpine` -> `alpine:3.24`）或进行二次开发：

```bash
git clone https://github.com/ElleryHe/OVH-Nexus.git
cd OVH-Nexus

# 复制并配置环境变量
cp .env.example .env
# 编辑 .env 并填入高强度 API_SECRET_KEY 与 DATA_ENCRYPTION_KEY

# 本地构建并启动（启动脚本将自动初始化 ./data 目录权限）
docker build -t elleryhe/ovh-nexus:local .
DOCKER_IMAGE=elleryhe/ovh-nexus:local docker compose up -d
```

## 仅监听本机回环地址

如果 OVH Nexus 部署在公网服务器上并配合本机反向代理（Caddy 或 Nginx）使用，建议在 `docker-compose.yml` 中将端口绑定限制在 `127.0.0.1`，避免 `20000` 端口直接暴露到公网：

```yaml
    ports:
      - "127.0.0.1:20000:8080"
```
