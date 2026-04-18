# ADR-019: TracerProvider Initialization and HTTP Tracing Middleware

**Date**: 2026-03-28
**Issue**: #307
**Status**: Accepted

### Context

Epic #306 ("Distributed tracing with request correlation") establishes the need for OTel
tracing in VibeWarden. Currently, the sidecar has metrics (MeterProvider) and logs
(LoggerProvider) but no tracing. Requests pass through without generating trace context,
making it impossible to correlate logs and metrics with specific request flows.

Issue #307 is the first story in this epic. It introduces:
1. TracerProvider initialization in the OTel provider adapter
2. HTTP middleware that wraps each request in a span
3. Config toggle `telemetry.traces.enabled`

The middleware must be the outermost middleware so it captures the full request lifecycle
including auth, rate limiting, and proxy latency. The span context must be stored in the
request context for downstream use (log correlation in #308, error responses in #309).

**Constraints:**
- Must reuse the existing OTLP endpoint configuration (same as metrics/logs)
- TracerProvider lifecycle must integrate with existing Shutdown path
- Middleware must not break existing tests or integration tests
- Default `traces.enabled: false` for backward compatibility

### Decision

Extend the existing OTel adapter with TracerProvider support and add tracing middleware
as a new Caddy handler contributed by the metrics plugin. The tracing middleware is
integrated into the Caddy catch-all handler chain at the outermost position (lowest
priority number).

#### Domain Model Changes

None. Tracing is an infrastructure concern, not a domain concept. The trace_id and span_id
are observability context, not domain entities.

#### Ports (Interfaces)

**1. Extend `ports.TelemetryConfig` with traces config**

Add a new field to `ports.TelemetryConfig` in `internal/ports/otel.go`:

```go
// TelemetryConfig holds all telemetry export settings.
type TelemetryConfig struct {
    // ... existing fields ...

    // Traces configures distributed tracing settings.
    Traces TraceExportConfig
}

// TraceExportConfig configures OTel tracing.
type TraceExportConfig struct {
    // Enabled toggles tracing (default: false).
    // When enabled, a span is created for each HTTP request.
    Enabled bool
}
```

**2. Extend `ports.OTelProvider` interface**

Add a method to expose the tracer:

```go
type OTelProvider interface {
    // ... existing methods ...

    // Tracer returns an OTel Tracer for creating spans.
    // Returns nil if tracing is disabled or Init has not been called.
    Tracer() Tracer

    // TracingEnabled returns true if the tracing exporter is active.
    TracingEnabled() bool
}
```

**3. New `ports.Tracer` interface**

Create a minimal tracer interface that decouples application code from the full OTel API:

```go
// Tracer is a subset of the OTel trace.Tracer interface.
// It exposes only the span creation method VibeWarden needs.
type Tracer interface {
    // Start creates a span and a context containing the newly-created span.
    // The span must be ended by calling span.End() when the operation completes.
    Start(ctx context.Context, spanName string, opts ...SpanStartOption) (context.Context, Span)
}

// Span represents a single operation within a trace.
type Span interface {
    // End marks the span as complete. Must be called exactly once.
    End()

    // SetStatus sets the span status.
    SetStatus(code SpanStatusCode, description string)

    // SetAttributes sets attributes on the span.
    SetAttributes(attrs ...Attribute)

    // RecordError records an error as a span event.
    RecordError(err error)
}

// SpanStartOption configures span creation.
type SpanStartOption interface {
    isSpanStartOption()
}

// SpanStatusCode represents the status of a span.
type SpanStatusCode int

const (
    SpanStatusUnset SpanStatusCode = iota
    SpanStatusOK
    SpanStatusError
)

// WithSpanKind returns a SpanStartOption that sets the span kind.
func WithSpanKind(kind SpanKind) SpanStartOption

// SpanKind is the type of span.
type SpanKind int

const (
    SpanKindInternal SpanKind = iota
    SpanKindServer
    SpanKindClient
)
```

#### Adapters

**1. Extend `internal/adapters/otel/provider.go`**

Add TracerProvider initialization alongside MeterProvider:

```go
type Provider struct {
    mu            sync.RWMutex
    meterProvider *sdkmetric.MeterProvider
    tracerProvider *sdktrace.TracerProvider  // NEW
    meter         otelmetric.Meter
    tracer        trace.Tracer              // NEW
    handler       http.Handler
    registry      *prometheusclient.Registry

    promEnabled bool
    otlpEnabled bool
    traceEnabled bool                        // NEW
}
```

In `Init()`, when `cfg.Traces.Enabled` is true and OTLP is configured:
1. Create an OTLP trace exporter using `otlptracehttp.New()`
2. Create a BatchSpanProcessor with the exporter
3. Create a TracerProvider with the resource and processor
4. Set as global tracer provider via `otel.SetTracerProvider()`
5. Create the application tracer via `tracerProvider.Tracer("github.com/vibewarden/vibewarden")`

In `Shutdown()`, shut down TracerProvider before MeterProvider (traces may reference metrics).

**2. New `internal/adapters/otel/tracer.go`**

Implement `ports.Tracer` by wrapping the OTel SDK tracer:

```go
// tracerAdapter wraps an OTel trace.Tracer to implement ports.Tracer.
type tracerAdapter struct {
    t trace.Tracer
}

func (a *tracerAdapter) Start(ctx context.Context, spanName string, opts ...ports.SpanStartOption) (context.Context, ports.Span) {
    // Convert ports.SpanStartOption to trace.SpanStartOption
    var traceOpts []trace.SpanStartOption
    for _, opt := range opts {
        if k, ok := opt.(spanKindOption); ok {
            traceOpts = append(traceOpts, trace.WithSpanKind(convertSpanKind(k.kind)))
        }
    }
    ctx, span := a.t.Start(ctx, spanName, traceOpts...)
    return ctx, &spanAdapter{s: span}
}

// spanAdapter wraps an OTel trace.Span to implement ports.Span.
type spanAdapter struct {
    s trace.Span
}

func (a *spanAdapter) End() {
    a.s.End()
}

func (a *spanAdapter) SetStatus(code ports.SpanStatusCode, description string) {
    a.s.SetStatus(convertStatusCode(code), description)
}

func (a *spanAdapter) SetAttributes(attrs ...ports.Attribute) {
    otelAttrs := make([]attribute.KeyValue, len(attrs))
    for i, attr := range attrs {
        otelAttrs[i] = attribute.String(attr.Key, attr.Value)
    }
    a.s.SetAttributes(otelAttrs...)
}

func (a *spanAdapter) RecordError(err error) {
    a.s.RecordError(err)
}
```

**3. New `internal/middleware/tracing.go`**

HTTP middleware that creates a span for each request:

```go
// TracingMiddleware returns HTTP middleware that creates an OTel span for each request.
// It must be the outermost middleware (first in, last out) to capture the full
// request lifecycle including auth, rate limiting, and proxy latency.
//
// The middleware sets standard HTTP span attributes:
//   - http.request.method
//   - url.path
//   - http.response.status_code
//   - http.route (normalized path pattern)
//
// The span context is stored in the request context for downstream use
// (log correlation, error responses).
//
// Requests to /_vibewarden/* paths are NOT traced to avoid self-referential noise.
func TracingMiddleware(
    tracer ports.Tracer,
    normalizePathFn func(string) string,
) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            // Skip tracing for internal endpoints.
            if strings.HasPrefix(r.URL.Path, "/_vibewarden/") {
                next.ServeHTTP(w, r)
                return
            }

            // Create span with server kind.
            ctx, span := tracer.Start(r.Context(), "HTTP "+r.Method,
                ports.WithSpanKind(ports.SpanKindServer))
            defer span.End()

            // Set initial attributes.
            route := normalizePathFn(r.URL.Path)
            span.SetAttributes(
                ports.Attribute{Key: "http.request.method", Value: r.Method},
                ports.Attribute{Key: "url.path", Value: r.URL.Path},
                ports.Attribute{Key: "http.route", Value: route},
            )

            // Wrap response writer to capture status code.
            rw := &tracingResponseWriter{ResponseWriter: w, statusCode: http.StatusOK}

            // Serve with span context.
            next.ServeHTTP(rw, r.WithContext(ctx))

            // Set final attributes.
            span.SetAttributes(
                ports.Attribute{Key: "http.response.status_code", Value: strconv.Itoa(rw.statusCode)},
            )

            // Set span status based on HTTP status.
            if rw.statusCode >= 500 {
                span.SetStatus(ports.SpanStatusError, http.StatusText(rw.statusCode))
            } else {
                span.SetStatus(ports.SpanStatusOK, "")
            }
        })
    }
}

// tracingResponseWriter wraps http.ResponseWriter to capture the status code.
type tracingResponseWriter struct {
    http.ResponseWriter
    statusCode int
    written    bool
}

func (rw *tracingResponseWriter) WriteHeader(code int) {
    if !rw.written {
        rw.statusCode = code
        rw.written = true
    }
    rw.ResponseWriter.WriteHeader(code)
}

func (rw *tracingResponseWriter) Write(b []byte) (int, error) {
    if !rw.written {
        rw.statusCode = http.StatusOK
        rw.written = true
    }
    return rw.ResponseWriter.Write(b)
}
```

#### Application Service

No application service changes. Tracing is infrastructure-level, handled by middleware and adapters.

#### File Layout

**New files:**

| File | Purpose |
|------|---------|
| `internal/adapters/otel/tracer.go` | `tracerAdapter` and `spanAdapter` implementations |
| `internal/adapters/otel/tracer_test.go` | Unit tests for tracer adapter |
| `internal/middleware/tracing.go` | `TracingMiddleware` implementation |
| `internal/middleware/tracing_test.go` | Unit tests for tracing middleware |

**Modified files:**

| File | Changes |
|------|---------|
| `internal/ports/otel.go` | Add `TraceExportConfig`, `Tracer`, `Span` interfaces |
| `internal/adapters/otel/provider.go` | Add TracerProvider initialization and shutdown |
| `internal/adapters/otel/provider_test.go` | Add tests for TracerProvider lifecycle |
| `internal/config/config.go` | Add `TracesConfig` struct to `TelemetryConfig` |
| `internal/plugins/metrics/config.go` | Add `TracesEnabled` field |
| `internal/plugins/metrics/plugin.go` | Initialize tracer, contribute tracing handler to Caddy |

#### Sequence

**Request flow with tracing enabled:**

1. HTTP request arrives at Caddy
2. Caddy handler chain starts; tracing handler is first (priority 5)
3. Tracing handler creates span via `tracer.Start()`
4. Span context stored in request context
5. Next handlers execute (security headers, auth, rate limit, proxy)
6. Response written by reverse proxy
7. Tracing handler captures status code from wrapped ResponseWriter
8. Tracing handler sets final attributes (status_code) and span status
9. Tracing handler calls `span.End()` (deferred)
10. Span is batched by BatchSpanProcessor
11. Batch is exported via OTLP HTTP to collector on configured interval

**TracerProvider initialization:**

1. Metrics plugin `Init()` calls `provider.Init()` with config
2. If `cfg.Traces.Enabled && cfg.OTLP.Enabled`:
   - Create OTLP HTTP trace exporter with same endpoint
   - Create BatchSpanProcessor with exporter
   - Create TracerProvider with resource and processor
   - Set global tracer provider
   - Create application tracer
3. Provider stores tracer for later retrieval

**TracerProvider shutdown:**

1. Metrics plugin `Stop()` calls `provider.Shutdown()`
2. TracerProvider is shut down first (flushes pending spans)
3. MeterProvider is shut down second

#### Error Cases

| Error | Handling |
|-------|----------|
| Traces enabled but OTLP disabled | Return error from `provider.Init()`: "traces require OTLP exporter to be enabled" |
| OTLP exporter creation fails | Return error from `provider.Init()` with wrapped exporter error |
| TracerProvider shutdown fails | Log error, continue shutdown (best effort) |
| Span creation panics | Should not happen with valid tracer; if it does, recover in middleware and serve without tracing |

#### Test Strategy

**Unit tests:**

| Test | Location | What it verifies |
|------|----------|------------------|
| `TestTracerAdapter_Start` | `internal/adapters/otel/tracer_test.go` | Span creation, context propagation |
| `TestSpanAdapter_SetAttributes` | `internal/adapters/otel/tracer_test.go` | Attribute setting |
| `TestSpanAdapter_SetStatus` | `internal/adapters/otel/tracer_test.go` | Status code mapping |
| `TestTracingMiddleware_CreatesSpan` | `internal/middleware/tracing_test.go` | Span is created for each request |
| `TestTracingMiddleware_SetsAttributes` | `internal/middleware/tracing_test.go` | HTTP attributes are set correctly |
| `TestTracingMiddleware_SkipsInternalPaths` | `internal/middleware/tracing_test.go` | `/_vibewarden/*` paths are not traced |
| `TestTracingMiddleware_CapturesStatusCode` | `internal/middleware/tracing_test.go` | Status code is captured from response |
| `TestTracingMiddleware_SetsErrorStatus` | `internal/middleware/tracing_test.go` | 5xx responses set error status |

**Integration tests:**

| Test | Location | What it verifies |
|------|----------|------------------|
| `TestProvider_TracerProvider_Init` | `internal/adapters/otel/provider_test.go` | TracerProvider initializes when traces enabled |
| `TestProvider_TracerProvider_RequiresOTLP` | `internal/adapters/otel/provider_test.go` | Error when traces enabled without OTLP |
| `TestProvider_Shutdown_TracerProvider` | `internal/adapters/otel/provider_test.go` | TracerProvider shuts down gracefully |

**Mock tracer for tests:**

Create a mock tracer in `internal/adapters/otel/testing.go` for use in middleware tests:

```go
// MockTracer implements ports.Tracer for testing.
type MockTracer struct {
    StartCalls []struct {
        Name string
        Opts []ports.SpanStartOption
    }
    SpanToReturn *MockSpan
}

// MockSpan implements ports.Span for testing.
type MockSpan struct {
    Ended      bool
    StatusCode ports.SpanStatusCode
    StatusDesc string
    Attrs      []ports.Attribute
    Errors     []error
}
```

#### New Dependencies

**None.** The required OTel tracing packages are already in `go.mod` as indirect dependencies:

| Package | Version | License | Status |
|---------|---------|---------|--------|
| `go.opentelemetry.io/otel/sdk/trace` | v1.42.0 | Apache 2.0 | Already indirect |
| `go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp` | v1.41.0 | Apache 2.0 | Already indirect |

The implementation will promote these to direct dependencies. No new licenses are introduced.

#### Config Schema

**Add to `config.TelemetryConfig`:**

```go
// TracesConfig holds tracing settings.
type TracesConfig struct {
    // Enabled toggles distributed tracing (default: false).
    // When enabled, a span is created for each HTTP request and exported via OTLP.
    // Requires telemetry.otlp.enabled to be true.
    Enabled bool `mapstructure:"enabled"`
}
```

**YAML example:**

```yaml
telemetry:
  enabled: true
  otlp:
    enabled: true
    endpoint: http://otel-collector:4318
  traces:
    enabled: true
```

### Consequences

**Positive:**

- Each HTTP request gets a trace context (trace_id, span_id)
- Span context is available in request context for downstream features (#308, #309)
- Traces are exported via OTLP to the same collector as metrics and logs
- TracerProvider shutdown flushes pending spans before exit
- Middleware pattern is consistent with existing metrics middleware
- Default false preserves backward compatibility

**Negative:**

- Small overhead per request (span creation, context propagation)
- Traces require OTLP to be enabled (no standalone traces-only mode)
- Additional complexity in provider lifecycle management

**Trade-offs:**

- **Trace all requests vs. sampling:** Chose to trace all requests (100% sampling) in v1
  for simplicity. Sampling can be added later via config. For the target vibe coder,
  having complete traces is more valuable than optimizing overhead.

- **Middleware priority:** Chose priority 5 (lower than security headers at 10) so tracing
  captures the full lifecycle. This means the span includes security header injection time,
  which is intentional — we want to measure end-to-end latency.

- **Skip internal paths:** Chose to skip `/_vibewarden/*` paths to avoid self-referential
  noise. This is consistent with the metrics middleware behavior.

- **No span propagation yet:** This ADR does not implement trace context propagation to
  upstream apps (that is #311). The span created here is a root span. Propagation will
  be added in a later story.

---
