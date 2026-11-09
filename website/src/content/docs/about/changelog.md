---
title: Changelog
description: Release history and notable changes for OVH Nexus.
---

:::note[Latest Release: v1.0.1]
Track official tagged releases and multi-architecture container builds on [GitHub Releases](https://github.com/ElleryHe/OVH-Nexus/releases) and [Docker Hub (`elleryhe/ovh-nexus`)](https://hub.docker.com/r/elleryhe/ovh-nexus).
:::

All notable changes to **OVH Nexus** are documented on this page following the [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) format and [Semantic Versioning](https://semver.org/).

## [1.0.1] - Documentation Site & CI/CD Optimization

### Added

- **Official Documentation Site (`website/`)**:
  - Built a full tri-lingual (`en`, `fr`, `zh-CN`) documentation site powered by **Astro Starlight** and **Pagefind** static offline search across 69 pages (`https://ovh-nexus.ellery.me`).
  - Configured assets-only **Cloudflare Workers** deployment (`wrangler.jsonc`, `workers_dev: false`, `preview_urls: false`) with strict CSP and immutable asset caching (`_headers`).

### Changed

- **Repository Storefront & CI Triggers**:
  - Streamlined `README.md`, `docs/README.fr.md`, and `docs/README.zh-CN.md` with quick-start instructions and direct links to the documentation portal.
  - Added `paths-ignore` rules to `.github/workflows/dockerhub.yml` and excluded `website/` in `.dockerignore` so documentation-only commits do not trigger unnecessary Docker image builds.

---

## [1.0.0] - Initial Public Release

### Added

- **Dedicated Server & Cloud Fleet Control**:
  - Dedicated Server inventory, MRTG traffic graphs, IPMI/KVM remote access (`SOL`, `KVM`, `xterm.js`), and power/rescue management.
  - Automated OS reinstallation (`/dedicated/server/{serviceName}/install/start`) with custom hardware/software RAID, Proxmox VE ZFS profiles, and one-click SSH public key injection (`/me/sshKey`).
  - IP Edge Firewall (`/ip/{ip}/firewall`) rule management and permanent VAC Anti-DDoS mitigation toggle (`/ip/{ip}/mitigation`).
- **Purchased VPS Fleet Control**:
  - Live overview of purchased VPS instances (`/vps`), vCore/RAM/Disk allocations, IPv4/IPv6 addresses, operational status, and one-click reboot (`/vps/{serviceName}/reboot`).
- **Orders, Live Delivery Pipeline & Renewal Matrix**:
  - Official OVHcloud order sync (`/me/order`), invoice/payment links, and real-time 4-stage hardware delivery pipeline (`CREATING` → `VALIDATING` → `DELIVERING` → `INSTALLATION`).
  - Unified Dedicated Server and VPS expiration countdown matrix (`/serviceInfos`) with `<= 7d` critical and `<= 30d` warning indicators.
- **Live Availability Matrix, Catalog Service & Pricing**:
  - Public Eco + Baremetal catalog dictionary (`CatalogService`) with 12h disk cache and deterministic fallback parser for unreleased SKUs.
  - Human-readable memory/storage formatting (`DDR4/DDR5 ECC`, `NVMe/SSD/SATA`, `Soft/HW RAID`) and exact configuration-level (`FQN`) monthly pricing with price sorting.
- **Public SKU Change Watcher**:
  - Zero-key background shelf watcher (`SkuWatcher`) with silent baseline initialization, 2-round removal confirmation, >30% drop circuit breaker, and regional suffix deduplication.
- **Pluggable Notification Center**:
  - Multi-instance **Telegram Bot** and **SMTP Email** (`SSL/TLS`, `STARTTLS`, `Plain`, HTML templates) channels subscribing to 7 fine-grained events (`stock.available`, `stock.unavailable`, `sku.changed`, `order.success`, `order.failed`, `order.delivered`, `service.expiring`).
- **Multi-Account, Security & Tri-Lingual UI**:
  - Multi-account registry (`ovh-eu`, `ovh-ca`, `ovh-us`) with `X-OVH-Account` isolation.
  - Mandatory `API_SECRET_KEY` validation, `HttpOnly` session cookies, brute-force rate limiting, secret masking (`****`), and `0600` data file permissions.
  - Built-in **English (`en`)**, **Français (`fr`)**, and **简体中文 (`zh-CN`)** localization and adaptive Light/Dark OVHcloud Blue themes.
