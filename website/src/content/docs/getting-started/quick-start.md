---
title: Quick Start
description: Deploy OVH Nexus in 30 seconds using Docker Compose.
---

OVH Nexus is published as a pre-built multi-architecture Docker image (`linux/amd64` and `linux/arm64`) on Docker Hub at `elleryhe/ovh-nexus`—a hardened ~20 MB `alpine:3.24` container running a single static Go 1.27 binary on port `8080`. On boot, its entrypoint automatically initializes `/data` (`0700`) and drops privileges via `su-exec` to non-root `PUID:PGID` (default `65532:65532`).

## Prerequisites

- **Docker Engine** 20.10+ and **Docker Compose** v2 (`docker compose`).
- At least **128 MB RAM** and **256 MB disk space**.
- Outbound HTTPS (`443`) access to OVHcloud API endpoints (`*.api.ovh.com`, `api.us.ovhcloud.com`), and optionally `api.telegram.org` or your SMTP server for outbound notifications.

## 1. Create a Working Directory & `.env`

Create a directory for OVH Nexus and generate strong keys for `API_SECRET_KEY` and `DATA_ENCRYPTION_KEY`:

```bash
mkdir -p ovh-nexus/data && cd ovh-nexus

cat > .env << EOF
# Required: Console login password & loopback API key (at least 12 characters)
API_SECRET_KEY=$(openssl rand -hex 24)

# Required: Independent master key for AES-256-GCM at-rest secret encryption (at least 16 characters)
DATA_ENCRYPTION_KEY=$(openssl rand -hex 32)

# Optional: Set to true when serving behind an HTTPS reverse proxy
COOKIE_SECURE=false

# Optional: Default WebUI locale (en | fr | zh-CN)
DEFAULT_LOCALE=en

# Optional: Container timezone for schedules & notifications (IANA name, default UTC)
TZ=UTC

# Optional: Backend log level (DEBUG | INFO | WARNING | ERROR)
LOG_LEVEL=INFO

# Optional: Comma-separated trusted proxy IPs/CIDRs for X-Forwarded-For / X-Real-IP
TRUSTED_PROXIES=

# Optional: Non-root UID/GID for /data ownership and su-exec privilege drop (default 65532:65532)
PUID=65532
PGID=65532
EOF
```

:::caution[Strong Password, Independent Encryption Key & Automatic Volume Initialization]
- OVH Nexus validates both `API_SECRET_KEY` (>= 12 chars) and `DATA_ENCRYPTION_KEY` (>= 16 chars, must differ from `API_SECRET_KEY`) at startup. If either is missing or weak, the server refuses to start.
- No manual host `chown` on `./data` is required: the container entrypoint automatically creates `/data` (`0700`) and drops privileges in-place via `su-exec` to `PUID:PGID` (`65532:65532` by default, or your own host UID/GID for non-root Linux users). Kubernetes `runAsNonRoot: true` is also supported cleanly when started directly as a non-root UID.
:::

## 2. Create `docker-compose.yml`

```yaml
services:
  ovh-nexus:
    image: ${DOCKER_IMAGE:-elleryhe/ovh-nexus:latest}
    container_name: ovh-nexus
    ports:
      - "20000:8080"
    environment:
      - API_SECRET_KEY=${API_SECRET_KEY:?Please set a strong API_SECRET_KEY (at least 12 characters) in .env}
      - DATA_ENCRYPTION_KEY=${DATA_ENCRYPTION_KEY:?Please set an independent DATA_ENCRYPTION_KEY (at least 16 characters) in .env}
      - COOKIE_SECURE=${COOKIE_SECURE:-false}
      - DEFAULT_LOCALE=${DEFAULT_LOCALE:-en}
      - TZ=${TZ:-UTC}
      - LOG_LEVEL=${LOG_LEVEL:-INFO}
      - TRUSTED_PROXIES=${TRUSTED_PROXIES:-}
      - PUID=${PUID:-65532}
      - PGID=${PGID:-65532}
    volumes:
      - ./data:/data
    restart: unless-stopped
```

## 3. Start the Container

```bash
docker compose up -d
```

Verify that the container is healthy:

```bash
curl -s http://localhost:20000/api/health
# Expected output: {"status":"ok"}
```

## 4. Sign In to the Web Console

1. Open `http://localhost:20000` in your browser.
2. Enter the `API_SECRET_KEY` from your `.env` file (run `grep API_SECRET_KEY .env` to view it).
3. Switch languages (`EN` / `FR` / `中文`) or theme (`System` / `Light` / `Dark`) anytime from the login header or sidebar.

:::tip[No OVHcloud API Key Needed for Public Monitoring]
Immediately after logging in, you can use the **Live Availability Matrix**, **Public SKU Change Watcher**, and **Notification Center** without entering an OVHcloud API key. To manage purchased servers, track orders, or run automated purchases, add your credentials in [OVHcloud API Credentials](/configuration/ovh-api-credentials/).
:::
