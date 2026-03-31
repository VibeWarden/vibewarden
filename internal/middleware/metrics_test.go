package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	vibemetrics "github.com/vibewarden/vibewarden/internal/adapters/metrics"
	"github.com/vibewarden/vibewarden/internal/domain/resilience"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// fakeMetricsCollector is a simple in-memory implementation of ports.MetricsCollector
// used in middleware unit tests.
type fakeMetricsCollector struct {
	requestTotalCalls []requestTotalCall
	durationCalls     []durationCall
	rateLimitCalls    []string
	authCalls         []string
	upstreamErrors    int
	activeConnections int
}

type requestTotalCall struct {
	method      string
	statusCode  string
	pathPattern string
}

type durationCall struct {
	method      string
	pathPattern string
	duration    time.Duration
}

func (f *fakeMetricsCollector) IncRequestTotal(method, statusCode, pathPattern string) {
	f.requestTotalCalls = append(f.requestTotalCalls, requestTotalCall{method, statusCode, pathPattern})
}

func (f *fakeMetricsCollector) ObserveRequestDuration(method, pathPattern string, duration time.Duration) {
	f.durationCalls = append(f.durationCalls, durationCall{method, pathPattern, duration})
}

func (f *fakeMetricsCollector) IncRateLimitHit(limitType string) {
	f.rateLimitCalls = append(f.rateLimitCalls, limitType)
}

func (f *fakeMetricsCollector) IncAuthDecision(decision string) {
	f.authCalls = append(f.authCalls, decision)
}

func (f *fakeMetricsCollector) IncUpstreamError() {
	f.upstreamErrors++
}

func (f *fakeMetricsCollector) IncUpstreamTimeout() {}

func (f *fakeMetricsCollector) IncUpstreamRetry(_ string) {}

func (f *fakeMetricsCollector) SetActiveConnections(n int) {
	f.activeConnections = n
}

// SetCircuitBreakerState implements ports.MetricsCollector and does nothing.
func (f *fakeMetricsCollector) SetCircuitBreakerState(_ context.Context, _ resilience.State) {}

// IncWAFDetection implements ports.MetricsCollector and does nothing.
func (f *fakeMetricsCollector) IncWAFDetection(_, _ string) {}

// IncEgressRequestTotal implements ports.MetricsCollector and does nothing.
func (f *fakeMetricsCollector) IncEgressRequestTotal(_, _, _ string) {}

// ObserveEgressDuration implements ports.MetricsCollector and does nothing.
func (f *fakeMetricsCollector) ObserveEgressDuration(_, _ string, _ time.Duration) {}

// IncEgressErrorTotal implements ports.MetricsCollector and does nothing.
func (f *fakeMetricsCollector) IncEgressErrorTotal(_ string) {}

// SetTLSCertExpirySeconds implements ports.MetricsCollector and does nothing.
func (f *fakeMetricsCollector) SetTLSCertExpirySeconds(_ string, _ float64) {}

// Compile-time check: fakeMetricsCollector satisfies ports.MetricsCollector.
var _ ports.MetricsCollector = (*fakeMetricsCollector)(nil)

// identityNormalize returns the path unchanged — used for tests that don't
// need cardinality control.
func identityNormalize(path string) string { return path }

func TestMetricsMiddleware_RecordsRequestTotal(t *testing.T) {
	tests := []struct {
		name          string
		method        string
		path          string
		handlerStatus int
		wantMethod    string
		wantStatus    string
		wantPattern   string
	}{
		{
			name:          "GET 200",
			method:        http.MethodGet,
			path:          "/health",
			handlerStatus: http.StatusOK,
			wantMethod:    "GET",
			wantStatus:    "200",
			wantPattern:   "/health",
		},
		{
			name:          "POST 201",
			method:        http.MethodPost,
			path:          "/users",
			handlerStatus: http.StatusCreated,
			wantMethod:    "POST",
			wantStatus:    "201",
			wantPattern:   "/users",
		},
		{
			name:          "GET 404",
			method:        http.MethodGet,
			path:          "/missing",
			handlerStatus: http.StatusNotFound,
			wantMethod:    "GET",
			wantStatus:    "404",
			wantPattern:   "/missing",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := &fakeMetricsCollector{}
			handler := MetricsMiddleware(fake, identityNormalize)(
				http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(tt.handlerStatus)
				}),
			)

			req := httptest.NewRequest(tt.method, tt.path, nil)
			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)

			if len(fake.requestTotalCalls) != 1 {
				t.Fatalf("expected 1 IncRequestTotal call, got %d", len(fake.requestTotalCalls))
			}
			call := fake.requestTotalCalls[0]
			if call.method != tt.wantMethod {
				t.Errorf("method = %q, want %q", call.method, tt.wantMethod)
			}
			if call.statusCode != tt.wantStatus {
				t.Errorf("statusCode = %q, want %q", call.statusCode, tt.wantStatus)
			}
			if call.pathPattern != tt.wantPattern {
				t.Errorf("pathPattern = %q, want %q", call.pathPattern, tt.wantPattern)
			}
		})
	}
}

func TestMetricsMiddleware_RecordsDuration(t *testing.T) {
	fake := &fakeMetricsCollector{}
	handler := MetricsMiddleware(fake, identityNormalize)(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
	)

	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if len(fake.durationCalls) != 1 {
		t.Fatalf("expected 1 ObserveRequestDuration call, got %d", len(fake.durationCalls))
	}
	dc := fake.durationCalls[0]
	if dc.method != "GET" {
		t.Errorf("duration call method = %q, want %q", dc.method, "GET")
	}
	if dc.pathPattern != "/api/test" {
		t.Errorf("duration call pathPattern = %q, want %q", dc.pathPattern, "/api/test")
	}
	if dc.duration < 0 {
		t.Errorf("duration must be non-negative, got %v", dc.duration)
	}
}

func TestMetricsMiddleware_SkipsMetricsEndpoint(t *testing.T) {
	fake := &fakeMetricsCollector{}
	handler := MetricsMiddleware(fake, identityNormalize)(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
	)

	req := httptest.NewRequest(http.MethodGet, "/_vibewarden/metrics", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if len(fake.requestTotalCalls) != 0 {
		t.Errorf("expected 0 IncRequestTotal calls for metrics path, got %d", len(fake.requestTotalCalls))
	}
	if len(fake.durationCalls) != 0 {
		t.Errorf("expected 0 ObserveRequestDuration calls for metrics path, got %d", len(fake.durationCalls))
	}
}

func TestMetricsMiddleware_UsesNormalizeFn(t *testing.T) {
	fake := &fakeMetricsCollector{}
	normalize := func(path string) string {
		if path == "/users/123" {
			return "/users/:id"
		}
		return "other"
	}
	handler := MetricsMiddleware(fake, normalize)(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
	)

	req := httptest.NewRequest(http.MethodGet, "/users/123", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if len(fake.requestTotalCalls) != 1 {
		t.Fatalf("expected 1 IncRequestTotal call, got %d", len(fake.requestTotalCalls))
	}
	if fake.requestTotalCalls[0].pathPattern != "/users/:id" {
		t.Errorf("pathPattern = %q, want %q", fake.requestTotalCalls[0].pathPattern, "/users/:id")
	}
}

func TestMetricsMiddleware_DefaultStatusOK_WhenHandlerWritesBody(t *testing.T) {
	fake := &fakeMetricsCollector{}
	handler := MetricsMiddleware(fake, identityNormalize)(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Write body without calling WriteHeader — Go net/http defaults to 200.
			_, _ = w.Write([]byte("hello"))
		}),
	)

	req := httptest.NewRequest(http.MethodGet, "/hello", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if len(fake.requestTotalCalls) != 1 {
		t.Fatalf("expected 1 IncRequestTotal call, got %d", len(fake.requestTotalCalls))
	}
	if fake.requestTotalCalls[0].statusCode != "200" {
		t.Errorf("statusCode = %q, want %q", fake.requestTotalCalls[0].statusCode, "200")
	}
}

func TestNoOpMetricsCollector(t *testing.T) {
	// Ensure NoOpMetricsCollector compiles and satisfies the interface.
	var mc ports.MetricsCollector = vibemetrics.NoOpMetricsCollector{}

	// All calls must be no-ops (no panic).
	mc.IncRequestTotal("GET", "200", "/health")
	mc.ObserveRequestDuration("GET", "/health", time.Second)
	mc.IncRateLimitHit("ip")
	mc.IncAuthDecision("allowed")
	mc.IncUpstreamError()
	mc.SetActiveConnections(10)
}
