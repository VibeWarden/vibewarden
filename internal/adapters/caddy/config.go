// Package caddy implements the ProxyServer port using embedded Caddy.
package caddy

import (
	"fmt"
	"log/slog"

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
// When TLS is enabled with self-signed or external provider, an HTTP-to-HTTPS
// redirect server is added automatically. For the letsencrypt provider, no
// manual redirect server is created — Caddy's built-in automatic HTTPS handles
// ACME HTTP-01 challenges and HTTP→HTTPS redirects on port 80 natively.
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
	// Middleware order: StripUserHeaders → SecurityHeaders → ResponseHeaders → AdminAuth →
	// [ExtraHandlers from plugins, sorted by Priority] →
	// BodySize → RateLimit → CircuitBreaker → Retry → Timeout → Compression → ReverseProxy
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

	// Add response header modification handler if any rules are configured.
	// This runs after security headers so that operator rules can override or
	// extend headers set by the security-headers plugin.
	if cfg.ResponseHeaders.Enabled {
		handlers = append(handlers, buildResponseHeadersHandlerJSON(cfg.ResponseHeaders))
	}

	// Add admin auth handler. It is always included so that admin paths return
	// the correct status (404 when disabled, 401 when token is wrong) even
	// when the admin API is not yet enabled.
	adminAuthHandler, err := buildAdminAuthHandlerJSON(cfg.AdminAuth)
	if err != nil {
		return nil, fmt.Errorf("building admin auth handler config: %w", err)
	}
	handlers = append(handlers, adminAuthHandler)

	// Insert extra handlers contributed by plugins (e.g. JWT bearer auth).
	// These run after AdminAuth but before BodySize/RateLimit/proxy so that
	// auth decisions are made before any resource-intensive processing.
	// The slice is pre-sorted by ascending Priority in buildProxyConfig.
	for _, eh := range cfg.ExtraHandlers {
		handlers = append(handlers, eh.Handler)
	}

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

	// Add compression handler before the reverse proxy so that Caddy can
	// compress the upstream response before writing it to the client.
	// The encode handler wraps the downstream response writer; it must appear
	// in the chain before any handler that writes response bytes.
	if cfg.Compression.Enabled {
		handlers = append(handlers, buildCompressionHandlerJSON(cfg.Compression))
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

	// Add extra routes contributed by plugins (e.g. JWKS endpoint, token endpoint).
	// These are already sorted by ascending Priority in buildProxyConfig.
	for _, r := range cfg.ExtraRoutes {
		// Each CaddyRoute.Handler is the raw route object — it contains both
		// the "match" and "handle" keys. Convert it to the Caddy route map.
		routes = append(routes, buildExtraRoute(r))
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

	// Apply server-level timeouts when non-zero. Caddy's Duration JSON type
	// accepts nanosecond integers, matching the representation of time.Duration.
	if cfg.ServerTimeouts.ReadTimeout > 0 {
		server["read_timeout"] = int64(cfg.ServerTimeouts.ReadTimeout)
	}
	if cfg.ServerTimeouts.WriteTimeout > 0 {
		server["write_timeout"] = int64(cfg.ServerTimeouts.WriteTimeout)
	}
	if cfg.ServerTimeouts.IdleTimeout > 0 {
		server["idle_timeout"] = int64(cfg.ServerTimeouts.IdleTimeout)
	}

	if cfg.TLS.Enabled {
		if cfg.TLS.Provider == ports.TLSProviderLetsEncrypt {
			// For Let's Encrypt, do NOT disable redirects. Caddy's built-in
			// automatic HTTPS will:
			//   1. Serve ACME HTTP-01 challenges on port 80
			//   2. Redirect all other HTTP traffic to HTTPS
			// A manual redirect server on port 80 would intercept ACME challenge
			// requests before Caddy's solver can handle them, causing cert issuance
			// to fail. We let Caddy own port 80 entirely.
		} else {
			// For self-signed and external TLS, disable Caddy's automatic
			// HTTP→HTTPS redirects and add our own redirect server instead.
			// These providers do not perform ACME challenges, so port 80 is
			// not needed for certificate management.
			server["automatic_https"] = map[string]any{
				"disable_redirects": true,
			}
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

	// When TLS is enabled and the provider is NOT Let's Encrypt, add a manual
	// HTTP→HTTPS redirect server on :80.
	// For Let's Encrypt, Caddy's automatic HTTPS handles both ACME challenges and
	// HTTP→HTTPS redirects on port 80 natively. Adding a manual redirect server
	// would intercept /.well-known/acme-challenge/* before Caddy's ACME solver,
	// causing certificate issuance to fail.
	if cfg.TLS.Enabled && cfg.TLS.Provider != ports.TLSProviderLetsEncrypt {
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
		slog.Default().Info("tls configuration applied",
			slog.String("provider", string(cfg.TLS.Provider)),
			slog.String("domain", cfg.TLS.Domain),
			slog.Bool("storage_override", cfg.TLS.StoragePath != ""),
		)
	}

	// Build the top-level Caddy config.
	caddyCfg := map[string]any{
		"apps": apps,
	}

	// StoragePath overrides where Caddy persists certificates and other assets.
	// Per Caddy's JSON schema, storage is a top-level field on the Config struct
	// (not inside the tls app). Setting it inside apps.tls causes Caddy to reject
	// the config with "unknown field: storage".
	if cfg.TLS.Enabled && cfg.TLS.StoragePath != "" {
		caddyCfg["storage"] = map[string]any{
			"module": "file_system",
			"root":   cfg.TLS.StoragePath,
		}
		slog.Default().Info("caddy storage path configured",
			slog.String("storage_path", cfg.TLS.StoragePath),
		)
	}

	return caddyCfg, nil
}

// buildExtraRoute converts a ports.CaddyRoute into the raw Caddy route map
// expected by the Caddy JSON configuration. CaddyRoute.Handler already contains
// the full route object (with "match" and "handle" keys) so it is used directly.
func buildExtraRoute(r ports.CaddyRoute) map[string]any {
	return r.Handler
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
//
// The ACME issuer is configured with explicit HTTP-01 challenge support.
// The HTTP-01 challenge listener is bound to the same :80 port as the redirect
// server — Caddy manages both from within the same embedded instance, so no
// port conflict occurs.
//
// Note: storage is intentionally NOT set here. Caddy's storage backend is a
// top-level field on the Config struct; placing it inside apps.tls causes Caddy
// to reject the config with "unknown field: storage". Storage is set at the
// top-level in BuildCaddyConfig when cfg.StoragePath is non-empty.
func buildLetsEncryptTLSApp(cfg ports.TLSConfig) map[string]any {
	policy := map[string]any{
		"subjects": []string{cfg.Domain},
		"issuers": []map[string]any{
			{
				"module": "acme",
				// Configure HTTP-01 challenge explicitly. Caddy's embedded HTTP
				// server on :80 serves the ACME challenge response automatically;
				// specifying the port here makes the intent unambiguous and avoids
				// relying on Caddy's automatic challenge port detection when running
				// behind Docker networking.
				"challenges": map[string]any{
					"http": map[string]any{
						"alternate_port": 80,
					},
				},
			},
		},
	}

	return map[string]any{
		"automation": map[string]any{
			"policies": []map[string]any{policy},
		},
	}
}

// buildSelfSignedTLSApp returns a Caddy TLS app configuration that instructs
// Caddy to generate an internal self-signed certificate.
// This is intended for local development and testing only.
//
// Note: storage is intentionally NOT set here. See buildLetsEncryptTLSApp for
// the explanation of why storage belongs at the top-level Caddy config.
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

	return map[string]any{
		"automation": map[string]any{
			"policies": []map[string]any{policy},
		},
	}
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
