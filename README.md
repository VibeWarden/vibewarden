# VibeWarden

Open-source security sidecar for vibe-coded apps. Zero-to-secure in minutes.

VibeWarden sits between the internet and your app as a reverse proxy, handling
TLS, authentication, rate limiting, security headers, and structured logging —
so you don't have to build any of it yourself.

## Why VibeWarden?

If you're building with AI coding tools (Claude Code, Cursor, Copilot), you
ship fast — but security often gets skipped. VibeWarden adds production-grade
security to any app without changing a single line of your code.

- **Automatic TLS** via Let's Encrypt (or self-signed for dev)
- **Authentication** via Ory Kratos — login, registration, session management
- **Rate limiting** — per-IP and per-user, token bucket algorithm
- **Security headers** — HSTS, CSP, X-Frame-Options, and more
- **AI-readable structured logs** — every security event follows a versioned JSON schema
- **Prometheus metrics** with a pre-built Grafana dashboard
- **Admin API** for user management with audit logging

## Quick Start

```bash
# Download the wrapper script
curl -fsSL https://vibewarden.dev/vibew > vibew && chmod +x vibew

# Scaffold VibeWarden into your project
./vibew init --upstream 3000 --auth --rate-limit

# Start everything
./vibew dev
```

That's it. Your app on port 3000 is now behind VibeWarden with TLS, auth,
rate limiting, and security headers.

### What `init` generates

```
vibewarden.yaml          # VibeWarden configuration
docker-compose.yml       # Full stack: VibeWarden + Kratos + Postgres
vibew                    # Wrapper script (macOS/Linux)
vibew.ps1                # Wrapper script (Windows)
vibew.cmd                # Wrapper script (Windows)
.vibewarden-version      # Pinned version
.claude/CLAUDE.md        # AI agent context (Claude Code)
.cursor/rules            # AI agent context (Cursor)
AGENTS.md                # AI agent context (generic)
```

### Windows

```powershell
Invoke-WebRequest -Uri https://vibewarden.dev/vibew.ps1 -OutFile vibew.ps1
.\vibew.ps1 init --upstream 3000 --auth --rate-limit
.\vibew.ps1 dev
```

## How It Works

```
Internet → VibeWarden (port 8080) → Your App (port 3000)
              │
              ├── TLS termination
              ├── Security headers
              ├── Authentication (Ory Kratos)
              ├── Rate limiting
              ├── Structured logging
              └── Metrics collection
```

VibeWarden runs as a local sidecar next to your app. It is never hosted
remotely — it always runs on the same machine as the app it protects.

## Features

### Authentication

VibeWarden uses [Ory Kratos](https://www.ory.sh/kratos/) for identity
management. Your app receives authenticated user info via headers:

| Header | Description |
|--------|-------------|
| `X-User-Id` | Kratos identity UUID |
| `X-User-Email` | Primary email address |
| `X-User-Verified` | Email verification status (`true`/`false`) |

Configure public paths that skip authentication:

```yaml
auth:
  public_paths:
    - /static/*
    - /api/public/*
    - /health
```

### Rate Limiting

Dual rate limiting with token bucket algorithm:

```yaml
rate_limit:
  per_ip:
    requests_per_second: 10
    burst: 20
  per_user:
    requests_per_second: 100
    burst: 200
```

Blocked requests get a `429 Too Many Requests` response with a `Retry-After` header.

### TLS

Three provider modes:

| Provider | Use case |
|----------|----------|
| `letsencrypt` | Production — automatic ACME certificates |
| `self-signed` | Development — Caddy generates internal certs |
| `external` | You provide cert/key files (Cloudflare, ACM, etc.) |

### Security Headers

All responses include security headers by default:

- `Strict-Transport-Security` (HTTPS only)
- `X-Content-Type-Options: nosniff`
- `X-Frame-Options: DENY`
- `Content-Security-Policy: default-src 'self'`
- `Referrer-Policy: strict-origin-when-cross-origin`

Every header is individually configurable or disableable in `vibewarden.yaml`.

### AI-Readable Structured Logs

Every security event follows a versioned JSON schema:

```json
{
  "schema_version": "v1",
  "event_type": "auth.failed",
  "timestamp": "2026-03-26T10:30:00Z",
  "ai_summary": "Authentication failed for IP 192.168.1.1: invalid session",
  "payload": {
    "ip": "192.168.1.1",
    "reason": "invalid_session"
  }
}
```

Pretty-print logs in your terminal:

```bash
./vibew logs --follow
```

### Observability

Prometheus metrics at `/_vibewarden/metrics` with a pre-built Grafana dashboard.

```bash
# Start with observability stack
./vibew dev --observability

# Open Grafana
make grafana-open    # http://localhost:3000
```

See [docs/observability.md](docs/observability.md) for details.

### Admin API

User management at `/_vibewarden/admin/*` (protected by bearer token):

```bash
# List users
curl -H "X-Admin-Key: $TOKEN" http://localhost:8080/_vibewarden/admin/users

# Create user
curl -X POST -H "X-Admin-Key: $TOKEN" \
  -d '{"email":"user@example.com","password":"..."}' \
  http://localhost:8080/_vibewarden/admin/users
```

## CLI Reference

| Command | Description |
|---------|-------------|
| `vibew init` | Scaffold VibeWarden into a project |
| `vibew add auth` | Enable authentication |
| `vibew add rate-limit` | Enable rate limiting |
| `vibew add tls --domain example.com` | Enable TLS |
| `vibew add observability` | Add Prometheus + Grafana |
| `vibew dev` | Start local dev environment |
| `vibew status` | Show health of all components |
| `vibew doctor` | Diagnose common issues |
| `vibew logs` | Pretty-print structured logs |
| `vibew secret generate` | Generate secure tokens |
| `vibew validate` | Validate configuration |
| `vibew context refresh` | Regenerate AI agent context |

## AI Agent Context

When you run `vibew init`, VibeWarden generates context files for your AI
coding agent. This tells the AI how to work with VibeWarden — so when you say
"add a login page," it knows to use Kratos flows instead of building auth
from scratch.

Supported agents:
- **Claude Code** — `.claude/CLAUDE.md`
- **Cursor** — `.cursor/rules`
- **Generic** — `AGENTS.md`

Regenerate after config changes:

```bash
./vibew context refresh
```

## Configuration

Copy the example config and customize:

```bash
cp vibewarden.example.yaml vibewarden.yaml
```

See [`vibewarden.example.yaml`](vibewarden.example.yaml) for all options with
detailed comments.

## Demo

A complete demo app is included at [`examples/demo-app/`](examples/demo-app/):

```bash
cd examples/demo-app
docker compose up -d
# Open http://localhost:8080
```

The demo shows authentication, rate limiting, security headers, and identity
header injection in action.

## Architecture

VibeWarden is built with hexagonal architecture (ports and adapters):

```
cmd/vibewarden/          # CLI entrypoint
internal/
  domain/                # Pure domain logic (zero external deps)
  ports/                 # Interface definitions
  adapters/              # Implementations (Caddy, Kratos, Postgres, ...)
  app/                   # Application services
  middleware/            # HTTP middleware
  config/                # Configuration loading
migrations/              # Database migrations
```

## License

Apache 2.0 — see [LICENSE](LICENSE).

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for development setup, coding standards,
and how to submit changes.
