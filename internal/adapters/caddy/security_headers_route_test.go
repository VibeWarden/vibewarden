package caddy

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	gocaddy "github.com/caddyserver/caddy/v2"

	"github.com/vibewarden/vibewarden/internal/ports"
)

// securityHeaderNames are the response headers the security-headers plugin
// owns. Every response the sidecar serves must carry the same values for all
// of them, whichever route produced it.
var securityHeaderNames = []string{
	"Content-Security-Policy",
	"X-Content-Type-Options",
	"X-Frame-Options",
	"Referrer-Policy",
	"Permissions-Policy",
	"Cross-Origin-Opener-Policy",
}

// TestSecurityHeaders_AppliedToVibeWardenOwnRoutes is the regression test for
// #1540. It runs a real Caddy instance built from BuildCaddyConfig and proves
// that the configured security headers are present on VibeWarden's own routes
// — the built-in login UI, the admin API (including the 401 response), and the
// Kratos proxy surface — and that they match the set served on the app route.
//
// Pre-fix the security-headers handler was appended only to the catch-all app
// route, so every sidecar-owned route answered with no security headers at all.
// A config-shape assertion cannot catch that class of bug: the handler JSON was
// correct, it was simply never in the chain for those paths.
func TestSecurityHeaders_AppliedToVibeWardenOwnRoutes(t *testing.T) {
	const adminToken = "security-headers-test-token"

	mockApp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, "app-ok")
	}))
	defer mockApp.Close()

	mockKratos := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"kratos":"ok"}`)
	}))
	defer mockKratos.Close()

	mockAuthUI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, "<html>login</html>")
	}))
	defer mockAuthUI.Close()

	mockAdmin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"users":[]}`)
	}))
	defer mockAdmin.Close()

	listenAddr := fmt.Sprintf("127.0.0.1:%d", freeTCPPort(t))

	cfg := &ports.ProxyConfig{
		ListenAddr:   listenAddr,
		UpstreamAddr: mockApp.Listener.Addr().String(),
		SecurityHeaders: ports.SecurityHeadersConfig{
			Enabled:                 true,
			HSTSMaxAge:              31536000,
			HSTSIncludeSubDomains:   true,
			ContentTypeNosniff:      true,
			FrameOption:             "SAMEORIGIN",
			ContentSecurityPolicy:   "default-src 'self'; style-src 'self' 'unsafe-inline'",
			ReferrerPolicy:          "strict-origin-when-cross-origin",
			PermissionsPolicy:       "geolocation=(), microphone=(), camera=()",
			CrossOriginOpenerPolicy: "same-origin",
		},
		Auth: ports.AuthConfig{
			Enabled:         true,
			KratosPublicURL: "http://" + mockKratos.Listener.Addr().String(),
		},
		Admin: ports.AdminProxyConfig{
			Enabled:      true,
			InternalAddr: mockAdmin.Listener.Addr().String(),
		},
		AdminAuth: ports.AdminAuthConfig{
			Enabled:    true,
			Token:      adminToken,
			ConfigPath: "/_vibewarden/config/",
		},
		// Mirrors the route the auth plugin contributes for the built-in
		// login UI (internal/plugins/auth/plugin.go).
		ExtraRoutes: []ports.CaddyRoute{
			{
				MatchPath: "/_vibewarden/login",
				Priority:  39,
				Handler: map[string]any{
					"match": []map[string]any{
						{"path": []string{"/_vibewarden/login"}},
					},
					"handle": []map[string]any{
						{
							"handler": "reverse_proxy",
							"upstreams": []map[string]any{
								{"dial": mockAuthUI.Listener.Addr().String()},
							},
						},
					},
				},
			},
		},
	}

	startCaddyForTest(t, cfg)

	client := &http.Client{
		Timeout:       5 * time.Second,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse },
	}
	baseURL := "http://" + listenAddr

	if err := waitForHTTP(client, baseURL+"/", 10*time.Second); err != nil {
		t.Fatalf("proxy server did not start: %v", err)
	}

	want := securityHeadersOf(t, client, baseURL+"/")
	for _, name := range securityHeaderNames {
		if want[name] == "" {
			t.Fatalf("app route GET /: security header %q missing — test baseline is broken", name)
		}
	}

	tests := []struct {
		name     string
		path     string
		wantCode int
	}{
		{"built-in login UI", "/_vibewarden/login", http.StatusOK},
		{"admin API unauthorized", "/_vibewarden/admin/users", http.StatusUnauthorized},
		{"kratos self-service flow", "/self-service/login/browser", http.StatusOK},
		{"health endpoint", "/_vibewarden/health", 0}, // status not asserted
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := client.Get(baseURL + tt.path)
			if err != nil {
				t.Fatalf("GET %s: %v", tt.path, err)
			}
			defer resp.Body.Close() //nolint:errcheck // test cleanup

			if tt.wantCode != 0 && resp.StatusCode != tt.wantCode {
				t.Fatalf("GET %s: status = %d, want %d", tt.path, resp.StatusCode, tt.wantCode)
			}

			for _, name := range securityHeaderNames {
				got := resp.Header.Get(name)
				if got != want[name] {
					t.Errorf("GET %s: header %s = %q, want %q (same set as GET /)", tt.path, name, got, want[name])
				}
			}
		})
	}
}

// securityHeadersOf performs a GET and returns the security-header values of
// the response.
func securityHeadersOf(t *testing.T, client *http.Client, url string) map[string]string {
	t.Helper()

	resp, err := client.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close() //nolint:errcheck // test cleanup

	out := make(map[string]string, len(securityHeaderNames))
	for _, name := range securityHeaderNames {
		out[name] = resp.Header.Get(name)
	}
	return out
}

// startCaddyForTest builds the production Caddy configuration from cfg, loads
// it into the embedded Caddy instance, and stops it when the test ends.
//
// The only deviation from the production config is the admin endpoint, which is
// disabled so that the test never binds Caddy's default localhost:2019.
func startCaddyForTest(t *testing.T, cfg *ports.ProxyConfig) {
	t.Helper()

	caddyCfg, err := BuildCaddyConfig(cfg)
	if err != nil {
		t.Fatalf("BuildCaddyConfig() unexpected error: %v", err)
	}
	caddyCfg["admin"] = map[string]any{"disabled": true}

	cfgJSON, err := json.Marshal(caddyCfg)
	if err != nil {
		t.Fatalf("marshalling caddy config: %v", err)
	}

	if err := gocaddy.Load(cfgJSON, true); err != nil {
		t.Fatalf("loading caddy config: %v", err)
	}
	t.Cleanup(func() {
		if err := gocaddy.Stop(); err != nil {
			t.Errorf("stopping caddy: %v", err)
		}
	})
}

// freeTCPPort returns a TCP port that is free on localhost at call time.
func freeTCPPort(t *testing.T) int {
	t.Helper()

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listening on a free port: %v", err)
	}
	defer l.Close() //nolint:errcheck // probe listener

	return l.Addr().(*net.TCPAddr).Port
}

// waitForHTTP polls url until it answers or timeout elapses.
func waitForHTTP(client *http.Client, url string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	var lastErr error
	for time.Now().Before(deadline) {
		resp, err := client.Get(url)
		if err == nil {
			_ = resp.Body.Close()
			return nil
		}
		lastErr = err
		time.Sleep(50 * time.Millisecond)
	}
	return fmt.Errorf("server at %s not ready within %s: %w", url, timeout, lastErr)
}
