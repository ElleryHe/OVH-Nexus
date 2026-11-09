# OVH Nexus (Français)

<p align="center">
  <img src="../public/favicon.svg" width="72" height="72" alt="OVH Nexus Logo" />
</p>

<p align="center">
  <strong>Console de Contrôle &amp; d’Approvisionnement OVHcloud</strong><br/>
  Gestion unifiée de flotte Cloud, suivi de livraison en direct et approvisionnement automatisé de serveurs dédiés et VPS.
</p>

<p align="center">
  <a href="../README.md">English</a> ·
  <a href="./README.fr.md"><strong>Français</strong></a> ·
  <a href="./README.zh-CN.md">简体中文</a>
</p>

<p align="center">
  <a href="https://ovh-nexus.ellery.me/fr/" target="_blank" rel="noopener noreferrer"><img src="https://img.shields.io/badge/Docs-ovh--nexus.ellery.me%2Ffr-0050D7?logo=read-the-docs&logoColor=white" alt="Documentation" /></a>
  <a href="https://hub.docker.com/r/elleryhe/ovh-nexus" target="_blank" rel="noopener noreferrer"><img src="https://img.shields.io/badge/Docker-elleryhe%2Fovh--nexus-2496ED?logo=docker&logoColor=white" alt="Docker Hub" /></a>
  <a href="../LICENSE"><img src="https://img.shields.io/badge/License-AGPL%20v3.0-blue.svg" alt="License: AGPL v3.0" /></a>
</p>

---

## Présentation

**OVH Nexus** est une console web auto-hébergée et multi-comptes conçue pour les utilisateurs d’OVHcloud (`ovh-eu`, `ovh-ca`, `ovh-us`). Elle réunit dans un seul conteneur Docker **la gestion quotidienne de votre infrastructure Cloud** et **la surveillance de stock, la veille SKU et la commande automatisée**.

- **Contrôle des Serveurs Dédiés & VPS** : Inventaire matériel, trafic MRTG, console IPMI/KVM, réinstallation OS automatisée (RAID/ZFS, injection de clés SSH), pare-feu IP Edge & mitigation Anti-DDoS, et gestion VPS.
- **Commandes, Livraison & Renouvellements** : Suivi en direct des 4 étapes de livraison (`CREATING` → `VALIDATING` → `DELIVERING` → `INSTALLATION`) et matrice d’expiration des services.
- **Disponibilité en Direct & File d’Achat Auto** : Stock en temps réel enrichi avec les noms commerciaux (`KS-A`, `RISE-1`, `SCALE-a1`), spécifications lisibles et tarification mensuelle exacte par configuration.
- **Veille SKU & Centre de Notifications** : Détection sans clé API des nouveaux modèles ou retraits de serveurs, et alertes multi-canaux (**Telegram Bot** et **Email SMTP**) sur 7 événements.

📚 **Documentation complète (Français)** : <a href="https://ovh-nexus.ellery.me/fr/" target="_blank" rel="noopener noreferrer"><strong>https://ovh-nexus.ellery.me/fr/</strong></a>

---

## Démarrage Rapide (Docker Compose)

OVH Nexus est distribué sous forme d'image Docker multi-architecture tout-en-un (`elleryhe/ovh-nexus:latest`). Il n'est pas nécessaire de cloner le dépôt Git pour le déployer.

### 1. Créer un Répertoire & le Fichier `.env`

```bash
mkdir -p ovh-nexus && cd ovh-nexus

cat > .env << EOF
# Obligatoire : Mot de passe de connexion (au moins 12 caractères)
API_SECRET_KEY=$(openssl rand -hex 24)

# Optionnel : Langue par défaut de l'interface (en | fr | zh-CN)
DEFAULT_LOCALE=fr

# Optionnel : Mettre à true derrière un reverse proxy HTTPS
COOKIE_SECURE=false
EOF
```

### 2. Créer `docker-compose.yml`

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

### 3. Lancer le Conteneur

```bash
docker compose up -d
```

Accédez à `http://localhost:20000` et connectez-vous avec la clé `API_SECRET_KEY` générée dans `.env` (`grep API_SECRET_KEY .env`).

---

## Liens Utiles

| Guide | Lien |
| :--- | :--- |
| **Démarrage Rapide & Installation** | <a href="https://ovh-nexus.ellery.me/fr/getting-started/quick-start/" target="_blank" rel="noopener noreferrer">ovh-nexus.ellery.me/fr/getting-started/quick-start/</a> |
| **Reverse Proxy & HTTPS (`COOKIE_SECURE`)** | <a href="https://ovh-nexus.ellery.me/fr/getting-started/reverse-proxy/" target="_blank" rel="noopener noreferrer">ovh-nexus.ellery.me/fr/getting-started/reverse-proxy/</a> |
| **Variables d’Environnement & Clés API OVH** | <a href="https://ovh-nexus.ellery.me/fr/configuration/environment-variables/" target="_blank" rel="noopener noreferrer">ovh-nexus.ellery.me/fr/configuration/environment-variables/</a> |
| **Guides des Fonctionnalités** | <a href="https://ovh-nexus.ellery.me/fr/features/dedicated-servers/" target="_blank" rel="noopener noreferrer">ovh-nexus.ellery.me/fr/features/dedicated-servers/</a> |
| **Sécurité & Dépannage** | <a href="https://ovh-nexus.ellery.me/fr/operations/security/" target="_blank" rel="noopener noreferrer">ovh-nexus.ellery.me/fr/operations/security/</a> |

---

## Licence & Clause de non-responsabilité

Distribué sous licence **GNU Affero General Public License v3.0 (AGPL-3.0)**. Voir [LICENSE](../LICENSE), [CONTRIBUTING.md](../CONTRIBUTING.md) et [SECURITY.md](../SECURITY.md).

> **OVH Nexus** est un projet open-source indépendant et n’est **pas affilié à, ni approuvé par OVH SAS / OVHcloud**. « OVH » et « OVHcloud » sont des marques déposées d’OVH SAS.
