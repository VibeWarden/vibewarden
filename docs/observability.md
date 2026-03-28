# Observability Stack

VibeWarden includes an optional local observability stack for development and testing.
It consists of **Prometheus** (metrics collection), **Loki** (log aggregation),
**Promtail** (log shipper), and **Grafana** (dashboards and log explorer), all started
via Docker Compose profiles so they do not run unless explicitly requested.

## Telemetry Configuration

VibeWarden uses OpenTelemetry as its telemetry foundation, supporting both pull-based
Prometheus scraping and push-based OTLP export. The `telemetry:` section in
`vibewarden.yaml` controls all telemetry behavior.

### Export Modes

VibeWarden supports three telemetry export modes:

| Mode | Prometheus | OTLP | Use Case |
|------|------------|------|----------|
| **Prometheus-only** (default) | Enabled | Disabled | Local development, single-instance deployments |
| **OTLP-only** | Disabled | Enabled | Cloud backends (Grafana Cloud, Datadog), fleet deployments |
| **Dual-export** | Enabled | Enabled | Migration, local + central collection |

### Prometheus-Only Mode (Default)

This is the zero-config default. VibeWarden exposes metrics at `/_vibewarden/metrics`
in Prometheus text format. No outbound connections are made.

```yaml
telemetry:
  enabled: true
  prometheus:
    enabled: true
  otlp:
    enabled: false
```

Or simply omit the `telemetry:` block entirely — the defaults match this configuration.

**When to use:** Local development, single-instance production where you run your own
Prometheus and scrape VibeWarden directly.

### OTLP-Only Mode

Metrics are pushed to an OTLP-compatible collector or backend. The `/_vibewarden/metrics`
endpoint is disabled. All telemetry flows outbound.

```yaml
telemetry:
  enabled: true
  prometheus:
    enabled: false
  otlp:
    enabled: true
    endpoint: https://otlp-gateway.example.com/otlp
    headers:
      Authorization: "Bearer ${OTLP_API_KEY}"
    interval: 30s
```

**When to use:** Cloud observability backends (Grafana Cloud, Datadog, Honeycomb, etc.),
fleet deployments where a central collector aggregates telemetry from multiple instances.

### Dual-Export Mode

Both Prometheus and OTLP exporters run simultaneously. Use this for gradual migration
or when you need both local scraping and central collection.

```yaml
telemetry:
  enabled: true
  prometheus:
    enabled: true
  otlp:
    enabled: true
    endpoint: http://otel-collector:4318
    interval: 15s
```

**When to use:** Migration from Prometheus-only to OTLP, or hybrid setups where local
dashboards coexist with central fleet observability.

### Configuration Reference

#### telemetry.enabled

**Type:** boolean
**Default:** `true`

Master switch for all telemetry collection. When `false`, no metrics are collected or
exported, and the `/_vibewarden/metrics` endpoint returns 404.

#### telemetry.path_patterns

**Type:** list of strings
**Default:** `[]`

URL path normalization patterns using colon-param syntax. Without patterns, all paths
are recorded as `"other"`. Configure the routes your app exposes to prevent
high-cardinality metric labels.

```yaml
telemetry:
  path_patterns:
    - "/users/:id"
    - "/api/v1/items/:item_id/comments/:comment_id"
```

#### telemetry.prometheus.enabled

**Type:** boolean
**Default:** `true`

Enables the Prometheus pull-based exporter. When enabled, metrics are served at
`/_vibewarden/metrics` in Prometheus text format with OpenMetrics compatibility.

#### telemetry.otlp.enabled

**Type:** boolean
**Default:** `false`

Enables the OTLP push-based exporter. Requires `telemetry.otlp.endpoint` to be set.

#### telemetry.otlp.endpoint

**Type:** string
**Default:** `""`

OTLP HTTP endpoint URL. Required when `telemetry.otlp.enabled` is `true`.

Examples:

- Local OTel Collector: `http://localhost:4318`
- Docker Compose: `http://otel-collector:4318`
- Grafana Cloud: `https://otlp-gateway-prod-us-central-0.grafana.net/otlp`

#### telemetry.otlp.headers

**Type:** map of string to string
**Default:** `{}`

HTTP headers to include with OTLP requests. Use for authentication.

```yaml
telemetry:
  otlp:
    headers:
      Authorization: "Basic ${GRAFANA_OTLP_TOKEN}"
      X-Custom-Header: "value"
```

#### telemetry.otlp.interval

**Type:** duration string
**Default:** `"30s"`

How often metrics are batched and pushed to the OTLP endpoint. Shorter intervals
reduce telemetry lag but increase network overhead.

Valid formats: `"15s"`, `"1m"`, `"30s"`.

#### telemetry.otlp.protocol

**Type:** string
**Default:** `"http"`

OTLP transport protocol. Only `"http"` is supported in this version. `"grpc"` is
reserved for future use.

#### telemetry.logs.otlp

**Type:** boolean
**Default:** `false`

Enables OTLP log export. When enabled, structured events (the AI-readable logs) are
exported to the same OTLP endpoint as metrics. Requires `telemetry.otlp.endpoint`
to be configured.

Logs are exported in addition to stdout JSON output — existing log collection via
stdout remains unchanged.

### Structured Event Log Export

VibeWarden's structured event logs (with `schema_version`, `event_type`, `ai_summary`,
and `payload` fields) can be exported via OTLP alongside metrics. Enable with:

```yaml
telemetry:
  otlp:
    enabled: true
    endpoint: http://otel-collector:4318
  logs:
    otlp: true
```

**How it works:**

1. Events are logged to stdout as JSON (existing behavior, always active)
2. Events are simultaneously sent to the OTel LoggerProvider
3. The LoggerProvider batches and pushes logs to the OTLP endpoint
4. OTel Collector receives logs and routes them to Loki (or any configured backend)

**OTel log record mapping:**

| Event field | OTel log record field |
|-------------|----------------------|
| `Timestamp` | `Timestamp` |
| `EventType` | Attribute: `event.type` |
| `SchemaVersion` | Attribute: `vibewarden.schema_version` |
| `AISummary` | `Body` (string) |
| `Payload.*` | Attributes: `vibewarden.payload.<key>` |

**Severity mapping:** Event types are mapped to OTel severity levels:

| Event type pattern | OTel Severity |
|-------------------|---------------|
| `*.failed`, `*.blocked`, `*.hit` | WARN |
| `*.unavailable`, `*_failed` | ERROR |
| All others | INFO |

### OTel Collector Architecture

When the observability profile is enabled (`docker compose --profile observability up`),
VibeWarden generates an OTel Collector configuration that acts as a telemetry hub:

```
VibeWarden --OTLP--> OTel Collector --metrics--> Prometheus (scrapes :8889)
                              |
                              +--logs--> Loki
```

The collector:

- Receives OTLP on port 4318 (HTTP)
- Exports metrics via Prometheus exporter on port 8889 (Prometheus scrapes this)
- Exports logs to Loki via the Loki exporter

**Collector config location:** `.vibewarden/generated/observability/otel-collector/config.yaml`

**Why a collector?**

- Decouples VibeWarden from backend details
- Enables batching, retry, and buffering
- Standard OTel pipeline that works with any OTLP-compatible backend
- Future-proof for distributed tracing

### Migrating from `metrics:` to `telemetry:`

The legacy `metrics:` config section is deprecated. VibeWarden automatically migrates
settings at startup and logs a warning.

**Before (deprecated):**

```yaml
metrics:
  enabled: true
  path_patterns:
    - "/users/:id"
```

**After (recommended):**

```yaml
telemetry:
  enabled: true
  path_patterns:
    - "/users/:id"
  prometheus:
    enabled: true
```

**Migration behavior:**

1. If `metrics:` is present but `telemetry:` is not, settings are copied automatically
2. A deprecation warning is logged at startup
3. The `/_vibewarden/metrics` endpoint works unchanged
4. Existing Prometheus scrapers and Grafana dashboards continue working

**When to migrate:** Update your config before the next major version. The `metrics:`
section will be removed in a future release.

### Example Configurations

#### Local Development (default)

No config needed. The defaults enable Prometheus-only mode:

```yaml
# Nothing required — defaults are:
# telemetry.enabled: true
# telemetry.prometheus.enabled: true
# telemetry.otlp.enabled: false
```

#### Grafana Cloud

Push metrics and logs to Grafana Cloud OTLP gateway:

```yaml
telemetry:
  enabled: true
  path_patterns:
    - "/api/v1/users/:id"
    - "/api/v1/orders/:order_id"
  prometheus:
    enabled: false  # Use OTLP instead
  otlp:
    enabled: true
    endpoint: https://otlp-gateway-prod-us-central-0.grafana.net/otlp
    headers:
      Authorization: "Basic ${GRAFANA_OTLP_TOKEN}"
    interval: 30s
  logs:
    otlp: true
```

Set `GRAFANA_OTLP_TOKEN` in your environment (base64-encoded `instanceId:apiKey`).

#### Self-Hosted OTel Collector

Push to your own OTel Collector while keeping local Prometheus scraping:

```yaml
telemetry:
  enabled: true
  path_patterns:
    - "/users/:id"
  prometheus:
    enabled: true  # Keep local /_vibewarden/metrics
  otlp:
    enabled: true
    endpoint: http://otel-collector.monitoring.svc:4318
    interval: 15s
  logs:
    otlp: true
```

#### Docker Compose Observability Stack

When using `docker compose --profile observability up`, the generated compose file
automatically sets these environment variables:

```
VIBEWARDEN_TELEMETRY_OTLP_ENABLED=true
VIBEWARDEN_TELEMETRY_OTLP_ENDPOINT=http://otel-collector:4318
VIBEWARDEN_TELEMETRY_LOGS_OTLP=true
```

No manual config changes needed — just enable the observability profile.

---

## Quick Start

Enable observability in `vibewarden.yaml`:

```yaml
observability:
  enabled: true
  grafana_port: 3001
```

Generate and start:

```bash
vibewarden generate
COMPOSE_PROFILES=observability docker compose -f .vibewarden/generated/docker-compose.yml up -d
```

Stop the stack:

```bash
COMPOSE_PROFILES=observability docker compose -f .vibewarden/generated/docker-compose.yml down
```

## Accessing the UIs

| Service    | URL                          | Notes                                  |
|------------|------------------------------|----------------------------------------|
| Grafana    | http://localhost:3000        | Anonymous access, Admin role, no login |
| Prometheus | http://localhost:9090        | No authentication required             |
| Loki       | http://localhost:3100/ready  | API only; query logs via Grafana       |

Grafana is configured with anonymous authentication so there is no login screen in
the local dev environment. This is intentional — do not use this configuration in
production.

## Dashboard Overview

The **VibeWarden** dashboard is automatically provisioned when Grafana starts. It
contains the following panels:

| Panel | Type | Description | Underlying Metric |
|-------|------|-------------|-------------------|
| Request Rate | Time series | Requests per second by HTTP status code | `vibewarden_requests_total` |
| Error Rate (5xx) | Stat | Fraction of requests returning 5xx responses | `vibewarden_requests_total{status_code=~"5.."}` |
| Latency Percentiles | Time series | P50, P95, P99 response times | `vibewarden_request_duration_seconds` |
| Active Connections | Gauge | Current open connections | `vibewarden_active_connections` |
| Rate Limit Hits/sec | Time series | Rate limit trigger rate | `vibewarden_rate_limit_hits_total` |
| Auth Decisions (Total) | Pie chart | Authentication allow vs. block counts | `vibewarden_auth_decisions_total` |
| Upstream Errors/sec | Time series | Rate of upstream connection failures | `vibewarden_upstream_errors_total` |

The dashboard JSON is embedded in the VibeWarden binary and generated to
`.vibewarden/generated/observability/grafana/dashboards/vibewarden.json` when
observability is enabled. It is loaded automatically by Grafana's provisioning config.

## Metrics Reference

VibeWarden exposes all metrics at:

```
http://localhost:8080/_vibewarden/metrics
```

The endpoint uses the standard Prometheus text exposition format.

### Application Metrics

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `vibewarden_requests_total` | Counter | `method`, `status_code`, `path_pattern` | Total HTTP requests handled |
| `vibewarden_request_duration_seconds` | Histogram | `method`, `path_pattern` | Request latency distribution |
| `vibewarden_rate_limit_hits_total` | Counter | `limit_type` | Number of rate limit triggers |
| `vibewarden_auth_decisions_total` | Counter | `decision` | Auth allow / block decisions |
| `vibewarden_upstream_errors_total` | Counter | — | Upstream connection failures |
| `vibewarden_active_connections` | Gauge | — | Currently active connections |

### Runtime Metrics

Go runtime and process metrics are exposed automatically via the standard
Prometheus collectors:

| Prefix | Description |
|--------|-------------|
| `go_*` | Go runtime metrics (goroutines, memory, GC pauses) |
| `process_*` | OS process metrics (CPU time, open file descriptors) |

## Architecture

```
[Your App]  <---->  [VibeWarden :8080]  <--(scrape)--  [Prometheus :9090]
                          |                                     |
                          +---> /_vibewarden/metrics            v
                                                         [Grafana :3000]
                                                               ^
[Docker container logs]  -->  [Promtail]  -->  [Loki :3100] --+
```

Prometheus scrapes VibeWarden every 15 seconds. Promtail discovers all running Docker
containers via the Docker socket, tails their log files, and ships log entries to Loki.
Grafana queries both Prometheus and Loki as data sources. All configs are generated
under `.vibewarden/generated/observability/` by `vibewarden generate`.

## Loki Log Aggregation

Loki aggregates logs from all Docker containers in the stack. Logs are available in
Grafana's **Explore** view (select the **Loki** data source).

### Querying VibeWarden Logs

VibeWarden emits structured JSON logs with the following top-level fields:

| Field            | Description                                      | Indexed as label |
|------------------|--------------------------------------------------|------------------|
| `schema_version` | Log schema version (e.g., `v1`)                  | Yes              |
| `event_type`     | Event kind (e.g., `request.completed`)           | Yes              |
| `level`          | Log level (`DEBUG`, `INFO`, `WARN`, `ERROR`)     | Yes              |
| `ai_summary`     | Human/AI-readable one-line description           | Structured metadata |
| `time`           | RFC 3339 timestamp of the event                  | Used as log timestamp |
| `payload`        | Event-specific data (arbitrary JSON object)      | Full-text search |

Example LogQL queries in Grafana Explore:

```logql
# All VibeWarden logs
{container="vibewarden-sidecar"}

# Only error-level events
{container="vibewarden-sidecar", level="ERROR"}

# Request completed events
{container="vibewarden-sidecar", event_type="request.completed"}

# Full-text search within payloads
{container="vibewarden-sidecar"} |= "rate_limit"

# Parse and filter on a payload field (e.g. status_code)
{container="vibewarden-sidecar", event_type="request.completed"}
  | json
  | payload_status_code >= 500
```

### Promtail Pipeline

Promtail parses each VibeWarden log line as JSON and:

1. Extracts `schema_version`, `event_type`, and `level` as Loki labels (low-cardinality,
   indexed for fast filtering).
2. Maps the `time` field to the Loki log timestamp so events are stored at the time
   VibeWarden recorded them, not the scrape time.
3. Promotes `ai_summary` as Loki structured metadata so it appears in the Grafana log
   details panel without bloating the label index.

Configuration is generated to `.vibewarden/generated/observability/promtail/promtail-config.yml`.

## Adding Custom Dashboards

1. Build your dashboard in the Grafana UI.
2. Export it: **Dashboard menu → Share → Export → Save to file**.
3. Place the exported JSON file in `.vibewarden/generated/observability/grafana/dashboards/`.
4. Restart Grafana to pick up the new file:
   ```bash
   docker compose -f .vibewarden/generated/docker-compose.yml restart grafana
   ```

Note: custom dashboards placed in the generated directory will be overwritten on the
next `vibewarden generate` run. For persistent custom dashboards, use Grafana's
built-in provisioning or import them via the Grafana API.

## Troubleshooting

### Grafana shows "No data" on panels

Prometheus may not have scraped VibeWarden yet, or VibeWarden is not running.

1. Check that all containers are up:
   ```bash
   docker compose --profile observability ps
   ```
2. Verify VibeWarden's metrics endpoint is reachable:
   ```bash
   curl http://localhost:8080/_vibewarden/metrics
   ```
3. Check Prometheus targets at http://localhost:9090/targets — the
   `vibewarden` target should show state `UP`.

### Port conflicts

If port 3000 or 9090 is already in use, Docker Compose will fail to start the
corresponding container. Stop the conflicting process or change the host port in
`docker-compose.yml`:

```yaml
ports:
  - "3001:3000"   # expose Grafana on host port 3001 instead
```

### Grafana starts but the dashboard is not visible

The dashboard is generated to
`.vibewarden/generated/observability/grafana/dashboards/vibewarden.json`. If the file
is missing, run `vibewarden generate` again. Check the Grafana container logs:

```bash
docker compose -f .vibewarden/generated/docker-compose.yml logs grafana
```

### Loki shows no logs in Grafana

1. Verify Loki is ready:
   ```bash
   curl http://localhost:3100/ready
   # expected: ready
   ```
2. Check Promtail is running and has no errors:
   ```bash
   docker compose --profile observability logs promtail
   ```
3. Confirm Promtail has write access to the Docker socket:
   ```bash
   docker compose --profile observability ps promtail
   ```
4. In Grafana Explore, select the **Loki** datasource and run:
   ```logql
   {service="vibewarden"}
   ```

### Prometheus cannot reach VibeWarden

Prometheus scrapes `vibewarden:8080` on the internal Docker network. If the
VibeWarden container is not healthy, Prometheus will mark the target as `DOWN`.
Check VibeWarden logs:

```bash
docker compose logs vibewarden
```

## Production Note

This observability stack is intended for **local development only**. For production:

- Deploy Prometheus and Grafana separately, with proper authentication and TLS.
- Configure alerting rules in Prometheus for critical metrics.
- Consider using the VibeWarden Fleet dashboard (Pro tier) at
  `app.vibewarden.dev`, which aggregates metrics and logs from multiple
  VibeWarden instances without requiring you to run your own Prometheus/Grafana.
