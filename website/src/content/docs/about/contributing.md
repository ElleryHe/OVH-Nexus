---
title: Contributing
description: Development setup, testing requirements, and pull request guidelines for OVH Nexus.
---

Thank you for your interest in contributing to **OVH Nexus**! Contributions to the backend, frontend console, localization dictionaries, and documentation are welcome.

## Security & Credential Safety

:::danger[Never Commit Credentials]
Never include real OVHcloud API credentials (`appKey`, `appSecret`, `consumerKey`), `API_SECRET_KEY`, Telegram Bot tokens, SMTP passwords, or private service names / IP addresses in code, tests, issues, or pull requests.

If you discover a security vulnerability, **do not open a public issue**. Report it privately via [GitHub Security Advisories](https://github.com/ElleryHe/OVH-Nexus/security/advisories/new).
:::

## Local Development Setup

### Prerequisites

- **Docker & Docker Compose** (for full container verification)
- **Node.js 18+** (Node.js 22 recommended for the `website/` documentation site)
- **Python 3.11+**

### Running the Console Locally

1. **Frontend (Vite HMR)**:
   ```bash
   npm ci
   npm run dev
   ```
2. **Backend (Flask)**:
   ```bash
   cd backend
   pip install -r requirements.txt -r requirements-dev.txt
   API_SECRET_KEY=dev-local-random-secret-key PORT=19998 python app.py
   ```
3. **Documentation Site (`website/`)**:
   ```bash
   cd website
   npm ci
   npm run dev
   ```

## Running Tests & Verification

Before opening a Pull Request, ensure all checks pass:

```bash
# 1. Backend security and functional test suite
pytest backend/tests/test_security.py

# 2. Frontend TypeScript & Vite production build
npm run build

# 3. Documentation site build & link validation (if editing docs)
cd website && npm run build
```

## Pull Request Guidelines

- **Conventional Commits**: Use `feat(scope): ...`, `fix(scope): ...`, `docs: ...`, or `chore: ...`.
- **Tri-Lingual Parity**: When adding user-facing UI keys or documentation pages, keep `en`, `fr`, and `zh-CN` synchronized.
- **Theme Compatibility**: Verify UI components in both **Light** (`Pure White`) and **Dark** (`Neutral Carbon Black`) modes.
