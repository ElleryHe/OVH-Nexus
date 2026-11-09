---
title: Quick Start
description: Deploy OVH Nexus in 30 seconds using Docker Compose.
---

OVH Nexus is published as a pre-built multi-architecture Docker image (`linux/amd64` and `linux/arm64`) on Docker Hub at `elleryhe/ovh-nexus`.

## Prerequisites

- **Docker Engine** 20.10+ and **Docker Compose** v2 (`docker compose`).
- At least **256 MB RAM** and **500 MB disk space**.
- Outbound HTTPS (`443`) access to OVHcloud API endpoints (`*.api.ovh.com`, `api.us.ovhcloud.com`), and optionally `api.telegram.org` or your SMTP server for notifications.

## 1. Create a Working Directory & `.env`

Create a directory for OVH Nexus and generate a strong `API_SECRET_KEY` (at least 12 characters). This key serves as your WebUI login password.

```bash
mkdir -p ovh-nexus && cd ovh-nexus

cat > .env << EOF
# Required: Console login password (at least 12 characters)
API_SECRET_KEY=$(openssl rand -hex 24)

# Optional: Default WebUI locale (en | fr | zh-CN)
DEFAULT_LOCALE=en

# Optional: Set to true when serving behind an HTTPS reverse proxy
COOKIE_SECURE=false
EOF
```

:::caution[Strong Password Required]
OVH Nexus validates `API_SECRET_KEY` at startup. If it is empty, shorter than 12 characters, or matches a known placeholder (such as `replace-with-a-strong-random-secret`, `123456`, or `password`), the backend will refuse to start.
:::

## 2. Create `docker-compose.yml`

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
      - API_SECRET_KEY=${API_SECRET_KEY:?Please set a strong API_SECRET_KEY (at least 12 characters) in .env}
      - COOKIE_SECURE=${COOKIE_SECURE:-false}
      - DEFAULT_LOCALE=${DEFAULT_LOCALE:-en}
      - BIND_HOST=127.0.0.1
    volumes:
      - ./data:/app/backend/data
      - ./logs:/app/backend/logs
      - ./cache:/app/backend/cache
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
