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
			name: "valid local config without TLS",
			cfg: &ports.ProxyConfig{
				ListenAddr:   "127.0.0.1:8080",
				UpstreamAddr: "127.0.0.1:3000",
			},
			wantErr: false,
		},
		{
			name: "letsencrypt requires domain",
			cfg: &ports.ProxyConfig{
				ListenAddr:   "127.0.0.1:8443",
				UpstreamAddr: "127.0.0.1:3000",
				TLS: ports.TLSConfig{
					Enabled:  true,
					Provider: ports.TLSProviderLetsEncrypt,
					Domain:   "",
				},
			},
			wantErr: true,
		},
		{
			name: "letsencrypt with domain is valid",
			cfg: &ports.ProxyConfig{
				ListenAddr:   "0.0.0.0:443",
				UpstreamAddr: "127.0.0.1:3000",
				TLS: ports.TLSConfig{
					Enabled:  true,
					Provider: ports.TLSProviderLetsEncrypt,
					Domain:   "example.com",
				},
			},
			wantErr: false,
		},
		{
			name: "external requires cert_path and key_path",
			cfg: &ports.ProxyConfig{
				ListenAddr:   "0.0.0.0:443",
				UpstreamAddr: "127.0.0.1:3000",
				TLS: ports.TLSConfig{
					Enabled:  true,
					Provider: ports.TLSProviderExternal,
				},
			},
			wantErr: true,
		},
		{
			name: "external with only cert_path is invalid",
			cfg: &ports.ProxyConfig{
				ListenAddr:   "0.0.0.0:443",
				UpstreamAddr: "127.0.0.1:3000",
				TLS: ports.TLSConfig{
					Enabled:  true,
					Provider: ports.TLSProviderExternal,
					CertPath: "/etc/certs/cert.pem",
				},
			},
			wantErr: true,
		},
		{
			name: "external with cert and key is valid",
			cfg: &ports.ProxyConfig{
				ListenAddr:   "0.0.0.0:443",
				UpstreamAddr: "127.0.0.1:3000",
				TLS: ports.TLSConfig{
					Enabled:  true,
					Provider: ports.TLSProviderExternal,
					CertPath: "/etc/certs/cert.pem",
					KeyPath:  "/etc/certs/key.pem",
				},
			},
			wantErr: false,
		},
		{
			name: "self-signed without domain is valid",
			cfg: &ports.ProxyConfig{
				ListenAddr:   "127.0.0.1:8443",
				UpstreamAddr: "127.0.0.1:3000",
				TLS: ports.TLSConfig{
					Enabled:  true,
					Provider: ports.TLSProviderSelfSigned,
				},
			},
			wantErr: false,
		},
		{
			name: "unknown provider is invalid",
			cfg: &ports.ProxyConfig{
				ListenAddr:   "0.0.0.0:443",
				UpstreamAddr: "127.0.0.1:3000",
				TLS: ports.TLSConfig{
					Enabled:  true,
					Provider: "cloudflare",
				},
			},
			wantErr: true,
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
		name string
		cfg  *ports.ProxyConfig
	}{
		{
			name: "TLS disabled",
			cfg: &ports.ProxyConfig{
				ListenAddr:   "0.0.0.0:8080",
				UpstreamAddr: "127.0.0.1:3000",
				TLS:          ports.TLSConfig{Enabled: false},
			},
		},
		{
			name: "TLS enabled with self-signed",
			cfg: &ports.ProxyConfig{
				ListenAddr:   "127.0.0.1:8443",
				UpstreamAddr: "127.0.0.1:3000",
				TLS: ports.TLSConfig{
					Enabled:  true,
					Provider: ports.TLSProviderSelfSigned,
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
			autoHTTPS, ok := server["automatic_https"].(map[string]any)
			if !ok {
				t.Fatal("automatic_https not found in server config")
			}

			disabled, ok := autoHTTPS["disable"].(bool)
			if !ok {
				t.Fatal("automatic_https.disable not found")
			}

			if !disabled {
				t.Error("automatic_https.disable must always be true (TLS is managed explicitly)")
			}
		})
	}
}

func TestBuildCaddyConfig_TLSProviders(t *testing.T) {
	tests := []struct {
		name             string
		cfg              *ports.ProxyConfig
		wantTLSApp       bool
		wantACMEModule   bool
		wantInternalIss  bool
		wantLoadFiles    bool
		wantRedirectSvr  bool
		wantStorageBlock bool
	}{
		{
			name: "letsencrypt provider builds ACME automation",
			cfg: &ports.ProxyConfig{
				ListenAddr:   "0.0.0.0:443",
				UpstreamAddr: "127.0.0.1:3000",
				TLS: ports.TLSConfig{
					Enabled:  true,
					Provider: ports.TLSProviderLetsEncrypt,
					Domain:   "example.com",
				},
			},
			wantTLSApp:      true,
			wantACMEModule:  true,
			wantRedirectSvr: true,
		},
		{
			name: "letsencrypt with storage path",
			cfg: &ports.ProxyConfig{
				ListenAddr:   "0.0.0.0:443",
				UpstreamAddr: "127.0.0.1:3000",
				TLS: ports.TLSConfig{
					Enabled:     true,
					Provider:    ports.TLSProviderLetsEncrypt,
					Domain:      "example.com",
					StoragePath: "/data/certs",
				},
			},
			wantTLSApp:       true,
			wantACMEModule:   true,
			wantRedirectSvr:  true,
			wantStorageBlock: true,
		},
		{
			name: "self-signed provider builds internal issuer",
			cfg: &ports.ProxyConfig{
				ListenAddr:   "127.0.0.1:8443",
				UpstreamAddr: "127.0.0.1:3000",
				TLS: ports.TLSConfig{
					Enabled:  true,
					Provider: ports.TLSProviderSelfSigned,
				},
			},
			wantTLSApp:      true,
			wantInternalIss: true,
			wantRedirectSvr: true,
		},
		{
			name: "external provider loads cert files",
			cfg: &ports.ProxyConfig{
				ListenAddr:   "0.0.0.0:443",
				UpstreamAddr: "127.0.0.1:3000",
				TLS: ports.TLSConfig{
					Enabled:  true,
					Provider: ports.TLSProviderExternal,
					CertPath: "/etc/certs/cert.pem",
					KeyPath:  "/etc/certs/key.pem",
				},
			},
			wantTLSApp:      true,
			wantLoadFiles:   true,
			wantRedirectSvr: true,
		},
		{
			name: "TLS disabled produces no TLS app",
			cfg: &ports.ProxyConfig{
				ListenAddr:   "127.0.0.1:8080",
				UpstreamAddr: "127.0.0.1:3000",
				TLS:          ports.TLSConfig{Enabled: false},
			},
			wantTLSApp:      false,
			wantRedirectSvr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := BuildCaddyConfig(tt.cfg)
			if err != nil {
				t.Fatalf("BuildCaddyConfig() unexpected error: %v", err)
			}

			apps, ok := result["apps"].(map[string]any)
			if !ok {
				t.Fatal("apps not found in config")
			}

			_, hasTLSApp := apps["tls"]
			if hasTLSApp != tt.wantTLSApp {
				t.Errorf("tls app present = %v, want %v", hasTLSApp, tt.wantTLSApp)
			}

			httpApp, ok := apps["http"].(map[string]any)
			if !ok {
				t.Fatal("http app not found")
			}
			servers, ok := httpApp["servers"].(map[string]any)
			if !ok {
				t.Fatal("servers not found in http app")
			}
			_, hasRedirect := servers["vibewarden_redirect"]
			if hasRedirect != tt.wantRedirectSvr {
				t.Errorf("redirect server present = %v, want %v", hasRedirect, tt.wantRedirectSvr)
			}

			if !tt.wantTLSApp {
				return
			}

			tlsApp, ok := apps["tls"].(map[string]any)
			if !ok {
				t.Fatal("tls app is not a map")
			}

			if tt.wantACMEModule {
				if !tlsAppHasIssuerModule(tlsApp, "acme") {
					t.Error("expected ACME issuer module in TLS automation policies")
				}
			}

			if tt.wantInternalIss {
				if !tlsAppHasIssuerModule(tlsApp, "internal") {
					t.Error("expected internal issuer module in TLS automation policies")
				}
			}

			if tt.wantLoadFiles {
				certs, ok := tlsApp["certificates"].(map[string]any)
				if !ok {
					t.Fatal("certificates not found in tls app")
				}
				loadFiles, ok := certs["load_files"].([]map[string]any)
				if !ok || len(loadFiles) == 0 {
					t.Fatal("load_files not found or empty in tls.certificates")
				}
			}

			if tt.wantStorageBlock {
				if _, ok := tlsApp["storage"]; !ok {
					t.Error("expected storage block in tls app")
				}
			}
		})
	}
}

// tlsAppHasIssuerModule checks whether any automation policy in the TLS app
// uses an issuer with the given module name.
func tlsAppHasIssuerModule(tlsApp map[string]any, module string) bool {
	automation, ok := tlsApp["automation"].(map[string]any)
	if !ok {
		return false
	}
	policies, ok := automation["policies"].([]map[string]any)
	if !ok {
		return false
	}
	for _, policy := range policies {
		issuers, ok := policy["issuers"].([]map[string]any)
		if !ok {
			continue
		}
		for _, issuer := range issuers {
			if issuer["module"] == module {
				return true
			}
		}
	}
	return false
}

func TestBuildCaddyConfig_LetsEncryptDomainInPolicy(t *testing.T) {
	cfg := &ports.ProxyConfig{
		ListenAddr:   "0.0.0.0:443",
		UpstreamAddr: "127.0.0.1:3000",
		TLS: ports.TLSConfig{
			Enabled:  true,
			Provider: ports.TLSProviderLetsEncrypt,
			Domain:   "myapp.example.com",
		},
	}

	result, err := BuildCaddyConfig(cfg)
	if err != nil {
		t.Fatalf("BuildCaddyConfig() unexpected error: %v", err)
	}

	apps := result["apps"].(map[string]any)
	tlsApp := apps["tls"].(map[string]any)
	automation := tlsApp["automation"].(map[string]any)
	policies := automation["policies"].([]map[string]any)

	if len(policies) == 0 {
		t.Fatal("expected at least one automation policy")
	}

	subjects, ok := policies[0]["subjects"].([]string)
	if !ok || len(subjects) == 0 {
		t.Fatal("subjects not found in first automation policy")
	}
	if subjects[0] != "myapp.example.com" {
		t.Errorf("subjects[0] = %q, want %q", subjects[0], "myapp.example.com")
	}
}

func TestBuildCaddyConfig_ExternalTLSPolicy(t *testing.T) {
	cfg := &ports.ProxyConfig{
		ListenAddr:   "0.0.0.0:443",
		UpstreamAddr: "127.0.0.1:3000",
		TLS: ports.TLSConfig{
			Enabled:  true,
			Provider: ports.TLSProviderExternal,
			CertPath: "/certs/cert.pem",
			KeyPath:  "/certs/key.pem",
		},
	}

	result, err := BuildCaddyConfig(cfg)
	if err != nil {
		t.Fatalf("BuildCaddyConfig() unexpected error: %v", err)
	}

	server := extractServer(t, result)
	policies, ok := server["tls_connection_policies"].([]map[string]any)
	if !ok || len(policies) == 0 {
		t.Fatal("tls_connection_policies not found")
	}

	certSel, ok := policies[0]["certificate_selection"].(map[string]any)
	if !ok {
		t.Fatal("certificate_selection not found in tls policy")
	}
	tags, ok := certSel["any_tag"].([]string)
	if !ok || len(tags) == 0 {
		t.Fatal("any_tag not found in certificate_selection")
	}
	if tags[0] != "vibewarden_external" {
		t.Errorf("any_tag[0] = %q, want %q", tags[0], "vibewarden_external")
	}
}

func TestBuildCaddyConfig_HTTPRedirectServer(t *testing.T) {
	cfg := &ports.ProxyConfig{
		ListenAddr:   "0.0.0.0:443",
		UpstreamAddr: "127.0.0.1:3000",
		TLS: ports.TLSConfig{
			Enabled:  true,
			Provider: ports.TLSProviderSelfSigned,
		},
	}

	result, err := BuildCaddyConfig(cfg)
	if err != nil {
		t.Fatalf("BuildCaddyConfig() unexpected error: %v", err)
	}

	apps := result["apps"].(map[string]any)
	httpApp := apps["http"].(map[string]any)
	servers := httpApp["servers"].(map[string]any)

	redirect, ok := servers["vibewarden_redirect"].(map[string]any)
	if !ok {
		t.Fatal("vibewarden_redirect server not found")
	}

	listen, ok := redirect["listen"].([]string)
	if !ok || len(listen) == 0 {
		t.Fatal("listen not found in redirect server")
	}
	if listen[0] != ":80" {
		t.Errorf("redirect listen = %q, want %q", listen[0], ":80")
	}

	routes, ok := redirect["routes"].([]map[string]any)
	if !ok || len(routes) == 0 {
		t.Fatal("routes not found in redirect server")
	}

	handlers, ok := routes[0]["handle"].([]map[string]any)
	if !ok || len(handlers) == 0 {
		t.Fatal("handle not found in redirect route")
	}

	if handlers[0]["status_code"] != 301 {
		t.Errorf("redirect status_code = %v, want 301", handlers[0]["status_code"])
	}

	autoHTTPS, ok := redirect["automatic_https"].(map[string]any)
	if !ok {
		t.Fatal("automatic_https not found in redirect server")
	}
	if disabled, _ := autoHTTPS["disable"].(bool); !disabled {
		t.Error("redirect server automatic_https.disable must be true")
	}
}

func TestBuildCaddyConfig_TLSDisabled_NoRedirectServer(t *testing.T) {
	cfg := &ports.ProxyConfig{
		ListenAddr:   "127.0.0.1:8080",
		UpstreamAddr: "127.0.0.1:3000",
		TLS:          ports.TLSConfig{Enabled: false},
	}

	result, err := BuildCaddyConfig(cfg)
	if err != nil {
		t.Fatalf("BuildCaddyConfig() unexpected error: %v", err)
	}

	apps := result["apps"].(map[string]any)
	httpApp := apps["http"].(map[string]any)
	servers := httpApp["servers"].(map[string]any)

	if _, ok := servers["vibewarden_redirect"]; ok {
		t.Error("redirect server must not be present when TLS is disabled")
	}

	if _, ok := apps["tls"]; ok {
		t.Error("tls app must not be present when TLS is disabled")
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
	if len(routes) < 2 {
		t.Fatalf("expected at least 2 routes (health + proxy), got %d", len(routes))
	}

	handlers, ok := routes[1]["handle"].([]map[string]any)
	if !ok {
		t.Fatal("handle not found in proxy route")
	}

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
	if len(routes) < 2 {
		t.Fatalf("expected at least 2 routes (health + proxy), got %d", len(routes))
	}

	handlers, ok := routes[1]["handle"].([]map[string]any)
	if !ok {
		t.Fatal("handle not found in proxy route")
	}

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
	if len(routes) < 2 {
		t.Fatalf("expected at least 2 routes (health + proxy), got %d", len(routes))
	}
	handlers, ok := routes[1]["handle"].([]map[string]any)
	if !ok {
		t.Fatal("handle not found in proxy route")
	}

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
		name          string
		cfg           ports.SecurityHeadersConfig
		tlsEnabled    bool
		wantHeaders   map[string]string
		absentHeaders []string
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
