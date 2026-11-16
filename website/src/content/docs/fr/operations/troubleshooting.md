---
title: Journaux & Dépannage
description: Comment utiliser la console de journaux SSE en direct, consulter les logs du conteneur et diagnostiquer les erreurs courantes.
---

## Consulter & Exporter les Journaux (Logs)

OVH Nexus propose deux moyens d'inspecter les journaux opérationnels (tous émis en anglais standardisé via `log/slog` avec masquage automatique des secrets) :

1. **Depuis la Console Web (`/logs`)** :
   - Suivez les journaux en temps réel via **Server-Sent Events (`GET /api/logs/stream`)**, alimentés par un tampon circulaire de 10 000 entrées dans `/data/nexus.db`.
   - Filtrez par **Module (`source`)**, **Niveau** (`DEBUG`, `INFO`, `WARNING`, `ERROR`) et **Mot-clé**, consultez les attributs structurés et exportez au format **`.jsonl`** ou **`.log`**.
2. **Flux Stdout du Conteneur** :
   - Journaux du conteneur en direct (`LOG_FORMAT=text` ou `json`) :
     ```bash
     docker compose logs -f --tail=100
     ```

## Problèmes Fréquents & Solutions

### 1. Le conteneur s'arrête immédiatement avec une erreur `API_SECRET_KEY` ou `DATA_ENCRYPTION_KEY`

**Cause** : `API_SECRET_KEY` (`< 12` caractères) ou `DATA_ENCRYPTION_KEY` (`< 16` caractères) dans `.env` est vide, identique à l'autre clé ou utilise une valeur d'exemple faible.

**Solution** : Générez deux clés aléatoires indépendantes et relancez le conteneur :

```bash
sed -i.bak "s|^API_SECRET_KEY=.*|API_SECRET_KEY=$(openssl rand -hex 24)|" .env
sed -i.bak "s|^DATA_ENCRYPTION_KEY=.*|DATA_ENCRYPTION_KEY=$(openssl rand -hex 32)|" .env
docker compose up -d
```

### 2. Erreur `permission denied` sur `/data/nexus.db` ou Rejet de `PUID`/`PGID`

**Cause** : En déploiement Docker standard, le point d'entrée initialise automatiquement `/data` (`0700`) puis abaisse les privilèges via `su-exec` vers `PUID:PGID` (`65532:65532` par défaut). En revanche, si vous forcez un UID non-root via `user:` (`docker run --user` / Compose `user:`) ou Kubernetes `securityContext.runAsNonRoot: true` (`id -u != 0`) sur un dossier hôte appartenant à `root`, le conteneur ne peut pas exécuter `chown` sur `/data`. De même, une valeur non numérique pour `PUID` ou `PGID` déclenche `[entrypoint] ERROR: PUID/PGID must be a numeric ...`.

**Solution** : Choisissez l'une des deux approches suivantes :

1. **Laisser le point d'entrée abaisser les privilèges via `su-exec` (Recommandé avec Docker Compose)** : Supprimez la directive `user:` forcée pour que le point d'entrée configure `/data` (`0700`) puis exécute `ovh-nexus` sans privilèges via `su-exec` (définissez si besoin des identifiants numériques `PUID` / `PGID` dans `.env` via `id -u` / `id -g`) ; **OU**
2. **Adapter les permissions du volume hôte pour `user:` forcé / Kubernetes `runAsNonRoot`** : Conservez `user:` ou `runAsNonRoot: true` et alignez le propriétaire du dossier hôte (ou `fsGroup` sous Kubernetes) sur l'UID d'exécution :

```bash
chown -R <RUNTIME_UID>:<RUNTIME_GID> ./data
docker compose up -d
```

### 3. La connexion réussit (`200 OK`) en HTTP, mais la page redemande immédiatement le mot de passe

**Cause** : `COOKIE_SECURE=true` est activé dans `.env` alors que vous accédez à la console en HTTP non chiffré (`http://<ip>:20000`). Les navigateurs refusent d'enregistrer ou d'envoyer un cookie marqué `Secure` sur une connexion HTTP en clair.

**Solution** : Accédez à la console via votre domaine `https://` derrière un reverse proxy, ou repassez `COOKIE_SECURE=false` dans `.env` pour un usage local en HTTP.

### 4. Erreur `429 Too Many Requests` lors de la connexion

**Cause** : Plus de 5 tentatives erronées depuis la même IP en moins de 5 minutes (`300` secondes), ou absence de `TRUSTED_PROXIES` derrière un reverse proxy (toutes les requêtes partagent l'IP du proxy).

**Solution** : Patientez jusqu'à la fin du délai `Retry-After` (300 s maximum) ou redémarrez (`docker compose restart`) après avoir vérifié `API_SECRET_KEY`. Derrière un reverse proxy, transmettez `X-Real-IP` / `X-Forwarded-For` et ajoutez l'IP/CIDR du proxy dans `TRUSTED_PROXIES`.

### 5. L'API OVHcloud renvoie `INVALID_CREDENTIAL` ou `This call has not been granted`

**Cause** : La `Consumer Key` a expiré ou a été générée sans les droits nécessaires pour la méthode ou la route appelée (par exemple `GET /vps*` ou `POST /order/*` manquant).

**Solution** : Générez un nouveau token sur la page `/createToken/` de votre région avec les droits `GET /*`, `POST /*`, `PUT /*`, `DELETE /*` (voir [Clés API OVHcloud](/fr/configuration/ovh-api-credentials/)) et mettez à jour le compte dans **Comptes API**.

### 6. Forcer le Rafraîchissement du Catalogue Public

OVH Nexus met en cache les catalogues publics Eco et Baremetal pendant 12 heures. Pour forcer une mise à jour immédiate :

- Cliquez sur **Rafraîchir** dans la **Matrice de Disponibilité en Direct** (`/ovh-availability`), ou redémarrez le conteneur (`docker compose restart`).
