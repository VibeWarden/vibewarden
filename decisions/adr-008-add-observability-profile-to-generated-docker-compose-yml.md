# ADR-008: Add Observability Profile to Generated docker-compose.yml


**Date**: 2026-03-28
**Issue**: #282
**Status**: Accepted

### Context

Epic #277 establishes that `vibewarden generate` should produce the entire runtime stack from
`vibewarden.yaml`. The observability stack (Prometheus, Grafana, Loki, Promtail) is currently
hand-crafted in the `observability/` directory with configs that must be manually copied.

This ADR designs the automatic generation of observability infrastructure:
- Prometheus for metrics collection (scrapes VibeWarden's `/_vibewarden/metrics` endpoint)
- Grafana for visualization (pre-provisioned with datasources and VibeWarden dashboard)
- Loki for log aggregation
- Promtail for log collection (scrapes Docker container logs)

The observability services are placed under a Docker Compose profile (`observability`) so they
only start when the user explicitly requests them via `COMPOSE_PROFILES=observability`.

### Decision

Add an `observability` config section to `vibewarden.yaml` and generate all observability
config files from templates based on the existing working configs in `observability/`.

#### Domain Model Changes

No new domain entities. This is a config-driven template enhancement.

#### Ports (Interfaces)

No new interfaces required. The existing `ports.TemplateRenderer` interface is sufficient.

#### Adapters

No new adapters. The existing template adapter handles the rendering.

#### Config Additions

Add `ObservabilityConfig` to `internal/config/config.go`:

```go
// ObservabilityConfig holds settings for the optional observability stack.
// When enabled, vibewarden generate produces Prometheus, Grafana, Loki, and
// Promtail configs under .vibewarden/generated/observability/.
type ObservabilityConfig struct {
    // Enabled toggles generation of the observability stack (default: false).
    Enabled bool `mapstructure:"enabled"`

    // GrafanaPort is the host port Grafana binds to (default: 3001).
    // This avoids conflict with common app ports like 3000.
    GrafanaPort int `mapstructure:"grafana_port"`

    // PrometheusPort is the host port Prometheus binds to (default: 9090).
    PrometheusPort int `mapstructure:"prometheus_port"`

    // LokiPort is the host port Loki binds to (default: 3100).
    LokiPort int `mapstructure:"loki_port"`

    // RetentionDays is how long Loki retains log data (default: 7).
    RetentionDays int `mapstructure:"retention_days"`
}
```

Add to the main `Config` struct:

```go
// Observability configures the optional observability stack (Prometheus,
// Grafana, Loki, Promtail) generated under the "observability" compose profile.
Observability ObservabilityConfig `mapstructure:"observability"`
```

Add defaults in `Load()`:

```go
v.SetDefault("observability.enabled", false)
v.SetDefault("observability.grafana_port", 3001)
v.SetDefault("observability.prometheus_port", 9090)
v.SetDefault("observability.loki_port", 3100)
v.SetDefault("observability.retention_days", 7)
```

#### Application Service Changes

Add a new helper function in `internal/app/generate/helpers.go`:

```go
// NeedsObservability returns true if the config requires the observability
// stack (Prometheus, Grafana, Loki, Promtail) in the generated compose.
func NeedsObservability(cfg *config.Config) bool {
    return cfg.Observability.Enabled
}
```

Update `internal/app/generate/Service.Generate()` to generate observability configs:

```go
// Generate observability configs when enabled.
if cfg.Observability.Enabled {
    if err := s.generateObservability(cfg, outputDir); err != nil {
        return fmt.Errorf("generating observability configs: %w", err)
    }
}
```

Add a new method `generateObservability()`:

```go
// generateObservability writes all observability config files to
// <outputDir>/observability/.
func (s *Service) generateObservability(cfg *config.Config, outputDir string) error {
    obsDir := filepath.Join(outputDir, "observability")

    // Create directory structure
    dirs := []string{
        filepath.Join(obsDir, "prometheus"),
        filepath.Join(obsDir, "grafana", "provisioning", "datasources"),
        filepath.Join(obsDir, "grafana", "provisioning", "dashboards"),
        filepath.Join(obsDir, "grafana", "dashboards"),
        filepath.Join(obsDir, "loki"),
        filepath.Join(obsDir, "promtail"),
    }
    for _, dir := range dirs {
        if err := os.MkdirAll(dir, permDir); err != nil {
            return fmt.Errorf("creating directory %q: %w", dir, err)
        }
    }

    // Render Prometheus config
    if err := s.renderer.RenderToFile(
        "observability/prometheus.yml.tmpl",
        cfg,
        filepath.Join(obsDir, "prometheus", "prometheus.yml"),
        true,
    ); err != nil {
        return fmt.Errorf("rendering prometheus.yml: %w", err)
    }

    // Render Grafana datasources
    if err := s.renderer.RenderToFile(
        "observability/grafana-datasources.yml.tmpl",
        cfg,
        filepath.Join(obsDir, "grafana", "provisioning", "datasources", "datasources.yml"),
        true,
    ); err != nil {
        return fmt.Errorf("rendering grafana datasources: %w", err)
    }

    // Render Grafana dashboard provisioner
    if err := s.renderer.RenderToFile(
        "observability/grafana-dashboards.yml.tmpl",
        cfg,
        filepath.Join(obsDir, "grafana", "provisioning", "dashboards", "dashboards.yml"),
        true,
    ); err != nil {
        return fmt.Errorf("rendering grafana dashboard provisioner: %w", err)
    }

    // Copy Grafana dashboard JSON (static, not a template)
    dashboardJSON, err := templates.FS.ReadFile("observability/vibewarden-dashboard.json")
    if err != nil {
        return fmt.Errorf("reading embedded dashboard JSON: %w", err)
    }
    dashboardPath := filepath.Join(obsDir, "grafana", "dashboards", "vibewarden.json")
    if err := os.WriteFile(dashboardPath, dashboardJSON, permConfig); err != nil {
        return fmt.Errorf("writing dashboard JSON: %w", err)
    }

    // Render Loki config
    if err := s.renderer.RenderToFile(
        "observability/loki-config.yml.tmpl",
        cfg,
        filepath.Join(obsDir, "loki", "loki-config.yml"),
        true,
    ); err != nil {
        return fmt.Errorf("rendering loki-config.yml: %w", err)
    }

    // Render Promtail config
    if err := s.renderer.RenderToFile(
        "observability/promtail-config.yml.tmpl",
        cfg,
        filepath.Join(obsDir, "promtail", "promtail-config.yml"),
        true,
    ); err != nil {
        return fmt.Errorf("rendering promtail-config.yml: %w", err)
    }

    return nil
}
```

#### File Layout

Files to modify:

| File | Change |
|------|--------|
| `internal/config/config.go` | Add `ObservabilityConfig` struct and field |
| `internal/app/generate/helpers.go` | Add `NeedsObservability()` function |
| `internal/app/generate/helpers_test.go` | Add tests for `NeedsObservability()` |
| `internal/app/generate/service.go` | Add `generateObservability()` method |
| `internal/app/generate/service_test.go` | Add tests for observability generation |
| `internal/config/templates/docker-compose.yml.tmpl` | Add observability services under profile |

New template files (in `internal/config/templates/`):

| File | Purpose |
|------|---------|
| `observability/prometheus.yml.tmpl` | Prometheus scrape config |
| `observability/grafana-datasources.yml.tmpl` | Grafana datasource provisioning |
| `observability/grafana-dashboards.yml.tmpl` | Grafana dashboard provisioner config |
| `observability/vibewarden-dashboard.json` | Pre-built VibeWarden dashboard (static) |
| `observability/loki-config.yml.tmpl` | Loki storage and retention config |
| `observability/promtail-config.yml.tmpl` | Promtail Docker log scraping config |

#### Template Specifications

##### prometheus.yml.tmpl

```yaml
# Prometheus configuration — Generated by VibeWarden
# Do not edit manually — re-run `vibewarden generate` to regenerate.

global:
  scrape_interval: 15s
  evaluation_interval: 15s

scrape_configs:
  - job_name: 'vibewarden'
    metrics_path: '/_vibewarden/metrics'
    static_configs:
      - targets: ['vibewarden:{{ .Server.Port }}']
        labels:
          instance: 'vibewarden-sidecar'

  - job_name: 'prometheus'
    static_configs:
      - targets: ['localhost:9090']
```

##### grafana-datasources.yml.tmpl

```yaml
# Grafana datasources — Generated by VibeWarden
# Do not edit manually — re-run `vibewarden generate` to regenerate.

apiVersion: 1

datasources:
  - name: Prometheus
    type: prometheus
    uid: prometheus
    access: proxy
    url: http://prometheus:9090
    isDefault: true
    editable: false

  - name: Loki
    type: loki
    uid: loki
    access: proxy
    url: http://loki:3100
    isDefault: false
    editable: false
    jsonData:
      maxLines: 1000
```

##### grafana-dashboards.yml.tmpl

```yaml
# Grafana dashboard provisioner — Generated by VibeWarden
# Do not edit manually — re-run `vibewarden generate` to regenerate.

apiVersion: 1

providers:
  - name: 'VibeWarden'
    orgId: 1
    folder: ''
    folderUid: ''
    type: file
    disableDeletion: false
    updateIntervalSeconds: 30
    allowUiUpdates: true
    options:
      path: /var/lib/grafana/dashboards
```

##### loki-config.yml.tmpl

```yaml
# Loki configuration — Generated by VibeWarden
# Do not edit manually — re-run `vibewarden generate` to regenerate.
#
# Storage: local filesystem (single-node, not for production clustering).
# Retention: {{ .Observability.RetentionDays }} days.

auth_enabled: false

server:
  http_listen_port: 3100
  grpc_listen_port: 9096
  log_level: warn

common:
  instance_addr: 127.0.0.1
  path_prefix: /loki
  storage:
    filesystem:
      chunks_directory: /loki/chunks
      rules_directory: /loki/rules
  replication_factor: 1
  ring:
    kvstore:
      store: inmemory

schema_config:
  configs:
    - from: "2020-10-24"
      store: tsdb
      object_store: filesystem
      schema: v13
      index:
        prefix: index_
        period: 24h

limits_config:
  retention_period: {{ mul .Observability.RetentionDays 24 }}h

compactor:
  working_directory: /loki/compactor
  retention_enabled: true
  delete_request_store: filesystem

ruler:
  alertmanager_url: http://localhost:9093
```

##### promtail-config.yml.tmpl

```yaml
# Promtail configuration — Generated by VibeWarden
# Do not edit manually — re-run `vibewarden generate` to regenerate.
#
# Scrapes Docker container logs and ships them to Loki.
# VibeWarden's structured JSON logs are parsed so that each field becomes
# queryable in Grafana.

server:
  http_listen_port: 9080
  grpc_listen_port: 0
  log_level: warn

positions:
  filename: /tmp/positions.yaml

clients:
  - url: http://loki:3100/loki/api/v1/push

scrape_configs:
  - job_name: docker
    docker_sd_configs:
      - host: unix:///var/run/docker.sock
        refresh_interval: 10s
        filters:
          - name: status
            values: ["running"]

    relabel_configs:
      - source_labels: [__meta_docker_container_name]
        regex: "/(.*)"
        target_label: container
      - source_labels: [__meta_docker_container_label_com_docker_compose_service]
        target_label: service
      - source_labels: [__meta_docker_container_label_com_docker_compose_project]
        target_label: compose_project

    pipeline_stages:
      - json:
          expressions:
            schema_version: schema_version
            event_type: event_type
            ai_summary: ai_summary
            level: level
            time: time

      - labels:
          schema_version:
          event_type:
          level:

      - timestamp:
          source: time
          format: RFC3339Nano
          fallback_formats:
            - RFC3339
            - UnixMs

      - structured_metadata:
          ai_summary:
```

##### vibewarden-dashboard.json

This file is copied verbatim from `observability/grafana/dashboards/vibewarden.json`.
It is a static JSON file (not a template) containing the pre-built VibeWarden dashboard
with panels for:
- Request rate and latency
- Error rates by status code
- Rate limiting metrics
- Auth middleware metrics
- Log explorer with VibeWarden structured log fields

#### Docker Compose Template Changes

Add observability services to `docker-compose.yml.tmpl` under the `observability` profile:

```yaml
{{- if .Observability.Enabled }}

  prometheus:
    image: prom/prometheus:v3.2.1
    profiles:
      - observability
    restart: unless-stopped
    command:
      - '--config.file=/etc/prometheus/prometheus.yml'
      - '--storage.tsdb.path=/prometheus'
      - '--web.console.libraries=/usr/share/prometheus/console_libraries'
      - '--web.console.templates=/usr/share/prometheus/consoles'
    ports:
      - "{{ .Observability.PrometheusPort }}:9090"
    volumes:
      - ./.vibewarden/generated/observability/prometheus/prometheus.yml:/etc/prometheus/prometheus.yml:ro
      - prometheus-data:/prometheus
    networks:
      - vibewarden
    healthcheck:
      test: ["CMD", "wget", "-q", "--spider", "http://localhost:9090/-/healthy"]
      interval: 10s
      timeout: 5s
      retries: 5

  loki:
    image: grafana/loki:3.4.3
    profiles:
      - observability
    restart: unless-stopped
    command: -config.file=/etc/loki/loki-config.yml
    ports:
      - "{{ .Observability.LokiPort }}:3100"
    volumes:
      - ./.vibewarden/generated/observability/loki/loki-config.yml:/etc/loki/loki-config.yml:ro
      - loki-data:/loki
    networks:
      - vibewarden
    healthcheck:
      test: ["CMD-SHELL", "wget -q --spider http://localhost:3100/ready || exit 1"]
      interval: 10s
      timeout: 5s
      retries: 5

  promtail:
    image: grafana/promtail:3.4.3
    profiles:
      - observability
    restart: unless-stopped
    command: -config.file=/etc/promtail/promtail-config.yml
    volumes:
      - ./.vibewarden/generated/observability/promtail/promtail-config.yml:/etc/promtail/promtail-config.yml:ro
      - /var/run/docker.sock:/var/run/docker.sock:ro
      - /var/lib/docker/containers:/var/lib/docker/containers:ro
    networks:
      - vibewarden
    depends_on:
      loki:
        condition: service_healthy

  grafana:
    image: grafana/grafana:11.5.2
    profiles:
      - observability
    restart: unless-stopped
    environment:
      GF_AUTH_ANONYMOUS_ENABLED: "true"
      GF_AUTH_ANONYMOUS_ORG_ROLE: "Admin"
      GF_AUTH_DISABLE_LOGIN_FORM: "true"
      GF_SECURITY_ADMIN_PASSWORD: "admin"
    ports:
      - "{{ .Observability.GrafanaPort }}:3000"
    volumes:
      - ./.vibewarden/generated/observability/grafana/provisioning:/etc/grafana/provisioning:ro
      - ./.vibewarden/generated/observability/grafana/dashboards:/var/lib/grafana/dashboards:ro
      - grafana-data:/var/lib/grafana
    networks:
      - vibewarden
    depends_on:
      prometheus:
        condition: service_healthy
      loki:
        condition: service_healthy
    healthcheck:
      test: ["CMD-SHELL", "wget -q --spider http://localhost:3000/api/health || exit 1"]
      interval: 10s
      timeout: 5s
      retries: 5
{{- end }}
```

Add observability volumes to the volumes section:

```yaml
volumes:
{{- /* existing volumes */ -}}
{{- if .Observability.Enabled }}
  prometheus-data:
  loki-data:
  grafana-data:
{{- end }}
```

Update the header comment to include observability services:

```yaml
{{- if .Observability.Enabled }}
#   prometheus  — Metrics collection (profile: observability)
#   loki        — Log aggregation (profile: observability)
#   promtail    — Log collector (profile: observability)
#   grafana     — Visualization (profile: observability)
{{- end }}
```

#### Sequence

1. User sets `observability.enabled: true` in `vibewarden.yaml`
2. User optionally customizes ports: `grafana_port`, `prometheus_port`, `loki_port`
3. User optionally sets `retention_days` for log retention
4. User runs `vibewarden generate`
5. Generate service reads config and:
   - Creates `.vibewarden/generated/observability/` directory structure
   - Renders Prometheus config with correct VibeWarden port
   - Renders Grafana datasources pointing to prometheus/loki containers
   - Renders Grafana dashboard provisioner config
   - Copies the static VibeWarden dashboard JSON
   - Renders Loki config with retention period
   - Renders Promtail config for Docker log scraping
   - Renders docker-compose.yml with observability services under profile
6. User starts the stack:
   - Without observability: `docker compose up`
   - With observability: `COMPOSE_PROFILES=observability docker compose up`
7. When observability profile is active:
   - Prometheus scrapes VibeWarden at `http://vibewarden:8080/_vibewarden/metrics`
   - Promtail tails Docker container logs and ships to Loki
   - Loki ingests logs with VibeWarden structured metadata
   - Grafana serves the pre-provisioned dashboard at `http://localhost:3001`

#### Generated Output Structure

When `observability.enabled: true`:

```
.vibewarden/generated/
  docker-compose.yml
  kratos/
    ...
  observability/
    prometheus/
      prometheus.yml
    grafana/
      provisioning/
        datasources/
          datasources.yml
        dashboards/
          dashboards.yml
      dashboards/
        vibewarden.json
    loki/
      loki-config.yml
    promtail/
      promtail-config.yml
```

#### Error Cases

| Error | Handling |
|-------|----------|
| Port conflict on GrafanaPort | Docker reports port binding error; user adjusts `grafana_port` |
| Port conflict on PrometheusPort | Docker reports port binding error; user adjusts `prometheus_port` |
| Docker socket not accessible | Promtail fails to start; logs show permission error |
| Loki fails healthcheck | Promtail/Grafana `depends_on` keeps them waiting |
| Prometheus fails healthcheck | Grafana `depends_on` keeps it waiting |
| Invalid retention_days (0 or negative) | Loki config invalid; service fails to start |
| Template rendering fails | Generate returns error; no partial output |

#### Validation

Add validation in `Config.Validate()`:

```go
// observability validation
if c.Observability.Enabled {
    if c.Observability.GrafanaPort <= 0 || c.Observability.GrafanaPort > 65535 {
        errs = append(errs, fmt.Sprintf(
            "observability.grafana_port %d is invalid; must be 1-65535",
            c.Observability.GrafanaPort,
        ))
    }
    if c.Observability.PrometheusPort <= 0 || c.Observability.PrometheusPort > 65535 {
        errs = append(errs, fmt.Sprintf(
            "observability.prometheus_port %d is invalid; must be 1-65535",
            c.Observability.PrometheusPort,
        ))
    }
    if c.Observability.LokiPort <= 0 || c.Observability.LokiPort > 65535 {
        errs = append(errs, fmt.Sprintf(
            "observability.loki_port %d is invalid; must be 1-65535",
            c.Observability.LokiPort,
        ))
    }
    if c.Observability.RetentionDays <= 0 {
        errs = append(errs, fmt.Sprintf(
            "observability.retention_days %d is invalid; must be > 0",
            c.Observability.RetentionDays,
        ))
    }
}
```

#### Template Function for Multiplication

The Loki template needs `mul` to calculate retention hours from days. Add to the template
FuncMap in `internal/adapters/template/renderer.go`:

```go
funcMap := template.FuncMap{
    "mul": func(a, b int) int { return a * b },
}
```

#### Test Strategy

**Unit Tests** (in `internal/app/generate/helpers_test.go`):

| Test | Description |
|------|-------------|
| `TestNeedsObservability_Enabled` | Returns true when `observability.enabled: true` |
| `TestNeedsObservability_Disabled` | Returns false when `observability.enabled: false` |
| `TestNeedsObservability_Default` | Returns false when observability section missing |

**Config Validation Tests** (in `internal/config/config_test.go`):

| Test | Description |
|------|-------------|
| `TestValidate_Observability_InvalidGrafanaPort` | Catches port < 1 or > 65535 |
| `TestValidate_Observability_InvalidPrometheusPort` | Catches port < 1 or > 65535 |
| `TestValidate_Observability_InvalidLokiPort` | Catches port < 1 or > 65535 |
| `TestValidate_Observability_InvalidRetentionDays` | Catches retention <= 0 |
| `TestValidate_Observability_ValidConfig` | Passes with valid values |

**Template Tests** (in `internal/app/generate/service_test.go`):

| Test | Description |
|------|-------------|
| `TestGenerate_Observability_WhenEnabled` | Observability dir and files created |
| `TestGenerate_Observability_WhenDisabled` | No observability dir created |
| `TestGenerate_Observability_PrometheusConfig` | Prometheus targets VibeWarden port |
| `TestGenerate_Observability_LokiRetention` | Loki retention matches config |
| `TestGenerate_Observability_GrafanaDatasources` | Datasources point to correct URLs |
| `TestGenerate_Observability_Dashboard` | Dashboard JSON copied correctly |
| `TestGenerate_Observability_ComposeServices` | Prometheus/Loki/Promtail/Grafana present |
| `TestGenerate_Observability_ComposeProfiles` | Services have `profiles: [observability]` |
| `TestGenerate_Observability_ComposeVolumes` | prometheus-data/loki-data/grafana-data volumes |
| `TestGenerate_Observability_ComposePorts` | Ports match config values |
| `TestGenerate_Observability_ComposeDependsOn` | Dependency chain correct |

**Integration Tests** (in `internal/app/generate/service_integration_test.go`):

| Test | Description |
|------|-------------|
| `TestGenerate_Integration_Observability` | Full render, validate all observability files |
| `TestGenerate_Integration_ObservabilityWithAuth` | Observability + Auth, validate compose |

Tests should:
- Parse generated YAML configs and verify structure
- Verify ports are substituted correctly
- Verify retention calculation is correct
- Verify Grafana dashboard JSON is valid JSON
- Verify compose services have correct profile annotation

#### New Dependencies

None. All images are pulled at runtime by Docker Compose:

| Image | Version | License | Purpose |
|-------|---------|---------|---------|
| `prom/prometheus` | v3.2.1 | Apache 2.0 | Metrics collection |
| `grafana/grafana` | 11.5.2 | AGPL 3.0 (runtime only) | Visualization |
| `grafana/loki` | 3.4.3 | AGPL 3.0 (runtime only) | Log aggregation |
| `grafana/promtail` | 3.4.3 | Apache 2.0 | Log collection |

Note: Grafana and Loki are AGPL 3.0 licensed. Since VibeWarden does not embed or link
against these components (they are pulled as Docker images at runtime), the AGPL does
not apply to VibeWarden's codebase. This is the standard usage pattern documented by
Grafana Labs for self-hosted deployments.

### Consequences

**Positive:**
- Full observability stack generated from single config file
- Zero manual setup for metrics, logs, and dashboards
- Compose profile keeps observability optional (not started by default)
- Pre-provisioned Grafana means instant dashboard access on first run
- Retention configurable to manage disk usage
- Ports configurable to avoid conflicts

**Negative:**
- Additional complexity in the generate service
- Dashboard JSON is embedded and may drift from the `observability/` reference
- Promtail requires Docker socket access (security consideration)
- Generated configs are dev-focused; prod deployments may need tuning

**Trade-offs:**
- Compose profile vs separate compose file: Profile keeps single file; separation would be cleaner
- Embedded dashboard vs generated: Embedded is simpler; generated would allow customization
- Single-node Loki vs cluster: Single-node is sufficient for dev; prod should use external stack
- Anonymous Grafana auth vs login: Anonymous is simpler for dev; prod should enable auth

---
