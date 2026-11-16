---
title: Contributing
description: Development setup, testing requirements, and pull request guidelines for OVH Nexus.
---

Thank you for your interest in contributing to **OVH Nexus**! Contributions to the Go backend, frontend console, localization dictionaries, and documentation are welcome.

## Security & Credential Safety

:::danger[Never Commit Credentials]
Never include real OVHcloud API credentials (`appKey`, `appSecret`, `consumerKey`), `API_SECRET_KEY`, `DATA_ENCRYPTION_KEY`, Telegram Bot tokens, SMTP passwords, or private service names / IP addresses in code, tests, issues, or pull requests.

If you discover a security vulnerability, **do not open a public issue**. Report it privately via [GitHub Security Advisories](https://github.com/ElleryHe/OVH-Nexus/security/advisories/new).
:::

## Local Development Setup

### Prerequisites

- **Docker & Docker Compose** (for full multi-stage container verification)
- **Go 1.27+** (for backend server & unit tests)
- **Node.js 24+** & `npm` (for frontend console and `website/` documentation site)

### Running the Console Locally

1. **Frontend (Vite HMR)**:
   ```bash
   npm ci
   npm run dev
   ```
2. **Backend (Go)**:
   ```bash
   cd server
   API_SECRET_KEY="dev-local-random-secret-key" \
   DATA_ENCRYPTION_KEY="dev-local-random-encryption-key" \
   DATA_DIR="./dev-data" \
   PORT="8080" \
   go run ./cmd/ovh-nexus
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
# 1. Go static analysis and race-enabled test suite
cd server && go vet ./... && go test -race ./...

# 2. Frontend TypeScript check & Vite production build
npx tsc --noEmit && npm run build

# 3. Documentation site build & link validation (if editing docs)
npm run build --prefix website
```

## Pull Request Guidelines

- **Conventional Commits**: Use `feat(scope): ...`, `fix(scope): ...`, `docs: ...`, or `chore: ...`.
- **Standardized Logging**: Backend `log/slog` entries must use concise English and canonical `source` names without exposing raw secrets.
- **Tri-Lingual Parity**: When adding user-facing UI keys or documentation pages, keep `en`, `fr`, and `zh-CN` synchronized.
- **Theme Compatibility**: Verify UI components in both **Light** (`Pure White`) and **Dark** (`Neutral Carbon Black`) modes.
