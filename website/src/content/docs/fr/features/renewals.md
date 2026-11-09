---
title: Matrice d'Expiration & Renouvellement des Services
description: Surveillez les comptes à rebours d'expiration et les modes de renouvellement de tous vos serveurs dédiés et VPS.
---

Lorsque vous gérez plusieurs dizaines de serveurs dédiés et VPS sur différents comptes OVHcloud, l'oubli d'une échéance en renouvellement manuel peut entraîner la résiliation définitive du serveur et la perte des données.

Le panneau **Aperçu des Expirations & Renouvellements** (situé sur le **Tableau de Bord** et alimenté par `/api/ovh/services/renewals`) agrège automatiquement `/dedicated/server/{name}/serviceInfos` et `/vps/{name}/serviceInfos` au sein d'une matrice triée par échéance.

## Niveaux d'Alerte d'Expiration

Les services sont triés par nombre de jours restants croissant (`daysLeft`) et classés en trois niveaux visuels :

| Niveau | Condition | Badge Visuel | Action Recommandée |
| :--- | :--- | :--- | :--- |
| **Critique** | `daysLeft <= 7` | Rouge (Critique) | Renouvelez immédiatement si le service est en mode manuel ou résiliation. |
| **Avertissement** | `8 <= daysLeft <= 30` | Ambre (Attention) | Vérifiez votre moyen de paiement ou la date de prochaine facturation. |
| **Sain** | `daysLeft > 30` | Vert (Normal) | Aucune action immédiate requise. |

## Modes de Renouvellement

Pour chaque serveur dédié et VPS, OVH Nexus analyse l'objet `renew` de `serviceInfos` :

- **Renouvellement Automatique (`automatic: true`)** : OVHcloud prélèvera automatiquement votre moyen de paiement par défaut selon la périodicité choisie (`period` en mois).
- **Renouvellement Manuel (`manualPayment: true`)** : Vous devez régler manuellement la facture de renouvellement avant la date d'`expiration`.
- **Résiliation à l'Échéance (`deleteAtExpiration: true`)** : La résiliation en fin de période est programmée ; le serveur sera libéré à la date d'expiration.

## Alertes d'Expiration (`service.expiring`)

Dans le **Centre de Notifications**, activez l'événement `service.expiring` et réglez le **Seuil d'alerte avant expiration (`expiryDays`, `7` jours par défaut, plage `1–90` jours)**. OVH Nexus enverra une notification dès qu'un service entrera dans cette fenêtre.
