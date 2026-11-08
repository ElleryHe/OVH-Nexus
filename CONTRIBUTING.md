# Contributing to OVH Nexus

Thank you for your interest in contributing to **OVH Nexus**! This document outlines the workflow, development setup, and standards for contributing code, documentation, or bug reports.

---

## 1. Code of Conduct

By participating in this project, you agree to abide by our [Code of Conduct](./CODE_OF_CONDUCT.md).

---

## 2. Security & Credential Safety

> [!CAUTION]
> **Never commit or paste real credentials** in code, tests, screenshots, issues, or pull requests:
> - OVHcloud API credentials (`appKey`, `appSecret`, `consumerKey`)
> - Console session secret (`API_SECRET_KEY`)
> - Telegram Bot tokens (`tgToken`) or Chat IDs
> - SMTP credentials (`username`, `password`)

If you discover a security vulnerability, **do not open a public issue**. Please report it privately via [GitHub Security Advisories](https://github.com/ElleryHe/OVH-Nexus/security/advisories/new) as described in [SECURITY.md](./SECURITY.md).

---

## 3. Development Setup

### Prerequisites
- **Docker & Docker Compose** (recommended for end-to-end testing)
- **Node.js 18+** & `npm` (for frontend development)
- **Python 3.11+** (for backend development & unit tests)

### Project Structure
```text
├── backend/
│   ├── app.py                   # Flask API application & multi-account engine
│   ├── api_auth_middleware.py   # Session cookie auth, rate limiting & loopback guards
│   ├── catalog_service.py       # Public OVHcloud catalog dictionary & hardware spec parser
│   ├── sku_watcher.py           # Public availability SKU change watcher & anti-glitch engine
│   ├── server_monitor.py        # Stock subscription & auto-order monitor
│   ├── notifications/           # Pluggable NotificationHub (Telegram & SMTP Email)
│   ├── routes/cloud_fleet.py    # OVHcloud Orders, Services, SSH Keys, VPS & Edge Firewall APIs
│   └── tests/                   # Backend security & functional unit test suite
├── src/
│   ├── components/              # Shared UI components, Sidebar, PasswordGate, NexusLogo
│   ├── context/                 # APIContext, LocaleContext (EN/FR/zh-CN), ThemeContext
│   ├── pages/                   # Console feature pages
│   └── utils/                   # API client & hardware/pricing formatters
├── nginx/                       # Nginx reverse-proxy & security header configuration
└── Dockerfile                   # Multi-stage single-container build
```

### Running Locally for Development

1. **Frontend (Vite HMR)**:
   ```bash
   npm ci
   npm run dev
   ```
2. **Backend (Flask)**:
   ```bash
   cd backend
   pip install -r requirements.txt -r requirements-dev.txt
   API_SECRET_KEY=dev-secret-key PORT=19998 python app.py
   ```
3. **Full Container Build & Verification**:
   ```bash
   docker build -t elleryhe/ovh-nexus:latest .
   docker compose up -d
   ```

---

## 4. Running Tests

All backend changes must pass the security and functional test suite before submitting a pull request:

```bash
pytest backend/tests/test_security.py
```

Frontend changes must compile cleanly without TypeScript or Vite bundling errors:

```bash
npm run build
```

---

## 5. Pull Request Guidelines

1. **Branching**: Create a descriptive feature or bugfix branch from `main` (e.g., `feat/webhook-channel` or `fix/catalog-timeout`).
2. **Commit Messages**: Follow [Conventional Commits](https://www.conventionalcommits.org/):
   - `feat(scope): ...` for new features
   - `fix(scope): ...` for bug fixes
   - `docs: ...` for documentation updates
   - `chore: ...` for build, CI, or dependency maintenance
3. **Internationalization (i18n)**: When adding user-facing strings to core navigation or shared components, update the `en`, `fr`, and `zh-CN` dictionaries in `src/context/LocaleContext.tsx`.
4. **Theme Compatibility**: Ensure UI components remain legible in both **Light** (`Pure White`) and **Dark** (`Carbon Black`) themes using the semantic `cyber-*` Tailwind tokens.

---

## 6. License Agreement

By submitting a contribution to **OVH Nexus**, you agree that your contribution will be licensed under the **GNU Affero General Public License v3.0 (AGPL-3.0)** as set forth in [LICENSE](./LICENSE).
