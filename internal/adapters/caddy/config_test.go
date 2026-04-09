package caddy

import (
	"testing"
	"time"

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
	t.Run("TLS disabled — automatic_https fully disabled", func(t *testing.T) {
		cfg := &ports.ProxyConfig{
			ListenAddr:   "0.0.0.0:8080",
			UpstreamAddr: "127.0.0.1:3000",
			TLS:          ports.TLSConfig{Enabled: false},
		}
		result, err := BuildCaddyConfig(cfg)
		if err != nil {
			t.Fatalf("BuildCaddyConfig() unexpected error: %v", err)
		}
		server := extractServer(t, result)
		autoHTTPS, ok := server["automatic_https"].(map[string]any)
		if !ok {
			t.Fatal("automatic_https not found in server config")
		}
		if disabled, _ := autoHTTPS["disable"].(bool); !disabled {
			t.Error("automatic_https.disable must be true when TLS is disabled")
		}
	})

	t.Run("TLS self-signed — redirects disabled but cert management active", func(t *testing.T) {
		cfg := &ports.ProxyConfig{
			ListenAddr:   "127.0.0.1:8443",
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
		server := extractServer(t, result)
		autoHTTPS, ok := server["automatic_https"].(map[string]any)
		if !ok {
			t.Fatal("automatic_https not found in server config")
		}
		if disabled, _ := autoHTTPS["disable"].(bool); disabled {
			t.Error("automatic_https.disable must NOT be true when TLS is enabled")
		}
		if redirectsDisabled, _ := autoHTTPS["disable_redirects"].(bool); !redirectsDisabled {
			t.Error("automatic_https.disable_redirects must be true for self-signed provider")
		}
	})

	t.Run("TLS letsencrypt — automatic_https not overridden (Caddy owns port 80)", func(t *testing.T) {
		// For the letsencrypt provider, Caddy's built-in automatic HTTPS handles
		// both ACME HTTP-01 challenges and HTTP→HTTPS redirects on port 80. We
		// must NOT set disable_redirects, which would prevent redirects from
		// working after ACME cert issuance.
		cfg := &ports.ProxyConfig{
			ListenAddr:   "0.0.0.0:443",
			UpstreamAddr: "127.0.0.1:3000",
			TLS: ports.TLSConfig{
				Enabled:  true,
				Provider: ports.TLSProviderLetsEncrypt,
				Domain:   "example.com",
			},
		}
		result, err := BuildCaddyConfig(cfg)
		if err != nil {
			t.Fatalf("BuildCaddyConfig() unexpected error: %v", err)
		}
		server := extractServer(t, result)
		// automatic_https must be absent — we do not set it for letsencrypt so
		// Caddy's default automatic HTTPS behaviour (ACME + redirects) is active.
		if autoHTTPS, ok := server["automatic_https"]; ok {
			t.Errorf("automatic_https must not be set for letsencrypt provider, got: %v", autoHTTPS)
		}
	})
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
			wantRedirectSvr: false, // Caddy handles port 80 natively for ACME + redirects
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
			wantRedirectSvr:  false, // Caddy handles port 80 natively for ACME + redirects
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
				// Storage is a top-level Caddy config field, not inside apps.tls.
				// Caddy's Config struct has `json:"storage"` at the root level;
				// placing it inside apps.tls causes Caddy to reject it with
				// "unknown field: storage".
				storage, ok := result["storage"].(map[string]any)
				if !ok {
					t.Error("expected storage block at top-level Caddy config, not inside apps.tls")
				} else {
					if storage["module"] != "file_system" {
						t.Errorf("storage.module = %q, want %q", storage["module"], "file_system")
					}
				}
				// Confirm storage is NOT inside the tls app.
				if _, ok := tlsApp["storage"]; ok {
					t.Error("storage must NOT appear inside apps.tls — it causes an 'unknown field' error in Caddy")
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

// TestBuildCaddyConfig_LetsEncryptACMEChallenges verifies that the ACME issuer
// configuration includes an explicit HTTP-01 challenge with alternate_port 80.
// Without this, Caddy may fail ACME challenges in Docker when port detection
// is unreliable, preventing proactive certificate issuance.
func TestBuildCaddyConfig_LetsEncryptACMEChallenges(t *testing.T) {
	cfg := &ports.ProxyConfig{
		ListenAddr:   "0.0.0.0:443",
		UpstreamAddr: "127.0.0.1:3000",
		TLS: ports.TLSConfig{
			Enabled:  true,
			Provider: ports.TLSProviderLetsEncrypt,
			Domain:   "example.com",
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

	issuers, ok := policies[0]["issuers"].([]map[string]any)
	if !ok || len(issuers) == 0 {
		t.Fatal("issuers not found in automation policy")
	}

	acmeIssuer := issuers[0]
	if acmeIssuer["module"] != "acme" {
		t.Fatalf("expected acme issuer module, got %q", acmeIssuer["module"])
	}

	challenges, ok := acmeIssuer["challenges"].(map[string]any)
	if !ok {
		t.Fatal("challenges not found in ACME issuer config — HTTP-01 challenge must be configured explicitly")
	}

	http01, ok := challenges["http"].(map[string]any)
	if !ok {
		t.Fatal("http challenge config not found")
	}

	alternatePort, ok := http01["alternate_port"].(int)
	if !ok {
		t.Fatal("alternate_port not found in http challenge config")
	}
	if alternatePort != 80 {
		t.Errorf("alternate_port = %d, want 80", alternatePort)
	}
}

// TestBuildCaddyConfig_StorageAtTopLevel verifies that when StoragePath is set,
// the storage module appears at the top level of the Caddy config (not inside
// apps.tls). Placing storage inside apps.tls causes Caddy to reject the config
// with "unknown field: storage".
func TestBuildCaddyConfig_StorageAtTopLevel(t *testing.T) {
	cfg := &ports.ProxyConfig{
		ListenAddr:   "0.0.0.0:443",
		UpstreamAddr: "127.0.0.1:3000",
		TLS: ports.TLSConfig{
			Enabled:     true,
			Provider:    ports.TLSProviderLetsEncrypt,
			Domain:      "example.com",
			StoragePath: "/data/caddy",
		},
	}

	result, err := BuildCaddyConfig(cfg)
	if err != nil {
		t.Fatalf("BuildCaddyConfig() unexpected error: %v", err)
	}

	// Storage must be at the root of the Caddy config.
	storage, ok := result["storage"].(map[string]any)
	if !ok {
		t.Fatal("storage not found at top-level Caddy config")
	}
	if storage["module"] != "file_system" {
		t.Errorf("storage.module = %q, want %q", storage["module"], "file_system")
	}
	if storage["root"] != "/data/caddy" {
		t.Errorf("storage.root = %q, want %q", storage["root"], "/data/caddy")
	}

	// Storage must NOT be inside apps.tls.
	apps := result["apps"].(map[string]any)
	tlsApp, ok := apps["tls"].(map[string]any)
	if !ok {
		t.Fatal("tls app not found")
	}
	if _, ok := tlsApp["storage"]; ok {
		t.Error("storage must NOT appear inside apps.tls — it causes Caddy to reject the config")
	}
}

// TestBuildCaddyConfig_StorageAbsentWhenNoPath verifies that no storage block
// is emitted at the top level when StoragePath is empty.
func TestBuildCaddyConfig_StorageAbsentWhenNoPath(t *testing.T) {
	cfg := &ports.ProxyConfig{
		ListenAddr:   "0.0.0.0:443",
		UpstreamAddr: "127.0.0.1:3000",
		TLS: ports.TLSConfig{
			Enabled:  true,
			Provider: ports.TLSProviderLetsEncrypt,
			Domain:   "example.com",
			// StoragePath intentionally empty — Caddy uses its default data dir.
		},
	}

	result, err := BuildCaddyConfig(cfg)
	if err != nil {
		t.Fatalf("BuildCaddyConfig() unexpected error: %v", err)
	}

	if _, ok := result["storage"]; ok {
		t.Error("storage must NOT appear in top-level Caddy config when StoragePath is empty")
	}
}

func TestBuildCaddyConfig_SelfSignedDefaultsToLocalhost(t *testing.T) {
	cfg := &ports.ProxyConfig{
		ListenAddr:   "0.0.0.0:8443",
		UpstreamAddr: "127.0.0.1:3000",
		TLS: ports.TLSConfig{
			Enabled:  true,
			Provider: ports.TLSProviderSelfSigned,
			// No domain — should default to "localhost"
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
		t.Fatal("subjects not found — self-signed must default to localhost")
	}
	if subjects[0] != "localhost" {
		t.Errorf("subjects[0] = %q, want %q", subjects[0], "localhost")
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
	if len(routes) < 3 {
		t.Fatalf("expected at least 3 routes (health + ready + proxy), got %d", len(routes))
	}

	handlers, ok := routes[2]["handle"].([]map[string]any)
	if !ok {
		t.Fatal("handle not found in proxy route")
	}

	// Minimum chain: strip_headers + security_headers + admin_auth + reverse_proxy.
	if len(handlers) < 4 {
		t.Fatalf("expected at least 4 handlers (strip+security+admin_auth+reverse_proxy), got %d", len(handlers))
	}

	// handlers[0] must be the user-header strip handler (a "headers" handler with
	// a "request.delete" key, not a "response" key).
	stripHandler := handlers[0]
	if stripHandler["handler"] != "headers" {
		t.Errorf("handlers[0] type = %v, want 'headers'", stripHandler["handler"])
	}
	req, ok := stripHandler["request"].(map[string]any)
	if !ok {
		t.Fatal("handlers[0] missing 'request' key — expected user-header strip handler")
	}
	if _, hasDelete := req["delete"]; !hasDelete {
		t.Error("handlers[0].request missing 'delete' key — expected user-header strip handler")
	}

	// handlers[1] must be the security headers handler (a "headers" handler with
	// a "response" key).
	secHandler := handlers[1]
	if secHandler["handler"] != "headers" {
		t.Errorf("handlers[1] type = %v, want 'headers'", secHandler["handler"])
	}
	if _, hasResponse := secHandler["response"]; !hasResponse {
		t.Error("handlers[1] missing 'response' key — expected security headers handler")
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
	if len(routes) < 3 {
		t.Fatalf("expected at least 3 routes (health + ready + proxy), got %d", len(routes))
	}

	handlers, ok := routes[2]["handle"].([]map[string]any)
	if !ok {
		t.Fatal("handle not found in proxy route")
	}

	// With security headers disabled and rate limiting disabled the chain is:
	// strip_headers → admin_auth → reverse_proxy.
	if len(handlers) != 3 {
		t.Fatalf("expected 3 handlers (strip_headers+admin_auth+reverse_proxy), got %d", len(handlers))
	}

	if handlers[0]["handler"] != "headers" {
		t.Errorf("handlers[0] = %v, want 'headers' (user-header strip)", handlers[0]["handler"])
	}
	req, ok := handlers[0]["request"].(map[string]any)
	if !ok {
		t.Fatal("handlers[0] missing 'request' key — expected user-header strip handler")
	}
	if _, hasDelete := req["delete"]; !hasDelete {
		t.Error("handlers[0].request missing 'delete' key — expected user-header strip handler")
	}

	if handlers[1]["handler"] != "vibewarden_admin_auth" {
		t.Errorf("handlers[1] = %v, want 'vibewarden_admin_auth'", handlers[1]["handler"])
	}

	if handlers[2]["handler"] != "reverse_proxy" {
		t.Errorf("handlers[2] = %v, want 'reverse_proxy'", handlers[2]["handler"])
	}
}

// TestBuildCaddyConfig_UserHeaderStripIsFirst verifies that the X-User-* header
// strip handler is always the very first handler in the catch-all route's chain,
// regardless of which other middleware are enabled.
func TestBuildCaddyConfig_UserHeaderStripIsFirst(t *testing.T) {
	tests := []struct {
		name string
		cfg  *ports.ProxyConfig
	}{
		{
			name: "strip handler is first when security headers disabled",
			cfg: &ports.ProxyConfig{
				ListenAddr:   "127.0.0.1:8080",
				UpstreamAddr: "127.0.0.1:3000",
			},
		},
		{
			name: "strip handler is first when security headers enabled",
			cfg: &ports.ProxyConfig{
				ListenAddr:   "127.0.0.1:8080",
				UpstreamAddr: "127.0.0.1:3000",
				SecurityHeaders: ports.SecurityHeadersConfig{
					Enabled:            true,
					ContentTypeNosniff: true,
				},
			},
		},
		{
			name: "strip handler is first when rate limiting enabled",
			cfg: &ports.ProxyConfig{
				ListenAddr:   "127.0.0.1:8080",
				UpstreamAddr: "127.0.0.1:3000",
				RateLimit: ports.RateLimitConfig{
					Enabled: true,
					PerIP: ports.RateLimitRule{
						RequestsPerSecond: 10,
						Burst:             20,
					},
				},
			},
		},
		{
			name: "strip handler is first when TLS enabled",
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
			routes, ok := server["routes"].([]map[string]any)
			if !ok || len(routes) == 0 {
				t.Fatal("routes not found in server config")
			}

			// The catch-all route is always last.
			catchAll := routes[len(routes)-1]
			handlers, ok := catchAll["handle"].([]map[string]any)
			if !ok || len(handlers) == 0 {
				t.Fatal("handle not found in catch-all route")
			}

			first := handlers[0]
			if first["handler"] != "headers" {
				t.Errorf("handlers[0].handler = %v, want 'headers'", first["handler"])
			}
			req, ok := first["request"].(map[string]any)
			if !ok {
				t.Fatal("handlers[0] missing 'request' key — expected user-header strip handler first")
			}
			if _, hasDelete := req["delete"]; !hasDelete {
				t.Fatal("handlers[0].request missing 'delete' key — expected user-header strip handler first")
			}
		})
	}
}

// TestBuildCaddyConfig_UserHeaderStripDeletesCorrectHeaders verifies that the
// strip handler targets exactly X-User-Id, X-User-Email, and X-User-Verified.
func TestBuildCaddyConfig_UserHeaderStripDeletesCorrectHeaders(t *testing.T) {
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
	if !ok || len(routes) == 0 {
		t.Fatal("routes not found in server config")
	}

	catchAll := routes[len(routes)-1]
	handlers, ok := catchAll["handle"].([]map[string]any)
	if !ok || len(handlers) == 0 {
		t.Fatal("handle not found in catch-all route")
	}

	stripHandler := handlers[0]
	req, ok := stripHandler["request"].(map[string]any)
	if !ok {
		t.Fatal("handlers[0] missing 'request' key")
	}
	deleted, ok := req["delete"].([]string)
	if !ok {
		t.Fatal("handlers[0].request.delete is not []string")
	}

	wantDeleted := map[string]bool{
		"X-User-Id":       true,
		"X-User-Email":    true,
		"X-User-Verified": true,
	}
	for _, h := range deleted {
		delete(wantDeleted, h)
	}
	if len(wantDeleted) > 0 {
		t.Errorf("missing headers in delete list: %v", wantDeleted)
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
	if len(routes) < 3 {
		t.Fatalf("expected at least 3 routes (health + ready + proxy), got %d", len(routes))
	}
	handlers, ok := routes[2]["handle"].([]map[string]any)
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
			if len(routes) < 3 {
				t.Fatalf("expected at least 3 routes (health + ready + proxy), got %d", len(routes))
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
		{
			name: "cross-origin-opener-policy",
			cfg: ports.SecurityHeadersConfig{
				CrossOriginOpenerPolicy: "same-origin",
			},
			tlsEnabled: false,
			wantHeaders: map[string]string{
				"Cross-Origin-Opener-Policy": "same-origin",
			},
		},
		{
			name: "cross-origin-resource-policy",
			cfg: ports.SecurityHeadersConfig{
				CrossOriginResourcePolicy: "same-origin",
			},
			tlsEnabled: false,
			wantHeaders: map[string]string{
				"Cross-Origin-Resource-Policy": "same-origin",
			},
		},
		{
			name: "x-permitted-cross-domain-policies",
			cfg: ports.SecurityHeadersConfig{
				PermittedCrossDomainPolicies: "none",
			},
			tlsEnabled: false,
			wantHeaders: map[string]string{
				"X-Permitted-Cross-Domain-Policies": "none",
			},
		},
		{
			name: "empty cross-origin-opener-policy disables header",
			cfg: ports.SecurityHeadersConfig{
				CrossOriginOpenerPolicy: "",
			},
			tlsEnabled:    false,
			wantHeaders:   map[string]string{},
			absentHeaders: []string{"Cross-Origin-Opener-Policy"},
		},
		{
			name: "empty cross-origin-resource-policy disables header",
			cfg: ports.SecurityHeadersConfig{
				CrossOriginResourcePolicy: "",
			},
			tlsEnabled:    false,
			wantHeaders:   map[string]string{},
			absentHeaders: []string{"Cross-Origin-Resource-Policy"},
		},
		{
			name: "empty permitted-cross-domain-policies disables header",
			cfg: ports.SecurityHeadersConfig{
				PermittedCrossDomainPolicies: "",
			},
			tlsEnabled:    false,
			wantHeaders:   map[string]string{},
			absentHeaders: []string{"X-Permitted-Cross-Domain-Policies"},
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

func TestBuildSecurityHeadersHandler_SuppressViaHeader(t *testing.T) {
	tests := []struct {
		name           string
		suppressVia    bool
		wantDeleteList bool
	}{
		{"suppress Via when true", true, true},
		{"no deletion when false", false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := ports.SecurityHeadersConfig{SuppressViaHeader: tt.suppressVia}
			handler := buildSecurityHeadersHandler(cfg, false)

			response, ok := handler["response"].(map[string]any)
			if !ok {
				t.Fatal("response not found in handler")
			}

			deleteList, hasDelete := response["delete"]
			if tt.wantDeleteList && !hasDelete {
				t.Error("expected response.delete to be set when SuppressViaHeader is true")
			}
			if !tt.wantDeleteList && hasDelete {
				t.Errorf("unexpected response.delete: %v", deleteList)
			}
			if tt.wantDeleteList {
				dl, ok := deleteList.([]string)
				if !ok {
					t.Fatalf("response.delete = %T, want []string", deleteList)
				}
				found := false
				for _, h := range dl {
					if h == "Via" {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("response.delete = %v, want it to contain \"Via\"", dl)
				}
			}
		})
	}
}

func TestBuildCaddyConfig_KratosFlowRoutes_Present(t *testing.T) {
	cfg := &ports.ProxyConfig{
		ListenAddr:   "127.0.0.1:8080",
		UpstreamAddr: "127.0.0.1:3000",
		Auth: ports.AuthConfig{
			Enabled:         true,
			KratosPublicURL: "http://127.0.0.1:4433",
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

	// Expect: health (index 0), ready (index 1), Kratos flow route (index 2), catch-all proxy (index 3).
	if len(routes) != 4 {
		t.Fatalf("expected 4 routes (health + ready + kratos + proxy), got %d", len(routes))
	}

	kratosRoute := routes[2]

	// Verify the route has matchers for the self-service paths.
	matchers, ok := kratosRoute["match"].([]map[string]any)
	if !ok || len(matchers) == 0 {
		t.Fatal("match not found in Kratos flow route")
	}

	paths, ok := matchers[0]["path"].([]string)
	if !ok || len(paths) == 0 {
		t.Fatal("path matcher not found in Kratos flow route")
	}

	// Verify all expected self-service paths are present.
	wantPaths := map[string]bool{
		"/self-service/login/*":        true,
		"/self-service/registration/*": true,
		"/self-service/logout/*":       true,
		"/self-service/settings/*":     true,
		"/self-service/recovery/*":     true,
		"/self-service/verification/*": true,
		"/.ory/kratos/public/*":        true,
	}
	for _, p := range paths {
		delete(wantPaths, p)
	}
	if len(wantPaths) > 0 {
		t.Errorf("missing Kratos flow paths: %v", wantPaths)
	}

	// Verify the route proxies to Kratos, not to the upstream.
	handlers, ok := kratosRoute["handle"].([]map[string]any)
	if !ok || len(handlers) == 0 {
		t.Fatal("handle not found in Kratos flow route")
	}

	if handlers[0]["handler"] != "reverse_proxy" {
		t.Errorf("Kratos flow route handler = %v, want reverse_proxy", handlers[0]["handler"])
	}

	upstreams, ok := handlers[0]["upstreams"].([]map[string]any)
	if !ok || len(upstreams) == 0 {
		t.Fatal("upstreams not found in Kratos flow route handler")
	}

	if upstreams[0]["dial"] != "127.0.0.1:4433" {
		t.Errorf("Kratos upstream dial = %v, want 127.0.0.1:4433", upstreams[0]["dial"])
	}
}

func TestBuildCaddyConfig_KratosFlowRoutes_AbsentWhenAuthDisabled(t *testing.T) {
	tests := []struct {
		name string
		cfg  *ports.ProxyConfig
	}{
		{
			name: "auth disabled",
			cfg: &ports.ProxyConfig{
				ListenAddr:   "127.0.0.1:8080",
				UpstreamAddr: "127.0.0.1:3000",
				Auth: ports.AuthConfig{
					Enabled:         false,
					KratosPublicURL: "http://127.0.0.1:4433",
				},
			},
		},
		{
			name: "auth enabled but no KratosPublicURL",
			cfg: &ports.ProxyConfig{
				ListenAddr:   "127.0.0.1:8080",
				UpstreamAddr: "127.0.0.1:3000",
				Auth: ports.AuthConfig{
					Enabled:         true,
					KratosPublicURL: "",
				},
			},
		},
		{
			name: "auth not configured",
			cfg: &ports.ProxyConfig{
				ListenAddr:   "127.0.0.1:8080",
				UpstreamAddr: "127.0.0.1:3000",
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
			if !ok {
				t.Fatal("routes not found in server config")
			}

			// Without auth: health + ready + catch-all = 3 routes.
			if len(routes) != 3 {
				t.Errorf("expected 3 routes (health + ready + proxy), got %d", len(routes))
			}
		})
	}
}

func TestBuildCaddyConfig_KratosRouteBeforeCatchAll(t *testing.T) {
	cfg := &ports.ProxyConfig{
		ListenAddr:   "127.0.0.1:8080",
		UpstreamAddr: "127.0.0.1:3000",
		Auth: ports.AuthConfig{
			Enabled:         true,
			KratosPublicURL: "http://127.0.0.1:4433",
		},
	}

	result, err := BuildCaddyConfig(cfg)
	if err != nil {
		t.Fatalf("BuildCaddyConfig() unexpected error: %v", err)
	}

	server := extractServer(t, result)
	routes, ok := server["routes"].([]map[string]any)
	if !ok || len(routes) < 4 {
		t.Fatalf("expected at least 4 routes, got %d", len(routes))
	}

	// Index 0 — health check: has a path matcher.
	healthRoute := routes[0]
	if _, hasMatcher := healthRoute["match"]; !hasMatcher {
		t.Error("routes[0] (health) must have a path matcher")
	}

	// Index 1 — ready probe: has a path matcher.
	readyRoute := routes[1]
	if _, hasMatcher := readyRoute["match"]; !hasMatcher {
		t.Error("routes[1] (ready) must have a path matcher")
	}

	// Index 2 — Kratos flow route: has a path matcher and proxies to Kratos.
	kratosRoute := routes[2]
	if _, hasMatcher := kratosRoute["match"]; !hasMatcher {
		t.Error("routes[2] (Kratos flow) must have a path matcher")
	}

	// Index 3 — catch-all proxy: no path matcher (matches everything).
	catchAll := routes[3]
	if _, hasMatcher := catchAll["match"]; hasMatcher {
		t.Error("routes[3] (catch-all) must not have a path matcher")
	}
}

func TestURLToDialAddr(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "http with explicit port",
			input: "http://127.0.0.1:4433",
			want:  "127.0.0.1:4433",
		},
		{
			name:  "https with explicit port",
			input: "https://kratos.example.com:8443",
			want:  "kratos.example.com:8443",
		},
		{
			name:  "http without port defaults to 80",
			input: "http://kratos.local",
			want:  "kratos.local:80",
		},
		{
			name:  "https without port defaults to 443",
			input: "https://kratos.local",
			want:  "kratos.local:443",
		},
		{
			name:  "url with path is stripped",
			input: "http://127.0.0.1:4433/some/path",
			want:  "127.0.0.1:4433",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := urlToDialAddr(tt.input)
			if got != tt.want {
				t.Errorf("urlToDialAddr(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestBuildCaddyConfig_MetricsRoute_PresentWhenEnabled(t *testing.T) {
	cfg := &ports.ProxyConfig{
		ListenAddr:   "127.0.0.1:8080",
		UpstreamAddr: "127.0.0.1:3000",
		Metrics: ports.MetricsProxyConfig{
			Enabled:      true,
			InternalAddr: "127.0.0.1:9091",
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
	// Expect: health (index 0), ready (index 1), metrics (index 2), catch-all (index 3).
	if len(routes) != 4 {
		t.Fatalf("expected 4 routes (health + ready + metrics + proxy), got %d", len(routes))
	}

	metricsRoute := routes[2]

	matchers, ok := metricsRoute["match"].([]map[string]any)
	if !ok || len(matchers) == 0 {
		t.Fatal("match not found in metrics route")
	}
	paths, ok := matchers[0]["path"].([]string)
	if !ok || len(paths) == 0 {
		t.Fatal("path not found in metrics route matcher")
	}
	if paths[0] != "/_vibewarden/metrics" {
		t.Errorf("metrics route path = %q, want %q", paths[0], "/_vibewarden/metrics")
	}

	handlers, ok := metricsRoute["handle"].([]map[string]any)
	if !ok || len(handlers) < 2 {
		t.Fatalf("expected at least 2 handlers (rewrite + reverse_proxy) in metrics route, got %v", handlers)
	}
	// First handler: rewrite.
	if handlers[0]["handler"] != "rewrite" {
		t.Errorf("metrics handlers[0] = %v, want rewrite", handlers[0]["handler"])
	}
	if handlers[0]["uri"] != "/metrics" {
		t.Errorf("metrics rewrite uri = %v, want /metrics", handlers[0]["uri"])
	}
	// Second handler: reverse_proxy to internal addr.
	if handlers[1]["handler"] != "reverse_proxy" {
		t.Errorf("metrics handlers[1] = %v, want reverse_proxy", handlers[1]["handler"])
	}
	upstreams, ok := handlers[1]["upstreams"].([]map[string]any)
	if !ok || len(upstreams) == 0 {
		t.Fatal("upstreams not found in metrics route handler")
	}
	if upstreams[0]["dial"] != "127.0.0.1:9091" {
		t.Errorf("metrics upstream dial = %v, want 127.0.0.1:9091", upstreams[0]["dial"])
	}
}

func TestBuildCaddyConfig_MetricsRoute_AbsentWhenDisabled(t *testing.T) {
	tests := []struct {
		name string
		cfg  *ports.ProxyConfig
	}{
		{
			name: "metrics disabled",
			cfg: &ports.ProxyConfig{
				ListenAddr:   "127.0.0.1:8080",
				UpstreamAddr: "127.0.0.1:3000",
				Metrics: ports.MetricsProxyConfig{
					Enabled:      false,
					InternalAddr: "127.0.0.1:9091",
				},
			},
		},
		{
			name: "metrics enabled but no internal addr",
			cfg: &ports.ProxyConfig{
				ListenAddr:   "127.0.0.1:8080",
				UpstreamAddr: "127.0.0.1:3000",
				Metrics: ports.MetricsProxyConfig{
					Enabled:      true,
					InternalAddr: "",
				},
			},
		},
		{
			name: "metrics not configured",
			cfg: &ports.ProxyConfig{
				ListenAddr:   "127.0.0.1:8080",
				UpstreamAddr: "127.0.0.1:3000",
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
			if !ok {
				t.Fatal("routes not found in server config")
			}
			// Without metrics: health + ready + catch-all = 3 routes.
			if len(routes) != 3 {
				t.Errorf("expected 3 routes (health + ready + proxy), got %d", len(routes))
			}
		})
	}
}

func TestBuildCaddyConfig_MetricsRouteBeforeCatchAll(t *testing.T) {
	cfg := &ports.ProxyConfig{
		ListenAddr:   "127.0.0.1:8080",
		UpstreamAddr: "127.0.0.1:3000",
		Metrics: ports.MetricsProxyConfig{
			Enabled:      true,
			InternalAddr: "127.0.0.1:9091",
		},
	}

	result, err := BuildCaddyConfig(cfg)
	if err != nil {
		t.Fatalf("BuildCaddyConfig() unexpected error: %v", err)
	}

	server := extractServer(t, result)
	routes, ok := server["routes"].([]map[string]any)
	if !ok || len(routes) < 4 {
		t.Fatalf("expected at least 4 routes, got %d", len(routes))
	}

	// routes[0] = health, routes[1] = ready, routes[2] = metrics, routes[3] = catch-all.
	metricsRoute := routes[2]
	if _, hasMatcher := metricsRoute["match"]; !hasMatcher {
		t.Error("routes[2] (metrics) must have a path matcher")
	}

	catchAll := routes[3]
	if _, hasMatcher := catchAll["match"]; hasMatcher {
		t.Error("routes[3] (catch-all) must not have a path matcher")
	}
}

func TestBuildMetricsRoute(t *testing.T) {
	tests := []struct {
		name         string
		internalAddr string
	}{
		{
			name:         "standard localhost addr",
			internalAddr: "127.0.0.1:9091",
		},
		{
			name:         "high port",
			internalAddr: "127.0.0.1:49152",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			route := buildMetricsRoute(tt.internalAddr)

			matchers, ok := route["match"].([]map[string]any)
			if !ok || len(matchers) == 0 {
				t.Fatal("match not found in route")
			}
			paths, ok := matchers[0]["path"].([]string)
			if !ok || len(paths) != 1 || paths[0] != "/_vibewarden/metrics" {
				t.Errorf("path = %v, want [/_vibewarden/metrics]", paths)
			}

			handlers, ok := route["handle"].([]map[string]any)
			if !ok || len(handlers) < 2 {
				t.Fatalf("expected at least 2 handlers (rewrite + reverse_proxy), got %v", handlers)
			}
			// handlers[0] must be the rewrite handler.
			if handlers[0]["handler"] != "rewrite" {
				t.Errorf("handlers[0] = %v, want rewrite", handlers[0]["handler"])
			}
			if handlers[0]["uri"] != "/metrics" {
				t.Errorf("rewrite uri = %v, want /metrics", handlers[0]["uri"])
			}
			// handlers[1] must be reverse_proxy.
			if handlers[1]["handler"] != "reverse_proxy" {
				t.Errorf("handlers[1] = %v, want reverse_proxy", handlers[1]["handler"])
			}
			upstreams, ok := handlers[1]["upstreams"].([]map[string]any)
			if !ok || len(upstreams) == 0 {
				t.Fatal("upstreams not found")
			}
			if upstreams[0]["dial"] != tt.internalAddr {
				t.Errorf("dial = %v, want %v", upstreams[0]["dial"], tt.internalAddr)
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

func TestBuildCaddyConfig_AdminRoute(t *testing.T) {
	tests := []struct {
		name              string
		cfg               *ports.ProxyConfig
		wantAdminRoute    bool
		wantAdminDialAddr string
	}{
		{
			name: "admin route present when enabled with internal addr",
			cfg: &ports.ProxyConfig{
				ListenAddr:   "127.0.0.1:8080",
				UpstreamAddr: "127.0.0.1:3000",
				Admin: ports.AdminProxyConfig{
					Enabled:      true,
					InternalAddr: "127.0.0.1:9092",
				},
			},
			wantAdminRoute:    true,
			wantAdminDialAddr: "127.0.0.1:9092",
		},
		{
			name: "admin route absent when disabled",
			cfg: &ports.ProxyConfig{
				ListenAddr:   "127.0.0.1:8080",
				UpstreamAddr: "127.0.0.1:3000",
				Admin: ports.AdminProxyConfig{
					Enabled: false,
				},
			},
			wantAdminRoute: false,
		},
		{
			name: "admin route absent when enabled but internal addr is empty",
			cfg: &ports.ProxyConfig{
				ListenAddr:   "127.0.0.1:8080",
				UpstreamAddr: "127.0.0.1:3000",
				Admin: ports.AdminProxyConfig{
					Enabled:      true,
					InternalAddr: "",
				},
			},
			wantAdminRoute: false,
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

			var adminRoute map[string]any
			for _, route := range routes {
				match, ok := route["match"].([]map[string]any)
				if !ok || len(match) == 0 {
					continue
				}
				paths, ok := match[0]["path"].([]string)
				if !ok || len(paths) == 0 {
					continue
				}
				if paths[0] == "/_vibewarden/admin/*" {
					adminRoute = route
					break
				}
			}

			if tt.wantAdminRoute && adminRoute == nil {
				t.Fatal("expected admin route to be present but not found")
			}
			if !tt.wantAdminRoute && adminRoute != nil {
				t.Fatal("expected no admin route but found one")
			}

			if !tt.wantAdminRoute {
				return
			}

			handles, ok := adminRoute["handle"].([]map[string]any)
			if !ok || len(handles) == 0 {
				t.Fatal("handle not found in admin route")
			}

			var rpHandler map[string]any
			for _, h := range handles {
				if h["handler"] == "reverse_proxy" {
					rpHandler = h
					break
				}
			}
			if rpHandler == nil {
				t.Fatal("reverse_proxy handler not found in admin route")
			}

			upstreams, ok := rpHandler["upstreams"].([]map[string]any)
			if !ok || len(upstreams) == 0 {
				t.Fatal("upstreams not found in reverse_proxy handler")
			}
			if upstreams[0]["dial"] != tt.wantAdminDialAddr {
				t.Errorf("admin upstream dial = %v, want %q", upstreams[0]["dial"], tt.wantAdminDialAddr)
			}
		})
	}
}

func TestBuildDocsRoute(t *testing.T) {
	tests := []struct {
		name         string
		internalAddr string
		wantPath     string
		wantDialAddr string
	}{
		{
			name:         "builds correct route for internal addr",
			internalAddr: "127.0.0.1:9092",
			wantPath:     "/_vibewarden/api/docs",
			wantDialAddr: "127.0.0.1:9092",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			route := buildDocsRoute(tt.internalAddr)

			match, ok := route["match"].([]map[string]any)
			if !ok || len(match) == 0 {
				t.Fatal("match not found in docs route")
			}
			paths, ok := match[0]["path"].([]string)
			if !ok || len(paths) == 0 {
				t.Fatal("path not found in docs route match")
			}
			if paths[0] != tt.wantPath {
				t.Errorf("path = %q, want %q", paths[0], tt.wantPath)
			}

			handles, ok := route["handle"].([]map[string]any)
			if !ok || len(handles) == 0 {
				t.Fatal("handle not found in docs route")
			}
			var rpHandler map[string]any
			for _, h := range handles {
				if h["handler"] == "reverse_proxy" {
					rpHandler = h
					break
				}
			}
			if rpHandler == nil {
				t.Fatal("reverse_proxy handler not found in docs route")
			}
			upstreams, ok := rpHandler["upstreams"].([]map[string]any)
			if !ok || len(upstreams) == 0 {
				t.Fatal("upstreams not found in docs route reverse_proxy handler")
			}
			if upstreams[0]["dial"] != tt.wantDialAddr {
				t.Errorf("upstream dial = %v, want %q", upstreams[0]["dial"], tt.wantDialAddr)
			}
		})
	}
}

func TestBuildCaddyConfig_DocsRoute(t *testing.T) {
	tests := []struct {
		name             string
		cfg              *ports.ProxyConfig
		wantDocsRoute    bool
		wantDocsDialAddr string
	}{
		{
			name: "docs route present when admin enabled with internal addr",
			cfg: &ports.ProxyConfig{
				ListenAddr:   "127.0.0.1:8080",
				UpstreamAddr: "127.0.0.1:3000",
				Admin: ports.AdminProxyConfig{
					Enabled:      true,
					InternalAddr: "127.0.0.1:9092",
				},
			},
			wantDocsRoute:    true,
			wantDocsDialAddr: "127.0.0.1:9092",
		},
		{
			name: "docs route absent when admin disabled",
			cfg: &ports.ProxyConfig{
				ListenAddr:   "127.0.0.1:8080",
				UpstreamAddr: "127.0.0.1:3000",
				Admin: ports.AdminProxyConfig{
					Enabled: false,
				},
			},
			wantDocsRoute: false,
		},
		{
			name: "docs route absent when admin enabled but internal addr empty",
			cfg: &ports.ProxyConfig{
				ListenAddr:   "127.0.0.1:8080",
				UpstreamAddr: "127.0.0.1:3000",
				Admin: ports.AdminProxyConfig{
					Enabled:      true,
					InternalAddr: "",
				},
			},
			wantDocsRoute: false,
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

			var docsRoute map[string]any
			for _, route := range routes {
				match, ok := route["match"].([]map[string]any)
				if !ok || len(match) == 0 {
					continue
				}
				paths, ok := match[0]["path"].([]string)
				if !ok || len(paths) == 0 {
					continue
				}
				if paths[0] == "/_vibewarden/api/docs" {
					docsRoute = route
					break
				}
			}

			if tt.wantDocsRoute && docsRoute == nil {
				t.Fatal("expected docs route to be present but not found")
			}
			if !tt.wantDocsRoute && docsRoute != nil {
				t.Fatal("expected no docs route but found one")
			}

			if !tt.wantDocsRoute {
				return
			}

			handles, ok := docsRoute["handle"].([]map[string]any)
			if !ok || len(handles) == 0 {
				t.Fatal("handle not found in docs route")
			}
			var rpHandler map[string]any
			for _, h := range handles {
				if h["handler"] == "reverse_proxy" {
					rpHandler = h
					break
				}
			}
			if rpHandler == nil {
				t.Fatal("reverse_proxy handler not found in docs route")
			}
			upstreams, ok := rpHandler["upstreams"].([]map[string]any)
			if !ok || len(upstreams) == 0 {
				t.Fatal("upstreams not found in reverse_proxy handler")
			}
			if upstreams[0]["dial"] != tt.wantDocsDialAddr {
				t.Errorf("docs upstream dial = %v, want %q", upstreams[0]["dial"], tt.wantDocsDialAddr)
			}
		})
	}
}

func TestBuildCaddyConfig_ServerTimeouts(t *testing.T) {
	tests := []struct {
		name             string
		serverTimeouts   ports.ServerTimeoutsConfig
		wantReadTimeout  int64 // 0 means key must be absent
		wantWriteTimeout int64
		wantIdleTimeout  int64
	}{
		{
			name:             "all timeouts absent when zero",
			serverTimeouts:   ports.ServerTimeoutsConfig{},
			wantReadTimeout:  0,
			wantWriteTimeout: 0,
			wantIdleTimeout:  0,
		},
		{
			name: "default timeouts wired correctly",
			serverTimeouts: ports.ServerTimeoutsConfig{
				ReadTimeout:  30 * time.Second,
				WriteTimeout: 60 * time.Second,
				IdleTimeout:  120 * time.Second,
			},
			wantReadTimeout:  int64(30 * time.Second),
			wantWriteTimeout: int64(60 * time.Second),
			wantIdleTimeout:  int64(120 * time.Second),
		},
		{
			name: "custom timeouts wired correctly",
			serverTimeouts: ports.ServerTimeoutsConfig{
				ReadTimeout:  5 * time.Second,
				WriteTimeout: 10 * time.Second,
				IdleTimeout:  15 * time.Second,
			},
			wantReadTimeout:  int64(5 * time.Second),
			wantWriteTimeout: int64(10 * time.Second),
			wantIdleTimeout:  int64(15 * time.Second),
		},
		{
			name: "only read timeout set",
			serverTimeouts: ports.ServerTimeoutsConfig{
				ReadTimeout: 45 * time.Second,
			},
			wantReadTimeout:  int64(45 * time.Second),
			wantWriteTimeout: 0,
			wantIdleTimeout:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &ports.ProxyConfig{
				ListenAddr:     "127.0.0.1:8080",
				UpstreamAddr:   "127.0.0.1:3000",
				ServerTimeouts: tt.serverTimeouts,
			}
			result, err := BuildCaddyConfig(cfg)
			if err != nil {
				t.Fatalf("BuildCaddyConfig() unexpected error: %v", err)
			}
			server := extractServer(t, result)

			checkTimeout := func(key string, want int64) {
				t.Helper()
				val, present := server[key]
				if want == 0 {
					if present {
						t.Errorf("server[%q] = %v, expected key to be absent when timeout is zero", key, val)
					}
					return
				}
				if !present {
					t.Errorf("server[%q] not found, want %d", key, want)
					return
				}
				got, ok := val.(int64)
				if !ok {
					t.Errorf("server[%q] has type %T, want int64", key, val)
					return
				}
				if got != want {
					t.Errorf("server[%q] = %d, want %d", key, got, want)
				}
			}

			checkTimeout("read_timeout", tt.wantReadTimeout)
			checkTimeout("write_timeout", tt.wantWriteTimeout)
			checkTimeout("idle_timeout", tt.wantIdleTimeout)
		})
	}
}

// ---------------------------------------------------------------------------
// ExtraRoutes and ExtraHandlers
// ---------------------------------------------------------------------------

func TestBuildCaddyConfig_ExtraRoutes_AddedBeforeCatchAll(t *testing.T) {
	cfg := minimalConfig()
	cfg.ExtraRoutes = []ports.CaddyRoute{
		{
			MatchPath: "/_vibewarden/jwks.json",
			Priority:  38,
			Handler: map[string]any{
				"match": []map[string]any{
					{"path": []string{"/_vibewarden/jwks.json"}},
				},
				"handle": []map[string]any{
					{
						"handler": "reverse_proxy",
						"upstreams": []map[string]any{
							{"dial": "127.0.0.1:55000"},
						},
					},
				},
			},
		},
	}

	out, err := BuildCaddyConfig(cfg)
	if err != nil {
		t.Fatalf("BuildCaddyConfig: %v", err)
	}

	routes := extractRoutes(t, out)
	// The JWKS extra route must appear before the catch-all (last route).
	found := false
	for i, r := range routes {
		matchSlice, ok := r["match"].([]map[string]any)
		if !ok {
			continue
		}
		if len(matchSlice) == 0 {
			continue
		}
		paths, ok := matchSlice[0]["path"].([]string)
		if !ok {
			continue
		}
		for _, p := range paths {
			if p == "/_vibewarden/jwks.json" {
				// Must not be the last route.
				if i == len(routes)-1 {
					t.Error("JWKS extra route is the catch-all (last) route; want it before catch-all")
				}
				found = true
			}
		}
	}
	if !found {
		t.Error("JWKS extra route not found in Caddy config routes")
	}
}

func TestBuildCaddyConfig_ExtraHandlers_InCatchAllChain(t *testing.T) {
	cfg := minimalConfig()
	cfg.ExtraHandlers = []ports.CaddyHandler{
		{
			Handler:  map[string]any{"handler": "jwt_bearer", "jwks_url": "http://127.0.0.1:55000/_vibewarden/jwks.json"},
			Priority: 40,
		},
	}

	out, err := BuildCaddyConfig(cfg)
	if err != nil {
		t.Fatalf("BuildCaddyConfig: %v", err)
	}

	routes := extractRoutes(t, out)
	if len(routes) == 0 {
		t.Fatal("no routes")
	}

	// The catch-all route is last.
	catchAll := routes[len(routes)-1]
	handles, ok := catchAll["handle"].([]map[string]any)
	if !ok {
		t.Fatalf("catch-all handle is not []map[string]any: %T", catchAll["handle"])
	}

	found := false
	for _, h := range handles {
		if h["handler"] == "jwt_bearer" {
			found = true
		}
	}
	if !found {
		t.Error("jwt_bearer extra handler not found in catch-all handler chain")
	}
}

func TestBuildCaddyConfig_ExtraHandlers_OrderedByPriority(t *testing.T) {
	cfg := minimalConfig()
	// ExtraHandlers are pre-sorted by buildProxyConfig before reaching BuildCaddyConfig.
	// Pass them already in ascending priority order (as buildProxyConfig guarantees).
	cfg.ExtraHandlers = []ports.CaddyHandler{
		{Handler: map[string]any{"handler": "first", "priority_marker": "a"}, Priority: 41},
		{Handler: map[string]any{"handler": "second", "priority_marker": "b"}, Priority: 42},
	}

	out, err := BuildCaddyConfig(cfg)
	if err != nil {
		t.Fatalf("BuildCaddyConfig: %v", err)
	}

	routes := extractRoutes(t, out)
	if len(routes) == 0 {
		t.Fatal("no routes")
	}

	catchAll := routes[len(routes)-1]
	handles, ok := catchAll["handle"].([]map[string]any)
	if !ok {
		t.Fatalf("catch-all handle is not []map[string]any: %T", catchAll["handle"])
	}

	// Find the "first" and "second" handlers and verify ordering.
	firstIdx, secondIdx := -1, -1
	for i, h := range handles {
		switch h["handler"] {
		case "first":
			firstIdx = i
		case "second":
			secondIdx = i
		}
	}

	if firstIdx == -1 {
		t.Fatal("'first' extra handler not found in chain")
	}
	if secondIdx == -1 {
		t.Fatal("'second' extra handler not found in chain")
	}
	if firstIdx >= secondIdx {
		t.Errorf("'first' (priority 41) at index %d should come before 'second' (priority 42) at index %d",
			firstIdx, secondIdx)
	}
}

// minimalConfig returns a ProxyConfig with just the required fields set.
func minimalConfig() *ports.ProxyConfig {
	return &ports.ProxyConfig{
		ListenAddr:   "127.0.0.1:8080",
		UpstreamAddr: "127.0.0.1:3000",
	}
}

// extractRoutes navigates the Caddy config map and returns the slice of route maps.
func extractRoutes(t *testing.T, cfg map[string]any) []map[string]any {
	t.Helper()

	apps, ok := cfg["apps"].(map[string]any)
	if !ok {
		t.Fatalf("apps is not map[string]any: %T", cfg["apps"])
	}
	httpApp, ok := apps["http"].(map[string]any)
	if !ok {
		t.Fatalf("http is not map[string]any: %T", apps["http"])
	}
	servers, ok := httpApp["servers"].(map[string]any)
	if !ok {
		t.Fatalf("servers is not map[string]any: %T", httpApp["servers"])
	}
	vw, ok := servers["vibewarden"].(map[string]any)
	if !ok {
		t.Fatalf("vibewarden server is not map[string]any: %T", servers["vibewarden"])
	}
	rawRoutes, ok := vw["routes"].([]map[string]any)
	if !ok {
		t.Fatalf("routes is not []map[string]any: %T", vw["routes"])
	}
	return rawRoutes
}
