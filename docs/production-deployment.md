# Production Deployment Guide

This guide walks you through deploying VibeWarden in production using Docker Compose
on a single Linux server. VibeWarden is a security sidecar — it always runs locally,
next to your app, and is never hosted on a separate machine from the app it protects.

---

## Prerequisites

### Server requirements

- **OS**: Ubuntu 24.04 LTS or Debian 12 (recommended). Any Linux distribution with
  Docker Engine 27+ and Docker Compose v2+ works.
- **RAM**: 512 MB minimum. 1 GB+ recommended when running the full stack
  (VibeWarden + Kratos + Postgres).
- **CPU**: 1 vCPU minimum. 2 vCPU recommended.
- **Disk**: 10 GB minimum for the OS, Docker images, and Postgres data.
- **Open ports**: 80 (HTTP, used for ACME HTTP-01 challenge) and 443 (HTTPS).

### Software

Install Docker Engine and Docker Compose v2 on the server:

```bash
curl -fsSL https://get.docker.com | sh
```

Verify versions:

```bash
docker --version        # 27.0.0 or newer
docker compose version  # v2.28.0 or newer
```

### Domain and DNS

- A registered domain name (e.g. `myapp.example.com`).
- An **A record** pointing your domain to the server's public IP address.
  Let's Encrypt's ACME HTTP-01 challenge requires this before issuing a certificate.

Verify DNS propagation before starting:

```bash
dig +short myapp.example.com
# must return your server's IP
```

---

## Directory layout

Create a working directory on the server:

```bash
mkdir -p /opt/myapp && cd /opt/myapp
mkdir -p config/kratos
```

You will end up with:

```
/opt/myapp/
  docker-compose.yml
  vibewarden.yaml
  config/
    kratos/
      kratos.yml
      identity.schema.json
  .env              # secrets — never commit
```

---

## Step 1: Write docker-compose.yml

```yaml
# /opt/myapp/docker-compose.yml
services:
  postgres:
    image: postgres:17-alpine
    container_name: myapp-postgres
    restart: unless-stopped
    environment:
      POSTGRES_USER:     ${POSTGRES_USER}
      POSTGRES_PASSWORD: ${POSTGRES_PASSWORD}
      POSTGRES_DB:       ${POSTGRES_DB}
    volumes:
      - postgres_data:/var/lib/postgresql/data
    networks:
      - myapp
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U ${POSTGRES_USER} -d ${POSTGRES_DB}"]
      interval: 5s
      timeout: 5s
      retries: 10

  kratos-migrate:
    image: oryd/kratos:v1.3.1
    container_name: myapp-kratos-migrate
    restart: on-failure
    environment:
      DSN: postgres://${POSTGRES_USER}:${POSTGRES_PASSWORD}@postgres:5432/${POSTGRES_DB}?sslmode=disable
    volumes:
      - ./config/kratos:/etc/config/kratos:ro
    command: migrate sql -e --yes
    depends_on:
      postgres:
        condition: service_healthy
    networks:
      - myapp

  kratos:
    image: oryd/kratos:v1.3.1
    container_name: myapp-kratos
    restart: unless-stopped
    environment:
      DSN: postgres://${POSTGRES_USER}:${POSTGRES_PASSWORD}@postgres:5432/${POSTGRES_DB}?sslmode=disable
      SERVE_PUBLIC_BASE_URL: https://${DOMAIN}/
      SERVE_ADMIN_BASE_URL: http://kratos:4434/
    volumes:
      - ./config/kratos:/etc/config/kratos:ro
    command: serve --config /etc/config/kratos/kratos.yml --watch-courier
    depends_on:
      kratos-migrate:
        condition: service_completed_successfully
    networks:
      - myapp
    healthcheck:
      test: ["CMD", "wget", "-q", "--spider", "http://kratos:4434/admin/health/ready"]
      interval: 10s
      timeout: 5s
      retries: 10
      start_period: 10s

  vibewarden:
    image: ghcr.io/vibewarden/vibewarden:latest
    container_name: myapp-vibewarden
    restart: unless-stopped
    ports:
      - "80:80"
      - "443:443"
    volumes:
      - ./vibewarden.yaml:/vibewarden.yaml:ro
      - caddy_data:/root/.local/share/caddy
    environment:
      VIBEWARDEN_KRATOS_PUBLIC_URL: http://kratos:4433
      VIBEWARDEN_KRATOS_ADMIN_URL:  http://kratos:4434
      VIBEWARDEN_UPSTREAM_HOST:     myapp        # your app's service name
      VIBEWARDEN_UPSTREAM_PORT:     "3000"
      VIBEWARDEN_SERVER_HOST:       "0.0.0.0"
      VIBEWARDEN_ADMIN_TOKEN:       ${VIBEWARDEN_ADMIN_TOKEN}
    depends_on:
      kratos:
        condition: service_healthy
    networks:
      - myapp
    healthcheck:
      test: ["CMD", "wget", "-q", "--spider", "http://localhost:80/_vibewarden/healthz"]
      interval: 10s
      timeout: 5s
      retries: 5
      start_period: 15s

  myapp:
    image: your-registry/your-app:latest
    container_name: myapp
    restart: unless-stopped
    networks:
      - myapp
    # Expose only on the internal network, not on the host.
    # VibeWarden is the only entry point.
    healthcheck:
      test: ["CMD", "wget", "-q", "--spider", "http://localhost:3000/health"]
      interval: 10s
      timeout: 5s
      retries: 5

volumes:
  postgres_data:
  caddy_data:

networks:
  myapp:
    driver: bridge
```

> Replace `myapp` and `your-registry/your-app:latest` with your application's
> service name and image.

---

## Step 2: Write vibewarden.yaml

```yaml
# /opt/myapp/vibewarden.yaml
server:
  host: "0.0.0.0"
  port: 443

upstream:
  host: "myapp"   # Docker Compose service name
  port: 3000

tls:
  enabled: true
  provider: letsencrypt
  domain: "myapp.example.com"
  storage_path: ""   # uses Caddy default: /root/.local/share/caddy

kratos:
  public_url: "http://kratos:4433"
  admin_url:  "http://kratos:4434"

auth:
  public_paths:
    - "/static/*"
    - "/favicon.ico"
    - "/robots.txt"
  session_cookie_name: "ory_kratos_session"
  login_url: ""

body_size:
  max: "10MB"

rate_limit:
  enabled: true
  per_ip:
    requests_per_second: 20
    burst: 40
  per_user:
    requests_per_second: 200
    burst: 400
  trust_proxy_headers: false

log:
  level: "info"
  format: "json"

admin:
  enabled: true
  token: ""   # set via VIBEWARDEN_ADMIN_TOKEN env var

metrics:
  enabled: true
  path_patterns:
    - "/api/v1/:resource"
    - "/api/v1/:resource/:id"

security_headers:
  enabled: true
  hsts_max_age: 31536000
  hsts_include_subdomains: true
  hsts_preload: false
  content_type_nosniff: true
  frame_option: "DENY"
  content_security_policy: "default-src 'self'; style-src 'self' 'unsafe-inline'"
  referrer_policy: "strict-origin-when-cross-origin"
  permissions_policy: ""
  cross_origin_opener_policy: "same-origin"
  cross_origin_resource_policy: "same-origin"
  permitted_cross_domain_policies: "none"
  suppress_via_header: true
```

---

## Step 3: Configure Ory Kratos

Create `config/kratos/identity.schema.json`:

```json
{
  "$id": "https://schemas.ory.sh/presets/kratos/identity.email.schema.json",
  "$schema": "http://json-schema.org/draft-07/schema#",
  "title": "Person",
  "type": "object",
  "properties": {
    "traits": {
      "type": "object",
      "properties": {
        "email": {
          "type": "string",
          "format": "email",
          "title": "E-Mail",
          "ory.sh/kratos": {
            "credentials": {
              "password": { "identifier": true }
            },
            "recovery": { "via": "email" },
            "verification": { "via": "email" }
          }
        }
      },
      "required": ["email"],
      "additionalProperties": false
    }
  }
}
```

Create `config/kratos/kratos.yml`:

```yaml
version: v1.3.1

dsn: memory   # overridden by DSN env var

serve:
  public:
    base_url: https://myapp.example.com/
    cors:
      enabled: false
  admin:
    base_url: http://kratos:4434/

selfservice:
  default_browser_return_url: https://myapp.example.com/
  allowed_return_urls:
    - https://myapp.example.com/

  methods:
    password:
      enabled: true

  flows:
    error:
      ui_url: https://myapp.example.com/error
    login:
      ui_url: https://myapp.example.com/login
      lifespan: 10m
    logout:
      default_browser_return_url: https://myapp.example.com/login
    registration:
      lifespan: 10m
      ui_url: https://myapp.example.com/register
    settings:
      ui_url: https://myapp.example.com/settings
    recovery:
      enabled: true
      ui_url: https://myapp.example.com/recovery
    verification:
      enabled: true
      ui_url: https://myapp.example.com/verification
      after:
        default_browser_return_url: https://myapp.example.com/

identity:
  default_schema_id: default
  schemas:
    - id: default
      url: file:///etc/config/kratos/identity.schema.json

courier:
  smtp:
    connection_uri: smtps://user:password@smtp.example.com:465/
    from_address: noreply@myapp.example.com
    from_name: "My App"
```

Replace `smtp.example.com`, credentials, and all `myapp.example.com` references with
your actual values.

---

## Step 4: Create the .env file

```bash
# /opt/myapp/.env
# Never commit this file.

DOMAIN=myapp.example.com

POSTGRES_USER=vibewarden
POSTGRES_PASSWORD=change-me-strong-password
POSTGRES_DB=vibewarden

VIBEWARDEN_ADMIN_TOKEN=change-me-random-64-char-token
```

Generate strong values:

```bash
openssl rand -hex 32   # for POSTGRES_PASSWORD
openssl rand -hex 32   # for VIBEWARDEN_ADMIN_TOKEN
```

---

## Step 5: TLS setup with Let's Encrypt

VibeWarden embeds Caddy, which handles ACME certificate issuance automatically when
`tls.provider` is set to `letsencrypt`.

**Requirements**:
- Port 80 must be reachable from the internet (ACME HTTP-01 challenge).
- Port 443 must be reachable from the internet.
- The domain must resolve to the server's public IP before you start the stack.

**How it works**:
1. On first startup, Caddy contacts Let's Encrypt and requests a certificate via
   the HTTP-01 challenge on port 80.
2. The certificate and private key are stored in the `caddy_data` Docker volume
   (path: `/root/.local/share/caddy` inside the container).
3. Caddy automatically renews the certificate before it expires (typically 30 days
   before the 90-day expiry).

**No manual action is required.** If certificate issuance fails, check:

```bash
docker compose logs vibewarden | grep -i "tls\|acme\|certificate"
```

Common causes of failure:
- DNS not yet propagated (the A record does not point to the server).
- Port 80 blocked by a firewall (see firewall section below).
- Rate limiting by Let's Encrypt (5 failures per hour per domain).

---

## Step 6: Start the stack

```bash
cd /opt/myapp
docker compose up -d
```

Check that all containers are healthy:

```bash
docker compose ps
```

Expected output (all `Status` columns showing `healthy` or `exited 0` for the
migrate container):

```
NAME                   STATUS
myapp-postgres         Up (healthy)
myapp-kratos-migrate   Exited (0)
myapp-kratos           Up (healthy)
myapp-vibewarden       Up (healthy)
myapp                  Up (healthy)
```

Verify the site is reachable:

```bash
curl -I https://myapp.example.com/_vibewarden/healthz
# HTTP/2 200
```

---

## Environment variables reference

All `vibewarden.yaml` settings can be overridden via environment variables using the
`VIBEWARDEN_` prefix and underscore-separated key path.

| Environment variable | Config key | Description |
|---|---|---|
| `VIBEWARDEN_SERVER_HOST` | `server.host` | Bind address (use `0.0.0.0` in containers) |
| `VIBEWARDEN_SERVER_PORT` | `server.port` | Listen port |
| `VIBEWARDEN_UPSTREAM_HOST` | `upstream.host` | Upstream app hostname |
| `VIBEWARDEN_UPSTREAM_PORT` | `upstream.port` | Upstream app port |
| `VIBEWARDEN_TLS_ENABLED` | `tls.enabled` | Enable TLS termination |
| `VIBEWARDEN_TLS_PROVIDER` | `tls.provider` | `letsencrypt`, `self-signed`, or `external` |
| `VIBEWARDEN_TLS_DOMAIN` | `tls.domain` | Domain for ACME certificate |
| `VIBEWARDEN_TLS_CERT_PATH` | `tls.cert_path` | Path to certificate file (external provider) |
| `VIBEWARDEN_TLS_KEY_PATH` | `tls.key_path` | Path to private key file (external provider) |
| `VIBEWARDEN_KRATOS_PUBLIC_URL` | `kratos.public_url` | Kratos public API URL |
| `VIBEWARDEN_KRATOS_ADMIN_URL` | `kratos.admin_url` | Kratos admin API URL |
| `VIBEWARDEN_ADMIN_TOKEN` | `admin.token` | Bearer token for the admin API |
| `VIBEWARDEN_LOG_LEVEL` | `log.level` | `debug`, `info`, `warn`, `error` |
| `VIBEWARDEN_LOG_FORMAT` | `log.format` | `json` or `text` |

---

## Database setup (Postgres)

VibeWarden uses Postgres exclusively for Ory Kratos (identity and session storage).
VibeWarden itself is stateless — no separate database is required for the sidecar.

The `kratos-migrate` service in the Compose file runs Kratos database migrations
automatically on every startup and exits with code 0 when done. `kratos` depends on
`kratos-migrate` completing successfully before it starts.

**Backup**: see the Backup and recovery section below.

**Production Postgres hardening** (beyond Docker Compose):
- Run Postgres on a separate host or use a managed service (e.g. Hetzner Managed
  Databases, AWS RDS, Supabase) to get automated backups and failover.
- Enforce SSL connections: add `?sslmode=require` to the DSN.
- Create a least-privilege Postgres role for Kratos (only the `kratos` database,
  no superuser).

---

## Reverse proxy considerations

### Cloudflare in front of VibeWarden

If you proxy traffic through Cloudflare (orange cloud):

1. Set `tls.provider: external` in `vibewarden.yaml` and supply Cloudflare Origin
   Certificates (`tls.cert_path` / `tls.key_path`), **or** set `tls.provider: self-signed`
   if Cloudflare is configured with **Full (strict)** SSL mode and you trust Cloudflare
   to handle public TLS.
2. Enable `rate_limit.trust_proxy_headers: true` so VibeWarden reads the real client
   IP from `CF-Connecting-IP` / `X-Forwarded-For` rather than the Cloudflare edge IP.
3. Consider enabling Cloudflare's WAF rules as a complementary layer — VibeWarden's
   own rate limiting and security headers still apply at the origin.

### nginx in front of VibeWarden

If you run nginx as an outer edge proxy (e.g. for multiple apps on one server):

```nginx
server {
    listen 443 ssl;
    server_name myapp.example.com;

    # nginx handles TLS; set tls.provider: external or tls.enabled: false in vibewarden.yaml
    ssl_certificate     /etc/ssl/certs/myapp.crt;
    ssl_certificate_key /etc/ssl/private/myapp.key;

    location / {
        proxy_pass         http://127.0.0.1:8080;
        proxy_set_header   Host              $host;
        proxy_set_header   X-Real-IP         $remote_addr;
        proxy_set_header   X-Forwarded-For   $proxy_add_x_forwarded_for;
        proxy_set_header   X-Forwarded-Proto https;
    }
}
```

In this topology set `rate_limit.trust_proxy_headers: true` in `vibewarden.yaml`.

---

## Monitoring and logging

### Structured logs

VibeWarden emits structured JSON logs to stdout. In production, ship them with your
existing log aggregation pipeline (Loki, Elasticsearch, CloudWatch, etc.).

Each log event contains:

| Field | Description |
|---|---|
| `schema_version` | Log schema version (e.g. `v1`) |
| `event_type` | Event kind (e.g. `request.completed`) |
| `ai_summary` | Human/AI-readable one-line description |
| `level` | `DEBUG`, `INFO`, `WARN`, `ERROR` |
| `time` | RFC 3339 timestamp |
| `payload` | Event-specific JSON object |

Follow logs:

```bash
docker compose logs -f vibewarden | jq .
```

### Prometheus metrics

VibeWarden exposes Prometheus metrics at `/_vibewarden/metrics`. Scrape this endpoint
from your Prometheus instance:

```yaml
# prometheus.yml (snippet)
scrape_configs:
  - job_name: vibewarden
    static_configs:
      - targets: ["myapp.example.com:443"]
    scheme: https
```

Key metrics to alert on:

| Metric | Condition | Severity |
|---|---|---|
| `vibewarden_upstream_errors_total` | Rate > 0 for 5 min | Critical |
| `vibewarden_request_duration_seconds_p99` | > 2s for 5 min | Warning |
| `vibewarden_rate_limit_hits_total` | Sudden spike | Warning |
| `vibewarden_auth_decisions_total{decision="blocked"}` | High rate | Warning |

See `docs/observability.md` for the full metrics reference and a local Grafana setup.

### Health check endpoint

```
GET /_vibewarden/healthz
```

Returns `200 OK` with `{"status":"ok"}` when VibeWarden is running. Use this for
load balancer and uptime monitor health checks.

---

## Backup and recovery

### What to back up

| Data | Location | Frequency |
|---|---|---|
| Postgres data (Kratos identities and sessions) | `postgres_data` Docker volume | Daily minimum |
| Caddy TLS certificates | `caddy_data` Docker volume | Weekly or after cert renewal |
| `vibewarden.yaml` | `/opt/myapp/vibewarden.yaml` | On every change, store in git |
| Kratos config | `/opt/myapp/config/kratos/` | On every change, store in git |
| `.env` | `/opt/myapp/.env` | On every change, store in a secrets manager |

### Postgres backup

```bash
# Manual snapshot
docker exec myapp-postgres pg_dump \
  -U "$POSTGRES_USER" "$POSTGRES_DB" \
  | gzip > /backups/vibewarden-$(date +%Y%m%d-%H%M%S).sql.gz

# Restore
gunzip -c /backups/vibewarden-20260101-120000.sql.gz \
  | docker exec -i myapp-postgres psql -U "$POSTGRES_USER" "$POSTGRES_DB"
```

Schedule daily backups with a systemd timer or cron and ship the dumps to object
storage (e.g. Hetzner Object Storage, S3-compatible).

### TLS certificate backup

```bash
docker run --rm \
  -v myapp_caddy_data:/data \
  -v /backups:/backup \
  alpine tar czf /backup/caddy-data-$(date +%Y%m%d).tar.gz -C /data .
```

Caddy automatically renews certificates before expiry, so you rarely need to restore
the `caddy_data` volume. If you lose it, Caddy will request a new certificate on
startup (subject to Let's Encrypt rate limits — 5 per hour per domain).

### Full stack restore

1. Restore `postgres_data` volume from the latest Postgres dump.
2. Restore `vibewarden.yaml`, Kratos config, and `.env` from version control /
   secrets manager.
3. Start the stack: `docker compose up -d`.
4. Verify: `docker compose ps` and `curl https://myapp.example.com/_vibewarden/healthz`.
