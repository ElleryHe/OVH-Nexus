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
| `MAJOR.MINOR.PATCH` | `elleryhe/ovh-nexus:1.3.0` | Pinned immutable semantic version release. |
| `MAJOR.MINOR` | `elleryhe/ovh-nexus:1.3` | Tracks the latest patch within the `1.3.x` series. |

To pin a specific version in `docker-compose.yml`, set `DOCKER_IMAGE` in your `.env` file:

```ini
DOCKER_IMAGE=elleryhe/ovh-nexus:1.3.0
```

## Upgrading OVH Nexus

Because all persistent state lives in the single SQLite WAL database inside `./data:/data` (`/data/nexus.db`), upgrading takes only a few seconds and preserves your accounts, 2FA configuration, notification channels, queue tasks, logs, and SKU history:

```bash
# 1. Pull the latest image
docker compose pull

# 2. Recreate the container
docker compose up -d
```

:::note[Automatic Non-Root Initialization (`su-exec`, `PUID`/`PGID`)]
On container boot, `/usr/local/bin/docker-entrypoint.sh` automatically initializes `/data` (`0700`) and drops privileges in-place via `su-exec` to `PUID:PGID` (default `65532:65532`). No manual `chown` on `./data` is required. Non-root Linux host users can optionally set `PUID`/`PGID` in `.env`, and Kubernetes `runAsNonRoot: true` is supported seamlessly when started directly as a non-root UID.
:::

## Building from Source

If you prefer to build the multi-stage static Go container image locally (`node:24-alpine` + `golang:1.27-alpine` -> `alpine:3.24`):

```bash
git clone https://github.com/ElleryHe/OVH-Nexus.git
cd OVH-Nexus

# Copy and configure environment variables
cp .env.example .env
# Edit .env and set API_SECRET_KEY and DATA_ENCRYPTION_KEY

# Build and start (entrypoint initializes ./data automatically)
docker build -t elleryhe/ovh-nexus:local .
DOCKER_IMAGE=elleryhe/ovh-nexus:local docker compose up -d
```

## Binding Port Only to Localhost

If OVH Nexus runs on a public VPS alongside a local reverse proxy (Caddy or Nginx), bind host port `20000` exclusively to loopback (`127.0.0.1`) in `docker-compose.yml` so port `20000` is not directly reachable from the public internet:

```yaml
    ports:
      - "127.0.0.1:20000:8080"
```
