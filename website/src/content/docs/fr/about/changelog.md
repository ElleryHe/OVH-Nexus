---
title: Journal des Modifications (Changelog)
description: Historique des versions et nouveautés d'OVH Nexus.
---

:::note[Dernière version : v1.1.1]
Retrouvez les versions officielles et les images Docker multi-architecture sur [GitHub Releases](https://github.com/ElleryHe/OVH-Nexus/releases) et [Docker Hub (`elleryhe/ovh-nexus`)](https://hub.docker.com/r/elleryhe/ovh-nexus).
:::

Toutes les modifications notables d'**OVH Nexus** sont documentées sur cette page selon le format [Keep a Changelog](https://keepachangelog.com/fr/1.1.0/) et la [Gestion Sémantique de Version (SemVer)](https://semver.org/lang/fr/).

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
