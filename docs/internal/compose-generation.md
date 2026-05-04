# Docker Compose Generation — Internal Reference

> This file consolidates content relocated from three ADRs on 2026-05-04 as part of the
> ADR audit (audit report: `~/notes/vibewarden/audit-adr-2026-05-03.md`). Stubs remain at
> the original decision paths; existing PR / commit references continue to resolve.

---

## From ADR-006 — Add User App Service to Generated docker-compose.yml

**Date**: 2026-03-28
**Issue**: #279

### Context

The generated `docker-compose.yml` originally included only the VibeWarden sidecar and
Kratos. This section documents the decision to add the user's own application service
automatically.

### Decision

An `app` section in `vibewarden.yaml` configures how the user's application is included.
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

## From ADR-007 — Add Plugin-Dependent Services to Generated docker-compose.yml (OpenBao, Redis)

**Date**: 2026-03-28
**Issue**: #281

### Context

When plugins like `secrets` (OpenBao) or `rate-limiting` with Redis backend are enabled,
users previously had to manually add those infrastructure services.

### Decision

Extend the `docker-compose.yml.tmpl` template to conditionally include:
- **OpenBao** when `secrets.enabled: true`
- **Redis** when `rate_limit.store: redis`

#### Helper functions

```go
// NeedsOpenBao returns true if the config requires an OpenBao service.
func NeedsOpenBao(cfg *config.Config) bool { return cfg.Secrets.Enabled }

// NeedsRedis returns true if the config requires a Redis service.
func NeedsRedis(cfg *config.Config) bool { return cfg.RateLimit.Store == "redis" }

// NeedsSeedSecrets returns true if dev mode should seed OpenBao with demo secrets.
func NeedsSeedSecrets(cfg *config.Config) bool {
    if !cfg.Secrets.Enabled { return false }
    return len(cfg.Secrets.Inject.Headers) > 0 || len(cfg.Secrets.Inject.Env) > 0
}
```

These helpers are registered as template `FuncMap` entries in the template adapter.

#### Dependency ordering

`vibewarden` depends on:
- `seed-secrets` (with `service_completed_successfully`) when inject entries exist
- `openbao` (with `service_healthy`) when secrets enabled but no inject entries
- `redis` (with `service_healthy`) when rate_limit.store is redis

---

## From ADR-008 — Add Observability Profile to Generated docker-compose.yml

**Date**: 2026-03-28
**Issue**: #282

### Context

The observability stack (Prometheus, Grafana, Loki, Promtail) was originally hand-crafted
in the `observability/` directory. This section documents making it generated from templates.

### Decision

Add an `observability` config section to `vibewarden.yaml` and generate all observability
config files from templates based on the working configs in `observability/`.

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
