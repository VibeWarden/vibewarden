# Observability Stack

VibeWarden includes an optional local observability stack for development and testing.
It consists of **Prometheus** (metrics collection), **Loki** (log aggregation),
**Promtail** (log shipper), and **Grafana** (dashboards and log explorer), all started
via Docker Compose profiles so they do not run unless explicitly requested.

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
