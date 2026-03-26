# VibeWarden Demo App

A minimal Go HTTP server (stdlib only) that demonstrates every major VibeWarden
feature.  The app itself performs no authentication — it simply trusts the
headers injected by the VibeWarden sidecar.

## Quickstart

```bash
cd examples/demo-app
docker compose up -d
# Visit http://localhost:8080
```

Wait ~15 seconds for the full stack to be healthy.  Your browser will be
redirected to the demo UI at `http://localhost:8080/static/index.html`.

## Full Demo Stack

The full demo stack adds Prometheus metrics, Grafana dashboards, and Loki log
aggregation on top of the basic stack.  It also enables self-signed TLS so you
can exercise the HTTPS path locally.

```bash
cd examples/demo-app
docker compose -f docker-compose.local-demo.yml up -d
# Visit https://localhost:8443  (accept the self-signed certificate warning)
```

Wait ~30 seconds for all services to become healthy.

### Access URLs

| Service | URL | Credentials |
|---|---|---|
| Demo app (via VibeWarden) | https://localhost:8443 | see Demo credentials below |
| Grafana | http://localhost:3000 | admin / admin |
| Prometheus | http://localhost:9090 | — |
| Loki (ready check) | http://localhost:3100/ready | — |
| Kratos public API | http://localhost:4433 | — |
| Mailslurper (email UI) | http://localhost:4437 | — |

### What the full stack demonstrates

- **Self-signed TLS** — VibeWarden terminates HTTPS using a Caddy-generated
  self-signed certificate.  No domain or ACME account required.
- **Metrics** — Prometheus scrapes `/_vibewarden/metrics` every 15 s.  Open
  Grafana and navigate to the VibeWarden dashboard to see request rates, latency
  histograms, rate-limit hits, and auth decisions in real time.
- **Log aggregation** — Promtail tails every container's stdout/stderr via the
  Docker socket and ships structured JSON logs to Loki.  Grafana's Explore view
  lets you query them with LogQL.
- **Pre-provisioned dashboards** — Grafana starts with the Prometheus and Loki
  datasources already configured and the VibeWarden dashboard pre-loaded.

### Teardown

```bash
docker compose -f docker-compose.local-demo.yml down        # stop containers
docker compose -f docker-compose.local-demo.yml down -v     # also remove volumes
```

## Demo UI

A plain HTML + vanilla JS frontend is embedded directly in the binary (no
build step required).  Four pages showcase each VibeWarden feature visually:

| Page | URL | What it shows |
|---|---|---|
| Home | `/static/index.html` | Auth status, VibeWarden health badge, login / register / logout |
| My Profile | `/static/me.html` | User ID, email, and verification status from VibeWarden headers |
| Headers Inspector | `/static/headers.html` | All response headers, security headers highlighted green / red |
| Rate Limit Test | `/static/ratelimit.html` | Fire 20 rapid requests and watch 429s appear in real time |

The UI uses [water.css](https://watercss.kognise.dev/) (MIT) loaded from
jsDelivr CDN for a clean, classless style with zero build tooling.

## Architecture

```
Browser / curl
    |
    | :8080
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

# After logging in (cookie set by Kratos)
curl -b cookies.txt http://localhost:8080/
# {"authenticated":true,"message":"Welcome, alice@example.com!"}
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

**Demonstrates:** Protected route — app trusts sidecar-injected identity headers.

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
VibeWarden's rate limiter (configured at 5 req/s per IP, burst 10).

**Demonstrates:** Rate limiting — the 11th back-to-back request gets a
`429 Too Many Requests` response with a `Retry-After` header.

```bash
for i in $(seq 1 20); do
  curl -s -o /dev/null -w "%{http_code}\n" -X POST http://localhost:8080/spam
done
# 200 x 10, then 429 x 10
```

### `GET /health` — Liveness check (public)

Returns `{"status":"ok"}`.

**Demonstrates:** Health endpoint excluded from auth and rate limiting.

```bash
curl http://localhost:8080/health
# {"status":"ok"}
```

## Services

### Basic stack (`docker-compose.yml`)

| Service | URL | Description |
|---|---|---|
| VibeWarden | http://localhost:8080 | Security sidecar — the entry point |
| Kratos public API | http://localhost:4433 | Self-service auth flows |
| Kratos admin API | http://localhost:4434 | Internal admin (not for browsers) |
| Mailslurper | http://localhost:4437 | Catches Kratos verification emails |

### Full demo stack (`docker-compose.local-demo.yml`)

| Service | URL | Description |
|---|---|---|
| VibeWarden (HTTPS) | https://localhost:8443 | Security sidecar with self-signed TLS |
| Grafana | http://localhost:3000 | Pre-provisioned dashboards (admin / admin) |
| Prometheus | http://localhost:9090 | Metrics storage and query UI |
| Loki | http://localhost:3100 | Log aggregation backend |
| Kratos public API | http://localhost:4433 | Self-service auth flows |
| Kratos admin API | http://localhost:4434 | Internal admin (not for browsers) |
| Mailslurper | http://localhost:4437 | Catches Kratos verification emails |

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

## Teardown

```bash
docker compose down        # stop containers
docker compose down -v     # also remove the Postgres volume
```
