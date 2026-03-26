# Observability Stack

VibeWarden includes an optional local observability stack for development and testing.
It consists of **Prometheus** (metrics collection) and **Grafana** (dashboards), both
started via Docker Compose profiles so they do not run unless explicitly requested.

## Quick Start

Start the full stack including Prometheus and Grafana:

```bash
make observability-up
# equivalent to:
docker compose --profile observability up -d
```

Stop the stack:

```bash
make observability-down
# equivalent to:
docker compose --profile observability down
```

Open the dashboards in your browser:

```bash
make grafana-open      # opens http://localhost:3000
make prometheus-open   # opens http://localhost:9090
```

## Accessing the UIs

| Service    | URL                    | Notes                                  |
|------------|------------------------|----------------------------------------|
| Grafana    | http://localhost:3000  | Anonymous access, Admin role, no login |
| Prometheus | http://localhost:9090  | No authentication required             |

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

The dashboard JSON definition lives at
`observability/grafana/dashboards/vibewarden.json` and is loaded automatically by
the Grafana provisioning config in `observability/grafana/provisioning/`.

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
```

Prometheus scrapes VibeWarden every 15 seconds (configured in
`observability/prometheus/prometheus.yml`). Grafana queries Prometheus as its
data source (provisioned automatically in
`observability/grafana/provisioning/datasources/prometheus.yml`).

## Adding Custom Dashboards

1. Build your dashboard in the Grafana UI at http://localhost:3000.
2. Export it: **Dashboard menu → Share → Export → Save to file**.
3. Place the exported JSON file in `observability/grafana/dashboards/`.
4. Restart Grafana to pick up the new file:
   ```bash
   docker compose restart grafana
   ```

The provisioning config (`observability/grafana/provisioning/dashboards/dashboard.yml`)
watches the `observability/grafana/dashboards/` directory, so any `.json` file placed
there is loaded automatically on startup.

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

The dashboard is provisioned from
`observability/grafana/dashboards/vibewarden.json`. If the file is missing or
malformed, Grafana will start without it. Check the Grafana container logs:

```bash
docker compose logs grafana
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
