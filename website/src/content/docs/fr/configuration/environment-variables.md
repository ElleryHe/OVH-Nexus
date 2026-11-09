---
title: Variables d'Environnement
description: Référence complète de toutes les variables d'environnement prises en charge par OVH Nexus.
---

OVH Nexus se configure au démarrage du conteneur via les variables d'environnement définies dans `.env` ou passées dans `docker-compose.yml`.

## Tableau des Variables d'Environnement

| Variable | Requis | Défaut | Description |
| :--- | :---: | :--- | :--- |
| `API_SECRET_KEY` | **Oui** | *(aucun)* | Mot de passe de connexion WebUI et clé API locale (loopback). Doit comporter **au moins 12 caractères** et ne correspondre à aucun exemple faible connu. |
| `DEFAULT_LOCALE` | Non | `en` | Langue par défaut de l'interface lors de la première visite (`en`, `fr` ou `zh-CN`). Modifiable à tout moment dans l'interface. |
| `COOKIE_SECURE` | Non | `false` | Active l'attribut `Secure` sur le cookie `ovh_session`. Passez à `true` lorsque l'accès se fait en HTTPS derrière un reverse proxy. |
| `SECRET_KEY` | Non | *(auto-généré)* | Clé de signature de session Flask. Si omise, une clé aléatoire de 32 octets est générée et persistée dans `/app/backend/data/session_secret` (`0600`). |
| `ENABLE_API_KEY_AUTH` | Non | `true` | Active le contrôle d'authentification. En production (`DEBUG=false`), l'authentification reste **toujours obligatoire** même si cette variable vaut `false`. |
| `DEBUG` | Non | `false` | Active le mode debug Flask. **Ne jamais définir `DEBUG=true` en production.** |
| `BIND_HOST` | Non | `127.0.0.1` | Interface d'écoute du backend Python à l'intérieur du conteneur (`127.0.0.1` pour que seul le processus Nginx interne puisse y accéder). |
| `PORT` | Non | `19998` | Port interne du backend Python Flask (relayé par Nginx sur le port `80` du conteneur). |
| `CORS_ORIGINS` | Non | `http://localhost:19999,http://127.0.0.1:19999` | Liste d'origines autorisées en développement séparé. Inutile en production Docker où Nginx sert l'interface et `/api/*` sur la même origine. |
| `DOCKER_IMAGE` | Non | `elleryhe/ovh-nexus:latest` | Permet de remplacer le tag de l'image dans `docker-compose.yml` (ex. `elleryhe/ovh-nexus:1.0.2`). |

## Générer une Clé `API_SECRET_KEY` Robuste

Utilisez l'une des commandes suivantes pour générer une clé aléatoire à haute entropie :

```bash
# Avec OpenSSL (48 caractères hexadécimaux)
openssl rand -hex 24

# Avec Python (base64 URL-safe)
python3 -c "import secrets; print(secrets.token_urlsafe(24))"
```

## Clés Faibles Rejetées

Pour éviter toute exposition accidentelle, `backend/api_key_config.py` bloque le démarrage si `API_SECRET_KEY` fait moins de 12 caractères ou figure dans la liste d'exclusion suivante :

- `123456`, `password`, `changeme`
- `ovh-nexus-default-secret-key`
- `replace-with-a-strong-random-secret`
- `votre-cle-secrete-robuste`
- `请替换为您自己的高强度密码`, `你得替换为你自己的密钥`
