package middleware

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// withSpanContext returns a context that carries a valid OTel span context.
// It uses the real OTel SDK so that SpanContextFromContext returns a valid,
// non-zero TraceID and SpanID — no mocks needed.
func withSpanContext(ctx context.Context) (context.Context, func()) {
	tp := sdktrace.NewTracerProvider()
	tracer := tp.Tracer("test")
	ctx, span := tracer.Start(ctx, "test-span")
	return ctx, func() { span.End() }
}

// decodeErrorResponse decodes the response body as an ErrorResponse.
func decodeErrorResponse(t *testing.T, w *httptest.ResponseRecorder) ErrorResponse {
	t.Helper()
	var resp ErrorResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode error response body: %v (body=%q)", err, w.Body.String())
	}
	return resp
}

// TestWriteErrorResponse_WithTraceContext verifies that when the request
// context carries a valid OTel span, the trace_id field is populated and
// request_id is absent.
func TestWriteErrorResponse_WithTraceContext(t *testing.T) {
	ctx, end := withSpanContext(context.Background())
	defer end()

	req := httptest.NewRequest(http.MethodGet, "/api", nil).WithContext(ctx)
	w := httptest.NewRecorder()

	WriteErrorResponse(w, req, http.StatusForbidden, "forbidden", "test message")

	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d", w.Code, http.StatusForbidden)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want %q", ct, "application/json")
	}

	resp := decodeErrorResponse(t, w)

	if resp.Error != "forbidden" {
		t.Errorf("error = %q, want %q", resp.Error, "forbidden")
	}
	if resp.Status != http.StatusForbidden {
		t.Errorf("status = %d, want %d", resp.Status, http.StatusForbidden)
	}
	if resp.Message != "test message" {
		t.Errorf("message = %q, want %q", resp.Message, "test message")
	}
	if len(resp.TraceID) != 32 {
		t.Errorf("trace_id = %q, want 32-char hex string", resp.TraceID)
	}
	if resp.RequestID != "" {
		t.Errorf("request_id = %q, want empty when trace is available", resp.RequestID)
	}
}

// TestWriteErrorResponse_WithoutTraceContext verifies that when the request
// context has no span, a request_id is generated and trace_id is absent.
func TestWriteErrorResponse_WithoutTraceContext(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api", nil)
	w := httptest.NewRecorder()

	WriteErrorResponse(w, req, http.StatusServiceUnavailable, "auth_provider_unavailable", "")

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want %d", w.Code, http.StatusServiceUnavailable)
	}

	resp := decodeErrorResponse(t, w)

	if resp.TraceID != "" {
		t.Errorf("trace_id = %q, want empty when tracing disabled", resp.TraceID)
	}
	if !strings.HasPrefix(resp.RequestID, "req_") {
		t.Errorf("request_id = %q, want prefix %q", resp.RequestID, "req_")
	}
	if len(resp.RequestID) != 16 {
		// "req_" (4) + 12 base32 chars = 16 total
		t.Errorf("request_id length = %d, want 16", len(resp.RequestID))
	}
}

// TestWriteErrorResponse_EmptyMessage verifies that an empty message field is
// omitted from the JSON output (omitempty).
func TestWriteErrorResponse_EmptyMessage(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api", nil)
	w := httptest.NewRecorder()

	WriteErrorResponse(w, req, http.StatusUnauthorized, "unauthorized", "")

	var raw map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if _, ok := raw["message"]; ok {
		t.Error("message field should be absent when empty (omitempty)")
	}
}

// TestWriteRateLimitResponse_WithTraceContext verifies that the 429 response
// includes trace_id, Retry-After header, and retry_after_seconds in the body.
func TestWriteRateLimitResponse_WithTraceContext(t *testing.T) {
	ctx, end := withSpanContext(context.Background())
	defer end()

	req := httptest.NewRequest(http.MethodGet, "/api", nil).WithContext(ctx)
	w := httptest.NewRecorder()

	WriteRateLimitResponse(w, req, 5)

	if w.Code != http.StatusTooManyRequests {
		t.Errorf("status = %d, want %d", w.Code, http.StatusTooManyRequests)
	}
	if got := w.Header().Get("Retry-After"); got != "5" {
		t.Errorf("Retry-After = %q, want %q", got, "5")
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want %q", ct, "application/json")
	}

	resp := decodeErrorResponse(t, w)

	if resp.Error != "rate_limit_exceeded" {
		t.Errorf("error = %q, want %q", resp.Error, "rate_limit_exceeded")
	}
	if resp.Status != http.StatusTooManyRequests {
		t.Errorf("status = %d, want %d", resp.Status, http.StatusTooManyRequests)
	}
	if resp.RetryAfterSeconds != 5 {
		t.Errorf("retry_after_seconds = %d, want 5", resp.RetryAfterSeconds)
	}
	if len(resp.TraceID) != 32 {
		t.Errorf("trace_id = %q, want 32-char hex string", resp.TraceID)
	}
	if resp.RequestID != "" {
		t.Errorf("request_id = %q, want empty when trace available", resp.RequestID)
	}
}

// TestWriteRateLimitResponse_WithoutTraceContext verifies that the 429 response
// includes a generated request_id when tracing is disabled.
func TestWriteRateLimitResponse_WithoutTraceContext(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api", nil)
	w := httptest.NewRecorder()

	WriteRateLimitResponse(w, req, 3)

	resp := decodeErrorResponse(t, w)

	if resp.TraceID != "" {
		t.Errorf("trace_id = %q, want empty when tracing disabled", resp.TraceID)
	}
	if !strings.HasPrefix(resp.RequestID, "req_") {
		t.Errorf("request_id = %q, want prefix %q", resp.RequestID, "req_")
	}
	if resp.RetryAfterSeconds != 3 {
		t.Errorf("retry_after_seconds = %d, want 3", resp.RetryAfterSeconds)
	}
}

// TestWriteRateLimitResponse_IncludesRetryAfterSeconds verifies the mapping
// between the retryAfterSeconds argument and both the header and body field.
func TestWriteRateLimitResponse_IncludesRetryAfterSeconds(t *testing.T) {
	tests := []struct {
		name    string
		seconds int
		want    string
	}{
		{"zero", 0, "0"},
		{"one second", 1, "1"},
		{"ten seconds", 10, "10"},
		{"large value", 300, "300"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			w := httptest.NewRecorder()

			WriteRateLimitResponse(w, req, tt.seconds)

			if got := w.Header().Get("Retry-After"); got != tt.want {
				t.Errorf("Retry-After header = %q, want %q", got, tt.want)
			}
			resp := decodeErrorResponse(t, w)
			if resp.RetryAfterSeconds != tt.seconds {
				t.Errorf("retry_after_seconds = %d, want %d", resp.RetryAfterSeconds, tt.seconds)
			}
		})
	}
}

// TestGenerateRequestID_Format verifies that generated IDs have the expected
// prefix and total length, and that consecutive calls produce distinct values.
func TestGenerateRequestID_Format(t *testing.T) {
	id := generateRequestID()

	if !strings.HasPrefix(id, "req_") {
		t.Errorf("generateRequestID() = %q, want prefix %q", id, "req_")
	}
	if len(id) != 16 {
		t.Errorf("generateRequestID() length = %d, want 16", len(id))
	}

	// Verify uniqueness across multiple calls.
	seen := make(map[string]bool)
	for i := 0; i < 100; i++ {
		next := generateRequestID()
		if seen[next] {
			t.Errorf("generateRequestID() produced duplicate: %q", next)
			break
		}
		seen[next] = true
	}
}

// TestCorrelationID_WithTrace verifies that CorrelationID returns the trace ID
// when the context has a valid OTel span.
func TestCorrelationID_WithTrace(t *testing.T) {
	ctx, end := withSpanContext(context.Background())
	defer end()

	id := CorrelationID(ctx)
	if len(id) != 32 {
		t.Errorf("CorrelationID with trace = %q, want 32-char hex string", id)
	}
}

// TestCorrelationID_WithRequestID verifies that CorrelationID returns the
// stored request ID when no span context is present.
func TestCorrelationID_WithRequestID(t *testing.T) {
	ctx := ContextWithRequestID(context.Background(), "req_TESTID123456")

	id := CorrelationID(ctx)
	if id != "req_TESTID123456" {
		t.Errorf("CorrelationID with request_id = %q, want %q", id, "req_TESTID123456")
	}
}

// TestCorrelationID_GeneratesNew verifies that CorrelationID generates a new
// request ID when no span context and no stored request ID are present.
func TestCorrelationID_GeneratesNew(t *testing.T) {
	id := CorrelationID(context.Background())
	if !strings.HasPrefix(id, "req_") {
		t.Errorf("CorrelationID without context = %q, want prefix %q", id, "req_")
	}
	if len(id) != 16 {
		t.Errorf("CorrelationID without context length = %d, want 16", len(id))
	}
}

// TestContextWithRequestID_RoundTrip verifies that ContextWithRequestID stores
// the ID and CorrelationID retrieves it correctly.
func TestContextWithRequestID_RoundTrip(t *testing.T) {
	want := "req_ABCDEFGHIJKL"
	ctx := ContextWithRequestID(context.Background(), want)

	got := CorrelationID(ctx)
	if got != want {
		t.Errorf("CorrelationID = %q, want %q", got, want)
	}
}

// TestCorrelationID_TracePreferredOverRequestID verifies that when both a span
// context and a stored request ID are present, the trace ID takes precedence.
func TestCorrelationID_TracePreferredOverRequestID(t *testing.T) {
	ctx, end := withSpanContext(context.Background())
	defer end()
	ctx = ContextWithRequestID(ctx, "req_SHOULDNOTAPPEAR")

	id := CorrelationID(ctx)
	if strings.HasPrefix(id, "req_") {
		t.Errorf("CorrelationID = %q: trace should take precedence over request_id", id)
	}
	if len(id) != 32 {
		t.Errorf("CorrelationID = %q, want 32-char trace ID", id)
	}
}
