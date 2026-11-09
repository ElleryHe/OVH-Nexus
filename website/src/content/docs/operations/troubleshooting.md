---
title: Logs & Troubleshooting
description: How to inspect container logs, diagnose common startup or API issues, and reset state safely.
---

## Viewing Logs

OVH Nexus provides two ways to inspect operational logs:

1. **In the Web Console (`/logs`)**:
   - View structured activity events (monitor checks, order attempts, SKU changes, notification deliveries) filtered by level (`INFO`, `WARNING`, `ERROR`) and category.
2. **Container & Host Log Files**:
   - Live container stdout/stderr:
     ```bash
     docker compose logs -f --tail=100
     ```
   - Persistent backend log file on the host:
     ```bash
     tail -f ./logs/app.log
     ```

## Common Issues & Solutions

### 1. Container Exits Immediately with `RuntimeError: API_SECRET_KEY ...`

**Cause**: `API_SECRET_KEY` in `.env` is missing, shorter than 12 characters, or uses one of the sample placeholder strings from the documentation (`replace-with-a-strong-random-secret`, `votre-cle-secrete-robuste`, `请替换为您自己的高强度密码`, `123456`).

**Fix**: Generate a random key and restart:

```bash
sed -i.bak "s|^API_SECRET_KEY=.*|API_SECRET_KEY=$(openssl rand -hex 24)|" .env
docker compose up -d
```

### 2. Login Succeeds (`200 OK`) Over HTTP, But Immediately Logs Out on Next Request

**Cause**: `COOKIE_SECURE=true` is enabled in `.env`, while you are accessing the console over plain `http://localhost:20000` or `http://<ip>:20000`. Browsers refuse to store or send `Secure` cookies over unencrypted HTTP (except on `localhost` in some browsers).

**Fix**: Either access the console via your `https://` reverse proxy domain, or set `COOKIE_SECURE=false` in `.env` when accessing over plain HTTP on a trusted local network.

### 3. `429 Too Many Requests` When Signing In

**Cause**: More than 5 incorrect password attempts were made from the same IP address within 5 minutes (`300` seconds).

**Fix**: Wait for the `Retry-After` window (up to 300 seconds) to expire, or restart the container (`docker compose restart`) after verifying the correct `API_SECRET_KEY` in `.env`. If behind a reverse proxy, ensure `X-Real-IP` is forwarded so all users do not share a single proxy IP.

### 4. OVHcloud API Returns `INVALID_CREDENTIAL` or `This call has not been granted`

**Cause**: The `Consumer Key` has expired, or the token was created without permission for the requested HTTP method/path (for example, missing `GET /vps*` or `POST /order/*`).

**Fix**: Generate a new token at the regional `/createToken/` page with `GET /*`, `POST /*`, `PUT /*`, `DELETE /*` permissions (see [OVHcloud API Credentials](/configuration/ovh-api-credentials/)) and update the account in **API Accounts**.

### 5. Forcing a Public Catalog Refresh

OVH Nexus caches public Eco and Baremetal catalogs in `./cache` for 12 hours (`CATALOG_TTL_SECONDS = 43200`). To force an immediate re-fetch of commercial names and prices:

- Click **Refresh** in the **Live Availability Matrix** (with force refresh), or
- Clear `./cache/*.json` and restart:
  ```bash
  rm -f ./cache/*.json && docker compose restart
  ```
