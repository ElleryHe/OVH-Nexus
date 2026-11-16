---
title: Logs & Troubleshooting
description: How to use the SSE live log console, inspect container output, diagnose startup errors, and refresh catalogs.
---

## Viewing & Exporting Logs

OVH Nexus provides two ways to inspect operational logs (all backend events are emitted in standardized English via `log/slog` with automatic secret redaction):

1. **In the Web Console (`/logs`)**:
   - Stream live logs in real time via **Server-Sent Events (`GET /api/logs/stream`)** backed by a 10,000-row ring buffer inside `/data/nexus.db`.
   - Filter by **Module (`source`)** (`server`, `http`, `auth`, `totp`, `vault`, `ovh`, `catalog`, `queue`, `stock-monitor`, `vps-monitor`, `sku-watcher`, `fleet`, `notify`, `mcp`), **Level** (`DEBUG`, `INFO`, `WARNING`, `ERROR`), and **Keyword**, inspect structured attribute pills, and export logs in **`.jsonl`** or **`.log`** format.
2. **Container Stdout**:
   - Live container logs (`LOG_FORMAT=text` or `json`):
     ```bash
     docker compose logs -f --tail=100
     ```

## Common Issues & Solutions

### 1. Container Exits Immediately with `API_SECRET_KEY ...` or `DATA_ENCRYPTION_KEY ...` Error

**Cause**: `API_SECRET_KEY` (`< 12` chars) or `DATA_ENCRYPTION_KEY` (`< 16` chars) in `.env` is missing, identical to the other key, or uses a weak placeholder string.

**Fix**: Generate strong independent keys and restart:

```bash
sed -i.bak "s|^API_SECRET_KEY=.*|API_SECRET_KEY=$(openssl rand -hex 24)|" .env
sed -i.bak "s|^DATA_ENCRYPTION_KEY=.*|DATA_ENCRYPTION_KEY=$(openssl rand -hex 32)|" .env
docker compose up -d
```

### 2. Container Fails to Open `/data/nexus.db` (`permission denied`) or Rejects `PUID`/`PGID`

**Cause**: In standard Docker deployments, the entrypoint initializes `/data` (`0700`) and drops privileges via `su-exec` to `PUID:PGID` (default `65532:65532`) automatically. However, if you run the container under a forced `user:` override (`docker run --user` / Compose `user:`) or Kubernetes `securityContext.runAsNonRoot: true` (`id -u != 0`) while mounting a host directory owned by `root`, the entrypoint runs unprivileged and cannot `chown` `/data`. Separately, passing a non-numeric `PUID` or `PGID` (such as a username) fails validation at boot (`[entrypoint] ERROR: PUID/PGID must be a numeric ...`).

**Fix**: Choose one of the following:

1. **Let the entrypoint drop privileges via `su-exec` (Recommended for Docker Compose)**: Remove any forced `user:` directive so the entrypoint runs briefly as `root` to apply `0700` + `PUID:PGID` ownership (optionally set numeric `PUID` / `PGID` in `.env` using `id -u` / `id -g`) and then executes `ovh-nexus` unprivileged via `su-exec`; **OR**
2. **Match host volume permissions for forced non-root / Kubernetes `runAsNonRoot`**: Keep `user:` or `runAsNonRoot: true` and ensure the mounted host directory ownership or Kubernetes `fsGroup` matches your forced runtime UID:

```bash
chown -R <RUNTIME_UID>:<RUNTIME_GID> ./data
docker compose up -d
```

### 3. Login Succeeds (`200 OK`) Over HTTP, But Immediately Logs Out on Next Request

**Cause**: `COOKIE_SECURE=true` is enabled in `.env`, while you are accessing the console over plain `http://localhost:20000` or `http://<ip>:20000`. Browsers refuse to store or send `Secure` cookies over unencrypted HTTP.

**Fix**: Either access the console via your `https://` reverse proxy domain, or set `COOKIE_SECURE=false` in `.env` when accessing over plain HTTP on a trusted local network.

### 4. `429 Too Many Requests` When Signing In

**Cause**: More than 5 incorrect password attempts were made from the same IP address within 5 minutes (`300` seconds), or requests behind a reverse proxy share a single proxy IP because `TRUSTED_PROXIES` is not configured.

**Fix**: Wait for the `Retry-After` window (up to 300 seconds) or restart (`docker compose restart`) after verifying `API_SECRET_KEY` in `.env`. Behind a reverse proxy, ensure the proxy forwards `X-Real-IP` / `X-Forwarded-For` and add the proxy's IP/CIDR to `TRUSTED_PROXIES` in `.env`.

### 5. OVHcloud API Returns `INVALID_CREDENTIAL` or `This call has not been granted`

**Cause**: The `Consumer Key` has expired, or the token was created without permission for the requested HTTP method/path (for example, missing `GET /vps*` or `POST /order/*`).

**Fix**: Generate a new token at the regional `/createToken/` page with `GET /*`, `POST /*`, `PUT /*`, `DELETE /*` permissions (see [OVHcloud API Credentials](/configuration/ovh-api-credentials/)) and update the account in **API Accounts**.

### 6. Forcing a Public Catalog Refresh

OVH Nexus caches public Eco and Baremetal catalogs for 12 hours. To force an immediate re-fetch of commercial names and prices:

- Click **Refresh** in the **Live Availability Matrix** (`/ovh-availability`), or restart the container (`docker compose restart`).
