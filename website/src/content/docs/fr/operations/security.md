---
title: Recommandations de Sécurité
description: Architecture de sécurité d'OVH Nexus et liste de contrôle pour durcir un déploiement en production.
---

OVH Nexus pilote des infrastructures Cloud critiques et des comptes de facturation. Son binaire statique unique Go (`internal/config`, `internal/auth`, `internal/vault`, `internal/httpx`) et son conteneur Alpine non-root appliquent plusieurs couches de défense en profondeur par défaut.

## Contrôles de Sécurité Intégrés

1. **Validation Stricte des Clés au Démarrage** :
   - `internal/config` valide `API_SECRET_KEY` (`>= 12` caractères) et `DATA_ENCRYPTION_KEY` (`>= 16` caractères, différente de `API_SECRET_KEY`) dès le démarrage. En cas d'absence, de faiblesse ou d'égalité, le serveur refuse de démarrer.
2. **Protection Native Contre Slowloris et Inondation HTTP** :
   - Le serveur HTTP Go applique des délais stricts (`ReadHeaderTimeout: 5s`, `ReadTimeout: 15s`, `WriteTimeout: 120s`, `IdleTimeout: 60s`), une limite de corps de requête de `2 Mo` et des en-têtes CSP stricts directement dans le binaire.
3. **Cookies de Session `HttpOnly` Signés Côté Serveur** :
   - L'authentification du navigateur repose sur des cookies signés (`ovh_session` via HMAC-SHA256) avec `HttpOnly=true`, `SameSite=Lax` et une durée de vie de 12 heures.
   - `API_SECRET_KEY` n'est jamais stocké dans le `localStorage` du navigateur.
4. **Limitation de Débit Compatible Reverse Proxy (`429 Too Many Requests`)** :
   - `/api/auth/login` et la vérification TOTP appliquent une limitation par IP en fenêtre glissante (**5 échecs en 300 secondes** → HTTP `429` avec `Retry-After`).
   - Les en-têtes `X-Forwarded-For` et `X-Real-IP` ne sont honorés **que** lorsque la connexion TCP provient d'une IP ou plage CIDR listée dans `TRUSTED_PROXIES`.
5. **Restriction Loopback pour `X-API-Key`** :
   - L'authentification par en-tête `X-API-Key` est strictement réservée aux connexions locales directes (`127.0.0.1` / `::1`) sans en-têtes de proxy transférés. Toute requête externe passant par un reverse proxy doit s'authentifier par cookie de session.
6. **Comparaison de Secrets en Temps Constant** :
   - Les vérifications de mots de passe, jetons internes, clés API et empreintes utilisent `crypto/subtle.ConstantTimeCompare` pour prévenir les attaques temporelles.
7. **Abaissement de Privilèges Non-Root (`su-exec`, `PUID`/`PGID`), Permissions `0600` & Chiffrement AES-256-GCM** :
   - Au démarrage, le point d'entrée Alpine (`/usr/local/bin/docker-entrypoint.sh`) initialise `/data` en mode `0700` et abaisse les privilèges via `su-exec` vers l'utilisateur non privilégié `PUID:PGID` (`65532:65532` par défaut, `nexus:nexus`), puis `ovh-nexus` ouvre `/data/nexus.db` en mode `0600` (`rw-------`). L'exécution directe sous Kubernetes `runAsNonRoot: true` (`id -u != 0`) est également supportée.
   - Tous les secrets stockés (`app_key`, `app_secret`, `consumer_key`, jetons Telegram, mots de passe SMTP, graines TOTP et `session_secret`) sont chiffrés via `HKDF-SHA256` + `AES-256-GCM` (`enc:v1:...`), et la clé brute est effacée de la mémoire après dérivation. Les jetons MCP et codes de secours ne sont stockés que sous forme d'empreintes `SHA-256` irréversibles.
8. **Authentification à Deux Facteurs TOTP (RFC 6238) & Contrôle d'Accès par Niveau** :
   - **Verrouillage par défaut sans 2FA** : Tant que le 2FA n'est pas activé dans **Paramètres**, toutes les opérations critiques sont bloquées (`403 TOTP_SETUP_REQUIRED`) et le niveau MCP `L3` reste verrouillé.
   - **Vérification `strict` vs `sudo`** : Les actions irréversibles (`strict`) exigent un code TOTP frais à chaque appel ; les opérations réversibles (`sudo`) ouvrent une fenêtre Sudo de **5 minutes**.
9. **Protection du Droit de Rétractation UE de 14 Jours (`waive_retractation_period = false`)** :
   - `auto_pay` et `waive_retractation_period` sont désactivés par défaut afin de préserver le droit légal de rétractation européen de 14 jours.
10. **Rédaction Automatique des Journaux & `robots.txt`** :
    - Le hub `log/slog` masque automatiquement les secrets avant écriture sur stdout, SQLite ou flux SSE, et `robots.txt` sert `Disallow: /`.

## Liste de Contrôle en Production

- [ ] Générez une clé `API_SECRET_KEY` (`openssl rand -hex 24`) et une clé indépendante `DATA_ENCRYPTION_KEY` (`openssl rand -hex 32`).
- [ ] Activez l'**Authentification à Deux Facteurs (2FA TOTP)** dès la première connexion et sauvegardez vos 8 codes de secours hors ligne.
- [ ] Liez le port Docker à localhost (`127.0.0.1:20000:8080`) et exposez la console derrière un reverse proxy HTTPS (Caddy, Nginx ou Cloudflare Tunnel).
- [ ] Activez `COOKIE_SECURE=true` et configurez `TRUSTED_PROXIES` dans `.env` lorsque vous servez la console en HTTPS.
- [ ] Restreignez les droits d'accès à `.env` sur l'hôte (`chmod 600 .env`) ; le point d'entrée du conteneur applique automatiquement le mode `0700` sur `/data` (`PUID:PGID`, `65532:65532` par défaut) et `0600` sur `/data/nexus.db`.
