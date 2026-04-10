package middleware_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	oteladapter "github.com/vibewarden/vibewarden/internal/adapters/otel"
	"github.com/vibewarden/vibewarden/internal/middleware"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// identityPath is a normalizer that returns the path unchanged.
func identityPath(p string) string { return p }

// mockPropagator implements ports.TextMapPropagator for testing.
// It records Extract and Inject calls and optionally stores a context key/value
// to simulate extracting a parent trace context.
type mockPropagator struct {
	ExtractCalls []http.Header
	InjectCalls  []http.Header
	// ContextKey and ContextValue are injected into the extracted context when set.
	ContextKey   interface{}
	ContextValue interface{}
}

func (m *mockPropagator) Extract(ctx context.Context, carrier ports.TextMapCarrier) context.Context {
	// Record which headers were seen.
	if hc, ok := carrier.(interface{ Keys() []string }); ok {
		_ = hc.Keys()
	}
	// Build a header snapshot for assertions.
	h := http.Header{}
	for _, k := range carrier.Keys() {
		h.Set(k, carrier.Get(k))
	}
	m.ExtractCalls = append(m.ExtractCalls, h)
	if m.ContextKey != nil {
		ctx = context.WithValue(ctx, m.ContextKey, m.ContextValue)
	}
	return ctx
}

func (m *mockPropagator) Inject(ctx context.Context, carrier ports.TextMapCarrier) {
	h := http.Header{}
	for _, k := range carrier.Keys() {
		h.Set(k, carrier.Get(k))
	}
	m.InjectCalls = append(m.InjectCalls, h)
}

func TestTracingMiddleware_CreatesSpan(t *testing.T) {
	mock := &oteladapter.MockTracer{}
	handler := middleware.TracingMiddleware(mock, identityPath, nil)(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
	)

	req := httptest.NewRequest(http.MethodGet, "/api/hello", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if len(mock.StartCalls) != 1 {
		t.Fatalf("expected 1 span start, got %d", len(mock.StartCalls))
	}
	if mock.StartCalls[0].Name != "HTTP GET" {
		t.Errorf("span name = %q, want %q", mock.StartCalls[0].Name, "HTTP GET")
	}
}

func TestTracingMiddleware_SetsAttributes(t *testing.T) {
	span := &oteladapter.MockSpan{}
	mock := &oteladapter.MockTracer{SpanToReturn: span}
	handler := middleware.TracingMiddleware(mock, identityPath, nil)(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
	)

	req := httptest.NewRequest(http.MethodPost, "/users/42", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	attrMap := make(map[string]string)
	for _, a := range span.Attrs {
		attrMap[a.Key] = a.Value
	}

	tests := []struct {
		key  string
		want string
	}{
		{"http.request.method", "POST"},
		{"url.path", "/users/42"},
		{"http.route", "/users/42"},
		{"http.response.status_code", "200"},
	}
	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			if got := attrMap[tt.key]; got != tt.want {
				t.Errorf("attr[%q] = %q, want %q", tt.key, got, tt.want)
			}
		})
	}
}

func TestTracingMiddleware_SetsNormalizedRoute(t *testing.T) {
	span := &oteladapter.MockSpan{}
	mock := &oteladapter.MockTracer{SpanToReturn: span}

	normalize := func(p string) string {
		if p == "/users/42" {
			return "/users/:id"
		}
		return p
	}

	handler := middleware.TracingMiddleware(mock, normalize, nil)(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
	)

	req := httptest.NewRequest(http.MethodGet, "/users/42", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	attrMap := make(map[string]string)
	for _, a := range span.Attrs {
		attrMap[a.Key] = a.Value
	}
	if got := attrMap["http.route"]; got != "/users/:id" {
		t.Errorf("http.route = %q, want %q", got, "/users/:id")
	}
}

func TestTracingMiddleware_SkipsInternalPaths(t *testing.T) {
	tests := []struct {
		name string
		path string
	}{
		{"metrics", "/_vibewarden/metrics"},
		{"health", "/_vibewarden/health"},
		{"admin", "/_vibewarden/admin/users"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &oteladapter.MockTracer{}
			handler := middleware.TracingMiddleware(mock, identityPath, nil)(
				http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusOK)
				}),
			)

			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)

			if len(mock.StartCalls) != 0 {
				t.Errorf("expected 0 span starts for path %q, got %d", tt.path, len(mock.StartCalls))
			}
		})
	}
}

func TestTracingMiddleware_CapturesStatusCode(t *testing.T) {
	tests := []struct {
		name       string
		writeCode  int
		wantStatus string
	}{
		{"200 ok", http.StatusOK, "200"},
		{"201 created", http.StatusCreated, "201"},
		{"404 not found", http.StatusNotFound, "404"},
		{"500 internal error", http.StatusInternalServerError, "500"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			span := &oteladapter.MockSpan{}
			mock := &oteladapter.MockTracer{SpanToReturn: span}

			handler := middleware.TracingMiddleware(mock, identityPath, nil)(
				http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(tt.writeCode)
				}),
			)

			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)

			attrMap := make(map[string]string)
			for _, a := range span.Attrs {
				attrMap[a.Key] = a.Value
			}
			if got := attrMap["http.response.status_code"]; got != tt.wantStatus {
				t.Errorf("http.response.status_code = %q, want %q", got, tt.wantStatus)
			}
		})
	}
}

func TestTracingMiddleware_SetsErrorStatus(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		wantStatus ports.SpanStatusCode
	}{
		{"500 is error", http.StatusInternalServerError, ports.SpanStatusError},
		{"503 is error", http.StatusServiceUnavailable, ports.SpanStatusError},
		{"200 is ok", http.StatusOK, ports.SpanStatusOK},
		{"404 is ok", http.StatusNotFound, ports.SpanStatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			span := &oteladapter.MockSpan{}
			mock := &oteladapter.MockTracer{SpanToReturn: span}

			handler := middleware.TracingMiddleware(mock, identityPath, nil)(
				http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(tt.statusCode)
				}),
			)

			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)

			if span.StatusCode != tt.wantStatus {
				t.Errorf("span.StatusCode = %v, want %v", span.StatusCode, tt.wantStatus)
			}
		})
	}
}

func TestTracingMiddleware_SpanEndedAfterRequest(t *testing.T) {
	span := &oteladapter.MockSpan{}
	mock := &oteladapter.MockTracer{SpanToReturn: span}

	handler := middleware.TracingMiddleware(mock, identityPath, nil)(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if span.Ended {
				t.Error("span should not be ended during request handling")
			}
			w.WriteHeader(http.StatusOK)
		}),
	)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if !span.Ended {
		t.Error("span should be ended after request completes")
	}
}

func TestTracingMiddleware_UsesSpanKindServer(t *testing.T) {
	mock := &oteladapter.MockTracer{}
	handler := middleware.TracingMiddleware(mock, identityPath, nil)(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
	)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if len(mock.StartCalls) == 0 {
		t.Fatal("no span started")
	}
	call := mock.StartCalls[0]
	kind := ports.KindOf(call.Opts)
	if kind != ports.SpanKindServer {
		t.Errorf("span kind = %v, want SpanKindServer", kind)
	}
}

func TestTracingMiddleware_DefaultStatusCode200(t *testing.T) {
	span := &oteladapter.MockSpan{}
	mock := &oteladapter.MockTracer{SpanToReturn: span}

	handler := middleware.TracingMiddleware(mock, identityPath, nil)(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Write body without calling WriteHeader — should default to 200.
			_, _ = w.Write([]byte("hello"))
		}),
	)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	attrMap := make(map[string]string)
	for _, a := range span.Attrs {
		attrMap[a.Key] = a.Value
	}
	if got := attrMap["http.response.status_code"]; got != "200" {
		t.Errorf("http.response.status_code = %q, want %q", got, "200")
	}
}

// ---------------------------------------------------------------------------
// Propagation tests
// ---------------------------------------------------------------------------

func TestTracingMiddleware_NilPropagator_DoesNotPanic(t *testing.T) {
	mock := &oteladapter.MockTracer{}
	handler := middleware.TracingMiddleware(mock, identityPath, nil)(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
	)

	req := httptest.NewRequest(http.MethodGet, "/api/hello", nil)
	req.Header.Set("Traceparent", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")
	rr := httptest.NewRecorder()
	// Must not panic even when propagator is nil.
	handler.ServeHTTP(rr, req)

	if len(mock.StartCalls) != 1 {
		t.Fatalf("expected 1 span start, got %d", len(mock.StartCalls))
	}
}

func TestTracingMiddleware_WithPropagator_CallsExtract(t *testing.T) {
	mock := &oteladapter.MockTracer{}
	prop := &mockPropagator{}

	handler := middleware.TracingMiddleware(mock, identityPath, prop)(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
	)

	req := httptest.NewRequest(http.MethodGet, "/api/hello", nil)
	req.Header.Set("Traceparent", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if len(prop.ExtractCalls) != 1 {
		t.Fatalf("expected 1 Extract call, got %d", len(prop.ExtractCalls))
	}
}

func TestTracingMiddleware_WithPropagator_SkipsExtractForInternalPaths(t *testing.T) {
	mock := &oteladapter.MockTracer{}
	prop := &mockPropagator{}

	handler := middleware.TracingMiddleware(mock, identityPath, prop)(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
	)

	req := httptest.NewRequest(http.MethodGet, "/_vibewarden/metrics", nil)
	req.Header.Set("Traceparent", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if len(prop.ExtractCalls) != 0 {
		t.Errorf("expected 0 Extract calls for internal path, got %d", len(prop.ExtractCalls))
	}
	if len(mock.StartCalls) != 0 {
		t.Errorf("expected 0 span starts for internal path, got %d", len(mock.StartCalls))
	}
}

type contextKey string

func TestTracingMiddleware_WithPropagator_ExtractedContextPassedToSpan(t *testing.T) {
	type ctxKey = contextKey
	const key ctxKey = "parent-trace-id"
	const wantVal = "test-parent"

	// The mock propagator will inject a value into the context during Extract.
	prop := &mockPropagator{ContextKey: key, ContextValue: wantVal}

	var gotVal interface{}
	mock := &oteladapter.MockTracer{}

	handler := middleware.TracingMiddleware(mock, identityPath, prop)(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// The context passed to ServeHTTP should contain the extracted value.
			gotVal = r.Context().Value(key)
			w.WriteHeader(http.StatusOK)
		}),
	)

	req := httptest.NewRequest(http.MethodGet, "/api/hello", nil)
	req.Header.Set("Traceparent", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if gotVal != wantVal {
		t.Errorf("extracted context value = %v, want %v", gotVal, wantVal)
	}
}

func TestTracingMiddleware_WithPropagator_SpanStartedAfterExtract(t *testing.T) {
	// Verify that tracer.Start is called once even with a propagator present.
	prop := &mockPropagator{}
	mock := &oteladapter.MockTracer{}

	handler := middleware.TracingMiddleware(mock, identityPath, prop)(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
	)

	req := httptest.NewRequest(http.MethodPut, "/resource/1", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if len(mock.StartCalls) != 1 {
		t.Fatalf("expected 1 span start, got %d", len(mock.StartCalls))
	}
	if mock.StartCalls[0].Name != "HTTP PUT" {
		t.Errorf("span name = %q, want %q", mock.StartCalls[0].Name, "HTTP PUT")
	}
	if len(prop.ExtractCalls) != 1 {
		t.Errorf("expected 1 Extract call, got %d", len(prop.ExtractCalls))
	}
}
