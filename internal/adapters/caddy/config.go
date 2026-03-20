// Package caddy implements the ProxyServer port using embedded Caddy.
package caddy

import (
	"fmt"
	"net"
	"strings"

	"github.com/vibewarden/vibewarden/internal/ports"
)

// BuildCaddyConfig constructs the Caddy JSON configuration from ProxyConfig.
// Uses Caddy's native JSON config format (not Caddyfile).
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

	// Determine if this is a local address (skip TLS for localhost)
	isLocal := isLocalAddress(cfg.UpstreamAddr) || isLocalAddress(cfg.ListenAddr)

	// Build the reverse proxy handler
	reverseProxyHandler := map[string]any{
		"handler": "reverse_proxy",
		"upstreams": []map[string]any{
			{"dial": cfg.UpstreamAddr},
		},
	}

	// Build route handlers (middleware chain + reverse proxy)
	handlers := []map[string]any{}

	// Add security headers handler if enabled.
	// TLS enabled state is passed so that HSTS is only included over HTTPS.
	if cfg.SecurityHeaders.Enabled {
		tlsEnabled := cfg.TLS.Enabled && !isLocal
		handlers = append(handlers, buildSecurityHeadersHandler(cfg.SecurityHeaders, tlsEnabled))
	}

	// Add reverse proxy as final handler
	handlers = append(handlers, reverseProxyHandler)

	// Build the health check route (must come before the catch-all proxy route)
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

	// Build routes — health check first, then catch-all proxy route
	routes := []map[string]any{
		healthRoute,
		{
			"handle": handlers,
		},
	}

	// Build the server configuration
	server := map[string]any{
		"listen": []string{cfg.ListenAddr},
		"routes": routes,
	}

	// Configure TLS if enabled and not local
	if cfg.TLS.Enabled && !isLocal {
		server["tls_connection_policies"] = buildTLSPolicy(cfg.TLS)

		// Enable automatic HTTPS
		server["automatic_https"] = map[string]any{
			"disable": false,
		}
	} else {
		// Disable automatic HTTPS for local development
		server["automatic_https"] = map[string]any{
			"disable": true,
		}
	}

	// Build apps configuration
	apps := map[string]any{
		"http": map[string]any{
			"servers": map[string]any{
				"vibewarden": server,
			},
		},
	}

	// Configure TLS automation if enabled
	if cfg.TLS.Enabled && cfg.TLS.AutoCert && !isLocal {
		apps["tls"] = buildTLSAutomation(cfg.TLS)
	}

	return map[string]any{
		"apps": apps,
	}, nil
}

// buildSecurityHeadersHandler creates the Caddy headers handler for security headers.
// tlsEnabled must be true for the HSTS header to be included; HSTS must not be sent
// over plain HTTP connections.
func buildSecurityHeadersHandler(cfg ports.SecurityHeadersConfig, tlsEnabled bool) map[string]any {
	headers := map[string][]string{}

	// HSTS — only over HTTPS
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

	// X-Content-Type-Options
	if cfg.ContentTypeNosniff {
		headers["X-Content-Type-Options"] = []string{"nosniff"}
	}

	// X-Frame-Options
	if cfg.FrameOption != "" {
		headers["X-Frame-Options"] = []string{cfg.FrameOption}
	}

	// Content-Security-Policy
	if cfg.ContentSecurityPolicy != "" {
		headers["Content-Security-Policy"] = []string{cfg.ContentSecurityPolicy}
	}

	// Referrer-Policy
	if cfg.ReferrerPolicy != "" {
		headers["Referrer-Policy"] = []string{cfg.ReferrerPolicy}
	}

	// Permissions-Policy
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

// buildTLSPolicy creates TLS connection policies for Caddy.
func buildTLSPolicy(_ ports.TLSConfig) []map[string]any {
	return []map[string]any{
		{
			// Default policy — Caddy handles certificate selection automatically
		},
	}
}

// buildTLSAutomation configures automatic certificate management.
func buildTLSAutomation(cfg ports.TLSConfig) map[string]any {
	automation := map[string]any{
		"automation": map[string]any{
			"policies": []map[string]any{
				{
					"subjects": []string{cfg.Domain},
					"issuers": []map[string]any{
						{
							"module": "acme",
						},
					},
				},
			},
		},
	}

	// Configure certificate storage if specified
	if cfg.StoragePath != "" {
		automation["storage"] = map[string]any{
			"module": "file_system",
			"root":   cfg.StoragePath,
		}
	}

	return automation
}

// isLocalAddress checks if the address is localhost or a loopback address.
func isLocalAddress(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
	}

	host = strings.ToLower(host)

	if host == "localhost" || host == "" {
		return true
	}

	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}

	return ip.IsLoopback()
}
