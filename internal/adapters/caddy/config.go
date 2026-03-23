// Package caddy implements the ProxyServer port using embedded Caddy.
package caddy

import (
	"fmt"

	"github.com/vibewarden/vibewarden/internal/ports"
)

// BuildCaddyConfig constructs the Caddy JSON configuration from ProxyConfig.
// Uses Caddy's native JSON config format (not Caddyfile).
//
// TLS behaviour is driven entirely by cfg.TLS.Provider:
//   - "letsencrypt" — automatic ACME certificate via Let's Encrypt
//   - "self-signed"  — Caddy generates an internal self-signed certificate
//   - "external"     — operator-supplied certificate and key files
//
// When TLS is enabled an HTTP-to-HTTPS redirect server is added automatically.
func BuildCaddyConfig(cfg *ports.ProxyConfig) (map[string]any, error) {
	if cfg == nil {
		return nil, fmt.Errorf("proxy config is required")
	}
	if cfg.ListenAddr == "" {
		return nil, fmt.Errorf("listen address is required")
	}
	if cfg.UpstreamAddr == "" {
		return nil, fmt.Errorf("upstream address is required")
	}

	if cfg.TLS.Enabled {
		if err := validateTLSConfig(cfg.TLS); err != nil {
			return nil, fmt.Errorf("tls config: %w", err)
		}
	}

	// Build the reverse proxy handler.
	reverseProxyHandler := map[string]any{
		"handler": "reverse_proxy",
		"upstreams": []map[string]any{
			{"dial": cfg.UpstreamAddr},
		},
	}

	// Build route handlers (middleware chain + reverse proxy).
	handlers := []map[string]any{}

	// Add security headers handler if enabled.
	if cfg.SecurityHeaders.Enabled {
		handlers = append(handlers, buildSecurityHeadersHandler(cfg.SecurityHeaders, cfg.TLS.Enabled))
	}

	// Add reverse proxy as final handler.
	handlers = append(handlers, reverseProxyHandler)

	// Build the health check route (must come before the catch-all proxy route).
	healthBody := fmt.Sprintf(`{"status":"ok","version":%q}`, cfg.Version)
	healthRoute := map[string]any{
		"match": []map[string]any{
			{"path": []string{"/_vibewarden/health"}},
		},
		"handle": []map[string]any{
			{
				"handler": "static_response",
				"headers": map[string][]string{
					"Content-Type": {"application/json"},
				},
				"body":        healthBody,
				"status_code": 200,
			},
		},
	}

	// Build routes — health check first, then catch-all proxy route.
	routes := []map[string]any{
		healthRoute,
		{
			"handle": handlers,
		},
	}

	// Build the main HTTPS (or plain HTTP) server configuration.
	// Caddy's built-in automatic HTTPS negotiation is always disabled here;
	// we control TLS completely through the explicit provider-based configuration.
	server := map[string]any{
		"listen":         []string{cfg.ListenAddr},
		"routes":         routes,
		"automatic_https": map[string]any{"disable": true},
	}

	// Configure TLS connection policies when TLS is enabled.
	if cfg.TLS.Enabled {
		server["tls_connection_policies"] = buildTLSPolicy(cfg.TLS)
	}

	// Start building the apps map.
	apps := map[string]any{}

	// HTTP servers map — may include the redirect server when TLS is on.
	httpServers := map[string]any{
		"vibewarden": server,
	}

	// When TLS is enabled, add an HTTP→HTTPS redirect server on :80.
	if cfg.TLS.Enabled {
		httpServers["vibewarden_redirect"] = buildHTTPRedirectServer()
	}

	apps["http"] = map[string]any{
		"servers": httpServers,
	}

	// Configure the Caddy TLS app section based on provider.
	if cfg.TLS.Enabled {
		tlsApp, err := buildTLSApp(cfg.TLS)
		if err != nil {
			return nil, fmt.Errorf("building tls app config: %w", err)
		}
		if tlsApp != nil {
			apps["tls"] = tlsApp
		}
	}

	return map[string]any{
		"apps": apps,
	}, nil
}

// validateTLSConfig checks that the TLS configuration is self-consistent.
func validateTLSConfig(cfg ports.TLSConfig) error {
	switch cfg.Provider {
	case ports.TLSProviderLetsEncrypt:
		if cfg.Domain == "" {
			return fmt.Errorf("domain is required for provider %q", cfg.Provider)
		}
	case ports.TLSProviderExternal:
		if cfg.CertPath == "" {
			return fmt.Errorf("cert_path is required for provider %q", cfg.Provider)
		}
		if cfg.KeyPath == "" {
			return fmt.Errorf("key_path is required for provider %q", cfg.Provider)
		}
	case ports.TLSProviderSelfSigned, "":
		// No additional fields required.
	default:
		return fmt.Errorf("unknown tls provider %q; valid values: letsencrypt, self-signed, external", cfg.Provider)
	}
	return nil
}

// buildTLSPolicy creates TLS connection policies for Caddy.
// For the external provider the policy references the operator-supplied certificate by tag.
// For all other providers an empty default policy lets Caddy select the certificate.
func buildTLSPolicy(cfg ports.TLSConfig) []map[string]any {
	if cfg.Provider == ports.TLSProviderExternal {
		return []map[string]any{
			{
				"certificate_selection": map[string]any{
					"any_tag": []string{"vibewarden_external"},
				},
			},
		}
	}

	// Default policy — Caddy selects the certificate automatically.
	return []map[string]any{{}}
}

// buildTLSApp builds the Caddy "tls" app configuration for the chosen provider.
// Returns nil when no TLS app section is needed.
func buildTLSApp(cfg ports.TLSConfig) (map[string]any, error) {
	switch cfg.Provider {
	case ports.TLSProviderLetsEncrypt:
		return buildLetsEncryptTLSApp(cfg), nil
	case ports.TLSProviderSelfSigned, "":
		return buildSelfSignedTLSApp(cfg), nil
	case ports.TLSProviderExternal:
		return buildExternalTLSApp(cfg), nil
	default:
		// Already validated in validateTLSConfig; this is a defensive fallback.
		return nil, fmt.Errorf("unknown tls provider: %q", cfg.Provider)
	}
}

// buildLetsEncryptTLSApp returns a Caddy TLS app configuration that provisions
// certificates automatically via ACME (Let's Encrypt).
func buildLetsEncryptTLSApp(cfg ports.TLSConfig) map[string]any {
	policy := map[string]any{
		"subjects": []string{cfg.Domain},
		"issuers": []map[string]any{
			{"module": "acme"},
		},
	}

	tlsApp := map[string]any{
		"automation": map[string]any{
			"policies": []map[string]any{policy},
		},
	}

	if cfg.StoragePath != "" {
		tlsApp["storage"] = map[string]any{
			"module": "file_system",
			"root":   cfg.StoragePath,
		}
	}

	return tlsApp
}

// buildSelfSignedTLSApp returns a Caddy TLS app configuration that instructs
// Caddy to generate an internal self-signed certificate.
// This is intended for local development and testing only.
func buildSelfSignedTLSApp(cfg ports.TLSConfig) map[string]any {
	policy := map[string]any{
		"issuers": []map[string]any{
			{"module": "internal"},
		},
	}

	// Scope the policy to the domain when one is provided.
	if cfg.Domain != "" {
		policy["subjects"] = []string{cfg.Domain}
	}

	tlsApp := map[string]any{
		"automation": map[string]any{
			"policies": []map[string]any{policy},
		},
	}

	if cfg.StoragePath != "" {
		tlsApp["storage"] = map[string]any{
			"module": "file_system",
			"root":   cfg.StoragePath,
		}
	}

	return tlsApp
}

// buildExternalTLSApp returns a Caddy TLS app configuration that loads
// certificates from the file paths supplied by the operator.
func buildExternalTLSApp(cfg ports.TLSConfig) map[string]any {
	return map[string]any{
		"certificates": map[string]any{
			"load_files": []map[string]any{
				{
					"certificate": cfg.CertPath,
					"key":         cfg.KeyPath,
					"tags":        []string{"vibewarden_external"},
				},
			},
		},
	}
}

// buildHTTPRedirectServer returns a Caddy server configuration that permanently
// (HTTP 301) redirects all plain HTTP requests to HTTPS.
func buildHTTPRedirectServer() map[string]any {
	return map[string]any{
		"listen": []string{":80"},
		"routes": []map[string]any{
			{
				"handle": []map[string]any{
					{
						"handler": "static_response",
						"headers": map[string][]string{
							"Location": {"{http.request.uri.scheme}s://{http.request.host}{http.request.uri}"},
						},
						"status_code": 301,
					},
				},
			},
		},
		"automatic_https": map[string]any{"disable": true},
	}
}

// buildSecurityHeadersHandler creates the Caddy headers handler for security headers.
// tlsEnabled must be true for the HSTS header to be included; HSTS must not be sent
// over plain HTTP connections.
func buildSecurityHeadersHandler(cfg ports.SecurityHeadersConfig, tlsEnabled bool) map[string]any {
	headers := map[string][]string{}

	// HSTS — only over HTTPS.
	if cfg.HSTSMaxAge > 0 && tlsEnabled {
		hsts := fmt.Sprintf("max-age=%d", cfg.HSTSMaxAge)
		if cfg.HSTSIncludeSubDomains {
			hsts += "; includeSubDomains"
		}
		if cfg.HSTSPreload {
			hsts += "; preload"
		}
		headers["Strict-Transport-Security"] = []string{hsts}
	}

	// X-Content-Type-Options.
	if cfg.ContentTypeNosniff {
		headers["X-Content-Type-Options"] = []string{"nosniff"}
	}

	// X-Frame-Options.
	if cfg.FrameOption != "" {
		headers["X-Frame-Options"] = []string{cfg.FrameOption}
	}

	// Content-Security-Policy.
	if cfg.ContentSecurityPolicy != "" {
		headers["Content-Security-Policy"] = []string{cfg.ContentSecurityPolicy}
	}

	// Referrer-Policy.
	if cfg.ReferrerPolicy != "" {
		headers["Referrer-Policy"] = []string{cfg.ReferrerPolicy}
	}

	// Permissions-Policy.
	if cfg.PermissionsPolicy != "" {
		headers["Permissions-Policy"] = []string{cfg.PermissionsPolicy}
	}

	return map[string]any{
		"handler": "headers",
		"response": map[string]any{
			"set": headers,
		},
	}
}
