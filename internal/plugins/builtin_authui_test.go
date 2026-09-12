package plugins_test

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/vibewarden/vibewarden/internal/config"
	"github.com/vibewarden/vibewarden/internal/plugins"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// findAuthPlugin returns the auth plugin from the registry.
func findAuthPlugin(registry *plugins.Registry) ports.Plugin {
	for _, p := range registry.Plugins() {
		if p.Name() == "auth" {
			return p
		}
	}
	return nil
}

// authUIDialAddr returns the upstream address of the Caddy route the auth
// plugin contributes for the built-in auth UI pages, or "" when the plugin
// contributes no such route.
func authUIDialAddr(t *testing.T, p ports.Plugin) string {
	t.Helper()

	contributor, ok := p.(interface {
		ContributeCaddyRoutes() []ports.CaddyRoute
	})
	if !ok {
		t.Fatal("auth plugin does not contribute Caddy routes")
	}
	for _, route := range contributor.ContributeCaddyRoutes() {
		if route.MatchPath != "/_vibewarden/login" {
			continue
		}
		handlers, ok := route.Handler["handle"].([]map[string]any)
		if !ok || len(handlers) == 0 {
			continue
		}
		upstreams, ok := handlers[0]["upstreams"].([]map[string]any)
		if !ok || len(upstreams) == 0 {
			continue
		}
		dial, ok := upstreams[0]["dial"].(string)
		if !ok {
			continue
		}
		return dial
	}
	return ""
}

// kratosAuthConfig returns a config with Kratos auth active and the given
// auth.ui block, which is the only combination that starts the built-in UI.
func kratosAuthConfig(ui config.AuthUIConfig) *config.Config {
	return &config.Config{
		Auth: config.AuthConfig{
			Mode:              config.AuthModeKratos,
			SessionCookieName: "ory_kratos_session",
			IdentitySchema:    "email_password",
			UI:                ui,
		},
		Kratos: config.KratosConfig{
			PublicURL: "http://127.0.0.1:4433",
			AdminURL:  "http://127.0.0.1:4434",
		},
	}
}

// TestRegisterBuiltinPlugins_AuthUIBrandingReachesRenderedPage is the
// regression test for #1511: auth.ui.app_name and auth.ui.logo_url were
// accepted and validated by the config loader but never reached the auth
// plugin, so the built-in pages rendered no branding at all. This test walks
// the whole chain — config → registry → plugin → internal UI server → HTML.
func TestRegisterBuiltinPlugins_AuthUIBrandingReachesRenderedPage(t *testing.T) {
	cfg := kratosAuthConfig(config.AuthUIConfig{
		Mode:            "built-in",
		AppName:         "Acme Corp",
		LogoURL:         "https://cdn.example.com/logo.svg",
		FaviconURL:      "/static/favicon.ico",
		CustomCSSURL:    "/static/auth.css",
		PrimaryColor:    "#00FF00",
		BackgroundColor: "#112233",
	})

	logger := discardLogger()
	registry := plugins.NewRegistry(logger)
	plugins.RegisterBuiltinPlugins(registry, cfg, stubEventLogger{}, logger)

	authPlugin := findAuthPlugin(registry)
	if authPlugin == nil {
		t.Fatal("auth plugin not registered")
	}
	if err := authPlugin.Init(context.Background()); err != nil {
		t.Fatalf("auth plugin Init() error: %v", err)
	}
	t.Cleanup(func() {
		if err := authPlugin.Stop(context.Background()); err != nil {
			t.Errorf("auth plugin Stop() error: %v", err)
		}
	})

	addr := authUIDialAddr(t, authPlugin)
	if addr == "" {
		t.Fatal("auth plugin contributes no built-in auth UI route")
	}

	client := &http.Client{Transport: &http.Transport{DisableKeepAlives: true}}
	req, err := http.NewRequestWithContext(
		context.Background(), http.MethodGet, "http://"+addr+"/_vibewarden/login", nil)
	if err != nil {
		t.Fatalf("building request: %v", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("GET login page: %v", err)
	}
	defer resp.Body.Close() //nolint:errcheck // response body close in test

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading login page: %v", err)
	}
	page := string(body)

	for _, want := range []string{
		"Acme Corp",
		"https://cdn.example.com/logo.svg",
		"/static/favicon.ico",
		"/static/auth.css",
		"#00FF00",
		"#112233",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("rendered login page does not contain configured auth.ui value %q", want)
		}
	}
}

// TestRegisterBuiltinPlugins_AuthUICustomModeSkipsBuiltInPages verifies that
// auth.ui.mode reaches the plugin too: in "custom" mode the sidecar must not
// serve or route its own pages.
func TestRegisterBuiltinPlugins_AuthUICustomModeSkipsBuiltInPages(t *testing.T) {
	cfg := kratosAuthConfig(config.AuthUIConfig{
		Mode:     "custom",
		LoginURL: "https://app.example.com/signin",
	})

	logger := discardLogger()
	registry := plugins.NewRegistry(logger)
	plugins.RegisterBuiltinPlugins(registry, cfg, stubEventLogger{}, logger)

	authPlugin := findAuthPlugin(registry)
	if authPlugin == nil {
		t.Fatal("auth plugin not registered")
	}
	if err := authPlugin.Init(context.Background()); err != nil {
		t.Fatalf("auth plugin Init() error: %v", err)
	}
	t.Cleanup(func() {
		if err := authPlugin.Stop(context.Background()); err != nil {
			t.Errorf("auth plugin Stop() error: %v", err)
		}
	})

	if addr := authUIDialAddr(t, authPlugin); addr != "" {
		t.Errorf("auth plugin routes the built-in auth UI (%s) even though auth.ui.mode is \"custom\"", addr)
	}
}
