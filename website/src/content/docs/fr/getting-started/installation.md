---
title: Installation & Mise à Jour
description: Options d'installation détaillées, tags de version, support multi-architecture et procédure de mise à jour.
---

## Images Officielles Docker Hub

Les images officielles sont compilées automatiquement par GitHub Actions et publiées sur [`elleryhe/ovh-nexus`](https://hub.docker.com/r/elleryhe/ovh-nexus) pour les architectures `linux/amd64` (x86_64) et `linux/arm64` (Apple Silicon, AWS Graviton, Raspberry Pi 4/5, Oracle Ampere).

### Tags d'Image Disponibles

| Modèle de Tag | Exemple | Description |
| :--- | :--- | :--- |
| `latest` | `elleryhe/ovh-nexus:latest` | Dernière version stable publiée depuis la branche `main`. |
| `MAJOR.MINOR.PATCH` | `elleryhe/ovh-nexus:1.3.0` | Version sémantique immuable figée. |
| `MAJOR.MINOR` | `elleryhe/ovh-nexus:1.3` | Suit automatiquement le dernier correctif de la série `1.3.x`. |

Pour fixer une version précise dans `docker-compose.yml`, définissez `DOCKER_IMAGE` dans votre fichier `.env` :

```ini
DOCKER_IMAGE=elleryhe/ovh-nexus:1.3.0
```

## Mettre à Jour OVH Nexus

Toutes les données persistantes résidant dans l'unique base SQLite WAL sous `./data:/data` (`/data/nexus.db`), la mise à jour ne prend que quelques secondes et conserve l'intégralité de vos comptes, paramètres 2FA, canaux de notification, files d'achat, journaux et historiques SKU :

```bash
# 1. Télécharger la dernière image
docker compose pull

# 2. Recréer le conteneur
docker compose up -d
```

:::note[Initialisation Automatique Non-Root (`su-exec`, `PUID`/`PGID`)]
Au démarrage du conteneur, `/usr/local/bin/docker-entrypoint.sh` initialise automatiquement `/data` (`0700`) et abaisse les privilèges via `su-exec` vers `PUID:PGID` (`65532:65532` par défaut). Aucun `chown` manuel sur `./data` n'est nécessaire. Les utilisateurs Linux non-root peuvent optionnellement définir `PUID`/`PGID` dans `.env`, et l'exécution sous Kubernetes `runAsNonRoot: true` est prise en charge nativement.
:::

## Compiler depuis les Sources

Si vous préférez construire l'image statique Go multi-étapes localement (`node:24-alpine` + `golang:1.27-alpine` -> `alpine:3.24`) :

```bash
git clone https://github.com/ElleryHe/OVH-Nexus.git
cd OVH-Nexus

cp .env.example .env
# Éditez .env et définissez API_SECRET_KEY et DATA_ENCRYPTION_KEY

docker build -t elleryhe/ovh-nexus:local .
DOCKER_IMAGE=elleryhe/ovh-nexus:local docker compose up -d
```

## Écouter Uniquement sur Localhost

Si OVH Nexus tourne sur un VPS public derrière un reverse proxy local (Caddy ou Nginx), liez le port `20000` uniquement à l'interface de bouclage (`127.0.0.1`) dans `docker-compose.yml` :

```yaml
    ports:
      - "127.0.0.1:20000:8080"
```
