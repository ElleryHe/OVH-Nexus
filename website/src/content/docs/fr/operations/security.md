---
title: Recommandations de Sécurité
description: Architecture de sécurité d'OVH Nexus et liste de contrôle pour durcir un déploiement en production.
---

OVH Nexus pilote des infrastructures Cloud critiques et des comptes de facturation. Son backend (`backend/api_auth_middleware.py`, `backend/api_key_config.py`) et son conteneur appliquent plusieurs couches de défense en profondeur par défaut.

## Contrôles de Sécurité Intégrés

1. **Validation Stricte du Mot de Passe au Démarrage** :
   - Le backend valide `API_SECRET_KEY` dès le démarrage (`validate_password()`). Si la clé est absente, comporte moins de 12 caractères ou correspond à une valeur d'exemple connue, le serveur refuse de démarrer.
2. **Cookies de Session `HttpOnly` Signés Côté Serveur** :
   - L'authentification du navigateur repose sur des sessions signées (`ovh_session`) avec `HttpOnly=True`, `SameSite=Lax` et une durée de vie de 12 heures (`PERMANENT_SESSION_LIFETIME`).
   - Le mot de passe (`API_SECRET_KEY`) n'est jamais stocké dans le `localStorage` du navigateur.
3. **Limitation de Débit Anti-Force-Brute (`429 Too Many Requests`)** :
   - `/api/auth/login` comptabilise les échecs de connexion par adresse IP (`X-Real-IP` / `remote_addr`). Après **5 tentatives infructueuses en moins de 300 secondes**, les tentatives suivantes sont bloquées avec le code HTTP `429` et l'en-tête `Retry-After`.
4. **Restriction Loopback pour `X-API-Key` (`_is_direct_loopback`)** :
   - L'authentification par en-tête `X-API-Key` est strictement réservée aux connexions locales directes (`127.0.0.1` / `::1`) dépourvues des en-têtes `X-Forwarded-For` ou `X-Real-IP`. Toute requête externe passant par Nginx doit s'authentifier par cookie de session.
5. **Comparaison de Secrets en Temps Constant** :
   - Toutes les vérifications de mots de passe, jetons internes (`INTERNAL_TOKEN`), clés API et secrets de webhook Telegram utilisent `hmac.compare_digest()` pour prévenir les attaques temporelles.
6. **Permissions Fichiers (`0600`) & Chiffrement au Repos AES-256-GCM** :
   - Au lancement, `app.py` applique `os.umask(0o077)` et restreint tous les fichiers de `/app/backend/data` au mode `0600` (`rw-------`).
   - Tous les secrets sur disque (`appKey`, `appSecret`, `consumerKey`, `tgToken`, mots de passe SMTP, graines 2FA, `session_secret`, `webhook_secret`) sont chiffrés via `HKDF-SHA256` + `AES-256-GCM` (`enc:v1:...`) à l'aide de la clé indépendante `DATA_ENCRYPTION_KEY`. Les jetons MCP ne sont stockés que sous forme d'empreinte `SHA-256` irréversible (`tokenHash`).
7. **Authentification à Deux Facteurs TOTP (RFC 6238) & Contrôle d'Accès par Niveau** :
   - **Verrouillage par défaut sans 2FA** : Tant que le 2FA n'est pas activé dans **Paramètres**, toutes les opérations critiques sont bloquées (`403 TOTP_SETUP_REQUIRED`) et le niveau MCP `L3` est verrouillé sur `False`.
   - **Vérification `strict` vs `sudo`** : Les actions irréversibles (`strict`) exigent un code TOTP frais à chaque appel ; les opérations réversibles (`sudo`) ouvrent une fenêtre Sudo de **5 minutes**.
8. **Protection du Droit de Rétractation UE de 14 Jours (`waiveRetractationPeriod = False`)** :
   - `auto_pay` et `waiveRetractationPeriod` sont désactivés par défaut afin de préserver le droit légal de rétractation européen de 14 jours.
9. **Protection Contre l'Indexation (`robots.txt`)** :
   - La console web inclut un fichier `robots.txt` configuré avec `Disallow: /` afin d'empêcher les moteurs de recherche d'indexer votre instance auto-hébergée.

## Liste de Contrôle en Production

- [ ] Générez une clé `API_SECRET_KEY` (`openssl rand -hex 24`) et une clé indépendante `DATA_ENCRYPTION_KEY` (`openssl rand -hex 32`).
- [ ] Activez l'**Authentification à Deux Facteurs (2FA TOTP)** dès la première connexion et sauvegardez vos 8 codes de secours hors ligne.
- [ ] Liez le port Docker à localhost (`127.0.0.1:20000:80`) et exposez la console derrière un reverse proxy HTTPS (Caddy, Nginx ou Cloudflare Tunnel).
- [ ] Activez `COOKIE_SECURE=true` dans `.env` lorsque vous servez la console en HTTPS.
- [ ] Maintenez `DEBUG=false` en production (même si `ENABLE_API_KEY_AUTH=false` est défini par erreur, OVH Nexus l'ignore et impose l'authentification dès que `DEBUG=false`).
- [ ] Restreignez les droits d'accès à `.env` et `./data` sur l'hôte (`chmod 600 .env && chmod 700 data`).
