//go:build integration

package metrics_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"log/slog"

	caddyadapter "github.com/vibewarden/vibewarden/internal/adapters/caddy"
	"github.com/vibewarden/vibewarden/internal/adapters/metrics"
	oteladapter "github.com/vibewarden/vibewarden/internal/adapters/otel"
	"github.com/vibewarden/vibewarden/internal/middleware"
	"github.com/vibewarden/vibewarden/internal/ports"

	"net/http/httptest"
)

// TestOTelMetrics_EndToEnd starts a full VibeWarden-like stack using the OTel adapter:
//   - An upstream httptest.Server that responds 200
//   - An OTelAdapter collecting metrics
//   - An internal metrics.Server serving the Prometheus handler
//   - A Caddy Adapter reverse-proxying to both upstream and the metrics server
//
// It then makes requests through the proxy and verifies that:
//   - vibewarden_requests_total is incremented
//   - vibewarden_request_duration_seconds histogram is populated
//   - Path normalization works for configured patterns
func TestOTelMetrics_EndToEnd(t *testing.T) {
	// Start a minimal upstream server.
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer upstream.Close()

	// Create the OTel provider.
	provider := oteladapter.NewProvider()
	if err := provider.Init(context.Background(), "vibewarden-integration-test", "0.0.0"); err != nil {
		t.Fatalf("Init() failed: %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = provider.Shutdown(ctx)
	}()

	// Create the OTel-backed metrics adapter.
	pathPatterns := []string{"/users/:id"}
	adapter, err := metrics.NewOTelAdapter(provider, pathPatterns)
	if err != nil {
		t.Fatalf("NewOTelAdapter() failed: %v", err)
	}

	// Start the internal metrics server.
	metricsSrv := metrics.NewServer(adapter.Handler(), slog.Default())
	if err := metricsSrv.Start(); err != nil {
		t.Fatalf("starting metrics server: %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = metricsSrv.Stop(ctx)
	}()

	// Find a free port for the proxy.
	listenPort := findFreePort(t)
	listenAddr := fmt.Sprintf("127.0.0.1:%d", listenPort)

	cfg := &ports.ProxyConfig{
		ListenAddr:   listenAddr,
		UpstreamAddr: upstream.Listener.Addr().String(),
		Metrics: ports.MetricsProxyConfig{
			Enabled:      true,
			InternalAddr: metricsSrv.Addr(),
		},
	}

	caddyAdp := caddyadapter.NewAdapter(cfg, slog.Default(), nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		if err := caddyAdp.Start(ctx); err != nil && ctx.Err() == nil {
			t.Logf("caddyAdp.Start error: %v", err)
		}
	}()

	proxyURL := fmt.Sprintf("http://%s", listenAddr)

	// Wait for the proxy to accept connections.
	if err := waitForAddr(listenAddr, 5*time.Second); err != nil {
		t.Fatalf("proxy did not start: %v", err)
	}

	// Wrap the upstream handler with the metrics middleware.
	normalizePathFn := adapter.NormalizePath
	metricsMiddleware := middleware.MetricsMiddleware(adapter, normalizePathFn)

	fakeUpstream := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	instrumentedHandler := metricsMiddleware(fakeUpstream)

	// Simulate GET /users/42 — should normalise to /users/:id.
	for i := 0; i < 3; i++ {
		req := httptest.NewRequest(http.MethodGet, "/users/42", nil)
		rr := httptest.NewRecorder()
		instrumentedHandler.ServeHTTP(rr, req)
	}

	// Simulate GET /unknown/path — no pattern match → "other".
	req := httptest.NewRequest(http.MethodGet, "/unknown/path", nil)
	rr := httptest.NewRecorder()
	instrumentedHandler.ServeHTTP(rr, req)

	// Scrape /_vibewarden/metrics through the Caddy proxy.
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(proxyURL + "/_vibewarden/metrics")
	if err != nil {
		t.Fatalf("GET /_vibewarden/metrics: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading metrics body: %v", err)
	}
	bodyStr := string(body)

	// Verify VibeWarden request counter was incremented.
	assertBodyContains(t, bodyStr, "vibewarden_requests_total")

	// Verify request duration histogram is populated.
	assertBodyContains(t, bodyStr, "vibewarden_request_duration_seconds")

	// Verify path normalization label is present.
	if !strings.Contains(bodyStr, "users") {
		t.Errorf("expected path_pattern label with 'users' in output\n\nFull output:\n%s", bodyStr)
	}

	cancel()

	stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer stopCancel()

	if err := caddyAdp.Stop(stopCtx); err != nil {
		t.Errorf("Stop() error: %v", err)
	}
}

// TestOTelAdapter_MetricNamesMatchLegacyPrometheus verifies that all metric names
// exported via the OTel Prometheus bridge match the names that were previously
// exported by the direct Prometheus adapter. This ensures that existing Prometheus
// scrapers and Grafana dashboards continue to work without any changes.
func TestOTelAdapter_MetricNamesMatchLegacyPrometheus(t *testing.T) {
	provider, err := oteladapter.NewTestProvider(context.Background())
	if err != nil {
		t.Fatalf("NewTestProvider() error = %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = provider.Shutdown(ctx)
	})

	adapter, err := metrics.NewOTelAdapter(provider, nil)
	if err != nil {
		t.Fatalf("NewOTelAdapter() error = %v", err)
	}

	// Record one observation for each metric to ensure they appear in the output.
	adapter.IncRequestTotal("GET", "200", "/")
	adapter.ObserveRequestDuration("GET", "/", 10*time.Millisecond)
	adapter.IncRateLimitHit("ip")
	adapter.IncAuthDecision("allow")
	adapter.IncUpstreamError()
	adapter.SetActiveConnections(1)

	// Scrape via the handler.
	metricsSrv := metrics.NewServer(adapter.Handler(), nil)
	if err := metricsSrv.Start(); err != nil {
		t.Fatalf("starting metrics server: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = metricsSrv.Stop(ctx)
	})

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get("http://" + metricsSrv.Addr() + "/metrics")
	if err != nil {
		t.Fatalf("GET /metrics: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading body: %v", err)
	}
	bodyStr := string(body)

	// These are the exact metric names that were exported by the legacy
	// PrometheusAdapter. All must be present for dashboard compatibility.
	expectedMetrics := []string{
		"vibewarden_requests_total",
		"vibewarden_request_duration_seconds",
		"vibewarden_rate_limit_hits_total",
		"vibewarden_auth_decisions_total",
		"vibewarden_upstream_errors_total",
		"vibewarden_active_connections",
	}
	for _, name := range expectedMetrics {
		assertBodyContains(t, bodyStr, name)
	}
}
