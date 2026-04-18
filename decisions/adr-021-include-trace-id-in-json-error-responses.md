# ADR-021: Include trace_id in JSON Error Responses

**Date**: 2026-03-28
**Issue**: #309
**Status**: Accepted

### Context

Epic #306 ("Distributed tracing with request correlation") requires that users can correlate
error responses with sidecar logs. Currently, when a user encounters a 429, 403, or 500 error,
they receive a JSON response like:

```json
{"error": "rate_limit_exceeded", "retry_after_seconds": 5}
```

There is no way to correlate this response with the corresponding log entry in the sidecar.
Support tickets often say "I got a 429" with no way to find the exact request in logs.

ADR-019 introduced TracingMiddleware which creates a span for each request and stores the
span context in the request context. ADR-020 injected trace_id and span_id into log lines.
This story completes the correlation loop by including the trace_id (or a fallback request_id)
in the JSON error response body.

**Error response locations in the codebase:**

1. **Rate limiter middleware** (`internal/middleware/ratelimit.go`):
   - 429 Too Many Requests with JSON body `{"error":"rate_limit_exceeded","retry_after_seconds":N}`
   - 403 Forbidden (unidentified client) with plain text

2. **Auth middleware** (`internal/middleware/auth.go`):
   - 503 Service Unavailable (Kratos unavailable) with plain text
   - Redirects (302) do not need trace_id

3. **IP filter Caddy handler** (`internal/adapters/caddy/ipfilter_handler.go`):
   - 403 Forbidden with plain text

4. **Body size Caddy handler** (`internal/adapters/caddy/bodysize_handler.go`):
   - 413 Payload Too Large via `http.MaxBytesReader` (handled by net/http, not our code)

5. **Admin auth middleware** (`internal/middleware/admin_auth.go`):
   - 401 Unauthorized with plain text

**Constraints:**

- Must include `trace_id` when tracing is enabled (span context is valid)
- Must include `request_id` as fallback when tracing is disabled
- The ID in the response must match the ID in the corresponding log line
- Must not break existing API contracts (additive field only)
- Plain text responses should become JSON responses for consistency

### Decision

Create a shared error response helper in `internal/middleware/error_response.go` that:

1. Extracts trace_id from the span context when available
2. Generates a lightweight request_id when tracing is disabled
3. Writes a consistent JSON error response with the correlation ID
4. Stores the correlation ID in the request context for logging

All middleware and handlers that return error responses will use this helper.

#### Domain Model Changes

None. Request IDs and trace IDs are observability infrastructure, not domain concepts.

#### Ports (Interfaces)

No new ports required. The helper is a pure function that operates on `context.Context`
and `http.ResponseWriter`. It does not need abstraction since it is tightly coupled to
HTTP error handling.

#### Adapters

**New file: `internal/middleware/error_response.go`**

Provides a centralized helper for writing JSON error responses with correlation IDs:

```go
// ErrorResponse is the JSON structure for all error responses from VibeWarden middleware.
// It always includes a correlation ID for log matching.
type ErrorResponse struct {
    // Error is the machine-readable error code (e.g., "rate_limit_exceeded", "forbidden").
    Error string `json:"error"`

    // Status is the HTTP status code.
    Status int `json:"status"`

    // Message is a human-readable error description (optional).
    Message string `json:"message,omitempty"`

    // TraceID is the OTel trace ID when tracing is enabled.
    // Mutually exclusive with RequestID.
    TraceID string `json:"trace_id,omitempty"`

    // RequestID is a generated ID when tracing is disabled.
    // Mutually exclusive with TraceID.
    RequestID string `json:"request_id,omitempty"`

    // RetryAfterSeconds is set only for 429 responses.
    RetryAfterSeconds int `json:"retry_after_seconds,omitempty"`
}

// WriteErrorResponse writes a JSON error response with a correlation ID.
// When the context contains a valid OTel span context, trace_id is used.
// Otherwise, a request_id is generated.
//
// The correlation ID is also stored in the request context under the
// correlationIDKey for use by event logging.
func WriteErrorResponse(w http.ResponseWriter, r *http.Request, status int, errorCode, message string) {
    // Extract trace_id or generate request_id
    var traceID, requestID string
    if sc := trace.SpanContextFromContext(r.Context()); sc.IsValid() {
        traceID = sc.TraceID().String()
    } else {
        requestID = generateRequestID()
    }

    resp := ErrorResponse{
        Error:     errorCode,
        Status:    status,
        Message:   message,
        TraceID:   traceID,
        RequestID: requestID,
    }

    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(status)
    _ = json.NewEncoder(w).Encode(resp)
}

// WriteRateLimitResponse writes a 429 response with retry information.
// It sets the Retry-After header and includes retry_after_seconds in the body.
func WriteRateLimitResponse(w http.ResponseWriter, r *http.Request, retryAfterSeconds int) {
    var traceID, requestID string
    if sc := trace.SpanContextFromContext(r.Context()); sc.IsValid() {
        traceID = sc.TraceID().String()
    } else {
        requestID = generateRequestID()
    }

    resp := ErrorResponse{
        Error:             "rate_limit_exceeded",
        Status:            http.StatusTooManyRequests,
        TraceID:           traceID,
        RequestID:         requestID,
        RetryAfterSeconds: retryAfterSeconds,
    }

    w.Header().Set("Content-Type", "application/json")
    w.Header().Set("Retry-After", strconv.Itoa(retryAfterSeconds))
    w.WriteHeader(http.StatusTooManyRequests)
    _ = json.NewEncoder(w).Encode(resp)
}

// generateRequestID creates a short, URL-safe request ID.
// Format: "req_" + 12 random base32 characters (e.g., "req_A3BKDMF7HQLN").
// Uses crypto/rand for unpredictability.
func generateRequestID() string {
    b := make([]byte, 8) // 8 bytes = 64 bits of randomness
    _, _ = rand.Read(b)  // crypto/rand never fails for 8 bytes
    return "req_" + base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b)[:12]
}

// CorrelationID extracts the trace_id or request_id from the context.
// Returns the trace_id if a valid span context exists, otherwise returns
// any previously stored request_id, or generates a new one.
// This is used by event loggers to ensure logs match error responses.
func CorrelationID(ctx context.Context) string {
    if sc := trace.SpanContextFromContext(ctx); sc.IsValid() {
        return sc.TraceID().String()
    }
    if id := requestIDFromContext(ctx); id != "" {
        return id
    }
    return generateRequestID()
}
```

**Request ID context storage:**

When a request_id is generated, it must be stored in the context so that subsequent
log calls include the same ID. Add context key and helpers:

```go
type contextKey int

const requestIDContextKey contextKey = iota

// ContextWithRequestID returns a new context with the request ID stored.
func ContextWithRequestID(ctx context.Context, id string) context.Context {
    return context.WithValue(ctx, requestIDContextKey, id)
}

// requestIDFromContext retrieves a previously stored request ID.
func requestIDFromContext(ctx context.Context) string {
    if id, ok := ctx.Value(requestIDContextKey).(string); ok {
        return id
    }
    return ""
}
```

**Modify: `internal/middleware/ratelimit.go`**

Replace `writeRateLimitResponse()` with the new helper:

```go
// Before:
func writeRateLimitResponse(w http.ResponseWriter, result ports.RateLimitResult) {
    retrySeconds := retryAfterSeconds(result.RetryAfter)
    w.Header().Set("Content-Type", "application/json")
    w.Header().Set("Retry-After", strconv.Itoa(retrySeconds))
    w.WriteHeader(http.StatusTooManyRequests)
    body := rateLimitErrorBody{
        Error:             "rate_limit_exceeded",
        RetryAfterSeconds: retrySeconds,
    }
    _ = json.NewEncoder(w).Encode(body)
}

// After:
// Remove writeRateLimitResponse, use WriteRateLimitResponse from error_response.go
```

Update the middleware to pass the request to the error helper:

```go
if !ipResult.Allowed {
    emitRateLimitHit(r, eventLogger, "ip", clientIP, "", ipResult)
    WriteRateLimitResponse(w, r, retryAfterSeconds(ipResult.RetryAfter))
    return
}
```

For the 403 "unidentified client" case, convert to JSON:

```go
// Before:
http.Error(w, "Forbidden", http.StatusForbidden)

// After:
WriteErrorResponse(w, r, http.StatusForbidden, "unidentified_client",
    "Could not identify client IP address")
```

**Modify: `internal/middleware/auth.go`**

Convert 503 responses to JSON:

```go
// Before:
http.Error(w, "Service Unavailable", http.StatusServiceUnavailable)

// After:
WriteErrorResponse(w, r, http.StatusServiceUnavailable, "auth_provider_unavailable",
    "Authentication service is temporarily unavailable")
```

**Modify: `internal/adapters/caddy/ipfilter_handler.go`**

Convert 403 responses to JSON. The handler needs access to the request context
for trace extraction:

```go
// Before:
http.Error(w, "Forbidden", http.StatusForbidden)

// After:
middleware.WriteErrorResponse(w, r, http.StatusForbidden, "ip_blocked",
    "Access denied by IP filter")
```

**Modify: `internal/middleware/admin_auth.go`**

Convert 401 responses to JSON:

```go
// Before:
http.Error(w, "Unauthorized", http.StatusUnauthorized)

// After:
WriteErrorResponse(w, r, http.StatusUnauthorized, "unauthorized",
    "Admin authentication required")
```

**Note on body size handler:**

The `http.MaxBytesReader` error is handled by net/http, not our code. When the
body exceeds the limit, the reader returns an error and net/http writes a 413
response. We cannot intercept this easily without wrapping the response writer,
which adds complexity. The body size handler is therefore **out of scope** for
this story. A future story could wrap the response writer to intercept 413 errors.

#### Application Service

No application service changes.

#### File Layout

**New files:**

| File | Purpose |
|------|---------|
| `internal/middleware/error_response.go` | Shared error response helper with trace/request ID |
| `internal/middleware/error_response_test.go` | Unit tests for error response helper |

**Modified files:**

| File | Changes |
|------|---------|
| `internal/middleware/ratelimit.go` | Use `WriteRateLimitResponse`, convert 403 to JSON |
| `internal/middleware/ratelimit_test.go` | Update tests for new response format |
| `internal/middleware/auth.go` | Use `WriteErrorResponse` for 503 |
| `internal/middleware/auth_test.go` | Update tests for new response format |
| `internal/middleware/admin_auth.go` | Use `WriteErrorResponse` for 401 |
| `internal/middleware/admin_auth_test.go` | Update tests for new response format |
| `internal/adapters/caddy/ipfilter_handler.go` | Use `WriteErrorResponse` for 403 |
| `internal/adapters/caddy/ipfilter_handler_test.go` | Update tests for new response format |

#### Sequence

**Error response with tracing enabled:**

1. Request arrives at VibeWarden
2. TracingMiddleware creates span, stores span context in request context
3. Rate limiter (or other middleware) determines request should be rejected
4. Middleware calls `WriteRateLimitResponse(w, r, retrySeconds)`
5. Helper extracts trace_id via `trace.SpanContextFromContext(r.Context())`
6. Helper writes JSON: `{"error":"rate_limit_exceeded","status":429,"trace_id":"abc123...","retry_after_seconds":5}`
7. Event logger also extracts trace_id from same context
8. Log line includes matching trace_id for correlation

**Error response with tracing disabled:**

1. Request arrives at VibeWarden
2. TracingMiddleware skips span creation (or is disabled)
3. Rate limiter determines request should be rejected
4. Middleware calls `WriteRateLimitResponse(w, r, retrySeconds)`
5. Helper finds no valid span context
6. Helper generates request_id: "req_A3BKDMF7HQLN"
7. Helper writes JSON: `{"error":"rate_limit_exceeded","status":429,"request_id":"req_A3BKDMF7HQLN","retry_after_seconds":5}`
8. (Future enhancement: event logger includes same request_id)

#### Error Cases

| Scenario | Handling |
|----------|----------|
| Context is nil | `SpanContextFromContext(nil)` returns invalid context; generate request_id |
| JSON encoding fails | Encoding a struct with string fields never fails; silently ignore |
| crypto/rand fails | For 8 bytes, crypto/rand never fails on any OS |
| Response already written | Standard Go behavior; second WriteHeader is ignored |

#### Test Strategy

**Unit tests in `internal/middleware/error_response_test.go`:**

| Test | What it verifies |
|------|------------------|
| `TestWriteErrorResponse_WithTraceContext` | trace_id in response when span context valid |
| `TestWriteErrorResponse_WithoutTraceContext` | request_id in response when no span context |
| `TestWriteRateLimitResponse_IncludesRetryAfter` | retry_after_seconds and Retry-After header |
| `TestGenerateRequestID_Format` | Format is "req_" + 12 chars, unique per call |
| `TestCorrelationID_WithTrace` | Returns trace_id when span context valid |
| `TestCorrelationID_WithRequestID` | Returns stored request_id when present |
| `TestCorrelationID_GeneratesNew` | Generates new ID when nothing in context |

**Updated tests in existing files:**

| File | Test changes |
|------|--------------|
| `internal/middleware/ratelimit_test.go` | Verify JSON response includes trace_id or request_id |
| `internal/middleware/auth_test.go` | Verify 503 is JSON with trace_id or request_id |
| `internal/middleware/admin_auth_test.go` | Verify 401 is JSON with trace_id or request_id |
| `internal/adapters/caddy/ipfilter_handler_test.go` | Verify 403 is JSON with trace_id or request_id |

**Test helper for creating span context:**

Reuse the pattern from ADR-020 tests:

```go
func withTraceContext(ctx context.Context) context.Context {
    tp := sdktrace.NewTracerProvider()
    tracer := tp.Tracer("test")
    ctx, _ = tracer.Start(ctx, "test-span")
    return ctx
}
```

**What to mock vs. real:**

- Real: OTel SDK for span context (cheap, no external calls)
- Real: crypto/rand for request ID generation (deterministic enough for tests)
- Mock: Nothing needed

#### New Dependencies

**None.** All required packages are already in use:

| Package | Status | License |
|---------|--------|---------|
| `go.opentelemetry.io/otel/trace` | Already direct (ADR-019/020) | Apache 2.0 |
| `crypto/rand` | stdlib | N/A |
| `encoding/base32` | stdlib | N/A |

### Consequences

**Positive:**

- Every error response includes a correlation ID (trace_id or request_id)
- Users can include the ID in support tickets for fast log lookup
- IDs match between response and log lines (same extraction logic)
- Consistent JSON format for all error responses
- No new dependencies

**Negative:**

- Breaking change to response format (plain text -> JSON) for some errors
- Body size 413 errors are not covered (net/http limitation)
- Slightly larger response bodies (~50 bytes for ID)

**Trade-offs:**

- **JSON vs. plain text for all errors:** Chose JSON for consistency and machine
  readability. The target vibe coder audience is building APIs, so JSON errors
  are expected. Frontends parsing "Forbidden" text strings is fragile anyway.

- **trace_id vs. request_id field naming:** Chose separate fields (`trace_id`
  when tracing enabled, `request_id` when disabled) rather than a generic
  `correlation_id`. This makes it explicit which ID type is being used and
  matches the log field names from ADR-020.

- **Request ID format:** Chose "req_" prefix + base32 for:
  - Human recognizable as a request ID
  - URL-safe (no special characters)
  - Short (16 chars total) but sufficient entropy (64 bits)
  - Different format from trace_id (32 hex chars) to avoid confusion

- **Body size 413 not covered:** Chose to exclude rather than wrap ResponseWriter.
  The added complexity of intercepting net/http errors is not worth it for v1.
  Body size errors are rare (misconfigured clients) and the rate limiter/auth
  paths are the common support ticket cases.

---
