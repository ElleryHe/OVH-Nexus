# Security Policy

## Supported Versions

Security updates are applied to the latest release on the `main` branch and the official `elleryhe/ovh-nexus:latest` Docker image.

| Version | Supported          |
| ------- | ------------------ |
| `1.2.x` | :white_check_mark: |
| `1.1.x` | :white_check_mark: |
| `1.0.x` | :x:                |
| `< 1.0` | :x:                |

---

## Reporting a Vulnerability

> [!IMPORTANT]
> **Please do NOT report security vulnerabilities through public GitHub issues, discussions, or pull requests.**

Instead, please report security vulnerabilities privately using **GitHub Private Vulnerability Reporting**:

- **Report URL**: [Open a Private Security Advisory](https://github.com/ElleryHe/OVH-Nexus/security/advisories/new)

### What to Include in Your Report
1. **Description**: A clear description of the vulnerability and the affected component (e.g., `backend/api_auth_middleware.py`, `backend/mcp/privacy_shield.py`, `nginx/nginx.conf`, `NotificationHub`, etc.).
2. **Reproduction Steps**: Minimal, reproducible steps or a proof-of-concept request/payload.
3. **Impact Assessment**: Potential impact (e.g., authentication bypass, secret exposure, SSRF, XSS, CSRF).
4. **Environment Details**: Deployment mode (Docker Compose, reverse proxy setup) and version/commit hash.

We aim to acknowledge receipt of vulnerability reports within **72 hours** and will coordinate disclosure once a fix is released.

---

## Security Best Practices for Self-Hosting

When deploying **OVH Nexus** in production:

1. **Use a Strong `API_SECRET_KEY`**: Generate a high-entropy random secret (`openssl rand -hex 32`) in your `.env` file. The container refuses to start if `API_SECRET_KEY` is unset or weak.
2. **Enable `COOKIE_SECURE=true` Behind HTTPS**: When exposing the console through a TLS-terminating reverse proxy (Nginx, Caddy, Cloudflare Tunnel, Traefik), set `COOKIE_SECURE=true` and configure `ALLOWED_ORIGINS` to your exact domain.
3. **Principle of Least Privilege for OVHcloud API Keys**: Only grant the OVHcloud API permissions required for your workflow, and rotate Consumer Keys periodically via the OVHcloud Token Manager.
4. **MCP AI Gateway Hardening (`/api/mcp`)**:
   - Keep the **MCP AI Gateway** disabled in `/mcp` when not actively in use.
   - Always connect external AI clients over HTTPS when accessing `/api/mcp` across a network, and restrict exposed accounts via the **Multi-Account Visibility Whitelist**.
   - Rotate or revoke the dedicated `nxs_mcp_...` Bearer Token immediately if an AI client configuration file is shared or compromised.
5. **Redact Logs Before Sharing**: Although OVH Nexus masks secrets (`****`) in API responses and pseudonymizes assets (`bm-01`, `vps-01`, `ord-01`) in MCP responses, always verify that any log snippets shared in public issues do not contain raw tokens, private IP addresses, or internal hostnames.
