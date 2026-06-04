# Docker Compose Generation — Internal Reference

> This file consolidates content relocated from three ADRs on 2026-05-04 as part of the
> ADR audit (audit report: `~/notes/vibewarden/audit-adr-2026-05-03.md`). Stubs remain at
> the original decision paths; existing PR / commit references continue to resolve.

---

## User App Service in Generated docker-compose.yml

**Source**: ADR-006 | **Date**: 2026-03-28 | **Issue**: #279

The generated `docker-compose.yml` includes the user's own application service automatically.
An `app` section in `vibewarden.yaml` controls how it is included.
Two modes:

1. **Dev mode** (`app.build`): Build the app from local Dockerfile
2. **Prod mode** (`app.image`): Pull a pre-built image from a registry

#### Config

```go
type AppConfig struct {
    Build string `mapstructure:"build"` // Docker build context, e.g. "."
    Image string `mapstructure:"image"` // Docker image ref, e.g. "ghcr.io/org/app:latest"
}
```

#### Key design points

- **Profile-agnostic template**: The template renders based on what is configured in the YAML.
- **Image override via env var**: `VIBEWARDEN_APP_IMAGE` overrides the configured image in prod.
- **Healthcheck**: The app service includes a default healthcheck to `localhost:<upstream_port>/health`.
- **Dependency ordering**: `vibewarden` service `depends_on` the app with `service_healthy` condition.

#### Build vs. image precedence

When both are set, `app.build` takes precedence (dev mode first).

---

## Plugin-Dependent Services (OpenBao, Redis)

**Source**: ADR-007 | **Date**: 2026-03-28 | **Issue**: #281

When plugins like `secrets` (OpenBao) or `rate-limiting` with Redis backend are enabled,
the `docker-compose.yml.tmpl` template conditionally includes:
- **OpenBao** when `secrets.enabled: true` AND `secrets.store: openbao`
  (`store: builtin` or unset → no OpenBao container, no `openbao/config.hcl`, no `seed-secrets.sh`)
- **Redis** when `rate_limit.store: redis`

#### Helper functions

```go
// NeedsOpenBao returns true if the config requires an OpenBao service.
func NeedsOpenBao(cfg *config.Config) bool { return cfg.Secrets.Enabled }

// NeedsRedis returns true if the config requires a Redis service.
func NeedsRedis(cfg *config.Config) bool { return cfg.RateLimit.Store == "redis" }

// NeedsOpenBaoConfig returns true if openbao/config.hcl should be written.
// Requires UsesOpenBao() (enabled AND store=="openbao") AND profile=="prod".
func NeedsOpenBaoConfig(cfg *config.Config) bool {
    return cfg.Secrets.UsesOpenBao() && cfg.Profile == "prod"
}

// NeedsSeedSecrets returns true if seed-secrets.sh should be written.
// Requires UsesOpenBao() AND at least one inject entry. Returns false for
// store:"builtin" — seed-secrets.sh is only consumed by the seed-secrets
// container which is itself only emitted for the openbao store.
func NeedsSeedSecrets(cfg *config.Config) bool {
    if !cfg.Secrets.UsesOpenBao() { return false }
    return len(cfg.Secrets.Inject.Headers) > 0 || len(cfg.Secrets.Inject.Env) > 0
}
```

`UsesOpenBao()` is defined on `SecretsConfig`: `return s.Enabled && s.Store == "openbao"`.
An empty `Store` defaults to `"builtin"`, so OpenBao infrastructure is only provisioned
when `store: openbao` is set explicitly.

These helpers are registered as template `FuncMap` entries in the template adapter.

#### Dependency ordering

`vibewarden` depends on:
- `seed-secrets` (with `service_completed_successfully`) when inject entries exist and store is openbao
- `openbao` (with `service_healthy`) when store is openbao but no inject entries
- `redis` (with `service_healthy`) when rate_limit.store is redis

---

## Observability Profile

**Source**: ADR-008 | **Date**: 2026-03-28 | **Issue**: #282

The observability stack (Prometheus, Grafana, Loki, Promtail) is generated from templates.
An `observability` config section in `vibewarden.yaml` controls what is generated.

#### Config

```go
type ObservabilityConfig struct {
    Enabled        bool `mapstructure:"enabled"`
    GrafanaPort    int  `mapstructure:"grafana_port"`    // default 3001
    PrometheusPort int  `mapstructure:"prometheus_port"` // default 9090
    LokiPort       int  `mapstructure:"loki_port"`       // default 3100
    RetentionDays  int  `mapstructure:"retention_days"`  // default 7
}
```

#### Services placed under `observability` Docker Compose profile

- `prometheus` — scrapes metrics
- `loki` — log aggregation
- `promtail` — Docker log collection (for non-VibeWarden containers)
- `grafana` — visualization with pre-provisioned dashboards

Start with: `COMPOSE_PROFILES=observability docker compose up`

#### Generated file layout (when enabled)

```
.vibewarden/generated/
  observability/
    prometheus/prometheus.yml
    grafana/provisioning/datasources/datasources.yml
    grafana/provisioning/dashboards/dashboards.yml
    grafana/dashboards/vibewarden.json
    loki/loki-config.yml
    promtail/promtail-config.yml
    otel-collector/config.yaml   (added by ADR-016)
```

#### License note for Docker images

Grafana and Loki are AGPL 3.0 licensed. Since VibeWarden does not embed or link against
these (they are Docker images pulled at runtime), the AGPL does not apply to VibeWarden's
codebase. Standard Grafana Labs self-hosted usage pattern.
