# Changelog

All notable changes to VibeWarden are documented in this file.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).
Versioning follows [Semantic Versioning](https://semver.org/).

Future releases are generated automatically via [goreleaser](https://goreleaser.com/).
This initial entry was written by hand to summarise the work leading up to v0.1.0.

---

## [Unreleased]

### Breaking

- **`vibew validate` / `vibew deploy` reject unknown keys** (#1053, ADR-082).
  Typos or removed keys in `vibewarden.yaml` or `vibewarden.production.yaml`
  (e.g. `tls.dmain: example.com`) now fail loudly with an error naming the
  file and the offending key. Previously such keys were silently dropped,
  which masked typos and caused silent misconfiguration in production. The
  runtime loader (`vibewarden serve`) is unchanged — it still accepts unknown
  keys for forward-compat per ADR-065. If you used the silent-drop behaviour
  to keep scratch annotations inside the YAML, move them to YAML comments
  (`# staging cutover 2026-04-18`).
- **Buypass removed from the default `letsencrypt` fallback chain**
  (#1055, ADR-083). `provider: letsencrypt` no longer falls back to Buypass.
  Buypass's ACME directory currently returns `403 Forbidden`, so keeping it
  in the chain only wasted recovery time. Buypass remains available as an
  explicit opt-in via `provider: buypass`; a `tls.acme.provider_deprecated`
  event is emitted at Init when that path is selected. Anyone who relied on
  the silent Buypass fallback should set `provider: buypass` explicitly or
  keep the default `letsencrypt` (which now falls through to ZeroSSL only
  when `tls.email` is set).

### Changed

- **ZeroSSL is skipped from the default chain when `tls.email` is empty**
  (#1055, ADR-083). Previously `provider: letsencrypt` wired ZeroSSL into
  the chain unconditionally; ZeroSSL then rejected the order because EAB
  requires an email, surfacing as a transient issuance error. The default
  chain now degrades to single-issuer Let's Encrypt when email is absent,
  and emits a `tls.acme.chain_skipped` event naming `zerossl` +
  `reason=email_not_configured` so operators see why. Set `tls.email` to
  opt back into the two-issuer chain.

### Fixes

- **Production override preserves every schema field** (#1053). `tls.email`,
  `tls.acme_ca`, `tls.cert_monitoring.*`, `server.host`, and any other field
  set only in `vibewarden.production.yaml` now reach the runtime
  `*config.Config`. Previously a hand-written allow-list silently dropped
  fields beyond `server.port`, `tls.enabled/provider/domain`, `log.level`,
  and `waf.mode`, which broke the ADR-078 promise that `tls.email` wires to
  the Caddy ACME issuer. The struct overlay now routes through the same YAML
  deep-merge that feeds the on-disk bundle.

### Added

- **Four new v1 structured log events for ACME chain observability**
  (#1055, ADR-083):
  - `tls.acme.chain_skipped` — emitted at plugin Init for every issuer
    evaluated and excluded from the default chain (payload:
    `provider`, `reason`, `primary_provider`).
  - `tls.acme.chain_configured` — emitted once at plugin Init with the
    resolved chain (payload: `primary_provider`, `resolved_chain`, `domain`).
  - `tls.acme.provider_deprecated` — emitted when `provider: buypass` is
    resolved (payload: `provider`, `reason`, `guidance`).
  - `tls.acme.chain_fallback` — reserved in the v1 schema for future use;
    emitted only once Caddy/certmagic exposes a stable issuer-transition
    hook (payload: `from_provider`, `to_provider`, `reason`, `domain`).

---

## [v0.15.0] — 2026-04-20

### Features

- **ACME fallback chain** (#1026). `tls.provider: letsencrypt` now configures three
  ACME issuers (Let's Encrypt → ZeroSSL → Buypass). If one CA is rate-limited, Caddy
  tries the next automatically. New providers: `zerossl`, `buypass`, `letsencrypt-staging`.
- **`/_vibewarden/me` endpoint** (#1021). When `auth.mode: kratos`, the sidecar serves
  session info as JSON at `/_vibewarden/me`. Frontend JS can fetch user ID, email,
  verified status, and role without calling Kratos directly.
- **`vibew tls status`** (#1034). New CLI command inspects the remote TLS certificate
  via SSH — shows domain, issuer, validity dates, and days remaining.
- **`vibew doctor` improvements** (#1033). Suggests `vibew doctor` on deploy failure.
  New checks: architecture compatibility, ACME email for ZeroSSL, image tag consistency.
- **Architecture mismatch detection** (#1032). Deploy detects when the local build arch
  doesn't match the remote server and errors with a fix-it message.

### Fixes

- **`tls.email` wired to Caddy** (#1027). The ACME account email was accepted in config
  but silently dropped in single-site mode. Now properly passed to the Caddy issuer.
- **`vibew dev` stale container detection** (#1028). Detects and rebuilds stale or
  wrong-project containers instead of silently reusing them.
- **Deploy health check diagnostics** (#1029). Classifies failures into container
  unhealthy, TLS error, upstream unreachable, timeout, or unknown — with relevant
  Caddy log excerpts.
- **Deploy status/logs correct directory** (#1030). `vibew deploy status` and
  `vibew deploy logs` now derive the remote directory consistently with `vibew deploy`.
- **Deploy drift false positives** (#1031). Credential preservation runs before drift
  detection, and rsync uses `--checksum` instead of mtime. Deploy-owned files are
  categorized separately from user modifications.

### Documentation

- AGENTS-VIBEWARDEN.md: image tag convention, TLS config keys table, VPS deploy
  section with manual fallback, cross-architecture build guidance.
- Updated deploy-reference.md, troubleshooting.md, configuration.md, llms-full.txt.
- Added reply style guidelines to CLAUDE.md for briefer agent responses.

---

## [v0.14.0] — 2026-04-20

### Features

- **Role-based access control (RBAC)** via Kratos identity traits (#985, #1019).
  `X-User-Role` header set on all authenticated requests. Optional `auth.role_paths`
  config for path-based enforcement with HTTP 403 JSON responses.
- **Optional auth on public paths** (#984, #1017). Public paths now check session
  cookies and inject identity headers (`X-User-Id`, `X-User-Email`, `X-User-Verified`,
  `X-User-Role`) when a valid session exists — without blocking or redirecting.
- **Composite secret placeholders** (#994, #1020). Embed secrets in strings with
  `${secret://path/key}` syntax. Supports multiple placeholders per value,
  `value_template` on inject entries, and `$${...}` escaping for literal output.
- **`secret://` URI resolution in config** (#1008, #1014). Any string field in
  `vibewarden.yaml` supports `secret://path/key` URIs, resolved from the encrypted
  store at config load time.

### Improvements

- Consolidated CI: Build, Test, and Coverage merged into single "Build & Test" job (#1015).
- Pipeline status tracked via GitHub labels instead of comments (#1013).
- Dual review pipeline enforced: Reviewer + Writer agents review every PR (#1010, #1011, #1012).
- Reviewer and writer agents post inline PR comments and resolve threads on re-approval.
- Documentation diagrams migrated from ASCII to Mermaid (#1004, #1016).

### Fixes

- CSP inline styles and health check HTTP fallback during ACME cert acquisition (#1007).
- Preserve `.env` and `.credentials` on redeploy (#991, #992, #1005).
- Kratos URLs use `tls.domain` instead of localhost in production (#990).
- Kratos `ui_url` uses HTTPS when TLS is enabled (#982).
- File/directory permissions fixed to 644/755 for container readability (#988, #989).
- Deploy config merge order and OpenBao bootstrap check (#986, #987).
- `vibew add tls --domain` creates production YAML when missing (#986).

### Dependencies

- Bump `golang.org/x/crypto` to 0.50.0
- Bump `pgx/v5` to 5.9.2
- Bump `go-viper/mapstructure/v2` to 2.5.0

---

## [v0.13.1] — 2026-04-19

### Features

- **`vibew init --name`** (#959): set a project name for image tags, compose
  project names, and deploy directories. Eliminates cross-project Docker image
  cache collisions.
- **`vibew deploy --dry-run`** (#958): generate the deploy bundle and inspect it
  without SSH/rsync. Shows exactly what would land on the server.
- **`vibew status` shows WAF** (#960): status output now lists WAF (with mode),
  CORS, egress, and compression plugins when enabled.

### Bug Fixes

- **Deploy bundle merge order** (#953): the generator was overwriting the merged
  `vibewarden.yaml` with the unmerged base config. Fixed by writing the merged
  config after generation. Also: compose template now uses production port/TLS
  via `overlayProdConfig`, and health check probes the correct port.
- **Build context rsync excludes** (#953): `TransferExcluding` prevents the
  app source rsync from overwriting bundle files (vibewarden.yaml, compose,
  credentials).
- **Deploy compose `context: .`** (#952): deploy mode uses `context: .` instead
  of the dev-mode `../../.` relative path. Verified by artifact regression test.
- **Drift detection false positive** (#962): first deploy to empty remote no
  longer reports "files modified" for new-file entries.
- **`vibew add tls --domain`** (#954): no longer modifies base `vibewarden.yaml`
  — writes domain to production override only.
- **Sidecar DNS** (#956): compose template includes `dns: [1.1.1.1, 8.8.8.8]`
  for hosts with systemd-resolved.
- **`vibew init` hidden dirs** (#957): `.claude/` and `.git/` directories no
  longer trigger "not empty" error.
- **Project-scoped Docker image tags** (#955): compose project name derives from
  `--name` flag or directory name, preventing `vibewarden-app:latest` collision.
- **Artifact regression tests** (#963): 10 tests that verify generated file
  content — deploy compose context, production merge, upstream resolution, image
  naming, build context exclusion, drift detection, YAML field preservation.

### Documentation

- AGENTS-VIBEWARDEN.md includes Dockerfile guidance: Alpine requirement, port
  matching, Node.js and Go examples. (#961)

---

## [v0.13.0] — 2026-04-19

### Breaking Changes

- **Deploy redesign (ADR-075)**: `vibew deploy` now generates a complete deploy
  bundle locally at `.vibewarden/deploy/<env>/` — no `sed` or runtime patching on
  the remote. All config is resolved before transfer.
- **Environment separation**: `vibew init` now generates two files:
  `vibewarden.yaml` (local dev, self-signed TLS, port 8443) and
  `vibewarden.production.yaml` (production overrides: letsencrypt, port 443).
  `vibew deploy` merges the production override on top of the base config.
  **Never put production-only config in `vibewarden.yaml`.**
- **`vibew add tls --domain`** now writes to `vibewarden.production.yaml` instead
  of the base config.
- **`vibew restart`** now runs `docker compose up -d --force-recreate --build`
  instead of `docker compose restart` — Dockerfile changes are picked up
  automatically.

### Features

- **Local Docker image transfer** (#937): `vibew deploy` with a bare image name
  (no registry prefix) automatically transfers the locally-built image via
  `docker save | rsync | docker load`. No registry needed. (#950)
- **Deploy bundles** (#938): `.vibewarden/deploy/production/` contains every file
  needed to run on the remote — inspectable, portable, no magic. (#948)
- **Self-documenting production overrides**: `vibewarden.production.yaml` shows
  all config options as comments with default values. Uncomment what you need.
- **`vibew deploy --env`** flag for environment-scoped bundles (default:
  production).

### Bug Fixes

- **`vibew init` non-TTY** (#939): no longer dies with EOF when run without a
  terminal — defaults to empty description. (#947)
- **Healthcheck shell detection** (#940): `vibew build` warns when the app image
  has no `/bin/sh` (distroless/scratch). (#946)
- **`vibew status` diagnosis** (#942): shows container state, ACME errors, and
  letsencrypt local-dev hint instead of bare "Proxy unreachable." (#946)
- **`vibew dev` letsencrypt warning** (#943): warns when `tls.provider:
  letsencrypt` is used locally (ACME challenges can't reach localhost). (#946)
- **`vibew dev` sidecar verification** (#945): checks sidecar is running after
  compose up, shows logs on failure instead of printing false success. (#947)
- **Stale AGENTS-VIBEWARDEN.md** (#944): removed incorrect "vibew add waf does
  not exist" note, updated doctor limitation. (#947)

### Documentation

- ADR-075: deploy redesign with environment separation model.
- Updated getting-started, deploy-to-vps, llms-full.txt, AGENTS-VIBEWARDEN.md,
  and reference yaml for two-file model.

---

## [v0.12.1] — 2026-04-19

### Bug Fixes

- **CRITICAL: WAF and rate-limiting now enforce in multi-site mode** — plugin handlers
  were completely missing from the multi-site Caddy config. Per-site plugin registries
  are now created and handlers injected into each site's route chain.
  (#934, closes #925)
- **Deploy exits non-zero on health check timeout** — previously reported "Site deployed"
  even when the sidecar wasn't running. Now returns `ErrHealthCheck` and suggests
  `vibew doctor --target <host>` for diagnostics. (#933, closes #927)
- **`vibew restart` shows stderr and diagnostic hint** on failure instead of bare exit
  code. (#935, closes #928)
- **`vibew add tls --domain` updates existing TLS config** instead of saying "already
  enabled — nothing to do." (#935, closes #929)
- **Project-scoped Docker image names** — compose `name:` directive prevents stale image
  reuse across different VibeWarden projects. (#935, closes #930)

### Documentation

- Multi-site deployment section added to AGENTS-VIBEWARDEN.md template — documents
  `sites/` layout, upstream.host container naming, centralized TLS.
  (#935, closes #931)

---

## [v0.12.0] — 2026-04-18

### Features

- **Built-in AES-256-GCM encrypted secret store** — eliminates OpenBao dependency
  for common secret management. Zero external deps, stdlib crypto only.
  (#904, closes #899)
- **`vibew add waf`** subcommand for CLI parity with other plugins. Accepts
  `--mode detect|block`. (#922, closes #912)
- **`vibew doctor` enhanced** with local runtime checks (upstream reachability,
  TLS cert validity) and production checks (SSH connectivity, remote container
  health, domain DNS, TLS cert expiry). Auto-detects applicable checks.
  (#923, closes #913)

### Bug Fixes

- **WAF handlers in `vibew eject`** — previously missing from the Caddy route
  chain in eject output, leading to false "WAF not enforced" diagnosis.
  (#915, closes #906)
- **Deploy health check protocol** — now uses HTTPS with `-k` when TLS is
  enabled, was polling plain HTTP on port 443. (#919, closes #907)
- **Deploy drift detection** — `vibew deploy` detects hand-edited files on the
  server and requires `--force` to overwrite. (#920, closes #908)
- **`proxy.started` log accuracy** — emits per-site events with correct TLS,
  upstream, and security header values in multi-site mode. (#918, closes #909)
- **Multi-site deploy production fixes** — `app.build` rsync, network
  `external: true`, `upstream.host` rewrite from loopback to container name.
  (#921, closes #911)
- AI prompt templates: removed 'without confirmation' phrasing that triggered
  safety guardrails. (#903)

### Documentation

- Fixed non-existent CLI flags in `llms-full.txt`; added `init`, `wrap`, and
  `add` flag documentation. (#917, closes #910)
- Known limitations section added to `AGENTS-VIBEWARDEN.md` template and
  example. (#916, closes #914)
- Missing `package-lock.json` added to Next.js example. (#902, closes #897)

---

## [v0.11.0] — 2026-04-15

### Breaking changes

- **`auth.enabled` removed from `vibewarden.yaml`** (ADR-065). `auth.mode` is
  now the single source of truth for whether authentication is enabled. Set
  `auth.mode: "none"` to disable auth, or `auth.mode: "kratos" | "jwt" | "api-key"`
  to enable a strategy. Any presence of `auth.enabled` — even `false` — is
  rejected at config load with an actionable error pointing at ADR-065.
  Migration: delete the `auth.enabled` line; keep or set `auth.mode`.
  (#845, closes #816)
- **`vibew init` no longer accepts a positional name argument** (ADR-073). It
  always scaffolds in the current directory. Use `mkdir myapp && cd myapp &&
  vibew init` instead of `vibew init myapp`. The `--name` flag is also removed.
  (#895, closes #842)

### Features

- **Multi-app deployment** (epic #869, ADRs 068-072) — deploy multiple apps to
  the same VM with subdomain routing. Each app gets its own `vibewarden.yaml`,
  independent TLS certs, and per-site middleware. `vibew deploy` detects an
  existing VibeWarden instance and adds sites automatically without downtime.
  - Site domain model and multi-config loader (#875, closes #870)
  - Caddy multi-host route generation (#876, closes #871)
  - Deploy detection and multi-app orchestration (#877, closes #872)
  - Multi-site directory watcher and hot-reload service (#878, closes #873)
  - Multi-app CLI and serve entry point (#879, closes #874)
- **Language-aware Docker health check probes** — Python uses `python -c`, Node
  uses `node -e`, Go/Alpine uses `wget`. Eliminates missing-wget failures on slim
  images. (#886, closes #884)
- **MCP tool list generated at runtime** — `vibew mcp --help` always reflects
  all registered tools; the list can no longer drift from the live registry.
  (#835, closes #813)
- **Composition roots moved to `cmd/vibewarden/`** (ADR-067) — cleaner
  separation between wiring and domain logic. (#867, closes #809)
- **Ports layer consolidated** (ADR-064) — all port interfaces live under
  `internal/ports/`, reducing import fan-out. (#843, closes #818)

### Bug fixes

- Caddy auth handler modules now registered when `auth.mode: kratos` is set;
  previously crashed with `unknown field "cookie_name"`. (#885, closes #883)
- External TLS provider in multi-site mode no longer falls through to ACME.
  (#858, closes #823)
- YAML quoting in language-aware health check commands corrected. (#898, closes #898)
- `vibew generate` no longer requires a scaffolding marker to be present.
  (#881, closes #880)
- Docker build context fixed for generated compose files. (#859, closes #808)
- `errors.Is` used consistently for `http.ErrServerClosed` checks across all
  five serve paths. (#837, closes #814)
- OpenBao `tryOpenBao` and `tryDynamicCredentials` silent error masking removed;
  transport errors now propagate. (#830, closes #812) (#834, closes #832)
- `CertMonitor.Stop` double-close panic fixed by adding a sync.Once guard.
  (#858, closes #823)
- SSRF `privateRanges` moved from package-level `init()` to per-guard state,
  eliminating a data race. (#838, closes #815)
- OpenBao `NotFound` is now a sentinel error for clean `errors.Is` checks.
  (#830, closes #821)
- WAF defaults in `vibewarden.reference.yaml` corrected. (#828, closes #810)
- Scaffold tests isolated and repo safety check added (ADR-066). (#846, closes #844)
- Removed `.claude/CLAUDE.md` and `.cursor/rules` from scaffold file lists.
  (#829, closes #811)
- CLI help-text corrections for `plugins` and `secret generate` commands.
  (#868, closes #865, #866)
- `vibew init` docs aligned with `vibew wrap` in getting-started guide.
  (#841, closes #820)
- `--lang` flag removed; bare `vibew init` used everywhere in docs. (#857, closes #842)

### Infrastructure and dependencies

- Dependabot configured for gomod, pip, docker, and github-actions ecosystems.
  (#847)
- Integration test pipeline added: Tier 1 multi-site Host-header routing and
  Tier 3 Docker-in-Docker deploy test. (#889, #890, #891)
- 9 Dependabot PRs merged: Alpine 3.21 to 3.23, `actions/checkout@v6`,
  `actions/setup-go@v6`, `actions/upload-artifact@v7`,
  `docker/setup-qemu-action@v4`, `docker/setup-buildx-action@v4`,
  `golangci/golangci-lint-action@v9`, OTel exporters (CVE-2026-39882),
  `modernc.org/sqlite` bump. (#847-#856, #827)
- CI Trivy image scan and coverage gate stabilised on main. (#833)
- All agents upgraded to Opus 4.7. (#803)
- ADRs split into individual files under `decisions/`; 6 previously missing
  ADRs recovered. (#894)

### Documentation

- `vibew deploy`, `vibew upgrade`, `vibew migrate`, `vibew plugins`, and
  `vibew secret generate` commands documented. (#864, closes #817)
- `llms.txt` short-index added at repo root. (#860, closes #826)
- Sample `AGENTS-VIBEWARDEN.md` added to `docs/examples/`. (#861, closes #825)
- Install PATH hint and dead-upstream error example added to docs. (#863, closes #825)
- Reference YAML readers pointed at `example.yaml` first. (#839, closes #822)

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

[v0.11.0]: https://github.com/vibewarden/vibewarden/releases/tag/v0.11.0
[v0.1.0]: https://github.com/vibewarden/vibewarden/releases/tag/v0.1.0
