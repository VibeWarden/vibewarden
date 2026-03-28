package middleware

import (
	"context"
	"crypto/rand"
	"encoding/base32"
	"encoding/json"
	"net/http"
	"strconv"

	"go.opentelemetry.io/otel/trace"
)

const (
	// requestIDContextKey is the context key under which a generated request ID
	// is stored when tracing is disabled.
	requestIDContextKey contextKey = iota + 1
)

// ErrorResponse is the JSON structure returned by all VibeWarden middleware
// error responses. It always includes a correlation ID (trace_id or request_id)
// to allow users to match the response to the corresponding log line.
type ErrorResponse struct {
	// Error is the machine-readable error code (e.g., "rate_limit_exceeded",
	// "forbidden", "unauthorized").
	Error string `json:"error"`

	// Status is the HTTP status code.
	Status int `json:"status"`

	// Message is a human-readable description of the error. Omitted when empty.
	Message string `json:"message,omitempty"`

	// TraceID is the OTel trace ID when tracing is enabled. Mutually exclusive
	// with RequestID.
	TraceID string `json:"trace_id,omitempty"`

	// RequestID is a generated correlation ID used when tracing is disabled.
	// Mutually exclusive with TraceID.
	RequestID string `json:"request_id,omitempty"`

	// RetryAfterSeconds is included only in 429 Too Many Requests responses.
	RetryAfterSeconds int `json:"retry_after_seconds,omitempty"`
}

// WriteErrorResponse writes a JSON error response to w. When the request
// context contains a valid OTel span context, the trace_id field is
// populated. Otherwise a short request_id is generated and stored in the
// request context for subsequent log correlation.
//
// The function sets Content-Type: application/json and writes the given
// HTTP status code before encoding the body. Any encoding error is silently
// discarded because the status code has already been committed.
func WriteErrorResponse(w http.ResponseWriter, r *http.Request, status int, errorCode, message string) {
	traceID, requestID := correlationPair(r.Context())

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

// WriteRateLimitResponse writes a 429 Too Many Requests JSON error response to w.
// It sets the Retry-After header and includes retry_after_seconds in the body.
// A trace_id or request_id is included for log correlation following the same
// rules as WriteErrorResponse.
func WriteRateLimitResponse(w http.ResponseWriter, r *http.Request, retryAfterSeconds int) {
	traceID, requestID := correlationPair(r.Context())

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

// CorrelationID returns the correlation ID for the given context. When the
// context contains a valid OTel span context, the trace ID is returned.
// When the context carries a previously generated request ID (stored by
// WriteErrorResponse or WriteRateLimitResponse), that ID is returned.
// Otherwise a new request ID is generated. The generated ID is NOT stored;
// callers that need persistence must use ContextWithRequestID explicitly.
func CorrelationID(ctx context.Context) string {
	if sc := trace.SpanContextFromContext(ctx); sc.IsValid() {
		return sc.TraceID().String()
	}
	if id := requestIDFromContext(ctx); id != "" {
		return id
	}
	return generateRequestID()
}

// ContextWithRequestID returns a new context derived from ctx that carries the
// given request ID. Subsequent calls to CorrelationID on the returned context
// will return this ID (unless a valid span context is also present).
func ContextWithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDContextKey, id)
}

// requestIDFromContext retrieves a previously stored request ID from ctx.
// Returns "" when no request ID is stored.
func requestIDFromContext(ctx context.Context) string {
	id, _ := ctx.Value(requestIDContextKey).(string)
	return id
}

// correlationPair returns (traceID, "") when the context has a valid span, or
// ("", requestID) otherwise. The requestID is freshly generated; it is not
// stored in the context because WriteErrorResponse and WriteRateLimitResponse
// write the response immediately and do not need subsequent retrieval.
func correlationPair(ctx context.Context) (traceID, requestID string) {
	if sc := trace.SpanContextFromContext(ctx); sc.IsValid() {
		return sc.TraceID().String(), ""
	}
	return "", generateRequestID()
}

// generateRequestID creates a short, URL-safe request ID with the prefix "req_"
// followed by 12 base32 characters (no padding). It uses 8 bytes (64 bits) of
// randomness from crypto/rand. The format is distinct from OTel trace IDs
// (32 hex chars) to prevent confusion.
//
// Example: "req_A3BKDMF7HQLN"
func generateRequestID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b) // crypto/rand never fails for small byte slices
	return "req_" + base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b)[:12]
}
