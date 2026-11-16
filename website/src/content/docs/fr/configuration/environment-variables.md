---
title: Variables d'Environnement
description: Référence complète de toutes les variables d'environnement prises en charge par OVH Nexus.
---

OVH Nexus se configure au démarrage du conteneur via les variables d'environnement définies dans `.env` ou passées dans `docker-compose.yml`. Toutes les variables sont validées dès le lancement par `server/internal/config`.

## Tableau des Variables d'Environnement

| Variable | Requis | Défaut | Description |
| :--- | :---: | :--- | :--- |
| `API_SECRET_KEY` | **Oui** | *(aucun)* | Mot de passe de connexion WebUI et clé API locale (loopback). Doit comporter **au moins 12 caractères** et ne correspondre à aucun exemple faible connu. |
| `DATA_ENCRYPTION_KEY` | **Oui** | *(aucun)* | Clé maîtresse indépendante de chiffrement au repos `HKDF-SHA256` + `AES-256-GCM` pour les secrets dans `/data/nexus.db` (`appKey`, `appSecret`, `consumerKey`, jetons Telegram, mots de passe SMTP, graines TOTP). Doit comporter **au moins 16 caractères** et être **différente de `API_SECRET_KEY`**. |
| `PORT` | Non | `8080` | Port d'écoute TCP du serveur HTTP Go dans le conteneur (mappé sur `20000:8080` dans `docker-compose.yml`). |
| `DATA_DIR` | Non | `/data` | Répertoire hébergeant la base SQLite WAL (`nexus.db`, répertoire `0700`, fichier `0600`). |
| `TZ` | Non | `UTC` | Fuseau horaire IANA appliqué via `time.LoadLocation` aux tâches planifiées et aux horodatages des notifications (ex. `UTC`, `Europe/Paris`, `Asia/Shanghai`). |
| `COOKIE_SECURE` | Non | `false` | Active l'attribut `Secure` sur le cookie `ovh_session`. Passez à `true` lorsque l'accès se fait en HTTPS derrière un reverse proxy. |
| `DEFAULT_LOCALE` | Non | `en` | Langue par défaut de l'interface lors de la première visite (`en`, `fr` ou `zh-CN`). Modifiable à tout moment dans l'interface. |
| `LOG_LEVEL` | Non | `INFO` | Niveau minimal des journaux (`DEBUG`, `INFO`, `WARNING` ou `ERROR`). |
| `LOG_FORMAT` | Non | `text` | Format de sortie standard du conteneur (`text` ou `json`). |
| `TRUSTED_PROXIES` | Non | *(vide)* | Liste d'IP ou de plages CIDR séparées par des virgules (ex. `127.0.0.1,172.16.0.0/12`) autorisées à transmettre `X-Forwarded-For` et `X-Real-IP`. |
| `PUID` | Non | `65532` | Identifiant utilisateur numérique vers lequel le point d'entrée (`su-exec`) abaisse les privilèges après avoir initialisé `/data` (`0700`). Peut être aligné sur l'UID Linux hôte (`id -u`) ; ignoré si le conteneur démarre déjà en non-root (`id -u != 0`, ex. Kubernetes `runAsNonRoot: true`). |
| `PGID` | Non | `65532` | Identifiant de groupe numérique vers lequel le point d'entrée (`su-exec`) abaisse les privilèges après avoir initialisé `/data` (`0700`). Peut être aligné sur le GID Linux hôte (`id -g`). |
| `SECRET_KEY` | Non | *(auto-généré)* | Clé optionnelle pour signer `ovh_session` (HMAC-SHA256). Si omise, une clé aléatoire est générée une fois et chiffrée dans `/data/nexus.db` (`kv_settings`). |
| `CORS_ORIGINS` | Non | `http://localhost:19999,http://127.0.0.1:19999` | Origines autorisées en développement local séparé. Inutile en production où le binaire unique sert WebUI et `/api/*` sur la même origine. |
| `DOCKER_IMAGE` | Non | `elleryhe/ovh-nexus:latest` | Permet de remplacer le tag de l'image dans `docker-compose.yml` (ex. `elleryhe/ovh-nexus:1.3.0`). |

## Générer des Clés Robustes

Utilisez `openssl` pour générer deux clés indépendantes à haute entropie :

```bash
# API_SECRET_KEY (48 caractères hexadécimaux)
openssl rand -hex 24

# DATA_ENCRYPTION_KEY (64 caractères hexadécimaux, distincte de API_SECRET_KEY)
openssl rand -hex 32
```

## Clés Faibles Rejetées

Pour éviter toute exposition accidentelle, `server/internal/config` bloque le démarrage si `API_SECRET_KEY` (`< 12` caractères) ou `DATA_ENCRYPTION_KEY` (`< 16` caractères) est trop courte, identique ou figure dans la liste d'exclusion suivante :

- `123456`, `123456789012`, `1234567890123456`, `password`, `changeme`, `secret`, `admin123456`
- `ovh-nexus-default-secret-key`, `ovh-nexus-default-encryption-key`
- `replace-with-a-strong-random-secret`, `replace-with-a-strong-encryption-key`
- `votre-cle-secrete-robuste`, `encryption-key-example`
- `请替换为您自己的高强度密码`, `请替换为您自己的高强度加密密钥`, `你得替换为你自己的密钥`
