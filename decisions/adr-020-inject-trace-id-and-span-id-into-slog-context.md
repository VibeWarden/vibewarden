# ADR-020: Inject trace_id and span_id into slog context

**Date**: 2026-03-28
**Issue**: #308
**Status**: Accepted

### Context

Epic #306 ("Distributed tracing with request correlation") requires that log lines can be
correlated with traces. ADR-019 introduced the TracerProvider and HTTP tracing middleware,
which creates a span for each request and stores the span context in the request context.

Currently, when `SlogEventLogger.Log()` is called with the request context, it does not
extract or include the trace_id and span_id. This means log lines have no request correlation.
When multiple requests are processed concurrently, it is impossible to tell which log lines
belong to which request.

Issue #308 addresses this by extracting trace_id and span_id from the span context and
injecting them as slog attributes. Every log line emitted during request processing will
automatically include these fields.

**Constraints:**
- Must not add trace fields when tracing is disabled (no empty strings)
- Must work with both stdout JSON handler and OTel log bridge
- Must not introduce new dependencies beyond what OTel SDK already provides
- Must not break existing log format (additive change only)

### Decision

Modify `SlogEventLogger.Log()` to extract trace context from the request context and add
`trace_id` and `span_id` as slog attributes when a valid span context is present.

#### Domain Model Changes

None. Trace IDs are observability infrastructure, not domain concepts.

#### Ports (Interfaces)

No port changes required. The `ports.EventLogger` interface remains unchanged.
The context passed to `Log(ctx, event)` already carries the span context after
the tracing middleware runs.

#### Adapters

**Modify `internal/adapters/log/slog_adapter.go`**

Update the `Log()` method to extract trace context and add trace attributes:

```go
import (
    // ... existing imports ...
    "go.opentelemetry.io/otel/trace"
)

// Log writes the event as a single JSON line to the configured writer.
// When the context contains a valid OTel span context (from TracingMiddleware),
// trace_id and span_id are added as top-level fields for request correlation.
func (l *SlogEventLogger) Log(ctx context.Context, event events.Event) error {
    // Serialize the payload map to a json.RawMessage (existing code)
    payload := event.Payload
    if payload == nil {
        payload = map[string]any{}
    }
    payloadBytes, err := json.Marshal(payload)
    if err != nil {
        return fmt.Errorf("marshalling event payload: %w", err)
    }

    // Build the list of attributes.
    attrs := []slog.Attr{
        slog.String("schema_version", event.SchemaVersion),
        slog.String("event_type", event.EventType),
        slog.Time("timestamp", event.Timestamp),
        slog.String("ai_summary", event.AISummary),
        slog.Any("payload", json.RawMessage(payloadBytes)),
    }

    // Extract trace context if present and valid.
    if sc := trace.SpanContextFromContext(ctx); sc.IsValid() {
        attrs = append(attrs,
            slog.String("trace_id", sc.TraceID().String()),
            slog.String("span_id", sc.SpanID().String()),
        )
    }

    l.logger.LogAttrs(ctx, slog.LevelInfo, "", attrs...)

    return nil
}
```

Key implementation details:

1. **Use `trace.SpanContextFromContext(ctx)`** — This is more efficient than
   `trace.SpanFromContext(ctx).SpanContext()` because it avoids creating a no-op
   span when the context has no span.

2. **Check `sc.IsValid()`** — Returns false when:
   - Tracing is disabled (no span in context)
   - The span context has invalid/zero trace ID or span ID
   This ensures we never emit empty string fields.

3. **Append to attrs slice** — Trace fields appear after payload in the JSON output.
   Order is: schema_version, event_type, timestamp, ai_summary, payload, trace_id, span_id.

4. **String representation** — `TraceID().String()` returns a 32-character hex string.
   `SpanID().String()` returns a 16-character hex string. These match the W3C Trace
   Context format that OTel collectors expect.

#### Application Service

No application service changes. The trace context flows through automatically via
the request context.

#### File Layout

**Modified files:**

| File | Change |
|------|--------|
| `internal/adapters/log/slog_adapter.go` | Add trace context extraction in `Log()` |
| `internal/adapters/log/slog_adapter_test.go` | Add tests for trace context injection |

**No new files required.** This is a minimal, focused change to the existing adapter.

#### Sequence

Request flow with trace context injection:

1. Request arrives at VibeWarden
2. TracingMiddleware creates span, stores in context via `r.WithContext(ctx)`
3. Request flows through auth, rate limiting, other middleware
4. Middleware or handler calls `eventLogger.Log(r.Context(), ev)`
5. `SlogEventLogger.Log()` extracts span context: `trace.SpanContextFromContext(ctx)`
6. If `sc.IsValid()`, appends trace_id and span_id to slog attrs
7. `logger.LogAttrs()` writes JSON with trace fields
8. Log line includes trace_id and span_id for correlation

For non-traced requests (tracing disabled or internal paths):

1. Request arrives at VibeWarden
2. TracingMiddleware skips span creation (or is not in middleware chain)
3. Request flows through middleware
4. Middleware calls `eventLogger.Log(r.Context(), ev)`
5. `SlogEventLogger.Log()` extracts span context: returns invalid SpanContext
6. `sc.IsValid()` returns false, no trace fields added
7. Log line has no trace_id or span_id fields (not empty strings, completely absent)

#### Error Cases

| Scenario | Handling |
|----------|----------|
| Context is nil | `SpanContextFromContext(nil)` returns invalid SpanContext; no trace fields added |
| Context has no span | `SpanContextFromContext` returns invalid SpanContext; no trace fields added |
| Span context is invalid | `sc.IsValid()` returns false; no trace fields added |
| TraceID or SpanID is zero | `sc.IsValid()` returns false; no trace fields added |

There are no error cases that require special handling. The design gracefully degrades
to no trace fields when tracing is not active.

#### Test Strategy

**Unit tests in `internal/adapters/log/slog_adapter_test.go`:**

| Test | What it verifies |
|------|------------------|
| `TestSlogEventLogger_Log_WithTraceContext` | When context has valid span, trace_id and span_id appear in JSON |
| `TestSlogEventLogger_Log_WithoutTraceContext` | When context has no span, trace_id and span_id are absent (not empty strings) |
| `TestSlogEventLogger_Log_WithInvalidSpanContext` | When span context is invalid, trace_id and span_id are absent |

Test implementation approach:

```go
func TestSlogEventLogger_Log_WithTraceContext(t *testing.T) {
    // Create a real span using OTel SDK in-memory exporter.
    // This ensures we test with actual OTel span context, not mocks.
    tp := sdktrace.NewTracerProvider()
    tracer := tp.Tracer("test")
    ctx, span := tracer.Start(context.Background(), "test-span")
    defer span.End()

    var buf bytes.Buffer
    logger := log.NewSlogEventLogger(&buf)

    ev := events.Event{
        SchemaVersion: "v1",
        EventType:     "test.event",
        Timestamp:     time.Now(),
        AISummary:     "Test event",
        Payload:       map[string]any{},
    }
    _ = logger.Log(ctx, ev)

    var out map[string]any
    _ = json.Unmarshal(buf.Bytes(), &out)

    // Verify trace_id is present and valid (32 hex chars).
    traceID, ok := out["trace_id"].(string)
    if !ok || len(traceID) != 32 {
        t.Errorf("trace_id = %q, want 32-char hex string", traceID)
    }

    // Verify span_id is present and valid (16 hex chars).
    spanID, ok := out["span_id"].(string)
    if !ok || len(spanID) != 16 {
        t.Errorf("span_id = %q, want 16-char hex string", spanID)
    }
}

func TestSlogEventLogger_Log_WithoutTraceContext(t *testing.T) {
    var buf bytes.Buffer
    logger := log.NewSlogEventLogger(&buf)

    ev := events.Event{
        SchemaVersion: "v1",
        EventType:     "test.event",
        Timestamp:     time.Now(),
        AISummary:     "Test event",
        Payload:       map[string]any{},
    }
    _ = logger.Log(context.Background(), ev)

    var out map[string]any
    _ = json.Unmarshal(buf.Bytes(), &out)

    // Verify trace_id and span_id are completely absent, not empty strings.
    if _, ok := out["trace_id"]; ok {
        t.Error("trace_id should be absent when no span context")
    }
    if _, ok := out["span_id"]; ok {
        t.Error("span_id should be absent when no span context")
    }
}
```

**No integration tests needed.** The trace context injection is purely in-memory
manipulation of slog attributes. The existing integration tests for the tracing
middleware (ADR-019) already verify that span context is stored in the request context.

#### New Dependencies

**None.** The `go.opentelemetry.io/otel/trace` package is already a direct dependency
(used by the tracer adapter in ADR-019). No new imports are introduced.

Existing dependency:
| Package | Version | License | Status |
|---------|---------|---------|--------|
| `go.opentelemetry.io/otel/trace` | v1.42.0 | Apache 2.0 | Already direct |

### Consequences

**Positive:**

- Every log line during a traced request includes trace_id and span_id
- Log aggregators (Grafana Loki, etc.) can correlate logs with traces
- No trace fields when tracing is disabled (clean output)
- Works with both stdout JSON and OTel log bridge
- Zero new dependencies
- Minimal code change (~10 lines)

**Negative:**

- Slight increase in log line size (48 bytes for trace fields)
- Import of `go.opentelemetry.io/otel/trace` in slog adapter (couples adapter to OTel)

**Trade-offs:**

- **Coupling slog adapter to OTel vs. using a port:** Chose direct OTel import
  because creating a port for trace context extraction would be over-engineering.
  The slog adapter is already an infrastructure adapter, and the OTel trace package
  is a stable, minimal API. If we ever need a non-OTel tracing library, we would
  need a new adapter anyway.

- **Always checking span context vs. configurable:** Chose to always check span
  context because `SpanContextFromContext` is cheap (single map lookup) and
  the conditional add is simpler than config-based logic. No performance concern.

- **Field names `trace_id`/`span_id` vs. OTel convention `traceID`/`spanID`:**
  Chose snake_case to match the existing VibeWarden log schema (schema_version,
  event_type, ai_summary). Consistency within our schema trumps OTel naming.

---
