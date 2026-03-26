//go:build integration

package metrics_test

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"log/slog"

	"github.com/vibewarden/vibewarden/internal/adapters/metrics"
	caddyadapter "github.com/vibewarden/vibewarden/internal/adapters/caddy"
	"github.com/vibewarden/vibewarden/internal/middleware"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// TestMetrics_EndToEnd starts a full VibeWarden-like stack:
//   - An upstream httptest.Server that responds 200
//   - A PrometheusAdapter collecting metrics
//   - An internal metrics.Server serving the Prometheus handler
//   - A Caddy Adapter reverse-proxying to both upstream and the metrics server
//
// It then makes requests through the proxy and verifies that:
//   - vibewarden_requests_total is incremented
//   - vibewarden_request_duration_seconds histogram is populated
//   - Go runtime metrics (go_*) are present
//   - Process metrics (process_*) are present
//   - Path normalization works for configured patterns
func TestMetrics_EndToEnd(t *testing.T) {
	// Start a minimal upstream server.
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer upstream.Close()

	// Create the Prometheus adapter with a path pattern for /users/:id.
	pathPatterns := []string{"/users/:id"}
	pa := metrics.NewPrometheusAdapter(pathPatterns)

	// Start the internal metrics server.
	metricsSrv := metrics.NewServer(pa.Handler(), slog.Default())
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

	adapter := caddyadapter.NewAdapter(cfg, slog.Default(), nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		if err := adapter.Start(ctx); err != nil && ctx.Err() == nil {
			t.Logf("adapter.Start error: %v", err)
		}
	}()

	proxyURL := fmt.Sprintf("http://%s", listenAddr)

	// Wait for the proxy to accept connections.
	if err := waitForAddr(listenAddr, 5*time.Second); err != nil {
		t.Fatalf("proxy did not start: %v", err)
	}

	// Wrap the upstream handler with the metrics middleware so that metrics are
	// actually recorded. In production this is wired in cmd/vibewarden/serve.go;
	// here we simulate a few direct hits against the adapter and record them
	// through the PrometheusAdapter directly.
	normalizePathFn := pa.NormalizePath
	metricsMiddleware := middleware.MetricsMiddleware(pa, normalizePathFn)

	// Record some fake requests through the middleware to populate counters.
	// We route them through a simple httptest.Server that wraps the middleware.
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

	// Verify Go runtime metrics.
	assertBodyContains(t, bodyStr, "go_goroutines")

	// Verify process metrics.
	assertBodyContains(t, bodyStr, "process_")

	// Verify VibeWarden request counter was incremented with path normalization.
	assertBodyContains(t, bodyStr, `vibewarden_requests_total{method="GET",path_pattern="/users/:id",status_code="200"} 3`)

	// Verify "other" label for unmatched path.
	assertBodyContains(t, bodyStr, `path_pattern="other"`)

	// Verify request duration histogram is populated.
	assertBodyContains(t, bodyStr, `vibewarden_request_duration_seconds_count{method="GET",path_pattern="/users/:id"} 3`)

	cancel()

	stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer stopCancel()

	if err := adapter.Stop(stopCtx); err != nil {
		t.Errorf("Stop() error: %v", err)
	}
}

// TestMetrics_GoRuntimeAndProcessMetricsPresent verifies that the metrics
// handler always exposes go_* and process_* metrics even without any requests.
func TestMetrics_GoRuntimeAndProcessMetricsPresent(t *testing.T) {
	pa := metrics.NewPrometheusAdapter(nil)
	metricsSrv := metrics.NewServer(pa.Handler(), slog.Default())
	if err := metricsSrv.Start(); err != nil {
		t.Fatalf("starting metrics server: %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = metricsSrv.Stop(ctx)
	}()

	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get("http://" + metricsSrv.Addr() + "/metrics")
	if err != nil {
		t.Fatalf("GET /metrics: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading body: %v", err)
	}
	bodyStr := string(body)

	assertBodyContains(t, bodyStr, "go_goroutines")
	assertBodyContains(t, bodyStr, "process_")
	// Gauge and plain Counter (non-Vec) metrics appear in the output even with zero
	// observations because they have a fixed value (no label cardinality).
	assertBodyContains(t, bodyStr, "vibewarden_active_connections")
	assertBodyContains(t, bodyStr, "vibewarden_upstream_errors_total")
}

// TestMetrics_PathNormalizationLabels verifies that configured path patterns
// are used as metric labels and unmatched paths collapse to "other".
func TestMetrics_PathNormalizationLabels(t *testing.T) {
	patterns := []string{
		"/users/:id",
		"/api/v1/items/:item_id/comments/:comment_id",
	}
	pa := metrics.NewPrometheusAdapter(patterns)

	tests := []struct {
		path        string
		wantPattern string
	}{
		{"/users/123", "/users/:id"},
		{"/users/abc", "/users/:id"},
		{"/api/v1/items/42/comments/7", "/api/v1/items/:item_id/comments/:comment_id"},
		{"/unknown", "other"},
		{"/api/v1/items/42", "other"},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got := pa.NormalizePath(tt.path)
			if got != tt.wantPattern {
				t.Errorf("NormalizePath(%q) = %q, want %q", tt.path, got, tt.wantPattern)
			}
		})
	}
}

// findFreePort returns a free TCP port on localhost.
func findFreePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("finding free port: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	return port
}

// waitForAddr polls the given host:port until a TCP connection succeeds or the
// timeout elapses.
func waitForAddr(addr string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 100*time.Millisecond)
		if err == nil {
			conn.Close()
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return fmt.Errorf("server at %s did not become ready within %s", addr, timeout)
}

// assertBodyContains fails the test if body does not contain substring.
func assertBodyContains(t *testing.T, body, substring string) {
	t.Helper()
	if !strings.Contains(body, substring) {
		t.Errorf("metrics output does not contain %q\n\nFull output:\n%s",
			substring, body)
	}
}

