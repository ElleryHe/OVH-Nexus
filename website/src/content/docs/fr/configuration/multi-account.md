---
title: Gestion Multi-Comptes
description: Gérez plusieurs comptes OVHcloud sur les régions Europe, Canada/APAC et US depuis une seule console.
---

OVH Nexus permet d'enregistrer et de basculer instantanément entre plusieurs comptes OVHcloud (`ovh-eu`, `ovh-ca`, `ovh-us`) sans redémarrer le conteneur ni déployer plusieurs instances.

## Fonctionnement de l'Isolation Multi-Comptes

- **Registre de Comptes Persistant (`data/accounts.json`)** : Stocke l'alias de chaque compte, son endpoint (`ovh-eu`, `ovh-ca`, `ovh-us`), sa filiale (`FR`, `IE`, `CA`, `US`, etc.) et ses clés API avec des permissions fichier `0600`.
- **Sélection par Requête (`X-OVH-Account`)** : Lorsque vous changez de compte actif dans la barre supérieure ou sur la page **Comptes API**, le frontend joint l'en-tête `X-OVH-Account: <account-id>` aux requêtes API afin que les opérations sur les serveurs, les réinstallations OS et les commandes s'exécutent sur le compte sélectionné.
- **Isolation du Cache par Compte** : Les listes de serveurs dédiés et leurs métadonnées sont mises en cache séparément par identifiant de compte, évitant tout mélange de flotte lors du changement de compte.

## Ajouter et Basculer entre les Comptes

1. Ouvrez **Comptes API** dans la barre latérale.
2. Cliquez sur **Ajouter un compte** et renseignez :
   - **Nom / Alias du compte** (ex. `Production EU (FR)`, `APAC Singapour (CA-SG)`, `US Hillsboro`).
   - **Endpoint** : `ovh-eu`, `ovh-ca` ou `ovh-us`.
   - **App Key**, **App Secret** et **Consumer Key**.
3. Utilisez **Tester la connexion** pour vérifier les clés auprès de `/me` et récupérer automatiquement votre `nichandle` et votre code filiale (`ovhSubsidiary`).
4. Définissez le compte souhaité comme **Compte par défaut** — les tâches de fond qui ne spécifient pas d'identifiant de compte explicite utiliseront ce compte par défaut.

## Filiale (`zone`) et Devise de Tarification

Chaque compte OVHcloud est rattaché à une filiale `ovhSubsidiary` (enregistrée sous `zone`, comme `FR`, `IE`, `DE`, `GB`, `CA`, `SG`, `AU`, `US`). OVH Nexus utilise ce code pour :

- Créer les paniers d'achat (`/order/cart`) lors des commandes automatisées ou manuelles.
- Récupérer la tarification mensuelle exacte par configuration depuis `/order/catalog/public/eco` et `/order/catalog/public/baremetal` dans la devise native de votre compte (`€`, `$`, `£`, `A$`, `S$`).
