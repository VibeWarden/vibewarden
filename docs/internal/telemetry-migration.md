# Telemetry Migration — Internal Reference

> This file was relocated from `decisions/adr-014-prometheus-fallback-exporter-for-backward-compatibility.md`
> on 2026-05-04 as part of the ADR audit (audit report: `~/notes/vibewarden/audit-adr-2026-05-03.md`).
> The stub at that path remains stable; existing PR / commit references continue to resolve.
>
> **Note on supersession**: ADR-014 was classified SUPERSEDED-BY-ADR-012/ADR-015 chain in the
> audit. The Prometheus fallback adapter was retired when the OTel SDK migration completed. See
> `docs/observability.md` for the current telemetry architecture.

## From ADR-014 — Prometheus Fallback Exporter for Backward Compatibility

**Status**: Superseded by ADR-012 / ADR-015 — Prometheus fallback adapter retired when OTel SDK migration completed.

**Date**: 2026-03-28
**Issue**: #288

### Context

ADR-012 and ADR-013 established the OpenTelemetry SDK as the metrics foundation. This ADR
completed the migration by removing the deprecated `prometheus/client_golang` direct adapter.

### What was decided

- Delete `internal/adapters/metrics/prometheus.go` (replaced by `otel.go`)
- Delete `internal/adapters/metrics/prometheus_test.go` and integration tests
- Update all tests to use `OTelAdapter` backed by the OTel Prometheus bridge
- Add `NewTestProvider` helper in `internal/adapters/otel/testing.go`

### Current state (post-migration)

The Prometheus export path is now:

```
MetricsCollector interface
    -> OTelAdapter (internal/adapters/metrics/otel.go)
        -> OTel Meter instruments
            -> OTel MeterProvider
                -> Prometheus exporter (go.opentelemetry.io/otel/exporters/prometheus)
                    -> promhttp.Handler
                        -> /_vibewarden/metrics
```

The old `PrometheusAdapter -> prometheus.Registry -> promhttp.Handler` path is gone.
`prometheus/client_golang` remains as a transitive dependency of the OTel exporter.

### Fallback guarantee

With default config (no explicit `telemetry:` block):
- `prometheus.enabled = true` (default)
- `otlp.enabled = false` (default)
- Result: Prometheus-only export, same behaviour as pre-OTel migration

Users cannot accidentally end up with no metrics export unless they explicitly disable
both exporters.

### Key compatibility note (for dashboard authors)

Metric names and labels are unchanged from the pre-OTel era. Existing Prometheus scrapers
and dashboards continue working. The `locked decision` in CLAUDE.md ("Metrics:
prometheus/client_golang") refers to the export format and backward compatibility,
not the internal SDK.
