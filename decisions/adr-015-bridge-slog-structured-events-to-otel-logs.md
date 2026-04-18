# ADR-015: Bridge slog Structured Events to OTel Logs

**Date**: 2026-03-28
**Issue**: #289
**Status**: Accepted

### Context

VibeWarden's structured event logging follows a v1 schema with `schema_version`, `event_type`,
`timestamp`, `ai_summary`, and `payload` fields. This schema is the project's key differentiator
for AI-readable logs. Currently, events are emitted via the `ports.EventLogger` interface,
implemented by `SlogEventLogger` which writes JSON to stdout.

Issue #289 (part of epic #280 "Switch telemetry from Prometheus to OpenTelemetry") adds the
ability to export these structured events to an OpenTelemetry Collector via OTLP. This enables
users to:

1. Centralize logs alongside metrics in their observability backend (Grafana Cloud, Datadog, etc.)
2. Correlate log events with traces (future: when distributed tracing is added)
3. Use OTel's standard log pipeline for filtering, sampling, and routing

**Design constraints from the epic:**

- slog stays as the primary logging interface (locked decision L-08)
- OTel is an export path, not a replacement for stdout logging
- Use the OTel log bridge for slog (`go.opentelemetry.io/contrib/bridges/otelslog`)
- Bridge structured events to OTel log records, preserving the full schema
- OTLP log exporter shares the same endpoint config as metrics

**Current state:**

- `internal/adapters/log/slog_adapter.go` implements `ports.EventLogger` using a `slog.JSONHandler`
- `internal/adapters/otel/provider.go` initializes the `MeterProvider` for metrics
- OTel log SDK packages are already transitive dependencies (via Caddy):
  - `go.opentelemetry.io/otel/log v0.16.0`
  - `go.opentelemetry.io/otel/sdk/log v0.16.0`
  - `go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp v0.16.0`

### Decision

Add OTel log export as an optional, additive feature alongside stdout JSON logging.
When `telemetry.logs.otlp: true` is configured, structured events are written to both
stdout (existing behavior) and to the OTel Collector (new behavior).

#### Domain Model Changes

None. The `events.Event` struct remains unchanged. The domain layer has no knowledge of
OTel or log export mechanisms.

#### Ports (Interfaces)

**New interface in `internal/ports/otel.go`:**

```go
// LoggerProvider manages the OTel Log SDK lifecycle.
// It creates LoggerProviders that bridge slog events to OTel log records.
type LoggerProvider interface {
    // Handler returns an slog.Handler that bridges log records to OTel.
    // The handler emits logs with the configured service identity and resource attributes.
    // Returns nil if log export is disabled or Init has not been called.
    Handler() slog.Handler

    // Shutdown gracefully shuts down the LoggerProvider, flushing any buffered logs.
    Shutdown(ctx context.Context) error
}
```

**Extend `TelemetryConfig` in `internal/ports/otel.go`:**

```go
// TelemetryConfig holds all telemetry export settings.
type TelemetryConfig struct {
    // Prometheus enables the Prometheus pull-based exporter for metrics.
    Prometheus PrometheusExporterConfig

    // OTLP enables the OTLP push-based exporter for metrics.
    OTLP OTLPExporterConfig

    // Logs configures log export settings.
    Logs LogExportConfig
}

// LogExportConfig configures log export via OTLP.
type LogExportConfig struct {
    // OTLPEnabled toggles OTLP log export (default: false).
    // When enabled, logs are exported to the same OTLP endpoint as metrics.
    OTLPEnabled bool
}
```

**Note:** `EventLogger` interface remains unchanged. The bridging happens at the adapter level,
not the port level.

#### Adapters

**New file: `internal/adapters/otel/log_provider.go`**

Implements `ports.LoggerProvider`. Initializes the OTel `LoggerProvider` with an OTLP HTTP
exporter using the same endpoint as metrics. Creates an `otelslog.Handler` that bridges
slog records to OTel log records.

```go
// LogProvider implements ports.LoggerProvider using the OTel Log SDK.
type LogProvider struct {
    mu             sync.RWMutex
    loggerProvider *sdklog.LoggerProvider
    handler        slog.Handler
}

// NewLogProvider creates an uninitialized LogProvider.
func NewLogProvider() *LogProvider

// Init initializes the OTel LoggerProvider with an OTLP HTTP exporter.
// serviceName and serviceVersion are recorded as OTel resource attributes.
// otlpEndpoint must be provided when OTLPEnabled is true in cfg.
func (p *LogProvider) Init(ctx context.Context, serviceName, serviceVersion, otlpEndpoint string, cfg ports.LogExportConfig) error

// Handler returns the otelslog.Handler, or nil if Init has not been called or logs disabled.
func (p *LogProvider) Handler() slog.Handler

// Shutdown gracefully shuts down the LoggerProvider.
func (p *LogProvider) Shutdown(ctx context.Context) error
```

**Modify: `internal/adapters/log/slog_adapter.go`**

Add a multi-handler variant that fans out to multiple slog handlers:

```go
// MultiHandler is an slog.Handler that dispatches to multiple handlers.
// All handlers receive every log record. Errors are silently ignored
// (best-effort logging).
type MultiHandler struct {
    handlers []slog.Handler
}

// NewMultiHandler creates a handler that dispatches to all given handlers.
func NewMultiHandler(handlers ...slog.Handler) *MultiHandler

// Enabled returns true if any underlying handler is enabled for the level.
func (h *MultiHandler) Enabled(ctx context.Context, level slog.Level) bool

// Handle dispatches the record to all handlers.
func (h *MultiHandler) Handle(ctx context.Context, r slog.Record) error

// WithAttrs returns a new MultiHandler with the given attrs added to each handler.
func (h *MultiHandler) WithAttrs(attrs []slog.Attr) slog.Handler

// WithGroup returns a new MultiHandler with the given group name.
func (h *MultiHandler) WithGroup(name string) slog.Handler
```

**Modify: `internal/adapters/log/slog_adapter.go`**

Update `SlogEventLogger` to accept an optional list of additional handlers:

```go
// NewSlogEventLogger creates a SlogEventLogger that writes JSON to w.
// Additional handlers (e.g., OTel bridge) can be provided; events are
// dispatched to all handlers.
func NewSlogEventLogger(w io.Writer, additionalHandlers ...slog.Handler) *SlogEventLogger
```

When additional handlers are provided, the logger uses a `MultiHandler` combining the
JSON handler with the additional handlers.

#### Application Service

No changes to application services.

#### File Layout

**New files:**

| File | Purpose |
|------|---------|
| `internal/adapters/otel/log_provider.go` | OTel LoggerProvider adapter |
| `internal/adapters/otel/log_provider_test.go` | Unit tests for LogProvider |
| `internal/adapters/log/multi_handler.go` | MultiHandler implementation |
| `internal/adapters/log/multi_handler_test.go` | Unit tests for MultiHandler |

**Modified files:**

| File | Changes |
|------|---------|
| `internal/ports/otel.go` | Add `LoggerProvider` interface, `LogExportConfig` struct, extend `TelemetryConfig` |
| `internal/adapters/log/slog_adapter.go` | Accept additional handlers in constructor |
| `internal/adapters/log/slog_adapter_test.go` | Test multi-handler dispatch |
| `internal/config/config.go` | Add `Logs` field to `TelemetryConfig`, add config defaults |
| `internal/plugins/metrics/plugin.go` | Initialize LogProvider when logs.otlp enabled |
| `internal/plugins/metrics/config.go` | Add `LogsOTLPEnabled` field |
| `cmd/vibewarden/serve.go` | Pass OTel handler to event logger when enabled |

#### Sequence

**Startup (logs.otlp enabled):**

1. Config loads `telemetry.logs.otlp: true`
2. Metrics plugin `Init`:
   a. Creates OTel MeterProvider (existing)
   b. Creates OTel LoggerProvider with OTLP HTTP exporter (new)
   c. LoggerProvider uses same endpoint as metrics OTLP exporter
3. `serve.go` retrieves OTel log handler from metrics plugin
4. Creates `SlogEventLogger` with JSON handler + OTel handler (multi-handler)
5. Events logged via `EventLogger.Log()` are dispatched to both handlers

**Runtime (event emitted):**

```
1. app/middleware calls EventLogger.Log(event)
2. SlogEventLogger.Log() serializes event to slog.LogAttrs()
3. MultiHandler.Handle() dispatches to:
   a. JSON handler -> stdout (existing behavior)
   b. OTel handler -> LoggerProvider -> BatchProcessor -> OTLP exporter
4. OTLP exporter batches and pushes to collector endpoint
```

**Shutdown:**

1. Plugin Stop called
2. LoggerProvider.Shutdown() flushes pending log batches
3. MeterProvider.Shutdown() flushes pending metrics (existing)

#### OTel Log Record Mapping

Each VibeWarden event maps to an OTel log record as follows:

| Event field | OTel log record field |
|-------------|----------------------|
| `Timestamp` | `Timestamp` |
| `EventType` | Attribute: `event.type` |
| `SchemaVersion` | Attribute: `vibewarden.schema_version` |
| `AISummary` | `Body` (string) |
| `Payload.*` | Attributes: `vibewarden.payload.<key>` |

**Severity mapping:**

Event types are mapped to OTel severity levels based on their semantic meaning:

| Event type pattern | OTel Severity |
|-------------------|---------------|
| `*.failed`, `*.blocked`, `*.hit` | WARN (13) |
| `*.unavailable`, `*_failed` | ERROR (17) |
| `*.success`, `*.created`, `*.started`, `*.recovered` | INFO (9) |
| Default | INFO (9) |

The severity mapping is implemented as a pure function in `log_provider.go`:

```go
func severityForEventType(eventType string) log.Severity {
    switch {
    case strings.HasSuffix(eventType, ".failed"),
         strings.HasSuffix(eventType, ".blocked"),
         strings.HasSuffix(eventType, ".hit"):
        return log.SeverityWarn
    case strings.HasSuffix(eventType, ".unavailable"),
         strings.HasSuffix(eventType, "_failed"):
        return log.SeverityError
    default:
        return log.SeverityInfo
    }
}
```

#### Error Cases

| Error | When | Handling |
|-------|------|----------|
| OTLP endpoint missing | `logs.otlp: true` but no `otlp.endpoint` | Error from LogProvider.Init |
| Collector unreachable | Network failure | OTLP exporter retries with backoff; logs are dropped after retry exhaustion (best-effort) |
| Invalid log record | Malformed event payload | OTel SDK logs warning; record skipped |

**Graceful degradation:**

- Stdout logging always works (direct I/O, no network)
- OTel log export is best-effort; failures do not block request processing
- If LogProvider fails to initialize, serve.go falls back to stdout-only logging

#### Test Strategy

**Unit tests:**

| File | Tests |
|------|-------|
| `internal/adapters/otel/log_provider_test.go` | Init with valid config; Init fails without endpoint; Shutdown idempotent |
| `internal/adapters/log/multi_handler_test.go` | Dispatches to all handlers; WithAttrs/WithGroup propagate; Enabled returns true if any enabled |
| `internal/adapters/log/slog_adapter_test.go` | Log with additional handlers; verify both handlers receive records |

**Integration tests:**

| File | Tests |
|------|-------|
| `internal/adapters/otel/log_provider_integration_test.go` | Full roundtrip: emit event -> verify log record attributes |

**What to mock vs. real:**

- Real: OTel LoggerProvider, MultiHandler, SlogEventLogger
- Mock: OTLP endpoint (use `httptest.Server` to capture exported logs)

**Test helper (add to `internal/adapters/otel/testing.go`):**

```go
// NewTestLogProvider creates a LoggerProvider with an in-memory exporter for testing.
// Returns the provider and a function to retrieve exported log records.
func NewTestLogProvider(ctx context.Context) (*LogProvider, func() []sdklog.ReadOnlyLogRecord, error)
```

#### New Dependencies

**Direct dependency to add:**

| Package | Version | License | Purpose |
|---------|---------|---------|---------|
| `go.opentelemetry.io/contrib/bridges/otelslog` | latest | Apache 2.0 | Bridge slog to OTel log SDK |

**License verification:**

The `opentelemetry-go-contrib` repository is licensed under Apache 2.0:
https://github.com/open-telemetry/opentelemetry-go-contrib/blob/main/LICENSE

All packages in the contrib repository share this license, including `bridges/otelslog`.

**Already transitive dependencies (no action needed):**

| Package | Version | License |
|---------|---------|---------|
| `go.opentelemetry.io/otel/log` | v0.16.0 | Apache 2.0 |
| `go.opentelemetry.io/otel/sdk/log` | v0.16.0 | Apache 2.0 |
| `go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp` | v0.16.0 | Apache 2.0 |

#### Configuration

**New config fields in `vibewarden.yaml`:**

```yaml
telemetry:
  # Existing fields...
  prometheus:
    enabled: true
  otlp:
    enabled: true
    endpoint: http://localhost:4318
  # New field:
  logs:
    otlp: false  # default: false (opt-in)
```

**Config struct changes in `internal/config/config.go`:**

```go
type TelemetryConfig struct {
    Enabled      bool                     `mapstructure:"enabled"`
    PathPatterns []string                 `mapstructure:"path_patterns"`
    Prometheus   PrometheusExporterConfig `mapstructure:"prometheus"`
    OTLP         OTLPExporterConfig       `mapstructure:"otlp"`
    Logs         LogsConfig               `mapstructure:"logs"`
}

type LogsConfig struct {
    // OTLP toggles OTLP log export (default: false).
    // When enabled, structured events are exported to the same OTLP endpoint as metrics.
    // Requires telemetry.otlp.endpoint to be configured.
    OTLP bool `mapstructure:"otlp"`
}
```

**Defaults:**

```go
v.SetDefault("telemetry.logs.otlp", false)
```

**Validation:**

```go
// If logs.otlp enabled, otlp.endpoint must be set
if c.Telemetry.Logs.OTLP && c.Telemetry.OTLP.Endpoint == "" {
    errs = append(errs, "telemetry.logs.otlp requires telemetry.otlp.endpoint")
}
```

### Consequences

**Positive:**

- **Unified observability:** Logs, metrics, and (future) traces all flow through OTel
- **AI-readable logs preserved:** Schema unchanged; OTel is purely an export path
- **Stdout always works:** OTel export is additive; existing behavior unchanged
- **Shared config:** Logs use same OTLP endpoint as metrics (DRY)
- **Future-proof:** When distributed tracing is added, logs can be correlated via trace context

**Negative:**

- **New dependency:** `otelslog` bridge adds to binary size (~small)
- **Complexity:** Multi-handler dispatch adds a layer of indirection
- **Batch delay:** OTLP export is batched; logs appear with ~1-30s delay in collector

**Trade-offs:**

- **Multi-handler vs. separate loggers:** Chose multi-handler. Alternative was to have
  two separate `EventLogger` implementations called sequentially. Multi-handler is more
  composable and follows slog idioms.

- **Severity in event vs. derived:** Chose derived from event_type. Alternative was to
  add a Severity field to `events.Event`. Derived keeps domain layer clean and works
  well for the current event type taxonomy.

- **Same endpoint vs. separate:** Chose same endpoint. Alternative was separate
  `telemetry.logs.endpoint`. Shared endpoint is simpler and matches how most users
  deploy OTel Collector (single receiver for all signals).

**Limitations:**

- Log export is push-only (no pull equivalent like Prometheus metrics)
- OTel log SDK is still maturing (v0.x); API may change in future OTel releases
- No trace correlation yet (requires #293: distributed tracing setup)

---
