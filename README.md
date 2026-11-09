# OVH Nexus

<p align="center">
  <img src="./public/favicon.svg" width="72" height="72" alt="OVH Nexus Logo" />
</p>

<p align="center">
  <strong>OVHcloud Control &amp; Procurement Console</strong><br/>
  Unified Cloud Fleet Management, Live Order &amp; Delivery Tracking, and Automated Bare-Metal / VPS Procurement.
</p>

<p align="center">
  <a href="./README.md"><strong>English</strong></a> ·
  <a href="./docs/README.fr.md">Français</a> ·
  <a href="./docs/README.zh-CN.md">简体中文</a>
</p>

<p align="center">
  <a href="https://ovh-nexus.ellery.me"><img src="https://img.shields.io/badge/Docs-ovh--nexus.ellery.me-0050D7?logo=read-the-docs&logoColor=white" alt="Documentation" /></a>
  <a href="https://hub.docker.com/r/elleryhe/ovh-nexus"><img src="https://img.shields.io/badge/Docker-elleryhe%2Fovh--nexus-2496ED?logo=docker&logoColor=white" alt="Docker Hub" /></a>
  <img src="https://img.shields.io/badge/License-AGPL%20v3.0-blue.svg" alt="License: AGPL v3.0" />
  <img src="https://img.shields.io/badge/i18n-EN%20%7C%20FR%20%7C%20%E4%B8%AD%E6%96%87-10B981" alt="Tri-Lingual UI" />
</p>

---

## Overview

**OVH Nexus** is a self-hosted, multi-account web console built for OVHcloud users (`ovh-eu`, `ovh-ca`, and `ovh-us`). It unifies **day-to-day cloud infrastructure operations** with **real-time datacenter stock monitoring, public SKU change tracking, and automated bare-metal/VPS procurement** in a single container.

- **Dedicated Server & VPS Fleet Control**: Hardware inventory, MRTG traffic telemetry, IPMI/KVM console, automated OS reinstallation with custom RAID/ZFS & SSH key injection, IP Edge Firewall & permanent Anti-DDoS mitigation, and VPS lifecycle control.
- **Orders, Delivery & Renewal Matrix**: Live 4-stage order delivery tracking (`CREATING` → `VALIDATING` → `DELIVERING` → `INSTALLATION`) and unified service expiration countdowns across all accounts.
- **Live Availability Matrix & Auto-Order Queue**: Real-time stock across global datacenters enriched with commercial model names (`KS-A`, `RISE-1`, `SCALE-a1`), readable hardware specs, and exact configuration-level monthly pricing.
- **Public SKU Watcher & Pluggable Notification Center**: Track newly racked or removed server SKUs without an API key, and dispatch alerts via multi-instance **Telegram Bots** and **SMTP Email** across 7 fine-grained events.

📚 **Full Documentation**: **[https://ovh-nexus.ellery.me](https://ovh-nexus.ellery.me)**

---

## 30-Second Quick Start

```bash
# 1. Create environment configuration
cp .env.example .env
# Edit .env and set a strong API_SECRET_KEY (at least 12 characters, e.g. openssl rand -hex 24)

# 2. Launch with Docker Compose
docker compose up -d
```

Open `http://localhost:20000` and sign in with your `API_SECRET_KEY`.

---

## Documentation Links

| Guide | Link |
| :--- | :--- |
| **Quick Start & Installation** | [ovh-nexus.ellery.me/getting-started/quick-start/](https://ovh-nexus.ellery.me/getting-started/quick-start/) |
| **Reverse Proxy & HTTPS (`COOKIE_SECURE`)** | [ovh-nexus.ellery.me/getting-started/reverse-proxy/](https://ovh-nexus.ellery.me/getting-started/reverse-proxy/) |
| **Environment Variables & OVH API Keys** | [ovh-nexus.ellery.me/configuration/environment-variables/](https://ovh-nexus.ellery.me/configuration/environment-variables/) |
| **Feature Guides (Fleet, Stock, SKU Watcher, Notifications)** | [ovh-nexus.ellery.me/features/dedicated-servers/](https://ovh-nexus.ellery.me/features/dedicated-servers/) |
| **Security Best Practices & Troubleshooting** | [ovh-nexus.ellery.me/operations/security/](https://ovh-nexus.ellery.me/operations/security/) |

---

## License & Disclaimer

Licensed under the **GNU Affero General Public License v3.0 (AGPL-3.0)**. See [LICENSE](./LICENSE), [CONTRIBUTING.md](./CONTRIBUTING.md), and [SECURITY.md](./SECURITY.md).

> **OVH Nexus** is an independent open-source project and is **not affiliated with, endorsed by, or sponsored by OVH SAS / OVHcloud**. "OVH" and "OVHcloud" are registered trademarks of OVH SAS.
