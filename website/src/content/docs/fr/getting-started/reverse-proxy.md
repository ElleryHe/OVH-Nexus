---
title: Reverse Proxy & HTTPS
description: Exposez OVH Nexus en toute sécurité via HTTPS avec Nginx, Caddy ou Cloudflare Tunnel.
---

Lorsque vous accédez à OVH Nexus depuis Internet, **placez toujours l'application derrière un reverse proxy HTTPS** et activez `COOKIE_SECURE=true` afin que le cookie de session `ovh_session` ne transite que sur des connexions TLS chiffrées.

## 1. Activer les Cookies Sécurisés dans `.env`

Mettez à jour votre fichier `.env` et redémarrez le conteneur :

```ini
COOKIE_SECURE=true
```

```bash
docker compose up -d
```

Liez également le port du conteneur à `127.0.0.1` dans `docker-compose.yml` :

```yaml
    ports:
      - "127.0.0.1:20000:80"
```

:::caution[Transmission de l'adresse IP réelle]
OVH Nexus utilise les en-têtes `X-Real-IP` et `X-Forwarded-For` pour identifier l'adresse IP du client lors de la limitation des tentatives de connexion (`5 tentatives par tranche de 300 secondes`) et pour bloquer toute usurpation externe de l'en-tête local `X-API-Key`. Veillez à toujours transmettre ces en-têtes dans votre reverse proxy.
:::

## 2. Exemples de Configuration Reverse Proxy

### Option A : Caddy (HTTPS Automatique)

Ajoutez le bloc suivant à `/etc/caddy/Caddyfile` :

```txt
nexus.example.com {
    reverse_proxy 127.0.0.1:20000 {
        header_up X-Real-IP {remote_host}
    }
}
```

Rechargez Caddy :

```bash
sudo systemctl reload caddy
```

### Option B : Nginx + Certbot

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

        # Support WebSocket / console série xterm.js
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_read_timeout 300s;
    }
}
```

### Option C : Cloudflare Tunnel (`cloudflared`)

Si votre serveur ne possède pas d'IPv4 publique ou si vous souhaitez n'ouvrir aucun port entrant :

1. Dirigez un nom d'hôte public Cloudflare Tunnel (ex. `nexus.example.com`) vers `http://127.0.0.1:20000`.
2. Définissez `COOKIE_SECURE=true` dans `.env`.
3. Vous pouvez également activer **Cloudflare Access (Zero Trust)** devant ce sous-domaine pour ajouter une authentification SSO/OTP en amont d'OVH Nexus.
