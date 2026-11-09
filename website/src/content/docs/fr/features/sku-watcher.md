---
title: Veille Publique des Changements SKU
description: Détectez sans clé API les nouveaux modèles de serveurs OVHcloud mis en baie ou retirés du catalogue.
---

Avant qu'OVHcloud ne lance une nouvelle génération de serveurs ou une série limitée (comme `27skle01-v1` ou `27risele01-v1`), les équipes techniques mettent généralement les machines en baie et enregistrent le SKU dans l'API publique `/dedicated/server/datacenter/availabilities` (souvent avec le statut `unavailable`) plusieurs heures ou jours avant son apparition sur le site commercial.

La **Veille Publique des Changements SKU** (`backend/sku_watcher.py`) surveille l'intégralité de l'étagère publique en tâche de fond et vous alerte dès qu'un modèle ou une sous-configuration est ajouté ou retiré.

## Moteur Anti-Faux-Positifs & Disjoncteur (Circuit-Breaker)

Les API publiques de disponibilité pouvant parfois subir des anomalies temporaires de cache CDN, `SkuWatcher` intègre 4 mécanismes de protection :

1. **Initialisation Silencieuse de la Référence (`initialized: false → true`)** :
   - Lors du tout premier scan (ou après un changement d'endpoint OVH), `SkuWatcher` enregistre tous les SKU existants dans `data/sku_snapshot.json` comme état de référence **sans envoyer aucune alerte**.
2. **Confirmation de Retrait sur 2 Cycles Consécutifs (`missingCounts >= 2`)** :
   - Lorsqu'un SKU connu disparaît lors d'une vérification, il n'est **pas** immédiatement signalé comme retiré ; son compteur `missingCounts` passe à `1`.
   - Ce n'est que s'il reste absent lors du **cycle consécutif suivant** (`missingCounts >= 2`) qu'un événement `sku.changed` (`SKU Retiré`) est émis. S'il réapparaît entre-temps, le compteur est remis à `0`.
3. **Disjoncteur en Cas de Chute Brusque > 30 %** :
   - Si le nombre total de SKU renvoyés chute de **plus de 30 %** en un seul cycle par rapport à la référence (lorsque la base compte `>= 5` SKU), le cycle est ignoré et consigné comme anomalie API temporaire.
4. **Fusion des Variantes Régionales (`skuMergeRegional`)** :
   - Lorsqu'elle est activée (`true` par défaut), les variantes régionales telles que `24ska01-sgp` et `24ska01-syd` sont fusionnées afin d'éviter les alertes en doublon.

## Règles de Surveillance Configurables

Dans **Centre de Notifications (`/notifications`) → Règles de Veille SKU**, vous pouvez personnaliser :

| Paramètre | Clé | Défaut | Description |
| :--- | :--- | :--- | :--- |
| **Activer la Veille SKU** | `skuEnabled` | `true` | Active l'analyse périodique en arrière-plan. |
| **Intervalle de Scan (s)** | `skuInterval` | `600` (10 min) | Fréquence de vérification en secondes (minimum `60` s). |
| **Gammes Surveillées** | `skuRanges` | `["KS","RISE","SYS","ADV","SCALE","HGR"]` | Filtre les alertes par famille de serveurs OVHcloud. |
| **Suivre les Sous-Configurations** | `skuTrackConfigs` | `false` | Si `true`, alerte également lorsqu'un SKU existant gagne ou perd une combinaison `mémoire + stockage` (`CONFIG_ADDED` / `CONFIG_REMOVED`). |
| **Fusionner les Suffixes Régionaux** | `skuMergeRegional` | `true` | Regroupe les variantes `-sgp` / `-syd` / `-mum`. |

## Consulter la Chronologie des Changements SKU

Le **Centre de Notifications (`/notifications`)** et la **Matrice de Disponibilité en Direct (`/ovh-availability`)** affichent tous deux la **Chronologie Récente des Changements SKU** (`data/sku_changes.json`, conservant jusqu'à 200 événements) avec horodatage, type de changement (`SKU Ajouté`, `SKU Retiré`, `Config Ajoutée`, `Config Retirée`), nom commercial, spécifications RAM/stockage et tarif mensuel.
