---
title: Changelog
description: Release history and notable changes for OVH Nexus.
---

:::note[Latest Release: v1.3.1]
Track official tagged releases and multi-architecture container builds on [GitHub Releases](https://github.com/ElleryHe/OVH-Nexus/releases) and [Docker Hub (`elleryhe/ovh-nexus`)](https://hub.docker.com/r/elleryhe/ovh-nexus).
:::

All notable changes to **OVH Nexus** are documented on this page following the [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) format and [Semantic Versioning](https://semver.org/).

## [1.3.1] - 2026-11-17 — Presale Detection, Quick Monitor, Tri-Lingual Datacenters & MCP Enhancements

### Added

- **Presale / Unlisted Server Detection & Filtering**:
  - Automatically identifies unannounced/presale `planCode`s present in OVHcloud live datacenter availability feeds prior to public catalog listing, adding **Presale** badges, hardware generation tags (`2024`–`2027`), and a **Presale Only** filter across both the Live Availability Matrix (`/availability`) and Server Catalog (`/servers`).
- **One-Click Quick Monitor Modal (`QuickMonitorDialog`)**:
  - Configure datacenter stock alerts, hardware config filters, and auto-order rules directly from any row in `/availability` or `/servers`.
- **Tri-Lingual Datacenter Geolocation Dictionary**:
  - Added localized city, country, and flag emoji labels (`en` / `fr` / `zh-CN`, e.g., `BHS (Beauharnois, Canada 🇨🇦)` / `BHS（博阿努瓦，加拿大 🇨🇦）`, plus compact `🇨🇦 BHS` badges with hover tooltips) across all WebUI views and notification templates, including support for the Toronto (`TOR` / `ca-east-tor`) datacenter.
- **Stock Monitor & Auto-Order Queue Source Linkage**:
  - Queue tasks created by stock monitor triggers now record `source: "stock-monitor"` with bidirectional jump links between `/monitor` and `/queue`, and single-source backend triggering (removing duplicate browser-side quick-order calls).
- **MCP Gateway Enhancements**:
  - Added **Server Entry Only** vs **Full Config File** snippet modes with JSON merge guidance on `/mcp`, added `offset` pagination and `presale_only` filtering to `catalog_availability`, and aligned `catalog_sku_changes` with the SKU Watcher timeline.

### Fixed

- **Baseline-Silent SKU Listing State Tracking**:
  - Initial deployment baseline SKUs are now marked as existing inventory (`baseline: true`) rather than newly listed, tracking distinct `presaleSince` and `listedAt` timestamps so only genuine post-baseline listings and `presale -> listed` transitions appear as **New**.
- **Toronto (`ca-east-tor`) Cart Region Inference**:
  - Fixed `InferCartRegion` to map `tor` / `ca-east-tor*` datacenters to `"canada"`.
- **Security Dependencies**:
  - Upgraded `golang.org/x/oauth2` to `v0.38.0` and pruned unused transitive `google.golang.org/protobuf` from `server/go.mod` (`CVE-2025-22868`, `CVE-2024-24786`).

---

## [1.3.0] - Go 1.27 Single Static Binary, SQLite WAL Persistence & SSE Structured Logging

:::caution[Breaking Changes in v1.3.0]
`v1.3.0` introduces a single static Go binary architecture (`:8080`), embedded SQLite WAL storage (`/data/nexus.db` with **no automatic migration** from `<= v1.2.3` JSON state files), outbound-only notifications, and standardized English `slog` logging. Review the **Breaking Changes** below before upgrading from `<= v1.2.3`.
:::

### Breaking Changes

- **Container Port & Single-Volume Layout (`docker-compose.yml`)**:
  - Internal HTTP listening port changed from `:80` to `:8080` (`ports: ["20000:8080"]`); update any port mappings or reverse proxy targets from `:80` to `:8080`.
  - Consolidated persistent mounts into a single `./data:/data` volume and removed separate `logs`/`cache` mounts as well as legacy `BIND_HOST`, `DEBUG`, and `ENABLE_API_KEY_AUTH` environment variables.
- **Embedded SQLite WAL Storage (`/data/nexus.db` — Clean Data Directory Required)**:
  - Replaced standalone JSON state files with `/data/nexus.db`. There is **no automatic migration** from `<= v1.2.3` JSON files — upgrading from earlier releases requires initializing a clean `./data` directory and re-enrolling 2FA (TOTP) and OVHcloud API accounts.
- **Outbound-Only Notification Center**:
  - Removed inbound Telegram Webhook callback routes (`/api/telegram/*`) in favor of outbound-only push delivery (Telegram Bot API & SMTP Email) with optional `panelUrl` links.
- **Standardized English Structured Logging**:
  - Replaced localized unstructured logs with English `slog` entries (UTC RFC3339 timestamps) and SSE live streaming (`/api/logs/stream`).

### Changed

- **Go 1.27 Single Static Binary Architecture (`server/`)**:
  - Compiled into a single static Go 1.27 binary (`CGO_ENABLED=0`) powered by `net/http`, `chi/v5`, and `github.com/ovh/go-ovh` with the React SPA embedded directly via `//go:embed`.
  - Added native Slowloris and request-flood defenses (`ReadHeaderTimeout: 5s`, `ReadTimeout: 15s`, `WriteTimeout: 120s`, `IdleTimeout: 60s`, `2 MB` body cap) plus CSP headers and gzip compression natively on port `8080`.
- **Hardened Minimal Container (`~20 MB`, `su-exec` Non-Root Entrypoint, `PUID`/`PGID`)**:
  - 3-stage `Dockerfile` (`node:24-alpine` -> `golang:1.27-alpine` -> `alpine:3.24`) bundling `su-exec` and `/usr/local/bin/docker-entrypoint.sh` to automatically initialize `/data` (`0700`) and drop privileges in-place to `PUID:PGID` (default `65532:65532`, eliminating manual host `chown` prerequisites while supporting Kubernetes `runAsNonRoot: true`).
  - Simplified `docker-compose.yml` to map `"20000:8080"` with a **single host mount `./data:/data`** and optional `PUID`/`PGID` overrides.
- **Pure-Go SQLite WAL Database (`/data/nexus.db`)**:
  - Consolidated accounts, TOTP state, purchase queue, order history, stock/VPS subscriptions, SKU baseline & timeline, notification channels, MCP config/aliases/audit logs, and system logs into ACID SQLite (`modernc.org/sqlite`, `WAL` mode, `0700` dir / `0600` file).
  - Column-level `HKDF-SHA256` + `AES-256-GCM` encryption (`enc:v1:...`) for stored API credentials, bot tokens, SMTP passwords, and TOTP secrets, plus `SHA-256` hashing for MCP Bearer tokens and recovery codes.
- **Unified Quick-Order & Outbound-Only Notifications**:
  - Standardized quick-order procurement under `/api/orders/quick-order` (`auto_pay=false` and `waive_retractation_period=false` by default).
  - Streamlined the **Notification Center** as an outbound-only dispatcher (Telegram Bot API & SMTP Email) with a **Notification Language (`locale`: `en`, `fr`, `zh-CN`)** selector and container `TZ` support (default `UTC`).

### Added

- **Standardized English `log/slog` Hub & SSE Live Console (`/logs`)**:
  - Emits English structured logs across 14 canonical `source` modules (`server`, `http`, `auth`, `totp`, `vault`, `ovh`, `catalog`, `queue`, `stock-monitor`, `vps-monitor`, `sku-watcher`, `fleet`, `notify`, `mcp`) with automatic secret redaction.
  - Persists a **10,000-row rolling ring buffer** in SQLite (`system_logs`), streams real-time entries via **Server-Sent Events (`GET /api/logs/stream`)**, and supports **`.jsonl` / `.log` export** (`GET /api/logs/export`).
  - Upgraded `LogsPage.tsx` with tri-lingual UI, module/level/keyword filters, attribute pills, and browser-local timestamps.
- **`TRUSTED_PROXIES` IP Extraction & `TZ` Configuration**:
  - Added CIDR/IP allowlisting for `X-Forwarded-For` / `X-Real-IP` headers and container timezone configuration via `TZ` (default `UTC`).

---

## [1.2.3] - Responsive Layout Fix, First-Run API Onboarding, 2FA Status Sync & Docs SEO

### Fixed

- **Tailwind CSS v3 Selector Escaping & Sidebar Layout**:
  - Pinned `"postcss-selector-parser": "^6.1.6"` in `package.json` `overrides` (preventing resolution to `7.2.0`, which corrupted responsive selectors like `lg:block` and hid the desktop sidebar).
  - Added a sticky mobile/tablet header bar (`< 1024px`) in `Layout.tsx` so navigation is always accessible across all viewport widths.
- **First-Time Deployment OVH API Onboarding & Dismissible Notice**:
  - Added **OVH API Accounts (`/api-accounts`)** to the main sidebar and linked `/settings` and `APINotice` directly to `/api-accounts`, with one-click banner dismissal and a clear 2FA prerequisite callout.
- **2FA (TOTP) Status Synchronization**:
  - Standardized `totpEnabled` and `message` payloads across `/api/auth/totp/*` endpoints so newly bound 2FA immediately displays as **Enabled** without false-negative UI resets.

### Added

- **Documentation Portal SEO (`robots.txt` & `sitemap.xml`)**:
  - Added direct `/sitemap.xml` generation alongside `/sitemap-index.xml` with `<lastmod>` and `x-default` `hreflang` tags, refined `robots.txt` crawler rules, and configured `og:image` / `twitter:image` metadata.

---

## [1.2.2] - Dependency Baseline Updates & Dependabot Governance Hardening

### Changed

- Upgraded security and documentation dependencies (`@astrojs/starlight@^0.42.11` in `website/package.json`).
- Hardened `.github/dependabot.yml` ignore rules for `npm` (`/` and `/website`), explicitly locking major-version boundaries for `react` / `react-dom` (`<19.0.0`), `tailwindcss` (`<4.0.0`), and `recharts` (`<3.0.0`) while suppressing routine breaking `semver-major` PRs.

---

## [1.2.1] - AES-256-GCM At-Rest Vault, TOTP 2FA Gatekeeper, EU 14-Day Retraction Opt-In & Node 24 LTS

### Security

- **At-Rest Secret Encryption (`DATA_ENCRYPTION_KEY`)**:
  - Added a mandatory, independent `DATA_ENCRYPTION_KEY` environment variable (`>= 16` chars, fail-fast on startup if missing, weak, or identical to `API_SECRET_KEY`).
  - Encrypted all secrets at rest (`appKey`, `appSecret`, `consumerKey`, Telegram Bot tokens, SMTP passwords, TOTP seeds, and session secrets) using `HKDF-SHA256` + `AES-256-GCM` (`enc:v1:<nonce>:<ciphertext>`).
  - Persisted MCP Bearer tokens strictly as irreversible `SHA-256` `tokenHash` digests with redacted `maskedToken` previews.
- **TOTP Two-Factor Authentication (RFC 6238) & Tiered High-Risk Gatekeeper**:
  - Added native RFC 6238 TOTP 2FA with offline QR code enrollment, 30-second anti-replay window, and 8 single-use emergency recovery codes (`XXXX-XXXX`).
  - Enforced a global high-risk operation gatekeeper: **when 2FA is not enabled, all high-risk routes return `403 TOTP_SETUP_REQUIRED` and MCP `L3` (`operations`) is forcibly locked to `false`**.
  - Implemented two-tier step-up verification (`strict` fresh TOTP required on every call for OS reinstall, service termination, ownership transfer, API credential mutation, and 2FA disable; `sudo` 5-minute session window for server/VPS reboot, rescue mode, IPMI console, firewall/network changes, MCP governance, and enabling `auto_pay` / `waive_retractation_period`).
- **Consumer Protection & Payment Guardrails (`waiveRetractationPeriod` & `auto_pay`)**:
  - Changed `waiveRetractationPeriod` from enabled-by-default to an explicit opt-in defaulting to `false` (preserving the EU 14-day statutory right of withdrawal by default), with per-task dual switches (`auto_pay` + `waive_retractation_period`) in the WebUI and hardcoded `false` in MCP tools.

### Changed

- Upgraded container base images and CI workflow runtimes to **Node 24 LTS (`node:24-alpine`)** and upgraded GitHub Actions (`actions/setup-node@v7`, `docker/setup-buildx-action@v4`, `peter-evans/dockerhub-description@v5`) and npm dependencies (`lucide-react@^1.72.0`, `sonner@^2.0.8`, `autoprefixer@^10.6.2`, `@radix-ui/react-alert-dialog@^1.2.1`, `@radix-ui/react-tooltip@^1.4.1`, `wrangler@^4.160.1`, `qrcode@^1.5.4`), refining `.github/dependabot.yml` to target `develop` with grouped minor/patch updates.

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
    - Unconditionally locks `auto_pay = false` and `quantity = 1` in code for all MCP orders and queue tasks, enforces a rolling hourly order limit, and permanently blocks OS reinstallation, service termination, raw IPMI console URLs, and credential mutations.
- **Bidirectional Privacy Shield (`Balanced Mode`)**:
  - Automatically pseudonymizes OVH account IDs (`acct-01`), dedicated server hostnames (`bm-01`), VPS instances (`vps-01`), public IPs (`ip-01`, masked as `*.*.*.42`), and order IDs (`ord-01`), while stripping `orderPassword` payment/invoice links and PII from AI context.
- **MCP AI Gateway WebUI Console (`/mcp`) & Audit Event (`mcp.action`)**:
  - Added a dedicated **MCP AI Gateway** page under the System sidebar group with token rotation/revocation, L1–L3 scope switches, multi-account visibility whitelist, one-click client config generators (Claude Code, Claude Desktop, Cursor, Antigravity), alias mapping viewer, and real-time `mcp.action` Notification Center alerts.

---

## [1.1.1] - Test Dependency Security Patch

### Security

- Remediated upstream test-suite dependency advisory `CVE-2025-71176` (`GHSA-6w46-j5rx-g56g`).

---

## [1.1.0] - Security Hardening, Visual Architecture Diagrams & Documentation Polish

### Security

- **Zero-Vulnerability Dependency Tree**:
  - Upgraded `react-router-dom` (`>= 7.18.5`) and `vite` (`>= 8.3.6`) in the main console, and `astro` (`>= 7.4.7`), `@astrojs/starlight` (`>= 0.42.10`), and `sharp` (`>= 0.35.5`) in the `website/` documentation portal.
  - Added npm `overrides` for transitive build-time packages (`esbuild >= 0.28.1`, `glob >= 14.0.2`, `minimatch >= 11.0.1`, `postcss-selector-parser >= 6.1.5`, `zod >= 4.6.6`), resolving all 31 GitHub Dependabot security advisories across both lockfiles (`0 vulnerabilities`).

### Added

- **Tri-Lingual System Architecture Diagrams**:
  - Added localized system architecture diagrams to the Getting Started introduction pages.
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
  - Built a full tri-lingual (`en`, `fr`, `zh-CN`) documentation site powered by **Astro Starlight** and **Pagefind** static offline search (`https://ovh-nexus.ellery.me`).
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
  - Public Eco + Baremetal catalog dictionary with 12h caching and deterministic fallback parser for unreleased SKUs.
  - Human-readable memory/storage formatting (`DDR4/DDR5 ECC`, `NVMe/SSD/SATA`, `Soft/HW RAID`) and exact configuration-level (`FQN`) monthly pricing with price sorting.
- **Public SKU Change Watcher**:
  - Zero-key background shelf watcher with silent baseline initialization, 2-round removal confirmation, >30% drop circuit breaker, and regional suffix deduplication.
- **Pluggable Notification Center**:
  - Multi-instance **Telegram Bot** and **SMTP Email** (`SSL/TLS`, `STARTTLS`, `Plain`, HTML templates) channels subscribing to fine-grained events (`stock.available`, `stock.unavailable`, `sku.changed`, `order.success`, `order.failed`, `order.delivered`, `service.expiring`).
- **Multi-Account, Security & Tri-Lingual UI**:
  - Multi-account registry (`ovh-eu`, `ovh-ca`, `ovh-us`) with `X-OVH-Account` isolation.
  - Mandatory `API_SECRET_KEY` validation, `HttpOnly` session cookies, brute-force rate limiting, secret masking (`****`), and `0600` data file permissions.
  - Built-in **English (`en`)**, **Français (`fr`)**, and **简体中文 (`zh-CN`)** localization and adaptive Light/Dark OVHcloud Blue themes.
