// Package caddy implements the ProxyServer port using embedded Caddy.
package caddy

import (
	"fmt"
	"net"
	"net/url"

	"github.com/vibewarden/vibewarden/internal/ports"
)

// kratosFlowPaths contains the URL path patterns that must be proxied to
// the Kratos public API instead of the upstream application.
// These paths are the Kratos self-service browser flows and the Ory canonical prefix.
var kratosFlowPaths = []string{
	"/self-service/login/*",
	"/self-service/registration/*",
	"/self-service/logout/*",
	"/self-service/settings/*",
	"/self-service/recovery/*",
	"/self-service/verification/*",
	"/.ory/kratos/public/*",
}

// BuildCaddyConfig constructs the Caddy JSON configuration from ProxyConfig.
// Uses Caddy's native JSON config format (not Caddyfile).
//
// TLS behaviour is driven entirely by cfg.TLS.Provider:
//   - "letsencrypt" — automatic ACME certificate via Let's Encrypt
//   - "self-signed"  — Caddy generates an internal self-signed certificate
//   - "external"     — operator-supplied certificate and key files
//
// When TLS is enabled an HTTP-to-HTTPS redirect server is added automatically.
//
// When auth is enabled (cfg.Auth.Enabled && cfg.Auth.KratosPublicURL != ""),
// Kratos self-service flow routes are inserted between the health check route
// and the catch-all proxy route. Requests to those paths are forwarded to the
// Kratos public API transparently so browsers can complete self-service flows.
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
	// Middleware order: StripUserHeaders → SecurityHeaders → AdminAuth → BodySize → RateLimit → ReverseProxy
	//
	// The header strip handler MUST be first so that spoofed X-User-* headers sent
	// by clients are removed before any other handler (including auth) runs.
	// These headers are only valid when VibeWarden itself injects them after
	// successful session validation.
	handlers := []map[string]any{buildUserHeaderStripHandler()}

	// Add security headers handler if enabled.
	if cfg.SecurityHeaders.Enabled {
		handlers = append(handlers, buildSecurityHeadersHandler(cfg.SecurityHeaders, cfg.TLS.Enabled))
	}

	// Add admin auth handler. It is always included so that admin paths return
	// the correct status (404 when disabled, 401 when token is wrong) even
	// when the admin API is not yet enabled.
	adminAuthHandler, err := buildAdminAuthHandlerJSON(cfg.AdminAuth)
	if err != nil {
		return nil, fmt.Errorf("building admin auth handler config: %w", err)
	}
	handlers = append(handlers, adminAuthHandler)

	// Add body size handler if enabled. It must run before the reverse proxy
	// so that oversized request bodies are rejected before any upstream I/O.
	if cfg.BodySize.Enabled {
		bsHandler, err := buildBodySizeHandlerJSON(cfg.BodySize)
		if err != nil {
			return nil, fmt.Errorf("building body size handler config: %w", err)
		}
		handlers = append(handlers, bsHandler)
	}

	// Add rate limit handler if enabled.
	if cfg.RateLimit.Enabled {
		rlHandler, err := buildRateLimitHandlerJSON(cfg.RateLimit)
		if err != nil {
			return nil, fmt.Errorf("building rate limit handler config: %w", err)
		}
		handlers = append(handlers, rlHandler)
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

	// Build routes — health check first, then metrics (when enabled), then
	// Kratos flow routes (when auth is configured), and finally the catch-all
	// proxy route.
	routes := []map[string]any{healthRoute}

	if cfg.Metrics.Enabled && cfg.Metrics.InternalAddr != "" {
		metricsRoute := buildMetricsRoute(cfg.Metrics.InternalAddr)
		routes = append(routes, metricsRoute)
	}

	if cfg.Auth.Enabled && cfg.Auth.KratosPublicURL != "" {
		kratosRoute := buildKratosFlowRoute(cfg.Auth.KratosPublicURL)
		routes = append(routes, kratosRoute)
	}

	if cfg.Admin.Enabled && cfg.Admin.InternalAddr != "" {
		adminRoute := buildAdminRoute(cfg.Admin.InternalAddr)
		routes = append(routes, adminRoute)
	}

	catchAllRoute := map[string]any{
		"handle": handlers,
	}
	// For self-signed TLS, add a host matcher so Caddy's auto-HTTPS knows
	// which domain to issue a certificate for. Without this, Caddy won't
	// generate any server certificate.
	if cfg.TLS.Enabled && cfg.TLS.Provider == ports.TLSProviderSelfSigned {
		domain := cfg.TLS.Domain
		if domain == "" {
			domain = "localhost"
		}
		catchAllRoute["match"] = []map[string]any{
			{"host": []string{domain}},
		}
	}
	routes = append(routes, catchAllRoute)

	// Build the main HTTPS (or plain HTTP) server configuration.
	server := map[string]any{
		"listen": []string{cfg.ListenAddr},
		"routes": routes,
	}

	if cfg.TLS.Enabled {
		// When TLS is enabled, only disable Caddy's automatic HTTP→HTTPS
		// redirects (we add our own redirect server). Certificate management
		// via the TLS automation policies must remain active.
		server["automatic_https"] = map[string]any{
			"disable_redirects": true,
		}
	} else {
		// No TLS — fully disable automatic HTTPS.
		server["automatic_https"] = map[string]any{"disable": true}
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

	// For self-signed, set a default SNI so Caddy can match the certificate
	// even when clients connect by IP (no SNI in the TLS handshake).
	if cfg.Provider == ports.TLSProviderSelfSigned {
		domain := cfg.Domain
		if domain == "" {
			domain = "localhost"
		}
		return []map[string]any{{"default_sni": domain}}
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

	// Scope the policy to the domain. For self-signed certs, Caddy's internal
	// issuer needs at least one subject to generate a certificate. Default to
	// "localhost" when no domain is configured (typical for local development).
	domain := cfg.Domain
	if domain == "" {
		domain = "localhost"
	}
	policy["subjects"] = []string{domain}

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
							"Location": {"https://{http.request.host}{http.request.uri}"},
						},
						"status_code": 301,
					},
				},
			},
		},
		"automatic_https": map[string]any{"disable": true},
	}
}

// buildKratosFlowRoute constructs a Caddy route that transparently proxies all
// Kratos self-service flow paths and the Ory canonical prefix to the Kratos
// public API. This route must be placed after the health check route and before
// the catch-all reverse proxy route so that Kratos paths are never forwarded to
// the upstream application.
//
// The kratosPublicURL must be a valid base URL (e.g. "http://127.0.0.1:4433").
// The host:port portion is extracted and used as the Caddy upstream dial address.
func buildKratosFlowRoute(kratosPublicURL string) map[string]any {
	// Convert the full URL to a host:port dial address for Caddy.
	// Caddy's reverse_proxy handler expects "host:port", not a full URL.
	kratosAddr := urlToDialAddr(kratosPublicURL)

	return map[string]any{
		"match": []map[string]any{
			{"path": kratosFlowPaths},
		},
		"handle": []map[string]any{
			{
				"handler": "reverse_proxy",
				"upstreams": []map[string]any{
					{"dial": kratosAddr},
				},
			},
		},
	}
}

// urlToDialAddr extracts the host:port dial address from a full URL string.
// For example "http://127.0.0.1:4433" becomes "127.0.0.1:4433".
// If the URL has no explicit port the scheme default is used: "80" for http,
// "443" for https. Malformed URLs fall back to returning the original string.
func urlToDialAddr(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}

	host := u.Hostname()
	port := u.Port()

	if port == "" {
		switch u.Scheme {
		case "https":
			port = "443"
		default:
			port = "80"
		}
	}

	return net.JoinHostPort(host, port)
}

// buildAdminRoute constructs a Caddy route that reverse-proxies all requests
// under /_vibewarden/admin/* to the internal admin HTTP server at internalAddr.
// The AdminAuthHandler in the middleware chain has already validated the bearer
// token by the time the request reaches this route, so no additional auth is
// performed here.
//
// The internalAddr must be a host:port string (e.g., "127.0.0.1:9092").
// The full request path is forwarded unchanged; the internal admin server
// handles routes under /_vibewarden/admin/*.
func buildAdminRoute(internalAddr string) map[string]any {
	return map[string]any{
		"match": []map[string]any{
			{"path": []string{"/_vibewarden/admin/*"}},
		},
		"handle": []map[string]any{
			{
				"handler": "reverse_proxy",
				"upstreams": []map[string]any{
					{"dial": internalAddr},
				},
			},
		},
	}
}

// buildMetricsRoute constructs a Caddy route that reverse-proxies requests to
// /_vibewarden/metrics to the internal metrics HTTP server at internalAddr.
// The internal server is started separately (see adapters/metrics.Server) and
// serves the Prometheus handler on a random localhost port.
//
// The internalAddr must be a host:port string (e.g., "127.0.0.1:9091").
//
// A rewrite handler is placed before reverse_proxy to translate the public path
// /_vibewarden/metrics into /metrics, which is the path the internal ServeMux
// listens on.
func buildMetricsRoute(internalAddr string) map[string]any {
	return map[string]any{
		"match": []map[string]any{
			{"path": []string{"/_vibewarden/metrics"}},
		},
		"handle": []map[string]any{
			// Rewrite /_vibewarden/metrics → /metrics before proxying.
			{
				"handler": "rewrite",
				"uri":     "/metrics",
			},
			{
				"handler": "reverse_proxy",
				"upstreams": []map[string]any{
					{"dial": internalAddr},
				},
			},
		},
	}
}

// buildUserHeaderStripHandler creates a Caddy headers handler that deletes the
// X-User-Id, X-User-Email, and X-User-Verified request headers.
//
// This handler must be placed as the very first handler in every route's chain.
// Removing these headers on every inbound request prevents a client from
// impersonating an authenticated user by injecting them directly. VibeWarden
// re-injects them only after a valid session has been verified.
func buildUserHeaderStripHandler() map[string]any {
	return map[string]any{
		"handler": "headers",
		"request": map[string]any{
			"delete": []string{"X-User-Id", "X-User-Email", "X-User-Verified"},
		},
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

	// Cross-Origin-Opener-Policy.
	if cfg.CrossOriginOpenerPolicy != "" {
		headers["Cross-Origin-Opener-Policy"] = []string{cfg.CrossOriginOpenerPolicy}
	}

	// Cross-Origin-Resource-Policy.
	if cfg.CrossOriginResourcePolicy != "" {
		headers["Cross-Origin-Resource-Policy"] = []string{cfg.CrossOriginResourcePolicy}
	}

	// X-Permitted-Cross-Domain-Policies.
	if cfg.PermittedCrossDomainPolicies != "" {
		headers["X-Permitted-Cross-Domain-Policies"] = []string{cfg.PermittedCrossDomainPolicies}
	}

	response := map[string]any{
		"set": headers,
	}

	// Suppress the Via header added by Caddy's reverse proxy to reduce
	// information disclosure about the proxy infrastructure.
	if cfg.SuppressViaHeader {
		response["delete"] = []string{"Via"}
	}

	return map[string]any{
		"handler":  "headers",
		"response": response,
	}
}
