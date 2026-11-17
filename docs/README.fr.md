# OVH Nexus (Français)

<p align="center">
  <img src="../public/logo.png" width="76" height="76" alt="OVH Nexus Logo" />
</p>

<p align="center">
  <strong>Console de Contrôle &amp; d’Approvisionnement OVHcloud</strong><br/>
  Gestion unifiée de flotte Cloud, suivi de livraison en direct, approvisionnement automatisé de serveurs dédiés et VPS, et passerelle IA MCP sécurisée.
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

**OVH Nexus** est une console web auto-hébergée et multi-comptes conçue pour les utilisateurs d’OVHcloud (`ovh-eu`, `ovh-ca`, `ovh-us`). Compilée sous forme de **binaire statique Go unique** (image Alpine non-root d'environ `20 Mo` intégrant la SPA React et une base SQLite WAL pure Go), elle réunit **la gestion quotidienne de votre infrastructure Cloud** et **la surveillance de stock, la veille SKU et la commande automatisée**.

- **Contrôle des Serveurs Dédiés & VPS** : Inventaire matériel, trafic MRTG, console IPMI/KVM, réinstallation OS automatisée (RAID/ZFS, injection de clés SSH), pare-feu IP Edge & mitigation Anti-DDoS, et gestion VPS.
- **Commandes, Livraison & Renouvellements** : Suivi en direct des 4 étapes de livraison (`CREATING` → `VALIDATING` → `DELIVERING` → `INSTALLATION`) et matrice d’expiration des services.
- **Disponibilité en Direct, Détection des Préventes & File d’Achat Auto** : Stock en temps réel sur tous les datacenters (`🇨🇦 BHS`, `🇨🇦 TOR`, `🇫🇷 GRA`, `🇩🇪 FRA`, `🇸🇬 SGP`, etc.) enrichi avec les noms commerciaux (`KS-A`, `RISE-1`, `SCALE-a1`), spécifications lisibles, tarification mensuelle exacte par configuration, détection automatique des serveurs en **Prévente** (`2024`–`2027` + filtre `Prévente uniquement`) et fenêtre de **Surveillance rapide** en un clic reliée à la file d'achat.
- **Veille SKU & Notifications Sortantes** : Suivi silencieux à l'initialisation (`baseline: true`) des modèles pré-rackés et des passages `presale -> listed` sans clé API, et alertes sortantes multi-canaux (**Telegram Bot** et **Email SMTP**) sur 8 événements avec modèles localisés (`en` / `fr` / `zh-CN`).
- **Journaux Structurés & Streaming SSE (`/logs`)** : Journalisation structurée en anglais (`log/slog`) avec tampon circulaire de 10 000 entrées dans SQLite, flux temps réel SSE, filtrage avancé, masquage des secrets et export `.jsonl`/`.log`.
- **Passerelle IA MCP & Sandbox de Confidentialité (`/api/mcp`)** : Connectez Claude Code, Claude Desktop, Cursor ou Antigravity via Streamable HTTP (JSON-RPC 2.0) avec pseudonymisation bidirectionnelle (`bm-01`, `vps-01`, `ord-01`, IP masquées `*.*.*.42`) et **interdiction stricte du paiement automatique** (`auto_pay=false` verrouillé en dur).
- **Sécurité Durcie en Binaire Unique** : Protection native contre Slowloris, chiffrement au repos `HKDF-SHA256` + `AES-256-GCM` dans SQLite (`/data/nexus.db`), 2FA TOTP RFC 6238 (`strict` / `sudo`) et limitation de débit compatible proxy (`TRUSTED_PROXIES`).

📚 **Documentation complète (Français)** : <a href="https://ovh-nexus.ellery.me/fr/" target="_blank" rel="noopener noreferrer"><strong>https://ovh-nexus.ellery.me/fr/</strong></a>

---

## Démarrage Rapide (Docker Compose)

OVH Nexus est distribué sous forme d'image Docker multi-architecture durcie (`elleryhe/ovh-nexus:latest`) basée sur `alpine:3.24` (écoute sur le port `8080`, initialise automatiquement `/data` et abaisse les privilèges via `su-exec` vers l'UID/GID non-root `PUID:PGID`, `65532:65532` par défaut). Il n'est pas nécessaire de cloner le dépôt Git pour le déployer.

### 1. Créer un Répertoire & `.env`

```bash
mkdir -p ovh-nexus/data && cd ovh-nexus

cat > .env << EOF
# Obligatoire : Mot de passe de connexion (au moins 12 caractères)
API_SECRET_KEY=$(openssl rand -hex 24)

# Obligatoire : Clé maîtresse indépendante de chiffrement au repos AES-256-GCM (au moins 16 caractères)
DATA_ENCRYPTION_KEY=$(openssl rand -hex 32)

# Optionnel : Mettre à true derrière un reverse proxy HTTPS
COOKIE_SECURE=false

# Optionnel : Langue par défaut de l'interface (en | fr | zh-CN)
DEFAULT_LOCALE=fr

# Optionnel : Fuseau horaire du conteneur (par défaut UTC)
TZ=UTC

# Optionnel : Niveau de journalisation (DEBUG | INFO | WARNING | ERROR)
LOG_LEVEL=INFO

# Optionnel : IP/CIDR de confiance des proxys inverses pour X-Forwarded-For / X-Real-IP
TRUSTED_PROXIES=

# Optionnel : UID/GID non-root pour /data et su-exec (65532:65532 par défaut)
PUID=65532
PGID=65532
EOF
```

> **Note** : Aucun `chown` manuel sur `./data` n'est nécessaire. Au démarrage du conteneur, le point d'entrée initialise automatiquement `/data` (`0700`) puis abaisse les privilèges via `su-exec` vers `PUID:PGID` (`65532:65532` par défaut, personnalisable pour les utilisateurs Linux non-root). La compatibilité Kubernetes `runAsNonRoot: true` est également assurée lorsqu'un UID non-root est déjà actif.

### 2. Créer `docker-compose.yml`

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

Distribué sous licence **GNU Affero General Public License v3.0 (AGPL-3.0)**. Voir [LICENSE](../LICENSE), [CHANGELOG.md](../CHANGELOG.md), [CONTRIBUTING.md](../CONTRIBUTING.md) et [SECURITY.md](../SECURITY.md).

> **OVH Nexus** est un projet open-source indépendant et n’est **pas affilié à, ni approuvé par OVH SAS / OVHcloud**. « OVH » et « OVHcloud » sont des marques déposées d’OVH SAS.
