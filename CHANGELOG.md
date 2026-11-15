# Changelog

All notable changes to **OVH Nexus** are documented in this file following [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and [Semantic Versioning](https://semver.org/).

📚 **Tri-Lingual Online Changelog**:
- **English**: <a href="https://ovh-nexus.ellery.me/about/changelog/" target="_blank" rel="noopener noreferrer">https://ovh-nexus.ellery.me/about/changelog/</a>
- **Français**: <a href="https://ovh-nexus.ellery.me/fr/about/changelog/" target="_blank" rel="noopener noreferrer">https://ovh-nexus.ellery.me/fr/about/changelog/</a>
- **简体中文**: <a href="https://ovh-nexus.ellery.me/zh-cn/about/changelog/" target="_blank" rel="noopener noreferrer">https://ovh-nexus.ellery.me/zh-cn/about/changelog/</a>

---

## [1.2.2] - 2026-11-15

### Changed
- Upgraded backend dependencies (`cryptography>=50.0.2` in `backend/requirements.txt`, `pytest>=9.2.0` in `backend/requirements-dev.txt`) and documentation portal framework (`@astrojs/starlight@^0.42.11` in `website/package.json`).
- Hardened `.github/dependabot.yml` ignore rules for `npm` (`/` and `/website`), explicitly locking major-version boundaries for `react` / `react-dom` (`<19.0.0`), `tailwindcss` (`<4.0.0`), and `recharts` (`<3.0.0`) while suppressing non-security `semver-major` PRs.

---

## [1.2.1] - 2026-11-15

### Security
- **At-Rest Secret Encryption (`DATA_ENCRYPTION_KEY`, `backend/crypto_vault.py`)**:
  - Added a mandatory, independent `DATA_ENCRYPTION_KEY` environment variable (`>= 16` chars, fail-fast on startup if missing, weak, or identical to `API_SECRET_KEY`).
  - Encrypts all sensitive secrets at rest in `./data/config.json` (`appKey`, `appSecret`, `consumerKey`, `tgToken`, `notifications.channels[*].config.token/password`), `./data/accounts.json`, `./data/totp_config.json`, and `./data/session_secret` / `./data/webhook_secret` using `HKDF-SHA256` key derivation and `AES-256-GCM` authenticated encryption (`enc:v1:<nonce>:<ciphertext>`), with automatic in-place migration of existing plaintext files.
  - Upgraded `./data/mcp_config.json` to persist only an irreversible `SHA-256` `tokenHash` and redacted `maskedToken` preview, wiping plaintext MCP Bearer tokens from disk.
- **TOTP Two-Factor Authentication (RFC 6238) & Tiered High-Risk Gatekeeper (`backend/totp_service.py`)**:
  - Added native RFC 6238 TOTP 2FA with offline QR code enrollment, 30-second anti-replay window (`lastUsedStep`), and 8 single-use emergency recovery codes (`XXXX-XXXX`).
  - Enforced a global high-risk operation gatekeeper: **when 2FA is not enabled, all high-risk routes return `403 TOTP_SETUP_REQUIRED` and MCP `L3` (`operations`) is forcibly locked to `False`**.
  - Implemented two-tier step-up verification (`strict` fresh TOTP required on every call for OS reinstall, service termination, ownership transfer, API credential mutation, and 2FA disable; `sudo` 5-minute session window for server/VPS reboot, rescue mode, IPMI console, firewall/network changes, MCP governance, and enabling `auto_pay` / `waiveRetractationPeriod`).
- **Consumer Protection & Payment Guardrails (`waiveRetractationPeriod` & `auto_pay`)**:
  - Changed `waiveRetractationPeriod` in `purchase_server()` and `/api/queue` from hardcoded `True` to an explicit opt-in defaulting to `False` (preserving the EU 14-day statutory right of withdrawal by default), with per-task dual switches (`auto_pay` + `waive_retractation_period`) in the WebUI and hardcoded `False` in MCP tools.

### Changed
- Upgraded container base images and CI workflow runtimes to **Node 24 LTS (`node:24-alpine`)** and **Python 3.12 (`python:3.12-slim`)**.
- Upgraded GitHub Actions (`actions/setup-node@v7`, `actions/setup-python@v7`, `docker/setup-buildx-action@v4`, `peter-evans/dockerhub-description@v5`) and npm dependencies (`lucide-react@^1.72.0`, `sonner@^2.0.8`, `autoprefixer@^10.6.2`, `@radix-ui/react-alert-dialog@^1.2.1`, `@radix-ui/react-tooltip@^1.4.1`, `wrangler@^4.160.1`, `qrcode@^1.5.4`), and refined `.github/dependabot.yml` to target `develop` with grouped minor/patch updates.

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
    - Unconditionally locks `auto_pay = False` and `quantity = 1` in code for all MCP orders and queue tasks, enforces a rolling hourly order limit, and permanently blocks OS reinstallation, service termination, raw IPMI console URLs, and credential mutations.
- **Bidirectional Privacy Shield (`Balanced Mode`, `backend/mcp/privacy_shield.py`)**:
  - Automatically pseudonymizes OVH account IDs (`acct-01`), dedicated server hostnames (`bm-01`), VPS instances (`vps-01`), public IPs (`ip-01`, masked as `*.*.*.42`), and order IDs (`ord-01`), while stripping `orderPassword` payment/invoice links and PII from AI context.
- **MCP AI Gateway WebUI Console (`/mcp`) & Audit Event (`mcp.action`)**:
  - Added a dedicated **MCP AI Gateway** page under the System sidebar group with token rotation/revocation, L1–L3 scope switches, multi-account visibility whitelist, one-click client config generators (Claude Code, Claude Desktop, Cursor, Antigravity), alias mapping viewer, and real-time `mcp.action` Notification Center alerts.
- **Repository CI/CD & Container Hardening**:
  - Added `.github/workflows/ci.yml` for automated backend `pytest`, WebUI Vite build, and Astro Starlight documentation build checks on `main` and `develop`.
  - Added `.github/dependabot.yml` covering all 5 dependency ecosystems (`npm` root, `npm` website, `pip` backend, `docker`, and `github-actions`).
  - Added automated Docker Hub README & short-description synchronization and OCI standard image labels to `.github/workflows/dockerhub.yml`.
  - Added end-to-end `HEALTHCHECK` and fail-fast dual-process supervision (`/start.sh`) to `Dockerfile`.

---

## [1.1.1] - 2026-11-12

### Security
- Upgraded `pytest` in `backend/requirements-dev.txt` from `8.4.2` to `>=9.0.3` to remediate `CVE-2025-71176` (`GHSA-6w46-j5rx-g56g`).

---

## [1.1.0] - 2026-11-09

### Security
- Upgraded frontend and documentation dependencies (`react-router-dom >= 7.18.5`, `vite >= 8.3.6`, `astro >= 7.4.7`, `@astrojs/starlight >= 0.42.10`, `sharp >= 0.35.5`) and transitive `overrides`, resolving all 31 GitHub Dependabot alerts.

### Added
- Tri-lingual high-resolution system architecture diagrams, dynamic copyright/license home footer, external link `target="_blank"` handling, and standalone 3-step Docker Compose Quick Start.

---

## [1.0.1] - 2026-11-09

### Added
- Official tri-lingual (`en`, `fr`, `zh-CN`) documentation portal (`website/`) powered by Astro Starlight and Pagefind offline search (`https://ovh-nexus.ellery.me`).

---

## [1.0.0] - 2026-11-09

### Added
- Initial public release unifying Dedicated Server & VPS Fleet Control, Orders & 4-stage Delivery Pipeline, Service Renewal Matrix, Live Availability & Pricing Matrix, Public SKU Watcher, and Pluggable Multi-Channel Notification Center (Telegram Bot & SMTP Email).
