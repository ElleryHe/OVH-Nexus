---
title: Journal des Modifications (Changelog)
description: Historique des versions et nouveautés d'OVH Nexus.
---

:::note[Dernière version : v1.3.0]
Retrouvez les versions officielles et les images Docker multi-architecture sur [GitHub Releases](https://github.com/ElleryHe/OVH-Nexus/releases) et [Docker Hub (`elleryhe/ovh-nexus`)](https://hub.docker.com/r/elleryhe/ovh-nexus).
:::

Toutes les modifications notables d'**OVH Nexus** sont documentées sur cette page selon le format [Keep a Changelog](https://keepachangelog.com/fr/1.1.0/) et la [Gestion Sémantique de Version (SemVer)](https://semver.org/lang/fr/).

## [1.3.0] - Binaire Statique Unique Go 1.27, SQLite WAL & Journaux Structurés SSE

:::caution[Changements Majeurs dans la v1.3.0]
La version `v1.3.0` introduit un binaire statique unique Go (`:8080`), une base SQLite WAL intégrée (`/data/nexus.db` **sans migration automatique** depuis les fichiers JSON `<= v1.2.3`), des notifications purement sortantes et une journalisation `slog` standardisée en anglais. Consultez les **Changements Majeurs** ci-dessous avant toute mise à niveau depuis `<= v1.2.3`.
:::

### Changements Majeurs (Breaking Changes)

- **Port du Conteneur & Volume Unique (`docker-compose.yml`)** :
  - Le port HTTP interne du conteneur passe de `:80` à `:8080` (`ports: ["20000:8080"]`) ; mettez à jour tout mappage ou cible de reverse proxy pointant vers `:80`.
  - Consolidation du stockage dans un volume unique `./data:/data` et suppression des montages séparés `logs`/`cache` ainsi que des anciennes variables `BIND_HOST`, `DEBUG` et `ENABLE_API_KEY_AUTH`.
- **Stockage SQLite WAL Embarqué (`/data/nexus.db` — Répertoire `./data` Propre Requis)** :
  - Remplacement des fichiers JSON autonomes par `/data/nexus.db`. Il n'existe **aucune migration automatique** depuis les fichiers JSON `<= v1.2.3` : la mise à niveau depuis une version antérieure nécessite de partir d'un répertoire `./data` vierge et de reconfigurer la 2FA (TOTP) ainsi que vos comptes API OVHcloud.
- **Centre de Notifications Purement Sortant** :
  - Suppression des routes entrantes de callback Webhook Telegram (`/api/telegram/*`) au profit d'une diffusion uniquement sortante (Telegram Bot API & SMTP) avec liens optionnels `panelUrl`.
- **Journalisation Structurée Standardisée en Anglais** :
  - Remplacement des journaux non structurés localisés par des entrées `slog` en anglais (horodatage UTC RFC3339) avec diffusion temps réel SSE (`/api/logs/stream`).

### Modifié (Changed)

- **Architecture Binaire Statique Unique Go 1.27 (`server/`)** :
  - Compilation en un unique binaire statique Go 1.27 (`CGO_ENABLED=0`) basé sur `net/http`, `chi/v5` et `github.com/ovh/go-ovh` intégrant la SPA React via `//go:embed`.
  - Protection native contre Slowloris et les requêtes excessives (`ReadHeaderTimeout: 5s`, `ReadTimeout: 15s`, `WriteTimeout: 120s`, `IdleTimeout: 60s`, limite `2 Mo`), en-têtes CSP et compression gzip directement sur le port `8080`.
- **Conteneur Minimal Durci (~20 Mo, Point d'Entrée Non-Root `su-exec`, `PUID`/`PGID`)** :
  - `Dockerfile` en 3 étapes (`node:24-alpine` -> `golang:1.27-alpine` -> `alpine:3.24`) intégrant `su-exec` et `/usr/local/bin/docker-entrypoint.sh` pour initialiser automatiquement `/data` (`0700`) et abaisser les privilèges vers `PUID:PGID` (`65532:65532` par défaut, éliminant tout `chown` manuel préalable tout en restant compatible Kubernetes `runAsNonRoot: true`).
  - `docker-compose.yml` simplifié mappant `"20000:8080"` avec un **unique volume persistant `./data:/data`** et variables optionnelles `PUID`/`PGID`.
- **Base SQLite WAL Pure Go (`/data/nexus.db`)** :
  - Consolidation de tous les états (comptes, 2FA TOTP, file d'achat, historique, abonnements stock/VPS, référence SKU, canaux de notification, config/alias/audit MCP et journaux système) dans `/data/nexus.db` (`modernc.org/sqlite`, mode `WAL`, permissions `0700` / `0600`).
  - Chiffrement colonne par colonne `HKDF-SHA256` + `AES-256-GCM` (`enc:v1:...`) pour les clés API, jetons Telegram, mots de passe SMTP et graines TOTP, et hachage `SHA-256` des jetons MCP et codes de secours.
- **Commande Rapide Unifiée & Notifications Uniquement Sortantes** :
  - Standardisation de la commande rapide sur `/api/orders/quick-order` (`auto_pay=false` et `waive_retractation_period=false` par défaut).
  - Simplification du **Centre de Notifications** en répartiteur purement sortant (Telegram Bot API & Email SMTP) avec sélecteur de **Langue de Notification (`locale` : `en`, `fr`, `zh-CN`)** et support complet du fuseau `TZ` (défaut `UTC`).

### Ajouté (Added)

- **Hub `log/slog` Anglais & Console SSE en Direct (`/logs`)** :
  - Émission de journaux structurés en anglais sur 14 modules canoniques (`server`, `http`, `auth`, `totp`, `vault`, `ovh`, `catalog`, `queue`, `stock-monitor`, `vps-monitor`, `sku-watcher`, `fleet`, `notify`, `mcp`) avec masquage automatique des secrets.
  - Tampon circulaire persistant de **10 000 entrées** dans SQLite (`system_logs`), diffusion temps réel **SSE (`GET /api/logs/stream`)** et export **`.jsonl` / `.log`** (`GET /api/logs/export`).
  - Refonte trilingue de `LogsPage.tsx` avec filtres par module/niveau/mot-clé, badges d'attributs et horodatages au fuseau local du navigateur.
- **Extraction d'IP Compatible Proxy (`TRUSTED_PROXIES`) & Fuseau Horaire (`TZ`)** :
  - Liste blanche IP/CIDR pour valider `X-Forwarded-For` / `X-Real-IP` uniquement depuis les proxys de confiance et configuration de `TZ`.

---

## [1.2.3] - Correctif d'Affichage Responsive, Configuration API Initiale, Synchronisation 2FA & SEO

### Corrigé (Fixed)

- **Sélecteurs Tailwind CSS v3 & Navigation Latérale** :
  - Verrouillage de `"postcss-selector-parser": "^6.1.6"` dans `package.json` pour restaurer les sélecteurs responsives (`lg:block`, `md:flex-row`) et ajout d'une barre supérieure dédiée sous `1024px`.
- **Parcours Initial des Comptes API OVH (`/api-accounts`)** :
  - Ajout de **Comptes API OVH** dans le menu latéral, redirection directe de la bannière d'accueil vers `/api-accounts` avec bouton de fermeture rapide, et rappel explicite d'activation 2FA.
- **Synchronisation d'État 2FA (TOTP)** :
  - Harmonisation du champ `totpEnabled` et des messages d'erreur sur `/api/auth/totp/*` pour refléter immédiatement l'activation du 2FA dans l'interface.

### Ajouté (Added)

- **SEO du Portail Documentaire (`robots.txt` & `sitemap.xml`)** :
  - Génération directe de `/sitemap.xml` et `/sitemap-index.xml` avec balises `<lastmod>` et `hreflang="x-default"`, optimisation de `robots.txt` et ajout de l'aperçu social OpenGraph (`og-image.jpg`).

---

## [1.2.2] - Mise à Jour des Dépendances & Gouvernance Dependabot

### Modifié (Changed)

- Mise à niveau des dépendances de sécurité et du portail de documentation (`@astrojs/starlight@^0.42.11` dans `website/package.json`).
- Renforcement des règles `ignore` dans `.github/dependabot.yml` (`/` et `/website`) pour verrouiller les versions majeures de `react` / `react-dom` (`<19.0.0`), `tailwindcss` (`<4.0.0`) et `recharts` (`<3.0.0`), tout en filtrant les mises à jour `semver-major` automatiques non liées à la sécurité.

---

## [1.2.1] - Chiffrement au Repos AES-256-GCM, 2FA TOTP, Droit de Rétractation UE 14 Jours & Node 24 LTS

### Sécurité (Security)

- **Chiffrement des Secrets au Repos (`DATA_ENCRYPTION_KEY`)** :
  - Ajout d'une variable d'environnement obligatoire et indépendante `DATA_ENCRYPTION_KEY` (`>= 16` caractères, arrêt immédiat au démarrage si absente, faible ou identique à `API_SECRET_KEY`).
  - Chiffrement de tous les secrets stockés sur disque (`appKey`, `appSecret`, `consumerKey`, jetons Telegram, mots de passe SMTP, graines TOTP et clés de session) via `HKDF-SHA256` + `AES-256-GCM` (`enc:v1:<nonce>:<ciphertext>`).
  - Stockage irréversible des jetons MCP sous forme d'empreinte `SHA-256` (`tokenHash`) et aperçu masqué (`maskedToken`).
- **Authentification à Deux Facteurs TOTP (RFC 6238) & Contrôle d'Accès par Niveau** :
  - Prise en charge native du 2FA TOTP RFC 6238 avec QR code hors ligne, protection anti-rejeu de 30 secondes et 8 codes de récupération à usage unique (`XXXX-XXXX`).
  - **Lorsque le 2FA n'est pas activé, toutes les opérations à haut risque renvoient `403 TOTP_SETUP_REQUIRED` et le niveau MCP `L3` (`operations`) est verrouillé sur `false`**.
  - Vérification à deux niveaux : niveau `strict` (code TOTP frais requis à chaque appel pour réinstallation OS, résiliation, transfert de propriété, modification des clés API, désactivation 2FA) et niveau `sudo` (fenêtre de 5 minutes pour redémarrage serveur/VPS, mode rescue, console IPMI, pare-feu, configuration MCP et activation de `auto_pay` / `waive_retractation_period`).
- **Protection du Droit de Rétractation UE de 14 Jours (`waiveRetractationPeriod` & `auto_pay`)** :
  - Passage de `waiveRetractationPeriod` à `false` par défaut afin de préserver le droit légal de rétractation européen de 14 jours, avec interrupteurs dédiés par tâche dans l'interface web.

### Modifié (Changed)

- Mise à niveau des images de base Docker et de la CI vers **Node 24 LTS (`node:24-alpine`)**, ainsi que des GitHub Actions (`actions/setup-node@v7`, `docker/setup-buildx-action@v4`, `peter-evans/dockerhub-description@v5`) et dépendances npm.

---

## [1.2.0] - Passerelle IA MCP, Bouclier de Confidentialité & Commandes Non Payées

### Ajouté (Added)

- **Passerelle Model Context Protocol (MCP) Streamable HTTP (`/api/mcp`)** :
  - Point de terminaison JSON-RPC 2.0 intégré inspiré d'`ovh/ovhcloud-cli`, désactivé par défaut et authentifié par un jeton Bearer isolé (`nxs_mcp_...`) avec protection anti-brute-force.
  - **20 Outils MCP répartis sur 3 niveaux de permissions configurables** :
    - `L1 Lecture Seule` (`11 outils`) : Profils de comptes pseudonymisés, serveurs dédiés, VPS, pare-feu IP Edge, échéances d'expiration, suivi des commandes, disponibilité temps réel et changements de SKU.
    - `L2 Commande Non Payée` (`5 outils`) : Devis et création de commande non payée en 2 étapes (`order_quote` → `order_create_unpaid`), file d'attente et alertes de stock.
    - `L3 Opérations Réversibles` (`4 outils`) : Redémarrage matériel serveur dédié/VPS, réinitialisation IPMI et règles du pare-feu IP Edge / Anti-DDoS.
  - **Lignes Rouges Financières & Destructives Verrouillées en Dur (L4)** :
    - Verrouillage inconditionnel de `auto_pay = false` et `quantity = 1` dans le code, quota horaire de commandes, et blocage permanent de la réinstallation d'OS, de la résiliation de service et des clés SSH/API.
- **Bouclier de Confidentialité Bidirectionnel (`Mode Équilibré`)** :
  - Pseudonymisation persistante des comptes (`acct-01`), serveurs dédiés (`bm-01`), VPS (`vps-01`), IP publiques (`ip-01`, `*.*.*.42`) et commandes (`ord-01`), avec suppression des liens de paiement contenant `orderPassword`.
- **Console WebUI Passerelle IA MCP (`/mcp`) & Audit (`mcp.action`)** :
  - Nouvelle page dédiée avec gestion des jetons, liste blanche multi-comptes, générateur de configuration client (Claude Code, Claude Desktop, Cursor, Antigravity), table des alias et alertes `mcp.action` dans le Centre de Notifications.

---

## [1.1.1] - Correctif de Sécurité des Dépendances de Test

### Sécurité (Security)

- Correction de l'avis de sécurité amont `CVE-2025-71176` (`GHSA-6w46-j5rx-g56g`).

---

## [1.1.0] - Durcissement des Dépendances, Schémas d'Architecture & Améliorations Documentaires

### Sécurité (Security)

- **Arbre de Dépendances Sans Vulnérabilité (`0 vulnerabilities`)** :
  - Mise à jour de `react-router-dom` (`>= 7.18.5`) et `vite` (`>= 8.3.6`) dans la console principale, ainsi que de `astro` (`>= 7.4.7`), `@astrojs/starlight` (`>= 0.42.10`) et `sharp` (`>= 0.35.5`) dans le site de documentation `website/`.
  - Ajout de règles npm `overrides` (`esbuild >= 0.28.1`, `glob >= 14.0.2`, `minimatch >= 11.0.1`, `postcss-selector-parser >= 6.1.5`, `zod >= 4.6.6`), corrigeant l'ensemble des 31 alertes GitHub Dependabot.

### Ajouté (Added)

- **Schémas d'Architecture Trilingues** :
  - Ajout des diagrammes d'architecture haute résolution localisés sur la page de présentation.
- **Pied de Page d'Accueil & Liens Externes** :
  - Ajout d'un pied de page dynamique (`© <Année> OVH-Nexus · AGPL-3.0`) sur la page d'accueil de la documentation et ouverture automatique des liens externes dans un nouvel onglet (`target="_blank"`).

### Modifié (Changed)

- **Build Docker Node 22 LTS & Démarrage Rapide Autonome** :
  - Passage de l'étape `frontend-builder` du `Dockerfile` de `node:18-alpine` à `node:22-alpine` (LTS).
  - Enrichissement du Démarrage Rapide dans `README.md`, `docs/README.fr.md` et `docs/README.zh-CN.md` avec un fichier `docker-compose.yml` complet prêt à l'emploi sans cloner le dépôt.
  - Correction des icônes de cartes Starlight (`telegram` et `padlock`) sur la page d'accueil.

---

## [1.0.1] - Site de Documentation & Optimisation CI/CD

### Ajouté (Added)

- **Site de Documentation Officiel (`website/`)** :
  - Création d'un site de documentation trilingue (`en`, `fr`, `zh-CN`) complet propulsé par **Astro Starlight** et la recherche statique hors ligne **Pagefind** sur 69 pages (`https://ovh-nexus.ellery.me`).
  - Configuration du déploiement **Cloudflare Workers** purement statique (`wrangler.jsonc`, `workers_dev: false`, `preview_urls: false`) avec en-têtes de sécurité CSP stricts (`_headers`).

### Modifié (Changed)

- **Présentation du Dépôt & Déclencheurs CI** :
  - Simplification de `README.md`, `docs/README.fr.md` et `docs/README.zh-CN.md` avec démarrage rapide en 30 secondes et liens vers la documentation.
  - Ajout de filtres `paths-ignore` dans `.github/workflows/dockerhub.yml` et exclusion de `website/` dans `.dockerignore` afin que les modifications documentaires ne déclenchent pas de reconstruction d'image Docker.

---

## [1.0.0] - Première Version Publique

### Ajouté (Added)

- **Contrôle de la Flotte de Serveurs Dédiés & VPS, Commandes, Matrice de Disponibilité, Veille SKU & Notifications** :
  - Première version publique unifiant la gestion matérielle, les graphiques MRTG, IPMI/KVM, la réinstallation OS, le pare-feu IP Edge, le suivi de livraison en 4 étapes, les renouvellements, la tarification exacte par FQN, la veille SKU et les alertes Telegram / Email SMTP en interface trilingue (`en`, `fr`, `zh-CN`).
