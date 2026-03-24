//go:build integration

package caddy

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"log/slog"

	"github.com/vibewarden/vibewarden/internal/ports"
)

// TestAdapter_Integration_KratosFlowProxied verifies that requests to Kratos
// self-service flow paths are forwarded to the Kratos upstream and NOT to the
// application upstream.
//
// A mock "Kratos" HTTP server and a mock "upstream app" HTTP server are used.
// The Caddy proxy is configured with auth.enabled and auth.kratos_public_url
// pointing to the mock Kratos server.
func TestAdapter_Integration_KratosFlowProxied(t *testing.T) {
	// Mock Kratos public API — responds to self-service flow paths.
	kratosHitPaths := make(chan string, 10)
	mockKratos := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		kratosHitPaths <- r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"kratos":"ok"}`)
	}))
	defer mockKratos.Close()

	// Mock upstream app — should NOT receive Kratos flow requests.
	upstreamHitPaths := make(chan string, 10)
	mockUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamHitPaths <- r.URL.Path
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "upstream-ok")
	}))
	defer mockUpstream.Close()

	listenPort := findFreePort(t)
	listenAddr := fmt.Sprintf("127.0.0.1:%d", listenPort)

	kratosAddr := mockKratos.Listener.Addr().String()
	upstreamAddr := mockUpstream.Listener.Addr().String()

	cfg := &ports.ProxyConfig{
		ListenAddr:   listenAddr,
		UpstreamAddr: upstreamAddr,
		Auth: ports.AuthConfig{
			Enabled:         true,
			KratosPublicURL: "http://" + kratosAddr,
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
	if err := waitForServer(proxyURL, 10*time.Second); err != nil {
		cancel()
		t.Fatalf("proxy server did not start: %v", err)
	}

	// Table of Kratos self-service paths that must be proxied to Kratos.
	kratosFlowTests := []struct {
		name string
		path string
	}{
		{"login browser flow", "/self-service/login/browser"},
		{"registration browser flow", "/self-service/registration/browser"},
		{"logout browser", "/self-service/logout"},
		{"settings browser", "/self-service/settings/browser"},
		{"recovery browser", "/self-service/recovery/browser"},
		{"verification browser", "/self-service/verification/browser"},
		{"ory kratos public prefix", "/.ory/kratos/public/sessions/whoami"},
	}

	client := &http.Client{
		Timeout: 5 * time.Second,
		// Do not follow redirects so we can inspect raw responses.
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	for _, tt := range kratosFlowTests {
		t.Run(tt.name, func(t *testing.T) {
			// Drain channels.
			for len(kratosHitPaths) > 0 {
				<-kratosHitPaths
			}
			for len(upstreamHitPaths) > 0 {
				<-upstreamHitPaths
			}

			resp, err := client.Get(proxyURL + tt.path)
			if err != nil {
				t.Fatalf("GET %s: %v", tt.path, err)
			}
			resp.Body.Close()

			// The request must have reached Kratos, not the upstream app.
			select {
			case kratosPath := <-kratosHitPaths:
				// Caddy strips the wildcard prefix only if we configured strip;
				// our route has no strip, so Kratos receives the full path.
				if kratosPath == "" {
					t.Errorf("Kratos received empty path for %s", tt.path)
				}
				t.Logf("Kratos received path: %s (requested: %s)", kratosPath, tt.path)
			case <-time.After(500 * time.Millisecond):
				t.Errorf("request to %s was NOT forwarded to Kratos", tt.path)
			}

			// The upstream app must NOT have received this request.
			select {
			case upstreamPath := <-upstreamHitPaths:
				t.Errorf("request to %s leaked to upstream app (path: %s)", tt.path, upstreamPath)
			default:
				// Good — upstream was not hit.
			}
		})
	}

	cancel()

	stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer stopCancel()

	if err := adapter.Stop(stopCtx); err != nil {
		t.Errorf("Stop() error: %v", err)
	}
}

// TestAdapter_Integration_AppRequestNotProxiedToKratos verifies that ordinary
// application requests (e.g. /dashboard, /api/data) are forwarded to the
// upstream app and NOT to the Kratos server.
func TestAdapter_Integration_AppRequestNotProxiedToKratos(t *testing.T) {
	kratosHitPaths := make(chan string, 5)
	mockKratos := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		kratosHitPaths <- r.URL.Path
		w.WriteHeader(http.StatusOK)
	}))
	defer mockKratos.Close()

	upstreamHitPaths := make(chan string, 5)
	mockUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamHitPaths <- r.URL.Path
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "upstream-response")
	}))
	defer mockUpstream.Close()

	listenPort := findFreePort(t)
	listenAddr := fmt.Sprintf("127.0.0.1:%d", listenPort)

	cfg := &ports.ProxyConfig{
		ListenAddr:   listenAddr,
		UpstreamAddr: mockUpstream.Listener.Addr().String(),
		Auth: ports.AuthConfig{
			Enabled:         true,
			KratosPublicURL: "http://" + mockKratos.Listener.Addr().String(),
		},
	}

	adapter := NewAdapter(cfg, slog.Default())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() { adapter.Start(ctx) }() //nolint:errcheck

	proxyURL := fmt.Sprintf("http://%s", listenAddr)
	if err := waitForServer(proxyURL, 10*time.Second); err != nil {
		cancel()
		t.Fatalf("proxy server did not start: %v", err)
	}

	appPaths := []string{"/", "/dashboard", "/api/v1/data"}

	client := &http.Client{
		Timeout: 5 * time.Second,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	for _, path := range appPaths {
		t.Run("app path: "+path, func(t *testing.T) {
			for len(kratosHitPaths) > 0 {
				<-kratosHitPaths
			}
			for len(upstreamHitPaths) > 0 {
				<-upstreamHitPaths
			}

			resp, err := client.Get(proxyURL + path)
			if err != nil {
				t.Fatalf("GET %s: %v", path, err)
			}
			resp.Body.Close()

			// Upstream must have received this request.
			select {
			case <-upstreamHitPaths:
				// Good.
			case <-time.After(500 * time.Millisecond):
				t.Errorf("request to %s was NOT forwarded to upstream", path)
			}

			// Kratos must NOT have received this request.
			select {
			case kratosPath := <-kratosHitPaths:
				t.Errorf("app request to %s leaked to Kratos (path: %s)", path, kratosPath)
			default:
				// Good.
			}
		})
	}

	cancel()
	stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer stopCancel()
	adapter.Stop(stopCtx) //nolint:errcheck
}

// TestAdapter_Integration_HealthCheckNotProxiedToKratosOrUpstream verifies
// that the /_vibewarden/health endpoint is served inline by Caddy and does
// not reach either the Kratos or upstream servers.
func TestAdapter_Integration_HealthCheckNotProxiedToKratosOrUpstream(t *testing.T) {
	kratosHit := make(chan struct{}, 1)
	mockKratos := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		kratosHit <- struct{}{}
		w.WriteHeader(http.StatusOK)
	}))
	defer mockKratos.Close()

	upstreamHit := make(chan struct{}, 1)
	mockUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		upstreamHit <- struct{}{}
		w.WriteHeader(http.StatusOK)
	}))
	defer mockUpstream.Close()

	listenPort := findFreePort(t)
	listenAddr := fmt.Sprintf("127.0.0.1:%d", listenPort)

	cfg := &ports.ProxyConfig{
		ListenAddr:   listenAddr,
		UpstreamAddr: mockUpstream.Listener.Addr().String(),
		Auth: ports.AuthConfig{
			Enabled:         true,
			KratosPublicURL: "http://" + mockKratos.Listener.Addr().String(),
		},
	}

	adapter := NewAdapter(cfg, slog.Default())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() { adapter.Start(ctx) }() //nolint:errcheck

	proxyURL := fmt.Sprintf("http://%s", listenAddr)
	if err := waitForServer(proxyURL, 10*time.Second); err != nil {
		cancel()
		t.Fatalf("proxy server did not start: %v", err)
	}

	resp, err := http.Get(proxyURL + "/_vibewarden/health")
	if err != nil {
		t.Fatalf("GET /_vibewarden/health: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("health check status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading health check body: %v", err)
	}
	if !strings.Contains(string(body), "ok") {
		t.Errorf("health check body = %q, expected to contain 'ok'", body)
	}

	// Neither Kratos nor upstream must have been hit.
	select {
	case <-kratosHit:
		t.Error("health check leaked to Kratos server")
	default:
	}
	select {
	case <-upstreamHit:
		t.Error("health check leaked to upstream server")
	default:
	}

	cancel()
	stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer stopCancel()
	adapter.Stop(stopCtx) //nolint:errcheck
}
