# Configuration Reference

All VibeWarden configuration lives in `vibewarden.yaml` at the root of your
project. Every field can also be set via an environment variable — the variable
name is the YAML key path uppercased and prefixed with `VIBEWARDEN_`, with dots
replaced by underscores. Example: `server.port` → `VIBEWARDEN_SERVER_PORT`.

Environment variable substitution in YAML values is supported using `${VAR}`
syntax. This is recommended for secrets:

```yaml
database:
  external_url: "postgres://user:${DB_PASSWORD}@db.example.com:5432/vibewarden"
```

---

## `profile`

**Type:** string
**Default:** `dev`
**Accepted values:** `dev`, `tls`, `prod`

Selects the deployment profile. Affects TLS settings, credential validation
strictness, and which generated files are produced.

```yaml
profile: dev
```

---

## `server`

Settings for the VibeWarden HTTP listener.

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `server.host` | string | `127.0.0.1` | Host/IP to bind to |
| `server.port` | int | `8443` | Port to listen on |

```yaml
server:
  host: 0.0.0.0
  port: 8443
```

---

## `upstream`

Settings for the upstream application being protected.

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `upstream.host` | string | `127.0.0.1` | Upstream host |
| `upstream.port` | int | `3000` | Upstream port |
| `upstream.health.enabled` | bool | `false` | Enable active upstream health checking |
| `upstream.health.path` | string | `/health` | HTTP path to probe |
| `upstream.health.interval` | duration | `10s` | Time between probes |
| `upstream.health.timeout` | duration | `5s` | Per-probe timeout |
| `upstream.health.unhealthy_threshold` | int | `3` | Consecutive failures to mark unhealthy |
| `upstream.health.healthy_threshold` | int | `2` | Consecutive successes to mark healthy |

```yaml
upstream:
  host: 127.0.0.1
  port: 3000
  health:
    enabled: true
    path: /health
    interval: 10s
    timeout: 5s
```

---

## `app`

Controls how your application is included in the generated Docker Compose file.
When neither `build` nor `image` is set, no app service is rendered and
VibeWarden falls back to `host.docker.internal`.

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `app.build` | string | `""` | Docker build context path (e.g., `.`) — used in dev/tls profiles |
| `app.image` | string | `""` | Docker image reference (e.g., `ghcr.io/org/myapp:latest`) — used in prod profile |

```yaml
app:
  build: .        # dev workflow: build from source
  # image: ghcr.io/org/myapp:latest   # prod workflow: use pre-built image
```

---

## `tls`

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `tls.enabled` | bool | `true` | Enable TLS |
| `tls.domain` | string | `""` | Domain for the TLS certificate. Required when `provider` is `letsencrypt` |
| `tls.provider` | string | `""` | Certificate provider: `letsencrypt`, `self-signed`, or `external` |
| `tls.cert_path` | string | `""` | Path to PEM certificate. Required when `provider` is `external` |
| `tls.key_path` | string | `""` | Path to PEM private key. Required when `provider` is `external` |
| `tls.storage_path` | string | `""` | Directory for ACME certificate storage. Applies to `letsencrypt` only |

```yaml
tls:
  enabled: true
  provider: letsencrypt
  domain: myapp.example.com
```

---

## `auth`

### `auth` (top-level)

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `auth.enabled` | bool | `false` | Enable the authentication middleware |
| `auth.mode` | string | `none` | Auth strategy: `none`, `kratos`, `jwt`, or `api-key` |
| `auth.public_paths` | list | `[]` | Glob patterns that bypass auth. `/_vibewarden/*` is always public |
| `auth.session_cookie_name` | string | `ory_kratos_session` | Kratos session cookie name |
| `auth.login_url` | string | `/self-service/login/browser` | Redirect for unauthenticated users |
| `auth.on_kratos_unavailable` | string | `503` | Behavior when Kratos is unreachable: `503` or `allow_public` |
| `auth.identity_schema` | string | `email_password` | Identity schema: `email_password`, `email_only`, `username_password`, `social`, or a file path |

### `auth.jwt`

Used when `auth.mode` is `jwt`.

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `auth.jwt.jwks_url` | string | `""` | Direct JWKS endpoint URL. Takes precedence over `issuer_url` |
| `auth.jwt.issuer_url` | string | `""` | OIDC issuer base URL for auto-discovery |
| `auth.jwt.issuer` | string | `""` | Expected `iss` claim value. Required |
| `auth.jwt.audience` | string | `""` | Expected `aud` claim value. Required |
| `auth.jwt.claims_to_headers` | map | see below | JWT claims injected as upstream request headers |
| `auth.jwt.allowed_algorithms` | list | `[RS256, ES256]` | Accepted signing algorithms. Never include `none` or `HS256` in production |
| `auth.jwt.cache_ttl` | duration | `1h` | How long the JWKS is cached locally |

Default `claims_to_headers`:
```yaml
claims_to_headers:
  sub: X-User-Id
  email: X-User-Email
  email_verified: X-User-Verified
```

### `auth.api_key`

Used when `auth.mode` is `api-key`.

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `auth.api_key.header` | string | `X-API-Key` | Request header carrying the API key |
| `auth.api_key.keys` | list | `[]` | Static key entries (see below) |
| `auth.api_key.openbao_path` | string | `""` | KV path in OpenBao where key hashes are stored |
| `auth.api_key.cache_ttl` | duration | `5m` | TTL for keys fetched from OpenBao |
| `auth.api_key.scope_rules` | list | `[]` | Ordered path+method authorization rules |

Each entry in `keys`:

| Field | Type | Description |
|-------|------|-------------|
| `name` | string | Human-readable label |
| `hash` | string | Hex-encoded SHA-256 of the plaintext key |
| `scopes` | list | Permission scopes granted to this key |

Each entry in `scope_rules`:

| Field | Type | Description |
|-------|------|-------------|
| `path` | string | Glob pattern matched against the request path |
| `methods` | list | HTTP methods this rule applies to (empty = all methods) |
| `required_scopes` | list | Scopes the key must possess |

### `auth.social_providers`

List of OAuth2/OIDC social login providers (used with `auth.mode: kratos`).

| Field | Type | Description |
|-------|------|-------------|
| `provider` | string | Provider name: `google`, `github`, `apple`, `facebook`, `microsoft`, `gitlab`, `discord`, `slack`, `spotify`, `oidc` |
| `client_id` | string | OAuth2 client ID |
| `client_secret` | string | OAuth2 client secret |
| `scopes` | list | OAuth2 scopes to request (optional; provider defaults apply) |
| `label` | string | Custom login button label (optional) |
| `team_id` | string | Apple Developer Team ID (Apple only) |
| `key_id` | string | Apple private key ID (Apple only) |
| `id` | string | Unique identifier for OIDC entries |
| `issuer_url` | string | OIDC issuer URL (OIDC provider only) |

### `auth.ui`

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `auth.ui.mode` | string | `built-in` | `built-in` or `custom` |
| `auth.ui.app_name` | string | `""` | Application name on built-in login pages |
| `auth.ui.logo_url` | string | `""` | Logo URL for built-in pages |
| `auth.ui.primary_color` | string | `#7C3AED` | Accent color for built-in pages |
| `auth.ui.background_color` | string | `#1a1a2e` | Background color for built-in pages |
| `auth.ui.login_url` | string | `""` | Custom login page URL (when `mode: custom`) |
| `auth.ui.registration_url` | string | `""` | Custom registration page URL |
| `auth.ui.settings_url` | string | `""` | Custom account settings page URL |
| `auth.ui.recovery_url` | string | `""` | Custom account recovery page URL |

---

## `kratos`

Used when `auth.mode` is `kratos`.

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `kratos.public_url` | string | `http://127.0.0.1:4433` | Kratos public API URL |
| `kratos.admin_url` | string | `http://127.0.0.1:4434` | Kratos admin API URL |
| `kratos.dsn` | string | `""` | Kratos database DSN (postgres URL) |
| `kratos.external` | bool | `false` | Connect to a user-managed Kratos instance instead of starting one |
| `kratos.smtp.host` | string | `localhost` | SMTP server host |
| `kratos.smtp.port` | int | `1025` | SMTP server port |
| `kratos.smtp.from` | string | `no-reply@vibewarden.local` | Sender address for Kratos emails |

---

## `rate_limit`

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `rate_limit.enabled` | bool | `true` | Enable rate limiting |
| `rate_limit.store` | string | `memory` | Backing store: `memory` or `redis` |
| `rate_limit.trust_proxy_headers` | bool | `false` | Read `X-Forwarded-For` for client IP. Enable only behind a trusted proxy |
| `rate_limit.exempt_paths` | list | `[]` | Glob patterns that bypass rate limiting. `/_vibewarden/*` is always exempt |

### `rate_limit.per_ip` and `rate_limit.per_user`

| Field | Type | Default (per_ip) | Default (per_user) | Description |
|-------|------|------|------|-------------|
| `requests_per_second` | float | `10` | `100` | Sustained token refill rate |
| `burst` | int | `20` | `200` | Maximum tokens that can accumulate |

### `rate_limit.redis`

Only read when `store` is `redis`.

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `url` | string | `""` | Redis URL (`redis://` or `rediss://`). Takes precedence over all other fields |
| `address` | string | `localhost:6379` | Redis address in `host:port` form |
| `password` | string | `""` | Redis AUTH password |
| `db` | int | `0` | Logical database index |
| `pool_size` | int | `0` (auto) | Connection pool size (`0` = auto based on CPU count) |
| `key_prefix` | string | `vibewarden` | Key namespace prefix |
| `fallback` | bool | `true` | Fail-open on Redis failure (`true`) or fail-closed (`false`) |
| `health_check_interval` | duration | `30s` | How often to probe Redis for recovery |

---

## `security_headers`

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `security_headers.enabled` | bool | `true` | Enable security headers middleware |
| `security_headers.hsts_max_age` | int | `31536000` | HSTS max-age in seconds (1 year) |
| `security_headers.hsts_include_subdomains` | bool | `true` | Add `includeSubDomains` to HSTS |
| `security_headers.hsts_preload` | bool | `false` | Add `preload` to HSTS |
| `security_headers.content_type_nosniff` | bool | `true` | Set `X-Content-Type-Options: nosniff` |
| `security_headers.frame_option` | string | `DENY` | `X-Frame-Options`: `DENY`, `SAMEORIGIN`, or `""` to disable |
| `security_headers.content_security_policy` | string | `""` (disabled) | `Content-Security-Policy` value — empty by default; set explicitly to opt in |
| `security_headers.referrer_policy` | string | `strict-origin-when-cross-origin` | `Referrer-Policy` value |
| `security_headers.permissions_policy` | string | `""` | `Permissions-Policy` value |
| `security_headers.cross_origin_opener_policy` | string | `same-origin` | `Cross-Origin-Opener-Policy` value |
| `security_headers.cross_origin_resource_policy` | string | `same-origin` | `Cross-Origin-Resource-Policy` value |
| `security_headers.permitted_cross_domain_policies` | string | `none` | `X-Permitted-Cross-Domain-Policies` value |
| `security_headers.suppress_via_header` | bool | `true` | Remove `Via` header from proxied responses |

---

## `cors`

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `cors.enabled` | bool | `false` | Enable CORS plugin |
| `cors.allowed_origins` | list | `[]` | Permitted origins. Use `["*"]` for development only |
| `cors.allowed_methods` | list | `[GET, POST, PUT, DELETE, OPTIONS]` | Permitted HTTP methods |
| `cors.allowed_headers` | list | `[Content-Type, Authorization]` | Permitted request headers |
| `cors.exposed_headers` | list | `[]` | Response headers exposed via `Access-Control-Expose-Headers` |
| `cors.allow_credentials` | bool | `false` | Set `Access-Control-Allow-Credentials: true`. Must not be combined with `allowed_origins: ["*"]` |
| `cors.max_age` | int | `0` | Preflight cache duration in seconds. `0` omits the header |

---

## `waf`

### `waf.content_type_validation`

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `waf.content_type_validation.enabled` | bool | `false` | Enable Content-Type validation on body-bearing requests |
| `waf.content_type_validation.allowed` | list | `[application/json, application/x-www-form-urlencoded, multipart/form-data]` | Permitted media types. Requests with other types receive `415 Unsupported Media Type` |

---

## `body_size`

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `body_size.max` | string | `""` | Global maximum body size (e.g., `1MB`, `512KB`). Empty means no limit |
| `body_size.overrides` | list | `[]` | Per-path overrides |

Each entry in `body_size.overrides`:

| Field | Type | Description |
|-------|------|-------------|
| `path` | string | URL path prefix to match |
| `max` | string | Maximum body size for this path |

```yaml
body_size:
  max: 1MB
  overrides:
    - path: /api/upload
      max: 50MB
```

---

## `ip_filter`

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `ip_filter.enabled` | bool | `false` | Enable IP filter plugin |
| `ip_filter.mode` | string | `blocklist` | `allowlist` or `blocklist` |
| `ip_filter.addresses` | list | `[]` | IP addresses or CIDR ranges |
| `ip_filter.trust_proxy_headers` | bool | `false` | Read `X-Forwarded-For` for client IP |

```yaml
ip_filter:
  enabled: true
  mode: allowlist
  addresses:
    - 10.0.0.0/8
    - 192.168.1.100
```

---

## `resilience`

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `resilience.timeout` | duration | `30s` | Upstream response timeout. `0` disables the timeout |

### `resilience.circuit_breaker`

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `resilience.circuit_breaker.enabled` | bool | `false` | Enable circuit breaker |
| `resilience.circuit_breaker.threshold` | int | `5` | Consecutive failures to trip the circuit |
| `resilience.circuit_breaker.timeout` | duration | `60s` | How long the circuit stays open before probing |

### `resilience.retry`

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `resilience.retry.enabled` | bool | `false` | Enable retry with exponential backoff |
| `resilience.retry.max_attempts` | int | `3` | Total attempts including the initial request |
| `resilience.retry.backoff` | duration | `100ms` | Wait before the first retry |
| `resilience.retry.max_backoff` | duration | `10s` | Upper bound on computed backoff |
| `resilience.retry.retry_on` | list | `[502, 503, 504]` | HTTP status codes that trigger a retry |

---

## `telemetry`

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `telemetry.enabled` | bool | `true` | Master switch for all telemetry |
| `telemetry.path_patterns` | list | `[]` | URL path normalization patterns using `:param` syntax |
| `telemetry.prometheus.enabled` | bool | `true` | Enable Prometheus pull-based exporter at `/_vibewarden/metrics` |
| `telemetry.otlp.enabled` | bool | `false` | Enable OTLP push-based exporter |
| `telemetry.otlp.endpoint` | string | `""` | OTLP HTTP endpoint URL. Required when `otlp.enabled` is `true` |
| `telemetry.otlp.headers` | map | `{}` | HTTP headers for OTLP authentication |
| `telemetry.otlp.interval` | duration | `30s` | Export interval |
| `telemetry.otlp.protocol` | string | `http` | Transport protocol. Only `http` is supported |
| `telemetry.logs.otlp` | bool | `false` | Export structured events via OTLP. Requires `otlp.endpoint` |
| `telemetry.traces.enabled` | bool | `false` | Enable distributed tracing. Requires `otlp.enabled` |

!!! note "Legacy `metrics:` section"
    The `metrics:` config block is deprecated. Settings are automatically migrated
    to `telemetry:` at startup with a deprecation warning. Migrate before the next
    major version.

---

## `observability`

Controls generation of the optional local observability stack (Grafana, Prometheus,
Loki, Promtail).

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `observability.enabled` | bool | `false` | Generate the observability stack |
| `observability.grafana_port` | int | `3001` | Host port for Grafana |
| `observability.prometheus_port` | int | `9090` | Host port for Prometheus |
| `observability.loki_port` | int | `3100` | Host port for Loki |
| `observability.retention_days` | int | `7` | Log retention period in Loki |

---

## `database`

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `database.external_url` | string | `""` | Full `postgres://` connection URL. When set, the local Postgres container is omitted from the generated Compose file |
| `database.tls_mode` | string | `require` | PostgreSQL SSL mode: `disable`, `require`, `verify-ca`, `verify-full` |
| `database.pool.max_conns` | int | `10` | Maximum open connections |
| `database.pool.min_conns` | int | `2` | Minimum idle connections |
| `database.connect_timeout` | duration | `10s` | Connection establishment timeout |

---

## `egress`

Configuration for the egress proxy plugin. See the [Egress Proxy guide](egress.md)
for a full feature walkthrough.

### `egress` (top-level)

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `egress.enabled` | bool | `false` | Enable the egress proxy plugin |
| `egress.listen` | string | `127.0.0.1:8081` | TCP address the egress proxy binds to |
| `egress.default_policy` | string | `deny` | Disposition for requests that match no route: `deny` or `allow` |
| `egress.allow_insecure` | bool | `false` | Permit plain HTTP egress requests globally. Per-route `allow_insecure` overrides this |
| `egress.default_timeout` | duration | `30s` | Request timeout applied when a route does not specify its own |
| `egress.default_body_size_limit` | string | `""` | Global maximum request body size (e.g. `10MB`). Empty means no limit |
| `egress.default_response_size_limit` | string | `""` | Global maximum response body size (e.g. `50MB`). Empty means no limit |

### `egress.dns`

DNS-level SSRF protection settings.

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `egress.dns.block_private` | bool | `true` | Block requests to RFC 1918 private, loopback, and reserved IP ranges |
| `egress.dns.allowed_private` | list | `[]` | CIDR ranges exempt from `block_private`. Example: `["10.0.0.0/8"]` |

### `egress.routes`

Ordered list of egress route definitions. Routes are evaluated in declaration
order; the first matching route wins.

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `name` | string | — | Unique human-readable identifier for this route. Required |
| `pattern` | string | — | URL glob matched against outbound request URLs (must start with `http://` or `https://`). Required |
| `methods` | list | `[]` | HTTP methods this route applies to. Empty means all methods |
| `timeout` | duration | `""` | Per-route request timeout. Empty uses `egress.default_timeout` |
| `secret` | string | `""` | OpenBao KV path of the secret to fetch and inject |
| `secret_header` | string | `""` | Request header to inject the secret value into (e.g. `Authorization`) |
| `secret_format` | string | `""` | Value template — `{value}` is replaced with the resolved secret (e.g. `Bearer {value}`) |
| `rate_limit` | string | `""` | Rate limit expression (e.g. `100/s`, `500/m`). Empty means no per-route limit |
| `body_size_limit` | string | `""` | Maximum request body size. Empty uses `egress.default_body_size_limit` |
| `response_size_limit` | string | `""` | Maximum response body size. Empty uses `egress.default_response_size_limit` |
| `allow_insecure` | bool | `false` | Permit plain HTTP for this route, overriding the global setting |

### `egress.routes[].circuit_breaker`

| Field | Type | Description |
|-------|------|-------------|
| `threshold` | int | Consecutive failures to trip the circuit. Must be > 0 |
| `reset_after` | duration | How long the circuit stays open before allowing a probe request |

### `egress.routes[].retries`

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `max` | int | — | Maximum retry attempts (not counting the initial request). Must be >= 1 |
| `methods` | list | `[GET, HEAD, PUT, DELETE]` | HTTP methods eligible for retry. Empty uses the default idempotent set |
| `backoff` | string | `exponential` | Backoff strategy: `exponential` or `fixed` |

### `egress.routes[].mtls`

| Field | Type | Description |
|-------|------|-------------|
| `cert_path` | string | Filesystem path to the PEM-encoded client certificate |
| `key_path` | string | Filesystem path to the PEM-encoded private key |
| `ca_path` | string | Filesystem path to a PEM-encoded CA bundle used to verify the server's certificate (optional; uses system roots when empty) |

### `egress.routes[].validate_response`

| Field | Type | Description |
|-------|------|-------------|
| `status_codes` | list | Allowed HTTP status code expressions (e.g. `["2xx", "301"]`). Empty means no validation |
| `content_types` | list | Allowed MIME type prefixes (e.g. `["application/json"]`). Empty means no validation |

### `egress.routes[].cache`

| Field | Type | Description |
|-------|------|-------------|
| `enabled` | bool | Activate in-memory response caching for this route |
| `ttl` | duration | Cache entry lifetime. Zero means never expire (not recommended) |
| `max_size` | int | Maximum cached response body size in bytes. Zero means no per-entry limit |

### `egress.routes[].sanitize`

| Field | Type | Description |
|-------|------|-------------|
| `headers` | list | Header names whose values are replaced with `[REDACTED]` in log events (forwarded request is unchanged) |
| `query_params` | list | Query parameter names stripped from the URL before forwarding |
| `body_fields` | list | JSON field names replaced with `[REDACTED]` in the request body before forwarding. Only applied when `Content-Type` is `application/json` |

### `egress.routes[].headers`

| Field | Type | Description |
|-------|------|-------------|
| `inject` | map | Static headers added to (or overwriting) the outbound request |
| `strip_request` | list | Request header names removed before forwarding upstream |
| `strip_response` | list | Response header names removed from the upstream response before returning to the app. `Server` and `X-Powered-By` are always stripped |

```yaml
egress:
  enabled: true
  listen: "127.0.0.1:8081"
  default_policy: deny
  default_timeout: "30s"

  dns:
    block_private: true

  routes:
    - name: stripe-api
      pattern: "https://api.stripe.com/**"
      methods: ["POST"]
      timeout: "10s"
      secret: app/stripe
      secret_header: Authorization
      secret_format: "Bearer {value}"
      rate_limit: "100/s"
      circuit_breaker:
        threshold: 5
        reset_after: "30s"
      retries:
        max: 3
        backoff: exponential
```

---

## `secrets`

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `secrets.enabled` | bool | `false` | Enable secrets management plugin |
| `secrets.provider` | string | `openbao` | Secret store backend |
| `secrets.cache_ttl` | duration | `5m` | How long fetched secrets are cached in memory |

### `secrets.openbao`

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `secrets.openbao.address` | string | `""` | OpenBao server URL (e.g., `http://openbao:8200`) |
| `secrets.openbao.mount_path` | string | `secret` | KV v2 mount path |
| `secrets.openbao.auth.method` | string | `""` | Auth method: `token` or `approle` |
| `secrets.openbao.auth.token` | string | `""` | Static token (used when `method: token`) |
| `secrets.openbao.auth.role_id` | string | `""` | AppRole role_id (used when `method: approle`) |
| `secrets.openbao.auth.secret_id` | string | `""` | AppRole secret_id (used when `method: approle`) |

### `secrets.inject`

| Field | Type | Description |
|-------|------|-------------|
| `secrets.inject.headers` | list | Secrets injected as HTTP request headers |
| `secrets.inject.env_file` | string | Path to write a `.env` file with secret values |
| `secrets.inject.env` | list | Secrets written to the env file |

Each entry in `secrets.inject.headers`:

| Field | Type | Description |
|-------|------|-------------|
| `secret_path` | string | KV path of the secret |
| `secret_key` | string | Key within the secret map |
| `header` | string | HTTP header name |

### `secrets.dynamic`

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `secrets.dynamic.postgres.enabled` | bool | `false` | Enable dynamic Postgres credential generation |
| `secrets.dynamic.postgres.roles` | list | `[]` | OpenBao database roles to request credentials for |

Each entry in `roles`:

| Field | Type | Description |
|-------|------|-------------|
| `name` | string | OpenBao database role name |
| `env_var_user` | string | Env var to write the generated username into |
| `env_var_password` | string | Env var to write the generated password into |

### `secrets.health`

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `secrets.health.check_interval` | duration | `6h` | How often to run secret health checks |
| `secrets.health.max_static_age` | duration | `2160h` | Maximum acceptable age of a static secret (90 days) |
| `secrets.health.weak_patterns` | list | `[]` | Substrings that indicate a weak or default secret |

---

## `webhooks`

| Field | Type | Description |
|-------|------|-------------|
| `webhooks.endpoints` | list | Webhook endpoints to deliver audit events to |

Each entry in `webhooks.endpoints`:

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `url` | string | — | HTTP(S) endpoint to POST events to. Required |
| `events` | list | — | Event types to deliver. Use `["*"]` to subscribe to all |
| `format` | string | `raw` | Payload format: `raw`, `slack`, or `discord` |
| `timeout_seconds` | int | `10` | Per-request HTTP timeout in seconds |

---

## `audit`

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `audit.enabled` | bool | `true` | Enable the audit log sink |
| `audit.output` | string | `stdout` | Write destination: `stdout` or a file path |

---

## `log`

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `log.level` | string | `info` | Log level: `debug`, `info`, `warn`, `error` |
| `log.format` | string | `json` | Log format: `json` or `text` |

---

## `admin`

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `admin.enabled` | bool | `false` | Enable the admin API at `/_vibewarden/admin/*` |
| `admin.token` | string | `""` | Bearer token for admin API authentication |

---

## `overrides`

Escape hatches for advanced users. All fields are optional.

| Field | Type | Description |
|-------|------|-------------|
| `overrides.kratos_config` | string | Path to a custom `kratos.yml` file. When set, VibeWarden uses it instead of generating one |
| `overrides.compose_file` | string | Path to a custom `docker-compose.yml` file |
| `overrides.identity_schema` | string | Path to a custom Kratos identity schema JSON file |

---

## Full example `vibewarden.yaml`

```yaml
profile: dev

server:
  host: 0.0.0.0
  port: 8443

tls:
  enabled: true
  provider: self-signed

upstream:
  host: 127.0.0.1
  port: 3000

auth:
  enabled: true
  mode: jwt
  jwt:
    jwks_url: "https://dev-abc123.us.auth0.com/.well-known/jwks.json"
    issuer:   "https://dev-abc123.us.auth0.com/"
    audience: "https://api.your-app.com"
    claims_to_headers:
      sub: X-User-Id
      email: X-User-Email
      email_verified: X-User-Verified
  public_paths:
    - /health
    - /static/*

rate_limit:
  enabled: true
  store: memory
  per_ip:
    requests_per_second: 10
    burst: 20
  per_user:
    requests_per_second: 100
    burst: 200

security_headers:
  enabled: true
  hsts_max_age: 31536000
  content_security_policy: ""

telemetry:
  enabled: true
  prometheus:
    enabled: true
  path_patterns:
    - "/api/v1/users/:id"
    - "/api/v1/orders/:order_id"

log:
  level: info
  format: json
```
