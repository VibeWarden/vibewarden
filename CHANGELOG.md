# Changelog

All notable changes to VibeWarden are documented in this file.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).
Versioning follows [Semantic Versioning](https://semver.org/).

Future releases are generated automatically via [goreleaser](https://goreleaser.com/).
This initial entry was written by hand to summarise the work leading up to v0.1.0.

---

## [v0.1.0] — 2026-03-28

First public release of the VibeWarden OSS core.
Single Go binary embedding Caddy. Zero-to-secure in minutes for vibe-coded apps.

### Core sidecar

- Embedded Caddy reverse proxy — programmatic config, no Caddyfile required
- Automatic TLS via Let's Encrypt, self-signed (dev), or external provider passthrough
- Per-path request body size limits
- W3C `traceparent` header propagation to upstream
- `trace_id` injected into JSON error responses
- OpenAPI 3.0 spec served at `/_vibewarden/openapi.json`
- Graceful degradation — sidecar stays up when optional backends (Kratos, OpenBao) are unavailable
- Project scaffold (`vibewarden init`) with profile-based Docker Compose generation

### Authentication

- [Ory Kratos](https://www.ory.sh/kratos/) session validation middleware
- Kratos flow proxy routes (`/self-service/*`) forwarded transparently
- Built-in auth UI pages: login, registration, account recovery, e-mail verification
- Social login (OIDC) with auto-selection of identity schema preset
- JWT/OIDC identity adapter with JWKS caching and configurable clock skew
- `auth.mode` config switch: `kratos` | `jwt` | `none`
- Identity provider port abstraction — swap backends without touching middleware
- Scoped API keys with path-based authorization
- API key validation middleware with OpenBao-backed storage and TTL cache
- `X-User-*` headers stripped at the Caddy layer to prevent client spoofing
- Configurable public-path bypass list

### Rate limiting

- In-memory token-bucket rate limiter (IP-based and user-based)
- Redis-backed rate limiter with graceful fallback to in-memory on Redis failure
- Per-path rate limiting configuration
- StateSync port abstraction with both in-memory and Redis adapters
- External Redis configuration with shared counters across replicas
- Per-route rate limiting on egress proxy routes

### Security

- Security headers plugin: `Strict-Transport-Security`, `X-Frame-Options`,
  `X-Content-Type-Options`, `Content-Security-Policy`, `Referrer-Policy`,
  `Permissions-Policy`
- CORS plugin with per-origin, per-method, and per-header configuration
- IP filter plugin: allowlist and blocklist with CIDR support
- Content-Type validation middleware (rejects mismatched or missing `Content-Type`)
- WAF rule engine with pattern detection for SQLi, XSS, path traversal, and more
- WAF middleware: `block` mode (reject request) and `detect` mode (log only)
- Audit event domain model with structured `AuditEvent` type
- Audit log sink adapters: JSON file, OTel logs, and multi-writer fan-out
- Audit events emitted from all security-relevant middleware
- Webhook delivery for audit events with retry and HMAC signing

### Secrets management

- OpenBao (HashiCorp Vault fork, Apache 2.0) integration
- Secret management plugin: read/write KV secrets at runtime
- `vibewarden secret get` and `vibewarden secret list` CLI commands
- `.env.template` generation with `vibewarden generate` for credential bootstrapping

### Observability

- Structured log events via `log/slog` with `schema_version`, `event_type`,
  `ai_summary`, and `payload` fields
- JSON Schema v1 for log events, published at `vibewarden.dev/schema/v1/event.json`
- Prometheus metrics adapter, metrics exposed at `/_vibewarden/metrics`
- OpenTelemetry SDK integration: metrics, logs, and traces under a single provider
- OTLP exporter with configurable endpoint and TLS
- OTel Collector in Docker Compose observability stack
- Jaeger / Grafana Tempo trace backend options
- HTTP tracing middleware with automatic span creation per request
- `trace_id` and `span_id` injected into slog context
- `slog` structured events bridged to OTel logs
- Grafana dashboards for request rates, error rates, latency, and upstream health
- Aggregate health endpoint at `/_vibewarden/health` — reports component and upstream status
- Active upstream health checker with configurable interval and thresholds
- Telemetry configuration guide and annotated example YAML

### Resilience

- Request timeout middleware (configurable per-path; returns `504` on breach)
- Circuit breaker middleware with half-open probe and configurable thresholds
- Retry middleware with exponential backoff and jitter
- Aggregate health endpoint combining all resilience signals

### Egress proxy

- Core egress proxy listener and request forwarding
- Domain types, ports, and config schema for egress routes
- Per-route header injection and stripping
- Per-route circuit breaker
- Per-route rate limiting
- Per-route timeout (`504`) and retry with exponential backoff
- Per-route mTLS client certificates
- Per-route secret injection via OpenBao
- SSRF protection and DNS resolution control (RFC 1918 blocking)
- TLS enforcement on egress routes with per-route override
- Request sanitisation and PII redaction before forwarding
- Request and response body size limits
- In-memory LRU response caching per route
- Egress response validation (status code allow-list, header assertions)
- Egress observability: tracing, Prometheus metrics, and structured logs
- Egress proxy wired into the plugin system (enable/disable via config)

### Developer experience

- `vibewarden init` — interactive project scaffold with opinionated defaults
- `vibewarden generate` — produces `docker-compose.yml` from `vibewarden.yaml`;
  includes app service, plugin-dependent services, observability stack, and
  credential generation via `.env.template`
- `vibewarden doctor` — pre-flight checks for config, TLS, and backend connectivity
- `vibewarden secret get / list` — read secrets from OpenBao at runtime
- Profile-based Docker Compose: `--profile observability`, `--profile demo`
- Demo app with Vulnerability Lab (SQLi, XSS, path traversal, and more)
- Production deployment guide, hardening checklist, and framework integration examples
- Rate limiting at scale guide with annotated Redis config reference
- Postgres deployment strategies guide with connection resilience config reference
- Identity providers and JWT/OIDC setup guide
- Social login setup guide

### CI / CD

- GitHub Actions CI pipeline: build, vet, test on every push and pull request
- goreleaser configuration with cross-compiled binaries and Docker image publishing
- Multi-arch Docker images published to `ghcr.io/vibewarden/vibewarden` for
  `linux/amd64` and `linux/arm64` via OCI manifest lists; works transparently on
  Apple Silicon, AWS Graviton, and other ARM64 hosts

---

[v0.1.0]: https://github.com/vibewarden/vibewarden/releases/tag/v0.1.0
