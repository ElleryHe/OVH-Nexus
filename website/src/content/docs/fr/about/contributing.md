---
title: Contribuer au Projet
description: Configuration de l'environnement de développement, exécution des tests et règles de Pull Request pour OVH Nexus.
---

Merci de votre intérêt pour **OVH Nexus** ! Les contributions au backend Go, à l'interface web React, aux traductions et à la documentation sont les bienvenues.

## Sécurité & Protection des Identifiants

:::danger[Ne commitez jamais d'identifiants réels]
N'incluez jamais de clés API OVHcloud réelles (`appKey`, `appSecret`, `consumerKey`), de `API_SECRET_KEY`, de `DATA_ENCRYPTION_KEY`, de jetons Telegram Bot, de mots de passe SMTP ou d'adresses IP / noms de service privés dans le code, les tests, les issues ou les pull requests.

Si vous découvrez une vulnérabilité de sécurité, **n'ouvrez pas d'issue publique**. Signalez-la de manière confidentielle via [GitHub Security Advisories](https://github.com/ElleryHe/OVH-Nexus/security/advisories/new).
:::

## Environnement de Développement Local

### Prérequis

- **Docker & Docker Compose** (pour tester le conteneur complet)
- **Go 1.27+** (pour le serveur backend et les tests unitaires)
- **Node.js 24+** & `npm` (pour le frontend et le site de documentation `website/`)

### Lancer le Projet en Local

1. **Frontend (Vite HMR)** :
   ```bash
   npm ci
   npm run dev
   ```
2. **Backend (Go)** :
   ```bash
   cd server
   API_SECRET_KEY="dev-local-random-secret-key" \
   DATA_ENCRYPTION_KEY="dev-local-random-encryption-key" \
   DATA_DIR="./dev-data" \
   PORT="8080" \
   go run ./cmd/ovh-nexus
   ```
3. **Site de Documentation (`website/`)** :
   ```bash
   cd website
   npm ci
   npm run dev
   ```

## Exécuter les Tests & Vérifications

Avant de soumettre une Pull Request, vérifiez que toutes les étapes réussissent :

```bash
# 1. Analyse statique et suite de tests Go avec détecteur de course
cd server && go vet ./... && go test -race ./...

# 2. Compilation TypeScript et build de production Vite
npx tsc --noEmit && npm run build

# 3. Build de la documentation et validation des liens
npm run build --prefix website
```

## Règles de Pull Request

- **Conventional Commits** : Utilisez les préfixes `feat(scope): ...`, `fix(scope): ...`, `docs: ...` ou `chore: ...`.
- **Journaux Standardisés** : Les messages `log/slog` du backend doivent être rédigés en anglais concis avec un nom de `source` canonique et sans exposer de secrets.
- **Parité Trilingue** : Lors de l'ajout de chaînes d'interface ou de pages de documentation, maintenez la synchronisation entre `en`, `fr` et `zh-CN`.
- **Compatibilité des Thèmes** : Vérifiez la lisibilité des composants dans les modes **Clair** (`Pure White`) et **Sombre** (`Neutral Carbon Black`).
