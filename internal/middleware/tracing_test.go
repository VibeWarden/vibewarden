package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	oteladapter "github.com/vibewarden/vibewarden/internal/adapters/otel"
	"github.com/vibewarden/vibewarden/internal/middleware"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// identityPath is a normalizer that returns the path unchanged.
func identityPath(p string) string { return p }

func TestTracingMiddleware_CreatesSpan(t *testing.T) {
	mock := &oteladapter.MockTracer{}
	handler := middleware.TracingMiddleware(mock, identityPath)(
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
	handler := middleware.TracingMiddleware(mock, identityPath)(
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

	handler := middleware.TracingMiddleware(mock, normalize)(
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
			handler := middleware.TracingMiddleware(mock, identityPath)(
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

			handler := middleware.TracingMiddleware(mock, identityPath)(
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

			handler := middleware.TracingMiddleware(mock, identityPath)(
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

	handler := middleware.TracingMiddleware(mock, identityPath)(
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
	handler := middleware.TracingMiddleware(mock, identityPath)(
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

	handler := middleware.TracingMiddleware(mock, identityPath)(
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
