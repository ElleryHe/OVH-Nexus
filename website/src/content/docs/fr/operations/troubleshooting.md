---
title: Journaux & Dépannage
description: Comment consulter les journaux du conteneur, diagnostiquer les erreurs courantes et rafraîchir le cache.
---

## Consulter les Journaux (Logs)

OVH Nexus propose deux moyens d'inspecter les journaux d'activité :

1. **Depuis la Console Web (`/logs`)** :
   - Consultez les événements structurés (vérifications de stock, tentatives de commande, changements SKU, envois de notifications) filtrés par niveau (`INFO`, `WARNING`, `ERROR`) et par catégorie.
2. **Journaux du Conteneur et Fichiers sur l'Hôte** :
   - Flux stdout/stderr du conteneur en direct :
     ```bash
     docker compose logs -f --tail=100
     ```
   - Fichier journal persistant sur l'hôte :
     ```bash
     tail -f ./logs/app.log
     ```

## Problèmes Fréquents & Solutions

### 1. Le conteneur s'arrête immédiatement avec `RuntimeError: API_SECRET_KEY ...`

**Cause** : La variable `API_SECRET_KEY` dans `.env` est vide, comporte moins de 12 caractères ou utilise l'une des valeurs d'exemple de la documentation (`votre-cle-secrete-robuste`, `replace-with-a-strong-random-secret`, `123456`).

**Solution** : Générez une clé aléatoire et relancez le conteneur :

```bash
sed -i.bak "s|^API_SECRET_KEY=.*|API_SECRET_KEY=$(openssl rand -hex 24)|" .env
docker compose up -d
```

### 2. La connexion réussit (`200 OK`) en HTTP, mais la page redemande immédiatement le mot de passe

**Cause** : `COOKIE_SECURE=true` est activé dans `.env` alors que vous accédez à la console en HTTP non chiffré (`http://<ip>:20000`). Les navigateurs refusent d'enregistrer ou d'envoyer un cookie marqué `Secure` sur une connexion HTTP en clair.

**Solution** : Accédez à la console via votre domaine `https://` derrière un reverse proxy, ou repassez `COOKIE_SECURE=false` dans `.env` pour un usage local en HTTP.

### 3. Erreur `429 Too Many Requests` lors de la connexion

**Cause** : Plus de 5 tentatives de mot de passe erronées ont été effectuées depuis la même adresse IP en moins de 5 minutes (`300` secondes).

**Solution** : Patientez jusqu'à la fin du délai `Retry-After` (300 secondes maximum) ou redémarrez le conteneur (`docker compose restart`) après avoir vérifié la clé `API_SECRET_KEY` dans `.env`. Derrière un reverse proxy, vérifiez que l'en-tête `X-Real-IP` est bien transmis.

### 4. L'API OVHcloud renvoie `INVALID_CREDENTIAL` ou `This call has not been granted`

**Cause** : La `Consumer Key` a expiré ou a été générée sans les droits nécessaires pour la méthode ou la route appelée (par exemple `GET /vps*` ou `POST /order/*` manquant).

**Solution** : Générez un nouveau token sur la page `/createToken/` de votre région avec les droits `GET /*`, `POST /*`, `PUT /*`, `DELETE /*` (voir [Clés API OVHcloud](/fr/configuration/ovh-api-credentials/)) et mettez à jour le compte dans **Comptes API**.

### 5. Forcer le Rafraîchissement du Catalogue Public

OVH Nexus met en cache les catalogues publics Eco et Baremetal dans `./cache` pendant 12 heures (`CATALOG_TTL_SECONDS = 43200`). Pour forcer une mise à jour immédiate des noms commerciaux et des prix :

- Cliquez sur le bouton **Rafraîchir** dans la **Matrice de Disponibilité en Direct**, ou
- Supprimez les fichiers `./cache/*.json` et redémarrez le conteneur :
  ```bash
  rm -f ./cache/*.json && docker compose restart
  ```
