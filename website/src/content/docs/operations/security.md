---
title: Security Best Practices
description: Security architecture of OVH Nexus and hardening recommendations for production deployments.
---

OVH Nexus is designed to manage high-value cloud infrastructure and billing accounts. Its Go single static binary (`internal/config`, `internal/auth`, `internal/vault`, `internal/httpx`) and non-root Alpine container enforce several layers of defense-in-depth by default.

## Built-in Security Controls

1. **Fail-Fast Key Validation**:
   - `internal/config` validates both `API_SECRET_KEY` (`>= 12` chars) and `DATA_ENCRYPTION_KEY` (`>= 16` chars, must differ from `API_SECRET_KEY`) at startup. If either is missing, weak, or matches a placeholder, the server refuses to start.
2. **Native Slowloris & HTTP Flood Hardening**:
   - The Go `net/http` server enforces strict socket timeouts (`ReadHeaderTimeout: 5s`, `ReadTimeout: 15s`, `WriteTimeout: 120s`, `IdleTimeout: 60s`), a `2 MB` request body limit, and strict Content-Security-Policy (`CSP`) headers directly in the binary.
3. **HttpOnly Server-Side Session Cookies**:
   - Browser authentication uses HMAC-SHA256 signed cookies (`ovh_session`) with `HttpOnly=true`, `SameSite=Lax`, and a 12-hour lifetime.
   - `API_SECRET_KEY` is never stored in browser `localStorage` or exposed to client-side JavaScript after login.
4. **Trusted-Proxy Rate Limiting (`429 Too Many Requests`)**:
   - `/api/auth/login` and TOTP verification enforce per-IP sliding-window rate limits (**5 failed attempts within 300 seconds**, returning HTTP `429` with `Retry-After`).
   - `X-Forwarded-For` and `X-Real-IP` headers are honored **only** when the direct TCP connection comes from an IP/CIDR explicitly listed in `TRUSTED_PROXIES`.
5. **Loopback-Only `X-API-Key` Guard**:
   - Header-based `X-API-Key` authentication is allowed strictly for direct loopback connections (`127.0.0.1` / `::1`) without forwarded proxy headers. External requests arriving through a reverse proxy must authenticate via `ovh_session` cookie.
6. **Constant-Time Secret Comparison**:
   - Password, internal token, API key, and token-hash checks use `crypto/subtle.ConstantTimeCompare` to prevent timing side-channel attacks.
7. **Non-Root Privilege Drop (`su-exec`, `PUID`/`PGID`), `0600` SQLite WAL & AES-256-GCM At-Rest Vault**:
   - On container boot, the Alpine entrypoint (`/usr/local/bin/docker-entrypoint.sh`) initializes `/data` with `0700` permissions and drops privileges in-place via `su-exec` to unprivileged `PUID:PGID` (default `65532:65532`, `nexus:nexus`), and `ovh-nexus` opens `/data/nexus.db` with `0600` (`rw-------`) permissions. Kubernetes `runAsNonRoot: true` (`id -u != 0`) is also supported cleanly.
   - All sensitive credentials (`app_key`, `app_secret`, `consumer_key`, Telegram tokens, SMTP passwords, TOTP seeds, and `session_secret`) are encrypted at rest (`enc:v1:<nonce>:<ciphertext>`) using `HKDF-SHA256` + `AES-256-GCM` derived from `DATA_ENCRYPTION_KEY`, and raw key material is wiped from config memory after initialization.
   - MCP Bearer tokens and TOTP emergency recovery codes are stored strictly as irreversible `SHA-256` hashes.
8. **RFC 6238 TOTP Two-Factor Authentication & Tiered High-Risk Gatekeeper**:
   - **Mandatory 2FA for High-Risk Actions**: Until 2FA is enabled in **Settings → Two-Factor Authentication**, all high-risk operations (server reboot/reinstall/termination/ownership transfer, IPMI console, firewall mutations, API credential changes, enabling `auto_pay` / `waive_retractation_period`, and MCP `L3` tools) are locked (`403 TOTP_SETUP_REQUIRED`).
   - **Two-Tier Step-Up Verification**:
     - **`strict` Tier**: Irreversible actions (OS reinstall, service termination, ownership transfer, API key mutations, 2FA disable) require a fresh 6-digit TOTP or recovery code on every request (with 30s anti-replay enforcement).
     - **`sudo` Tier**: Reversible operational actions (server/VPS reboot, rescue mode, IPMI console, firewall/network changes, MCP config, enabling `auto_pay` or `waive_retractation_period`) grant a **5-minute Sudo window** after verifying once.
9. **Consumer Protection (`waive_retractation_period = false` by Default)**:
   - Both `auto_pay` and `waive_retractation_period` default to `false` across all procurement queues and quick-orders, preserving the EU 14-day statutory right of withdrawal unless explicitly opted in (with 2FA Sudo verification).
10. **Log Secret Redaction & Anti-Indexing (`robots.txt`)**:
   - The `log/slog` logging hub automatically redacts sensitive tokens/keys/passwords before writing to stdout, SQLite, or SSE streams, and built-in `robots.txt` serves `Disallow: /`.

## Production Hardening Checklist

- [ ] Generate a random `API_SECRET_KEY` (`openssl rand -hex 24`) and an independent `DATA_ENCRYPTION_KEY` (`openssl rand -hex 32`).
- [ ] Enable **TOTP Two-Factor Authentication (2FA)** immediately after first login in **Settings** and store your 8 single-use recovery codes offline.
- [ ] Bind the Docker container port to localhost (`127.0.0.1:20000:8080`) and expose it through an HTTPS reverse proxy (Caddy, Nginx, or Cloudflare Tunnel).
- [ ] Set `COOKIE_SECURE=true` and configure `TRUSTED_PROXIES` in `.env` when serving behind HTTPS.
- [ ] Protect `.env` permissions on the host (`chmod 600 .env`); the container entrypoint automatically initializes `/data` with `0700` permissions (`PUID:PGID`, default `65532:65532`) and `ovh-nexus` enforces `0600` on `/data/nexus.db`.
