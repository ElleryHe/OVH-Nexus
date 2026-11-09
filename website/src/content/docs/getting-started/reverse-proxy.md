---
title: Reverse Proxy & HTTPS
description: Expose OVH Nexus securely over HTTPS using Nginx, Caddy, or Cloudflare Tunnel.
---

When accessing OVH Nexus over the internet, **always place it behind an HTTPS reverse proxy** and enable `COOKIE_SECURE=true` so the `ovh_session` authentication cookie is only transmitted over encrypted TLS connections.

## 1. Enable Secure Cookies in `.env`

Update your `.env` file and restart the container:

```ini
COOKIE_SECURE=true
```

```bash
docker compose up -d
```

Also bind the container port to `127.0.0.1` in `docker-compose.yml`:

```yaml
    ports:
      - "127.0.0.1:20000:80"
```

:::caution[How Loopback Detection Works]
OVH Nexus uses `X-Forwarded-For` and `X-Real-IP` headers to identify client IP addresses for login rate limiting (`5 attempts per 300 seconds`) and to prevent external requests from spoofing local `X-API-Key` header authentication. Always forward `X-Real-IP` and `X-Forwarded-For` in your reverse proxy.
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

```nginx
server {
    listen 443 ssl http2;
    listen [::]:443 ssl http2;
    server_name nexus.example.com;

    ssl_certificate     /etc/letsencrypt/live/nexus.example.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/nexus.example.com/privkey.pem;

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
2. Set `COOKIE_SECURE=true` in `.env`.
3. Optionally enable **Cloudflare Access (Zero Trust)** in front of the hostname for an additional layer of SSO/OTP protection before requests even reach OVH Nexus.
