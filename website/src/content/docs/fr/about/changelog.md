---
title: Journal des Modifications (Changelog)
description: Historique des versions et nouveautés d'OVH Nexus.
---

:::note[Dernière version : v1.2.1]
Retrouvez les versions officielles et les images Docker multi-architecture sur [GitHub Releases](https://github.com/ElleryHe/OVH-Nexus/releases) et [Docker Hub (`elleryhe/ovh-nexus`)](https://hub.docker.com/r/elleryhe/ovh-nexus).
:::

Toutes les modifications notables d'**OVH Nexus** sont documentées sur cette page selon le format [Keep a Changelog](https://keepachangelog.com/fr/1.1.0/) et la [Gestion Sémantique de Version (SemVer)](https://semver.org/lang/fr/).

## [1.2.1] - Chiffrement au Repos AES-256-GCM, 2FA TOTP, Droit de Rétractation UE 14 Jours & Node 24 / Python 3.12 LTS

### Sécurité (Security)

- **Chiffrement des Secrets au Repos (`DATA_ENCRYPTION_KEY`, `backend/crypto_vault.py`)** :
  - Ajout d'une variable d'environnement obligatoire et indépendante `DATA_ENCRYPTION_KEY` (`>= 16` caractères, arrêt immédiat au démarrage si absente, faible ou identique à `API_SECRET_KEY`).
  - Chiffrement de tous les secrets stockés sur disque dans `./data/config.json`, `./data/accounts.json`, `./data/totp_config.json`, `./data/session_secret` et `./data/webhook_secret` via `HKDF-SHA256` + `AES-256-GCM` (`enc:v1:<nonce>:<ciphertext>`), avec migration automatique des fichiers en clair existants.
  - Mise à niveau de `./data/mcp_config.json` pour ne stocker qu'une empreinte irréversible `SHA-256` (`tokenHash`) et un aperçu masqué (`maskedToken`).
- **Authentification à Deux Facteurs TOTP (RFC 6238) & Contrôle d'Accès par Niveau (`backend/totp_service.py`)** :
  - Prise en charge native du 2FA TOTP RFC 6238 avec QR code hors ligne, protection anti-rejeu de 30 secondes et 8 codes de récupération à usage unique (`XXXX-XXXX`).
  - **Lorsque le 2FA n'est pas activé, toutes les opérations à haut risque renvoient `403 TOTP_SETUP_REQUIRED` et le niveau MCP `L3` (`operations`) est verrouillé sur `False`**.
  - Vérification à deux niveaux : niveau `strict` (code TOTP frais requis à chaque appel pour réinstallation OS, résiliation, transfert de propriété, modification des clés API, désactivation 2FA) et niveau `sudo` (fenêtre de 5 minutes pour redémarrage serveur/VPS, mode rescue, console IPMI, pare-feu, configuration MCP et activation de `auto_pay` / `waiveRetractationPeriod`).
- **Protection du Droit de Rétractation UE de 14 Jours (`waiveRetractationPeriod` & `auto_pay`)** :
  - Passage de `waiveRetractationPeriod` à `False` par défaut dans `purchase_server()` et `/api/queue` afin de préserver par défaut le droit légal de rétractation européen de 14 jours, avec interrupteurs dédiés par tâche dans l'interface web.

### Modifié (Changed)

- Mise à niveau des images de base Docker et de la CI vers **Node 24 LTS (`node:24-alpine`)** et **Python 3.12 (`python:3.12-slim`)**, ainsi que des GitHub Actions et dépendances npm.

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
    - Verrouillage inconditionnel de `auto_pay = False` et `quantity = 1` dans le code, quota horaire de commandes, et blocage permanent de la réinstallation d'OS, de la résiliation de service et des clés SSH/API.
- **Bouclier de Confidentialité Bidirectionnel (`Mode Équilibré`, `backend/mcp/privacy_shield.py`)** :
  - Pseudonymisation persistante des comptes (`acct-01`), serveurs dédiés (`bm-01`), VPS (`vps-01`), IP publiques (`ip-01`, `*.*.*.42`) et commandes (`ord-01`), avec suppression des liens de paiement contenant `orderPassword`.
- **Console WebUI Passerelle IA MCP (`/mcp`) & Audit (`mcp.action`)** :
  - Nouvelle page dédiée avec gestion des jetons, liste blanche multi-comptes, générateur de configuration client (Claude Code, Claude Desktop, Cursor, Antigravity), table des alias et alertes `mcp.action` dans le Centre de Notifications.

---

## [1.1.1] - Correctif de Sécurité des Dépendances de Test Backend

### Sécurité (Security)

- **Mise à jour de la dépendance de test Backend (`CVE-2025-71176` / `GHSA-6w46-j5rx-g56g`)** :
  - Mise à jour de `pytest` dans `backend/requirements-dev.txt` de `8.4.2` vers `>=9.0.3` afin de corriger la vulnérabilité liée aux répertoires temporaires UNIX (`CVE-2025-71176`), ramenant le nombre total d'alertes Dependabot (npm et pip) à 0.

---

## [1.1.0] - Durcissement des Dépendances, Schémas d'Architecture & Améliorations Documentaires

### Sécurité (Security)

- **Arbre de Dépendances Sans Vulnérabilité (`0 vulnerabilities`)** :
  - Mise à jour de `react-router-dom` (`>= 7.18.5`) et `vite` (`>= 8.3.6`) dans la console principale, ainsi que de `astro` (`>= 7.4.7`), `@astrojs/starlight` (`>= 0.42.10`) et `sharp` (`>= 0.35.5`) dans le site de documentation `website/`.
  - Ajout de règles npm `overrides` (`esbuild >= 0.28.1`, `glob >= 14.0.2`, `minimatch >= 11.0.1`, `postcss-selector-parser >= 6.1.5`, `zod >= 4.6.6`), corrigeant l'ensemble des 31 alertes GitHub Dependabot.

### Ajouté (Added)

- **Schémas d'Architecture Trilingues** :
  - Ajout des diagrammes d'architecture haute résolution localisés (`architecture-en.jpg`, `architecture-fr.jpg`, `architecture-zh.jpg`) sur la page de présentation.
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

- **Contrôle de la Flotte de Serveurs Dédiés** :
  - Inventaire matériel, graphiques de bande passante MRTG, accès distant IPMI/KVM (`SOL`, `KVM`, `xterm.js`) et contrôle de l'alimentation / mode rescue.
  - Réinstallation OS automatisée (`/dedicated/server/{serviceName}/install/start`) avec RAID matériel/logiciel, profils Proxmox VE ZFS et injection directe de clés publiques SSH (`/me/sshKey`).
  - Gestion des règles du pare-feu IP Edge (`/ip/{ip}/firewall`) et activation de la mitigation permanente Anti-DDoS VAC (`/ip/{ip}/mitigation`).
- **Gestion de la Flotte VPS Achetée** :
  - Vue d'ensemble en direct des instances VPS (`/vps`), ressources vCore/RAM/Disque, adresses IPv4/IPv6, état de service et redémarrage en un clic (`/vps/{serviceName}/reboot`).
- **Commandes, Pipeline de Livraison en Direct & Matrice des Renouvellements** :
  - Synchronisation des commandes officielles OVHcloud (`/me/order`), liens de paiement/facture PDF et suivi en direct des 4 étapes de livraison (`CREATING` → `VALIDATING` → `DELIVERING` → `INSTALLATION`).
  - Matrice unifiée d'expiration des serveurs dédiés et VPS (`/serviceInfos`) avec alertes visuelles `<= 7j` (critique) et `<= 30j` (attention).
- **Matrice de Disponibilité en Direct, Service de Catalogue & Tarification** :
  - Dictionnaire des catalogues publics Eco + Baremetal (`CatalogService`) avec cache disque de 12 h et analyseur déterministe pour les nouveaux SKU non publiés.
  - Formatage lisible de la mémoire et du stockage (`DDR4/DDR5 ECC`, `NVMe/SSD/SATA`, `Soft/HW RAID`) et calcul du prix mensuel exact par sous-configuration (`FQN`) avec tri par prix.
- **Veille Publique des Changements SKU** :
  - Surveillance en arrière-plan sans clé API (`SkuWatcher`) avec initialisation silencieuse, confirmation de retrait sur 2 cycles, disjoncteur en cas de chute > 30 % et fusion des variantes régionales.
- **Centre de Notifications Modulaire** :
  - Canaux multiples **Bot Telegram** et **Email SMTP** (`SSL/TLS`, `STARTTLS`, `Plain`, modèles HTML) abonnés à 7 événements granulaires.
- **Multi-Comptes, Sécurité & Interface Trilingue** :
  - Registre multi-comptes (`ovh-eu`, `ovh-ca`, `ovh-us`) avec isolation par en-tête `X-OVH-Account`.
  - Validation obligatoire de `API_SECRET_KEY`, cookies `HttpOnly`, limitation de débit anti-force-brute, masquage des secrets (`****`) et permissions fichiers `0600`.
  - Interface trilingue **Français (`fr`)**, **English (`en`)**, **简体中文 (`zh-CN`)** et thèmes adaptatifs Clair / Sombre aux couleurs OVHcloud Blue.
