---
title: Matrice de Disponibilité en Direct & Tarification
description: Consultez le stock bare-metal OVHcloud en temps réel, les spécifications matérielles lisibles et le prix mensuel exact par configuration.
---

La **Matrice de Disponibilité OVH en Direct** (`/ovh-availability`) interroge l'API publique `/dedicated/server/datacenter/availabilities` d'OVHcloud et enrichit chaque résultat grâce à `internal/catalog` (`/order/catalog/public/eco` et `/order/catalog/public/baremetal`).

:::tip[Aucune clé API requise]
L'API de disponibilité ainsi que les catalogues Eco et Baremetal sont des points de terminaison publics d'OVHcloud. Vous pouvez consulter les stocks et les tarifs dans tous les datacenters immédiatement après le déploiement d'OVH Nexus.
:::

## Noms Commerciaux & Formatage Lisible du Matériel

Les réponses brutes d'OVHcloud n'exposent que des codes techniques tels que `24ska01`, `memory: ram-64g-ecc-3200` et `storage: raid-r1-2x480ssd-s10-2x6000sa`. OVH Nexus les traduit automatiquement en informations claires :

1. **Résolution du Nom Commercial** :
   - Associe chaque `planCode` aux catalogues publics Eco (`Kimsufi`, `So you Start`, `Rise`) et Baremetal (`Advance`, `Scale`, `High Grade`) mis en cache pendant 12 heures.
   - Pour les nouveaux modèles récemment rackés mais pas encore publiés dans le catalogue commercial (ex. `27skle01-v1`), utilise un analyseur déterministe (`KS-LE-1`, `KS-A`, `RISE-1`, `SCALE-a1`).
   - Fusionne automatiquement les variantes régionales APAC (`-sgp`, `-syd`, `-mum`, `-apac`) et les suffixes de version (`-v1`, `-v2`).
2. **Traduction Localisée des Spécifications (`en` / `fr` / `zh-CN`)** :
   - **Mémoire RAM** : Convertit `ram-64g-ecc-3200` en `64 Go DDR4 ECC 3200 MHz`.
   - **Stockage & Disques Système** : Convertit `softraid-2x512nvme` en `2 × 512 Go NVMe (Soft RAID)` et sépare `raid-r1-2x480ssd-s10-2x6000sa` en disque système (`2 × 480 Go SSD, HW RAID 1`) et disques de données (`2 × 6 To SATA, HW RAID 10`).

## Tarification Mensuelle HT par Configuration & Vues Compactes

Contrairement aux outils classiques qui n'affichent que le prix d'appel d'un `planCode`, OVH Nexus calcule le **prix mensuel estimé HT pour chaque configuration matérielle (`configKey` : `mémoire+stockage`)** via `/api/catalog/availability`, `/api/catalog/plans` et `GET /api/catalog/plans/{planCode}/configs` :

```text
Prix Mensuel Estimé HT = Prix Mensuel du Plan de Base (P1M)
                       + Supplément Mensuel de l'Option RAM (si > 0)
                       + Supplément Mensuel de l'Option Stockage (si > 0)
```

- **Matrice de Disponibilité Compacte à 1 Ligne par `planCode` (`/availability`)** :
  - Regroupe les configurations d'un même `planCode` sur **une seule ligne** (tout en conservant des lignes distinctes pour les variantes régionales telles que `-sgp`, `-syd` et `-mum`).
  - Chaque ligne intègre un **menu déroulant de configuration** :
    - **Toutes les configurations (`N`)** *(par défaut)* : Chaque cellule datacenter affiche le **meilleur état de disponibilité** parmi les configurations du modèle ainsi que le nombre de configurations en stock.
    - **Configuration spécifique (`Mémoire + Stockage — XX,XX €/mois · K DCs`)** : La sélection d'une configuration affiche immédiatement son tarif mensuel HT et son état de stock exact dans chaque datacenter.
- **Cartes Compactes Repliables du Catalogue Serveurs (`/servers`)** :
  - Grille compacte de 3 à 4 cartes par ligne sur ordinateur, **repliées par défaut** (nom commercial, `planCode`, CPU, résumé standard sur 1 ligne, prix d'appel `à partir de XX,XX €/mois`, badges datacenters compacts et badge d'état).
  - Le dépliage d'une carte présente **une liste unifiée de configurations sélectionnables** (la configuration par défaut étant marquée `Défaut` et présélectionnée, avec tarif mensuel HT et disponibilité par datacenter). Par défaut, seuls les datacenters et configurations en stock sont affichés ; cochez **Afficher tout l'inventaire (y compris hors stock)** pour tout voir, ou dépliez **Options avancées** pour la bande passante et le vRack.
- Triez par **Prix croissant** ou **Prix décroissant**, ou filtrez par gamme (`KS`, `SYS`, `RISE`, `ADV`, `SCALE`, `HGR`), datacenter (`GRA`, `SBG`, `RBX`, `FRA`, `BHS`, `TOR`, `SGP`, `SYD`, etc.) et statut de publication (`Tous`, `En vente`, `Annoncé`, `Prévente`, `Nouveau`).

## États de Publication (`listed`, `announced`, `presale`) & Surveillance Multi-Cibles

OVH Nexus distingue trois états de cycle de vie dans `/availability` et `/servers` :

- **En vente (`listingState: "listed"`)** : Présent dans le catalogue public (`eco` / `baremetalServers`) **et** actif dans le flux de disponibilité des datacenters.
- **Annoncé (`listingState: "announced"`)** : Publié avec son tarif officiel dans le catalogue public OVHcloud mais **pas encore ouvert** dans `/dedicated/server/datacenter/availabilities` (comme les séries limitées `KS-LE`, `RISE-LE` et `SYS-LE` avant l'ouverture des ventes). Dès l'apparition du flux de stock, une alerte **Mise en vente (`saleStarted: true`)** est émise.
- **Prévente (`listingState: "presale"`)** : Nouveaux `planCode` (`2024`–`2027`) déjà visibles dans le flux technique de disponibilité avant leur publication au catalogue public.
- **Surveillance Rapide Multi-Cibles (`QuickMonitorDialog`)** : Un clic sur **Surveillance rapide** ou **Ajouter à la file** transmet la configuration sélectionnée (`configKey`, `options`, prix estimé) et les datacenters pour configurer une ou plusieurs cibles avec quantité et **plafond mensuel HT (`maxMonthlyPrice`)**.
- **Dictionnaire Géographique Trilingue des Datacenters** : Tous les badges et filtres de datacenters affichent la ville, le pays et le drapeau localisés (`en` / `fr` / `zh-CN`), par exemple `BHS (Beauharnois, Canada 🇨🇦)` et `TOR (Toronto, Canada 🇨🇦)` en vue détaillée ou `🇨🇦 BHS` / `🇨🇦 TOR` en badge compact avec info-bulle au survol.
