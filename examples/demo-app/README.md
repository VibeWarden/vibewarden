# VibeWarden Demo App

A minimal Go HTTP server (stdlib only) that demonstrates every major VibeWarden
feature.  The app itself performs no authentication — it simply trusts the
headers injected by the VibeWarden sidecar.

## Workflow

This demo showcases the intended VibeWarden user workflow:

1. Commit only `vibewarden.yaml` — the single source of truth
2. Run `vibewarden generate` — generates the full runtime stack
3. Run `docker compose -f .vibewarden/generated/docker-compose.yml up`

Generated files are written to `.vibewarden/generated/` which is gitignored.

### Quick start

```bash
cd examples/demo-app
make demo
# Visit http://localhost:8080
```

Wait ~30 seconds for the full stack to be healthy.

| Service | URL | Credentials |
|---|---|---|
| Demo app (via VibeWarden) | http://localhost:8080 | see Demo credentials below |
| Grafana | http://localhost:3001 | admin / admin |
| Prometheus | http://localhost:9090 | — |
| Kratos public API | http://localhost:4433 | — |
| OpenBao (secrets) | http://localhost:8200 | token: see `.vibewarden/generated/.credentials` |

Or step by step:

```bash
cd examples/demo-app
../../bin/vibewarden generate
COMPOSE_PROFILES=observability \
  docker compose -f .vibewarden/generated/docker-compose.yml up -d
```

### TLS — self-signed HTTPS

```bash
make demo-tls
# Visit https://localhost:8443  (accept the self-signed certificate warning)
# Grafana: http://localhost:3001
```

Or manually:

```bash
cd examples/demo-app
VIBEWARDEN_TLS_ENABLED=true \
VIBEWARDEN_TLS_PROVIDER=self-signed \
VIBEWARDEN_SERVER_PORT=8443 \
  ../../bin/vibewarden generate
COMPOSE_PROFILES=observability \
  docker compose -f .vibewarden/generated/docker-compose.yml up -d
```

## Teardown

```bash
make demo-down          # stop containers, keep volumes
make demo-clean         # stop containers and remove all volumes + generated files
```

Or manually:

```bash
cd examples/demo-app
docker compose -f .vibewarden/generated/docker-compose.yml down
docker compose -f .vibewarden/generated/docker-compose.yml down -v
```

## What this demo exercises

| Feature | Config key | What you see |
|---|---|---|
| Auth (Ory Kratos) | `auth.enabled: true` | Login redirect, session cookie, `X-User-*` headers |
| Rate limiting | `rate_limit.enabled: true` | 429 after 10 requests burst |
| Security headers | `security_headers.enabled: true` | HSTS, CSP, X-Frame-Options on every response |
| Secrets injection | `secrets.enabled: true` | `X-Demo-Api-Key` header injected from OpenBao |
| Metrics | `metrics.enabled: true` | Prometheus scrapes VibeWarden metrics |
| Observability | `observability.enabled: true` | Grafana dashboard, Loki log aggregation |

## Demo UI

A plain HTML + vanilla JS frontend is embedded directly in the binary (no
build step required).  Four pages showcase each VibeWarden feature visually:

| Page | URL | What it shows |
|---|---|---|
| Home | `/static/index.html` | Auth status, VibeWarden health badge, login / register / logout |
| My Profile | `/static/me.html` | User ID, email, and verification status from VibeWarden headers |
| Headers Inspector | `/static/headers.html` | All response headers, security headers highlighted green / red |
| Rate Limit Test | `/static/ratelimit.html` | Fire 20 rapid requests and watch 429s appear in real time |

The UI uses [water.css](https://watercss.kognise.dev/) (MIT) loaded locally
for a clean, classless style with zero build tooling.

## Architecture

```
Browser / curl
    |
    | :8080 (dev) or :8443 (tls)
    v
+-------------------+
|    VibeWarden      |  <-- auth check (Kratos), rate limiting, security headers, secrets injection
+-------------------+
    |
    | :3000 (internal)
    v
+-------------------+
|    demo-app        |  <-- your Go application (trusts sidecar headers)
+-------------------+
```

Generated stack:

```
.vibewarden/generated/
  docker-compose.yml        # full stack definition
  .credentials              # auto-generated secrets (gitignored)
  .env.template             # non-secret config template
  kratos/                   # Kratos identity server config
  seed-secrets.sh           # seeds OpenBao with demo secrets
  observability/            # Prometheus, Grafana, Loki, Promtail configs
```

## Endpoints

### `GET /` — Greeting (public)

Returns a personalised greeting when logged in, or a generic welcome when not.

**Demonstrates:** VibeWarden forwards `X-User-Id` and `X-User-Email` headers
from the validated Kratos session.

```bash
curl http://localhost:8080/
# {"authenticated":false,"message":"Welcome! Please log in."}
```

### `GET /profile` — Active profile info (public)

Returns the active demo profile and which feature sets are available.

```bash
curl http://localhost:8080/profile
# {"observability_enabled":false,"profile":"dev","tls_enabled":false}
```

### `GET /public` — Public endpoint (no auth required)

Always returns a timestamp. VibeWarden skips auth for this path.

**Demonstrates:** `public_paths` configuration — no Kratos check, no redirect.

```bash
curl http://localhost:8080/public
# {"message":"This is a public endpoint","timestamp":"2025-01-15T12:00:00Z"}
```

### `GET /me` — User profile (protected)

Returns the authenticated user's ID, email, and email-verification status.
Returns 401 if the request did not pass through VibeWarden (no session cookie).

```bash
curl -b cookies.txt http://localhost:8080/me
# {"email":"alice@example.com","user_id":"...","verified":"true"}
```

### `GET /headers` — Echo request headers

Returns all incoming request headers as a JSON object.

**Demonstrates:** The full set of headers VibeWarden adds:
`X-User-Id`, `X-User-Email`, `X-User-Verified`, `X-Request-Id`, plus all
security response headers visible in the response.

```bash
curl http://localhost:8080/headers
```

### `POST /spam` — Rate-limit trigger

Increments an in-memory counter.  Hitting this endpoint rapidly will trigger
VibeWarden's rate limiter.

```bash
for i in $(seq 1 20); do
  curl -s -o /dev/null -w "%{http_code}\n" -X POST http://localhost:8080/spam
done
# 200 x 10, then 429 x 10
```

### `GET /health` — Liveness check (public)

Returns `{"status":"ok"}`.

```bash
curl http://localhost:8080/health
# {"status":"ok"}
```

## Demo credentials

Two users are seeded automatically when the stack starts.  No registration
step required — just use these at the login page:

| Email | Password |
|---|---|
| `demo@vibewarden.dev` | `demo1234` |
| `alice@vibewarden.dev` | `alice1234` |

Both accounts have their email address pre-verified so you can immediately
access protected endpoints without completing a verification flow.

## Registration and login

VibeWarden proxies Kratos self-service flows, so you can register and log in
through the same `:8080` port:

```bash
# Start a browser login flow
open http://localhost:8080/self-service/login/browser

# Start a browser registration flow
open http://localhost:8080/self-service/registration/browser
```

Verification emails are captured by Mailslurper at http://localhost:4437 —
no real email is sent.

## Security headers

Every response from VibeWarden includes:

- `Strict-Transport-Security`
- `X-Content-Type-Options: nosniff`
- `X-Frame-Options: DENY`
- `Content-Security-Policy: default-src 'self'`
- `Referrer-Policy: strict-origin-when-cross-origin`

Inspect them with:

```bash
curl -I http://localhost:8080/public
```
