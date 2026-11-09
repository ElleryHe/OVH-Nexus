---
title: Flotte VPS & Surveillance de Stock
description: Gérez vos instances VPS OVHcloud achetées et surveillez la disponibilité des nouvelles gammes VPS.
---

OVH Nexus couvre l'ensemble du cycle de vie des Serveurs Privés Virtuels (VPS) OVHcloud : **la gestion quotidienne de vos VPS achetés** et **la surveillance de disponibilité du catalogue VPS public**.

## Gestion des VPS Achetés (`/vps`)

Dans la page **Moniteur VPS** (`/vps-monitor`), ouvrez l'onglet **Mes VPS Achetés** (nécessite une clé API OVHcloud configurée) pour inspecter toutes les instances renvoyées par `/vps` :

- **Vue d'Ensemble de l'Instance** : Nom de service (`vps-xxxx.vps.ovh.net`), nom d'affichage personnalisé, offre commerciale (`VPS-1`, `VPS-2`, `Elite`, `Comfort`, `Essential`, `Starter`) et zone datacenter (`GRA`, `SBG`, `BHS`, `WAW`, `FRA`, `LON`, `SGP`, `SYD`).
- **Ressources Allouées** : Nombre de vCores, mémoire RAM (Go) et espace disque NVMe/SSD.
- **Réseau & Cycle de Vie** : Adresses IPv4 et IPv6 principales (`/vps/{serviceName}/ips`), état courant (`running`, `stopped`, `rebooting`, `installing`, `maintenance`), date de création, date d'expiration et mode de renouvellement.
- **Redémarrage en Un Clic** : Déclenchez un redémarrage immédiat au niveau de l'hyperviseur via `POST /vps/{serviceName}/reboot`.

## Surveillance du Stock Public VPS

Dans l'onglet **Surveillance Stock VPS**, vous pouvez suivre sans clé API la disponibilité des offres VPS publiques OVHcloud (`/order/catalog/public/vps`) :

- Visualisez quelles zones géographiques disposent actuellement de stock (`available` vs `out-of-stock`) pour chaque `planCode` VPS.
- Créez des abonnements de surveillance (`data/vps_subscriptions.json`) afin qu'OVH Nexus vérifie périodiquement les offres choisies en tâche de fond et envoie une alerte via le **Centre de Notifications** dès le retour en stock.
