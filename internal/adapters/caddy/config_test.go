package caddy

import (
	"testing"

	"github.com/vibewarden/vibewarden/internal/ports"
)

func TestBuildCaddyConfig_Validation(t *testing.T) {
	tests := []struct {
		name    string
		cfg     *ports.ProxyConfig
		wantErr bool
	}{
		{
			name:    "nil config",
			cfg:     nil,
			wantErr: true,
		},
		{
			name: "missing listen addr",
			cfg: &ports.ProxyConfig{
				UpstreamAddr: "127.0.0.1:3000",
			},
			wantErr: true,
		},
		{
			name: "missing upstream addr",
			cfg: &ports.ProxyConfig{
				ListenAddr: "127.0.0.1:8080",
			},
			wantErr: true,
		},
		{
			name: "valid local config",
			cfg: &ports.ProxyConfig{
				ListenAddr:   "127.0.0.1:8080",
				UpstreamAddr: "127.0.0.1:3000",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := BuildCaddyConfig(tt.cfg)
			if (err != nil) != tt.wantErr {
				t.Errorf("BuildCaddyConfig() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestBuildCaddyConfig_AutomaticHTTPS(t *testing.T) {
	tests := []struct {
		name          string
		cfg           *ports.ProxyConfig
		wantDisableAH bool
	}{
		{
			name: "TLS disabled disables automatic HTTPS",
			cfg: &ports.ProxyConfig{
				ListenAddr:   "0.0.0.0:8080",
				UpstreamAddr: "127.0.0.1:3000",
				TLS: ports.TLSConfig{
					Enabled: false,
				},
			},
			wantDisableAH: true,
		},
		{
			name: "TLS enabled enables automatic HTTPS",
			cfg: &ports.ProxyConfig{
				ListenAddr:   "127.0.0.1:8080",
				UpstreamAddr: "127.0.0.1:3000",
				TLS: ports.TLSConfig{
					Enabled: true,
				},
			},
			wantDisableAH: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := BuildCaddyConfig(tt.cfg)
			if err != nil {
				t.Fatalf("BuildCaddyConfig() unexpected error: %v", err)
			}

			server := extractServer(t, result)
			autoHTTPS, ok := server["automatic_https"].(map[string]any)
			if !ok {
				t.Fatal("automatic_https not found in server config")
			}

			disabled, ok := autoHTTPS["disable"].(bool)
			if !ok {
				t.Fatal("automatic_https.disable not found")
			}

			if disabled != tt.wantDisableAH {
				t.Errorf("automatic_https.disable = %v, want %v", disabled, tt.wantDisableAH)
			}
		})
	}
}

func TestBuildCaddyConfig_SecurityHeaders(t *testing.T) {
	cfg := &ports.ProxyConfig{
		ListenAddr:   "127.0.0.1:8080",
		UpstreamAddr: "127.0.0.1:3000",
		SecurityHeaders: ports.SecurityHeadersConfig{
			Enabled:               true,
			HSTSMaxAge:            31536000,
			HSTSIncludeSubDomains: true,
			HSTSPreload:           false,
			ContentTypeNosniff:    true,
			FrameOption:           "DENY",
			ContentSecurityPolicy: "default-src 'self'",
			ReferrerPolicy:        "strict-origin-when-cross-origin",
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
	// routes[0] = health check, routes[1] = catch-all proxy route
	if len(routes) < 2 {
		t.Fatalf("expected at least 2 routes (health + proxy), got %d", len(routes))
	}

	handlers, ok := routes[1]["handle"].([]map[string]any)
	if !ok {
		t.Fatal("handle not found in proxy route")
	}

	// First handler should be security headers when enabled
	if len(handlers) < 2 {
		t.Fatalf("expected at least 2 handlers (security headers + reverse proxy), got %d", len(handlers))
	}

	firstHandler := handlers[0]
	if firstHandler["handler"] != "headers" {
		t.Errorf("first handler = %v, want 'headers'", firstHandler["handler"])
	}
}

func TestBuildCaddyConfig_NoSecurityHeaders(t *testing.T) {
	cfg := &ports.ProxyConfig{
		ListenAddr:   "127.0.0.1:8080",
		UpstreamAddr: "127.0.0.1:3000",
		SecurityHeaders: ports.SecurityHeadersConfig{
			Enabled: false,
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
	// routes[0] = health check, routes[1] = catch-all proxy route
	if len(routes) < 2 {
		t.Fatalf("expected at least 2 routes (health + proxy), got %d", len(routes))
	}

	handlers, ok := routes[1]["handle"].([]map[string]any)
	if !ok {
		t.Fatal("handle not found in proxy route")
	}

	// Only the reverse proxy handler when security headers disabled
	if len(handlers) != 1 {
		t.Fatalf("expected 1 handler (reverse proxy only), got %d", len(handlers))
	}

	if handlers[0]["handler"] != "reverse_proxy" {
		t.Errorf("handler = %v, want 'reverse_proxy'", handlers[0]["handler"])
	}
}

func TestBuildCaddyConfig_ReverseProxyUpstream(t *testing.T) {
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
	if !ok {
		t.Fatal("routes not found in server config")
	}
	// routes[0] = health check, routes[1] = catch-all proxy route
	if len(routes) < 2 {
		t.Fatalf("expected at least 2 routes (health + proxy), got %d", len(routes))
	}
	handlers, ok := routes[1]["handle"].([]map[string]any)
	if !ok {
		t.Fatal("handle not found in proxy route")
	}

	// Find reverse proxy handler
	var rpHandler map[string]any
	for _, h := range handlers {
		if h["handler"] == "reverse_proxy" {
			rpHandler = h
			break
		}
	}
	if rpHandler == nil {
		t.Fatal("reverse_proxy handler not found")
	}

	upstreams, ok := rpHandler["upstreams"].([]map[string]any)
	if !ok || len(upstreams) == 0 {
		t.Fatal("upstreams not found in reverse_proxy handler")
	}

	if upstreams[0]["dial"] != "127.0.0.1:3000" {
		t.Errorf("upstream dial = %v, want '127.0.0.1:3000'", upstreams[0]["dial"])
	}
}

func TestBuildCaddyConfig_HealthRoute(t *testing.T) {
	tests := []struct {
		name        string
		cfg         *ports.ProxyConfig
		wantVersion string
	}{
		{
			name: "health route uses version from config",
			cfg: &ports.ProxyConfig{
				ListenAddr:   "127.0.0.1:8080",
				UpstreamAddr: "127.0.0.1:3000",
				Version:      "v1.2.3",
			},
			wantVersion: "v1.2.3",
		},
		{
			name: "health route with empty version",
			cfg: &ports.ProxyConfig{
				ListenAddr:   "127.0.0.1:8080",
				UpstreamAddr: "127.0.0.1:3000",
				Version:      "",
			},
			wantVersion: "",
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
			if !ok {
				t.Fatal("routes not found in server config")
			}
			if len(routes) < 2 {
				t.Fatalf("expected at least 2 routes (health + proxy), got %d", len(routes))
			}

			// routes[0] must be the health check route
			healthRoute := routes[0]

			matchers, ok := healthRoute["match"].([]map[string]any)
			if !ok || len(matchers) == 0 {
				t.Fatal("match not found in health route")
			}

			paths, ok := matchers[0]["path"].([]string)
			if !ok || len(paths) == 0 {
				t.Fatal("path not found in health route matcher")
			}
			if paths[0] != "/_vibewarden/health" {
				t.Errorf("health route path = %q, want %q", paths[0], "/_vibewarden/health")
			}

			handlers, ok := healthRoute["handle"].([]map[string]any)
			if !ok || len(handlers) == 0 {
				t.Fatal("handle not found in health route")
			}

			if handlers[0]["handler"] != "static_response" {
				t.Errorf("health handler type = %v, want 'static_response'", handlers[0]["handler"])
			}

			body, ok := handlers[0]["body"].(string)
			if !ok {
				t.Fatal("body not found in health route handler")
			}
			if tt.wantVersion != "" && body == "" {
				t.Error("expected non-empty body in health route handler")
			}
		})
	}
}

func TestBuildSecurityHeadersHandler(t *testing.T) {
	tests := []struct {
		name           string
		cfg            ports.SecurityHeadersConfig
		tlsEnabled     bool
		wantHeaders    map[string]string
		absentHeaders  []string
	}{
		{
			name: "HSTS with all options over TLS",
			cfg: ports.SecurityHeadersConfig{
				HSTSMaxAge:            31536000,
				HSTSIncludeSubDomains: true,
				HSTSPreload:           true,
			},
			tlsEnabled: true,
			wantHeaders: map[string]string{
				"Strict-Transport-Security": "max-age=31536000; includeSubDomains; preload",
			},
		},
		{
			name: "HSTS not included when TLS disabled",
			cfg: ports.SecurityHeadersConfig{
				HSTSMaxAge:            31536000,
				HSTSIncludeSubDomains: true,
				HSTSPreload:           true,
			},
			tlsEnabled:    false,
			wantHeaders:   map[string]string{},
			absentHeaders: []string{"Strict-Transport-Security"},
		},
		{
			name: "HSTS without subdomains and preload over TLS",
			cfg: ports.SecurityHeadersConfig{
				HSTSMaxAge: 3600,
			},
			tlsEnabled: true,
			wantHeaders: map[string]string{
				"Strict-Transport-Security": "max-age=3600",
			},
		},
		{
			name: "content type nosniff",
			cfg: ports.SecurityHeadersConfig{
				ContentTypeNosniff: true,
			},
			tlsEnabled: false,
			wantHeaders: map[string]string{
				"X-Content-Type-Options": "nosniff",
			},
		},
		{
			name: "frame options DENY",
			cfg: ports.SecurityHeadersConfig{
				FrameOption: "DENY",
			},
			tlsEnabled: false,
			wantHeaders: map[string]string{
				"X-Frame-Options": "DENY",
			},
		},
		{
			name: "CSP header",
			cfg: ports.SecurityHeadersConfig{
				ContentSecurityPolicy: "default-src 'self'",
			},
			tlsEnabled: false,
			wantHeaders: map[string]string{
				"Content-Security-Policy": "default-src 'self'",
			},
		},
		{
			name: "referrer policy",
			cfg: ports.SecurityHeadersConfig{
				ReferrerPolicy: "strict-origin-when-cross-origin",
			},
			tlsEnabled: false,
			wantHeaders: map[string]string{
				"Referrer-Policy": "strict-origin-when-cross-origin",
			},
		},
		{
			name: "permissions policy",
			cfg: ports.SecurityHeadersConfig{
				PermissionsPolicy: "camera=(), microphone=()",
			},
			tlsEnabled: false,
			wantHeaders: map[string]string{
				"Permissions-Policy": "camera=(), microphone=()",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := buildSecurityHeadersHandler(tt.cfg, tt.tlsEnabled)

			if handler["handler"] != "headers" {
				t.Errorf("handler type = %v, want 'headers'", handler["handler"])
			}

			response, ok := handler["response"].(map[string]any)
			if !ok {
				t.Fatal("response not found in handler")
			}

			setHeaders, ok := response["set"].(map[string][]string)
			if !ok {
				t.Fatal("set not found in response")
			}

			for headerName, wantValue := range tt.wantHeaders {
				values, found := setHeaders[headerName]
				if !found {
					t.Errorf("header %q not found", headerName)
					continue
				}
				if len(values) == 0 || values[0] != wantValue {
					t.Errorf("header %q = %v, want %q", headerName, values, wantValue)
				}
			}

			for _, headerName := range tt.absentHeaders {
				if _, found := setHeaders[headerName]; found {
					t.Errorf("header %q must not be present when TLS is disabled", headerName)
				}
			}
		})
	}
}

// extractServer is a helper to navigate the Caddy config structure to the server map.
func extractServer(t *testing.T, result map[string]any) map[string]any {
	t.Helper()

	apps, ok := result["apps"].(map[string]any)
	if !ok {
		t.Fatal("apps not found in config")
	}

	httpApp, ok := apps["http"].(map[string]any)
	if !ok {
		t.Fatal("http app not found in apps")
	}

	servers, ok := httpApp["servers"].(map[string]any)
	if !ok {
		t.Fatal("servers not found in http app")
	}

	server, ok := servers["vibewarden"].(map[string]any)
	if !ok {
		t.Fatal("vibewarden server not found in servers")
	}

	return server
}
