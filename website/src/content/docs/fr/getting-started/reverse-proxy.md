---
title: Reverse Proxy & HTTPS
description: Exposez OVH Nexus en toute sécurité via HTTPS avec Caddy, Nginx ou Cloudflare Tunnel.
---

Lorsque vous accédez à OVH Nexus depuis Internet, **placez toujours l'application derrière un reverse proxy HTTPS**, activez `COOKIE_SECURE=true` et configurez `TRUSTED_PROXIES` pour que la détection de l'adresse IP client et la limitation de débit fonctionnent correctement.

## 1. Configurer les Cookies Sécurisés & `TRUSTED_PROXIES` dans `.env`

Mettez à jour votre fichier `.env` et redémarrez le conteneur :

```ini
COOKIE_SECURE=true
# Faites confiance au reverse proxy local et/ou au réseau Docker pour X-Forwarded-For / X-Real-IP
TRUSTED_PROXIES=127.0.0.1,172.16.0.0/12
```

```bash
docker compose up -d
```

Liez également le port du conteneur (`8080`) à `127.0.0.1` dans `docker-compose.yml` :

```yaml
    ports:
      - "127.0.0.1:20000:8080"
```

:::caution[Fonctionnement de `TRUSTED_PROXIES`]
OVH Nexus n'accepte les en-têtes `X-Real-IP` et `X-Forwarded-For` que lorsque la connexion TCP directe provient d'une adresse IP ou d'un CIDR listé dans `TRUSTED_PROXIES`. Si `TRUSTED_PROXIES` est vide, ces en-têtes sont ignorés pour empêcher toute usurpation d'IP, et tous les utilisateurs derrière le proxy partageront l'IP du proxy dans le limiteur de tentatives de connexion (`5 tentatives / 300 secondes`).
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

L'endpoint `/api/logs/stream` utilise des **Server-Sent Events (SSE)** (`text/event-stream`) : désactivez `proxy_buffering` pour ce chemin afin de recevoir les journaux en temps réel sans latence :

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
2. Définissez `COOKIE_SECURE=true` et ajoutez l'IP/CIDR de `cloudflared` (ex. `127.0.0.1` ou le sous-réseau Docker) dans `TRUSTED_PROXIES`.
3. Vous pouvez également activer **Cloudflare Access (Zero Trust)** devant ce sous-domaine pour ajouter une authentification SSO/OTP en amont d'OVH Nexus.
