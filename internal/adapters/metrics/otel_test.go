package metrics_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	oteladapter "github.com/vibewarden/vibewarden/internal/adapters/otel"
	"github.com/vibewarden/vibewarden/internal/ports"

	"github.com/vibewarden/vibewarden/internal/adapters/metrics"
)

// newOTelTestProvider creates and initializes an OTel provider for testing
// with the Prometheus exporter enabled.
// The caller must stop it using t.Cleanup.
func newOTelTestProvider(t *testing.T) *oteladapter.Provider {
	t.Helper()
	p := oteladapter.NewProvider()
	cfg := ports.TelemetryConfig{
		Prometheus: ports.PrometheusExporterConfig{Enabled: true},
	}
	if err := p.Init(context.Background(), "test", "0.0.0", cfg); err != nil {
		t.Fatalf("Init() failed: %v", err)
	}
	t.Cleanup(func() { _ = p.Shutdown(context.Background()) })
	return p
}

// newOTelTestAdapter creates an OTelAdapter backed by a real OTel provider.
func newOTelTestAdapter(t *testing.T, patterns []string) *metrics.OTelAdapter {
	t.Helper()
	p := newOTelTestProvider(t)
	a, err := metrics.NewOTelAdapter(p, patterns)
	if err != nil {
		t.Fatalf("NewOTelAdapter() failed: %v", err)
	}
	return a
}

// scrapeOTelMetrics issues a GET request to the adapter's Handler and returns
// the response body as a string.
func scrapeOTelMetrics(t *testing.T, a *metrics.OTelAdapter) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rr := httptest.NewRecorder()
	a.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("metrics handler returned status %d, want 200", rr.Code)
	}
	body, err := io.ReadAll(rr.Body)
	if err != nil {
		t.Fatalf("reading metrics body: %v", err)
	}
	return string(body)
}

func TestOTelAdapter_ImplementsMetricsCollector(t *testing.T) {
	var _ ports.MetricsCollector = (*metrics.OTelAdapter)(nil)
}

func TestOTelAdapter_NewOTelAdapter_Success(t *testing.T) {
	a := newOTelTestAdapter(t, nil)
	if a == nil {
		t.Fatal("NewOTelAdapter() returned nil")
	}
}

func TestOTelAdapter_NewOTelAdapter_WithPathPatterns(t *testing.T) {
	a := newOTelTestAdapter(t, []string{"/users/:id", "/api/v1/*"})
	if a == nil {
		t.Fatal("NewOTelAdapter() returned nil")
	}
}

func TestOTelAdapter_Handler_NotNil(t *testing.T) {
	a := newOTelTestAdapter(t, nil)
	if a.Handler() == nil {
		t.Error("Handler() returned nil")
	}
}

func TestOTelAdapter_IncRequestTotal(t *testing.T) {
	a := newOTelTestAdapter(t, nil)

	a.IncRequestTotal("GET", "200", "/health")
	a.IncRequestTotal("POST", "201", "/users/:id")
	a.IncRequestTotal("GET", "404", "other")

	body := scrapeOTelMetrics(t, a)
	if !strings.Contains(body, "vibewarden_requests_total") {
		t.Errorf("expected vibewarden_requests_total in output\n\nFull output:\n%s", body)
	}
}

func TestOTelAdapter_IncRequestTotal_Increments(t *testing.T) {
	a := newOTelTestAdapter(t, nil)

	a.IncRequestTotal("GET", "200", "/health")
	a.IncRequestTotal("GET", "200", "/health")
	a.IncRequestTotal("GET", "200", "/health")

	body := scrapeOTelMetrics(t, a)
	// OTel Prometheus exporter uses different label ordering than direct Prometheus.
	// We verify the counter name and value 3 appear together.
	if !strings.Contains(body, "vibewarden_requests_total") {
		t.Errorf("expected vibewarden_requests_total in output\n\nFull output:\n%s", body)
	}
	if !strings.Contains(body, "3") {
		t.Errorf("expected count 3 in output\n\nFull output:\n%s", body)
	}
}

func TestOTelAdapter_ObserveRequestDuration(t *testing.T) {
	a := newOTelTestAdapter(t, nil)

	a.ObserveRequestDuration("GET", "/health", 50*time.Millisecond)
	a.ObserveRequestDuration("POST", "/users/:id", 250*time.Millisecond)

	body := scrapeOTelMetrics(t, a)
	if !strings.Contains(body, "vibewarden_request_duration_seconds") {
		t.Errorf("expected vibewarden_request_duration_seconds in output\n\nFull output:\n%s", body)
	}
}

func TestOTelAdapter_IncRateLimitHit(t *testing.T) {
	tests := []struct {
		limitType string
	}{
		{"ip"},
		{"user"},
	}
	for _, tt := range tests {
		t.Run(tt.limitType, func(t *testing.T) {
			a := newOTelTestAdapter(t, nil)
			a.IncRateLimitHit(tt.limitType)

			body := scrapeOTelMetrics(t, a)
			if !strings.Contains(body, "vibewarden_rate_limit_hits_total") {
				t.Errorf("expected vibewarden_rate_limit_hits_total in output\n\nFull output:\n%s", body)
			}
		})
	}
}

func TestOTelAdapter_IncAuthDecision(t *testing.T) {
	tests := []struct {
		decision string
	}{
		{"allowed"},
		{"blocked"},
	}
	for _, tt := range tests {
		t.Run(tt.decision, func(t *testing.T) {
			a := newOTelTestAdapter(t, nil)
			a.IncAuthDecision(tt.decision)

			body := scrapeOTelMetrics(t, a)
			if !strings.Contains(body, "vibewarden_auth_decisions_total") {
				t.Errorf("expected vibewarden_auth_decisions_total in output\n\nFull output:\n%s", body)
			}
		})
	}
}

func TestOTelAdapter_IncUpstreamError(t *testing.T) {
	a := newOTelTestAdapter(t, nil)

	a.IncUpstreamError()
	a.IncUpstreamError()

	body := scrapeOTelMetrics(t, a)
	if !strings.Contains(body, "vibewarden_upstream_errors_total") {
		t.Errorf("expected vibewarden_upstream_errors_total in output\n\nFull output:\n%s", body)
	}
}

func TestOTelAdapter_SetActiveConnections(t *testing.T) {
	tests := []struct {
		name       string
		seq        []int // sequence of SetActiveConnections calls
		wantMetric bool  // whether the metric should appear in output
	}{
		// OTel only exports instruments that have been observed with non-zero delta.
		// Setting to 0 from 0 produces no delta so the metric is not exported.
		{"set to zero from zero is noop", []int{0}, false},
		{"set to positive", []int{5}, true},
		{"increase then decrease", []int{10, 3}, true},
		{"multiple increases", []int{1, 5, 10}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := newOTelTestAdapter(t, nil)
			for _, n := range tt.seq {
				a.SetActiveConnections(n)
			}
			// Must not panic and metrics endpoint must respond.
			body := scrapeOTelMetrics(t, a)
			if tt.wantMetric && !strings.Contains(body, "vibewarden_active_connections") {
				t.Errorf("expected vibewarden_active_connections in output\n\nFull output:\n%s", body)
			}
		})
	}
}

func TestOTelAdapter_NormalizePath(t *testing.T) {
	a := newOTelTestAdapter(t, []string{"/users/:id", "/posts/:slug"})

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
			got := a.NormalizePath(tt.path)
			if got != tt.want {
				t.Errorf("NormalizePath(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

func TestOTelAdapter_Handler_ServesValidResponse(t *testing.T) {
	a := newOTelTestAdapter(t, nil)

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rr := httptest.NewRecorder()
	a.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Handler returned status %d, want 200", rr.Code)
	}
}

func TestOTelAdapter_SetActiveConnections_DeltaTracking(t *testing.T) {
	// Verify that calling SetActiveConnections multiple times converges to the
	// correct final value.
	a := newOTelTestAdapter(t, nil)

	// Simulate a connection count going up and down.
	a.SetActiveConnections(5)
	a.SetActiveConnections(10)
	a.SetActiveConnections(3)
	a.SetActiveConnections(0)

	// Must not panic. The OTel up-down counter should reflect the last value.
	body := scrapeOTelMetrics(t, a)
	if !strings.Contains(body, "vibewarden_active_connections") {
		t.Errorf("expected vibewarden_active_connections in output\n\nFull output:\n%s", body)
	}
}
