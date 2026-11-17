# Changelog

All notable changes to **OVH Nexus** are documented in this file following [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and [Semantic Versioning](https://semver.org/).

📚 **Tri-Lingual Online Changelog**:
- **English**: <a href="https://ovh-nexus.ellery.me/about/changelog/" target="_blank" rel="noopener noreferrer">https://ovh-nexus.ellery.me/about/changelog/</a>
- **Français**: <a href="https://ovh-nexus.ellery.me/fr/about/changelog/" target="_blank" rel="noopener noreferrer">https://ovh-nexus.ellery.me/fr/about/changelog/</a>
- **简体中文**: <a href="https://ovh-nexus.ellery.me/zh-cn/about/changelog/" target="_blank" rel="noopener noreferrer">https://ovh-nexus.ellery.me/zh-cn/about/changelog/</a>

---

## [1.3.2] - 2026-11-17

### Added
- **Announced Server Listing State (`listingState: "announced"`) & Sale Started Alerts**:
  - Identifies bare-metal models already published with pricing in OVHcloud public catalogs (`eco` / `baremetalServers`) prior to live availability feed activation (such as `KS-LE`, `RISE-LE`, and `SYS-LE` Limited Edition series), exposing **Announced** badges and filters in both the Live Availability Matrix (`/availability`) and Server Catalog (`/servers`).
  - Automatically emits `saleStarted: true` SKU transition records and **Sale Started** notifications when an `announced` model first enters the live datacenter availability feed.
- **SKU Version Refresh (`replaced`) Detection & Product-Line Grouped Notifications**:
  - Detects same-cycle `planCode` version refreshes within the same product line (`Name + CPU`, e.g., `24sys022 -> 24sys02-v3`) as a single `replaced` (`replacedFrom`) timeline event instead of separate add/remove entries, and groups `sku.changed` notifications by **New**, **Sale Started**, **Replaced**, and **Discontinued** sections under each product line.
  - Extended MCP `catalog_availability` and `catalog_sku_changes` tools with unified `listingState` (`listed` / `announced` / `presale`), `saleStarted`, and `replacedFrom` fields.
- **Per-Plan Configuration Endpoint (`GET /api/catalog/plans/{planCode}/configs`)**:
  - Returns merged default and live-availability configurations for any `planCode`, including `configKey` (`memory+storage`), localized specs, addon `options`, `isDefault` markers, ex-VAT `estimatedMonthlyPrice` (`basePrice` + memory/storage addon surcharges), currency, and per-datacenter stock status.
- **Multi-Target Configuration-Level Stock Monitor (`targets[]`) & Live Price Cap Guard**:
  - Upgraded stock monitor subscriptions (`StockSubscription.targets[]`) so each target independently binds a specific hardware configuration (`configKey` + addon `options`, or `Any Configuration` for notify-only rules), datacenter filter, notification switches, `autoOrder` toggle, order `quantity`, and ex-VAT `maxMonthlyPrice` protection cap.
  - Before automated checkout, live cart quotes are verified against `maxMonthlyPrice` (ex-VAT recurring monthly fee, excluding one-time setup fee), rejecting orders that exceed the configured cap (`PRICE_ABOVE_LIMIT`) with a warning log and `order.failed` notification.

### Changed
- **Compact Real-Time Availability Matrix (`/availability`)**:
  - Consolidated rows to **1 row per `planCode`** (keeping distinct regional variants such as `-sgp`, `-syd`, and `-mum` on separate rows) with an inline configuration selector switching between `All Configurations` (best stock across configurations) and any specific configuration's ex-VAT monthly price and per-datacenter availability.
- **Compact Collapsible Server Catalog Cards (`/servers`)**:
  - Redesigned the Server Catalog into a compact 3–4 column desktop grid with cards collapsed by default (showing model, CPU, 1-line default spec summary, starting monthly price, and compact datacenter pills).
  - Merged separate default and optional configuration blocks inside expanded cards into a single unified configuration selector (defaulting to in-stock configurations and datacenters with a **Show all inventory (including out-of-stock)** toggle and a collapsible **Advanced Options** section), passing the selected configuration directly into **Add to Queue** and **Quick Monitor**.
- **Faster Bare-Metal SKU Watcher & 20s Confirmation Recheck**:
  - Reduced the default SKU watcher interval from `600s` to `120s` (automatically migrating legacy `600s` default configs), isolated bare-metal SKU checks (`GetBaremetalDictionary`) from the VPS catalog, and added a dedicated ~20-second confirmation recheck when a SKU first goes missing before marking it as discontinued.
- **Safe Legacy Auto-Order Migration (`needsConfigSelection`)**:
  - Existing stock subscriptions are automatically migrated to configuration targets on startup; legacy subscriptions that had `autoOrder` enabled without an explicit configuration are safely paused with `needsConfigSelection: true` and surfaced with a prominent warning banner in `/monitor` until a specific configuration and price cap are selected.

### Fixed
- **Catalog Dictionary Alias Pollution & Presale Detection Accuracy**:
  - Separated exact catalog `planCode` entries (`RealPlans`) from `baseCode` and `product` metadata lookup aliases in `CatalogDictionary`, silently pruning historical alias pollution (such as `24sys02-1g` or stripped version prefixes) from SKU state and restoring accurate `presale` vs `listed` vs `announced` classification.
  - Backfilled missing `listedAt` timestamps from `firstSeen` for post-baseline SKU entries.
- **Strict Stock-Monitor Procurement Configuration & Quantity Enforcement**:
  - Removed the unsafe fallback that automatically selected the first available configuration when `options` were omitted during `stock-monitor` procurement (`CONFIG_REQUIRED_FOR_AUTO_ORDER`), and enforced the target's configured `quantity` during cart creation.

---

## [1.3.1] - 2026-11-17

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

## [1.3.0] - 2026-11-16

### Breaking Changes
- **Container Port & Single-Volume Layout (`docker-compose.yml`)**:
  - Internal container HTTP listening port changed from `:80` to `:8080` (`ports: ["20000:8080"]`); existing reverse proxy or port-mapping configurations pointing to container port `80` must be updated to `8080`.
  - Consolidated container storage into a single `./data:/data` volume mount and removed separate `logs`/`cache` bind mounts as well as legacy `BIND_HOST`, `DEBUG`, and `ENABLE_API_KEY_AUTH` environment variables.
- **Embedded SQLite WAL Storage (`/data/nexus.db` — Clean Data Directory Required)**:
  - Replaced standalone JSON state files with `/data/nexus.db`. There is **no automatic migration** from `<= v1.2.3` JSON state files — upgrading from earlier releases requires initializing a clean `./data` directory and re-enrolling 2FA (TOTP) and OVHcloud API accounts.
- **Outbound-Only Notification Center**:
  - Removed inbound Telegram Webhook callback routes (`/api/telegram/*`) in favor of purely outbound push delivery (Telegram Bot API & SMTP Email) with optional `panelUrl` action links.
- **Standardized English Structured Logging**:
  - Replaced localized unstructured logs with standardized English `log/slog` entries (UTC RFC3339 timestamps) persisted in SQLite (`system_logs`) and streamed live over SSE (`/api/logs/stream`).

### Changed
- **Go 1.27 Single Static Binary Architecture (`server/`)**:
  - Replaced the dual-process runtime with a **single statically compiled Go 1.27 binary** (`CGO_ENABLED=0`) powered by `net/http`, `chi/v5`, and `github.com/ovh/go-ovh`.
  - Embedded the React SPA directly into the binary via `//go:embed`, serving immutable cached assets, CSP/security headers, gzip compression, and SPA fallback natively on port `8080` without an internal Nginx tier.
  - Added native Slowloris and request-flood defenses (`ReadHeaderTimeout: 5s`, `ReadTimeout: 15s`, `WriteTimeout: 120s`, `IdleTimeout: 60s`, and a `2 MB` request body cap).
- **Hardened Minimal Container (`~20 MB`, `su-exec` Non-Root Entrypoint, `PUID`/`PGID`)**:
  - Updated `Dockerfile` to a 3-stage build (`node:24-alpine` -> `golang:1.27-alpine` -> `alpine:3.24`) with `ca-certificates`, `tzdata`, `su-exec`, and `/usr/local/bin/docker-entrypoint.sh`.
  - On boot, the entrypoint automatically initializes `/data` (`0700`) and drops privileges in-place via `exec su-exec` to `PUID:PGID` (default `65532:65532`, eliminating first-run host `chown` permission failures while remaining cleanly compatible with Kubernetes `runAsNonRoot: true`).
  - Simplified `docker-compose.yml` to map `"20000:8080"` with a **single persistent volume `./data:/data`** and optional `PUID`/`PGID` overrides.
- **Pure-Go Embedded SQLite WAL Persistence (`/data/nexus.db`)**:
  - Consolidated all runtime state (accounts, TOTP state, purchase queue, order history, stock/VPS subscriptions, SKU baseline & timeline, notification channels, MCP config/aliases/audit logs, and system logs) into a single ACID SQLite database (`modernc.org/sqlite`, `WAL` mode, `0700` dir / `0600` file permissions).
  - Preserved column-level `HKDF-SHA256` + `AES-256-GCM` at-rest encryption (`enc:v1:...`) for OVH API keys, Telegram Bot tokens, SMTP passwords, and TOTP secrets, plus `SHA-256` hashing for MCP Bearer tokens and recovery codes.
- **Unified Procurement Endpoint & Outbound-Only Notifications**:
  - Standardized quick-order procurement under `/api/orders/quick-order` with `auto_pay=false` and `waive_retractation_period=false` defaults.
  - Streamlined the **Notification Center** as a purely outbound dispatcher (Telegram Bot API & SMTP Email) with a configurable **Notification Language (`locale`: `en` default, `fr`, `zh-CN`)** and full container `TZ` timezone support (default `UTC`).
- **CI/CD & Dependabot Governance**:
  - Updated `.github/workflows/ci.yml` to run `npm ci && npm run build`, embed the SPA into `server/internal/webui/dist/`, and execute `go vet ./...` and `go test -race ./...` on Go `1.27`, alongside the Astro Starlight documentation build.
  - Updated `.github/dependabot.yml` to track `/server` (`gomod`) and pinned `node`, `golang`, and `alpine` Docker base images.

### Added
- **Standardized English Structured Logging & SSE Live Console (`/logs`)**:
  - Implemented a unified `log/slog` logging hub emitting standardized English entries across 14 canonical `source` modules (`server`, `http`, `auth`, `totp`, `vault`, `ovh`, `catalog`, `queue`, `stock-monitor`, `vps-monitor`, `sku-watcher`, `fleet`, `notify`, `mcp`) with automatic secret redaction.
  - Persists a **10,000-row rolling ring buffer** in SQLite (`system_logs`), streams real-time entries over **Server-Sent Events (`GET /api/logs/stream`)**, and supports one-click **`.jsonl` / `.log` export** (`GET /api/logs/export`).
  - Upgraded `LogsPage.tsx` with tri-lingual UI (`en` / `fr` / `zh-CN`), module/level/keyword filtering, structured attribute pills, and browser-local timestamp formatting.
- **Proxy-Aware Client IP Resolution (`TRUSTED_PROXIES`) & Container Timezone (`TZ`)**:
  - Added `TRUSTED_PROXIES` CIDR/IP allowlisting so `X-Forwarded-For` / `X-Real-IP` headers are honored only when requests originate from trusted reverse proxies.
  - Added `TZ` environment configuration (default `UTC`) controlling scheduled worker and notification timestamps.

---

## [1.2.3] - 2026-11-16

### Fixed
- **Tailwind CSS v3 Selector Escaping & Sidebar Layout Deadlock**:
  - Pinned `"postcss-selector-parser": "^6.1.6"` in `package.json` `overrides` (preventing resolution to `7.2.0`, which triple-escaped responsive/state selectors like `.lg\:block` and caused the desktop navigation sidebar to stay `hidden`).
  - Added a dedicated sticky top navigation bar (`< 1024px`) in `Layout.tsx` and aligned drawer breakpoints to eliminate the `768px–1023px` viewport navigation gap.
- **First-Time Deployment OVH API Onboarding & Top Notice Banner**:
  - Added **OVH API Accounts (`/api-accounts`)** directly to the left sidebar navigation and added a dedicated OVH API Accounts management card inside `/settings`.
  - Updated the unconfigured API banner (`APINotice.tsx`) to link directly to `/api-accounts`, auto-hide on `/api-accounts` and `/settings`, and support one-click dismissal.
  - Added a 2FA prerequisite onboarding callout on `/api-accounts` and automatic `checkAuthentication()` state refresh after saving/deleting accounts.
- **2FA (TOTP) Status Synchronization (`SettingsPage.tsx`)**:
  - Fixed `/api/auth/totp/status` and `/api/auth/totp/activate` to return `totpEnabled` alongside `enabled` and standardized `message` error payloads so newly activated 2FA immediately transitions to **Enabled** in the WebUI.

### Added
- **Documentation Portal SEO (`website/`)**:
  - Added build-time `/sitemap.xml` generation alongside `/sitemap-index.xml` with `<lastmod>` timestamps and `x-default` `hreflang` alternates, refined `robots.txt` crawler rules, added explicit XML/text `_headers`, and configured OpenGraph / Twitter social preview metadata (`og-image.jpg`).

### Changed
- Upgraded GitHub Actions (`actions/checkout@v7`, `docker/setup-qemu-action@v4`, `docker/login-action@v4`, `docker/metadata-action@v6`, `docker/build-push-action@v7`) and 16 minor/patch frontend dependencies (`tailwindcss@^3.4.19`, `vite@^8.3.8`, `typescript@^5.9.3`, `axios@^1.21.1`, `@tanstack/react-query@^5.107.1`, `framer-motion@^12.43.0`).

---

## [1.2.2] - 2026-11-15

### Changed
- Upgraded security and documentation dependencies (`@astrojs/starlight@^0.42.11` in `website/package.json`).
- Hardened `.github/dependabot.yml` ignore rules for `npm` (`/` and `/website`), explicitly locking major-version boundaries for `react` / `react-dom` (`<19.0.0`), `tailwindcss` (`<4.0.0`), and `recharts` (`<3.0.0`) while suppressing non-security `semver-major` PRs.

---

## [1.2.1] - 2026-11-15

### Security
- **At-Rest Secret Encryption (`DATA_ENCRYPTION_KEY`)**:
  - Added a mandatory, independent `DATA_ENCRYPTION_KEY` environment variable (`>= 16` chars, fail-fast on startup if missing, weak, or identical to `API_SECRET_KEY`).
  - Encrypted all sensitive secrets at rest (`appKey`, `appSecret`, `consumerKey`, Telegram Bot tokens, SMTP passwords, TOTP seeds, and session secrets) using `HKDF-SHA256` key derivation and `AES-256-GCM` authenticated encryption (`enc:v1:<nonce>:<ciphertext>`).
  - Persisted MCP Bearer tokens strictly as irreversible `SHA-256` `tokenHash` digests with redacted `maskedToken` previews.
- **TOTP Two-Factor Authentication (RFC 6238) & Tiered High-Risk Gatekeeper**:
  - Added native RFC 6238 TOTP 2FA with offline QR code enrollment, 30-second anti-replay window (`lastUsedStep`), and 8 single-use emergency recovery codes (`XXXX-XXXX`).
  - Enforced a global high-risk operation gatekeeper: **when 2FA is not enabled, all high-risk routes return `403 TOTP_SETUP_REQUIRED` and MCP `L3` (`operations`) is forcibly locked to `false`**.
  - Implemented two-tier step-up verification (`strict` fresh TOTP required on every call for OS reinstall, service termination, ownership transfer, API credential mutation, and 2FA disable; `sudo` 5-minute session window for server/VPS reboot, rescue mode, IPMI console, firewall/network changes, MCP governance, and enabling `auto_pay` / `waive_retractation_period`).
- **Consumer Protection & Payment Guardrails (`waiveRetractationPeriod` & `auto_pay`)**:
  - Changed `waiveRetractationPeriod` from enabled-by-default to an explicit opt-in defaulting to `false` (preserving the EU 14-day statutory right of withdrawal by default), with per-task dual switches (`auto_pay` + `waive_retractation_period`) in the WebUI and hardcoded `false` in MCP tools.

### Changed
- Upgraded container base images and CI workflow runtimes to **Node 24 LTS (`node:24-alpine`)** and upgraded GitHub Actions (`actions/setup-node@v7`, `docker/setup-buildx-action@v4`, `peter-evans/dockerhub-description@v5`) and npm dependencies (`lucide-react@^1.72.0`, `sonner@^2.0.8`, `autoprefixer@^10.6.2`, `@radix-ui/react-alert-dialog@^1.2.1`, `@radix-ui/react-tooltip@^1.4.1`, `wrangler@^4.160.1`, `qrcode@^1.5.4`), refining `.github/dependabot.yml` to target `develop` with grouped minor/patch updates.

---

## [1.2.0] - 2026-11-15

### Added
- **Model Context Protocol (MCP) Streamable HTTP Gateway (`/api/mcp`)**:
  - Built-in JSON-RPC 2.0 Streamable HTTP endpoint inspired by `ovh/ovhcloud-cli`, disabled by default and authenticated via an isolated high-entropy Bearer Token (`nxs_mcp_...`) with per-IP brute-force rate limiting.
  - **20 MCP Tools across 3 Configurable Permission Scopes**:
    - `L1 Read-Only` (`11 tools`): Obfuscated account profiles, dedicated servers, VPS instances, IP Edge Firewall rules, service expiration countdowns, order delivery tracking, live datacenter stock, and SKU watcher changes.
    - `L2 Unpaid Procurement` (`5 tools`): Two-step price quote (`order_quote` issuing a 5-minute single-use `quote_id`) and unpaid order creation (`order_create_unpaid`), auto-order queue management, and stock monitor subscriptions.
    - `L3 Reversible Operations` (`4 tools`): Dedicated server / VPS hardware reboot, stuck IPMI session reset, and IP Edge Firewall / Anti-DDoS mitigation updates.
  - **L4 Hardcoded Financial & Destructive Redlines**:
    - Unconditionally locks `auto_pay = false` and `quantity = 1` in code for all MCP orders and queue tasks, enforces a rolling hourly order limit, and permanently blocks OS reinstallation, service termination, raw IPMI console URLs, and credential mutations.
- **Bidirectional Privacy Shield (`Balanced Mode`)**:
  - Automatically pseudonymizes OVH account IDs (`acct-01`), dedicated server hostnames (`bm-01`), VPS instances (`vps-01`), public IPs (`ip-01`, masked as `*.*.*.42`), and order IDs (`ord-01`), while stripping `orderPassword` payment/invoice links and PII from AI context.
- **MCP AI Gateway WebUI Console (`/mcp`) & Audit Event (`mcp.action`)**:
  - Added a dedicated **MCP AI Gateway** page under the System sidebar group with token rotation/revocation, L1–L3 scope switches, multi-account visibility whitelist, one-click client config generators (Claude Code, Claude Desktop, Cursor, Antigravity), alias mapping viewer, and real-time `mcp.action` Notification Center alerts.
- **Repository CI/CD & Container Hardening**:
  - Added `.github/workflows/ci.yml` for automated backend, WebUI Vite build, and Astro Starlight documentation build checks on `main` and `develop`.
  - Added `.github/dependabot.yml` covering all 5 dependency ecosystems (`npm` root, `npm` website, backend dependencies, `docker`, and `github-actions`).
  - Added automated Docker Hub README & short-description synchronization and OCI standard image labels to `.github/workflows/dockerhub.yml`.
  - Added end-to-end `HEALTHCHECK` to `Dockerfile`.

---

## [1.1.1] - 2026-11-12

### Security
- Remediated upstream test-suite dependency advisory `CVE-2025-71176` (`GHSA-6w46-j5rx-g56g`).

---

## [1.1.0] - 2026-11-09

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

## [1.0.1] - 2026-11-09

### Added
- **Official Documentation Site (`website/`)**:
  - Built a full tri-lingual (`en`, `fr`, `zh-CN`) documentation site powered by **Astro Starlight** and **Pagefind** static offline search (`https://ovh-nexus.ellery.me`).
  - Configured assets-only **Cloudflare Workers** deployment (`wrangler.jsonc`, `workers_dev: false`, `preview_urls: false`) with strict CSP and immutable asset caching (`_headers`).

### Changed
- **Repository Storefront & CI Triggers**:
  - Streamlined `README.md`, `docs/README.fr.md`, and `docs/README.zh-CN.md` with quick-start instructions and direct links to the documentation portal.
  - Added `paths-ignore` rules to `.github/workflows/dockerhub.yml` and excluded `website/` in `.dockerignore` so documentation-only commits do not trigger unnecessary Docker image builds.

---

## [1.0.0] - 2026-11-09

### Added
- **Initial Public Release**: Unified Dedicated Server & VPS Fleet Control, Orders & 4-stage Delivery Pipeline, Service Renewal Matrix, Live Availability & Pricing Matrix, Public SKU Watcher, and Pluggable Multi-Channel Notification Center (Telegram Bot & SMTP Email) with tri-lingual (`en`, `fr`, `zh-CN`) WebUI and adaptive Light/Dark OVHcloud Blue themes.
