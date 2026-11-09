---
title: Security Best Practices
description: Security architecture of OVH Nexus and hardening recommendations for production deployments.
---

OVH Nexus is designed to manage high-value cloud infrastructure and billing accounts. Its backend (`backend/api_auth_middleware.py`, `backend/api_key_config.py`) and frontend container enforce several layers of defense-in-depth by default.

## Built-in Security Controls

1. **Mandatory Strong Password Validation**:
   - The backend validates `API_SECRET_KEY` at startup (`validate_password()`). If unset, shorter than 12 characters, or matching any known placeholder/default key, the process refuses to start.
2. **HttpOnly Server-Side Session Cookies**:
   - Browser authentication uses signed server-side sessions (`ovh_session`) with `HttpOnly=True`, `SameSite=Lax`, and a 12-hour lifetime (`PERMANENT_SESSION_LIFETIME`).
   - The password (`API_SECRET_KEY`) is never stored in browser `localStorage` or exposed to client-side JavaScript after login.
3. **Brute-Force Rate Limiting (`429 Too Many Requests`)**:
   - `/api/auth/login` tracks failed login attempts per client IP (`X-Real-IP` / `remote_addr`). After **5 failed attempts within 300 seconds**, further login attempts from that IP are blocked with HTTP `429` and a `Retry-After` header.
4. **Loopback-Only `X-API-Key` Guard**:
   - Header-based `X-API-Key` authentication is restricted exclusively to direct loopback connections (`127.0.0.1` / `::1`) without `X-Forwarded-For` or `X-Real-IP` headers (`_is_direct_loopback()`). External requests passing through Nginx must authenticate via session cookie.
5. **Constant-Time Secret Comparison**:
   - All password, internal token (`INTERNAL_TOKEN`), API key, and Telegram webhook token comparisons use `hmac.compare_digest()` to prevent timing side-channel attacks.
6. **File Permission Hardening (`0600`)**:
   - At startup, `app.py` sets `os.umask(0o077)` and tightens all existing files in `/app/backend/data` to `0600` (`rw-------`).
7. **Anti-Indexing (`robots.txt`)**:
   - The console's built-in `robots.txt` serves `Disallow: /` so search engines do not index self-hosted login pages.

## Production Hardening Checklist

- [ ] Generate a random `API_SECRET_KEY` of at least 24 bytes (`openssl rand -hex 24`).
- [ ] Bind the Docker container port to localhost (`127.0.0.1:20000:80`) and expose it through an HTTPS reverse proxy (Caddy, Nginx, or Cloudflare Tunnel).
- [ ] Set `COOKIE_SECURE=true` in `.env` when serving over HTTPS.
- [ ] Keep `DEBUG=false` in production (note that even if `ENABLE_API_KEY_AUTH=false` is set accidentally, OVH Nexus ignores it and enforces authentication whenever `DEBUG=false`).
- [ ] Restrict `.env` and `./data` directory permissions on the host (`chmod 600 .env && chmod 700 data`).
