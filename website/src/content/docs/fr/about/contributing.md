---
title: Contribuer au Projet
description: Configuration de l'environnement de développement, exécution des tests et règles de Pull Request pour OVH Nexus.
---

Merci de votre intérêt pour **OVH Nexus** ! Les contributions au backend, à l'interface web, aux traductions et à la documentation sont les bienvenues.

## Sécurité & Protection des Identifiants

:::danger[Ne commitez jamais d'identifiants réels]
N'incluez jamais de clés API OVHcloud réelles (`appKey`, `appSecret`, `consumerKey`), de `API_SECRET_KEY`, de jetons Telegram Bot, de mots de passe SMTP ou d'adresses IP / noms de service privés dans le code, les tests, les issues ou les pull requests.

Si vous découvrez une vulnérabilité de sécurité, **n'ouvrez pas d'issue publique**. Signalez-la de manière confidentielle via [GitHub Security Advisories](https://github.com/ElleryHe/OVH-Nexus/security/advisories/new).
:::

## Environnement de Développement Local

### Prérequis

- **Docker & Docker Compose** (pour tester le conteneur complet)
- **Node.js 18+** (Node.js 22 recommandé pour le site de documentation `website/`)
- **Python 3.11+**

### Lancer le Projet en Local

1. **Frontend (Vite HMR)** :
   ```bash
   npm ci
   npm run dev
   ```
2. **Backend (Flask)** :
   ```bash
   cd backend
   pip install -r requirements.txt -r requirements-dev.txt
   API_SECRET_KEY=dev-local-random-secret-key PORT=19998 python app.py
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
# 1. Suite de tests unitaires et de sécurité du backend
pytest backend/tests/test_security.py

# 2. Compilation TypeScript et build de production Vite
npm run build

# 3. Build de la documentation et validation des liens
cd website && npm run build
```

## Règles de Pull Request

- **Conventional Commits** : Utilisez les préfixes `feat(scope): ...`, `fix(scope): ...`, `docs: ...` ou `chore: ...`.
- **Parité Trilingue** : Lors de l'ajout de chaînes d'interface ou de pages de documentation, maintenez la synchronisation entre `en`, `fr` et `zh-CN`.
- **Compatibilité des Thèmes** : Vérifiez la lisibilité des composants dans les modes **Clair** (`Pure White`) et **Sombre** (`Neutral Carbon Black`).
