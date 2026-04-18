# ADR-017: Update Grafana Dashboards for OTel-Sourced Metrics

**Date**: 2026-03-28
**Issue**: #291
**Status**: Accepted

### Context

Epic #280 ("Switch telemetry from Prometheus to OpenTelemetry") transitions VibeWarden from
pull-based Prometheus scraping to push-based OTLP. Previous stories in this epic added:

- ADR-012: OTel SDK integration and MetricsCollector port/adapter refactoring
- ADR-016: OTel Collector in Docker Compose observability stack

The existing Grafana dashboard (`vibewarden-dashboard.json`) has PromQL and LogQL queries
designed for the original telemetry path:

1. **Metrics**: Prometheus scraped VibeWarden directly at `/_vibewarden/metrics`
2. **Logs**: Promtail scraped Docker container logs with label `container="vibewarden"`

With the new OTel pipeline:

1. **Metrics**: VibeWarden pushes OTLP to OTel Collector, which exports to Prometheus
2. **Logs**: VibeWarden pushes OTLP logs to OTel Collector, which exports to Loki

**Issue #1: Double namespace prefix**

The OTel Collector config (`otel-collector-config.yml.tmpl`) has:

```yaml
exporters:
  prometheus:
    namespace: vibewarden
```

Meanwhile, the OTel adapter (`internal/adapters/metrics/otel.go`) already names metrics with
the `vibewarden_` prefix:

- `vibewarden_requests_total`
- `vibewarden_request_duration_seconds`
- `vibewarden_rate_limit_hits_total`
- etc.

The combination produces double-prefixed metric names like `vibewarden_vibewarden_requests_total`,
which breaks all dashboard PromQL queries.

**Issue #2: Loki label changes**

The dashboard LogQL queries use `{container="vibewarden"}`:

```
{container="vibewarden"} | json
{container="vibewarden"} | json | event_type =~ "auth.*"
```

This relies on Promtail's automatic `container` label from Docker. With OTel Collector's Loki
exporter, logs arrive with different labels. Per ADR-016, the Loki exporter is configured to
use `service.name` resource attribute mapped to `service` label:

```yaml
exporters:
  loki:
    labels:
      resource:
        service.name: "service"
```

Logs from VibeWarden will have `{service="vibewarden"}`, not `{container="vibewarden"}`.

**Issue #3: OTel scope labels**

The OTel Prometheus exporter may add `otel_scope_name` and `otel_scope_version` labels to
metrics. Dashboard queries should be resilient to these additional labels.

### Decision

Fix the OTel Collector configuration and update the Grafana dashboard to work with
OTel-sourced metrics and logs. This is a configuration-only change (no Go code changes).

#### Domain Model Changes

None. This story is pure infrastructure (config templates and dashboard JSON).

#### Ports (Interfaces)

None. No Go code changes.

#### Adapters

None. No Go code changes.

#### Application Service

None. No Go code changes.

#### File Layout

**Modified files:**

| File | Changes |
|------|---------|
| `internal/config/templates/observability/otel-collector-config.yml.tmpl` | Remove `namespace: vibewarden` from Prometheus exporter |
| `internal/config/templates/observability/vibewarden-dashboard.json` | Update Loki queries from `container="vibewarden"` to `service="vibewarden"` |

No new files.

#### Changes

**1. Fix OTel Collector Prometheus exporter (remove double prefix)**

In `internal/config/templates/observability/otel-collector-config.yml.tmpl`, change:

```yaml
exporters:
  prometheus:
    endpoint: 0.0.0.0:8889
    namespace: vibewarden  # REMOVE THIS LINE
    const_labels:
      source: otel_collector
```

To:

```yaml
exporters:
  prometheus:
    endpoint: 0.0.0.0:8889
    const_labels:
      source: otel_collector
```

**Why:** The OTel adapter already names metrics with `vibewarden_` prefix. The Prometheus
exporter's `namespace` option prepends another prefix, resulting in `vibewarden_vibewarden_*`.
Removing the namespace preserves the original metric names that the dashboard expects.

**2. Update Loki queries in dashboard**

In `internal/config/templates/observability/vibewarden-dashboard.json`, update all Loki
queries from `{container="vibewarden"}` to `{service="vibewarden"}`:

| Panel ID | Panel Title | Old Query | New Query |
|----------|-------------|-----------|-----------|
| 20 | Log Stream | `{container="vibewarden"} \| json` | `{service="vibewarden"} \| json` |
| 21 | Auth Events | `{container="vibewarden"} \| json \| event_type =~ "auth.*"` | `{service="vibewarden"} \| json \| event_type =~ "auth.*"` |
| 22 | Rate Limit Events | `{container="vibewarden"} \| json \| event_type =~ "rate_limit.*"` | `{service="vibewarden"} \| json \| event_type =~ "rate_limit.*"` |
| 23 | Security Events | `{container="vibewarden"} \| json \| event_type =~ "security.*" or ...` | `{service="vibewarden"} \| json \| event_type =~ "security.*" or ...` |

**Why:** OTel Collector's Loki exporter uses the `service.name` resource attribute (mapped to
`service` label) instead of Docker's `container` label. VibeWarden sets `service.name` to
`"vibewarden"` in the OTel provider initialization.

**3. PromQL queries remain unchanged**

The existing PromQL queries do not need changes:

- `sum(rate(vibewarden_requests_total[5m])) by (status_code)` — works as-is
- `histogram_quantile(0.50, sum(rate(vibewarden_request_duration_seconds_bucket[5m])) by (le))` — works as-is
- etc.

The OTel Prometheus exporter may add `otel_scope_name` and `otel_scope_version` labels, but
these are stripped by the `sum(...) by (label)` aggregations in all queries. No changes needed.

#### Sequence

No runtime sequence changes. This is a configuration fix.

**Verification sequence:**

1. Run `vibewarden generate` to regenerate observability configs
2. Start the observability stack: `docker compose --profile observability up -d`
3. Send traffic through the sidecar to generate metrics and logs
4. Open Grafana at `http://localhost:3000`
5. Navigate to the VibeWarden dashboard
6. Verify all 8 metric panels render data (panels 1-7)
7. Verify all 4 log panels show logs (panels 20-23)

#### Error Cases

| Error | When | Handling |
|-------|------|----------|
| Metrics missing in Grafana | OTel Collector not running or unhealthy | Check `docker compose ps` for collector health |
| Logs missing in Grafana | Loki exporter misconfigured | Check collector logs for Loki export errors |
| No data in dashboard | Sidecar not sending OTLP | Verify `VIBEWARDEN_TELEMETRY_OTLP_ENABLED=true` |

#### Test Strategy

**Unit tests:**

None. Config file changes; no Go code.

**Integration tests (manual or CI):**

| Test | Verification |
|------|--------------|
| Start observability stack | `docker compose --profile observability up -d` succeeds |
| Send requests | `curl http://localhost:8080/health` generates metrics |
| Check Prometheus | Query `vibewarden_requests_total` returns data (not `vibewarden_vibewarden_requests_total`) |
| Check Grafana metrics | All 7 metric panels (1-7) render charts with data |
| Check Grafana logs | All 4 log panels (20-23) show log entries |

**What to mock vs. real:**

- Real: Full observability stack (Docker Compose)
- Mock: None (end-to-end verification required)

#### New Dependencies

None. No new Go dependencies or Docker images.

### Consequences

**Positive:**

- **Dashboard works with OTel pipeline:** All panels render correctly after the fix
- **No metric name changes:** Preserves original metric names for backward compatibility
- **Clean label scheme:** Logs use semantic `service` label instead of Docker-specific `container`
- **No code changes:** Pure configuration fix, minimal risk

**Negative:**

- **Promtail logs incompatible:** If users also run Promtail for VibeWarden logs (not recommended),
  they will see `container="vibewarden"` labels while OTel logs have `service="vibewarden"`.
  This is acceptable because ADR-016 established that VibeWarden logs should flow via OTLP,
  not Promtail.

**Trade-offs:**

- **Dual label compatibility vs. clean break:** Could have made Loki queries match both
  `{container="vibewarden"} or {service="vibewarden"}`, but this adds complexity and the
  Promtail path is deprecated for VibeWarden logs. Clean break is simpler.

---
