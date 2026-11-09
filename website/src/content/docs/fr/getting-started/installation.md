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
| `MAJOR.MINOR.PATCH` | `elleryhe/ovh-nexus:1.0.0` | Version sémantique immuable figée. |
| `MAJOR.MINOR` | `elleryhe/ovh-nexus:1.0` | Suit automatiquement le dernier correctif de la série `1.0.x`. |

Pour fixer une version précise dans `docker-compose.yml`, définissez `DOCKER_IMAGE` dans votre fichier `.env` :

```ini
DOCKER_IMAGE=elleryhe/ovh-nexus:1.0.0
```

## Mettre à Jour OVH Nexus

Toutes les données persistantes étant stockées dans `./data`, `./logs` et `./cache` sur l'hôte, la mise à jour ne prend que quelques secondes et conserve l'intégralité de vos comptes, canaux de notification, files d'achat et historiques SKU :

```bash
# 1. Télécharger la dernière image
docker compose pull

# 2. Recréer le conteneur
docker compose up -d
```

:::note[Migration automatique de la configuration]
Lors d'une mise à jour depuis un format de configuration antérieur (par exemple les champs `tgToken` et `tgChatId` dans `config.json`), `NotificationHub` les migre automatiquement vers un canal Telegram structuré au démarrage.
:::

## Compiler depuis les Sources

Si vous préférez construire l'image localement :

```bash
git clone https://github.com/ElleryHe/OVH-Nexus.git
cd OVH-Nexus

cp .env.example .env
# Éditez .env et définissez API_SECRET_KEY

docker build -t elleryhe/ovh-nexus:local .
DOCKER_IMAGE=elleryhe/ovh-nexus:local docker compose up -d
```

## Écouter Uniquement sur Localhost

Si OVH Nexus tourne sur un VPS public derrière un reverse proxy local (Nginx ou Caddy), liez le port `20000` uniquement à l'interface de bouclage (`127.0.0.1`) dans `docker-compose.yml` :

```yaml
    ports:
      - "127.0.0.1:20000:80"
```
