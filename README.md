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
  <img src="https://img.shields.io/badge/License-AGPL%20v3.0-blue.svg" alt="License: AGPL v3.0" />
  <img src="https://img.shields.io/badge/Docker-elleryhe%2Fovh--nexus-2496ED?logo=docker&logoColor=white" alt="Docker Hub" />
  <img src="https://img.shields.io/badge/i18n-EN%20%7C%20FR%20%7C%20%E4%B8%AD%E6%96%87-10B981" alt="Tri-Lingual UI" />
</p>

---

## Overview

**OVH Nexus** is a self-hosted, multi-account web console built for OVHcloud users (`OVH Europe`, `OVH North America / CA`, and `OVH US`). It combines **day-to-day cloud infrastructure operations** with **real-time stock monitoring and automated server procurement** in a single container.

### Core Capabilities

1. **Dedicated Server Fleet Control (`/dedicated/server`)**
   - Hardware inventory, network/bandwidth specifications, MRTG real-time traffic telemetry, and IPMI/KVM remote access (serial-over-LAN, KVM applet, xterm.js).
   - Automated OS reinstallation with custom hardware/software RAID, Proxmox ZFS profiles, and **one-click SSH public key injection (`/me/sshKey`)**.
   - **IP Edge Firewall & Anti-DDoS Mitigation (`/ip/{ip}/firewall` & `/mitigation`)**: Manage edge firewall rules (`permit`/`deny`, TCP/UDP/ICMP) and toggle permanent Anti-DDoS mitigation directly from the console.
2. **Purchased VPS Fleet Control (`/vps`)**
   - Inspect purchased VPS instances across zones, vCore/RAM/Disk allocations, IPv4/IPv6 addresses, service states, and trigger instant reboots (`/vps/{serviceName}/reboot`).
3. **Order & Live Delivery Pipeline (`/me/order` & `/followUp`)**
   - Track recent OVHcloud orders, retrieve payment & invoice links, and monitor the 4-stage hardware delivery pipeline (`CREATING` → `VALIDATING` → `DELIVERING` → `INSTALLATION`) in real time.
4. **Services Expiration & Auto-Renewal Matrix (`/serviceInfos`)**
   - Unified expiration countdown (`<= 7d` critical, `<= 30d` warning, `> 30d` healthy) and renewal mode overview (`Auto` / `Manual` / `Terminate at expiry`) across all Dedicated Servers and VPS instances.
5. **Real-Time Datacenter Stock, SKU Change Watcher & Multi-Channel Notification Hub**
   - Live availability matrix across global OVHcloud datacenters (`GRA`, `SBG`, `RBX`, `LIM`, `FRA`, `LON`, `WAW`, `BHS`, `SGP`, `SYD`, `VIN`, `HIL`) with **consumer-friendly commercial model names (`KS-A`, `RISE-1`, `SCALE-a1`), readable hardware specs, and exact configuration-level monthly pricing** merged from OVHcloud public Eco & Baremetal catalogs.
   - **Public SKU Change Watcher**: Automatically tracks newly racked or removed SKUs and hardware configurations across OVHcloud shelves (with 2-round removal verification and anti-glitch protection) without requiring an OVH API key.
   - **Pluggable Notification Center**: Multi-instance **Telegram Bot** and **SMTP Email** (`SSL/TLS`, `STARTTLS`, `Plain`) channels subscribing to 7 fine-grained events (`stock.available`, `stock.unavailable`, `sku.changed`, `order.success`, `order.failed`, `order.delivered`, `service.expiring`).
6. **Built-in Tri-Lingual Console (`en` / `fr` / `zh-CN`)**
   - Switch between **English**, **Français**, and **简体中文** at any time in the WebUI, or set the default language via `DEFAULT_LOCALE`.

---

## Quick Start (Docker Compose)

OVH Nexus is distributed as an all-in-one Docker image (`elleryhe/ovh-nexus:latest`) bundling the React frontend (served by Nginx) and the Python Gunicorn backend.

### 1. Create `.env`

```bash
cat > .env << 'EOF'
# Required: Console login password / API secret key
API_SECRET_KEY=replace-with-a-strong-random-secret

# Optional: Default WebUI language (en | fr | zh-CN)
DEFAULT_LOCALE=en

# Optional: Set to true if accessed exclusively over HTTPS
COOKIE_SECURE=false
EOF
```

### 2. Start with `docker-compose.yml`

```yaml
services:
  ovh-nexus:
    image: ${DOCKER_IMAGE:-elleryhe/ovh-nexus:latest}
    container_name: ovh-nexus
    restart: unless-stopped
    ports:
      - "20000:80"
    environment:
      - TZ=Asia/Shanghai
      - PORT=5000
      - API_SECRET_KEY=${API_SECRET_KEY}
      - DEFAULT_LOCALE=${DEFAULT_LOCALE:-en}
      - ALLOWED_ORIGINS=${ALLOWED_ORIGINS:-http://localhost:20000,http://127.0.0.1:20000}
      - COOKIE_SECURE=${COOKIE_SECURE:-false}
    volumes:
      - ./data:/app/data
      - ./logs:/app/logs
```

Run:

```bash
docker compose up -d
```

Open `http://localhost:20000` and sign in using your `API_SECRET_KEY`.

---

## OVHcloud API Permissions

Generate your OVHcloud API credentials (`App Key`, `App Secret`, `Consumer Key`) at:
- **Europe (`ovh-eu`)**: `https://eu.api.ovh.com/createToken/`
- **Canada / Global (`ovh-ca`)**: `https://ca.api.ovh.com/createToken/`
- **United States (`ovh-us`)**: `https://api.us.ovhcloud.com/createToken/`

Recommended access rules:
- `GET /*`
- `POST /*`
- `PUT /*`
- `DELETE /*`

---

## License

This program is free software: you can redistribute it and/or modify it under the terms of the **GNU Affero General Public License v3.0 (AGPL-3.0)** as published by the Free Software Foundation. See [LICENSE](./LICENSE) for the full license text, [CONTRIBUTING.md](./CONTRIBUTING.md) for contribution guidelines, and [SECURITY.md](./SECURITY.md) for our security policy.

---

## Disclaimer

> **OVH Nexus** is an independent, unofficial open-source project and is **not affiliated with, endorsed by, or sponsored by OVH SAS / OVHcloud**. "OVH" and "OVHcloud" are registered trademarks of OVH SAS. Use this software responsibly and in compliance with OVHcloud's Terms of Service and API rate limits.
