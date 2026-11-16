---
title: Environment Variables
description: Complete reference of all environment variables supported by OVH Nexus.
---

OVH Nexus is configured at startup via environment variables defined in `.env` or passed directly in `docker-compose.yml`. All configuration is validated fail-fast by `server/internal/config` before the HTTP server binds.

## Environment Variable Reference

| Variable | Required | Default | Description |
| :--- | :---: | :--- | :--- |
| `API_SECRET_KEY` | **Yes** | *(none)* | WebUI login password and local loopback API key. Must be **at least 12 characters** and not match any known weak/placeholder value. |
| `DATA_ENCRYPTION_KEY` | **Yes** | *(none)* | Independent master key for `HKDF-SHA256` + `AES-256-GCM` at-rest encryption of secrets stored in `/data/nexus.db` (`appKey`, `appSecret`, `consumerKey`, Telegram tokens, SMTP passwords, and TOTP secrets). Must be **at least 16 characters** and **must not** equal `API_SECRET_KEY`. |
| `PORT` | No | `8080` | TCP listening port of the Go HTTP server inside the container (mapped to host `20000:8080` by default in `docker-compose.yml`). |
| `DATA_DIR` | No | `/data` | Directory where the SQLite WAL database (`nexus.db`) is stored (directory `0700`, file `0600`). |
| `TZ` | No | `UTC` | IANA timezone applied via `time.LoadLocation` for background schedules and outbound notification timestamps (e.g., `UTC`, `Europe/Paris`, `Asia/Shanghai`). |
| `COOKIE_SECURE` | No | `false` | Sets the `Secure` attribute on the `ovh_session` cookie. Set to `true` when serving OVH Nexus over HTTPS behind a reverse proxy. |
| `DEFAULT_LOCALE` | No | `en` | Default interface language on first visit (`en`, `fr`, or `zh-CN`). Users can also switch languages at any time in the UI. |
| `LOG_LEVEL` | No | `INFO` | Minimum backend log level (`DEBUG`, `INFO`, `WARNING`, or `ERROR`). |
| `LOG_FORMAT` | No | `text` | Container stdout log format (`text` or `json`). Structured records in `/data/nexus.db` and `/api/logs` remain queryable regardless of stdout format. |
| `TRUSTED_PROXIES` | No | *(empty)* | Comma-separated reverse proxy IPs or CIDRs (e.g., `127.0.0.1,172.16.0.0/12`) trusted to supply `X-Forwarded-For` and `X-Real-IP` headers. |
| `PUID` | No | `65532` | Numeric user ID that the container entrypoint (`su-exec`) drops privileges to after initializing `/data` (`0700`). Set to a non-root Linux host UID (`id -u`) if desired; skipped when already running as non-root (`id -u != 0`, e.g., Kubernetes `runAsNonRoot: true`). |
| `PGID` | No | `65532` | Numeric group ID that the container entrypoint (`su-exec`) drops privileges to after initializing `/data` (`0700`). Set to match your Linux host GID (`id -g`) if desired. |
| `SECRET_KEY` | No | *(auto-generated)* | Optional override for the HMAC-SHA256 `ovh_session` signing key. If omitted, a random key is generated once and persisted encrypted in `/data/nexus.db` (`kv_settings`). |
| `CORS_ORIGINS` | No | `http://localhost:19999,http://127.0.0.1:19999` | Comma-separated allowed origins for split local frontend/backend development. Not needed in single-binary production where the WebUI and `/api/*` share one origin. |
| `DOCKER_IMAGE` | No | `elleryhe/ovh-nexus:latest` | Used by `docker-compose.yml` to override the container image tag (e.g., `elleryhe/ovh-nexus:1.3.0`). |

## Generating Strong Keys

Use `openssl` to generate high-entropy values for both required keys:

```bash
# API_SECRET_KEY (48 hex characters)
openssl rand -hex 24

# DATA_ENCRYPTION_KEY (64 hex characters, independent from API_SECRET_KEY)
openssl rand -hex 32
```

## Rejected Weak Keys

To prevent accidental exposure of self-hosted instances, `server/internal/config` blocks startup if `API_SECRET_KEY` (`< 12` chars) or `DATA_ENCRYPTION_KEY` (`< 16` chars) is too short, identical, or matches known placeholders such as:

- `123456`, `123456789012`, `1234567890123456`, `password`, `changeme`, `secret`, `admin123456`
- `ovh-nexus-default-secret-key`, `ovh-nexus-default-encryption-key`
- `replace-with-a-strong-random-secret`, `replace-with-a-strong-encryption-key`
- `votre-cle-secrete-robuste`, `encryption-key-example`
- `请替换为您自己的高强度密码`, `请替换为您自己的高强度加密密钥`, `你得替换为你自己的密钥`
