---
title: Foire Aux Questions (FAQ)
description: Réponses aux questions fréquentes sur OVH Nexus, les clés API, l'achat automatisé, la tarification et les notifications.
---

### Une clé API OVHcloud est-elle obligatoire pour utiliser OVH Nexus ?

**Non, pas pour la surveillance publique du stock et des SKU.** Dès le déploiement d'OVH Nexus, vous pouvez utiliser la **Matrice de Disponibilité en Direct** (avec noms commerciaux, spécifications matérielles et prix mensuels exacts par configuration), la **Veille Publique des Changements SKU** et les notifications **Telegram / Email SMTP** (`stock.available`, `sku.changed`) sans clé API OVHcloud.

Une clé API OVHcloud n'est requise que pour les fonctionnalités liées à vos services : **Contrôle des Serveurs Dédiés**, **Gestion des VPS Achetés**, **Suivi des Commandes & Livraison**, **Matrice des Renouvellements** et **Achat Automatisé**.

---

### L'achat automatisé débite-t-il automatiquement ma carte bancaire ou mon compte PayPal ?

Par défaut, OVH Nexus génère un bon de commande officiel via `/order/cart/{cartId}/checkout` avec `autoPayWithPreferredPaymentMethod: false`. Cela réserve l'allocation du serveur et vous envoie immédiatement une notification `order.success` contenant le **lien officiel de paiement OVHcloud**, vous permettant de vérifier et de régler la commande dans votre navigateur.

---

### Pourquoi certains nouveaux SKU détectés par la veille SKU n'affichent-ils pas encore de prix ?

Lorsque les ingénieurs d'OVHcloud mettent en baie une toute nouvelle référence de serveur (par exemple `27skle01-v1`) dans `/dedicated/server/datacenter/availabilities`, elle apparaît souvent dans le flux de disponibilité des datacenters avant qu'OVHcloud ne publie son tarif commercial dans `/order/catalog/public/eco` ou `/baremetal`.

Pendant cette phase de pré-lancement, OVH Nexus utilise son analyseur déterministe (`infer_plan_from_code`) pour déduire la gamme commerciale (`KS-LE-1`) et les spécifications RAM/stockage, puis affiche automatiquement le prix mensuel dès son ajout au catalogue public.

---

### Puis-je gérer simultanément des comptes OVH Europe (`ovh-eu`), Canada (`ovh-ca`) et US (`ovh-us`) ?

Oui. Vous pouvez enregistrer autant de comptes `ovh-eu`, `ovh-ca` et `ovh-us` que souhaité dans **Comptes API** et changer de compte actif en un clic depuis la barre supérieure.

---

### Comment changer la langue par défaut de l'interface web ?

Définissez `DEFAULT_LOCALE` dans votre fichier `.env` sur `fr` (Français), `en` (English) ou `zh-CN` (简体中文), ou utilisez le sélecteur `EN | FR | 中文` présent sur la page de connexion et dans la barre latérale.
