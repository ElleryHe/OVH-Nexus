---
title: Environment Variables
description: Complete reference of all environment variables supported by OVH Nexus.
---

OVH Nexus is configured at container startup via environment variables defined in `.env` or passed directly in `docker-compose.yml`.

## Environment Variable Reference

| Variable | Required | Default | Description |
| :--- | :---: | :--- | :--- |
| `API_SECRET_KEY` | **Yes** | *(none)* | WebUI login password and local loopback API key. Must be **at least 12 characters** and not match any known placeholder/weak value. |
| `DEFAULT_LOCALE` | No | `en` | Default interface language on first visit (`en`, `fr`, or `zh-CN`). Users can also switch languages at any time in the UI. |
| `COOKIE_SECURE` | No | `false` | Sets the `Secure` attribute on the `ovh_session` cookie. Set to `true` when serving OVH Nexus over HTTPS behind a reverse proxy. |
| `SECRET_KEY` | No | *(auto-generated)* | Flask session signing secret. If omitted, a random 32-byte key is generated once and persisted in `/app/backend/data/session_secret` (`0600`). |
| `ENABLE_API_KEY_AUTH` | No | `true` | Enables authentication enforcement. Note: in production (`DEBUG=false`), authentication is **always enforced** even if set to `false`. |
| `DEBUG` | No | `false` | Enables Flask debug mode. **Never set `DEBUG=true` in production.** |
| `BIND_HOST` | No | `127.0.0.1` | Host interface the Python backend binds to inside the container (`127.0.0.1` so only the internal Nginx process can reach it). |
| `PORT` | No | `19998` | Internal port for the Python Flask backend (proxied by internal Nginx on container port `80`). |
| `CORS_ORIGINS` | No | `http://localhost:19999,http://127.0.0.1:19999` | Comma-separated list of allowed cross-origin development origins. Not needed in production Docker deployments where Nginx serves the UI and `/api/*` from the same origin. |
| `DOCKER_IMAGE` | No | `elleryhe/ovh-nexus:latest` | Used by `docker-compose.yml` to override the container image tag (e.g., `elleryhe/ovh-nexus:1.1.0`). |

## Generating a Strong `API_SECRET_KEY`

Use any of the following commands to generate a cryptographically strong key:

```bash
# Using OpenSSL (48 hex characters)
openssl rand -hex 24

# Using Python (URL-safe base64)
python3 -c "import secrets; print(secrets.token_urlsafe(24))"
```

## Rejected Weak Keys

To prevent accidental exposure of self-hosted instances, `backend/api_key_config.py` blocks startup if `API_SECRET_KEY` is shorter than 12 characters or matches any of the following values:

- `123456`, `password`, `changeme`
- `ovh-nexus-default-secret-key`
- `replace-with-a-strong-random-secret`
- `votre-cle-secrete-robuste`
- `请替换为您自己的高强度密码`, `你得替换为你自己的密钥`
