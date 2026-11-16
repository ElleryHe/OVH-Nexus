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
> - Console session secret (`API_SECRET_KEY`), master key (`DATA_ENCRYPTION_KEY`), or MCP Bearer Tokens (`nxs_mcp_...`)
> - Telegram Bot tokens or Chat IDs
> - SMTP credentials (`username`, `password`)

If you discover a security vulnerability, **do not open a public issue**. Please report it privately via [GitHub Security Advisories](https://github.com/ElleryHe/OVH-Nexus/security/advisories/new) as described in [SECURITY.md](./SECURITY.md).

---

## 3. Development Setup

### Prerequisites

- **Docker & Docker Compose** (recommended for end-to-end verification)
- **Go 1.27+** (for backend server & unit tests, `CGO_ENABLED=0` static build)
- **Node.js 24+** & `npm` (for frontend & documentation development)

### Project Structure

```text
├── server/
│   ├── go.mod / go.sum            # Go 1.27 module (chi/v5, go-ovh, modernc.org/sqlite, x/crypto)
│   ├── cmd/ovh-nexus/             # Single static binary entrypoint
│   └── internal/
│       ├── config/                # Fail-fast env loader & validation (TZ, TRUSTED_PROXIES, keys)
│       ├── vault/                 # HKDF-SHA256 + AES-256-GCM encryption & SHA-256 token hashing
│       ├── store/                 # Pure-Go SQLite WAL (/data/nexus.db) schema & repositories
│       ├── logger/                # Standardized English log/slog hub, SSE broker & redactor
│       ├── auth/                  # HMAC HttpOnly sessions, rate limiter & RFC 6238 TOTP gatekeeper
│       ├── ovhclient/             # Multi-account go-ovh client pool & public HTTP client
│       ├── catalog/               # OVH public catalog parser, enrichment & FQN pricing
│       ├── procurement/           # Cart builder, queue & quick-order engine
│       ├── monitor/               # Goroutine workers (stock, VPS, public SKU watcher, lifecycle)
│       ├── fleet/                 # Dedicated Server, VPS & Account Hub APIs
│       ├── notify/                # Outbound Telegram Bot & SMTP dispatchers (en / fr / zh-CN)
│       ├── mcp/                   # Streamable HTTP /api/mcp JSON-RPC 2.0 + Bidirectional Privacy Shield
│       ├── httpx/                 # Router, Slowloris timeouts, security headers & REST endpoints
│       └── webui/                 # //go:embed SPA static handler
├── src/
│   ├── components/                # Shared UI components, Sidebar, PasswordGate, TotpStepUpModal
│   ├── context/                   # APIContext, LocaleContext (EN/FR/zh-CN), ThemeContext
│   ├── pages/                     # Console feature pages (LogsPage, NotificationCenterPage, MCPGatewayPage, etc.)
│   └── utils/                     # API client & hardware/pricing formatters
├── website/                       # Official tri-lingual documentation portal (Astro Starlight)
└── Dockerfile                     # 3-stage static Go build (node:24-alpine + golang:1.27-alpine -> alpine:3.24)
```

### Running Locally for Development

1. **Frontend (Vite HMR)**:
   ```bash
   npm ci
   npm run dev
   ```
2. **Backend (Go)**:
   ```bash
   cd server
   API_SECRET_KEY="dev-secret-key-at-least-12-chars" \
   DATA_ENCRYPTION_KEY="dev-encryption-key-at-least-16-chars" \
   DATA_DIR="./dev-data" \
   PORT="8080" \
   go run ./cmd/ovh-nexus
   ```
3. **Full Container Build & Verification**:
   ```bash
   docker build -t elleryhe/ovh-nexus:latest .
   docker compose up -d
   ```

---

## 4. Running Tests

All Go backend changes must pass static analysis and the race-enabled unit test suite before submitting a pull request:

```bash
cd server
go vet ./...
go test -race ./...
```

Frontend and documentation changes must compile cleanly without TypeScript, Vite, or Astro bundling errors:

```bash
npx tsc --noEmit
npm run build
npm run build --prefix website
```

---

## 5. Pull Request Guidelines

1. **Branching**: Create a descriptive feature or bugfix branch (e.g., `feat/discord-notifier` or `fix/catalog-timeout`). Keep branch history linear via `git rebase`.
2. **Commit Messages**: Follow [Conventional Commits](https://www.conventionalcommits.org/):
   - `feat(scope): ...` for new features
   - `fix(scope): ...` for bug fixes
   - `docs: ...` for documentation updates
   - `chore: ...` for build, CI, or dependency maintenance
3. **Standardized Logging**: All backend `log/slog` messages must use concise English and one of the canonical `source` modules without exposing raw credentials.
4. **Internationalization (i18n)**: When adding user-facing strings to core navigation, shared components, or docs, keep `en`, `fr`, and `zh-CN` synchronized in `src/context/LocaleContext.tsx` and `website/src/content/docs/`.
5. **Theme Compatibility**: Ensure UI components remain legible in both **Light** (`Pure White`) and **Dark** (`Carbon Black`) themes using the semantic `cyber-*` Tailwind tokens.

---

## 6. License Agreement

By submitting a contribution to **OVH Nexus**, you agree that your contribution will be licensed under the **GNU Affero General Public License v3.0 (AGPL-3.0)** as set forth in [LICENSE](./LICENSE).
