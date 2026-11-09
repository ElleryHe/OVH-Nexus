---
title: Installation & Upgrade
description: Detailed installation options, version tags, multi-arch support, and upgrade instructions.
---

## Official Docker Hub Images

Official container images are built automatically by GitHub Actions and published to [`elleryhe/ovh-nexus`](https://hub.docker.com/r/elleryhe/ovh-nexus) for both `linux/amd64` (x86_64) and `linux/arm64` (Apple Silicon, AWS Graviton, Raspberry Pi 4/5, Oracle Ampere).

### Available Image Tags

| Tag Pattern | Example | Description |
| :--- | :--- | :--- |
| `latest` | `elleryhe/ovh-nexus:latest` | Latest stable release built from the `main` branch. |
| `MAJOR.MINOR.PATCH` | `elleryhe/ovh-nexus:1.0.2` | Pinned immutable semantic version release. |
| `MAJOR.MINOR` | `elleryhe/ovh-nexus:1.0` | Tracks the latest patch within the `1.0.x` series. |

To pin a specific version in `docker-compose.yml`, set `DOCKER_IMAGE` in your `.env` file:

```ini
DOCKER_IMAGE=elleryhe/ovh-nexus:1.0.2
```

## Upgrading OVH Nexus

Because all persistent state is stored in `./data`, `./logs`, and `./cache` on the host, upgrading takes only a few seconds and preserves your accounts, notification channels, queue tasks, and SKU history:

```bash
# 1. Pull the latest image
docker compose pull

# 2. Recreate the container
docker compose up -d
```

:::note[Automatic Schema Migration]
When upgrading from an earlier configuration format (for example, legacy `tgToken` and `tgChatId` fields in `config.json`), `NotificationHub` automatically migrates them into a structured Telegram channel on startup while preserving backward compatibility.
:::

## Building from Source

If you prefer to build the container image locally or customize the source code:

```bash
git clone https://github.com/ElleryHe/OVH-Nexus.git
cd OVH-Nexus

# Copy and configure environment variables
cp .env.example .env
# Edit .env and set API_SECRET_KEY

# Build and start
docker build -t elleryhe/ovh-nexus:local .
DOCKER_IMAGE=elleryhe/ovh-nexus:local docker compose up -d
```

## Binding Port Only to Localhost

If OVH Nexus runs on a public VPS alongside a local reverse proxy (Nginx or Caddy), bind host port `20000` exclusively to loopback (`127.0.0.1`) in `docker-compose.yml` so port `20000` is not directly reachable from the public internet:

```yaml
    ports:
      - "127.0.0.1:20000:80"
```
