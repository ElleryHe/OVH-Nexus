---
title: Démarrage Rapide
description: Déployez OVH Nexus en 30 secondes avec Docker Compose.
---

OVH Nexus est publié sous forme d'image Docker multi-architecture (`linux/amd64` et `linux/arm64`) sur Docker Hub sous le nom `elleryhe/ovh-nexus`.

## Prérequis

- **Docker Engine** 20.10+ et **Docker Compose** v2 (`docker compose`).
- Au moins **256 Mo de RAM** et **500 Mo d'espace disque**.
- Accès HTTPS sortant (`443`) vers les endpoints API OVHcloud (`*.api.ovh.com`, `api.us.ovhcloud.com`), et éventuellement `api.telegram.org` ou votre serveur SMTP pour les notifications.

## 1. Créer un Répertoire de Travail et le Fichier `.env`

Créez un dossier pour OVH Nexus et générez une clé `API_SECRET_KEY` robuste (au moins 12 caractères). Cette clé sert de mot de passe de connexion à l'interface web.

```bash
mkdir -p ovh-nexus && cd ovh-nexus

cat > .env << EOF
# Obligatoire : Mot de passe de la console (au moins 12 caractères)
API_SECRET_KEY=$(openssl rand -hex 24)

# Optionnel : Langue par défaut de l'interface (en | fr | zh-CN)
DEFAULT_LOCALE=fr

# Optionnel : Mettre à true derrière un reverse proxy HTTPS
COOKIE_SECURE=false
EOF
```

:::caution[Mot de passe robuste obligatoire]
OVH Nexus vérifie `API_SECRET_KEY` au démarrage. Si la clé est vide, comporte moins de 12 caractères ou correspond à une valeur d'exemple connue (`votre-cle-secrete-robuste`, `replace-with-a-strong-random-secret`, `123456`), le conteneur refusera de démarrer.
:::

## 2. Créer `docker-compose.yml`

```yaml
services:
  ovh-nexus:
    image: ${DOCKER_IMAGE:-elleryhe/ovh-nexus:latest}
    container_name: ovh-nexus
    ports:
      - "20000:80"
    environment:
      - DEBUG=false
      - ENABLE_API_KEY_AUTH=true
      - API_SECRET_KEY=${API_SECRET_KEY:?Veuillez définir une clé API_SECRET_KEY robuste dans .env}
      - COOKIE_SECURE=${COOKIE_SECURE:-false}
      - DEFAULT_LOCALE=${DEFAULT_LOCALE:-fr}
      - BIND_HOST=127.0.0.1
    volumes:
      - ./data:/app/backend/data
      - ./logs:/app/backend/logs
      - ./cache:/app/backend/cache
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
