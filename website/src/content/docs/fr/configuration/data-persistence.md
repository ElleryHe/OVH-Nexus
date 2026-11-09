---
title: Persistance des Données & Sauvegarde
description: Structure des répertoires, fichiers JSON persistés, durée de vie du cache et procédures de sauvegarde.
---

OVH Nexus stocke l'ensemble de son état d'exécution dans trois répertoires du conteneur. Le montage de ces trois volumes dans `docker-compose.yml` garantit qu'aucune donnée n'est perdue lors d'un redémarrage ou d'une mise à jour d'image.

## Vue d'Ensemble des Volumes

| Chemin Hôte | Chemin Conteneur | Rôle |
| :--- | :--- | :--- |
| `./data` | `/app/backend/data` | Comptes API, paramètres, canaux de notification, files d'achat, historique des commandes et instantanés SKU. |
| `./logs` | `/app/backend/logs` | Journal applicatif du backend (`app.log`). |
| `./cache` | `/app/backend/cache` | Cache disque des catalogues publics Eco/Baremetal OVHcloud (peut être purgé à tout moment). |

## Fichiers Contenus dans `/app/backend/data`

Tous les fichiers créés dans `/app/backend/data` reçoivent automatiquement les permissions `0600` (`rw-------`) au démarrage (`os.umask(0o077)`).

| Fichier | Description |
| :--- | :--- |
| `config.json` | Paramètres du compte par défaut, canaux de notification (`notifications.channels`) et règles de veille SKU (`notifications.rules`). |
| `accounts.json` | Registre multi-comptes des clés API OVHcloud et identifiant du compte par défaut. |
| `session_secret` | Clé aléatoire de 32 octets générée automatiquement pour signer le cookie `ovh_session`. |
| `webhook_secret` | Jeton secret généré automatiquement pour authentifier les requêtes entrantes du webhook Telegram (`/api/telegram/webhook`). |
| `queue.json` | Tâches actives de la file d'approvisionnement automatisé. |
| `config_sniper_tasks.json` | Tâches de surveillance ciblant une sous-configuration matérielle précise (`planCode + mémoire + stockage + datacenter`). |
| `subscriptions.json` | Abonnements de surveillance de stock des serveurs dédiés. |
| `vps_subscriptions.json` | Abonnements de surveillance de stock des VPS. |
| `history.json` | Historique des commandes générées (avec numéros de commande et liens de paiement). |
| `sku_snapshot.json` | État de référence des SKU publics OVHcloud et compteurs d'absence. |
| `sku_changes.json` | Chronologie des 200 derniers ajouts, retraits et changements de configuration SKU. |

## Sauvegarde & Restauration

Pour sauvegarder l'intégralité de votre instance OVH Nexus, créez une archive restreinte contenant le dossier `./data` et le fichier `.env` :

```bash
tar -czf ovh-nexus-backup-$(date +%F).tar.gz .env data/
chmod 600 ovh-nexus-backup-*.tar.gz
```

:::danger[Protégez vos archives de sauvegarde]
`data/config.json` et `data/accounts.json` contiennent vos clés API OVHcloud, jetons Telegram Bot et mots de passe SMTP. Ne publiez jamais `data/` ou `.env` sur un dépôt Git public.
:::
