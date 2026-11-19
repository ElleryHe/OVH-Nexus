---
title: Journal des Modifications (Changelog)
description: Historique des versions et nouveautés d'OVH Nexus.
---

:::note[Dernière version : v1.4.0]
Retrouvez les versions officielles et les images Docker multi-architecture sur [GitHub Releases](https://github.com/ElleryHe/OVH-Nexus/releases) et [Docker Hub (`elleryhe/ovh-nexus`)](https://hub.docker.com/r/elleryhe/ovh-nexus).
:::

:::tip[Flux RSS du Journal des Modifications]
Abonnez-vous aux notes de version d'OVH Nexus depuis votre lecteur RSS (Feedly, Inoreader, NetNewsWire, Follow, etc.) : **[`/fr/rss.xml`](https://ovh-nexus.ellery.me/fr/rss.xml)** (Anglais : [`/rss.xml`](https://ovh-nexus.ellery.me/rss.xml) · [`/feed.xml`](https://ovh-nexus.ellery.me/feed.xml) · Chinois : [`/zh-cn/rss.xml`](https://ovh-nexus.ellery.me/zh-cn/rss.xml)).
:::

Toutes les modifications notables d'**OVH Nexus** sont documentées sur cette page selon le format [Keep a Changelog](https://keepachangelog.com/fr/1.1.0/) et la [Gestion Sémantique de Version (SemVer)](https://semver.org/lang/fr/).

## [1.4.0] - 2026-11-19 — Refonte Mémoire & Cache (-99 %), Radar des Promotions & Inspecteur Debug, et Intelligence Promo MCP

### Ajouté (Added)

- **Radar des Promotions OVH, Bannière Repliable & Inspecteur Debug à Double Niveau (`/servers`, `GET /api/catalog/promotions`)** :
  - Ajout de `GET /api/catalog/promotions` agrégeant toutes les campagnes promotionnelles actives du catalogue OVHcloud (`promoCode`, `context`, `autoApplied`, `capacities`, `supportedModes`, durées, compte à rebours d'expiration, économie maximale et compteurs de stock par campagne).
  - Prise en charge complète des promotions bi-canal (récurrent + frais d'installation simultanés, ex. `26game01-v4` avec `BF26_OVH_GAME_15OFF` + `BF26_OVH_GAME_15OFF_SETUP`) sans perte de métadonnées.
  - Ajout d'une **bannière de promotions repliable** en haut du catalogue `/servers` avec filtrage en un clic par campagne et modale d'**Inspecteur des Règles & Debug** clarifiant les remises catalogue automatiques (`context="standard"`) par rapport aux coupons panier manuels.
  - Ajout d'un panneau **Debug promo & tarification** dans la fenêtre de commande (décomposition serveur de base vs supplément matériel RAM/stockage, alerte de mode de facturation manqué, garde-fou anti-saisie d'identifiants automatiques dans le champ Coupon et bouton **Tester au panier** en temps réel via un panier éphémère).
- **Intelligence Promo MCP & Boucle de Décision Engagement/Coupon (`21 Outils`)** :
  - Nouvel outil L1 lecture seule sans clé API **`catalog_promotions`** (portant L1 à **12 outils** et le total à **21 outils**), exposant campagnes, modes éligibles, `autoAppliedCodes` et `agentGuidance`.
  - Enrichissement des outils L2 **`order_quote`** (transmis à **`order_create_unpaid`**) et **`queue_add`** avec les paramètres `pricing_mode` (`default`, `upfront12`, `upfront24`), `coupon` et `max_monthly_price` tout en maintenant `auto_pay=false` et `quantity=1` verrouillés en dur.

### Modifié & Optimisé (Changed & Optimized)

- **Réduction de 99 % du Cache Disque & Refonte de l'Empreinte Mémoire Go** :
  - Suppression des fuites mémoire de graphes `map[string]any` (`Service.rawCatalogs`, `Service.vpsCache` et fichiers `vps_*.json` inutilisés).
  - Remplacement de la désérialisation `map[string]any` par un décodage séquentiel en flux `json.NewDecoder` vers des structures Go typées minimales (plafond décompressé porté de `32 MiB` à `128 MiB`, délai HTTP catalogue public `60s`).
  - Ajout d'un **court-circuit SHA-256 sur flux Gzip** avec garde contre les pannes partielles sur les vérifications SKU toutes les 120 secondes : **zéro décodage JSON, zéro reconstruction et zéro écriture disque** lorsque le catalogue est inchangé.
  - Déduplication de `CatalogDictionary` via `PlanAliases`, filtrage des familles de licences logicielles (`application-license`, `distribution-license`) et projection réseau allégée réduisant `GET /api/catalog/dictionary` de **56 Mo à ~0,5 Mo** (rétablissant la mise en cache `localStorage` navigateur).
  - Passage du cache disque au format compressé atomique `.json.gz` (réduisant `/data/cache/` de **~95 Mo à < 1 Mo**) avec purge automatique au démarrage des anciens fichiers `v1`/`v2`/`v3`/`vps_*.json`, et injection par défaut de `GOMEMLIMIT=256MiB` + `GOGC=50` + `debug.FreeOSMemory()`.

---

## [1.3.4] - 2026-11-18 — Correctifs de Disposition & Promotions de la Matrice de Stock, Restauration Auto de la Base SKU & Optimisation IA `/llms.txt`

### Ajouté (Added)

- **Flux RSS 2.0 Multilingues du Journal des Modifications (`/rss.xml`, `/feed.xml`, `/zh-cn/rss.xml`, `/fr/rss.xml`)** :
  - Génération automatique à la compilation des flux RSS 2.0 standards en anglais (`/rss.xml` et `/feed.xml`), chinois simplifié (`/zh-cn/rss.xml`) et français (`/fr/rss.xml`) avec notes de version HTML complètes et balises d'auto-découverte `<link rel="alternate" type="application/rss+xml">` dans `<head>`.
- **Standard de Contexte IA & LLM (`/llms.txt` & `/llms-full.txt`)** :
  - Génération automatique à la compilation de `dist/llms.txt` (index structuré conforme à [llmstxt.org](https://llmstxt.org) avec résumé du projet, URL canoniques et points d'entrée multilingues) et `dist/llms-full.txt` (documentation Markdown complète nettoyée en un seul fichier), accompagnée d'en-têtes `text/plain; charset=utf-8` et de règles `Allow: /` explicites dans `robots.txt` pour `GPTBot`, `OAI-SearchBot`, `ChatGPT-User`, `ClaudeBot`, `Claude-Web`, `PerplexityBot`, `Google-Extended` et `Applebot-Extended`.
- **Indexation Automatique IndexNow & Protection des Domaines de Prévisualisation** :
  - Ajout de la clé de vérification IndexNow, du script `website/scripts/indexnow.mjs` et du workflow `.github/workflows/docs-indexnow.yml` pour notifier automatiquement `https://api.indexnow.org/indexnow` après chaque déploiement en production.
  - Ajout d'un middleware Cloudflare Worker/Pages (`website/src/worker.ts` et `website/functions/_middleware.ts`), de l'injection `X-Robots-Tag: noindex, nofollow` sur tout domaine différent de `ovh-nexus.ellery.me` (`*.pages.dev`, `*.workers.dev`) et de l'isolation automatique des routes d'archives/anciennes versions (`noindex, follow` + URL canonique récente).

### Corrigé (Fixed)

- **Disposition Anti-Compression de la Matrice de Disponibilité (`/availability`) & Contraste en Mode Clair** :
  - Refonte des lignes de la matrice : le nom du modèle, les badges d'état/promo/génération (`whitespace-nowrap shrink-0`) et le CPU restent alignés à gauche sans retour à la ligne vertical, tandis que le sélecteur de configuration (`sm:w-[260px] lg:w-[300px]`), les frais d'installation, le tarif mensuel et le bouton de surveillance rapide s'alignent proprement à droite.
  - Élargissement des cellules datacenter (`min-w-[116px]` avec drapeau et code `shrink-0 whitespace-nowrap`) pour éliminer la troncature des codes (ex. `SBG` tronqué en `S`) lors de l'affichage du compteur de configurations (`1H-Low · 6 cfg`).
  - Amélioration du contraste des textes de filtres et de badges en mode clair (`text-rose-600`, `text-emerald-700`, `text-amber-700`, `text-purple-700`, `text-cyan-700`) sur `/availability` et `/servers`.
- **Précision du Prix d'Appel par Configuration & Détection des Vraies Promotions (`hasPromo`)** :
  - Correction d'un bogue dans `MergedPlanRow` qui comparait le prix de la configuration la moins chère au prix initial d'une configuration supérieure ou interprétait les modèles sans frais de mise en service (`originalSetupFee === 0`) comme des promotions `FREE SETUP` ; le prix d'appel et le prix barré proviennent désormais strictement de la même configuration la moins chère.
- **Préservation & Auto-Réparation de la Base Initiale SKU (`baseline: true`)** :
  - Correction de `normalizeLoadedSKUs` dans `server/internal/monitor/sku.go` afin de ne jamais écraser les entrées `Baseline: true` existantes, avec migration automatique au démarrage restaurant `Baseline: true` (et vidant `ListedAt`) pour les SKU historiques n'ayant jamais fait l'objet d'un événement `"added"` ou `"replaced"` dans `ChangesJSON`.

---

## [1.3.3] - 2026-11-17 — Connexion Gestionnaire de Mots de Passe (`ADMIN_USERNAME`), Frais d'Installation, Promotions, Engagements Annuels & Codes Promo

### Ajouté (Added)

- **Connexion Console Compatible avec les Gestionnaires de Mots de Passe (`ADMIN_USERNAME`)** :
  - Ajout de la variable d'environnement `ADMIN_USERNAME` (alias compatible `NEXUS_USERNAME`, par défaut `Nexus`, strictement sensible à la casse et vérifiée en temps constant avec `API_SECRET_KEY`) et des champs standards `username` + `current-password` (`autoComplete="on"`) sur le formulaire de connexion pour la saisie automatique par les navigateurs et gestionnaires de mots de passe (1Password, Bitwarden, Mots de passe Apple).
- **Fenêtre Dédiée de Configuration & Commande de Serveur (`/servers`)** :
  - Ajout d'une **Fenêtre de Configuration & Commande de Serveur** dans le Catalogue Serveurs permettant d'inspecter les configurations matérielles (badge `Par défaut`), la tarification mensuelle HT, la disponibilité par datacenter (option **Afficher tout l'inventaire (y compris hors stock)**), les **Options avancées** repliables (Bande passante / vRack), la surveillance rapide, l'ajout à la file d'attente et la commande directe.
- **Affichage des Frais d'Installation (`installation`) avant Commande** :
  - Analyse des frais de mise en service uniques (`capacities: ["installation"]`) et des exonérations promotionnelles sur l'ensemble des modèles et configurations, avec affichage de `setupFee` et du tarif initial barré `originalSetupFee` dans le Catalogue Serveurs (`/servers`), la Matrice de Disponibilité (`/availability`), la Fenêtre de Configuration & Commande et la Surveillance Rapide (`QuickMonitorDialog`).
- **Détection & Filtrage Automatiques des Promotions Catalogue (`promotions`)** :
  - Analyse automatique des promotions OVHcloud actives (remises en pourcentage comme le Black Friday `-20 %` et frais d'installation offerts `FREE SETUP`), affichant le prix remisé avec le prix d'origine barré (`originalMonthlyPrice`), des badges promotionnels (`🔥 -20%`, `🔥 FREE SETUP`) et un onglet de filtre dédié **🔥 Promo** dans `/servers` et `/availability`.
- **Modes d'Engagement Multi-Périodes (`pricingMode` & `duration`)** :
  - Prise en charge de la facturation mensuelle sans engagement (`default` / `P1M`), annuelle prépayée 12 mois (`upfront12` / `P12M`), biennale prépayée 24 mois (`upfront24` / `P24M`) et mensuelle dégressive (`degressivity12` / `degressivity24`) lors de la commande directe, dans les cibles d'achat automatique du moniteur de stock (`targets[]`) et dans la file d'achat (`/queue`).
  - Mise à jour instantanée du total par période, de l'équivalent mensuel HT (`monthlyEquivalentPrice`) et des frais d'installation offerts (`0,00 €` en `upfront12`/`upfront24`), avec évaluation du plafond `maxMonthlyPrice` par rapport à l'équivalent mensuel HT remisé (`periodPrice / intervalMonths`).
- **Prise en Charge des Codes Promo / Coupons (`coupon`) avec Validation Stricte (`INVALID_COUPON`)** :
  - Ajout d'un champ optionnel `coupon` dans la Fenêtre de Configuration & Commande, la Surveillance Rapide et la File d'Achat, appliqué via `POST /order/cart/{cartId}/coupons` avec arrêt immédiat (`400 INVALID_COUPON`, suppression du panier temporaire, journal `WARNING` et alerte `order.failed`) si le code est refusé par OVHcloud afin de ne jamais commander au plein tarif par erreur.

### Modifié (Changed)

- **Grille Modulaire 3 Colonnes à Hauteur Fixe dans le Catalogue Serveurs (`/servers`)** :
  - Refonte du Catalogue Serveurs en une grille de 3 colonnes à hauteur fixe avec disposition modulaire 2×2 des spécifications matérielles (**CPU**, **Mémoire**, **Stockage**, **Bande passante**), ligne dédiée à la disponibilité des datacenters et alignement uniforme des cartes.
- **Identité Visuelle « N » Liquid Glass & Icônes Unifiées** :
  - Unification du logo, des icônes de site (`favicon.svg`, `favicon.ico`, `apple-touch-icon.png`) et de la bannière d'aperçu social OpenGraph avec le nouveau design « N » bleu translucide Liquid Glass.

---

## [1.3.2] - 2026-11-17 — État SKU Annoncé & Remplacé, Surveillance Multi-Cibles par Configuration & Interface Compacte

### Ajouté (Added)

- **État de Publication « Annoncé » (`listingState: "announced"`) & Alertes de Mise en Vente (`saleStarted`)** :
  - Identifie les modèles bare-metal déjà publiés avec leur tarif dans les catalogues publics OVHcloud (`eco` / `baremetalServers`) avant l'ouverture de leur flux de disponibilité (tels que les séries limitées `KS-LE`, `RISE-LE` et `SYS-LE`), avec badges et filtres **Annoncé** (`Announced`) dans la Matrice de Disponibilité (`/availability`) et le Catalogue Serveurs (`/servers`).
  - Émet automatiquement un événement `saleStarted: true` et une notification **Mise en vente** dès qu'un modèle `announced` apparaît dans le flux de disponibilité des datacenters.
- **Détection des Remplacements de Version SKU (`replaced`) & Notifications Groupées par Gamme** :
  - Fusionne le retrait d'un ancien `planCode` et l'ajout d'un nouveau au sein de la même gamme matérielle (`Nom + CPU`, ex. `24sys022 -> 24sys02-v3`) en une seule entrée `replaced` (`replacedFrom`), et regroupe les notifications `sku.changed` par sections (**Nouveau**, **Mise en vente**, **Remplacé**, **Retiré**) sous chaque ligne de produit.
  - Enrichissement des outils MCP `catalog_availability` et `catalog_sku_changes` avec `listingState` (`listed` / `announced` / `presale`), `saleStarted` et `replacedFrom`.
- **Point de Terminaison des Configurations par Modèle (`GET /api/catalog/plans/{planCode}/configs`)** :
  - Renvoie la liste fusionnée de la configuration par défaut et des configurations de disponibilité pour tout `planCode`, incluant `configKey` (`mémoire+stockage`), libellés localisés, codes d'options `options`, marqueur `isDefault`, prix mensuel estimé HT (`estimatedMonthlyPrice`), devise et stock par datacenter.
- **Surveillance de Stock Multi-Cibles par Configuration (`targets[]`) & Plafond Mensuel HT (`maxMonthlyPrice`)** :
  - Chaque abonnement de stock (`StockSubscription.targets[]`) peut désormais définir une ou plusieurs cibles associant chacune une configuration précise (`configKey` + `options`, ou `Toute configuration` en mode notification seule), des datacenters, des alertes de stock, l'option `autoOrder`, la quantité (`quantity`) et un **prix mensuel maximum HT (`maxMonthlyPrice`)**.
  - Avant de valider une commande automatique, le montant mensuel récurrent HT du devis panier en direct (hors frais d'installation uniques) est comparé à `maxMonthlyPrice` ; en cas de dépassement (`PRICE_ABOVE_LIMIT`), la commande est bloquée avec un journal `WARNING` et une alerte `order.failed`.

### Modifié (Changed)

- **Matrice de Disponibilité en Direct Compacte (`/availability`)** :
  - Regroupement à **1 ligne par `planCode`** (les variantes régionales distinctes `-sgp`, `-syd`, `-mum` conservant leur propre ligne) avec sélecteur de configuration intégré permettant de basculer entre `Toutes les configurations` (meilleur état de stock et compteur de configs disponibles) et une configuration précise (prix mensuel HT et stock par datacenter).
- **Cartes Compactes Repliables dans le Catalogue Serveurs (`/servers`)** :
  - Nouvelle grille compacte de 3 à 4 cartes par ligne sur ordinateur, repliées par défaut (nom du modèle, CPU, résumé standard sur 1 ligne, prix mensuel d'appel et badges datacenters compacts).
  - À l'ouverture d'une carte, fusion de la configuration par défaut et des configurations optionnelles en **une seule liste sélectionnable** (filtrée par défaut sur le stock disponible avec case à cocher **Afficher tout l'inventaire (y compris hors stock)** et section repliable **Options avancées**), transmettant directement la configuration choisie vers **Ajouter à la file** et **Surveillance rapide**.
- **Veille SKU Bare-Metal Accélérée & Revérification Rapide à 20 s** :
  - Réduction de l'intervalle par défaut de la veille SKU de `600 s` à `120 s` (avec migration automatique de l'ancien défaut `600 s`), isolation du catalogue bare-metal (`GetBaremetalDictionary`) vis-à-vis du catalogue VPS, et ajout d'une revérification dédiée sous ~20 secondes avant de confirmer le retrait d'un SKU manquant.
- **Migration Sécurisée des Anciens Abonnements d'Achat Auto (`needsConfigSelection`)** :
  - Les anciens abonnements de stock sont convertis en cibles de notification « Toute configuration » ; ceux dont `autoOrder` était actif sans configuration explicite mettent l'achat automatique en pause (`needsConfigSelection: true`) et affichent une bannière d'avertissement sur `/monitor` invitant à choisir une configuration et un plafond de prix.

### Corrigé (Fixed)

- **Suppression de la Pollution par Alias dans le Dictionnaire Catalogue & Détection Prévente** :
  - Séparation des `planCode` réels (`RealPlans`) et des clés d'alias de métadonnées (`baseCode`, `product`) dans `CatalogDictionary`, nettoyage silencieux des anciens alias SKU (`24sys02-1g`, `27risele01`) sans fausse alerte de retrait, et rétro-remplissage de `listedAt` à partir de `firstSeen`.
- **Validation Stricte de la Configuration & de la Quantité lors de l'Achat Automatique** :
  - Suppression du repli implicite sur la première configuration disponible lorsqu'aucune option n'était fournie lors d'un achat déclenché par `stock-monitor` (`CONFIG_REQUIRED_FOR_AUTO_ORDER`), et prise en compte effective du paramètre `quantity` lors de la création du panier.

---

## [1.3.1] - 2026-11-17 — Détection des Modèles en Prévente, Surveillance Rapide, Datacenters Trilingues & Améliorations MCP

### Ajouté (Added)

- **Détection & Filtrage des Serveurs en Prévente / Non Listés** :
  - Identifie automatiquement les `planCode` pré-rackés ou en prévente présents dans les flux de disponibilité OVHcloud en direct avant leur publication dans le catalogue public, avec badges **Prévente** (`Presale`), étiquettes de génération matérielle (`2024`–`2027`) et filtre **Prévente uniquement** (`Presale Only`) sur la Matrice de Disponibilité en Direct (`/availability`) et le Catalogue Serveurs (`/servers`).
- **Fenêtre de Surveillance Rapide en Un Clic (`QuickMonitorDialog`)** :
  - Configurez les alertes de stock par datacenter, le filtrage de sous-configurations matérielles et les règles d'achat automatisé directement depuis n'importe quelle ligne de `/availability` ou `/servers`.
- **Dictionnaire Géographique Trilingue des Datacenters** :
  - Ajout des libellés localisés ville, pays et drapeau (`en` / `fr` / `zh-CN`, ex. `BHS (Beauharnois, Canada 🇨🇦)`, ainsi que des badges compacts `🇨🇦 BHS` avec info-bulle au survol) sur l'ensemble des vues WebUI et modèles de notification, incluant la prise en charge du datacenter de Toronto (`TOR` / `ca-east-tor`).
- **Liaison de Source entre Surveillance de Stock & File d'Achat Auto** :
  - Les tâches créées dans la file d'attente par le moniteur de stock enregistrent désormais `source: "stock-monitor"` avec liens de navigation bidirectionnels entre `/monitor` et `/queue`, et déclenchement unifié côté serveur (suppression des appels `quick-order` redondants côté navigateur).
- **Améliorations de la Passerelle MCP** :
  - Ajout des modes de copie **Entrée Serveur Uniquement** (`Server Entry Only`) et **Fichier Complet** (`Full Config File`) avec conseils de fusion JSON sur `/mcp`, ajout de la pagination `offset` et du filtre `presale_only` sur `catalog_availability`, et alignement de `catalog_sku_changes` sur la chronologie réelle de la Veille SKU.

### Corrigé (Fixed)

- **Suivi Silencieux de l'État Initial des SKU (`baseline: true`)** :
  - Les SKU présents lors du premier déploiement sont désormais marqués comme inventaire existant (`baseline: true`) au lieu de nouveaux ajouts, avec suivi distinct des horodatages `presaleSince` et `listedAt` afin que seules les vraies mises en vente post-initialisation et les transitions `presale -> listed` apparaissent comme **Nouveau**.
- **Inférence de Région Panier pour Toronto (`ca-east-tor`)** :
  - Correction de `InferCartRegion` pour associer les datacenters `tor` / `ca-east-tor*` à la région `"canada"`.
- **Dépendances de Sécurité** :
  - Mise à niveau de `golang.org/x/oauth2` vers `v0.38.0` et nettoyage de la dépendance transitive inutilisée `google.golang.org/protobuf` dans `server/go.mod` (`CVE-2025-22868`, `CVE-2024-24786`).

---

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
