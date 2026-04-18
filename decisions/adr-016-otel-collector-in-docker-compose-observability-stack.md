# ADR-016: OTel Collector in Docker Compose Observability Stack

**Date**: 2026-03-28
**Issue**: #290
**Status**: Accepted

### Context

Epic #280 ("Switch telemetry from Prometheus to OpenTelemetry") transitions VibeWarden from
pull-based Prometheus scraping to push-based OTLP. Previous stories in this epic added:

- ADR-012: OTel SDK integration and MetricsCollector port/adapter refactoring
- ADR-013: OTLP exporter configuration
- ADR-014: Prometheus fallback exporter for backward compatibility
- ADR-015: Bridge slog structured events to OTel logs

The current observability stack (docker-compose observability profile) has:

- **Prometheus**: Scrapes `/_vibewarden/metrics` from the sidecar (pull)
- **Promtail**: Scrapes Docker container logs and pushes to Loki
- **Loki**: Receives logs from Promtail
- **Grafana**: Visualizes Prometheus metrics and Loki logs

With OTel, the sidecar now pushes metrics and logs via OTLP. The stack needs an
**OTel Collector** to receive OTLP from the sidecar and export to Prometheus and Loki.
This replaces the direct Prometheus scraping model.

**Goals from issue #290:**

1. Add `otel-collector` service to Docker Compose observability profile
2. Collector receives OTLP HTTP on port 4318 from VibeWarden
3. Collector exports metrics to Prometheus (via Prometheus exporter for scraping)
4. Collector exports logs to Loki via loki exporter
5. Remove or deprecate Promtail (OTel Collector replaces its function for VibeWarden logs)
6. Keep Grafana dashboards working with minimal changes
7. Healthcheck on collector service

### Decision

Add the OpenTelemetry Collector Contrib as the telemetry hub in the Docker Compose
observability stack. The collector acts as a central aggregation point:

```
VibeWarden --OTLP--> OTel Collector --metrics--> Prometheus --scrape--> Prometheus
                                    --logs--> Loki
```

**Architecture change:**

| Before (pull) | After (push via collector) |
|---------------|---------------------------|
| VibeWarden exposes `/_vibewarden/metrics` | VibeWarden pushes OTLP to collector |
| Prometheus scrapes VibeWarden directly | Prometheus scrapes collector's Prometheus exporter |
| Promtail scrapes Docker logs | OTel Collector receives OTLP logs from VibeWarden |
| | Collector pushes logs to Loki |

**Key decisions:**

1. **Use `otel/opentelemetry-collector-contrib` image**: The contrib distribution includes
   the `lokiexporter` required for Loki integration. License: Apache 2.0.

2. **Prometheus exporter (not remote write)**: The collector exposes a `/metrics` endpoint
   that Prometheus scrapes. This keeps Prometheus in its natural pull mode and requires
   minimal Prometheus config changes. Alternative was `prometheusremotewrite` exporter,
   but that requires enabling remote-write receiver in Prometheus and adds complexity.

3. **Keep Promtail for non-VibeWarden logs**: Promtail continues to scrape Docker logs
   for other containers (app, kratos, etc.). The OTel Collector handles only VibeWarden's
   structured event logs via OTLP. This avoids losing logs from services that do not
   speak OTLP.

4. **Collector port 4318 (OTLP HTTP)**: Standard OTLP HTTP port. The collector binds to
   4318 inside the Docker network; VibeWarden's default `telemetry.otlp.endpoint` in dev
   compose points to `http://otel-collector:4318`.

5. **Collector metrics endpoint on 8889**: The Prometheus exporter exposes metrics at
   `otel-collector:8889/metrics`. Prometheus scrapes this instead of VibeWarden directly.

#### Domain Model Changes

None. This story is pure infrastructure (Docker Compose and config templates). No changes
to domain entities, value objects, or events.

#### Ports (Interfaces)

None. No new Go interfaces. The OTel Collector is an external container, not embedded
in the VibeWarden binary.

#### Adapters

None. No Go adapter changes. The existing OTLP exporter in `internal/adapters/otel/`
already supports pushing to any OTLP endpoint.

#### Application Service

**Modify: `internal/app/generate/service.go`**

Add rendering of the OTel Collector config template. Update `generateObservability()` to:

1. Create `observability/otel-collector/` directory
2. Render `otel-collector-config.yml.tmpl` to `observability/otel-collector/config.yaml`

```go
// In generateObservability():

// Create otel-collector directory
dirs := []string{
    // ... existing dirs ...
    filepath.Join(obsDir, "otel-collector"),
}

// Render OTel Collector config
if err := s.renderer.RenderToFile(
    "observability/otel-collector-config.yml.tmpl",
    cfg,
    filepath.Join(obsDir, "otel-collector", "config.yaml"),
    true,
); err != nil {
    return fmt.Errorf("rendering otel-collector config: %w", err)
}
```

#### File Layout

**New files:**

| File | Purpose |
|------|---------|
| `internal/config/templates/observability/otel-collector-config.yml.tmpl` | OTel Collector YAML config template |

**Modified files:**

| File | Changes |
|------|---------|
| `internal/config/templates/docker-compose.yml.tmpl` | Add `otel-collector` service under observability profile |
| `internal/config/templates/observability/prometheus.yml.tmpl` | Scrape `otel-collector:8889` instead of `vibewarden` |
| `internal/app/generate/service.go` | Add otel-collector directory creation and config rendering |
| `internal/app/generate/service_test.go` | Add test for otel-collector config generation |

#### New Template: otel-collector-config.yml.tmpl

```yaml
# OTel Collector configuration — Generated by VibeWarden
# Do not edit manually — re-run `vibewarden generate` to regenerate.
#
# Receives OTLP from VibeWarden sidecar, exports to Prometheus and Loki.

receivers:
  otlp:
    protocols:
      http:
        endpoint: 0.0.0.0:4318

processors:
  batch:
    timeout: 10s
    send_batch_size: 1024

exporters:
  # Prometheus exporter: exposes /metrics for Prometheus to scrape
  prometheus:
    endpoint: 0.0.0.0:8889
    namespace: vibewarden
    const_labels:
      source: otel_collector

  # Loki exporter: pushes logs to Loki
  loki:
    endpoint: http://loki:3100/loki/api/v1/push
    default_labels_enabled:
      exporter: false
      job: true
    labels:
      attributes:
        event.type: "event_type"
        vibewarden.schema_version: "schema_version"
      resource:
        service.name: "service"

  # Debug exporter for troubleshooting (logs to stdout)
  debug:
    verbosity: basic

service:
  telemetry:
    logs:
      level: warn
    metrics:
      level: none
  pipelines:
    metrics:
      receivers: [otlp]
      processors: [batch]
      exporters: [prometheus]
    logs:
      receivers: [otlp]
      processors: [batch]
      exporters: [loki]
```

#### Modified Template: docker-compose.yml.tmpl

Add under observability services (after promtail, before grafana):

```yaml
  otel-collector:
    image: otel/opentelemetry-collector-contrib:0.123.0
    profiles:
      - observability
    restart: unless-stopped
    command: ["--config=/etc/otelcol-contrib/config.yaml"]
    volumes:
      - ./.vibewarden/generated/observability/otel-collector/config.yaml:/etc/otelcol-contrib/config.yaml:ro
    networks:
      - vibewarden
    depends_on:
      loki:
        condition: service_healthy
    healthcheck:
      test: ["CMD", "wget", "-q", "--spider", "http://localhost:13133/"]
      interval: 10s
      timeout: 5s
      retries: 5
```

**Notes:**
- Port 13133 is the OTel Collector's default health check extension port
- Port 4318 (OTLP HTTP) is exposed only within the Docker network (no host binding needed)
- Port 8889 (Prometheus exporter) is exposed only within the Docker network
- Collector depends on Loki being healthy (for log export)

**Update vibewarden service environment:**

When observability is enabled, set the OTLP endpoint to point to the collector:

```yaml
{{- if .Observability.Enabled }}
      - VIBEWARDEN_TELEMETRY_OTLP_ENABLED=true
      - VIBEWARDEN_TELEMETRY_OTLP_ENDPOINT=http://otel-collector:4318
      - VIBEWARDEN_TELEMETRY_LOGS_OTLP=true
{{- end }}
```

**Update Grafana depends_on:**

Grafana should also depend on otel-collector for a clean startup sequence:

```yaml
    depends_on:
      prometheus:
        condition: service_healthy
      loki:
        condition: service_healthy
      otel-collector:
        condition: service_healthy
```

#### Modified Template: prometheus.yml.tmpl

Change the vibewarden scrape target to scrape the OTel Collector's Prometheus exporter:

```yaml
# Prometheus configuration — Generated by VibeWarden
# Do not edit manually — re-run `vibewarden generate` to regenerate.

global:
  scrape_interval: 15s
  evaluation_interval: 15s

scrape_configs:
  - job_name: 'otel-collector'
    metrics_path: '/metrics'
    static_configs:
      - targets: ['otel-collector:8889']
        labels:
          instance: 'vibewarden-sidecar'

  - job_name: 'prometheus'
    static_configs:
      - targets: ['localhost:9090']
```

**Note:** The job_name remains descriptive. The `instance` label preserves dashboard
compatibility. Metric names remain unchanged because the OTel SDK produces the same
metric names as before.

#### Sequence

**Startup sequence (docker compose --profile observability up):**

1. Loki starts and becomes healthy
2. OTel Collector starts, depends on Loki healthy
3. Prometheus starts, scrapes OTel Collector
4. Promtail starts, scrapes Docker logs (non-VibeWarden containers)
5. Grafana starts, depends on Prometheus and Loki healthy
6. VibeWarden starts, pushes OTLP to OTel Collector

**Runtime telemetry flow:**

```
VibeWarden sidecar
    |
    +-- OTLP HTTP (metrics + logs) --> otel-collector:4318
                                              |
                                              +-- metrics --> :8889/metrics
                                              |                   |
                                              |                   v
                                              |            Prometheus scrapes
                                              |                   |
                                              |                   v
                                              |              Grafana
                                              |
                                              +-- logs --> loki:3100
                                                               |
                                                               v
                                                           Grafana
```

**Promtail parallel flow (unchanged):**

```
Docker containers (app, kratos, etc.)
    |
    +-- Docker logs --> Promtail --> Loki --> Grafana
```

#### Error Cases

| Error | When | Handling |
|-------|------|----------|
| Collector not reachable | Network partition, collector down | OTLP exporter retries with exponential backoff; sidecar continues operating (telemetry is best-effort) |
| Loki not reachable | Loki down | Collector buffers logs, retries; eventually drops if buffer full |
| Prometheus not scraping | Prometheus down | Metrics accumulate in collector; no data loss until collector restart |
| Invalid OTLP payload | Bug in sidecar | Collector logs error, drops invalid records |
| Collector unhealthy | Crash loop | Grafana won't start (depends_on); Docker restarts collector |

**Graceful degradation:**

- VibeWarden sidecar continues operating even if collector is unreachable
- Prometheus fallback exporter (`/_vibewarden/metrics`) remains available for direct scraping
- Promtail continues collecting non-VibeWarden logs independently

#### Test Strategy

**Unit tests:**

| File | Tests |
|------|-------|
| `internal/app/generate/service_test.go` | Verify otel-collector directory created; verify config.yaml rendered; verify docker-compose includes otel-collector service |

**Integration tests (manual or CI):**

| Test | Verification |
|------|--------------|
| `docker compose --profile observability up` | All services start and become healthy |
| Send request through sidecar | Metrics appear in Prometheus via collector |
| Trigger structured event | Log appears in Loki via collector |
| Grafana dashboard | Existing dashboards show data |

**What to mock vs. real:**

- Real: Template rendering, file system operations
- Mock: None needed for unit tests (template rendering is deterministic)

#### New Dependencies

**Docker image (not a Go dependency):**

| Image | Version | License | Purpose |
|-------|---------|---------|---------|
| `otel/opentelemetry-collector-contrib` | 0.123.0 | Apache 2.0 | OTel Collector with Loki exporter |

**License verification:**

The OpenTelemetry Collector Contrib repository is licensed under Apache 2.0:
https://github.com/open-telemetry/opentelemetry-collector-contrib/blob/main/LICENSE

Verified via:
```bash
curl -s https://raw.githubusercontent.com/open-telemetry/opentelemetry-collector-contrib/main/LICENSE | head -5
```

Output confirms Apache License Version 2.0.

**No new Go dependencies.** The OTel Collector runs as a separate container.

#### Configuration

**No new config fields in vibewarden.yaml.** The OTLP endpoint is set via environment
variables in docker-compose.yml when the observability profile is active.

**Environment variables set by docker-compose (observability profile):**

| Variable | Value | Purpose |
|----------|-------|---------|
| `VIBEWARDEN_TELEMETRY_OTLP_ENABLED` | `true` | Enable OTLP export |
| `VIBEWARDEN_TELEMETRY_OTLP_ENDPOINT` | `http://otel-collector:4318` | Collector endpoint |
| `VIBEWARDEN_TELEMETRY_LOGS_OTLP` | `true` | Enable log export via OTLP |

Users can override these in their own compose files or environment if they want to
point to a different collector.

### Consequences

**Positive:**

- **Unified telemetry hub:** Metrics and logs flow through a single collector
- **Push-based model:** No need for sidecar to expose metrics endpoint to external scrapers
- **Vendor-neutral:** Users can swap the collector's exporters to send data anywhere
- **Standard OTel pipeline:** Follows industry best practices for observability
- **Backward compatible:** Grafana dashboards work unchanged (metric names preserved)
- **Promtail coexists:** Non-VibeWarden logs still flow via Promtail

**Negative:**

- **Additional container:** One more service to run (small resource footprint)
- **Complexity:** Adds a hop between sidecar and backends
- **Version management:** Must track OTel Collector Contrib releases

**Trade-offs:**

- **Prometheus exporter vs. remote write:** Chose exporter. Remote write requires
  Prometheus config changes (enable receiver) and is less common in local dev setups.
  Exporter keeps Prometheus in its natural pull mode.

- **Keep Promtail vs. remove:** Chose keep. Removing Promtail would lose logs from
  other containers (app, kratos) that do not speak OTLP. Promtail is lightweight and
  handles non-VibeWarden logs.

- **Single collector vs. per-signal:** Chose single collector with multiple pipelines.
  Simpler to operate than separate collectors for metrics and logs.

**Future considerations:**

- When distributed tracing is added (#293), the collector will handle traces too
- Fleet dashboard (Pro tier) can point to a cloud-hosted OTel Collector instead of local
- Users with existing collectors can disable the bundled one and point sidecar directly
