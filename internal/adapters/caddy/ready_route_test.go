package caddy

import (
	"encoding/json"
	"testing"

	"github.com/vibewarden/vibewarden/internal/ports"
)

// TestBuildCaddyConfig_ReadyRoute_AlwaysPresent verifies that the
// /_vibewarden/ready route is always present in the Caddy config, even when
// readiness is not explicitly configured. The route is placed at index 1
// (after the health route).
func TestBuildCaddyConfig_ReadyRoute_AlwaysPresent(t *testing.T) {
	cfg := &ports.ProxyConfig{
		ListenAddr:   "127.0.0.1:8080",
		UpstreamAddr: "127.0.0.1:3000",
	}

	result, err := BuildCaddyConfig(cfg)
	if err != nil {
		t.Fatalf("BuildCaddyConfig() unexpected error: %v", err)
	}

	server := extractServer(t, result)
	routes, ok := server["routes"].([]map[string]any)
	if !ok || len(routes) < 2 {
		t.Fatalf("expected at least 2 routes, got %d", len(routes))
	}

	readyRoute := routes[1]

	matchers, ok := readyRoute["match"].([]map[string]any)
	if !ok || len(matchers) == 0 {
		t.Fatal("match not found in ready route")
	}
	paths, ok := matchers[0]["path"].([]string)
	if !ok || len(paths) == 0 {
		t.Fatal("path not found in ready route matcher")
	}
	if paths[0] != "/_vibewarden/ready" {
		t.Errorf("ready route path = %q, want %q", paths[0], "/_vibewarden/ready")
	}
}

// TestBuildCaddyConfig_ReadyRoute_StaticWhenNotConfigured verifies that when
// no readiness internal address is configured, the ready route uses a
// static_response handler.
func TestBuildCaddyConfig_ReadyRoute_StaticWhenNotConfigured(t *testing.T) {
	tests := []struct {
		name string
		cfg  *ports.ProxyConfig
	}{
		{
			name: "readiness not configured",
			cfg: &ports.ProxyConfig{
				ListenAddr:   "127.0.0.1:8080",
				UpstreamAddr: "127.0.0.1:3000",
			},
		},
		{
			name: "readiness enabled but no internal addr",
			cfg: &ports.ProxyConfig{
				ListenAddr:   "127.0.0.1:8080",
				UpstreamAddr: "127.0.0.1:3000",
				Readiness: ports.ReadinessProxyConfig{
					Enabled:      true,
					InternalAddr: "",
				},
			},
		},
		{
			name: "readiness disabled with internal addr",
			cfg: &ports.ProxyConfig{
				ListenAddr:   "127.0.0.1:8080",
				UpstreamAddr: "127.0.0.1:3000",
				Readiness: ports.ReadinessProxyConfig{
					Enabled:      false,
					InternalAddr: "127.0.0.1:9093",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := BuildCaddyConfig(tt.cfg)
			if err != nil {
				t.Fatalf("BuildCaddyConfig() unexpected error: %v", err)
			}

			server := extractServer(t, result)
			routes, ok := server["routes"].([]map[string]any)
			if !ok || len(routes) < 2 {
				t.Fatalf("expected at least 2 routes, got %d", len(routes))
			}

			readyRoute := routes[1]
			handlers, ok := readyRoute["handle"].([]map[string]any)
			if !ok || len(handlers) == 0 {
				t.Fatal("handle not found in ready route")
			}
			if handlers[0]["handler"] != "static_response" {
				t.Errorf("ready handler = %v, want static_response", handlers[0]["handler"])
			}
		})
	}
}

// TestBuildCaddyConfig_ReadyRoute_StaticResponseIs503 verifies that the static
// ready route returns 503 to indicate "not yet ready" when no internal server
// is wired.
func TestBuildCaddyConfig_ReadyRoute_StaticResponseIs503(t *testing.T) {
	cfg := &ports.ProxyConfig{
		ListenAddr:   "127.0.0.1:8080",
		UpstreamAddr: "127.0.0.1:3000",
	}

	result, err := BuildCaddyConfig(cfg)
	if err != nil {
		t.Fatalf("BuildCaddyConfig() unexpected error: %v", err)
	}

	server := extractServer(t, result)
	routes := server["routes"].([]map[string]any)
	readyRoute := routes[1]
	handlers := readyRoute["handle"].([]map[string]any)

	statusCode, ok := handlers[0]["status_code"].(int)
	if !ok {
		t.Fatalf("status_code not an int: %T %v", handlers[0]["status_code"], handlers[0]["status_code"])
	}
	if statusCode != 503 {
		t.Errorf("status_code = %d, want 503", statusCode)
	}

	body, ok := handlers[0]["body"].(string)
	if !ok || body == "" {
		t.Fatal("body not found or empty in static ready route handler")
	}
	// Validate the body is valid JSON and contains "ready":false.
	var parsed map[string]any
	if err := json.Unmarshal([]byte(body), &parsed); err != nil {
		t.Fatalf("body is not valid JSON: %v", err)
	}
	if parsed["ready"] != false {
		t.Errorf("body[ready] = %v, want false", parsed["ready"])
	}
}

// TestBuildCaddyConfig_ReadyRoute_DynamicWhenConfigured verifies that when a
// readiness internal address is configured, the ready route uses a
// reverse_proxy handler pointing to that address.
func TestBuildCaddyConfig_ReadyRoute_DynamicWhenConfigured(t *testing.T) {
	const internalAddr = "127.0.0.1:9093"
	cfg := &ports.ProxyConfig{
		ListenAddr:   "127.0.0.1:8080",
		UpstreamAddr: "127.0.0.1:3000",
		Readiness: ports.ReadinessProxyConfig{
			Enabled:      true,
			InternalAddr: internalAddr,
		},
	}

	result, err := BuildCaddyConfig(cfg)
	if err != nil {
		t.Fatalf("BuildCaddyConfig() unexpected error: %v", err)
	}

	server := extractServer(t, result)
	routes := server["routes"].([]map[string]any)

	// Route order: health (0), ready (1), catch-all (2).
	if len(routes) < 3 {
		t.Fatalf("expected at least 3 routes, got %d", len(routes))
	}

	readyRoute := routes[1]

	// Verify path matcher.
	matchers := readyRoute["match"].([]map[string]any)
	paths := matchers[0]["path"].([]string)
	if paths[0] != "/_vibewarden/ready" {
		t.Errorf("ready route path = %q, want %q", paths[0], "/_vibewarden/ready")
	}

	// Verify handler is reverse_proxy.
	handlers := readyRoute["handle"].([]map[string]any)
	if handlers[0]["handler"] != "reverse_proxy" {
		t.Errorf("ready handler = %v, want reverse_proxy", handlers[0]["handler"])
	}

	// Verify upstream dial address.
	upstreams, ok := handlers[0]["upstreams"].([]map[string]any)
	if !ok || len(upstreams) == 0 {
		t.Fatal("upstreams not found in dynamic ready route handler")
	}
	if upstreams[0]["dial"] != internalAddr {
		t.Errorf("upstream dial = %v, want %q", upstreams[0]["dial"], internalAddr)
	}
}

// TestBuildCaddyConfig_ReadyRoute_BeforeCatchAll verifies that the ready route
// is always placed before the catch-all proxy route.
func TestBuildCaddyConfig_ReadyRoute_BeforeCatchAll(t *testing.T) {
	cfg := &ports.ProxyConfig{
		ListenAddr:   "127.0.0.1:8080",
		UpstreamAddr: "127.0.0.1:3000",
	}

	result, err := BuildCaddyConfig(cfg)
	if err != nil {
		t.Fatalf("BuildCaddyConfig() unexpected error: %v", err)
	}

	server := extractServer(t, result)
	routes := server["routes"].([]map[string]any)

	// routes[1] = ready (has path matcher), routes[last] = catch-all (no matcher).
	readyRoute := routes[1]
	if _, hasMatcher := readyRoute["match"]; !hasMatcher {
		t.Error("routes[1] (ready) must have a path matcher")
	}

	catchAll := routes[len(routes)-1]
	if _, hasMatcher := catchAll["match"]; hasMatcher {
		t.Error("last route (catch-all) must not have a path matcher")
	}
}

// TestBuildCaddyConfig_ReadyRoute_ContentTypeHeader verifies the static ready
// route sets Content-Type: application/json.
func TestBuildCaddyConfig_ReadyRoute_ContentTypeHeader(t *testing.T) {
	cfg := &ports.ProxyConfig{
		ListenAddr:   "127.0.0.1:8080",
		UpstreamAddr: "127.0.0.1:3000",
	}

	result, err := BuildCaddyConfig(cfg)
	if err != nil {
		t.Fatalf("BuildCaddyConfig() unexpected error: %v", err)
	}

	server := extractServer(t, result)
	routes := server["routes"].([]map[string]any)
	readyRoute := routes[1]
	handlers := readyRoute["handle"].([]map[string]any)

	headers, ok := handlers[0]["headers"].(map[string][]string)
	if !ok {
		t.Fatal("headers not found in static ready route handler")
	}
	ct := headers["Content-Type"]
	if len(ct) == 0 || ct[0] != "application/json" {
		t.Errorf("Content-Type = %v, want [application/json]", ct)
	}
}

// TestBuildStaticReadyRoute verifies the helper function directly.
func TestBuildStaticReadyRoute(t *testing.T) {
	route := buildStaticReadyRoute()

	matchers, ok := route["match"].([]map[string]any)
	if !ok || len(matchers) == 0 {
		t.Fatal("match not found")
	}
	paths, ok := matchers[0]["path"].([]string)
	if !ok || len(paths) != 1 || paths[0] != "/_vibewarden/ready" {
		t.Errorf("path = %v, want [/_vibewarden/ready]", paths)
	}

	handlers, ok := route["handle"].([]map[string]any)
	if !ok || len(handlers) == 0 {
		t.Fatal("handle not found")
	}
	if handlers[0]["handler"] != "static_response" {
		t.Errorf("handler = %v, want static_response", handlers[0]["handler"])
	}
}

// TestBuildDynamicReadyRoute verifies the helper function directly.
func TestBuildDynamicReadyRoute(t *testing.T) {
	const addr = "127.0.0.1:9093"
	route := buildDynamicReadyRoute(addr)

	matchers, ok := route["match"].([]map[string]any)
	if !ok || len(matchers) == 0 {
		t.Fatal("match not found")
	}
	paths, ok := matchers[0]["path"].([]string)
	if !ok || len(paths) != 1 || paths[0] != "/_vibewarden/ready" {
		t.Errorf("path = %v, want [/_vibewarden/ready]", paths)
	}

	handlers, ok := route["handle"].([]map[string]any)
	if !ok || len(handlers) == 0 {
		t.Fatal("handle not found")
	}
	if handlers[0]["handler"] != "reverse_proxy" {
		t.Errorf("handler = %v, want reverse_proxy", handlers[0]["handler"])
	}
	upstreams, ok := handlers[0]["upstreams"].([]map[string]any)
	if !ok || len(upstreams) == 0 {
		t.Fatal("upstreams not found")
	}
	if upstreams[0]["dial"] != addr {
		t.Errorf("dial = %v, want %q", upstreams[0]["dial"], addr)
	}
}
