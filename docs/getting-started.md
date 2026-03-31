# Getting Started

This guide walks you through adding VibeWarden to an existing app in three commands
and explains what happens under the hood.

---

## Prerequisites

- **Docker Engine 27+** and **Docker Compose v2+** installed and running.
- Your app listening on a local port (e.g., `3000`).

!!! note "Multi-architecture support"
    VibeWarden Docker images are published as multi-arch manifests covering
    `linux/amd64` and `linux/arm64`. Docker selects the correct image automatically —
    no extra flags needed on Apple Silicon (M1/M2/M3) or ARM64 servers such as
    AWS Graviton.

---

## Step 1 — Install the `vibew` wrapper

The `vibew` script is a thin shell wrapper that downloads the correct VibeWarden
binary for your platform and delegates all commands to it. Commit it to your repo —
it pins the version your team uses.

=== "macOS / Linux"

    ```bash
    curl -fsSL https://vibewarden.dev/vibew > vibew && chmod +x vibew
    ```

=== "Windows (PowerShell)"

    ```powershell
    Invoke-WebRequest -Uri https://vibewarden.dev/vibew.ps1 -OutFile vibew.ps1
    ```

You can also install `vibew` globally:

```bash
sudo mv vibew /usr/local/bin/vibew
```

---

## Step 2 — `vibew init`

Run `vibew init` inside your project directory. Pass `--upstream` with the port
your app listens on. Add feature flags for the security plugins you want enabled.

```bash
./vibew init --upstream 3000 --auth --rate-limit
```

Common flags:

| Flag | Description |
|------|-------------|
| `--upstream <port>` | Port your app listens on (default: auto-detected or 3000) |
| `--auth` | Enable authentication (Ory Kratos) |
| `--rate-limit` | Enable rate limiting |
| `--tls --domain example.com` | Enable TLS (requires `--domain`) |
| `--force` | Overwrite existing files |
| `--skip-wrapper` | Skip vibew wrapper script generation |
| `--agent <type>` | Generate AI context files: `claude`, `cursor`, `generic`, `all`, or `none` |

### What `init` generates

```
vibewarden.yaml          # Main config — commit this
vibew / vibew.ps1        # Wrapper scripts (macOS/Linux/Windows)
.vibewarden-version      # Pinned version
.claude/CLAUDE.md        # AI agent context (Claude Code)
.cursor/rules            # AI agent context (Cursor)
AGENTS.md                # AI agent context (generic)
```

!!! tip "AI agent context"
    `vibew init` generates context files for your AI coding assistant. When you
    ask Claude or Cursor to "add a login page," the AI knows to use Kratos flows
    instead of building auth from scratch. Regenerate after config changes with
    `./vibew context refresh`.

---

## Step 3 — `vibew dev`

Start the full local stack:

```bash
./vibew dev
```

This command:

1. Runs `vibew generate` to produce `.vibewarden/generated/docker-compose.yml`
   from your `vibewarden.yaml`.
2. Starts the stack with `docker compose up`.

Your app is now protected at `https://localhost:8443`.

!!! tip "Trust the self-signed certificate"
    On first run, VibeWarden generates a self-signed CA certificate so your browser
    can open `https://localhost:8443` without TLS errors. Export and trust it with:

    ```bash
    ./vibew cert export > vibewarden-ca.pem
    ```

    Then import `vibewarden-ca.pem` into your browser's or OS's trusted certificate
    store (or pass `--cacert vibewarden-ca.pem` to `curl`).

---

## What just happened

### The stack

`vibew dev` starts several containers:

| Container | Purpose |
|-----------|---------|
| `vibewarden` | The security sidecar — Caddy embedding all middleware |
| `kratos` | Identity server (only when `auth.mode: kratos`) |
| `kratos-db` | Postgres for Kratos (only when `auth.mode: kratos` and no external DB) |
| `openbao` | Secrets manager (only when `secrets.enabled: true`) |

Your app runs outside Docker and is reached from the container network via
`host.docker.internal`. Alternatively, set `app.build` or `app.image` in
`vibewarden.yaml` to include your app in the Compose stack.

### The middleware chain

Every inbound request passes through this ordered chain before reaching your app:

```
Request
   │
   ▼
 IP filter (if enabled)
   │
   ▼
 Rate limiter — per-IP token bucket
   │
   ▼
 Body size limit
   │
   ▼
 WAF — SQLi / XSS / path traversal detection
   │
   ▼
 Authentication — JWT / Kratos / API key
   │
   ▼
 Rate limiter — per-user token bucket
   │
   ▼
 Secret injection into request headers
   │
   ▼
 Upstream (your app)
   │
   ▼
 Security headers added to response
   │
   ▼
 Audit log event emitted
   │
   ▼
Response
```

### Generated files

Runtime files land under `.vibewarden/generated/` (add this to `.gitignore`):

```
.vibewarden/generated/
  docker-compose.yml           # Full stack
  kratos/kratos.yml            # Ory Kratos config
  kratos/identity.schema.json  # Identity schema
  observability/               # Grafana/Prometheus/Loki (when enabled)
```

Do not edit generated files. Re-run `vibew generate` after changing
`vibewarden.yaml`.

---

## Next steps

### Enable authentication

The default JWT mode works with any OIDC provider. Edit `vibewarden.yaml`:

```yaml
auth:
  mode: jwt
  jwt:
    jwks_url: "https://your-provider/.well-known/jwks.json"
    issuer:   "https://your-provider/"
    audience: "your-api-identifier"
  public_paths:
    - /static/*
    - /health
```

See the [Identity Providers guide](identity-providers.md) for step-by-step
examples with Auth0, Keycloak, Firebase, Cognito, Okta, Supabase, and Kratos.

### Add observability

```bash
./vibew add metrics
./vibew dev
```

Open Grafana at `http://localhost:3001` to see request rate, latency percentiles,
rate limit hits, and auth decisions in real time.

!!! tip "Generate a dev JWT"
    Use `vibew token` to mint a signed JWT for local testing without an external
    OIDC provider:

    ```bash
    curl https://localhost:8443/api/me \
      --cacert vibewarden-ca.pem \
      -H "Authorization: Bearer $(./vibew token --json)"
    ```

See the [Observability guide](observability.md) for details.

### Enable TLS for production

```bash
./vibew add tls --domain myapp.example.com
```

This sets `tls.provider: letsencrypt` and `tls.domain` in `vibewarden.yaml`.
On the next `vibew generate` + `docker compose up`, Caddy obtains a certificate
from Let's Encrypt automatically.

See the [Production Deployment guide](production-deployment.md) for the full
production checklist.

### Validate your config

```bash
./vibew validate
```

Reports all validation errors in `vibewarden.yaml` before you start the stack.

---

## Troubleshooting

### Port already in use

VibeWarden defaults to port `8443`. Change it in `vibewarden.yaml`:

```yaml
server:
  port: 9443
```

### App not reachable

If your app does not run inside Docker, verify it is listening on `0.0.0.0`
(not `127.0.0.1`), or override the upstream host:

```yaml
upstream:
  host: host.docker.internal
  port: 3000
```

### Containers not starting

```bash
# Check container health
./vibew status

# Show detailed logs for all containers
./vibew logs

# Diagnose common issues automatically
./vibew doctor
```
