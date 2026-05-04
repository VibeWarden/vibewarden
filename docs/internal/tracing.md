# Tracing — Internal Reference

> This file consolidates content relocated from two ADRs on 2026-05-04 as part of the
> ADR audit (audit report: `~/notes/vibewarden/audit-adr-2026-05-03.md`). Stubs remain at
> the original decision paths; existing PR / commit references continue to resolve.

---

## From ADR-020 — Inject trace_id and span_id into slog context

**Date**: 2026-03-28 | **Issue**: #308

ADR-019 introduced `TracingMiddleware` which creates a span per request. ADR-020 extended
`SlogEventLogger.Log()` to extract and inject trace context into every log line for request
correlation.

`SlogEventLogger.Log()` checks `trace.SpanContextFromContext(ctx)`. When the span context
is valid, it appends `trace_id` (32-char hex) and `span_id` (16-char hex) as slog attributes.

**Key implementation details:**

- Use `trace.SpanContextFromContext(ctx)` (not `SpanFromContext`) — avoids creating a
  no-op span when the context has no span.
- Check `sc.IsValid()` — returns false when tracing is disabled or IDs are zero.
  This ensures no empty string fields are emitted.
- Field names use snake_case (`trace_id`, `span_id`) to match the VibeWarden log schema
  (`schema_version`, `event_type`, etc.).

### Log field order

`schema_version`, `event_type`, `timestamp`, `ai_summary`, `payload`, `trace_id`, `span_id`

The last two fields are absent when tracing is disabled.

---

## From ADR-021 — Include trace_id in JSON Error Responses

**Date**: 2026-03-28 | **Issue**: #309

ADR-021 completed the correlation loop: error responses (429, 403, 503, 401) include the
same `trace_id` that appears in the corresponding log line.

`internal/middleware/error_response.go` provides `WriteErrorResponse` and
`WriteRateLimitResponse` helpers used by all middleware and handlers.

**Correlation ID strategy:**

- When a valid span context exists: use `trace_id` (matches the log line)
- When tracing is disabled: generate a `request_id` with format `req_` + 12 base32 chars

**JSON error response structure:**

```go
type ErrorResponse struct {
    Error             string `json:"error"`
    Status            int    `json:"status"`
    Message           string `json:"message,omitempty"`
    TraceID           string `json:"trace_id,omitempty"`
    RequestID         string `json:"request_id,omitempty"`
    RetryAfterSeconds int    `json:"retry_after_seconds,omitempty"`
}
```

`TraceID` and `RequestID` are mutually exclusive. `TraceID` is present when OTel tracing
is active; `RequestID` is present otherwise.

### Coverage

All middleware error responses are now JSON:
- Rate limiter: 429 (was JSON), 403 unidentified client (was plain text → now JSON)
- Auth middleware: 503 auth provider unavailable (was plain text → now JSON)
- IP filter Caddy handler: 403 (was plain text → now JSON)
- Admin auth middleware: 401 (was plain text → now JSON)

**Out of scope**: Body size 413 (handled by `http.MaxBytesReader` / net/http — cannot
intercept without wrapping `ResponseWriter`).
