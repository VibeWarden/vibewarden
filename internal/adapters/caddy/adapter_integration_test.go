//go:build integration

package caddy

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
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

	adapter := NewAdapter(cfg, slog.Default(), nil)

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

	adapter := NewAdapter(cfg, slog.Default(), nil)

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

// TestAdapter_Integration_SelfSignedTLS starts Caddy with a self-signed certificate
// and verifies that HTTPS connections succeed (using a TLS client that accepts
// self-signed certificates for testing purposes).
func TestAdapter_Integration_SelfSignedTLS(t *testing.T) {
	// Start a minimal upstream that returns a plain 200.
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "ok")
	}))
	defer upstream.Close()

	httpsPort := findFreePort(t)
	listenAddr := fmt.Sprintf("127.0.0.1:%d", httpsPort)

	cfg := &ports.ProxyConfig{
		ListenAddr:   listenAddr,
		UpstreamAddr: upstream.Listener.Addr().String(),
		TLS: ports.TLSConfig{
			Enabled:  true,
			Provider: ports.TLSProviderSelfSigned,
		},
	}

	adapter := NewAdapter(cfg, slog.Default(), nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	startErr := make(chan error, 1)
	go func() {
		startErr <- adapter.Start(ctx)
	}()

	// Use an HTTP client that accepts self-signed (untrusted) certificates.
	insecureClient := &http.Client{
		Timeout: 500 * time.Millisecond,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // intentional in test
		},
	}

	proxyURL := fmt.Sprintf("https://%s", listenAddr)
	if err := waitForServerWithClient(insecureClient, proxyURL, 10*time.Second); err != nil {
		cancel()
		t.Fatalf("HTTPS proxy server did not start: %v", err)
	}

	resp, err := insecureClient.Get(proxyURL + "/")
	if err != nil {
		t.Fatalf("GET through HTTPS proxy: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status code = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	// Verify the connection was indeed TLS.
	if resp.TLS == nil {
		t.Error("expected a TLS connection, got plain HTTP")
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading response body: %v", err)
	}
	if string(body) != "ok" {
		t.Errorf("response body = %q, want %q", string(body), "ok")
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
	return waitForServerWithClient(&http.Client{Timeout: 500 * time.Millisecond}, url, timeout)
}

// waitForServerWithClient polls the given URL using the provided client until it
// returns a 2xx response or the timeout elapses.
func waitForServerWithClient(client *http.Client, url string, timeout time.Duration) error {
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

// TestAdapter_Integration_MaxConnections boots the adapter with a connection
// cap configured and asserts the server still starts and serves. This is the
// tripwire for a typo in the "caddy.listeners.vibewarden_conn_limit" module ID
// or a Provision error: Caddy fails the whole config load in either case, which
// no unit test on the emitted JSON can see. See ADR-110.
func TestAdapter_Integration_MaxConnections(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "ok")
	}))
	defer upstream.Close()

	listenAddr := fmt.Sprintf("127.0.0.1:%d", findFreePort(t))

	cfg := &ports.ProxyConfig{
		ListenAddr:     listenAddr,
		UpstreamAddr:   upstream.Listener.Addr().String(),
		MaxConnections: 4,
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
		t.Fatalf("proxy server with max_connections did not start: %v", err)
	}

	resp, err := http.Get(proxyURL + "/")
	if err != nil {
		t.Fatalf("GET through capped proxy: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status code = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	cancel()

	stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer stopCancel()
	if err := adapter.Stop(stopCtx); err != nil {
		t.Errorf("Stop() error: %v", err)
	}
}

// startTestProxy starts an adapter for cfg, waits until it answers on
// plain HTTP, and registers its shutdown with t.Cleanup.
func startTestProxy(t *testing.T, cfg *ports.ProxyConfig) string {
	t.Helper()
	adapter := NewAdapter(cfg, slog.Default(), nil)
	ctx, cancel := context.WithCancel(context.Background())
	go func() { _ = adapter.Start(ctx) }()
	t.Cleanup(func() {
		cancel()
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer stopCancel()
		_ = adapter.Stop(stopCtx)
	})
	proxyURL := "http://" + cfg.ListenAddr
	if err := waitForServer(proxyURL, 5*time.Second); err != nil {
		t.Fatalf("proxy did not start: %v", err)
	}
	return proxyURL
}

// TestAdapter_Integration_MaxHeaderBytes verifies that the embedded Caddy's
// request header cap is live by default (16 KiB, answered with 431 over
// HTTP/1.1) and that ProxyConfig.MaxHeaderBytes raises it.
func TestAdapter_Integration_MaxHeaderBytes(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	// 24 KiB: past the 16 KiB cap plus the 4 KiB of slack net/http allows
	// around it, so the default rejects it deterministically.
	bigHeader := strings.Repeat("a", 24*1024)

	for _, tc := range []struct {
		name           string
		maxHeaderBytes int
		wantStatus     int
	}{
		{name: "default cap rejects a 24 KiB header", maxHeaderBytes: 0, wantStatus: http.StatusRequestHeaderFieldsTooLarge},
		{name: "raised cap accepts it", maxHeaderBytes: 64 * 1024, wantStatus: http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			proxyURL := startTestProxy(t, &ports.ProxyConfig{
				ListenAddr:     fmt.Sprintf("127.0.0.1:%d", findFreePort(t)),
				UpstreamAddr:   upstream.Listener.Addr().String(),
				MaxHeaderBytes: tc.maxHeaderBytes,
			})

			req, err := http.NewRequest(http.MethodGet, proxyURL+"/", nil)
			if err != nil {
				t.Fatalf("building request: %v", err)
			}
			req.Header.Set("X-Big", bigHeader)
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("GET with 24 KiB header: %v", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != tc.wantStatus {
				t.Errorf("status = %d, want %d", resp.StatusCode, tc.wantStatus)
			}
		})
	}
}

// TestAdapter_Integration_ReadIdleTimeout verifies ReadIdleTimeout reaches the
// embedded Caddy: when a client stops sending its request body, the upstream's
// copy of that body is cut after the configured idle time (2 s here) instead
// of Caddy's 1-minute default.
func TestAdapter_Integration_ReadIdleTimeout(t *testing.T) {
	bodyEnded := make(chan time.Duration, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			// Readiness probes from startTestProxy; not the stalled upload.
			w.WriteHeader(http.StatusOK)
			return
		}
		start := time.Now()
		_, _ = io.Copy(io.Discard, r.Body)
		bodyEnded <- time.Since(start)
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	listenAddr := fmt.Sprintf("127.0.0.1:%d", findFreePort(t))
	startTestProxy(t, &ports.ProxyConfig{
		ListenAddr:     listenAddr,
		UpstreamAddr:   upstream.Listener.Addr().String(),
		ServerTimeouts: ports.ServerTimeoutsConfig{ReadIdleTimeout: 2 * time.Second},
	})

	conn, err := net.Dial("tcp", listenAddr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	// Announce 1000 body bytes, send 10, then go silent.
	fmt.Fprintf(conn, "POST /upload HTTP/1.1\r\nHost: localhost\r\nContent-Length: 1000\r\nContent-Type: text/plain\r\n\r\n%s", strings.Repeat("x", 10))

	select {
	case took := <-bodyEnded:
		if took > 15*time.Second {
			t.Errorf("upstream body read ended after %v, want about 2s", took)
		}
		t.Logf("upstream body read ended after %v", took.Round(100*time.Millisecond))
	case <-time.After(30 * time.Second):
		t.Fatal("upstream body read still blocked after 30s: read_idle_timeout not applied")
	}
}

// TestAdapter_Integration_DisabledTimeoutsAccepted verifies that the -1
// emitted for ports.TimeoutDisabled is accepted by Caddy and the proxy serves.
func TestAdapter_Integration_DisabledTimeoutsAccepted(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	off := ports.TimeoutDisabled
	proxyURL := startTestProxy(t, &ports.ProxyConfig{
		ListenAddr:   fmt.Sprintf("127.0.0.1:%d", findFreePort(t)),
		UpstreamAddr: upstream.Listener.Addr().String(),
		ServerTimeouts: ports.ServerTimeoutsConfig{
			ReadTimeout: off, WriteTimeout: off, IdleTimeout: off,
			ReadHeaderTimeout: off, ReadIdleTimeout: off, WriteIdleTimeout: off,
		},
	})
	resp, err := http.Get(proxyURL + "/")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}
