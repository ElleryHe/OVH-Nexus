---
title: Matrice de Disponibilité en Direct & Tarification
description: Consultez le stock bare-metal OVHcloud en temps réel, les spécifications matérielles lisibles et le prix mensuel exact par configuration.
---

La **Matrice de Disponibilité OVH en Direct** (`/ovh-availability`) interroge l'API publique `/dedicated/server/datacenter/availabilities` d'OVHcloud et enrichit chaque résultat grâce à `CatalogService` (`/order/catalog/public/eco` et `/order/catalog/public/baremetal`).

:::tip[Aucune clé API requise]
L'API de disponibilité ainsi que les catalogues Eco et Baremetal sont des points de terminaison publics d'OVHcloud. Vous pouvez consulter les stocks et les tarifs dans tous les datacenters immédiatement après le déploiement d'OVH Nexus.
:::

## Noms Commerciaux & Formatage Lisible du Matériel

Les réponses brutes d'OVHcloud n'exposent que des codes techniques tels que `24ska01`, `memory: ram-64g-ecc-3200` et `storage: raid-r1-2x480ssd-s10-2x6000sa`. OVH Nexus les traduit automatiquement en informations claires :

1. **Résolution du Nom Commercial** :
   - Associe chaque `planCode` aux catalogues publics Eco (`Kimsufi`, `So you Start`, `Rise`) et Baremetal (`Advance`, `Scale`, `High Grade`) mis en cache sur disque pendant 12 heures (`cache/`).
   - Pour les nouveaux modèles récemment rackés mais pas encore publiés dans le catalogue commercial (ex. `27skle01-v1`), utilise un analyseur déterministe (`KS-LE-1`, `KS-A`, `RISE-1`, `SCALE-a1`).
   - Fusionne automatiquement les variantes régionales APAC (`-sgp`, `-syd`, `-mum`, `-apac`) et les suffixes de version (`-v1`, `-v2`).
2. **Traduction Localisée des Spécifications (`en` / `fr` / `zh-CN`)** :
   - **Mémoire RAM** : Convertit `ram-64g-ecc-3200` en `64 Go DDR4 ECC 3200 MHz`.
   - **Stockage & Disques Système** : Convertit `softraid-2x512nvme` en `2 × 512 Go NVMe (Soft RAID)` et sépare `raid-r1-2x480ssd-s10-2x6000sa` en disque système (`2 × 480 Go SSD, HW RAID 1`) et disques de données (`2 × 6 To SATA, HW RAID 10`).

## Tarification Mensuelle Exacte par Configuration (`FQN`)

Contrairement aux outils classiques qui n'affichent que le prix d'appel d'un `planCode`, OVH Nexus calcule le **prix mensuel exact pour chaque sous-configuration matérielle (`FQN`)** :

```text
Prix Mensuel Total = Prix Mensuel du Plan de Base (P1M)
                   + Supplément Mensuel de l'Option RAM (si > 0)
                   + Supplément Mensuel de l'Option Stockage (si > 0)
                   + Supplément Mensuel du Disque Système (si > 0)
```

- Lorsqu'un `planCode` propose plusieurs combinaisons RAM/stockage à des tarifs différents, la carte principale indique le prix de départ (`À partir de XX,XX €/mois`) et chaque sous-configuration affiche son **badge de prix mensuel exact** (`XX,XX €/mois` + frais d'installation éventuels).
- Triez l'ensemble de la matrice par **Prix croissant** ou **Prix décroissant**, ou filtrez par gamme (`KS`, `SYS`, `RISE`, `ADV`, `SCALE`, `HGR`), datacenter (`GRA`, `SBG`, `RBX`, `FRA`, `BHS`, `SGP`, `SYD`, etc.) et état de stock (`Disponible uniquement`).
