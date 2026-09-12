package caddy

import (
	"testing"

	"github.com/vibewarden/vibewarden/internal/ports"
)

func TestBuildResponseHeadersHandlerJSON_Remove(t *testing.T) {
	cfg := ports.ResponseHeadersConfig{
		Enabled: true,
		Remove:  []string{"Server", "X-Powered-By"},
	}
	result := buildResponseHeadersHandlerJSON(cfg)

	if result["handler"] != "headers" {
		t.Errorf("handler = %q, want \"headers\"", result["handler"])
	}

	resp, ok := result["response"].(map[string]any)
	if !ok {
		t.Fatalf("response = %T, want map[string]any", result["response"])
	}

	del, ok := resp["delete"].([]string)
	if !ok {
		t.Fatalf("response.delete = %T, want []string", resp["delete"])
	}
	if len(del) != 2 {
		t.Fatalf("response.delete len = %d, want 2", len(del))
	}
	for _, want := range []string{"Server", "X-Powered-By"} {
		found := false
		for _, got := range del {
			if got == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("response.delete does not contain %q; got %v", want, del)
		}
	}

	// Must not have set or add keys.
	if _, has := resp["set"]; has {
		t.Error("response.set should not be present when Set map is empty")
	}
	if _, has := resp["add"]; has {
		t.Error("response.add should not be present when Add map is empty")
	}
}

func TestBuildResponseHeadersHandlerJSON_Set(t *testing.T) {
	cfg := ports.ResponseHeadersConfig{
		Enabled: true,
		Set:     map[string]string{"X-Service-Version": "1.0.0"},
	}
	result := buildResponseHeadersHandlerJSON(cfg)

	resp, ok := result["response"].(map[string]any)
	if !ok {
		t.Fatalf("response = %T, want map[string]any", result["response"])
	}

	set, ok := resp["set"].(map[string][]string)
	if !ok {
		t.Fatalf("response.set = %T, want map[string][]string", resp["set"])
	}
	vals, exists := set["X-Service-Version"]
	if !exists {
		t.Fatal("response.set missing X-Service-Version")
	}
	if len(vals) != 1 || vals[0] != "1.0.0" {
		t.Errorf("X-Service-Version = %v, want [\"1.0.0\"]", vals)
	}

	// Must not have delete or add keys.
	if _, has := resp["delete"]; has {
		t.Error("response.delete should not be present when Remove is empty")
	}
	if _, has := resp["add"]; has {
		t.Error("response.add should not be present when Add map is empty")
	}
}

func TestBuildResponseHeadersHandlerJSON_Add(t *testing.T) {
	cfg := ports.ResponseHeadersConfig{
		Enabled: true,
		Add:     map[string]string{"Cache-Control": "no-store"},
	}
	result := buildResponseHeadersHandlerJSON(cfg)

	resp, ok := result["response"].(map[string]any)
	if !ok {
		t.Fatalf("response = %T, want map[string]any", result["response"])
	}

	add, ok := resp["add"].(map[string][]string)
	if !ok {
		t.Fatalf("response.add = %T, want map[string][]string", resp["add"])
	}
	vals, exists := add["Cache-Control"]
	if !exists {
		t.Fatal("response.add missing Cache-Control")
	}
	if len(vals) != 1 || vals[0] != "no-store" {
		t.Errorf("Cache-Control = %v, want [\"no-store\"]", vals)
	}

	// Must not have delete or set keys.
	if _, has := resp["delete"]; has {
		t.Error("response.delete should not be present when Remove is empty")
	}
	if _, has := resp["set"]; has {
		t.Error("response.set should not be present when Set map is empty")
	}
}

func TestBuildResponseHeadersHandlerJSON_AllOperations(t *testing.T) {
	cfg := ports.ResponseHeadersConfig{
		Enabled: true,
		Set:     map[string]string{"X-Version": "2"},
		Add:     map[string]string{"X-Extra": "yes"},
		Remove:  []string{"Server"},
	}
	result := buildResponseHeadersHandlerJSON(cfg)

	resp, ok := result["response"].(map[string]any)
	if !ok {
		t.Fatalf("response = %T, want map[string]any", result["response"])
	}

	if _, ok := resp["delete"]; !ok {
		t.Error("expected response.delete to be present")
	}
	if _, ok := resp["set"]; !ok {
		t.Error("expected response.set to be present")
	}
	if _, ok := resp["add"]; !ok {
		t.Error("expected response.add to be present")
	}
}

func TestBuildResponseHeadersHandlerJSON_EnvVarPassThrough(t *testing.T) {
	cfg := ports.ResponseHeadersConfig{
		Enabled: true,
		Set:     map[string]string{"X-Version": "${APP_VERSION}"},
	}
	result := buildResponseHeadersHandlerJSON(cfg)

	resp := result["response"].(map[string]any)
	set := resp["set"].(map[string][]string)
	vals := set["X-Version"]
	if len(vals) != 1 || vals[0] != "${APP_VERSION}" {
		t.Errorf("X-Version = %v, want [\"${APP_VERSION}\"]", vals)
	}
}

// ---------------------------------------------------------------------------
// Integration with BuildCaddyConfig
// ---------------------------------------------------------------------------

func TestBuildCaddyConfig_ResponseHeaders_Disabled(t *testing.T) {
	cfg := &ports.ProxyConfig{
		ListenAddr:   "127.0.0.1:8080",
		UpstreamAddr: "127.0.0.1:3000",
		ResponseHeaders: ports.ResponseHeadersConfig{
			Enabled: false,
		},
	}
	result, err := BuildCaddyConfig(cfg)
	if err != nil {
		t.Fatalf("BuildCaddyConfig() unexpected error: %v", err)
	}

	handlers := extractCatchAllHandlers(t, result)

	// When disabled, no extra headers handler should appear beyond the
	// user-header-strip handler and the reverse proxy.
	for _, h := range handlers {
		handler, _ := h["handler"].(string)
		if handler != "headers" {
			continue
		}
		// The user-header-strip handler operates on "request", not "response".
		// If any headers handler has a "response" key it came from our plugin.
		if _, hasResp := h["response"]; hasResp {
			t.Error("unexpected response headers handler when ResponseHeaders.Enabled is false")
		}
	}
}

func TestBuildCaddyConfig_ResponseHeaders_RemoveServer(t *testing.T) {
	cfg := &ports.ProxyConfig{
		ListenAddr:   "127.0.0.1:8080",
		UpstreamAddr: "127.0.0.1:3000",
		ResponseHeaders: ports.ResponseHeadersConfig{
			Enabled: true,
			Remove:  []string{"Server"},
		},
	}
	result, err := BuildCaddyConfig(cfg)
	if err != nil {
		t.Fatalf("BuildCaddyConfig() unexpected error: %v", err)
	}

	handlers := extractCatchAllHandlers(t, result)

	found := false
	for _, h := range handlers {
		resp, ok := h["response"].(map[string]any)
		if !ok {
			continue
		}
		del, ok := resp["delete"].([]string)
		if !ok {
			continue
		}
		for _, name := range del {
			if name == "Server" {
				found = true
			}
		}
	}
	if !found {
		t.Error("expected response headers handler with Server in delete list")
	}
}

func TestBuildCaddyConfig_ResponseHeaders_SetHeader(t *testing.T) {
	cfg := &ports.ProxyConfig{
		ListenAddr:   "127.0.0.1:8080",
		UpstreamAddr: "127.0.0.1:3000",
		ResponseHeaders: ports.ResponseHeadersConfig{
			Enabled: true,
			Set:     map[string]string{"X-Service-Version": "1.2.3"},
		},
	}
	result, err := BuildCaddyConfig(cfg)
	if err != nil {
		t.Fatalf("BuildCaddyConfig() unexpected error: %v", err)
	}

	handlers := extractCatchAllHandlers(t, result)

	found := false
	for _, h := range handlers {
		resp, ok := h["response"].(map[string]any)
		if !ok {
			continue
		}
		set, ok := resp["set"].(map[string][]string)
		if !ok {
			continue
		}
		if vals, exists := set["X-Service-Version"]; exists {
			if len(vals) == 1 && vals[0] == "1.2.3" {
				found = true
			}
		}
	}
	if !found {
		t.Error("expected response headers handler with X-Service-Version set to 1.2.3")
	}
}

func TestBuildCaddyConfig_ResponseHeaders_AfterSecurityHeaders(t *testing.T) {
	// Verify that the response-headers handler runs after the security-headers
	// handler, so operator rules can override security headers. Since #1540 the
	// two live in different routes: security headers in the global first route,
	// response headers in the catch-all chain. Both apply their ops to the same
	// response header map, in route order, so the override still holds.
	cfg := &ports.ProxyConfig{
		ListenAddr:   "127.0.0.1:8080",
		UpstreamAddr: "127.0.0.1:3000",
		SecurityHeaders: ports.SecurityHeadersConfig{
			Enabled:     true,
			FrameOption: "DENY",
		},
		ResponseHeaders: ports.ResponseHeadersConfig{
			Enabled: true,
			Set:     map[string]string{"X-Frame-Options": "SAMEORIGIN"},
		},
	}
	result, err := BuildCaddyConfig(cfg)
	if err != nil {
		t.Fatalf("BuildCaddyConfig() unexpected error: %v", err)
	}

	server := extractServer(t, result)
	routes, ok := server["routes"].([]map[string]any)
	if !ok {
		t.Fatal("routes not found in server config")
	}

	// The security-headers handler must be the first route (setting DENY).
	secRouteHandlers, ok := routes[0]["handle"].([]map[string]any)
	if !ok || len(secRouteHandlers) != 1 {
		t.Fatal("routes[0] is not the global security-headers route")
	}
	if got := frameOptionOf(t, secRouteHandlers[0]); got != "DENY" {
		t.Fatalf("security-headers route sets X-Frame-Options = %q, want %q", got, "DENY")
	}

	// The response-headers handler lives in the catch-all chain (setting
	// SAMEORIGIN), which Caddy evaluates after the global route.
	handlers := extractCatchAllHandlers(t, result)
	respHeadersIdx := -1
	for i, h := range handlers {
		if frameOptionOf(t, h) == "SAMEORIGIN" {
			respHeadersIdx = i
			break
		}
	}
	if respHeadersIdx == -1 {
		t.Fatal("could not find response-headers handler in catch-all chain")
	}
}

// frameOptionOf returns the X-Frame-Options value a Caddy headers handler sets,
// or "" when the handler is not a response-headers handler or does not set it.
func frameOptionOf(t *testing.T, h map[string]any) string {
	t.Helper()

	if h["handler"] != "headers" {
		return ""
	}
	resp, ok := h["response"].(map[string]any)
	if !ok {
		return ""
	}
	setMap, ok := resp["set"].(map[string][]string)
	if !ok {
		return ""
	}
	vals := setMap["X-Frame-Options"]
	if len(vals) == 0 {
		return ""
	}
	return vals[0]
}
