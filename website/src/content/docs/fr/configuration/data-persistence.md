---
title: Persistance des Données & Sauvegarde
description: Architecture mono-volume SQLite WAL, chiffrement colonne par colonne et procédures de sauvegarde.
---

OVH Nexus regroupe l'ensemble de la configuration, des secrets chiffrés, des files d'achat, des états de veille et des journaux structurés dans **un unique volume persistant (`./data:/data`)** géré par une base **SQLite embarquée pure Go en mode WAL** (`/data/nexus.db`).

## Vue d'Ensemble du Volume

| Chemin Hôte | Chemin Conteneur | Rôle |
| :--- | :--- | :--- |
| `./data` | `/data` | Stocke `nexus.db` (ainsi que les fichiers SQLite `-wal` et `-shm`) contenant tout l'état persistant. |

:::note[Initialisation Automatique Non-Root (`su-exec`, `PUID`/`PGID`)]
Au démarrage du conteneur, `/usr/local/bin/docker-entrypoint.sh` crée et configure automatiquement `/data` avec les permissions `0700` et l'appartenance `PUID:PGID` (`65532:65532` par défaut, `nexus:nexus`), puis abaisse les privilèges via `su-exec` avant qu'`ovh-nexus` n'ouvre `/data/nexus.db` en mode `0600` (`rw-------`). Aucun `chown` manuel sur l'hôte n'est requis. Les utilisateurs Linux non-root peuvent optionnellement définir `PUID`/`PGID` dans `.env`. Si le conteneur démarre déjà avec un UID non-root (`id -u != 0`, comme Kubernetes `runAsNonRoot: true`), le point d'entrée lance `ovh-nexus` directement ; le volume ou `fsGroup` doit alors autoriser cet UID en écriture.
:::

## Tables SQLite dans `/data/nexus.db`

Toutes les tables utilisent `PRAGMA journal_mode=WAL; PRAGMA synchronous=NORMAL; PRAGMA busy_timeout=5000; PRAGMA foreign_keys=ON;` :

| Table | Contenu & Protection |
| :--- | :--- |
| `kv_settings` | Paramètres système (`default_account_id`, langue de notification `locale`, `panel_url`, `vps_check_interval`) et `session_secret` chiffré. |
| `ovh_accounts` | Registre multi-comptes OVHcloud (`endpoint`, `zone`, `is_default`) avec `app_key`, `app_secret` et `consumer_key` chiffrés via `AES-256-GCM` (`enc:v1:...`). |
| `totp_state` | Singleton 2FA TOTP RFC 6238 (`enabled`, `secret` & `pending_secret` chiffrés, codes de secours hachés `SHA-256`, `last_used_step`). |
| `order_queue` | Tâches de la file d'approvisionnement automatisé (`plan_code`, datacenters, options, `auto_pay`, `waive_retractation_period`). |
| `purchase_history` | Historique des commandes automatiques et manuelles (numéros de commande, URLs de paiement, tarifs, statut). |
| `stock_subscriptions` & `vps_subscriptions` | Abonnements de surveillance de stock Serveurs Dédiés & VPS avec instantanés d'état. |
| `sku_watcher_state` | Référence publique SKU, compteurs de disparition et chronologie récente des changements SKU. |
| `notification_channels` & `notified_markers` | Canaux sortants Telegram Bot & Email SMTP (jetons/mots de passe chiffrés) et marqueurs anti-doublon pour les livraisons/expirations. |
| `mcp_config`, `mcp_aliases`, `mcp_audit_logs` | Empreinte irréversible `SHA-256` `token_hash` + `masked_token`, scopes L1–L3, table d'alias bidirectionnels (`acct-01`, `bm-01`, `vps-01`, `ip-01`, `ord-01`) et journal d'audit MCP. |
| `system_logs` | Journaux structurés en anglais (`id`, `timestamp` UTC, `level`, `source`, `message`, `attrs_json`), purgés automatiquement pour conserver un **tampon circulaire de 10 000 lignes**. |

## Sauvegarde & Restauration

Pour sauvegarder l'intégralité de votre instance OVH Nexus en garantissant la cohérence du journal WAL, arrêtez brièvement le conteneur et archivez `.env` et `./data/` ensemble :

```bash
docker compose stop
tar -czf ovh-nexus-backup-$(date +%F).tar.gz .env data/
chmod 600 ovh-nexus-backup-*.tar.gz
docker compose start
```

:::danger[Protégez vos archives de sauvegarde & `DATA_ENCRYPTION_KEY`]
`/data/nexus.db` contient vos clés API OVHcloud, jetons Telegram Bot, mots de passe SMTP et graines 2FA chiffrés à l'aide de `DATA_ENCRYPTION_KEY` (dans `.env`). Sauvegardez toujours `.env` et `./data/` ensemble, conservez les archives hors ligne ou chiffrées et ne les publiez jamais sur un dépôt Git public.
:::
