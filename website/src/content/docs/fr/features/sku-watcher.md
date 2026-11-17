---
title: Veille Publique des Changements SKU
description: Détectez sans clé API les nouveaux modèles de serveurs OVHcloud mis en baie ou retirés du catalogue.
---

Avant qu'OVHcloud ne lance une nouvelle génération de serveurs ou une série limitée (comme `27skle01-v1` ou `27risele01-v1`), les équipes techniques mettent généralement les machines en baie et enregistrent le SKU dans l'API publique `/dedicated/server/datacenter/availabilities` (souvent avec le statut `unavailable`) plusieurs heures ou jours avant son apparition sur le site commercial.

La **Veille Publique des Changements SKU** (`internal/monitor`) surveille l'intégralité de l'étagère publique en tâche de fond et vous alerte dès qu'un modèle ou une sous-configuration est ajouté ou retiré.

## Moteur Anti-Faux-Positifs & Suivi du Cycle de Vie à 3 États

Les API publiques de disponibilité pouvant parfois subir des anomalies temporaires de cache CDN, le moteur intègre plusieurs mécanismes de précision et de protection :

1. **Initialisation Silencieuse (`baseline: true`) & Cycle de Vie à 3 États (`presale` / `announced` / `listed`)** :
   - Lors du premier déploiement (ou après un changement d'endpoint OVH), tous les SKU existants sont enregistrés dans `/data/nexus.db` (`sku_watcher_state`) avec `baseline: true` **sans envoyer d'alerte ni marquer l'inventaire existant comme nouveau**.
   - La veille interroge exclusivement les `planCode` bare-metal réels (`RealPlans` issus des catalogues `eco` et `baremetalServers`, isolés du catalogue VPS et purgés des alias de métadonnées `baseCode`/`product`) et les croise avec le flux de disponibilité :
     - **`presale` (Prévente)** : Présent dans le flux de disponibilité des datacenters avant sa publication dans le catalogue public (enregistre `presaleSince`).
     - **`announced` (Annoncé)** : Publié avec son tarif dans le catalogue public (`eco` / `baremetalServers`), mais sans aucune configuration active dans `/dedicated/server/datacenter/availabilities` (comme les séries limitées `KS-LE`, `RISE-LE` et `SYS-LE`).
     - **`listed` (En vente)** : Actif à la fois dans le catalogue public et dans le flux de disponibilité (enregistre `listedAt`). Lorsqu'un modèle `announced` entre pour la première fois dans le flux de disponibilité, le moteur émet une entrée `added` avec `listingState: "listed"` et `saleStarted: true` (notification **Mise en vente**).
2. **Détection des Remplacements de Version (`replaced`) & Notifications Groupées par Gamme** :
   - Lorsqu'un ancien `planCode` disparaît et qu'un nouveau `planCode` de la même ligne de produit (`Nom + CPU`, ex. `24sys022 -> 24sys02-v3`) apparaît au cours du même cycle, la paire est fusionnée en un unique événement **`replaced`** (`replacedFrom: "24sys022"`) au lieu d'une double alerte ajout/retrait.
   - Les notifications `sku.changed` sont regroupées par sections (**`🆕 Nouveau`**, **`🚀 Mise en vente`**, **`🔄 Remplacé`**, **`➖ Retiré`**) et classées par ligne de produit.
3. **Revérification Rapide sous ~20 Secondes (`missingCounts >= 2`)** :
   - Lorsqu'un SKU connu disparaît lors d'un cycle (`missingCounts == 1`), le moteur planifie automatiquement une **revérification dédiée sous ~20 secondes** sans attendre le prochain cycle complet.
   - Si l'absence est confirmée lors de cette revérification (`missingCounts >= 2`), le SKU est immédiatement marqué `removed` (`Retiré`). S'il réapparaît, le compteur revient à `0`.
4. **Disjoncteur > 30 % & Fusion des Variantes Régionales (`skuMergeRegional`)** :
   - Si le nombre total de SKU renvoyés chute de **plus de 30 %** en un seul cycle (avec une base `>= 5` SKU), le cycle est ignoré comme anomalie API temporaire.
   - Lorsque `skuMergeRegional` est actif (`true` par défaut), les variantes régionales (`-sgp`, `-syd`, `-mum`) sont fusionnées pour éviter les alertes en doublon.

## Règles de Surveillance Configurables

Dans **Surveillance Stock & SKU (`/monitor`)** ou **Centre de Notifications (`/notifications`)**, vous pouvez personnaliser :

| Paramètre | Clé | Défaut | Description |
| :--- | :--- | :--- | :--- |
| **Activer la Veille SKU** | `skuEnabled` | `true` | Active l'analyse périodique bare-metal en arrière-plan. |
| **Intervalle de Scan (s)** | `skuInterval` | `120` (2 min) | Fréquence de vérification en secondes (minimum `60` s ; l'ancien défaut `600` s est automatiquement migré vers `120` s). |
| **Gammes Surveillées** | `skuRanges` | `["KS","RISE","SYS","ADV","SCALE","HGR"]` | Filtre les alertes par famille de serveurs OVHcloud. |
| **Suivre les Sous-Configurations** | `skuTrackConfigs` | `false` | Si `true`, alerte également lorsqu'un SKU existant gagne ou perd une combinaison `mémoire + stockage`. |
| **Fusionner les Suffixes Régionaux** | `skuMergeRegional` | `true` | Regroupe les variantes `-sgp` / `-syd` / `-mum`. |

## Consulter la Chronologie des Changements SKU

La page **Surveillance Stock & SKU (`/monitor`)** et la **Matrice de Disponibilité en Direct (`/availability`)** affichent la **Chronologie des Changements SKU** (persistée dans `/data/nexus.db` et exposée aux clients IA via l'outil MCP `catalog_sku_changes`) avec horodatage, badges (**Nouveau**, **Annoncé**, **Prévente**, **Mise en vente**, **Remplacé (`oldCode → newCode`)**, **Retiré**), nom commercial, spécifications CPU/RAM/stockage et tarif mensuel HT.
