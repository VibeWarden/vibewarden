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

	"github.com/vibewarden/vibewarden/internal/ports"
)

// TestAdminAuth_Integration_GateEnforced is the primary regression test for
// #1393. It proves end-to-end through a running Caddy instance that:
//
//   - GET /_vibewarden/admin/users with NO X-Admin-Key → 401, upstream NOT hit.
//   - GET /_vibewarden/admin/users with wrong X-Admin-Key → 401, upstream NOT hit.
//   - GET /_vibewarden/admin/users with correct X-Admin-Key → 200, upstream hit.
//
// Pre-fix, all three cases returned 503 (or 200) and the upstream was hit
// regardless of the token, because the admin route had no auth handler inlined
// and the catch-all auth handler was shadowed by the dedicated admin route.
func TestAdminAuth_Integration_GateEnforced(t *testing.T) {
	const adminToken = "integration-secret-token"

	// Mock internal admin server — records which paths are hit.
	adminHitPaths := make(chan string, 10)
	mockAdminServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		adminHitPaths <- r.URL.Path
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "admin-upstream-ok")
	}))
	defer mockAdminServer.Close()

	// Mock upstream app — must NEVER receive /_vibewarden/admin/* requests.
	appHitPaths := make(chan string, 10)
	mockAppServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		appHitPaths <- r.URL.Path
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "app-ok")
	}))
	defer mockAppServer.Close()

	listenPort := findFreePort(t)
	listenAddr := fmt.Sprintf("127.0.0.1:%d", listenPort)

	cfg := &ports.ProxyConfig{
		ListenAddr:   listenAddr,
		UpstreamAddr: mockAppServer.Listener.Addr().String(),
		Admin: ports.AdminProxyConfig{
			Enabled:      true,
			InternalAddr: mockAdminServer.Listener.Addr().String(),
		},
		AdminAuth: ports.AdminAuthConfig{
			Enabled:    true,
			Token:      adminToken,
			ConfigPath: "/_vibewarden/config/",
		},
	}

	adapter := NewAdapter(cfg, slog.Default(), nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() { adapter.Start(ctx) }() //nolint:errcheck

	proxyURL := fmt.Sprintf("http://%s", listenAddr)
	if err := waitForServer(proxyURL, 10*time.Second); err != nil {
		cancel()
		t.Fatalf("proxy server did not start: %v", err)
	}

	client := &http.Client{
		Timeout:       5 * time.Second,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse },
	}

	drainChannels := func() {
		for len(adminHitPaths) > 0 {
			<-adminHitPaths
		}
		for len(appHitPaths) > 0 {
			<-appHitPaths
		}
	}

	assertUpstreamNotHit := func(t *testing.T, label string) {
		t.Helper()
		select {
		case path := <-adminHitPaths:
			t.Errorf("%s: request leaked to admin upstream (path: %s); auth gate did NOT run", label, path)
		default:
			// Good — admin upstream was not hit.
		}
		select {
		case path := <-appHitPaths:
			t.Errorf("%s: request leaked to app upstream (path: %s)", label, path)
		default:
			// Good.
		}
	}

	// --- 401 matrix ---

	tokenTests := []struct {
		name    string
		key     string
		wantOK  bool
		wantHit bool
	}{
		{"absent token → 401", "", false, false},
		{"wrong token → 401", "wrong-token", false, false},
		{"correct token → 200", adminToken, true, true},
	}

	for _, tt := range tokenTests {
		t.Run(tt.name, func(t *testing.T) {
			drainChannels()

			req, err := http.NewRequestWithContext(ctx, http.MethodGet, proxyURL+"/_vibewarden/admin/users", nil)
			if err != nil {
				t.Fatalf("building request: %v", err)
			}
			if tt.key != "" {
				req.Header.Set("X-Admin-Key", tt.key)
			}

			resp, err := client.Do(req)
			if err != nil {
				t.Fatalf("GET /_vibewarden/admin/users: %v", err)
			}
			defer resp.Body.Close() //nolint:errcheck

			if tt.wantOK {
				if resp.StatusCode != http.StatusOK {
					body, _ := io.ReadAll(resp.Body)
					t.Errorf("status = %d, want 200; body: %s", resp.StatusCode, body)
				}
				select {
				case path := <-adminHitPaths:
					if path != "/users" && !strings.HasSuffix(path, "/users") {
						t.Logf("admin upstream received path: %s (OK)", path)
					}
				case <-time.After(500 * time.Millisecond):
					t.Error("admin upstream was not hit for valid token")
				}
			} else {
				if resp.StatusCode != http.StatusUnauthorized {
					body, _ := io.ReadAll(resp.Body)
					t.Errorf("status = %d, want 401 Unauthorized; body: %s", resp.StatusCode, body)
				}
				assertUpstreamNotHit(t, tt.name)
			}
		})
	}

	// --- UI path requires token on main (carve-out lands in #1391) ---
	// NOTE: The /_vibewarden/admin/ui tokenless carve-out (adminUIPrefix in
	// the middleware) is not yet on main; it arrives with PR #1391 (embedded
	// admin UI). On main, /_vibewarden/admin/ui also requires a token.
	// The integration test for the tokenless carve-out lives in #1391.

	// --- Public docs route ---
	// /_vibewarden/api/docs must be public (no auth required) and must reach the
	// internal admin server. It is a separate route with NO auth handler.

	t.Run("docs path with no token → upstream hit (public route)", func(t *testing.T) {
		drainChannels()

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, proxyURL+"/_vibewarden/api/docs", nil)
		if err != nil {
			t.Fatalf("building request: %v", err)
		}

		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("GET /_vibewarden/api/docs: %v", err)
		}
		defer resp.Body.Close() //nolint:errcheck

		select {
		case <-adminHitPaths:
			// Good — docs route reached the internal server without auth.
		case <-time.After(500 * time.Millisecond):
			t.Errorf("docs path did not reach upstream (status: %d); it should be public (no auth required)", resp.StatusCode)
		}
	})

	cancel()
	stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer stopCancel()
	adapter.Stop(stopCtx) //nolint:errcheck
}

// TestAdminAuth_Integration_CleanStart is the crash-loop regression test for
// #1393. It asserts that BuildCaddyConfig succeeds AND adapter.Start brings
// Caddy up cleanly when admin is enabled — no "unknown handler" rejection from
// caddy.Load. Pre-fix, the plugin's ExtraHandlers included an "admin_auth" map
// (wrong module name), causing caddy.Load to fail immediately.
func TestAdminAuth_Integration_CleanStart(t *testing.T) {
	mockAdmin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer mockAdmin.Close()

	mockApp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer mockApp.Close()

	listenPort := findFreePort(t)
	listenAddr := fmt.Sprintf("127.0.0.1:%d", listenPort)

	cfg := &ports.ProxyConfig{
		ListenAddr:   listenAddr,
		UpstreamAddr: mockApp.Listener.Addr().String(),
		Admin: ports.AdminProxyConfig{
			Enabled:      true,
			InternalAddr: mockAdmin.Listener.Addr().String(),
		},
		AdminAuth: ports.AdminAuthConfig{
			Enabled:    true,
			Token:      "any-token",
			ConfigPath: "/_vibewarden/config/",
		},
	}

	// Verify BuildCaddyConfig succeeds — no divergent admin_auth handler.
	builtCfg, err := BuildCaddyConfig(cfg)
	if err != nil {
		t.Fatalf("BuildCaddyConfig() error = %v; expect clean build when admin is enabled", err)
	}
	if builtCfg == nil {
		t.Fatal("BuildCaddyConfig() returned nil config")
	}

	// Verify the server actually starts (caddy.Load accepts the config).
	adapter := NewAdapter(cfg, slog.Default(), nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	startErr := make(chan error, 1)
	go func() {
		startErr <- adapter.Start(ctx)
	}()

	proxyURL := fmt.Sprintf("http://%s", listenAddr)
	if err := waitForServer(proxyURL, 10*time.Second); err != nil {
		cancel()
		// Check if Start returned an error (caddy.Load rejection).
		select {
		case se := <-startErr:
			t.Fatalf("adapter.Start() failed with: %v (crash-loop regression)", se)
		default:
			t.Fatalf("proxy server did not start within timeout: %v", err)
		}
	}

	// The sidecar is up — confirm the /_vibewarden/health endpoint responds.
	resp, err := http.Get(proxyURL + "/_vibewarden/health")
	if err != nil {
		t.Fatalf("GET /_vibewarden/health: %v", err)
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != http.StatusOK {
		t.Errorf("health check status = %d, want 200", resp.StatusCode)
	}

	cancel()
	stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer stopCancel()
	adapter.Stop(stopCtx) //nolint:errcheck
}
