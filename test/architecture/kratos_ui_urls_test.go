package architecture_test

import (
	"context"
	"net/http"
	"net/url"
	"testing"

	"github.com/vibewarden/vibewarden/internal/adapters/authui"
	"github.com/vibewarden/vibewarden/internal/config"
	"github.com/vibewarden/vibewarden/internal/domain/identity"
	auth "github.com/vibewarden/vibewarden/internal/plugins/auth"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// stubIdentityProvider is a no-op ports.IdentityProvider. The auth plugin needs
// one to construct, but these tests never authenticate a request.
type stubIdentityProvider struct{}

func (stubIdentityProvider) Name() string { return "stub" }

func (stubIdentityProvider) Authenticate(_ context.Context, _ *http.Request) identity.AuthResult {
	return identity.Failure("no_credentials", "stub provider")
}

// builtInFlowPaths returns the URL paths of every Kratos self-service flow
// ui_url the generator emits in the default built-in UI mode.
func builtInFlowPaths(t *testing.T) []string {
	t.Helper()

	cfg := config.Config{Server: config.ServerConfig{Port: 8080}}
	f := cfg.AuthFlowURLs()

	raw := map[string]string{
		"login":         f.Login,
		"registration":  f.Registration,
		"recovery":      f.Recovery,
		"verification":  f.Verification,
		"settings":      f.Settings,
		"error":         f.Error,
		"logout return": f.LogoutReturn,
	}

	seen := make(map[string]bool, len(raw))
	paths := make([]string, 0, len(raw))
	for flow, rawURL := range raw {
		u, err := url.Parse(rawURL)
		if err != nil {
			t.Fatalf("%s flow ui_url %q is not a valid URL: %v", flow, rawURL, err)
		}
		if !seen[u.Path] {
			seen[u.Path] = true
			paths = append(paths, u.Path)
		}
	}
	return paths
}

// TestKratosUIUrls_BuiltInPathsAreServedByAuthUI pins the invariant that every
// ui_url the generated kratos.yml points at in built-in mode is a path the
// sidecar's auth UI actually serves.
//
// A path emitted here that the UI does not serve does not 404: it falls
// through the Caddy route table to the upstream app, so the flow breaks
// silently with the app's own 404 page. That was the defect in #1516, where
// every flow pointed at /auth/*.
func TestKratosUIUrls_BuiltInPathsAreServedByAuthUI(t *testing.T) {
	h, err := authui.NewHandler(authui.AuthUIConfig{}, discardSlogLogger(t))
	if err != nil {
		t.Fatalf("authui.NewHandler() error: %v", err)
	}
	if startErr := h.Start(); startErr != nil {
		t.Fatalf("authui Handler.Start() error: %v", startErr)
	}
	t.Cleanup(func() {
		if stopErr := h.Stop(context.Background()); stopErr != nil {
			t.Errorf("authui Handler.Stop() error: %v", stopErr)
		}
	})

	// Keep-alives off so Handler.Stop is not blocked by idle connections.
	client := &http.Client{Transport: &http.Transport{DisableKeepAlives: true}}
	for _, path := range builtInFlowPaths(t) {
		t.Run(path, func(t *testing.T) {
			req, reqErr := http.NewRequestWithContext(
				context.Background(), http.MethodGet, "http://"+h.Addr()+path, nil)
			if reqErr != nil {
				t.Fatalf("building request: %v", reqErr)
			}
			resp, doErr := client.Do(req)
			if doErr != nil {
				t.Fatalf("GET %s: %v", path, doErr)
			}
			defer resp.Body.Close() //nolint:errcheck // response body close in test

			if resp.StatusCode != http.StatusOK {
				t.Errorf("GET %s returned %d, want 200 — the generated kratos.yml points a flow at a path the built-in auth UI does not serve, so the redirect falls through to the upstream app",
					path, resp.StatusCode)
			}
		})
	}
}

// TestKratosUIUrls_BuiltInPathsAreRoutedByAuthPlugin pins the companion
// invariant: the auth plugin must contribute a Caddy route for every path the
// generated kratos.yml points at. A page the internal server renders but Caddy
// never routes to is just as broken as a page that does not exist.
func TestKratosUIUrls_BuiltInPathsAreRoutedByAuthPlugin(t *testing.T) {
	p := auth.New(auth.Config{
		Enabled:           true,
		Mode:              auth.ModeKratos,
		KratosPublicURL:   "http://127.0.0.1:4433",
		KratosAdminURL:    "http://127.0.0.1:4434",
		SessionCookieName: "ory_kratos_session",
		IdentitySchema:    "email_password",
	}, discardSlogLogger(t), stubIdentityProvider{})

	if err := p.Init(context.Background()); err != nil {
		t.Fatalf("auth plugin Init() error: %v", err)
	}
	t.Cleanup(func() {
		if err := p.Stop(context.Background()); err != nil {
			t.Errorf("auth plugin Stop() error: %v", err)
		}
	})

	routed := make(map[string]bool)
	for _, route := range p.ContributeCaddyRoutes() {
		matchers, ok := route.Handler["match"].([]map[string]any)
		if !ok {
			continue
		}
		for _, m := range matchers {
			paths, ok := m["path"].([]string)
			if !ok {
				continue
			}
			for _, path := range paths {
				routed[path] = true
			}
		}
	}

	for _, path := range builtInFlowPaths(t) {
		if !routed[path] {
			t.Errorf("auth plugin contributes no Caddy route for %q, which the generated kratos.yml points a flow at; routed paths: %v",
				path, routed)
		}
	}
}

// TestKratosUIUrls_CustomModeDoesNotUseBuiltInPaths pins that custom UI mode
// never emits a /_vibewarden/ path. In custom mode the auth plugin contributes
// no UI route (the operator serves the pages), so a built-in path there would
// be routed nowhere.
func TestKratosUIUrls_CustomModeDoesNotUseBuiltInPaths(t *testing.T) {
	cfg := config.Config{
		Server: config.ServerConfig{Port: 8080},
		Auth: config.AuthConfig{UI: config.AuthUIConfig{
			Mode:     config.AuthUIModeCustom,
			LoginURL: "https://app.example.com/signin",
		}},
	}

	f := cfg.AuthFlowURLs()
	for name, got := range map[string]string{
		"login":         f.Login,
		"registration":  f.Registration,
		"recovery":      f.Recovery,
		"verification":  f.Verification,
		"settings":      f.Settings,
		"error":         f.Error,
		"logout return": f.LogoutReturn,
	} {
		if got != "https://app.example.com/signin" {
			t.Errorf("%s flow ui_url = %q, want the configured custom login URL", name, got)
		}
	}
}

// compile-time assertion that the stub satisfies the port.
var _ ports.IdentityProvider = stubIdentityProvider{}
