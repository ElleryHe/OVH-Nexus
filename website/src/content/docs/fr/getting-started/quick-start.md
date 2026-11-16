---
title: Démarrage Rapide
description: Déployez OVH Nexus en 30 secondes avec Docker Compose.
---

OVH Nexus est publié sous forme d'image Docker multi-architecture (`linux/amd64` et `linux/arm64`) sur Docker Hub sous le nom `elleryhe/ovh-nexus` — un conteneur `alpine:3.24` durci (~20 Mo) exécutant un binaire statique unique Go 1.27 sur le port `8080`. Au démarrage, son point d'entrée initialise automatiquement `/data` (`0700`) et abaisse les privilèges via `su-exec` vers l'UID/GID non-root `PUID:PGID` (`65532:65532` par défaut).

## Prérequis

- **Docker Engine** 20.10+ et **Docker Compose** v2 (`docker compose`).
- Au moins **128 Mo de RAM** et **256 Mo d'espace disque**.
- Accès HTTPS sortant (`443`) vers les endpoints API OVHcloud (`*.api.ovh.com`, `api.us.ovhcloud.com`), et éventuellement `api.telegram.org` ou votre serveur SMTP pour les notifications sortantes.

## 1. Créer un Répertoire & `.env`

Créez un dossier pour OVH Nexus, puis générez `API_SECRET_KEY` et `DATA_ENCRYPTION_KEY` :

```bash
mkdir -p ovh-nexus/data && cd ovh-nexus

cat > .env << EOF
# Obligatoire : Mot de passe de connexion et clé API locale (au moins 12 caractères)
API_SECRET_KEY=$(openssl rand -hex 24)

# Obligatoire : Clé maîtresse indépendante de chiffrement au repos AES-256-GCM (au moins 16 caractères)
DATA_ENCRYPTION_KEY=$(openssl rand -hex 32)

# Optionnel : Mettre à true derrière un reverse proxy HTTPS
COOKIE_SECURE=false

# Optionnel : Langue par défaut de l'interface (en | fr | zh-CN)
DEFAULT_LOCALE=fr

# Optionnel : Fuseau horaire du conteneur (nom IANA, par défaut UTC)
TZ=UTC

# Optionnel : Niveau de journalisation (DEBUG | INFO | WARNING | ERROR)
LOG_LEVEL=INFO

# Optionnel : IP/CIDR des proxys inverses de confiance pour X-Forwarded-For / X-Real-IP
TRUSTED_PROXIES=

# Optionnel : UID/GID non-root pour /data et su-exec (65532:65532 par défaut)
PUID=65532
PGID=65532
EOF
```

:::caution[Mot de passe robuste, clé de chiffrement & initialisation automatique du volume]
- OVH Nexus vérifie `API_SECRET_KEY` (>= 12 caractères) et `DATA_ENCRYPTION_KEY` (>= 16 caractères, distincte de `API_SECRET_KEY`) au démarrage.
- Aucun `chown` manuel sur `./data` n'est requis : au démarrage, le point d'entrée initialise automatiquement `/data` (`0700`) et abaisse les privilèges via `su-exec` vers `PUID:PGID` (`65532:65532` par défaut, ou votre propre UID/GID Linux non-root). L'exécution sous Kubernetes `runAsNonRoot: true` est également prise en charge nativement.
:::

## 2. Créer `docker-compose.yml`

```yaml
services:
  ovh-nexus:
    image: ${DOCKER_IMAGE:-elleryhe/ovh-nexus:latest}
    container_name: ovh-nexus
    ports:
      - "20000:8080"
    environment:
      - API_SECRET_KEY=${API_SECRET_KEY:?Veuillez définir une clé API_SECRET_KEY robuste dans .env}
      - DATA_ENCRYPTION_KEY=${DATA_ENCRYPTION_KEY:?Veuillez définir une clé DATA_ENCRYPTION_KEY indépendante (min. 16 caractères) dans .env}
      - COOKIE_SECURE=${COOKIE_SECURE:-false}
      - DEFAULT_LOCALE=${DEFAULT_LOCALE:-fr}
      - TZ=${TZ:-UTC}
      - LOG_LEVEL=${LOG_LEVEL:-INFO}
      - TRUSTED_PROXIES=${TRUSTED_PROXIES:-}
      - PUID=${PUID:-65532}
      - PGID=${PGID:-65532}
    volumes:
      - ./data:/data
    restart: unless-stopped
```

## 3. Démarrer le Conteneur

```bash
docker compose up -d
```

Vérifiez l'état de santé du service :

```bash
curl -s http://localhost:20000/api/health
# Résultat attendu : {"status":"ok"}
```

## 4. Se Connecter à la Console Web

1. Ouvrez `http://localhost:20000` dans votre navigateur.
2. Saisissez la valeur de `API_SECRET_KEY` définie dans votre fichier `.env` (`grep API_SECRET_KEY .env`).
3. Vous pouvez changer la langue (`EN` / `FR` / `中文`) ou le thème (`Système` / `Clair` / `Sombre`) à tout moment depuis l'écran de connexion ou la barre latérale.

:::tip[Aucune clé API OVHcloud requise pour la surveillance publique]
Dès la connexion, vous pouvez utiliser la **Matrice de Disponibilité en Direct**, la **Veille des Changements SKU** et le **Centre de Notifications** sans clé API OVHcloud. Pour gérer vos serveurs achetés, suivre vos commandes ou automatiser vos achats, consultez [Clés API OVHcloud](/fr/configuration/ovh-api-credentials/).
:::
