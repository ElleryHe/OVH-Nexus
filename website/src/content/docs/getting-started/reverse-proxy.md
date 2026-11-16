---
title: Reverse Proxy & HTTPS
description: Expose OVH Nexus securely over HTTPS using Caddy, Nginx, or Cloudflare Tunnel.
---

When accessing OVH Nexus over the internet, **always place it behind an HTTPS reverse proxy**, enable `COOKIE_SECURE=true`, and configure `TRUSTED_PROXIES` so client IP extraction and login rate limiting work accurately.

## 1. Configure Secure Cookies & Trusted Proxies in `.env`

Update your `.env` file and restart the container:

```ini
COOKIE_SECURE=true
# Trust local reverse proxy and/or Docker bridge subnet so X-Forwarded-For / X-Real-IP are honored
TRUSTED_PROXIES=127.0.0.1,172.16.0.0/12
```

```bash
docker compose up -d
```

Bind the container port (`8080`) exclusively to `127.0.0.1` in `docker-compose.yml`:

```yaml
    ports:
      - "127.0.0.1:20000:8080"
```

:::caution[How `TRUSTED_PROXIES` & Rate Limiting Work]
OVH Nexus only reads `X-Forwarded-For` and `X-Real-IP` headers when the direct TCP connection originates from an IP or CIDR listed in `TRUSTED_PROXIES`. If `TRUSTED_PROXIES` is empty, proxy headers are ignored to prevent IP spoofing, causing all proxied users to share the proxy's source IP in the login rate limiter (`5 attempts per 300 seconds`).
:::

## 2. Reverse Proxy Configurations

### Option A: Caddy (Automatic HTTPS)

Add the following block to your `/etc/caddy/Caddyfile`:

```txt
nexus.example.com {
    reverse_proxy 127.0.0.1:20000 {
        header_up X-Real-IP {remote_host}
    }
}
```

Reload Caddy:

```bash
sudo systemctl reload caddy
```

### Option B: Nginx + Certbot

Note that `/api/logs/stream` uses **Server-Sent Events (SSE)** (`text/event-stream`), so disabling proxy buffering for `/api/logs/stream` ensures zero-latency real-time log tailing:

```nginx
server {
    listen 443 ssl http2;
    listen [::]:443 ssl http2;
    server_name nexus.example.com;

    ssl_certificate     /etc/letsencrypt/live/nexus.example.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/nexus.example.com/privkey.pem;

    location /api/logs/stream {
        proxy_pass http://127.0.0.1:20000;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_buffering off;
        proxy_cache off;
        proxy_read_timeout 3600s;
    }

    location / {
        proxy_pass http://127.0.0.1:20000;
        proxy_http_version 1.1;

        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;

        # WebSocket / xterm.js serial console support
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_read_timeout 300s;
    }
}
```

### Option C: Cloudflare Tunnel (`cloudflared`)

If your server has no public IPv4 address or you want zero open inbound ports:

1. Point a Cloudflare Tunnel public hostname (e.g., `nexus.example.com`) to `http://127.0.0.1:20000`.
2. Set `COOKIE_SECURE=true` and add `cloudflared`'s source IP/CIDR (e.g., `127.0.0.1` or Docker network CIDR) to `TRUSTED_PROXIES` in `.env`.
3. Optionally enable **Cloudflare Access (Zero Trust)** in front of the hostname for an additional layer of SSO/OTP protection before requests even reach OVH Nexus.
