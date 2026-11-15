---
title: Changelog
description: Release history and notable changes for OVH Nexus.
---

:::note[Latest Release: v1.2.1]
Track official tagged releases and multi-architecture container builds on [GitHub Releases](https://github.com/ElleryHe/OVH-Nexus/releases) and [Docker Hub (`elleryhe/ovh-nexus`)](https://hub.docker.com/r/elleryhe/ovh-nexus).
:::

All notable changes to **OVH Nexus** are documented on this page following the [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) format and [Semantic Versioning](https://semver.org/).

## [1.2.1] - AES-256-GCM At-Rest Vault, TOTP 2FA Gatekeeper, EU 14-Day Retraction Opt-In & Node 24 / Python 3.12 LTS

### Security

- **At-Rest Secret Encryption (`DATA_ENCRYPTION_KEY`, `backend/crypto_vault.py`)**:
  - Added a mandatory, independent `DATA_ENCRYPTION_KEY` environment variable (`>= 16` chars, fail-fast on startup if missing, weak, or identical to `API_SECRET_KEY`).
  - Encrypts all secrets at rest in `./data/config.json` (`appKey`, `appSecret`, `consumerKey`, `tgToken`, `notifications.channels[*].config.token/password`), `./data/accounts.json`, `./data/totp_config.json`, and `./data/session_secret` / `./data/webhook_secret` using `HKDF-SHA256` + `AES-256-GCM` (`enc:v1:<nonce>:<ciphertext>`), with automatic in-place migration of existing plaintext files.
  - Upgraded `./data/mcp_config.json` to persist only an irreversible `SHA-256` `tokenHash` and redacted `maskedToken` preview, wiping plaintext MCP Bearer tokens from disk.
- **TOTP Two-Factor Authentication (RFC 6238) & Tiered High-Risk Gatekeeper (`backend/totp_service.py`)**:
  - Added native RFC 6238 TOTP 2FA with offline QR code enrollment, 30-second anti-replay window, and 8 single-use emergency recovery codes (`XXXX-XXXX`).
  - Enforced a global high-risk operation gatekeeper: **when 2FA is not enabled, all high-risk routes return `403 TOTP_SETUP_REQUIRED` and MCP `L3` (`operations`) is forcibly locked to `False`**.
  - Implemented two-tier step-up verification (`strict` fresh TOTP required on every call for OS reinstall, service termination, ownership transfer, API credential mutation, and 2FA disable; `sudo` 5-minute session window for server/VPS reboot, rescue mode, IPMI console, firewall/network changes, MCP governance, and enabling `auto_pay` / `waiveRetractationPeriod`).
- **Consumer Protection & Payment Guardrails (`waiveRetractationPeriod` & `auto_pay`)**:
  - Changed `waiveRetractationPeriod` in `purchase_server()` and `/api/queue` from hardcoded `True` to an explicit opt-in defaulting to `False` (preserving the EU 14-day statutory right of withdrawal by default), with per-task dual switches (`auto_pay` + `waive_retractation_period`) in the WebUI and hardcoded `False` in MCP tools.

### Changed

- Upgraded container base images and CI workflow runtimes to **Node 24 LTS (`node:24-alpine`)** and **Python 3.12 (`python:3.12-slim`)**.
- Upgraded GitHub Actions (`actions/setup-node@v7`, `actions/setup-python@v7`, `docker/setup-buildx-action@v4`, `peter-evans/dockerhub-description@v5`) and npm dependencies (`lucide-react@^1.72.0`, `sonner@^2.0.8`, `autoprefixer@^10.6.2`, `@radix-ui/react-alert-dialog@^1.2.1`, `@radix-ui/react-tooltip@^1.4.1`, `wrangler@^4.160.1`, `qrcode@^1.5.4`), and refined `.github/dependabot.yml` to target `develop` with grouped minor/patch updates.

---

## [1.2.0] - MCP AI Gateway, Bidirectional Privacy Shield & Unpaid Order Guardrails

### Added

- **Model Context Protocol (MCP) Streamable HTTP Gateway (`/api/mcp`)**:
  - Built-in JSON-RPC 2.0 Streamable HTTP endpoint inspired by `ovh/ovhcloud-cli`, disabled by default and authenticated via an isolated high-entropy Bearer Token (`nxs_mcp_...`) with per-IP brute-force protection.
  - **20 MCP Tools across 3 Configurable Permission Scopes**:
    - `L1 Read-Only` (`11 tools`): Obfuscated account profiles, dedicated servers, VPS instances, IP Edge Firewall rules, service expiration countdowns, order delivery tracking, live datacenter stock, and SKU watcher changes.
    - `L2 Unpaid Procurement` (`5 tools`): Two-step price quote and unpaid order creation (`order_quote` → `order_create_unpaid`), auto-order queue management, and stock monitor subscriptions.
    - `L3 Reversible Operations` (`4 tools`): Dedicated server / VPS hardware reboot, stuck IPMI session reset, and IP Edge Firewall / Anti-DDoS mitigation updates.
  - **L4 Hardcoded Financial & Destructive Redlines**:
    - Unconditionally locks `auto_pay = False` and `quantity = 1` in code for all MCP orders and queue tasks, enforces a rolling hourly order limit, and permanently blocks OS reinstallation, service termination, raw IPMI console URLs, and credential mutations.
- **Bidirectional Privacy Shield (`Balanced Mode`, `backend/mcp/privacy_shield.py`)**:
  - Automatically pseudonymizes OVH account IDs (`acct-01`), dedicated server hostnames (`bm-01`), VPS instances (`vps-01`), public IPs (`ip-01`, masked as `*.*.*.42`), and order IDs (`ord-01`), while stripping `orderPassword` payment/invoice links and PII from AI context.
- **MCP AI Gateway WebUI Console (`/mcp`) & Audit Event (`mcp.action`)**:
  - Added a dedicated **MCP AI Gateway** page under the System sidebar group with token rotation/revocation, L1–L3 scope switches, multi-account visibility whitelist, one-click client config generators (Claude Code, Claude Desktop, Cursor, Antigravity), alias mapping viewer, and real-time `mcp.action` Notification Center alerts.

---

## [1.1.1] - Backend Test Dependency Security Patch

### Security

- **Backend Dev Dependency Update (`CVE-2025-71176` / `GHSA-6w46-j5rx-g56g`)**:
  - Upgraded `pytest` in `backend/requirements-dev.txt` from `8.4.2` to `>=9.0.3` to remediate the UNIX temporary directory handling vulnerability (`CVE-2025-71176`), achieving 0 open Dependabot alerts across all npm and pip manifests.

---

## [1.1.0] - Security Hardening, Visual Architecture Diagrams & Documentation Polish

### Security

- **Zero-Vulnerability Dependency Tree**:
  - Upgraded `react-router-dom` (`>= 7.18.5`) and `vite` (`>= 8.3.6`) in the main console, and `astro` (`>= 7.4.7`), `@astrojs/starlight` (`>= 0.42.10`), and `sharp` (`>= 0.35.5`) in the `website/` documentation portal.
  - Added npm `overrides` for transitive build-time packages (`esbuild >= 0.28.1`, `glob >= 14.0.2`, `minimatch >= 11.0.1`, `postcss-selector-parser >= 6.1.5`, `zod >= 4.6.6`), resolving all 31 GitHub Dependabot security advisories across both lockfiles (`0 vulnerabilities`).

### Added

- **Tri-Lingual System Architecture Diagrams**:
  - Added localized, high-resolution system architecture diagrams (`architecture-en.jpg`, `architecture-fr.jpg`, `architecture-zh.jpg`) to the Getting Started introduction pages.
- **Home Page Footer & External Link Handling**:
  - Added a dynamic copyright and license footer (`© <Year> OVH-Nexus · AGPL-3.0`) on the documentation home page and automatic `target="_blank"` handling for external links.

### Changed

- **Docker Build Runtime & Standalone README Quick Start**:
  - Upgraded the `frontend-builder` stage in `Dockerfile` from `node:18-alpine` to `node:22-alpine` (LTS).
  - Expanded the Quick Start in `README.md`, `docs/README.fr.md`, and `docs/README.zh-CN.md` with a self-contained `docker-compose.yml` and `.env` setup that runs without cloning the repository.
  - Fixed missing Starlight card icons (`telegram` and `padlock`) on the documentation home page.

---

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
