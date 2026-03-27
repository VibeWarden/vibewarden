# VibeWarden Demo App

A minimal Go HTTP server (stdlib only) that demonstrates every major VibeWarden
feature.  The app itself performs no authentication — it simply trusts the
headers injected by the VibeWarden sidecar.

## Profiles

All demo variants now live in a single `docker-compose.yml`.
Pick a profile with the `VIBEWARDEN_PROFILE` and `COMPOSE_PROFILES` environment
variables.

### Default — HTTP dev stack

```bash
cd examples/demo-app
docker compose up -d
# Visit http://localhost:8080
```

Wait ~15 seconds for the full stack to be healthy.  Your browser will be
redirected to the demo UI at `http://localhost:8080/static/index.html`.

| Service | URL | Credentials |
|---|---|---|
| Demo app (via VibeWarden) | http://localhost:8080 | see Demo credentials below |
| Kratos public API | http://localhost:4433 | — |
| Kratos admin API | http://localhost:4434 | — |
| Mailslurper (email UI) | http://localhost:4437 | — |

### TLS — self-signed HTTPS

```bash
cd examples/demo-app
VIBEWARDEN_PROFILE=tls \
VIBEWARDEN_HTTP_PORT=8443 \
VIBEWARDEN_SERVER_PORT=8443 \
VIBEWARDEN_TLS_ENABLED=true \
VIBEWARDEN_TLS_PROVIDER=self-signed \
KRATOS_PUBLIC_BASE_URL=https://localhost:8443/ \
docker compose up -d
# Visit https://localhost:8443  (accept the self-signed certificate warning)
```

Or put the variables in a `.env` file (copy `.env.example` as a starting point):

```bash
cp .env.example .env
# Set VIBEWARDEN_PROFILE=tls and the related variables in .env
docker compose up -d
```

### Observability — HTTP + Prometheus / Grafana / Loki

```bash
cd examples/demo-app
COMPOSE_PROFILES=observability docker compose up -d
# Demo app: http://localhost:8080
# Grafana:  http://localhost:3001  (admin / admin)
```

Wait ~30 seconds for all services to become healthy.

| Service | URL | Credentials |
|---|---|---|
| Demo app (via VibeWarden) | http://localhost:8080 | see Demo credentials below |
| Grafana | http://localhost:3001 | admin / admin |
| Prometheus | http://localhost:9090 | — |
| Loki (ready check) | http://localhost:3100/ready | — |
| Kratos public API | http://localhost:4433 | — |
| Mailslurper (email UI) | http://localhost:4437 | — |

### Full — TLS + observability

```bash
cd examples/demo-app
VIBEWARDEN_PROFILE=tls \
VIBEWARDEN_HTTP_PORT=8443 \
VIBEWARDEN_SERVER_PORT=8443 \
VIBEWARDEN_TLS_ENABLED=true \
VIBEWARDEN_TLS_PROVIDER=self-signed \
KRATOS_PUBLIC_BASE_URL=https://localhost:8443/ \
COMPOSE_PROFILES=full \
docker compose up -d
# Visit https://localhost:8443  (accept the self-signed certificate warning)
# Grafana: http://localhost:3001
```

### Production — Let's Encrypt (public server)

```bash
cd examples/demo-app
cp .env.example .env
# Edit .env: set VIBEWARDEN_PROFILE=prod, DOMAIN, POSTGRES_PASSWORD, GRAFANA_ADMIN_PASSWORD, etc.
COMPOSE_PROFILES=observability docker compose up -d
```

Required `.env` variables for prod:

| Variable | Example |
|---|---|
| `VIBEWARDEN_PROFILE` | `prod` |
| `VIBEWARDEN_HTTP_PORT` | `443` |
| `VIBEWARDEN_SERVER_PORT` | `443` |
| `VIBEWARDEN_TLS_ENABLED` | `true` |
| `VIBEWARDEN_TLS_PROVIDER` | `letsencrypt` |
| `VIBEWARDEN_TLS_DOMAIN` | `challenge.vibewarden.dev` |
| `KRATOS_PUBLIC_BASE_URL` | `https://challenge.vibewarden.dev/` |
| `POSTGRES_PASSWORD` | _(strong secret)_ |
| `GRAFANA_ADMIN_PASSWORD` | _(strong secret)_ |

## What each profile demonstrates

| Profile | TLS | Observability | Rate limits |
|---|---|---|---|
| `dev` (default) | HTTP | none | 5 req/s per IP |
| `tls` | self-signed HTTPS | none | 5 req/s per IP |
| `observability` | HTTP | Prometheus + Grafana + Loki | 5 req/s per IP |
| `full` | self-signed HTTPS | Prometheus + Grafana + Loki | 5 req/s per IP |
| `prod` | Let's Encrypt | optional | configurable |

The landing page at `/static/index.html` detects the active profile and
shows or hides sections accordingly (TLS badge, observability links, etc.).

## Teardown

```bash
docker compose down           # stop containers, keep volumes
docker compose down -v        # stop containers and remove volumes

# With a non-default profile:
COMPOSE_PROFILES=observability docker compose down
COMPOSE_PROFILES=observability docker compose down -v
```

## Demo UI

A plain HTML + vanilla JS frontend is embedded directly in the binary (no
build step required).  Four pages showcase each VibeWarden feature visually:

| Page | URL | What it shows |
|---|---|---|
| Home | `/static/index.html` | Auth status, VibeWarden health badge, profile banner, login / register / logout |
| My Profile | `/static/me.html` | User ID, email, and verification status from VibeWarden headers |
| Headers Inspector | `/static/headers.html` | All response headers, security headers highlighted green / red |
| Rate Limit Test | `/static/ratelimit.html` | Fire 20 rapid requests and watch 429s appear in real time |

The UI uses [water.css](https://watercss.kognise.dev/) (MIT) loaded locally
for a clean, classless style with zero build tooling.

## Architecture

```
Browser / curl
    |
    | :8080 (dev) or :8443 (tls) or :443 (prod)
    v
+-------------------+
|    VibeWarden      |  <-- auth check (Kratos), rate limiting, security headers
+-------------------+
    |
    | :3000 (internal)
    v
+-------------------+
|    demo-app        |  <-- your Go application (trusts sidecar headers)
+-------------------+
```

## Endpoints

### `GET /` — Greeting (public)

Returns a personalised greeting when logged in, or a generic welcome when not.

**Demonstrates:** VibeWarden forwards `X-User-Id` and `X-User-Email` headers
from the validated Kratos session.

```bash
# Unauthenticated
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
