//go:build integration

package caddy

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/vibewarden/vibewarden/internal/ports"
)

// findFreePort finds an available TCP port on localhost.
func findFreePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("finding free port: %v", err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

func TestAdapter_Integration_ProxyRequest(t *testing.T) {
	// Start a mock upstream server.
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "hello from upstream")
	}))
	defer upstream.Close()

	listenPort := findFreePort(t)
	listenAddr := fmt.Sprintf("127.0.0.1:%d", listenPort)
	upstreamAddr := upstream.Listener.Addr().String()

	cfg := &ports.ProxyConfig{
		ListenAddr:   listenAddr,
		UpstreamAddr: upstreamAddr,
	}

	adapter := NewAdapter(cfg, slog.Default())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start the proxy in a goroutine.
	startErr := make(chan error, 1)
	go func() {
		startErr <- adapter.Start(ctx)
	}()

	// Wait for the proxy to be ready.
	proxyURL := fmt.Sprintf("http://%s", listenAddr)
	if err := waitForServer(proxyURL, 5*time.Second); err != nil {
		cancel()
		t.Fatalf("proxy server did not start: %v", err)
	}

	// Make a request through the proxy.
	resp, err := http.Get(proxyURL + "/")
	if err != nil {
		t.Fatalf("GET through proxy: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status code = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading response body: %v", err)
	}

	if string(body) != "hello from upstream" {
		t.Errorf("response body = %q, want %q", string(body), "hello from upstream")
	}

	// Shut down.
	cancel()

	stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer stopCancel()

	if err := adapter.Stop(stopCtx); err != nil {
		t.Errorf("Stop() error: %v", err)
	}
}

func TestAdapter_Integration_GracefulShutdown(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	listenPort := findFreePort(t)
	listenAddr := fmt.Sprintf("127.0.0.1:%d", listenPort)

	cfg := &ports.ProxyConfig{
		ListenAddr:   listenAddr,
		UpstreamAddr: upstream.Listener.Addr().String(),
	}

	adapter := NewAdapter(cfg, slog.Default())

	ctx, cancel := context.WithCancel(context.Background())

	startErr := make(chan error, 1)
	go func() {
		startErr <- adapter.Start(ctx)
	}()

	proxyURL := fmt.Sprintf("http://%s", listenAddr)
	if err := waitForServer(proxyURL, 5*time.Second); err != nil {
		cancel()
		t.Fatalf("proxy did not start: %v", err)
	}

	// Cancel the context to trigger shutdown signal.
	cancel()

	// Allow time for Start to return.
	select {
	case err := <-startErr:
		if err != nil {
			t.Logf("Start() returned: %v (expected after context cancel)", err)
		}
	case <-time.After(5 * time.Second):
		t.Error("Start() did not return after context cancellation")
	}

	// Explicit Stop after context is cancelled.
	stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer stopCancel()

	if err := adapter.Stop(stopCtx); err != nil {
		t.Errorf("Stop() error: %v", err)
	}
}

func TestAdapter_Integration_SecurityHeadersInProxiedResponse(t *testing.T) {
	// Start a minimal upstream that returns a plain 200.
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	listenPort := findFreePort(t)
	listenAddr := fmt.Sprintf("127.0.0.1:%d", listenPort)

	cfg := &ports.ProxyConfig{
		ListenAddr:   listenAddr,
		UpstreamAddr: upstream.Listener.Addr().String(),
		SecurityHeaders: ports.SecurityHeadersConfig{
			Enabled:               true,
			ContentTypeNosniff:    true,
			FrameOption:           "DENY",
			ContentSecurityPolicy: "default-src 'self'",
			ReferrerPolicy:        "strict-origin-when-cross-origin",
			// HSTSMaxAge is set to verify HSTS is NOT added to the response:
			// the proxy is HTTP-only in this test, so HSTS must not be present
			// regardless of config value.
			HSTSMaxAge: 31536000,
		},
	}

	adapter := NewAdapter(cfg, slog.Default())

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

	resp, err := http.Get(proxyURL + "/")
	if err != nil {
		t.Fatalf("GET through proxy: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status code = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	// Verify non-HSTS security headers are present in the proxied response.
	wantHeaders := map[string]string{
		"X-Content-Type-Options":  "nosniff",
		"X-Frame-Options":         "DENY",
		"Content-Security-Policy": "default-src 'self'",
		"Referrer-Policy":         "strict-origin-when-cross-origin",
	}
	for header, want := range wantHeaders {
		got := resp.Header.Get(header)
		if got != want {
			t.Errorf("header %q = %q, want %q", header, got, want)
		}
	}

	// HSTS must NOT be present on a plain HTTP connection.
	if hsts := resp.Header.Get("Strict-Transport-Security"); hsts != "" {
		t.Errorf("Strict-Transport-Security must not be set over HTTP, got %q", hsts)
	}

	cancel()

	stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer stopCancel()

	if err := adapter.Stop(stopCtx); err != nil {
		t.Errorf("Stop() error: %v", err)
	}
}

// waitForServer polls the given URL until it returns a 2xx response or times out.
func waitForServer(url string, timeout time.Duration) error {
	client := &http.Client{Timeout: 500 * time.Millisecond}
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		resp, err := client.Get(url)
		if err == nil {
			resp.Body.Close()
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}

	return fmt.Errorf("server at %s did not become ready within %s", url, timeout)
}
