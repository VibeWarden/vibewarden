//go:build integration

package caddy

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	metricsadapter "github.com/vibewarden/vibewarden/internal/adapters/metrics"
	oteladapter "github.com/vibewarden/vibewarden/internal/adapters/otel"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// newTestMetricsServer creates an OTelAdapter-backed internal metrics server for
// use in integration tests. It returns the started server; callers are responsible
// for stopping it.
func newTestMetricsServer(t *testing.T) *metricsadapter.Server {
	t.Helper()
	provider, err := oteladapter.NewTestProvider(context.Background())
	if err != nil {
		t.Fatalf("NewTestProvider() error = %v", err)
	}
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })

	adapter, err := metricsadapter.NewOTelAdapter(provider, nil)
	if err != nil {
		t.Fatalf("NewOTelAdapter() error = %v", err)
	}

	srv := metricsadapter.NewServer(adapter.Handler(), slog.Default())
	if err := srv.Start(); err != nil {
		t.Fatalf("starting metrics server: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Stop(ctx)
	})
	return srv
}

// TestAdapter_Integration_MetricsEndpoint starts Caddy with the metrics endpoint
// enabled, scrapes /_vibewarden/metrics through the proxy, and verifies the
// response is valid Prometheus text format.
func TestAdapter_Integration_MetricsEndpoint(t *testing.T) {
	// Start a minimal upstream server.
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	// Start the internal metrics server backed by OTelAdapter.
	metricsSrv := newTestMetricsServer(t)

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

	adapter := NewAdapter(cfg, slog.Default(), nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	startErr := make(chan error, 1)
	go func() {
		startErr <- adapter.Start(ctx)
	}()

	proxyURL := fmt.Sprintf("http://%s", listenAddr)
	if err := waitForServer(proxyURL, 5*time.Second); err != nil {
		cancel()
		t.Fatalf("proxy server did not start: %v", err)
	}

	// Scrape /_vibewarden/metrics through the proxy.
	resp, err := http.Get(proxyURL + "/_vibewarden/metrics")
	if err != nil {
		t.Fatalf("GET /_vibewarden/metrics: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading response body: %v", err)
	}

	bodyStr := string(body)

	// Verify Prometheus text format markers.
	// Go runtime and process metrics are always present regardless of request count.
	if !strings.Contains(bodyStr, "go_goroutines") {
		t.Error("metrics response does not contain go_goroutines")
	}

	// VibeWarden metric type/help lines are present even when counters are zero
	// because they are registered in the Prometheus registry at startup.
	if !strings.Contains(bodyStr, "vibewarden_") {
		t.Error("metrics response does not contain any vibewarden_ prefixed metrics")
	}

	cancel()

	stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer stopCancel()

	if err := adapter.Stop(stopCtx); err != nil {
		t.Errorf("Stop() error: %v", err)
	}
}

// TestAdapter_Integration_MetricsEndpoint_BypassesUpstream verifies that requests
// to /_vibewarden/metrics are served by the internal metrics server and do not
// reach the upstream application.
func TestAdapter_Integration_MetricsEndpoint_BypassesUpstream(t *testing.T) {
	upstreamCalled := false
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		upstreamCalled = true
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	metricsSrv := newTestMetricsServer(t)

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

	adapter := NewAdapter(cfg, slog.Default(), nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	startErr := make(chan error, 1)
	go func() {
		startErr <- adapter.Start(ctx)
	}()

	// Poll the health endpoint (/_vibewarden/health) instead of root "/" to
	// avoid inadvertently triggering the upstream before the assertion.
	proxyURL := fmt.Sprintf("http://%s", listenAddr)
	healthURL := proxyURL + "/_vibewarden/health"
	if err := waitForServer(healthURL, 5*time.Second); err != nil {
		cancel()
		t.Fatalf("proxy server did not start: %v", err)
	}

	// At this point, the upstream must not have been called yet (health is served
	// by Caddy's static_response handler, not by the upstream).
	if upstreamCalled {
		t.Fatal("upstream was unexpectedly called during startup health poll")
	}

	resp, err := http.Get(proxyURL + "/_vibewarden/metrics")
	if err != nil {
		t.Fatalf("GET /_vibewarden/metrics: %v", err)
	}
	resp.Body.Close()

	if upstreamCalled {
		t.Error("upstream was called for /_vibewarden/metrics; expected bypass")
	}

	cancel()

	stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer stopCancel()

	if err := adapter.Stop(stopCtx); err != nil {
		t.Errorf("Stop() error: %v", err)
	}
}
