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
			name: "localhost disables automatic HTTPS",
			cfg: &ports.ProxyConfig{
				ListenAddr:   "127.0.0.1:8080",
				UpstreamAddr: "127.0.0.1:3000",
			},
			wantDisableAH: true,
		},
		{
			name: "localhost keyword disables automatic HTTPS",
			cfg: &ports.ProxyConfig{
				ListenAddr:   "localhost:8080",
				UpstreamAddr: "localhost:3000",
			},
			wantDisableAH: true,
		},
		{
			name: "TLS disabled also disables automatic HTTPS",
			cfg: &ports.ProxyConfig{
				ListenAddr:   "0.0.0.0:8080",
				UpstreamAddr: "127.0.0.1:3000",
				TLS: ports.TLSConfig{
					Enabled: false,
				},
			},
			wantDisableAH: true,
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
	if len(routes) == 0 {
		t.Fatal("expected at least one route")
	}

	handlers, ok := routes[0]["handle"].([]map[string]any)
	if !ok {
		t.Fatal("handle not found in route")
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
	if len(routes) == 0 {
		t.Fatal("expected at least one route")
	}

	handlers, ok := routes[0]["handle"].([]map[string]any)
	if !ok {
		t.Fatal("handle not found in route")
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
	handlers, ok := routes[0]["handle"].([]map[string]any)
	if !ok {
		t.Fatal("handle not found in route")
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

func TestBuildSecurityHeadersHandler(t *testing.T) {
	tests := []struct {
		name        string
		cfg         ports.SecurityHeadersConfig
		wantHeaders map[string]string
	}{
		{
			name: "HSTS with all options",
			cfg: ports.SecurityHeadersConfig{
				HSTSMaxAge:            31536000,
				HSTSIncludeSubDomains: true,
				HSTSPreload:           true,
			},
			wantHeaders: map[string]string{
				"Strict-Transport-Security": "max-age=31536000; includeSubDomains; preload",
			},
		},
		{
			name: "HSTS without subdomains and preload",
			cfg: ports.SecurityHeadersConfig{
				HSTSMaxAge: 3600,
			},
			wantHeaders: map[string]string{
				"Strict-Transport-Security": "max-age=3600",
			},
		},
		{
			name: "content type nosniff",
			cfg: ports.SecurityHeadersConfig{
				ContentTypeNosniff: true,
			},
			wantHeaders: map[string]string{
				"X-Content-Type-Options": "nosniff",
			},
		},
		{
			name: "frame options DENY",
			cfg: ports.SecurityHeadersConfig{
				FrameOption: "DENY",
			},
			wantHeaders: map[string]string{
				"X-Frame-Options": "DENY",
			},
		},
		{
			name: "CSP header",
			cfg: ports.SecurityHeadersConfig{
				ContentSecurityPolicy: "default-src 'self'",
			},
			wantHeaders: map[string]string{
				"Content-Security-Policy": "default-src 'self'",
			},
		},
		{
			name: "referrer policy",
			cfg: ports.SecurityHeadersConfig{
				ReferrerPolicy: "strict-origin-when-cross-origin",
			},
			wantHeaders: map[string]string{
				"Referrer-Policy": "strict-origin-when-cross-origin",
			},
		},
		{
			name: "permissions policy",
			cfg: ports.SecurityHeadersConfig{
				PermissionsPolicy: "camera=(), microphone=()",
			},
			wantHeaders: map[string]string{
				"Permissions-Policy": "camera=(), microphone=()",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := buildSecurityHeadersHandler(tt.cfg)

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
		})
	}
}

func TestIsLocalAddress(t *testing.T) {
	tests := []struct {
		name string
		addr string
		want bool
	}{
		{"loopback IP with port", "127.0.0.1:8080", true},
		{"loopback IP without port", "127.0.0.1", true},
		{"localhost with port", "localhost:8080", true},
		{"localhost without port", "localhost", true},
		{"empty", "", true},
		{"public IP", "1.2.3.4:8080", false},
		{"public IP without port", "1.2.3.4", false},
		{"all interfaces", "0.0.0.0:8080", false},
		{"IPv6 loopback", "::1", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isLocalAddress(tt.addr)
			if got != tt.want {
				t.Errorf("isLocalAddress(%q) = %v, want %v", tt.addr, got, tt.want)
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
