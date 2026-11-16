# Security Policy

## Supported Versions

Security updates are applied to the latest release on the `main` branch and the official `elleryhe/ovh-nexus:latest` Docker image.

| Version | Supported          |
| ------- | ------------------ |
| `1.3.x` | :white_check_mark: |
| `1.2.x` | :x:                |
| `1.1.x` | :x:                |
| `< 1.1` | :x:                |

---

## Reporting a Vulnerability

> [!IMPORTANT]
> **Please do NOT report security vulnerabilities through public GitHub issues, discussions, or pull requests.**

Instead, please report security vulnerabilities privately using **GitHub Private Vulnerability Reporting**:

- **Report URL**: [Open a Private Security Advisory](https://github.com/ElleryHe/OVH-Nexus/security/advisories/new)

### What to Include in Your Report
1. **Description**: A clear description of the vulnerability and the affected component (e.g., `server/internal/auth`, `server/internal/vault`, `server/internal/mcp`, `server/internal/httpx`, etc.).
2. **Reproduction Steps**: Minimal, reproducible steps or a proof-of-concept request/payload.
3. **Impact Assessment**: Potential impact (e.g., authentication bypass, secret exposure, SSRF, XSS, CSRF).
4. **Environment Details**: Deployment mode (Docker Compose, reverse proxy setup, `TRUSTED_PROXIES` configuration) and version/commit hash.

We aim to acknowledge receipt of vulnerability reports within **72 hours** and will coordinate disclosure once a fix is released.

---

## Security Best Practices for Self-Hosting

When deploying **OVH Nexus** in production:

1. **Use Strong, Independent Master Keys**: Generate high-entropy values for both `API_SECRET_KEY` (`openssl rand -hex 24`) and `DATA_ENCRYPTION_KEY` (`openssl rand -hex 32`) in your `.env` file. The Go binary refuses to start if either is missing, weak, or identical.
2. **Enable `COOKIE_SECURE=true` & Configure `TRUSTED_PROXIES` Behind HTTPS**: When exposing the console through a TLS-terminating reverse proxy (Caddy, Nginx, Cloudflare Tunnel, Traefik), set `COOKIE_SECURE=true` and specify your proxy IP/CIDR in `TRUSTED_PROXIES` so `X-Forwarded-For` / `X-Real-IP` headers are accepted strictly from trusted hops.
3. **Principle of Least Privilege for OVHcloud API Keys**: Only grant the OVHcloud API permissions required for your workflow, and rotate Consumer Keys periodically via the OVHcloud Token Manager.
4. **Enable RFC 6238 TOTP 2FA**: Activate Two-Factor Authentication immediately after first login in **Settings** and store your 8 single-use recovery codes offline. High-risk operations and MCP `L3` tools remain locked until 2FA is active.
5. **MCP AI Gateway Hardening (`/api/mcp`)**:
   - Keep the **MCP AI Gateway** disabled in `/mcp` when not actively in use.
   - Always connect external AI clients over HTTPS when accessing `/api/mcp` across a network, and restrict exposed accounts via the **Multi-Account Visibility Whitelist**.
   - Rotate or revoke the dedicated `nxs_mcp_...` Bearer Token immediately if an AI client configuration file is shared or compromised.
6. **Redact Logs Before Sharing**: Although OVH Nexus automatically redacts secrets in its `log/slog` pipeline and masks credentials (`****`) in API responses, always review exported logs or diagnostic snippets before sharing them in public issues.
