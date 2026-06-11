package caddy

import (
	"encoding/json"
	"testing"

	"github.com/vibewarden/vibewarden/internal/ports"
)

// TestBuildAdminRoute_HandleChainOrder verifies that the admin route's handle
// chain places vibewarden_admin_auth BEFORE reverse_proxy.
//
// This is the core of the #1393 fix: the dedicated admin route used to have
// only [reverse_proxy] in its chain, bypassing the auth gate that lived in the
// catch-all handler chain (which Caddy's route-ordering means never runs for a
// path matched by an earlier dedicated route).
func TestBuildAdminRoute_HandleChainOrder(t *testing.T) {
	auth := ports.AdminAuthConfig{
		Enabled:    true,
		Token:      "test-token",
		ConfigPath: "/_vibewarden/config/",
	}

	route, err := buildAdminRoute("127.0.0.1:9092", auth)
	if err != nil {
		t.Fatalf("buildAdminRoute() error = %v", err)
	}

	handlers, ok := route["handle"].([]map[string]any)
	if !ok {
		t.Fatalf("handle is not []map[string]any: %T", route["handle"])
	}
	if len(handlers) != 2 {
		t.Fatalf("handle chain length = %d, want 2 (vibewarden_admin_auth, reverse_proxy)", len(handlers))
	}

	// Index 0 must be the auth gate.
	if got := handlers[0]["handler"]; got != "vibewarden_admin_auth" {
		t.Errorf("handlers[0].handler = %q, want %q (auth gate must be first)", got, "vibewarden_admin_auth")
	}

	// Index 1 must be the reverse proxy.
	if got := handlers[1]["handler"]; got != "reverse_proxy" {
		t.Errorf("handlers[1].handler = %q, want %q (proxy after gate)", got, "reverse_proxy")
	}
}

// TestBuildAdminRoute_AuthConfigInlined verifies that the auth handler embedded
// in the admin route carries the correct config: enabled, token, and config_path.
func TestBuildAdminRoute_AuthConfigInlined(t *testing.T) {
	auth := ports.AdminAuthConfig{
		Enabled:    true,
		Token:      "my-secret",
		ConfigPath: "/_vibewarden/config/",
	}

	route, err := buildAdminRoute("127.0.0.1:9092", auth)
	if err != nil {
		t.Fatalf("buildAdminRoute() error = %v", err)
	}

	handlers, ok := route["handle"].([]map[string]any)
	if !ok || len(handlers) < 1 {
		t.Fatal("handle chain too short")
	}

	authHandler := handlers[0]
	if authHandler["handler"] != "vibewarden_admin_auth" {
		t.Fatalf("handlers[0].handler = %q, want %q", authHandler["handler"], "vibewarden_admin_auth")
	}

	cfgRaw, ok := authHandler["config"]
	if !ok {
		t.Fatal("auth handler missing 'config' key")
	}
	cfgBytes, err := json.Marshal(cfgRaw)
	if err != nil {
		t.Fatalf("cannot marshal auth handler config: %v", err)
	}
	var cfg map[string]any
	if err := json.Unmarshal(cfgBytes, &cfg); err != nil {
		t.Fatalf("auth handler config is not valid JSON: %v", err)
	}

	if enabled, _ := cfg["enabled"].(bool); !enabled {
		t.Error("auth handler config.enabled should be true")
	}
	if token, _ := cfg["token"].(string); token != "my-secret" {
		t.Errorf("auth handler config.token = %q, want %q", token, "my-secret")
	}
	if cp, _ := cfg["config_path"].(string); cp != "/_vibewarden/config/" {
		t.Errorf("auth handler config.config_path = %q, want %q", cp, "/_vibewarden/config/")
	}
}

// TestBuildAdminRoute_MatchPath verifies the route matches /_vibewarden/admin/*.
func TestBuildAdminRoute_MatchPath(t *testing.T) {
	route, err := buildAdminRoute("127.0.0.1:9092", ports.AdminAuthConfig{Enabled: true, Token: "tok"})
	if err != nil {
		t.Fatalf("buildAdminRoute() error = %v", err)
	}

	matchers, ok := route["match"].([]map[string]any)
	if !ok || len(matchers) == 0 {
		t.Fatal("match not found or empty")
	}
	paths, ok := matchers[0]["path"].([]string)
	if !ok || len(paths) == 0 {
		t.Fatal("path matcher not found")
	}
	if paths[0] != "/_vibewarden/admin/*" {
		t.Errorf("match.path[0] = %q, want %q", paths[0], "/_vibewarden/admin/*")
	}
}

// TestBuildAdminRoute_ReverseProxyDialAddr verifies the reverse_proxy handler
// uses the internalAddr as its upstream dial address.
func TestBuildAdminRoute_ReverseProxyDialAddr(t *testing.T) {
	const wantAddr = "127.0.0.1:9092"

	route, err := buildAdminRoute(wantAddr, ports.AdminAuthConfig{Enabled: true, Token: "tok"})
	if err != nil {
		t.Fatalf("buildAdminRoute() error = %v", err)
	}

	handlers, ok := route["handle"].([]map[string]any)
	if !ok || len(handlers) < 2 {
		t.Fatal("handle chain too short")
	}

	// The proxy handler is at index 1.
	proxy := handlers[1]
	if proxy["handler"] != "reverse_proxy" {
		t.Fatalf("handlers[1].handler = %q, want reverse_proxy", proxy["handler"])
	}
	upstreams, ok := proxy["upstreams"].([]map[string]any)
	if !ok || len(upstreams) == 0 {
		t.Fatal("upstreams not found in reverse_proxy handler")
	}
	if dial, _ := upstreams[0]["dial"].(string); dial != wantAddr {
		t.Errorf("upstreams[0].dial = %q, want %q", dial, wantAddr)
	}
}

// TestBuildConfigRoute_HandleChainOrder verifies that the config route's handle
// chain places vibewarden_admin_auth BEFORE reverse_proxy, mirroring the admin
// route. The config hot-reload endpoints must be token-gated AND proxied to the
// internal admin server — not the upstream app.
func TestBuildConfigRoute_HandleChainOrder(t *testing.T) {
	auth := ports.AdminAuthConfig{
		Enabled:    true,
		Token:      "test-token",
		ConfigPath: "/_vibewarden/config/",
	}

	route, err := buildConfigRoute("127.0.0.1:9092", auth)
	if err != nil {
		t.Fatalf("buildConfigRoute() error = %v", err)
	}

	handlers, ok := route["handle"].([]map[string]any)
	if !ok {
		t.Fatalf("handle is not []map[string]any: %T", route["handle"])
	}
	if len(handlers) != 2 {
		t.Fatalf("handle chain length = %d, want 2 (vibewarden_admin_auth, reverse_proxy)", len(handlers))
	}
	if got := handlers[0]["handler"]; got != "vibewarden_admin_auth" {
		t.Errorf("handlers[0].handler = %q, want vibewarden_admin_auth (gate first)", got)
	}
	if got := handlers[1]["handler"]; got != "reverse_proxy" {
		t.Errorf("handlers[1].handler = %q, want reverse_proxy", got)
	}
}

// TestBuildConfigRoute_MatchPathsCoverNoSlashAndSubtree verifies the config
// route matches BOTH the exact no-slash inspection path (/_vibewarden/config)
// and the /_vibewarden/config/* subtree (reload endpoint). The no-slash GET
// must not be stranded — the original plugin route only matched the subtree.
func TestBuildConfigRoute_MatchPathsCoverNoSlashAndSubtree(t *testing.T) {
	route, err := buildConfigRoute("127.0.0.1:9092", ports.AdminAuthConfig{Enabled: true, Token: "tok"})
	if err != nil {
		t.Fatalf("buildConfigRoute() error = %v", err)
	}

	matchers, ok := route["match"].([]map[string]any)
	if !ok || len(matchers) == 0 {
		t.Fatal("match not found or empty")
	}
	paths, ok := matchers[0]["path"].([]string)
	if !ok {
		t.Fatalf("match.path is not []string: %T", matchers[0]["path"])
	}

	wantPaths := map[string]bool{
		"/_vibewarden/config":   false,
		"/_vibewarden/config/*": false,
	}
	for _, p := range paths {
		if _, expected := wantPaths[p]; expected {
			wantPaths[p] = true
		}
	}
	for p, found := range wantPaths {
		if !found {
			t.Errorf("config route match.path missing %q", p)
		}
	}
}

// TestBuildConfigRoute_ReverseProxyDialAddr verifies the proxy targets the
// internal admin server, not the upstream app.
func TestBuildConfigRoute_ReverseProxyDialAddr(t *testing.T) {
	const wantAddr = "127.0.0.1:9092"

	route, err := buildConfigRoute(wantAddr, ports.AdminAuthConfig{Enabled: true, Token: "tok"})
	if err != nil {
		t.Fatalf("buildConfigRoute() error = %v", err)
	}
	handlers, ok := route["handle"].([]map[string]any)
	if !ok || len(handlers) < 2 {
		t.Fatal("handle chain too short")
	}
	proxy := handlers[1]
	upstreams, ok := proxy["upstreams"].([]map[string]any)
	if !ok || len(upstreams) == 0 {
		t.Fatal("upstreams not found")
	}
	if dial, _ := upstreams[0]["dial"].(string); dial != wantAddr {
		t.Errorf("config route upstream dial = %q, want %q", dial, wantAddr)
	}
}

// TestBuildCaddyConfig_ConfigRoutePresent verifies the assembled config contains
// a config route (matching /_vibewarden/config) when admin is enabled, gated by
// vibewarden_admin_auth and proxying to the internal admin server. Regression
// for the dropped /_vibewarden/config/* route in #1393's first revision.
func TestBuildCaddyConfig_ConfigRoutePresent(t *testing.T) {
	const internalAddr = "127.0.0.1:9092"
	cfg := &ports.ProxyConfig{
		ListenAddr:   "127.0.0.1:8080",
		UpstreamAddr: "127.0.0.1:3000",
		Admin: ports.AdminProxyConfig{
			Enabled:      true,
			InternalAddr: internalAddr,
		},
		AdminAuth: ports.AdminAuthConfig{
			Enabled:    true,
			Token:      "secret-token",
			ConfigPath: "/_vibewarden/config/",
		},
	}

	result, err := BuildCaddyConfig(cfg)
	if err != nil {
		t.Fatalf("BuildCaddyConfig() unexpected error: %v", err)
	}

	server := extractServer(t, result)
	routes, ok := server["routes"].([]map[string]any)
	if !ok {
		t.Fatal("routes not found")
	}

	var configRoute map[string]any
	for _, route := range routes {
		matchers, ok := route["match"].([]map[string]any)
		if !ok {
			continue
		}
		for _, m := range matchers {
			paths, ok := m["path"].([]string)
			if !ok {
				continue
			}
			for _, p := range paths {
				if p == "/_vibewarden/config" {
					configRoute = route
				}
			}
		}
	}
	if configRoute == nil {
		t.Fatal("config route (/_vibewarden/config) not found in assembled config — hot-reload endpoints would leak to the upstream app")
	}

	handlers, ok := configRoute["handle"].([]map[string]any)
	if !ok || len(handlers) < 2 {
		t.Fatalf("config route handle chain length = %d, want >= 2", len(handlers))
	}
	if got := handlers[0]["handler"]; got != "vibewarden_admin_auth" {
		t.Errorf("config route handlers[0].handler = %q, want vibewarden_admin_auth (gate first)", got)
	}
	proxy := handlers[1]
	if proxy["handler"] != "reverse_proxy" {
		t.Errorf("config route handlers[1].handler = %q, want reverse_proxy", proxy["handler"])
	}
	upstreams, _ := proxy["upstreams"].([]map[string]any)
	if len(upstreams) == 0 || upstreams[0]["dial"] != internalAddr {
		t.Errorf("config route must proxy to internal admin server %q, got %v", internalAddr, upstreams)
	}
}

// TestBuildCaddyConfig_AdminRoute_NoAdminAuthInOldDivergentForm verifies that
// the assembled Caddy config does NOT contain a "handler":"admin_auth" key
// anywhere in the route list. The old plugin-contributed form used the wrong
// module name ("admin_auth" vs. "vibewarden_admin_auth") causing caddy.Load to
// fail — this test would have caught that bug in #1393.
func TestBuildCaddyConfig_AdminRoute_NoAdminAuthInOldDivergentForm(t *testing.T) {
	cfg := &ports.ProxyConfig{
		ListenAddr:   "127.0.0.1:8080",
		UpstreamAddr: "127.0.0.1:3000",
		Admin: ports.AdminProxyConfig{
			Enabled:      true,
			InternalAddr: "127.0.0.1:9092",
		},
		AdminAuth: ports.AdminAuthConfig{
			Enabled:    true,
			Token:      "secret-token",
			ConfigPath: "/_vibewarden/config/",
		},
	}

	result, err := BuildCaddyConfig(cfg)
	if err != nil {
		t.Fatalf("BuildCaddyConfig() unexpected error: %v", err)
	}

	// Walk all handler maps in all routes and assert "admin_auth" (the wrong
	// module name) is never used.
	server := extractServer(t, result)
	routes, ok := server["routes"].([]map[string]any)
	if !ok {
		t.Fatal("routes not found")
	}
	for i, route := range routes {
		handlers, ok := route["handle"].([]map[string]any)
		if !ok {
			continue
		}
		for j, h := range handlers {
			if name, _ := h["handler"].(string); name == "admin_auth" {
				t.Errorf("routes[%d].handle[%d].handler = %q — this is the wrong module name (should be 'vibewarden_admin_auth'); caddy.Load would reject it", i, j, name)
			}
		}
	}
}

// TestBuildCaddyConfig_AdminRouteHandlerChain verifies that when admin is
// enabled the route for /_vibewarden/admin/* has vibewarden_admin_auth as its
// first handler and reverse_proxy as its second — the gate-first invariant.
func TestBuildCaddyConfig_AdminRouteHandlerChain(t *testing.T) {
	cfg := &ports.ProxyConfig{
		ListenAddr:   "127.0.0.1:8080",
		UpstreamAddr: "127.0.0.1:3000",
		Admin: ports.AdminProxyConfig{
			Enabled:      true,
			InternalAddr: "127.0.0.1:9092",
		},
		AdminAuth: ports.AdminAuthConfig{
			Enabled:    true,
			Token:      "secret-token",
			ConfigPath: "/_vibewarden/config/",
		},
	}

	result, err := BuildCaddyConfig(cfg)
	if err != nil {
		t.Fatalf("BuildCaddyConfig() unexpected error: %v", err)
	}

	server := extractServer(t, result)
	routes, ok := server["routes"].([]map[string]any)
	if !ok {
		t.Fatal("routes not found")
	}

	// Find the admin route by its path matcher.
	var adminRoute map[string]any
	for _, route := range routes {
		matchers, ok := route["match"].([]map[string]any)
		if !ok {
			continue
		}
		for _, m := range matchers {
			paths, ok := m["path"].([]string)
			if !ok {
				continue
			}
			for _, p := range paths {
				if p == "/_vibewarden/admin/*" {
					adminRoute = route
				}
			}
		}
	}
	if adminRoute == nil {
		t.Fatal("admin route (/_vibewarden/admin/*) not found in assembled config")
	}

	handlers, ok := adminRoute["handle"].([]map[string]any)
	if !ok || len(handlers) < 2 {
		t.Fatalf("admin route handle chain length = %d, want >= 2", len(handlers))
	}

	if got := handlers[0]["handler"]; got != "vibewarden_admin_auth" {
		t.Errorf("admin route handlers[0].handler = %q, want vibewarden_admin_auth (gate must run first)", got)
	}
	if got := handlers[1]["handler"]; got != "reverse_proxy" {
		t.Errorf("admin route handlers[1].handler = %q, want reverse_proxy", got)
	}
}

// TestBuildCaddyConfig_AdminDisabled_NoAdminRoute verifies that when
// Admin.InternalAddr is empty (admin disabled) no /_vibewarden/admin/* route
// is emitted at all — the surface is hidden.
func TestBuildCaddyConfig_AdminDisabled_NoAdminRoute(t *testing.T) {
	cfg := &ports.ProxyConfig{
		ListenAddr:   "127.0.0.1:8080",
		UpstreamAddr: "127.0.0.1:3000",
		Admin:        ports.AdminProxyConfig{Enabled: false},
	}

	result, err := BuildCaddyConfig(cfg)
	if err != nil {
		t.Fatalf("BuildCaddyConfig() unexpected error: %v", err)
	}

	server := extractServer(t, result)
	routes, ok := server["routes"].([]map[string]any)
	if !ok {
		t.Fatal("routes not found")
	}

	for _, route := range routes {
		matchers, ok := route["match"].([]map[string]any)
		if !ok {
			continue
		}
		for _, m := range matchers {
			paths, ok := m["path"].([]string)
			if !ok {
				continue
			}
			for _, p := range paths {
				if p == "/_vibewarden/admin/*" {
					t.Error("admin route should not be present when admin is disabled")
				}
			}
		}
	}
}
