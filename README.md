# OVH Nexus

<p align="center">
  <img src="./public/favicon.svg" width="72" height="72" alt="OVH Nexus Logo" />
</p>

<p align="center">
  <strong>OVHcloud Control &amp; Procurement Console</strong><br/>
  Unified Cloud Fleet Management, Live Order &amp; Delivery Tracking, Automated Bare-Metal / VPS Procurement, and Privacy-Shielded MCP AI Gateway.
</p>

<p align="center">
  <a href="./README.md"><strong>English</strong></a> ·
  <a href="./docs/README.fr.md">Français</a> ·
  <a href="./docs/README.zh-CN.md">简体中文</a>
</p>

<p align="center">
  <a href="https://ovh-nexus.ellery.me" target="_blank" rel="noopener noreferrer"><img src="https://img.shields.io/badge/Docs-ovh--nexus.ellery.me-0050D7?logo=read-the-docs&logoColor=white" alt="Documentation" /></a>
  <a href="https://hub.docker.com/r/elleryhe/ovh-nexus" target="_blank" rel="noopener noreferrer"><img src="https://img.shields.io/badge/Docker-elleryhe%2Fovh--nexus-2496ED?logo=docker&logoColor=white" alt="Docker Hub" /></a>
  <a href="./LICENSE"><img src="https://img.shields.io/badge/License-AGPL%20v3.0-blue.svg" alt="License: AGPL v3.0" /></a>
  <img src="https://img.shields.io/badge/i18n-EN%20%7C%20FR%20%7C%20%E4%B8%AD%E6%96%87-10B981" alt="Tri-Lingual UI" />
</p>

---

## Overview

**OVH Nexus** is a self-hosted, multi-account web console built for OVHcloud users (`ovh-eu`, `ovh-ca`, and `ovh-us`). Compiled as a **single static Go binary** (`~20 MB` non-root Alpine container with an embedded React SPA and pure-Go SQLite WAL storage), it unifies **day-to-day cloud infrastructure operations** with **real-time datacenter stock monitoring, public SKU change tracking, and automated bare-metal/VPS procurement**.

- **Dedicated Server & VPS Fleet Control**: Hardware inventory, MRTG traffic telemetry, IPMI/KVM console, automated OS reinstallation with custom RAID/ZFS & SSH key injection, IP Edge Firewall & permanent Anti-DDoS mitigation, and VPS lifecycle control.
- **Orders, Delivery & Renewal Matrix**: Live 4-stage order delivery tracking (`CREATING` → `VALIDATING` → `DELIVERING` → `INSTALLATION`) and unified service expiration countdowns across all accounts.
- **Live Availability Matrix, Presale Detection & Auto-Order Queue**: Real-time stock across global datacenters (`🇨🇦 BHS`, `🇨🇦 TOR`, `🇫🇷 GRA`, `🇩🇪 FRA`, `🇸🇬 SGP`, etc.) enriched with commercial model names (`KS-A`, `RISE-1`, `SCALE-a1`), readable hardware specs, exact configuration-level monthly pricing, automatic **Presale / Unlisted** SKU detection (`2024`–`2027` generation tags + `Presale Only` filter), and a one-click **Quick Monitor** modal linked directly to the Auto-Order Queue.
- **Public SKU Watcher & Outbound Notification Center**: Baseline-silent tracking (`baseline: true`) of newly racked presale models and `presale -> listed` public catalog launches without an API key, dispatching alerts via multi-instance **Telegram Bots** and **SMTP Email** across 8 fine-grained events with localized templates (`en` / `fr` / `zh-CN`).
- **Real-Time Observability & SSE Log Console (`/logs`)**: 100% standardized English structured logging (`log/slog`) with a 10,000-row SQLite ring buffer, Server-Sent Events live streaming, module/level/keyword filters, secret redaction, and `.jsonl`/`.log` export.
- **MCP AI Gateway & Privacy Sandbox (`/api/mcp`)**: Connect Claude Code, Claude Desktop, Cursor, or Antigravity via Streamable HTTP (JSON-RPC 2.0) with bidirectional alias obfuscation (`bm-01`, `vps-01`, `ord-01`, masked IP tails `*.*.*.42`) and a strict **two-step unpaid-order guardrail** (`auto_pay=false` hardcoded).
- **Hardened Single-Binary Security**: Native Slowloris timeouts, `HKDF-SHA256` + `AES-256-GCM` at-rest credential encryption in SQLite (`/data/nexus.db`), RFC 6238 TOTP 2FA (`strict` / `sudo` gatekeeper), and trusted-proxy rate limiting.

📚 **Full Documentation**: <a href="https://ovh-nexus.ellery.me" target="_blank" rel="noopener noreferrer"><strong>https://ovh-nexus.ellery.me</strong></a>

---

## Quick Start (Docker Compose)

OVH Nexus is distributed as a hardened multi-arch Docker image (`elleryhe/ovh-nexus:latest`) containing a single static Go binary (`alpine:3.24`, listening on port `8080`, automatically initializing `/data` and dropping privileges via `su-exec` to non-root `PUID:PGID`, default `65532:65532`). No Git clone is required to deploy.

### 1. Create a Working Directory & `.env`

```bash
mkdir -p ovh-nexus/data && cd ovh-nexus

cat > .env << EOF
# Required: Console login password / loopback API secret key (at least 12 characters)
API_SECRET_KEY=$(openssl rand -hex 24)

# Required: Independent master key for AES-256-GCM at-rest secret encryption (at least 16 characters)
DATA_ENCRYPTION_KEY=$(openssl rand -hex 32)

# Optional: Set to true when serving behind an HTTPS reverse proxy
COOKIE_SECURE=false

# Optional: Default WebUI locale (en | fr | zh-CN)
DEFAULT_LOCALE=en

# Optional: Container timezone for schedules & notification timestamps (default UTC)
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

> **Note**: No manual host `chown` on `./data` is required. On container boot, the entrypoint automatically initializes `/data` (`0700`) and drops privileges in-place via `su-exec` to `PUID:PGID` (`65532:65532` by default, or any host UID/GID you set for non-root Linux users). Kubernetes `runAsNonRoot: true` is also supported cleanly when started directly as a non-root UID.

### 2. Create `docker-compose.yml`

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

### 3. Launch the Container

```bash
docker compose up -d
```

Open `http://localhost:20000` and sign in with the `API_SECRET_KEY` generated in your `.env` file (`grep API_SECRET_KEY .env`).

---

## Documentation Links

| Guide | Link |
| :--- | :--- |
| **Quick Start & Installation** | <a href="https://ovh-nexus.ellery.me/getting-started/quick-start/" target="_blank" rel="noopener noreferrer">ovh-nexus.ellery.me/getting-started/quick-start/</a> |
| **Reverse Proxy & HTTPS (`COOKIE_SECURE`)** | <a href="https://ovh-nexus.ellery.me/getting-started/reverse-proxy/" target="_blank" rel="noopener noreferrer">ovh-nexus.ellery.me/getting-started/reverse-proxy/</a> |
| **Environment Variables & OVH API Keys** | <a href="https://ovh-nexus.ellery.me/configuration/environment-variables/" target="_blank" rel="noopener noreferrer">ovh-nexus.ellery.me/configuration/environment-variables/</a> |
| **Feature Guides (Fleet, Stock, SKU Watcher, Notifications)** | <a href="https://ovh-nexus.ellery.me/features/dedicated-servers/" target="_blank" rel="noopener noreferrer">ovh-nexus.ellery.me/features/dedicated-servers/</a> |
| **Security Best Practices & Troubleshooting** | <a href="https://ovh-nexus.ellery.me/operations/security/" target="_blank" rel="noopener noreferrer">ovh-nexus.ellery.me/operations/security/</a> |

---

## License & Disclaimer

Licensed under the **GNU Affero General Public License v3.0 (AGPL-3.0)**. See [LICENSE](./LICENSE), [CHANGELOG.md](./CHANGELOG.md), [CONTRIBUTING.md](./CONTRIBUTING.md), and [SECURITY.md](./SECURITY.md).

> **OVH Nexus** is an independent open-source project and is **not affiliated with, endorsed by, or sponsored by OVH SAS / OVHcloud**. "OVH" and "OVHcloud" are registered trademarks of OVH SAS.
