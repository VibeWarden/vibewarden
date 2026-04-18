# ADR-012: OTel SDK Integration and MetricsCollector Port/Adapter Refactoring

**Date**: 2026-03-28
**Issue**: #285
**Status**: Accepted

### Context

VibeWarden currently uses `prometheus/client_golang` directly for metrics collection (locked
decision L-07). The existing `MetricsCollector` port and `PrometheusAdapter` implementation
work well, but they are tightly coupled to Prometheus-specific types.

Epic #280 (OpenTelemetry Integration) requires VibeWarden to adopt the OpenTelemetry SDK as
the unified observability foundation. This enables:

1. **OTLP export** — Push metrics to any OTLP-compatible backend (future story #286)
2. **Unified SDK** — Single initialization path for metrics, traces, and logs
3. **Prometheus bridge** — Continue serving `/_vibewarden/metrics` via OTel's Prometheus exporter
4. **Fleet integration** — Future Pro tier can receive OTel-formatted telemetry

The OTel Go SDK packages are already transitive dependencies through Caddy:
- `go.opentelemetry.io/otel` v1.41.0
- `go.opentelemetry.io/otel/metric` v1.41.0
- `go.opentelemetry.io/otel/sdk` v1.41.0
- `go.opentelemetry.io/otel/sdk/metric` v1.41.0
- `go.opentelemetry.io/otel/exporters/prometheus` v0.62.0

All are licensed under **Apache 2.0** (verified), which is on the approved list.

This ADR covers the foundation story: OTel SDK initialization, updated port interface,
and the new OTel adapter that replaces the direct Prometheus implementation.

### Decision

Refactor the metrics subsystem to use OpenTelemetry SDK as the metrics API while
maintaining Prometheus export compatibility. The `MetricsCollector` port interface
remains stable; only the adapter implementation changes.

#### Domain Model Changes

No domain model changes. Metrics are infrastructure concerns, not domain entities.

#### Ports (Interfaces)

The existing `ports.MetricsCollector` interface **remains unchanged**:

```go
// internal/ports/metrics.go
type MetricsCollector interface {
    IncRequestTotal(method, statusCode, pathPattern string)
    ObserveRequestDuration(method, pathPattern string, duration time.Duration)
    IncRateLimitHit(limitType string)
    IncAuthDecision(decision string)
    IncUpstreamError()
    SetActiveConnections(n int)
}
```

This interface is deliberately backend-agnostic. Callers (middleware, plugins) do not
need to know whether the underlying implementation uses Prometheus directly or OTel.

**New port interface** for OTel lifecycle management:

```go
// internal/ports/otel.go
package ports

import (
    "context"
    "net/http"
)

// OTelProvider manages the OpenTelemetry SDK lifecycle.
// It initializes the MeterProvider and exposes an HTTP handler for Prometheus scraping.
// Implementations must be safe for concurrent use after Init returns.
type OTelProvider interface {
    // Init initializes the OTel SDK with the given service name and version.
    // It sets up the MeterProvider with a Prometheus exporter.
    // Must be called once before any other methods.
    Init(ctx context.Context, serviceName, serviceVersion string) error

    // Shutdown gracefully shuts down the OTel SDK, flushing any buffered data.
    // Must honour the context deadline.
    Shutdown(ctx context.Context) error

    // Handler returns an http.Handler that serves Prometheus metrics.
    // Returns nil if Init has not been called.
    Handler() http.Handler

    // Meter returns a named OTel Meter for creating instruments.
    // The scope name is "github.com/vibewarden/vibewarden".
    Meter() Meter
}

// Meter is a subset of the OTel metric.Meter interface, exposing only the
// instrument creation methods VibeWarden needs. This keeps the port layer
// decoupled from the full OTel API.
type Meter interface {
    // Int64Counter creates a Counter instrument for incrementing metrics.
    Int64Counter(name string, options ...InstrumentOption) (Int64Counter, error)

    // Float64Histogram creates a Histogram instrument for recording distributions.
    Float64Histogram(name string, options ...InstrumentOption) (Float64Histogram, error)

    // Int64UpDownCounter creates an UpDownCounter for gauge-like values that can
    // increase or decrease.
    Int64UpDownCounter(name string, options ...InstrumentOption) (Int64UpDownCounter, error)
}

// InstrumentOption configures an OTel instrument (description, unit, etc.).
// This is a placeholder type; the adapter translates to OTel SDK options.
type InstrumentOption interface {
    isInstrumentOption()
}

// Int64Counter is an OTel counter instrument for int64 increments.
type Int64Counter interface {
    Add(ctx context.Context, incr int64, attrs ...Attribute)
}

// Float64Histogram is an OTel histogram instrument for float64 observations.
type Float64Histogram interface {
    Record(ctx context.Context, value float64, attrs ...Attribute)
}

// Int64UpDownCounter is an OTel up-down counter for gauge-like int64 values.
type Int64UpDownCounter interface {
    Add(ctx context.Context, incr int64, attrs ...Attribute)
}

// Attribute is a key-value pair attached to metric observations.
type Attribute struct {
    Key   string
    Value string
}
```

**Why wrap OTel types?**

The ports layer must not import external packages (hexagonal architecture principle).
These thin wrapper types allow the domain/app layers to reference metric concepts
without depending on `go.opentelemetry.io/otel/metric`. The adapter layer performs
the type conversion.

#### Adapters

**New file:** `internal/adapters/otel/provider.go`

```go
// Package otel provides the OpenTelemetry SDK adapter for VibeWarden.
//
// It initializes the MeterProvider with a Prometheus exporter and implements
// ports.OTelProvider. The provider is the single source of truth for OTel
// SDK lifecycle management.
package otel

import (
    "context"
    "fmt"
    "net/http"
    "sync"

    "github.com/prometheus/client_golang/prometheus"
    "github.com/vibewarden/vibewarden/internal/ports"
    "go.opentelemetry.io/otel"
    "go.opentelemetry.io/otel/exporters/prometheus"
    "go.opentelemetry.io/otel/metric"
    sdkmetric "go.opentelemetry.io/otel/sdk/metric"
    "go.opentelemetry.io/otel/sdk/resource"
    semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

// Provider implements ports.OTelProvider using the OTel Go SDK.
type Provider struct {
    mu            sync.RWMutex
    meterProvider *sdkmetric.MeterProvider
    meter         metric.Meter
    handler       http.Handler
    registry      *prometheus.Registry
}

// NewProvider creates an uninitialized Provider.
// Call Init before using any other methods.
func NewProvider() *Provider {
    return &Provider{}
}

// Init initializes the OTel SDK with a Prometheus exporter.
func (p *Provider) Init(ctx context.Context, serviceName, serviceVersion string) error {
    p.mu.Lock()
    defer p.mu.Unlock()

    if p.meterProvider != nil {
        return fmt.Errorf("otel provider already initialized")
    }

    // Create a dedicated Prometheus registry (not the global default).
    p.registry = prometheus.NewRegistry()

    // Create Prometheus exporter with the isolated registry.
    exporter, err := prometheus.New(
        prometheus.WithRegisterer(p.registry),
        prometheus.WithoutScopeInfo(),
    )
    if err != nil {
        return fmt.Errorf("creating prometheus exporter: %w", err)
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

    // Create MeterProvider with the Prometheus exporter.
    p.meterProvider = sdkmetric.NewMeterProvider(
        sdkmetric.WithResource(res),
        sdkmetric.WithReader(exporter),
    )

    // Set as global provider for any code that uses otel.GetMeterProvider().
    otel.SetMeterProvider(p.meterProvider)

    // Create the application meter.
    p.meter = p.meterProvider.Meter("github.com/vibewarden/vibewarden")

    // Create the HTTP handler for Prometheus scraping.
    p.handler = promhttp.HandlerFor(p.registry, promhttp.HandlerOpts{
        EnableOpenMetrics: true,
    })

    return nil
}

// Shutdown shuts down the MeterProvider.
func (p *Provider) Shutdown(ctx context.Context) error {
    p.mu.Lock()
    defer p.mu.Unlock()

    if p.meterProvider == nil {
        return nil
    }
    return p.meterProvider.Shutdown(ctx)
}

// Handler returns the Prometheus metrics HTTP handler.
func (p *Provider) Handler() http.Handler {
    p.mu.RLock()
    defer p.mu.RUnlock()
    return p.handler
}

// Meter returns a ports.Meter wrapping the OTel SDK meter.
func (p *Provider) Meter() ports.Meter {
    p.mu.RLock()
    defer p.mu.RUnlock()
    return &meterAdapter{m: p.meter}
}
```

**New file:** `internal/adapters/otel/meter.go`

```go
package otel

import (
    "context"

    "github.com/vibewarden/vibewarden/internal/ports"
    "go.opentelemetry.io/otel/attribute"
    "go.opentelemetry.io/otel/metric"
)

// meterAdapter wraps an OTel metric.Meter to implement ports.Meter.
type meterAdapter struct {
    m metric.Meter
}

func (a *meterAdapter) Int64Counter(name string, opts ...ports.InstrumentOption) (ports.Int64Counter, error) {
    c, err := a.m.Int64Counter(name, translateOptions(opts)...)
    if err != nil {
        return nil, err
    }
    return &int64CounterAdapter{c: c}, nil
}

func (a *meterAdapter) Float64Histogram(name string, opts ...ports.InstrumentOption) (ports.Float64Histogram, error) {
    h, err := a.m.Float64Histogram(name, translateOptions(opts)...)
    if err != nil {
        return nil, err
    }
    return &float64HistogramAdapter{h: h}, nil
}

func (a *meterAdapter) Int64UpDownCounter(name string, opts ...ports.InstrumentOption) (ports.Int64UpDownCounter, error) {
    c, err := a.m.Int64UpDownCounter(name, translateOptions(opts)...)
    if err != nil {
        return nil, err
    }
    return &int64UpDownCounterAdapter{c: c}, nil
}

// translateOptions converts ports.InstrumentOption to OTel SDK options.
func translateOptions(opts []ports.InstrumentOption) []metric.Int64CounterOption {
    // Implementation translates Description, Unit options.
    // Simplified for ADR; full implementation in code.
    return nil
}

// Instrument adapters...
type int64CounterAdapter struct{ c metric.Int64Counter }

func (a *int64CounterAdapter) Add(ctx context.Context, incr int64, attrs ...ports.Attribute) {
    a.c.Add(ctx, incr, toOTelAttrs(attrs)...)
}

type float64HistogramAdapter struct{ h metric.Float64Histogram }

func (a *float64HistogramAdapter) Record(ctx context.Context, value float64, attrs ...ports.Attribute) {
    a.h.Record(ctx, value, toOTelAttrs(attrs)...)
}

type int64UpDownCounterAdapter struct{ c metric.Int64UpDownCounter }

func (a *int64UpDownCounterAdapter) Add(ctx context.Context, incr int64, attrs ...ports.Attribute) {
    a.c.Add(ctx, incr, toOTelAttrs(attrs)...)
}

func toOTelAttrs(attrs []ports.Attribute) []attribute.KeyValue {
    kvs := make([]attribute.KeyValue, len(attrs))
    for i, a := range attrs {
        kvs[i] = attribute.String(a.Key, a.Value)
    }
    return kvs
}
```

**Updated file:** `internal/adapters/metrics/otel.go` (replaces prometheus.go)

```go
// Package metrics provides metrics adapter implementations for VibeWarden.
package metrics

import (
    "context"
    "net/http"
    "time"

    "github.com/vibewarden/vibewarden/internal/ports"
)

// OTelAdapter implements ports.MetricsCollector using an OTel MeterProvider.
// It creates counters and histograms via ports.Meter and records observations.
type OTelAdapter struct {
    requestsTotal     ports.Int64Counter
    requestDuration   ports.Float64Histogram
    rateLimitHits     ports.Int64Counter
    authDecisions     ports.Int64Counter
    upstreamErrors    ports.Int64Counter
    activeConnections ports.Int64UpDownCounter
    pathMatcher       *PathMatcher
    handler           http.Handler
}

// NewOTelAdapter creates a new OTel-backed MetricsCollector.
// The provider must be initialized before calling this function.
func NewOTelAdapter(provider ports.OTelProvider, pathPatterns []string) (*OTelAdapter, error) {
    meter := provider.Meter()

    requestsTotal, err := meter.Int64Counter("vibewarden_requests_total",
        ports.WithDescription("Total number of HTTP requests processed."),
    )
    if err != nil {
        return nil, err
    }

    requestDuration, err := meter.Float64Histogram("vibewarden_request_duration_seconds",
        ports.WithDescription("HTTP request duration in seconds."),
        ports.WithUnit("s"),
        ports.WithExplicitBuckets([]float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10}),
    )
    if err != nil {
        return nil, err
    }

    rateLimitHits, err := meter.Int64Counter("vibewarden_rate_limit_hits_total",
        ports.WithDescription("Total number of rate limit hits."),
    )
    if err != nil {
        return nil, err
    }

    authDecisions, err := meter.Int64Counter("vibewarden_auth_decisions_total",
        ports.WithDescription("Total number of authentication decisions."),
    )
    if err != nil {
        return nil, err
    }

    upstreamErrors, err := meter.Int64Counter("vibewarden_upstream_errors_total",
        ports.WithDescription("Total number of upstream connection errors."),
    )
    if err != nil {
        return nil, err
    }

    activeConnections, err := meter.Int64UpDownCounter("vibewarden_active_connections",
        ports.WithDescription("Current number of active proxy connections."),
    )
    if err != nil {
        return nil, err
    }

    return &OTelAdapter{
        requestsTotal:     requestsTotal,
        requestDuration:   requestDuration,
        rateLimitHits:     rateLimitHits,
        authDecisions:     authDecisions,
        upstreamErrors:    upstreamErrors,
        activeConnections: activeConnections,
        pathMatcher:       NewPathMatcher(pathPatterns),
        handler:           provider.Handler(),
    }, nil
}

// Handler returns the Prometheus HTTP handler for scraping.
func (a *OTelAdapter) Handler() http.Handler { return a.handler }

// NormalizePath returns the matching pattern for a path.
func (a *OTelAdapter) NormalizePath(path string) string {
    return a.pathMatcher.Match(path)
}

// IncRequestTotal implements ports.MetricsCollector.
func (a *OTelAdapter) IncRequestTotal(method, statusCode, pathPattern string) {
    a.requestsTotal.Add(context.Background(), 1,
        ports.Attribute{Key: "method", Value: method},
        ports.Attribute{Key: "status_code", Value: statusCode},
        ports.Attribute{Key: "path_pattern", Value: pathPattern},
    )
}

// ObserveRequestDuration implements ports.MetricsCollector.
func (a *OTelAdapter) ObserveRequestDuration(method, pathPattern string, duration time.Duration) {
    a.requestDuration.Record(context.Background(), duration.Seconds(),
        ports.Attribute{Key: "method", Value: method},
        ports.Attribute{Key: "path_pattern", Value: pathPattern},
    )
}

// IncRateLimitHit implements ports.MetricsCollector.
func (a *OTelAdapter) IncRateLimitHit(limitType string) {
    a.rateLimitHits.Add(context.Background(), 1,
        ports.Attribute{Key: "limit_type", Value: limitType},
    )
}

// IncAuthDecision implements ports.MetricsCollector.
func (a *OTelAdapter) IncAuthDecision(decision string) {
    a.authDecisions.Add(context.Background(), 1,
        ports.Attribute{Key: "decision", Value: decision},
    )
}

// IncUpstreamError implements ports.MetricsCollector.
func (a *OTelAdapter) IncUpstreamError() {
    a.upstreamErrors.Add(context.Background(), 1)
}

// SetActiveConnections implements ports.MetricsCollector.
func (a *OTelAdapter) SetActiveConnections(n int) {
    // OTel doesn't have a "set" operation for UpDownCounter.
    // We need to track the previous value and add the delta.
    // For simplicity in this foundation story, we use a synchronous approach.
    // A production implementation would track state atomically.
    a.activeConnections.Add(context.Background(), int64(n))
}
```

**Note on SetActiveConnections:** OTel's UpDownCounter only supports Add, not Set.
The implementation must track the previous value and compute the delta. This is
a known OTel limitation. The adapter will maintain an atomic int64 to track current
value and add/subtract the difference.

#### Application Service

No new application service. The metrics plugin orchestrates the OTel provider and adapter.

**Updated metrics plugin:** `internal/plugins/metrics/plugin.go`

```go
package metrics

import (
    "context"
    "fmt"
    "log/slog"

    metricsadapter "github.com/vibewarden/vibewarden/internal/adapters/metrics"
    oteladapter "github.com/vibewarden/vibewarden/internal/adapters/otel"
    "github.com/vibewarden/vibewarden/internal/ports"
)

type Plugin struct {
    cfg          Config
    logger       *slog.Logger
    otelProvider *oteladapter.Provider
    adapter      *metricsadapter.OTelAdapter
    server       *metricsadapter.Server
    internalAddr string
    running      bool
}

func New(cfg Config, logger *slog.Logger) *Plugin {
    return &Plugin{cfg: cfg, logger: logger}
}

func (p *Plugin) Name() string { return "metrics" }
func (p *Plugin) Priority() int { return 30 }

func (p *Plugin) Init(ctx context.Context) error {
    if !p.cfg.Enabled {
        return nil
    }

    // Initialize OTel provider.
    p.otelProvider = oteladapter.NewProvider()
    if err := p.otelProvider.Init(ctx, "vibewarden", Version); err != nil {
        return fmt.Errorf("metrics plugin: initializing otel provider: %w", err)
    }

    // Create the OTel-backed metrics adapter.
    adapter, err := metricsadapter.NewOTelAdapter(p.otelProvider, p.cfg.PathPatterns)
    if err != nil {
        return fmt.Errorf("metrics plugin: creating otel adapter: %w", err)
    }
    p.adapter = adapter

    p.logger.Info("metrics plugin initialised with OTel SDK",
        slog.Int("path_patterns", len(p.cfg.PathPatterns)),
    )
    return nil
}

func (p *Plugin) Start(ctx context.Context) error {
    if !p.cfg.Enabled {
        return nil
    }
    p.server = metricsadapter.NewServer(p.adapter.Handler(), p.logger)
    if err := p.server.Start(); err != nil {
        return fmt.Errorf("metrics plugin: starting internal server: %w", err)
    }
    p.internalAddr = p.server.Addr()
    p.running = true
    p.logger.Info("metrics plugin started",
        slog.String("internal_addr", p.internalAddr),
    )
    return nil
}

func (p *Plugin) Stop(ctx context.Context) error {
    if p.server != nil {
        p.running = false
        if err := p.server.Stop(ctx); err != nil {
            return fmt.Errorf("metrics plugin: stopping internal server: %w", err)
        }
    }
    if p.otelProvider != nil {
        if err := p.otelProvider.Shutdown(ctx); err != nil {
            return fmt.Errorf("metrics plugin: shutting down otel provider: %w", err)
        }
    }
    return nil
}

// Collector returns the MetricsCollector for use by middleware.
// Returns nil if the plugin is disabled or not initialized.
func (p *Plugin) Collector() ports.MetricsCollector {
    if p.adapter == nil {
        return metricsadapter.NoOpMetricsCollector{}
    }
    return p.adapter
}

// ... Health, ContributeCaddyRoutes, InternalAddr unchanged ...
```

#### File Layout

**New files:**

```
internal/
  ports/
    otel.go                    # OTelProvider, Meter, Instrument interfaces
  adapters/
    otel/
      provider.go              # Provider implementing ports.OTelProvider
      provider_test.go         # Unit tests for Provider
      meter.go                 # meterAdapter, instrument adapters
      meter_test.go            # Unit tests for meter adapters
    metrics/
      otel.go                  # OTelAdapter implementing ports.MetricsCollector
      otel_test.go             # Unit tests for OTelAdapter
```

**Deprecated files (to be removed in follow-up):**

```
internal/
  adapters/
    metrics/
      prometheus.go            # DEPRECATED: replaced by otel.go
      prometheus_test.go       # DEPRECATED: replaced by otel_test.go
```

**Unchanged files:**

```
internal/
  ports/
    metrics.go                 # MetricsCollector interface (unchanged)
  adapters/
    metrics/
      noop.go                  # NoOpMetricsCollector (unchanged)
      noop_test.go             # Tests (unchanged)
      path_matcher.go          # PathMatcher (unchanged, reused by OTelAdapter)
      path_matcher_test.go     # Tests (unchanged)
      server.go                # Internal HTTP server (unchanged)
      server_test.go           # Tests (unchanged)
  plugins/
    metrics/
      plugin.go                # Updated to use OTel provider
      plugin_test.go           # Updated tests
      config.go                # Unchanged
      meta.go                  # Unchanged
  middleware/
    metrics.go                 # Unchanged (uses ports.MetricsCollector)
    metrics_test.go            # Unchanged
```

#### Sequence

**Initialization flow:**

1. `main()` loads config, creates metrics plugin with `metrics.New(cfg, logger)`
2. Plugin registry calls `plugin.Init(ctx)`:
   a. Create `oteladapter.Provider` (uninitialized)
   b. Call `provider.Init(ctx, "vibewarden", version)`:
      - Create Prometheus registry
      - Create Prometheus exporter with registry
      - Create OTel Resource with service name/version
      - Create MeterProvider with exporter
      - Set global MeterProvider
      - Create Meter for scope "github.com/vibewarden/vibewarden"
      - Create promhttp.Handler for the registry
   c. Create `metricsadapter.OTelAdapter(provider, pathPatterns)`:
      - Get Meter from provider
      - Create Int64Counter for requests_total
      - Create Float64Histogram for request_duration
      - Create counters for rate_limit_hits, auth_decisions, upstream_errors
      - Create Int64UpDownCounter for active_connections
      - Store handler from provider
3. Plugin registry calls `plugin.Start(ctx)`:
   a. Create internal HTTP server with adapter.Handler()
   b. Server binds random localhost port
   c. Store internal address for Caddy reverse-proxy

**Request flow (unchanged from caller's perspective):**

1. HTTP request arrives at Caddy
2. `MetricsMiddleware` intercepts, records start time
3. Request proceeds through handler chain
4. On response, middleware calls:
   - `mc.IncRequestTotal(method, statusCode, pathPattern)`
   - `mc.ObserveRequestDuration(method, pathPattern, duration)`
5. OTelAdapter forwards to OTel instruments with attributes
6. OTel SDK aggregates observations in memory
7. Prometheus exporter exposes aggregated metrics at `/_vibewarden/metrics`

**Shutdown flow:**

1. Plugin registry calls `plugin.Stop(ctx)`
2. Plugin stops internal HTTP server
3. Plugin calls `provider.Shutdown(ctx)`
4. MeterProvider flushes any pending data
5. Exporter is released

#### Error Cases

| Error | Cause | Handling |
|-------|-------|----------|
| `otel provider already initialized` | Init called twice | Return error, log warning |
| `creating prometheus exporter` | Registry conflict | Return error, plugin fails to init |
| `creating otel resource` | Invalid resource attributes | Return error, plugin fails to init |
| Instrument creation fails | Invalid metric name | Return error from NewOTelAdapter |
| Nil provider.Handler() | Init not called | Return nil, internal server fails |
| Context cancelled during Init | Timeout | Return ctx.Err() |
| Shutdown with pending data | Exporter blocked | Honour context deadline, may lose data |

All errors are wrapped with context and propagated to the plugin registry, which
logs them and marks the plugin as unhealthy.

#### Test Strategy

**Unit tests:**

| File | Coverage |
|------|----------|
| `internal/adapters/otel/provider_test.go` | Init, Shutdown, Handler, Meter accessors |
| `internal/adapters/otel/meter_test.go` | Instrument creation, Add/Record calls, attribute conversion |
| `internal/adapters/metrics/otel_test.go` | All MetricsCollector methods, path normalization |
| `internal/plugins/metrics/plugin_test.go` | Updated for OTel provider lifecycle |

**Unit test approach:**

- Use `go.opentelemetry.io/otel/sdk/metric/metrictest` for in-memory reading of recorded values
- Verify correct metric names, descriptions, and attribute labels
- Test SetActiveConnections delta calculation
- Mock provider for adapter tests

**Integration tests:**

| Test | Coverage |
|------|----------|
| `internal/adapters/metrics/otel_integration_test.go` | Full stack: Provider → Adapter → HTTP scrape |

**Integration test approach:**

- Start real OTel provider with Prometheus exporter
- Record metrics through adapter
- Scrape `/_vibewarden/metrics` endpoint
- Verify Prometheus format output contains expected metrics
- Verify Go runtime metrics are still present

**What to mock vs. what to test real:**

- Mock: Nothing at unit level; OTel SDK is fast and deterministic
- Real: Full integration test with HTTP scraping
- Skip: External OTLP export (future story #286)

#### New Dependencies

| Package | Version | License | Reason |
|---------|---------|---------|--------|
| `go.opentelemetry.io/otel` | v1.41.0 | Apache 2.0 | Core OTel API |
| `go.opentelemetry.io/otel/sdk` | v1.41.0 | Apache 2.0 | MeterProvider implementation |
| `go.opentelemetry.io/otel/sdk/metric` | v1.41.0 | Apache 2.0 | Metric SDK |
| `go.opentelemetry.io/otel/exporters/prometheus` | v0.62.0 | Apache 2.0 | Prometheus exporter |
| `go.opentelemetry.io/otel/semconv/v1.26.0` | v1.41.0 | Apache 2.0 | Semantic conventions |

**License verification:** All packages are part of the `opentelemetry-go` repository,
which is licensed under Apache 2.0 (verified by reading LICENSE file from
https://github.com/open-telemetry/opentelemetry-go). This license is on the
approved list per CLAUDE.md.

**Note:** These packages are already transitive dependencies through Caddy.
Promoting them to direct dependencies does not increase the binary size.

### Consequences

**Positive:**

- **Unified SDK:** Single OTel MeterProvider for all metrics, enabling future
  traces and logs integration with consistent resource attributes.
- **OTLP-ready:** Adding OTLP export (story #286) becomes a one-line change
  to add another reader to the MeterProvider.
- **Prometheus compatible:** Existing scrapers and dashboards work unchanged.
- **No breaking changes:** The `MetricsCollector` port interface is stable;
  callers do not need modification.
- **Vendor-neutral:** OTel is CNCF graduated; no vendor lock-in.

**Negative:**

- **SetActiveConnections complexity:** OTel UpDownCounter lacks Set semantics,
  requiring delta tracking in the adapter. This adds state management overhead.
- **Transitive dependency size:** While already present via Caddy, the OTel SDK
  is larger than prometheus/client_golang alone. Acceptable trade-off for
  observability standardization.
- **Learning curve:** Developers must understand OTel concepts (MeterProvider,
  Meter, Instruments, Attributes) vs. simpler Prometheus registry model.

**Trade-offs:**

- **Port wrapper types vs. direct OTel imports:** Chose wrappers to maintain
  hexagonal purity. Cost: more adapter code. Benefit: domain/app layers remain
  decoupled from OTel specifics.
- **Global MeterProvider:** Setting OTel's global provider simplifies integration
  with any code that uses `otel.GetMeterProvider()`. Risk: potential conflicts
  if user code also sets globals. Mitigation: documented behavior.
- **Deprecate vs. delete prometheus.go:** Chose deprecation in this story,
  deletion in follow-up. Allows rollback if issues discovered.

**Migration path:**

1. This story implements OTel adapter alongside existing Prometheus adapter
2. Plugin switched to use OTel adapter (breaking change for plugin internals only)
3. Follow-up story deletes deprecated prometheus.go
4. Future story #286 adds OTLP exporter configuration

**Locked decision update:**

L-07 currently reads: "Metrics: prometheus/client_golang (Apache 2.0)".

This ADR does **not** change the locked decision. Prometheus remains the export
format; we're changing the internal SDK from prometheus/client_golang to OTel SDK
with Prometheus exporter. The user-facing `/_vibewarden/metrics` endpoint remains
Prometheus-compatible.

A future ADR may update L-07 to "Metrics: OpenTelemetry SDK with Prometheus export"
once this migration is complete and validated.

---
