package metrics

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// newTestAdapter creates a PrometheusAdapter with a standard set of path patterns
// for use in tests.
func newTestAdapter(patterns []string) *PrometheusAdapter {
	return NewPrometheusAdapter(patterns)
}

func TestPrometheusAdapter_IncRequestTotal(t *testing.T) {
	adapter := newTestAdapter(nil)

	// Increment with distinct label combinations — should not panic.
	adapter.IncRequestTotal("GET", "200", "/health")
	adapter.IncRequestTotal("POST", "201", "/users/:id")
	adapter.IncRequestTotal("GET", "404", "other")

	// Confirm metrics are exported by querying the handler.
	body := scrapeMetrics(t, adapter)
	assertContains(t, body, `vibewarden_requests_total{method="GET",path_pattern="/health",status_code="200"} 1`)
	assertContains(t, body, `vibewarden_requests_total{method="POST",path_pattern="/users/:id",status_code="201"} 1`)
}

func TestPrometheusAdapter_IncRequestTotal_Increments(t *testing.T) {
	adapter := newTestAdapter(nil)

	adapter.IncRequestTotal("GET", "200", "/health")
	adapter.IncRequestTotal("GET", "200", "/health")
	adapter.IncRequestTotal("GET", "200", "/health")

	body := scrapeMetrics(t, adapter)
	assertContains(t, body, `vibewarden_requests_total{method="GET",path_pattern="/health",status_code="200"} 3`)
}

func TestPrometheusAdapter_ObserveRequestDuration(t *testing.T) {
	adapter := newTestAdapter(nil)

	// Should not panic with a valid duration.
	adapter.ObserveRequestDuration("GET", "/health", 50*time.Millisecond)
	adapter.ObserveRequestDuration("POST", "/users/:id", 250*time.Millisecond)

	body := scrapeMetrics(t, adapter)
	assertContains(t, body, `vibewarden_request_duration_seconds_count{method="GET",path_pattern="/health"} 1`)
	assertContains(t, body, `vibewarden_request_duration_seconds_count{method="POST",path_pattern="/users/:id"} 1`)
}

func TestPrometheusAdapter_IncRateLimitHit(t *testing.T) {
	tests := []struct {
		limitType string
	}{
		{"ip"},
		{"user"},
	}
	for _, tt := range tests {
		t.Run(tt.limitType, func(t *testing.T) {
			adapter := newTestAdapter(nil)
			adapter.IncRateLimitHit(tt.limitType)

			body := scrapeMetrics(t, adapter)
			assertContains(t, body, `vibewarden_rate_limit_hits_total{limit_type="`+tt.limitType+`"} 1`)
		})
	}
}

func TestPrometheusAdapter_IncAuthDecision(t *testing.T) {
	tests := []struct {
		decision string
	}{
		{"allowed"},
		{"blocked"},
	}
	for _, tt := range tests {
		t.Run(tt.decision, func(t *testing.T) {
			adapter := newTestAdapter(nil)
			adapter.IncAuthDecision(tt.decision)

			body := scrapeMetrics(t, adapter)
			assertContains(t, body, `vibewarden_auth_decisions_total{decision="`+tt.decision+`"} 1`)
		})
	}
}

func TestPrometheusAdapter_IncUpstreamError(t *testing.T) {
	adapter := newTestAdapter(nil)

	adapter.IncUpstreamError()
	adapter.IncUpstreamError()

	body := scrapeMetrics(t, adapter)
	assertContains(t, body, `vibewarden_upstream_errors_total 2`)
}

func TestPrometheusAdapter_SetActiveConnections(t *testing.T) {
	tests := []struct {
		name string
		n    int
		want string
	}{
		{"zero", 0, "vibewarden_active_connections 0"},
		{"positive", 5, "vibewarden_active_connections 5"},
		{"large", 1000, "vibewarden_active_connections 1000"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			adapter := newTestAdapter(nil)
			adapter.SetActiveConnections(tt.n)

			body := scrapeMetrics(t, adapter)
			assertContains(t, body, tt.want)
		})
	}
}

func TestPrometheusAdapter_Handler_ReturnsPrometheusFormat(t *testing.T) {
	adapter := newTestAdapter(nil)

	body := scrapeMetrics(t, adapter)

	// Go runtime metrics must be present (registered via collectors.NewGoCollector).
	assertContains(t, body, "go_goroutines")
	// Process metrics must be present (registered via collectors.NewProcessCollector).
	assertContains(t, body, "process_")
}

func TestPrometheusAdapter_NormalizePath(t *testing.T) {
	adapter := newTestAdapter([]string{"/users/:id", "/posts/:slug"})

	tests := []struct {
		path string
		want string
	}{
		{"/users/42", "/users/:id"},
		{"/posts/hello-world", "/posts/:slug"},
		{"/unknown/path", "other"},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got := adapter.NormalizePath(tt.path)
			if got != tt.want {
				t.Errorf("NormalizePath(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

func TestPrometheusAdapter_ImplementsMetricsCollector(t *testing.T) {
	// Compile-time interface satisfaction check via assignment to interface variable.
	// If PrometheusAdapter does not satisfy ports.MetricsCollector, this file will
	// not compile. We import only the local package here to keep tests self-contained;
	// the ports package check is in the ports_test.
	var _ interface {
		IncRequestTotal(method, statusCode, pathPattern string)
		ObserveRequestDuration(method, pathPattern string, duration time.Duration)
		IncRateLimitHit(limitType string)
		IncAuthDecision(decision string)
		IncUpstreamError()
		SetActiveConnections(n int)
	} = newTestAdapter(nil)
}

// scrapeMetrics issues a GET request to the adapter's Handler and returns the
// response body as a string. It fails the test on any HTTP or I/O error.
func scrapeMetrics(t *testing.T, adapter *PrometheusAdapter) string {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rr := httptest.NewRecorder()
	adapter.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("metrics handler returned status %d, want 200", rr.Code)
	}

	body, err := io.ReadAll(rr.Body)
	if err != nil {
		t.Fatalf("reading metrics body: %v", err)
	}
	return string(body)
}

// assertContains fails the test if body does not contain substring.
func assertContains(t *testing.T, body, substring string) {
	t.Helper()
	if !strings.Contains(body, substring) {
		t.Errorf("metrics output does not contain %q\n\nFull output:\n%s", substring, body)
	}
}
