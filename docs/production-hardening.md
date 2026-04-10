# Production Hardening Checklist

Use this checklist before going live and after each significant infrastructure change.
Each item is a concrete action, not a vague recommendation.

---

## Security checklist

### TLS

- [ ] `tls.enabled: true` in `vibewarden.yaml`.
- [ ] `tls.provider: letsencrypt` (or `external` if TLS is terminated upstream by
  Cloudflare / nginx). Never use `self-signed` in production.
- [ ] `tls.domain` set to the exact public hostname (e.g. `myapp.example.com`).
- [ ] Caddy `caddy_data` volume is persisted across restarts so certificates survive
  container recreation.
- [ ] Port 80 is open to the internet for Let's Encrypt ACME HTTP-01 challenges.
- [ ] Port 443 is open to the internet.
- [ ] `hsts_max_age` is at least `31536000` (1 year).
- [ ] `hsts_include_subdomains: true` if all subdomains are also served over HTTPS.
- [ ] `hsts_preload: false` unless you have submitted the domain to the HSTS preload
  list and understand the implications (cannot revert without browser update cycles).

### Admin token rotation

- [ ] `admin.enabled: true` only if you actively use the admin API. Disable it if
  you do not need it.
- [ ] `admin.token` is set exclusively via the `VIBEWARDEN_ADMIN_TOKEN` environment
  variable — never hard-coded in `vibewarden.yaml` or committed to version control.
- [ ] Admin token is at least 32 bytes of random data:
  ```bash
  openssl rand -hex 32
  ```
- [ ] Admin token is stored in a secrets manager (e.g. HashiCorp Vault, AWS Secrets
  Manager, Hetzner Robot secrets) or at minimum in an `.env` file with `chmod 600`.
- [ ] Rotate the admin token every 90 days or after any team member with access leaves.
  Update the environment variable and restart the VibeWarden container.
- [ ] The admin API endpoint (`/_vibewarden/admin/*`) is not directly reachable from
  the internet. If you use a firewall allow-list, restrict this path to your internal
  management network.

### Rate limits

- [ ] `rate_limit.enabled: true`.
- [ ] `per_ip.requests_per_second` is tuned to your expected legitimate traffic.
  A good starting point: 20 req/s per IP, burst 40. Tighten if you observe abuse.
- [ ] `per_user.requests_per_second` is higher than `per_ip` (authenticated users
  are known). Starting point: 200 req/s per user, burst 400.
- [ ] `trust_proxy_headers: true` only when VibeWarden sits behind a trusted proxy
  (Cloudflare, nginx). When `false` (the default), the TCP source IP is used —
  this prevents IP spoofing via forged `X-Forwarded-For` headers.
- [ ] `exempt_paths` contains only truly public, non-abusable paths.
  Do not exempt API endpoints that mutate state.

### Security headers

- [ ] `security_headers.enabled: true`.
- [ ] `content_security_policy` is set to the strictest policy your app supports.
  The default is empty (no CSP header) so that VibeWarden works with any app out
  of the box. For production, start with `"default-src 'self'"` and tighten from
  there after testing with your actual app:
  ```yaml
  content_security_policy: >-
    default-src 'self';
    script-src 'self' 'nonce-{nonce}';
    style-src 'self' 'unsafe-inline';
    img-src 'self' data: https:;
    connect-src 'self' https://api.myapp.example.com
  ```
- [ ] `frame_option: "DENY"` unless your app intentionally embeds other origins in
  frames. Use `"SAMEORIGIN"` if you embed pages from the same origin.
- [ ] `referrer_policy: "strict-origin-when-cross-origin"` or stricter
  (`"no-referrer"`) if your app does not depend on referrer information.
- [ ] `permissions_policy` is set to restrict browser features your app does not use.
  Example:
  ```yaml
  permissions_policy: "camera=(), microphone=(), geolocation=(), payment=()"
  ```
- [ ] `cross_origin_opener_policy: "same-origin"`.
- [ ] `cross_origin_resource_policy: "same-origin"`.
- [ ] `suppress_via_header: true` (reduces information disclosure about the proxy).

---

## Docker hardening

### Non-root user

The official VibeWarden image runs as a non-root user by default. Verify:

```bash
docker inspect ghcr.io/vibewarden/vibewarden:latest \
  --format '{{.Config.User}}'
# expected: vibewarden (or a non-zero UID)
```

If building a custom image, add to your `Dockerfile`:

```dockerfile
RUN addgroup -S vibewarden && adduser -S vibewarden -G vibewarden
USER vibewarden
```

### Read-only filesystem

Add `read_only: true` to the VibeWarden service in `docker-compose.yml` and mount
writable directories explicitly:

```yaml
vibewarden:
  image: ghcr.io/vibewarden/vibewarden:latest
  read_only: true
  tmpfs:
    - /tmp
  volumes:
    - ./vibewarden.yaml:/vibewarden.yaml:ro
    - caddy_data:/root/.local/share/caddy   # writable: Caddy stores certs here
```

### Resource limits

Prevent runaway containers from exhausting the host:

```yaml
vibewarden:
  deploy:
    resources:
      limits:
        cpus: "1.0"
        memory: "256M"
      reservations:
        cpus: "0.25"
        memory: "64M"

kratos:
  deploy:
    resources:
      limits:
        cpus: "1.0"
        memory: "256M"

postgres:
  deploy:
    resources:
      limits:
        cpus: "2.0"
        memory: "512M"
```

> `deploy.resources` in Docker Compose requires the `docker compose` CLI (v2) and
> has no effect with the standalone `docker-compose` v1 binary.

### No privileged containers

- [ ] No container uses `privileged: true`.
- [ ] No container uses `--cap-add` beyond what is strictly required. VibeWarden
  requires no extra capabilities.
- [ ] Drop all capabilities explicitly:
  ```yaml
  vibewarden:
    cap_drop:
      - ALL
    cap_add:
      - NET_BIND_SERVICE   # required to bind ports 80 and 443
  ```

### Image provenance

- [ ] Pin the VibeWarden image to a specific digest or tag (not `latest`) in production:
  ```yaml
  image: ghcr.io/vibewarden/vibewarden:v0.3.0
  ```
- [ ] Verify the image signature before deploying (when Cosign signing is available).
- [ ] Scan the image for vulnerabilities before deploying:
  ```bash
  docker scout cves ghcr.io/vibewarden/vibewarden:v0.3.0
  # or:
  trivy image ghcr.io/vibewarden/vibewarden:v0.3.0
  ```

---

## Network hardening

### Firewall rules

Allow only the ports your server must expose publicly. Example using `ufw`:

```bash
# Default: deny all incoming
ufw default deny incoming
ufw default allow outgoing

# Allow SSH (change port if you moved it)
ufw allow 22/tcp

# Allow HTTP and HTTPS (required for ACME challenge and app traffic)
ufw allow 80/tcp
ufw allow 443/tcp

# Enable the firewall
ufw enable

# Verify
ufw status verbose
```

### Expose only 80 and 443

- [ ] Postgres is not exposed on any host port. Remove or omit the `ports:` directive
  for the `postgres` service in `docker-compose.yml`. Kratos connects to Postgres
  over the internal Docker network.
- [ ] Kratos admin API (port 4434) is not exposed on any host port. VibeWarden
  connects to it over the internal Docker network via `VIBEWARDEN_KRATOS_ADMIN_URL`.
- [ ] Kratos public API (port 4433) is not exposed on any host port when VibeWarden
  proxies all Kratos traffic. VibeWarden forwards `/self-service/*` to Kratos
  internally.
- [ ] Your application container exposes no host ports. VibeWarden connects to it
  over the internal Docker network.
- [ ] The Prometheus metrics endpoint (`/_vibewarden/metrics`) is accessible over
  HTTPS. Restrict access to your Prometheus scraper's IP using Cloudflare Access,
  nginx `allow`/`deny`, or a VPN if you do not want metrics publicly readable.

### Internal-only services

Add explicit `expose` declarations (not `ports`) for services that should only be
reachable on the internal Docker network:

```yaml
postgres:
  expose:
    - "5432"  # internal only

kratos:
  expose:
    - "4433"
    - "4434"  # internal only

myapp:
  expose:
    - "3000"  # internal only
```

---

## Kratos hardening

### SMTP

- [ ] Configure a real SMTP relay for Kratos email delivery. Do not use `mailslurper`
  (the dev mail sink) in production.
- [ ] Use TLS-secured SMTP (`smtps://` or STARTTLS). Connection URI format:
  ```
  smtps://user:password@smtp.example.com:465/
  ```
- [ ] Store SMTP credentials in environment variables, not in `kratos.yml` directly.
  Kratos supports environment variable substitution in its config:
  ```yaml
  courier:
    smtp:
      connection_uri: ${SMTP_URI}
  ```
- [ ] Set a recognisable `from_address` and `from_name` so users trust verification
  and recovery emails.

### Session configuration

- [ ] `selfservice.flows.login.lifespan` is set to a short but usable value (e.g. `10m`
  for the login flow itself, not the session).
- [ ] Configure session lifespan in `kratos.yml`:
  ```yaml
  session:
    lifespan: 720h   # 30 days — adjust to your security requirements
    cookie:
      same_site: Lax
  ```
- [ ] `selfservice.allowed_return_urls` contains only your own domain. Never allow
  arbitrary return URLs (open redirect).
- [ ] `selfservice.default_browser_return_url` points to your app's authenticated
  landing page.
- [ ] Account verification (`flows.verification.enabled: true`) is enabled so users
  must confirm their email address.
- [ ] Account recovery (`flows.recovery.enabled: true`) is enabled so users can
  regain access via email.

### Kratos admin API exposure

- [ ] The Kratos admin API (`kratos:4434`) is reachable only from within the Docker
  network. It is not exposed to the host or the internet.
- [ ] VibeWarden communicates with the Kratos admin API via `VIBEWARDEN_KRATOS_ADMIN_URL`
  using the internal Docker network hostname (e.g. `http://kratos:4434`).

---

## Monitoring checklist

### Alerting

Configure alerts in Prometheus (or your alerting platform) for:

- [ ] **Upstream errors**: `rate(vibewarden_upstream_errors_total[5m]) > 0`
  — your app is unreachable or crashing.
- [ ] **High latency**: `histogram_quantile(0.99, rate(vibewarden_request_duration_seconds_bucket[5m])) > 2`
  — p99 latency exceeds 2 seconds.
- [ ] **Rate limit spike**: `rate(vibewarden_rate_limit_hits_total[5m]) > 10`
  — possible brute-force or abuse.
- [ ] **High auth block rate**: `rate(vibewarden_auth_decisions_total{decision="blocked"}[5m]) > 5`
  — many unauthenticated requests to protected routes.
- [ ] **Container restarts**: Docker / host-level alert when any container in the
  stack has restart count > 2 in 10 minutes.
- [ ] **Disk usage**: alert when the volume hosting `postgres_data` exceeds 80%
  capacity.
- [ ] **Certificate expiry**: Caddy logs a warning 30 days before expiry. Configure
  a log-based alert on `"certificate will expire"` in your log aggregation platform.

### Log retention

- [ ] Set a log retention policy in your aggregation platform.
  Recommended minimum: 30 days. Compliance requirements may mandate 90 days or more.
- [ ] If using Loki locally, configure retention in `loki-config.yml`:
  ```yaml
  limits_config:
    retention_period: 30d
  compactor:
    retention_enabled: true
  ```
- [ ] VibeWarden logs are structured JSON — ensure your log aggregation pipeline
  parses them as JSON rather than treating them as plain text strings.
- [ ] Set `log.level: "info"` in production. Use `"debug"` only temporarily when
  diagnosing a specific issue, as debug logs are verbose and may contain sensitive data.

---

## Regular maintenance

### Updates

- [ ] Subscribe to VibeWarden releases: watch the GitHub repository
  (`vibewarden/vibewarden`) for new tags.
- [ ] Update the VibeWarden image tag in `docker-compose.yml` after each release.
  Pull and restart:
  ```bash
  docker compose pull vibewarden
  docker compose up -d vibewarden
  ```
- [ ] Update the Kratos image tag in `docker-compose.yml` after each Kratos release.
  Run `kratos-migrate` before starting the new Kratos container:
  ```bash
  docker compose run --rm kratos-migrate
  docker compose up -d kratos
  ```
- [ ] Update the Postgres image tag once a year (or when a new major version ships
  with a supported upgrade path).
- [ ] Apply OS security patches monthly:
  ```bash
  apt-get update && apt-get dist-upgrade -y
  ```
- [ ] Reboot the server after kernel updates to apply them.

### Certificate renewal

Caddy handles certificate renewal automatically. Verify renewal is working:

```bash
docker compose logs vibewarden | grep -i "renewed\|renew\|certificate"
```

If renewal fails (e.g. port 80 was temporarily blocked), Caddy retries automatically.
Let's Encrypt certificates are valid for 90 days; Caddy renews 30 days before expiry,
giving a 30-day window to resolve any issues before the site goes down.

### Secret rotation schedule

| Secret | Rotation frequency | Action |
|---|---|---|
| `VIBEWARDEN_ADMIN_TOKEN` | 90 days | Update env var, restart VibeWarden container |
| `POSTGRES_PASSWORD` | 180 days | Update DSN in all services, rolling restart |
| SMTP credentials | On provider request or annually | Update `SMTP_URI`, restart Kratos |
| TLS certificates | Automatic (Caddy) | Monitor Caddy logs for renewal errors |

### Database maintenance

- [ ] Run `VACUUM ANALYZE` on Postgres monthly to keep query plans fresh:
  ```bash
  docker exec myapp-postgres psql -U "$POSTGRES_USER" "$POSTGRES_DB" \
    -c "VACUUM ANALYZE;"
  ```
- [ ] Review Postgres slow query log quarterly. Enable it in `postgresql.conf`:
  ```
  log_min_duration_statement = 1000   # log queries slower than 1s
  ```
- [ ] Verify that daily backup jobs are completing successfully and that backup files
  are landing in the expected destination. Test restores quarterly.
