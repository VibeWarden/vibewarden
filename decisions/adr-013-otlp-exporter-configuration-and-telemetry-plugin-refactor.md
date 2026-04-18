# ADR-013: OTLP Exporter Configuration and Telemetry Plugin Refactor

**Date**: 2026-03-28
**Issue**: #287
**Status**: Accepted

### Context

ADR-012 introduced the OpenTelemetry SDK as the metrics foundation, using the Prometheus
exporter for pull-based metrics at `/_vibewarden/metrics`. Epic #280 (OpenTelemetry
Integration) requires push-based OTLP export as the primary telemetry path.

The issue is that the current architecture:

1. Only supports Prometheus pull-based export (scraping)
2. Requires opening inbound endpoints, conflicting with localhost-only security model
3. Has a `MetricsConfig` that is too narrow for the broader telemetry scope

OTLP export is push-based: the sidecar initiates outbound connections to send telemetry
to a collector endpoint. This aligns with VibeWarden's security model (localhost-only,
no inbound ports beyond the reverse proxy).

The acceptance criteria from issue #287:
- Refactor config to support both OTLP and Prometheus exporters
- Configure OTLP HTTP exporter with endpoint, headers, and interval
- Allow both exporters to run simultaneously
- Graceful shutdown with pending telemetry flush
- Backward compatibility: map legacy `metrics:` config to `telemetry:`

### Decision

Add OTLP HTTP exporter support to the OTelProvider and introduce a new `TelemetryConfig`
configuration section that replaces the narrow `MetricsConfig`. The system supports
running both Prometheus and OTLP exporters simultaneously.

#### Domain Model Changes

No domain model changes. Telemetry configuration is infrastructure concern.

#### Ports (Interfaces)

**Update `internal/ports/otel.go`** to add telemetry configuration types:

```go
// TelemetryConfig holds all telemetry export settings.
// It is passed to OTelProvider.Init to configure exporters.
type TelemetryConfig struct {
    // Prometheus enables the Prometheus pull-based exporter.
    // When enabled, metrics are available at /_vibewarden/metrics.
    Prometheus PrometheusExporterConfig

    // OTLP enables the OTLP push-based exporter.
    // When enabled, metrics are pushed to the configured endpoint.
    OTLP OTLPExporterConfig
}

// PrometheusExporterConfig configures the Prometheus pull-based exporter.
type PrometheusExporterConfig struct {
    // Enabled toggles the Prometheus exporter (default: true).
    Enabled bool
}

// OTLPExporterConfig configures the OTLP push-based exporter.
type OTLPExporterConfig struct {
    // Enabled toggles the OTLP exporter (default: false).
    Enabled bool

    // Endpoint is the OTLP HTTP endpoint URL (e.g., "http://localhost:4318").
    // Required when Enabled is true.
    Endpoint string

    // Headers are optional HTTP headers for authentication (e.g., API keys).
    // Keys are header names, values are header values.
    Headers map[string]string

    // Interval is the export interval (default: 30s).
    // Metrics are batched and pushed at this interval.
    Interval time.Duration

    // Protocol specifies the OTLP protocol: "http" or "grpc" (default: "http").
    // This story only implements "http"; "grpc" is reserved for future use.
    Protocol string
}
```

**Update `OTelProvider` interface:**

```go
// OTelProvider manages the OpenTelemetry SDK lifecycle.
// It initializes the MeterProvider with configured exporters and exposes
// an HTTP handler for Prometheus scraping (when Prometheus exporter is enabled).
// Implementations must be safe for concurrent use after Init returns.
type OTelProvider interface {
    // Init initializes the OTel SDK with the given service identity and telemetry config.
    // It sets up the MeterProvider with the configured exporters (Prometheus, OTLP, or both).
    // Must be called once before any other methods.
    Init(ctx context.Context, serviceName, serviceVersion string, cfg TelemetryConfig) error

    // Shutdown gracefully shuts down the OTel SDK, flushing any buffered data.
    // For OTLP exporter, this flushes pending metrics to the endpoint.
    // Must honour the context deadline.
    Shutdown(ctx context.Context) error

    // Handler returns an http.Handler that serves Prometheus metrics.
    // Returns nil if Prometheus exporter is disabled or Init has not been called.
    Handler() http.Handler

    // Meter returns a named OTel Meter for creating instruments.
    // The scope name is "github.com/vibewarden/vibewarden".
    Meter() Meter

    // PrometheusEnabled returns true if the Prometheus exporter is active.
    PrometheusEnabled() bool

    // OTLPEnabled returns true if the OTLP exporter is active.
    OTLPEnabled() bool
}
```

#### Adapters

**Update `internal/adapters/otel/provider.go`:**

```go
package otel

import (
    "context"
    "fmt"
    "net/http"
    "sync"
    "time"

    prometheusclient "github.com/prometheus/client_golang/prometheus"
    "github.com/prometheus/client_golang/prometheus/promhttp"
    "github.com/vibewarden/vibewarden/internal/ports"
    "go.opentelemetry.io/otel"
    otelprom "go.opentelemetry.io/otel/exporters/prometheus"
    "go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
    otelmetric "go.opentelemetry.io/otel/metric"
    sdkmetric "go.opentelemetry.io/otel/sdk/metric"
    "go.opentelemetry.io/otel/sdk/resource"
    semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

// Provider implements ports.OTelProvider using the OTel Go SDK.
// It supports both Prometheus and OTLP exporters, configured via Init.
type Provider struct {
    mu            sync.RWMutex
    meterProvider *sdkmetric.MeterProvider
    meter         otelmetric.Meter
    handler       http.Handler
    registry      *prometheusclient.Registry

    promEnabled bool
    otlpEnabled bool
}

// NewProvider creates an uninitialized Provider.
// Call Init before using any other methods.
func NewProvider() *Provider {
    return &Provider{}
}

// Init initializes the OTel SDK with configured exporters.
// serviceName and serviceVersion are recorded as OTel resource attributes.
// Returns an error if Init has already been called, if no exporters are enabled,
// or if OTLP is enabled without an endpoint.
func (p *Provider) Init(ctx context.Context, serviceName, serviceVersion string, cfg ports.TelemetryConfig) error {
    p.mu.Lock()
    defer p.mu.Unlock()

    if p.meterProvider != nil {
        return fmt.Errorf("otel provider already initialized")
    }

    // Validate config.
    if !cfg.Prometheus.Enabled && !cfg.OTLP.Enabled {
        return fmt.Errorf("at least one exporter must be enabled")
    }
    if cfg.OTLP.Enabled && cfg.OTLP.Endpoint == "" {
        return fmt.Errorf("OTLP endpoint required when OTLP exporter is enabled")
    }

    // Build resource with service identity.
    res, err := resource.New(ctx,
        resource.WithAttributes(
            semconv.ServiceName(serviceName),
            semconv.ServiceVersion(serviceVersion),
        ),
    )
    if err != nil {
        return fmt.Errorf("creating otel resource: %w", err)
    }

    // Collect readers for each enabled exporter.
    var readers []sdkmetric.Option

    // Prometheus exporter (pull-based).
    if cfg.Prometheus.Enabled {
        p.registry = prometheusclient.NewRegistry()
        promExporter, err := otelprom.New(
            otelprom.WithRegisterer(p.registry),
            otelprom.WithoutScopeInfo(),
        )
        if err != nil {
            return fmt.Errorf("creating prometheus exporter: %w", err)
        }
        readers = append(readers, sdkmetric.WithReader(promExporter))
        p.handler = promhttp.HandlerFor(p.registry, promhttp.HandlerOpts{
            EnableOpenMetrics: true,
        })
        p.promEnabled = true
    }

    // OTLP HTTP exporter (push-based).
    if cfg.OTLP.Enabled {
        interval := cfg.OTLP.Interval
        if interval == 0 {
            interval = 30 * time.Second
        }

        // Build OTLP HTTP exporter options.
        otlpOpts := []otlpmetrichttp.Option{
            otlpmetrichttp.WithEndpointURL(cfg.OTLP.Endpoint),
        }
        if len(cfg.OTLP.Headers) > 0 {
            otlpOpts = append(otlpOpts, otlpmetrichttp.WithHeaders(cfg.OTLP.Headers))
        }

        otlpExporter, err := otlpmetrichttp.New(ctx, otlpOpts...)
        if err != nil {
            return fmt.Errorf("creating otlp exporter: %w", err)
        }

        // Periodic reader pushes metrics at the configured interval.
        periodicReader := sdkmetric.NewPeriodicReader(otlpExporter,
            sdkmetric.WithInterval(interval),
        )
        readers = append(readers, sdkmetric.WithReader(periodicReader))
        p.otlpEnabled = true
    }

    // Create MeterProvider with all configured readers.
    opts := []sdkmetric.Option{sdkmetric.WithResource(res)}
    opts = append(opts, readers...)
    p.meterProvider = sdkmetric.NewMeterProvider(opts...)

    // Set as global provider.
    otel.SetMeterProvider(p.meterProvider)

    // Create the application meter.
    p.meter = p.meterProvider.Meter("github.com/vibewarden/vibewarden")

    return nil
}

// Shutdown gracefully shuts down the MeterProvider, flushing any buffered data.
// For OTLP exporter, this ensures pending metrics are pushed to the endpoint.
func (p *Provider) Shutdown(ctx context.Context) error {
    p.mu.Lock()
    defer p.mu.Unlock()

    if p.meterProvider == nil {
        return nil
    }
    return p.meterProvider.Shutdown(ctx)
}

// Handler returns the Prometheus metrics HTTP handler.
// Returns nil if Prometheus exporter is disabled or Init has not been called.
func (p *Provider) Handler() http.Handler {
    p.mu.RLock()
    defer p.mu.RUnlock()
    return p.handler
}

// Meter returns a ports.Meter wrapping the OTel SDK meter.
// Returns nil if Init has not been called.
func (p *Provider) Meter() ports.Meter {
    p.mu.RLock()
    defer p.mu.RUnlock()
    if p.meter == nil {
        return nil
    }
    return &meterAdapter{m: p.meter}
}

// PrometheusEnabled returns true if the Prometheus exporter is active.
func (p *Provider) PrometheusEnabled() bool {
    p.mu.RLock()
    defer p.mu.RUnlock()
    return p.promEnabled
}

// OTLPEnabled returns true if the OTLP exporter is active.
func (p *Provider) OTLPEnabled() bool {
    p.mu.RLock()
    defer p.mu.RUnlock()
    return p.otlpEnabled
}
```

#### Application Service

**Update `internal/config/config.go`:**

Add new `TelemetryConfig` struct and deprecate `MetricsConfig`:

```go
// TelemetryConfig holds all telemetry export settings.
// This replaces the narrower MetricsConfig and supports both pull (Prometheus)
// and push (OTLP) export modes.
type TelemetryConfig struct {
    // Enabled toggles telemetry collection entirely (default: true).
    Enabled bool `mapstructure:"enabled"`

    // PathPatterns is a list of URL path normalization patterns using :param syntax.
    // Example: "/users/:id", "/api/v1/items/:item_id/comments/:comment_id"
    // Paths that don't match any pattern are recorded as "other".
    PathPatterns []string `mapstructure:"path_patterns"`

    // Prometheus configures the pull-based Prometheus exporter.
    Prometheus PrometheusExporterConfig `mapstructure:"prometheus"`

    // OTLP configures the push-based OTLP exporter.
    OTLP OTLPExporterConfig `mapstructure:"otlp"`
}

// PrometheusExporterConfig configures the Prometheus pull-based exporter.
type PrometheusExporterConfig struct {
    // Enabled toggles the Prometheus exporter (default: true).
    // When enabled, metrics are served at /_vibewarden/metrics.
    Enabled bool `mapstructure:"enabled"`
}

// OTLPExporterConfig configures the OTLP push-based exporter.
type OTLPExporterConfig struct {
    // Enabled toggles the OTLP exporter (default: false).
    Enabled bool `mapstructure:"enabled"`

    // Endpoint is the OTLP HTTP endpoint URL (e.g., "http://localhost:4318").
    // Required when Enabled is true.
    Endpoint string `mapstructure:"endpoint"`

    // Headers are optional HTTP headers for authentication.
    // Example: {"Authorization": "Bearer <token>"}
    Headers map[string]string `mapstructure:"headers"`

    // Interval is the export interval as a duration string (default: "30s").
    // Metrics are batched and pushed at this interval.
    Interval string `mapstructure:"interval"`

    // Protocol is "http" or "grpc" (default: "http").
    // Only "http" is supported in this version.
    Protocol string `mapstructure:"protocol"`
}

// MetricsConfig is DEPRECATED. Use TelemetryConfig instead.
// This struct remains for backward compatibility and is mapped to TelemetryConfig
// during config loading.
type MetricsConfig struct {
    // Enabled toggles metrics collection and the /_vibewarden/metrics endpoint (default: true).
    Enabled bool `mapstructure:"enabled"`

    // PathPatterns is a list of URL path normalization patterns using :param syntax.
    PathPatterns []string `mapstructure:"path_patterns"`
}
```

**Add to `Load()` function:**

```go
// Defaults for telemetry.
v.SetDefault("telemetry.enabled", true)
v.SetDefault("telemetry.prometheus.enabled", true)
v.SetDefault("telemetry.otlp.enabled", false)
v.SetDefault("telemetry.otlp.interval", "30s")
v.SetDefault("telemetry.otlp.protocol", "http")
```

**Add migration helper in `internal/config/migrate.go`:**

```go
// MigrateLegacyMetrics converts legacy metrics config to telemetry config.
// If the user has a metrics: section but no telemetry: section, this function
// copies settings and logs a deprecation warning.
func MigrateLegacyMetrics(cfg *Config, logger *slog.Logger) {
    // Only migrate if telemetry is at defaults and metrics is customized.
    if cfg.Metrics.Enabled == false || len(cfg.Metrics.PathPatterns) > 0 {
        // User has customized metrics config, migrate it.
        cfg.Telemetry.Enabled = cfg.Metrics.Enabled
        cfg.Telemetry.PathPatterns = cfg.Metrics.PathPatterns
        cfg.Telemetry.Prometheus.Enabled = cfg.Metrics.Enabled

        logger.Warn("DEPRECATED: 'metrics:' config section is deprecated, use 'telemetry:' instead",
            slog.Bool("metrics_enabled", cfg.Metrics.Enabled),
            slog.Int("path_patterns", len(cfg.Metrics.PathPatterns)),
        )
    }
}
```

#### File Layout

**New files:**

```
internal/
  config/
    migrate.go                 # Legacy config migration helpers
    migrate_test.go            # Tests for migration
  adapters/
    otel/
      otlp.go                  # OTLP exporter helpers (optional, if extraction needed)
      otlp_test.go             # OTLP-specific unit tests
```

**Modified files:**

```
internal/
  ports/
    otel.go                    # Add TelemetryConfig, update OTelProvider interface
  config/
    config.go                  # Add TelemetryConfig, deprecate MetricsConfig
  adapters/
    otel/
      provider.go              # Add OTLP exporter support, update Init signature
      provider_test.go         # Add tests for OTLP exporter, dual-exporter mode
  plugins/
    metrics/
      plugin.go                # Update to use TelemetryConfig, pass to provider
      config.go                # Update Config struct to use TelemetryConfig fields
      plugin_test.go           # Update tests
```

#### Sequence

**Initialization flow (updated):**

1. `main()` loads config with `config.Load()`
2. `config.MigrateLegacyMetrics()` checks for deprecated `metrics:` section
3. If `metrics:` found but no `telemetry:`, copy settings and log warning
4. Plugin registry creates metrics plugin with `metrics.New(cfg, logger)`
5. Plugin registry calls `plugin.Init(ctx)`:
   a. Build `ports.TelemetryConfig` from config
   b. Create `oteladapter.Provider`
   c. Call `provider.Init(ctx, "vibewarden", version, telemetryCfg)`:
      - Validate: at least one exporter enabled
      - Validate: OTLP endpoint present if OTLP enabled
      - Create OTel Resource with service name/version
      - **If Prometheus enabled:**
        - Create Prometheus registry
        - Create Prometheus exporter with registry
        - Add to readers list
        - Create promhttp.Handler
      - **If OTLP enabled:**
        - Parse interval duration
        - Build OTLP HTTP exporter with endpoint, headers
        - Create PeriodicReader with exporter and interval
        - Add to readers list
      - Create MeterProvider with all readers
      - Set global MeterProvider
      - Create Meter for scope
   d. Create `metricsadapter.OTelAdapter(provider, pathPatterns)`
6. Plugin registry calls `plugin.Start(ctx)`:
   a. **If Prometheus enabled:**
      - Create internal HTTP server with adapter.Handler()
      - Server binds random localhost port
      - Store internal address for Caddy reverse-proxy
   b. **If only OTLP enabled:**
      - No internal server needed (push-based)
      - Plugin still contributes no Caddy routes

**OTLP push flow:**

1. HTTP request arrives at Caddy
2. `MetricsMiddleware` intercepts, records start time
3. Request proceeds through handler chain
4. On response, middleware calls MetricsCollector methods
5. OTelAdapter forwards to OTel instruments with attributes
6. OTel SDK aggregates observations in memory
7. **PeriodicReader** (every 30s by default):
   - Collects aggregated metrics from MeterProvider
   - Pushes to OTLP endpoint via HTTP POST
   - Endpoint returns 200 OK on success
8. On shutdown, `provider.Shutdown()` forces final flush

**Shutdown flow (updated):**

1. Plugin registry calls `plugin.Stop(ctx)`
2. **If Prometheus enabled:** Plugin stops internal HTTP server
3. Plugin calls `provider.Shutdown(ctx)`
4. MeterProvider triggers final export on all readers:
   - Prometheus exporter: no-op (pull-based)
   - OTLP exporter: flush pending metrics to endpoint
5. Wait for flush or context deadline
6. Exporters released

#### Error Cases

| Error | Cause | Handling |
|-------|-------|----------|
| `at least one exporter must be enabled` | Both Prometheus and OTLP disabled | Return error from Init |
| `OTLP endpoint required when OTLP exporter is enabled` | OTLP enabled but endpoint empty | Return error from Init |
| `creating otlp exporter` | Invalid endpoint URL | Return error from Init |
| `invalid interval duration` | Malformed interval string | Return error during config parsing |
| OTLP push fails (network error) | Collector unreachable | OTel SDK retries with backoff, logs warning |
| OTLP push fails (auth error) | Invalid headers/API key | OTel SDK logs error, continues trying |
| Shutdown timeout | Flush takes too long | Context deadline exceeded, may lose pending data |
| `unsupported protocol: grpc` | Protocol set to grpc | Return error from Init (grpc not implemented) |

**OTLP error handling philosophy:**

The OTel SDK handles transient OTLP export failures gracefully:
- Automatic retry with exponential backoff
- Logs export failures but does not crash
- Continues collecting metrics locally
- Next push interval attempts again

This matches the sidecar's resilience requirements: telemetry loss is acceptable,
crashes are not.

#### Test Strategy

**Unit tests:**

| File | Coverage |
|------|----------|
| `internal/adapters/otel/provider_test.go` | Init with various TelemetryConfig combinations |
| `internal/adapters/otel/otlp_test.go` | OTLP exporter creation, option translation |
| `internal/config/config_test.go` | TelemetryConfig parsing, defaults |
| `internal/config/migrate_test.go` | Legacy metrics config migration |
| `internal/plugins/metrics/plugin_test.go` | Updated plugin lifecycle with TelemetryConfig |

**Unit test cases for provider:**

1. Init with Prometheus only (current behavior, regression test)
2. Init with OTLP only (new behavior)
3. Init with both Prometheus and OTLP (dual-exporter)
4. Init with neither (error case)
5. Init with OTLP enabled but no endpoint (error case)
6. Init with custom OTLP headers
7. Init with custom OTLP interval
8. Shutdown flushes OTLP (verify call to exporter.Shutdown)
9. PrometheusEnabled/OTLPEnabled return correct values

**Integration tests:**

| Test | Coverage |
|------|----------|
| `internal/adapters/otel/provider_integration_test.go` | Full OTLP export to mock server |

**Integration test approach for OTLP:**

1. Start mock OTLP HTTP server (net/http/httptest)
2. Configure provider with mock server endpoint
3. Record metrics through adapter
4. Trigger manual flush via shutdown or short interval
5. Verify mock server received expected OTLP payload
6. Verify metric names, labels, values in payload

**What to mock vs. what to test real:**

- Mock: OTLP collector endpoint (httptest server)
- Real: Full OTel SDK stack, Prometheus exporter
- Skip: Real OTel Collector (tested in Docker Compose story #290)

#### New Dependencies

| Package | Version | License | Reason |
|---------|---------|---------|--------|
| `go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp` | v1.40.0 | Apache 2.0 | OTLP HTTP exporter for push-based metrics |

**License verification:** This package is part of the `opentelemetry-go` repository,
which is licensed under Apache 2.0 (verified in ADR-012). The package is already
a transitive dependency through Caddy (line 185 of go.mod), so promoting it to
a direct dependency does not increase binary size.

**No new transitive dependencies** are introduced; all required packages are
already in the dependency tree.

### Consequences

**Positive:**

- **Push-based export:** OTLP enables outbound-only telemetry, aligning with
  localhost-only security model. No inbound scrape ports needed.
- **Dual-mode support:** Users can run Prometheus (for local /metrics) AND OTLP
  (for central collection) simultaneously. Gradual migration path.
- **Fleet-ready:** Future Pro tier fleet dashboard can receive OTLP directly
  from local instances without scraping.
- **Vendor-neutral:** OTLP is CNCF standard; works with any OTLP-compatible backend
  (Grafana Cloud, Datadog, Honeycomb, self-hosted OTel Collector).
- **Backward compatible:** Legacy `metrics:` config continues to work with
  deprecation warning. No breaking changes for existing users.

**Negative:**

- **Configuration complexity:** TelemetryConfig has more options than MetricsConfig.
  Mitigated by sensible defaults (Prometheus enabled, OTLP disabled).
- **Network dependency:** OTLP requires network access to collector. If collector
  is down, metrics are lost after SDK buffer fills. Acceptable for observability.
- **No gRPC support yet:** Only HTTP protocol implemented. gRPC requires additional
  dependency (`otlpmetricgrpc`). Can be added in follow-up if needed.

**Trade-offs:**

- **Immediate flush vs. batching:** Chose batching with PeriodicReader (default 30s)
  for efficiency. Trade-off: up to 30s telemetry lag. Users can configure shorter
  intervals if needed.
- **Prometheus as default:** Kept Prometheus enabled by default for backward
  compatibility. New users may prefer OTLP-only, but this requires explicit opt-in.
- **Single OTLP endpoint:** No support for multiple OTLP endpoints. Users needing
  fan-out should use OTel Collector as aggregator.

**Migration path:**

1. **This story (ADR-013):** Add OTLP support, TelemetryConfig, deprecate MetricsConfig
2. **Story #288:** Prometheus fallback (ensure Prometheus still works as expected)
3. **Story #290:** OTel Collector in Docker Compose (OTLP receiver)
4. **Future:** Remove deprecated MetricsConfig after 2 minor versions

**Example `vibewarden.yaml` configurations:**

```yaml
# Prometheus only (current default, backward compatible)
telemetry:
  enabled: true
  prometheus:
    enabled: true
  otlp:
    enabled: false

# OTLP only (push to Grafana Cloud)
telemetry:
  enabled: true
  prometheus:
    enabled: false
  otlp:
    enabled: true
    endpoint: https://otlp-gateway-prod-us-central-0.grafana.net/otlp
    headers:
      Authorization: "Basic ${GRAFANA_OTLP_TOKEN}"
    interval: 30s

# Dual-mode (local scraping + central push)
telemetry:
  enabled: true
  path_patterns:
    - "/users/:id"
    - "/api/v1/items/:item_id"
  prometheus:
    enabled: true
  otlp:
    enabled: true
    endpoint: http://otel-collector:4318
    interval: 15s

# Legacy config (will be migrated with warning)
metrics:
  enabled: true
  path_patterns:
    - "/users/:id"
```

---
