---
title: Approvisionnement Automatisé & File d'Achat
description: Configurez des tâches d'achat automatisé de serveurs dédiés et des snipers de configuration précise.
---

OVH Nexus propose deux moteurs complémentaires pour automatiser l'acquisition de serveurs dédiés lorsque des modèles très demandés (`KS-LE`, `KS-A` ou ventes flash) ne restent en stock que quelques secondes :

1. **File d'Achat (`/queue`)** : Surveille un `planCode` cible sur un ou plusieurs datacenters prioritaires avec options matérielles.
2. **Sniper de Configuration (`/monitor` & `/ovh-availability`)** : Cible une sous-configuration matérielle exacte (`planCode + mémoire + stockage + datacenter`) directement depuis la Matrice de Disponibilité en Direct.

## Déroulement du Processus d'Achat Automatisé

Dès que le démon de surveillance détecte du stock disponible (`1H-low`, `1H-high`, `24H`, `72H` ou `available`) sur la configuration et le datacenter ciblés :

1. **Création du Panier** : Appelle `POST /order/cart` avec la filiale `ovhSubsidiary` de votre compte (`zone`, ex. `FR`, `IE`, `CA`, `US`) puis assigne le panier à votre compte authentifié (`POST /order/cart/{cartId}/assign`).
2. **Ajout du Serveur** : Appelle `POST /order/cart/{cartId}/eco` (ou `/baremetal`) avec le `planCode`, `duration: P1M`, `pricingMode: default` et `quantity: 1`.
3. **Configuration du Datacenter & des Options (Addons)** :
   - Définit le `datacenter` cible sur l'élément du panier.
   - Attache les codes options exacts `memory`, `storage`, `system-storage` et bande passante requis par le FQN.
4. **Validation du Bon de Commande** : Appelle `POST /order/cart/{cartId}/checkout` (`autoPayWithPreferredPaymentMethod: false` par défaut afin de générer un bon de commande officiel réservant le serveur sans débiter votre moyen de paiement à votre insu).
5. **Envoi de la Notification** : Émet l'événement `order.success` (comprenant le numéro de commande, le récapitulatif matériel, le prix TTC et le lien direct de paiement OVHcloud) ou `order.failed` via le **Centre de Notifications**.

## Créer une Tâche en Un Clic

Depuis le **Catalogue Serveurs (`/servers`)** ou la **Matrice de Disponibilité en Direct (`/ovh-availability`)** :

1. Repérez le modèle et la combinaison `mémoire + stockage` souhaités.
2. Cliquez sur **Ajouter à la file** ou **Surveiller cette config**.
3. Sélectionnez les datacenters souhaités (ou `any` pour accepter le premier datacenter disponible) ainsi que l'intervalle de vérification (`15–30` secondes).
4. Une fois la commande générée avec succès, la tâche passe automatiquement à l'état terminé et enregistre la commande dans **Historique & Commandes (`/history`)**.
