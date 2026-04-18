# ADR-014: Prometheus Fallback Exporter for Backward Compatibility

**Date**: 2026-03-28
**Issue**: #288
**Status**: Accepted

### Context

ADR-012 and ADR-013 established the OpenTelemetry SDK as the metrics foundation, with both
Prometheus (pull-based) and OTLP (push-based) export capabilities. The implementation uses
`go.opentelemetry.io/otel/exporters/prometheus` as the OTel-to-Prometheus bridge.

Issue #288 requires completing this migration by:

1. Removing the deprecated `prometheus/client_golang` direct adapter
2. Ensuring `/_vibewarden/metrics` continues to work as before
3. Validating that Prometheus is the automatic fallback when OTLP is not configured
4. Verifying metric names and labels remain identical (no breaking changes for dashboards)

**Current state after ADR-012/013:**

- OTel provider at `internal/adapters/otel/provider.go` already supports Prometheus export
- Metrics plugin already uses `OTelAdapter` for metric collection
- Config defaults: `prometheus.enabled = true`, `otlp.enabled = false`
- Legacy `prometheus.go` adapter still exists but is unused by the plugin

The old `PrometheusAdapter` in `internal/adapters/metrics/prometheus.go` is now dead code.
Some tests still reference it, creating maintenance burden and potential confusion.

### Decision

Complete the OTel migration by removing deprecated prometheus/client_golang adapter code
and updating all tests to use the OTel-based implementation. Add explicit integration tests
verifying backward compatibility of the `/_vibewarden/metrics` endpoint.

#### Domain Model Changes

None. This is a cleanup story with no domain impact.

#### Ports (Interfaces)

No changes. The existing `ports.MetricsCollector` and `ports.OTelProvider` interfaces
remain stable.

#### Adapters

**Files to delete:**

```
internal/adapters/metrics/prometheus.go           # REMOVE: replaced by otel.go
internal/adapters/metrics/prometheus_test.go      # REMOVE: replaced by otel_test.go
internal/adapters/metrics/prometheus_integration_test.go  # REMOVE: consolidate into otel tests
```

**Files to update:**

```
internal/adapters/metrics/server_test.go          # UPDATE: use OTelAdapter instead of PrometheusAdapter
internal/adapters/caddy/metrics_integration_test.go  # UPDATE: use OTelAdapter
```

#### Application Service

No changes to application services.

#### File Layout

**Files to delete:**

| File | Reason |
|------|--------|
| `internal/adapters/metrics/prometheus.go` | Replaced by `otel.go` |
| `internal/adapters/metrics/prometheus_test.go` | Replaced by `otel_test.go` |
| `internal/adapters/metrics/prometheus_integration_test.go` | Tests migrated to `otel_integration_test.go` |

**Files to modify:**

| File | Changes |
|------|---------|
| `internal/adapters/metrics/server_test.go` | Replace `NewPrometheusAdapter` with `NewOTelAdapter` using test provider |
| `internal/adapters/caddy/metrics_integration_test.go` | Replace `PrometheusAdapter` with `OTelAdapter` |
| `internal/adapters/otel/provider.go` | Add doc comment clarifying fallback behavior |
| `internal/config/config.go` | Add doc comment about Prometheus as default fallback |

**Files unchanged:**

| File | Status |
|------|--------|
| `internal/adapters/metrics/otel.go` | Already implements MetricsCollector via OTel |
| `internal/adapters/metrics/otel_test.go` | Already tests OTelAdapter |
| `internal/adapters/metrics/otel_integration_test.go` | Already tests HTTP scrape |
| `internal/adapters/metrics/noop.go` | Unchanged |
| `internal/adapters/metrics/path_matcher.go` | Unchanged, used by OTelAdapter |
| `internal/adapters/metrics/server.go` | Unchanged |
| `internal/plugins/metrics/plugin.go` | Already uses OTelAdapter |

#### Sequence

This story does not change any runtime sequences. The flows established in ADR-012
and ADR-013 remain unchanged:

1. On startup, metrics plugin builds `TelemetryConfig` from config
2. If `prometheus.enabled = true` (default), OTelProvider creates Prometheus exporter
3. If `otlp.enabled = true` AND endpoint provided, OTelProvider also creates OTLP exporter
4. If neither enabled, Init returns error "at least one exporter must be enabled"
5. Internal HTTP server serves `/metrics` via OTel Prometheus handler
6. Caddy reverse-proxies `/_vibewarden/metrics` to internal server

**Fallback behavior (clarified):**

With default config (no explicit `telemetry:` block):
- `prometheus.enabled = true` (default)
- `otlp.enabled = false` (default)
- Result: Prometheus-only export, same behavior as pre-OTel migration

This ensures zero-config backward compatibility.

#### Error Cases

No new error cases. Existing validation:

| Error | When | Handling |
|-------|------|----------|
| `at least one exporter must be enabled` | Both exporters explicitly disabled | Error from provider.Init |
| Invalid OTLP endpoint | OTLP enabled, bad URL | Error from provider.Init |

The key guarantee: users cannot accidentally end up with no metrics export unless
they explicitly disable both exporters, which is a conscious choice.

#### Test Strategy

**Unit tests to update:**

| File | Changes |
|------|---------|
| `internal/adapters/metrics/server_test.go` | Create test OTelProvider, pass to NewOTelAdapter |

The server tests need a handler; currently they use `NewPrometheusAdapter(nil).Handler()`.
After this change, they will use an OTel-backed handler via a test helper.

**Test helper to add in `internal/adapters/otel/testing.go`:**

```go
// NewTestProvider creates an OTelProvider with Prometheus enabled for testing.
// It initializes the provider and returns it ready for use.
func NewTestProvider(ctx context.Context) (*Provider, error) {
    p := NewProvider()
    cfg := ports.TelemetryConfig{
        Prometheus: ports.PrometheusExporterConfig{Enabled: true},
        OTLP:       ports.OTLPExporterConfig{Enabled: false},
    }
    if err := p.Init(ctx, "vibewarden-test", "0.0.0-test", cfg); err != nil {
        return nil, err
    }
    return p, nil
}
```

**Integration tests to update:**

| File | Changes |
|------|---------|
| `internal/adapters/caddy/metrics_integration_test.go` | Use OTelAdapter with test provider |

**Integration tests to verify (already exist in otel_integration_test.go):**

1. `/_vibewarden/metrics` returns HTTP 200
2. Response is valid Prometheus text format
3. Response contains expected metric names: `vibewarden_requests_total`, `vibewarden_request_duration_seconds`, etc.
4. Metric labels match expected format (method, status_code, path_pattern, etc.)
5. Go runtime metrics present (go_goroutines, go_memstats_*, etc.)

**New test case to add in otel_integration_test.go:**

```go
func TestOTelAdapter_MetricNamesMatchLegacyPrometheus(t *testing.T) {
    // Verify that all metric names exported via OTel Prometheus bridge
    // match the names that were exported by the direct Prometheus adapter.
    // This ensures dashboard compatibility.
    expectedMetrics := []string{
        "vibewarden_requests_total",
        "vibewarden_request_duration_seconds",
        "vibewarden_rate_limit_hits_total",
        "vibewarden_auth_decisions_total",
        "vibewarden_upstream_errors_total",
        "vibewarden_active_connections",
    }
    // ... scrape /_vibewarden/metrics and verify all expected metrics present
}
```

**What to mock vs. what to test real:**

- Real: Full OTel SDK, Prometheus exporter, HTTP scraping
- Mock: Nothing at unit level (OTel SDK is fast and deterministic)

#### New Dependencies

None. All required dependencies are already present:

| Package | Status | License |
|---------|--------|---------|
| `go.opentelemetry.io/otel/exporters/prometheus` | Already in go.mod | Apache 2.0 |
| `prometheus/client_golang` | Remains as transitive dep of OTel exporter | Apache 2.0 |

Note: `prometheus/client_golang` will remain in go.mod as a transitive dependency
of the OTel Prometheus exporter. We are removing direct usage, not the dependency itself.

### Consequences

**Positive:**

- **Cleaner codebase:** Remove ~300 lines of dead code (prometheus.go + tests)
- **Single path:** All metrics flow through OTel SDK, simplifying debugging
- **Consistent testing:** All tests use the same adapter type
- **Dashboard compatibility:** Metric names and labels unchanged
- **Fallback guaranteed:** Default config always enables Prometheus

**Negative:**

- **Test churn:** Several test files need updates (one-time cost)
- **Transitive dependency:** `prometheus/client_golang` remains in dependency tree
  via OTel exporter (unavoidable; OTel bridge requires it)

**Trade-offs:**

- **Keep vs. delete prometheus.go:** Chose deletion. Keeping dead code creates
  maintenance burden and confusion. The OTel bridge provides identical functionality.
- **Test helper vs. inline setup:** Chose helper (`NewTestProvider`) for DRY tests
  and clearer intent. Trade-off: one more file to maintain.

**Migration complete:**

After this story, the Prometheus export path is:

```
MetricsCollector interface
    -> OTelAdapter (internal/adapters/metrics/otel.go)
        -> OTel Meter instruments
            -> OTel MeterProvider
                -> Prometheus exporter (go.opentelemetry.io/otel/exporters/prometheus)
                    -> promhttp.Handler
                        -> /_vibewarden/metrics
```

The old path (`PrometheusAdapter -> prometheus.Registry -> promhttp.Handler`) is removed.

**Locked decision impact:**

CLAUDE.md line 33 states: "Metrics: prometheus/client_golang (Apache 2.0)"

This locked decision refers to the export format and backward compatibility, not the
internal SDK. The change is compliant because:

1. `/_vibewarden/metrics` still serves Prometheus format
2. `prometheus/client_golang` remains in the dependency tree (via OTel bridge)
3. Existing Prometheus scrapers and dashboards continue working

A future ADR may update the locked decision text to "Metrics: OpenTelemetry SDK
with Prometheus export" for accuracy, but this is documentation, not a breaking change.

---
