---
title: Commandes & Pipeline de Livraison en Direct
description: Suivez vos commandes OVHcloud, récupérez les liens de paiement et observez les 4 étapes de livraison matérielle.
---

Une fois qu'une commande est générée (automatiquement par la file d'achat OVH Nexus ou manuellement sur le site OVHcloud), la page **Historique & Commandes** (`/history`) permet d'en suivre l'avancement en temps réel via `/me/order`.

## Synchronisation des Commandes Officielles OVHcloud

Cliquez sur **Synchroniser les commandes OVH** dans l'onglet **Commandes Officielles OVH** pour récupérer les commandes récentes du compte actif :

- **Métadonnées de Commande** : Numéro de commande (`orderId`), date de création, montant TTC (`priceWithTax.text`) et devise.
- **Liens de Paiement & Factures PDF** : Accès direct au bon de commande et au portail de règlement OVHcloud (`url`) pour régler immédiatement une commande en attente de paiement (`unpaid` / `notPaid`).

## Pipeline de Livraison Matérielle en 4 Étapes (`/followUp`)

Pour chaque commande, OVH Nexus interroge `/me/order/{orderId}/followUp` et `/me/order/{orderId}/status` afin d'afficher le pipeline officiel en 4 étapes :

| Étape | Code OVH | Description |
| :--- | :--- | :--- |
| **1. Création** | `CREATING` | Le bon de commande est généré et en attente de règlement ou de validation initiale. |
| **2. Validation** | `VALIDATING` | Vérification du paiement et contrôles anti-fraude par OVHcloud. |
| **3. Livraison** | `DELIVERING` | Allocation du châssis en baie et préparation matérielle dans le datacenter. |
| **4. Installation** | `INSTALLATION` | Vérifications finales du firmware/OS et mise à disposition du serveur sur votre compte. |

Chaque étape affiche son statut en direct (`DONE`, `DOING`, `TODO` ou `ERROR`) ainsi que l'historique détaillé des horodatages renvoyés par l'API.

## Notifications de Livraison (`order.delivered`)

Lorsque l'événement `order.delivered` est activé dans le **Centre de Notifications**, OVH Nexus vous envoie automatiquement une alerte Telegram ou Email SMTP dès que le serveur atteint la fin de l'étape `INSTALLATION`.
