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
	// Middleware order: StripUserHeaders → SecurityHeaders → AdminAuth → BodySize → RateLimit → CircuitBreaker → Retry → Timeout → ReverseProxy
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

	// Add circuit breaker handler before the retry and timeout handlers. The circuit
	// breaker must run first so that open-circuit requests are rejected immediately,
	// before the timeout budget is even started.
	if cfg.Resilience.CircuitBreaker.Enabled {
		cbHandler, err := buildCircuitBreakerHandlerJSON(cfg.Resilience)
		if err != nil {
			return nil, fmt.Errorf("building circuit breaker handler config: %w", err)
		}
		if cbHandler != nil {
			handlers = append(handlers, cbHandler)
		}
	}

	// Add retry handler after the circuit breaker but before the timeout handler.
	// Positioning the retry handler inside the timeout budget ensures that the
	// total time across all attempts is bounded by the timeout middleware.
	if cfg.Resilience.Retry.Enabled {
		retryHandler, err := buildRetryHandlerJSON(cfg.Resilience)
		if err != nil {
			return nil, fmt.Errorf("building retry handler config: %w", err)
		}
		if retryHandler != nil {
			handlers = append(handlers, retryHandler)
		}
	}

	// Add timeout handler wrapping the reverse proxy when a timeout is configured.
	// The timeout handler must be the last middleware before the reverse proxy so
	// that it constrains only the upstream I/O time, not the full middleware chain.
	if cfg.Resilience.Timeout > 0 {
		timeoutHandler, err := buildTimeoutHandlerJSON(cfg.Resilience)
		if err != nil {
			return nil, fmt.Errorf("building timeout handler config: %w", err)
		}
		if timeoutHandler != nil {
			handlers = append(handlers, timeoutHandler)
		}
	}

	// Add reverse proxy as final handler.
	handlers = append(handlers, reverseProxyHandler)

	// Build the health check route (must come before the catch-all proxy route).
	// The static response includes the components field for backward-compatible
	// aggregate health. The upstream status is reported as "unknown" here because
	// this is a Caddy static_response handler; the Go HealthHandler middleware
	// provides dynamic upstream status when used directly.
	healthBody := fmt.Sprintf(`{"status":"ok","version":%q,"components":{"sidecar":"ok","upstream":"unknown"}}`, cfg.Version)
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

	// Build the readiness probe route (must come before the catch-all proxy route).
	// When readiness.enabled is true and an internal address is configured, Caddy
	// reverse-proxies /_vibewarden/ready to the internal Go HTTP server running
	// ReadyHandler for live plugin and upstream checks.
	// Otherwise a static 503 response is returned — the process is alive but not
	// yet confirmed ready.
	var readyRoute map[string]any
	if cfg.Readiness.Enabled && cfg.Readiness.InternalAddr != "" {
		readyRoute = buildDynamicReadyRoute(cfg.Readiness.InternalAddr)
	} else {
		readyRoute = buildStaticReadyRoute()
	}

	// Build routes — health (liveness) first, then readiness probe, then metrics
	// (when enabled), then Kratos flow routes (when auth is configured), and
	// finally the catch-all proxy route.
	routes := []map[string]any{healthRoute, readyRoute}

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
		docsRoute := buildDocsRoute(cfg.Admin.InternalAddr)
		routes = append(routes, docsRoute)
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
