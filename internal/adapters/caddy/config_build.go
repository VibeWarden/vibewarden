package caddy

import (
	"fmt"
	"log/slog"

	"github.com/vibewarden/vibewarden/internal/ports"
)

// validateBuildInput checks the minimum required fields of the ProxyConfig.
// Returns an error when cfg is nil, ListenAddr is empty, UpstreamAddr is empty,
// or TLS is enabled with an invalid configuration.
func validateBuildInput(cfg *ports.ProxyConfig) error {
	if cfg == nil {
		return fmt.Errorf("proxy config is required")
	}
	if cfg.ListenAddr == "" {
		return fmt.Errorf("listen address is required")
	}
	if cfg.UpstreamAddr == "" {
		return fmt.Errorf("upstream address is required")
	}
	if cfg.TLS.Enabled {
		if err := validateTLSConfig(cfg.TLS); err != nil {
			return fmt.Errorf("tls config: %w", err)
		}
	}
	return nil
}

// buildCatchAllHandlers assembles the full middleware chain for the catch-all
// proxy route. The chain order is fixed:
//
//	StripUserHeaders → SecurityHeaders → ResponseHeaders → AdminAuth →
//	[ExtraHandlers from plugins, sorted by Priority] →
//	BodySize → RateLimit → CircuitBreaker → Retry → Timeout → Compression → ReverseProxy
//
// The header strip handler is always first so that spoofed X-User-* headers
// sent by clients are removed before any other handler (including auth) runs.
func buildCatchAllHandlers(cfg *ports.ProxyConfig) ([]map[string]any, error) {
	reverseProxyHandler := map[string]any{
		"handler": "reverse_proxy",
		"upstreams": []map[string]any{
			{"dial": cfg.UpstreamAddr},
		},
	}

	handlers := []map[string]any{buildUserHeaderStripHandler()}

	if cfg.SecurityHeaders.Enabled {
		handlers = append(handlers, buildSecurityHeadersHandler(cfg.SecurityHeaders, cfg.TLS.Enabled))
	}

	if cfg.ResponseHeaders.Enabled {
		handlers = append(handlers, buildResponseHeadersHandlerJSON(cfg.ResponseHeaders))
	}

	adminAuthHandler, err := buildAdminAuthHandlerJSON(cfg.AdminAuth)
	if err != nil {
		return nil, fmt.Errorf("building admin auth handler config: %w", err)
	}
	handlers = append(handlers, adminAuthHandler)

	for _, eh := range cfg.ExtraHandlers {
		handlers = append(handlers, eh.Handler)
	}

	if cfg.BodySize.Enabled {
		bsHandler, err := buildBodySizeHandlerJSON(cfg.BodySize)
		if err != nil {
			return nil, fmt.Errorf("building body size handler config: %w", err)
		}
		handlers = append(handlers, bsHandler)
	}

	if cfg.RateLimit.Enabled {
		rlHandler, err := buildRateLimitHandlerJSON(cfg.RateLimit)
		if err != nil {
			return nil, fmt.Errorf("building rate limit handler config: %w", err)
		}
		handlers = append(handlers, rlHandler)
	}

	if cfg.Resilience.CircuitBreaker.Enabled {
		cbHandler, err := buildCircuitBreakerHandlerJSON(cfg.Resilience)
		if err != nil {
			return nil, fmt.Errorf("building circuit breaker handler config: %w", err)
		}
		if cbHandler != nil {
			handlers = append(handlers, cbHandler)
		}
	}

	if cfg.Resilience.Retry.Enabled {
		retryHandler, err := buildRetryHandlerJSON(cfg.Resilience)
		if err != nil {
			return nil, fmt.Errorf("building retry handler config: %w", err)
		}
		if retryHandler != nil {
			handlers = append(handlers, retryHandler)
		}
	}

	if cfg.Resilience.Timeout > 0 {
		timeoutHandler, err := buildTimeoutHandlerJSON(cfg.Resilience)
		if err != nil {
			return nil, fmt.Errorf("building timeout handler config: %w", err)
		}
		if timeoutHandler != nil {
			handlers = append(handlers, timeoutHandler)
		}
	}

	if cfg.Compression.Enabled {
		handlers = append(handlers, buildCompressionHandlerJSON(cfg.Compression))
	}

	handlers = append(handlers, reverseProxyHandler)
	return handlers, nil
}

// buildHealthRoute builds the static health check route that always returns
// {"status":"ok",...} with a 200 status code.
func buildHealthRoute(version string) map[string]any {
	healthBody := fmt.Sprintf(`{"status":"ok","version":%q,"components":{"sidecar":"ok","upstream":"unknown"}}`, version)
	return map[string]any{
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
}

// buildRoutes assembles the full ordered route list:
// health → ready → metrics → kratos-flow → me → admin → docs → extra routes → catch-all.
func buildRoutes(cfg *ports.ProxyConfig, handlers []map[string]any) []map[string]any {
	healthRoute := buildHealthRoute(cfg.Version)

	var readyRoute map[string]any
	if cfg.Readiness.Enabled && cfg.Readiness.InternalAddr != "" {
		readyRoute = buildDynamicReadyRoute(cfg.Readiness.InternalAddr)
	} else {
		readyRoute = buildStaticReadyRoute()
	}

	routes := []map[string]any{healthRoute, readyRoute}

	if cfg.Metrics.Enabled && cfg.Metrics.InternalAddr != "" {
		routes = append(routes, buildMetricsRoute(cfg.Metrics.InternalAddr))
	}

	if cfg.Auth.Enabled && cfg.Auth.KratosPublicURL != "" {
		routes = append(routes, buildKratosFlowRoute(cfg.Auth.KratosPublicURL))

		cookieName := cfg.Auth.SessionCookieName
		if cookieName == "" {
			cookieName = "ory_kratos_session"
		}
		routes = append(routes, buildMeRoute(cfg.Auth.KratosPublicURL, cookieName))
	}

	if cfg.Admin.Enabled && cfg.Admin.InternalAddr != "" {
		routes = append(routes, buildAdminRoute(cfg.Admin.InternalAddr))
		routes = append(routes, buildDocsRoute(cfg.Admin.InternalAddr))
	}

	for _, r := range cfg.ExtraRoutes {
		routes = append(routes, buildExtraRoute(r))
	}

	routes = append(routes, buildCatchAllRoute(cfg, handlers))
	return routes
}

// buildCatchAllRoute constructs the catch-all proxy route. When TLS is enabled
// with an ACME or self-signed provider a host matcher is added so that Caddy's
// auto-HTTPS knows which domain to manage certificates for.
func buildCatchAllRoute(cfg *ports.ProxyConfig, handlers []map[string]any) map[string]any {
	route := map[string]any{
		"handle": handlers,
	}
	if cfg.TLS.Enabled && (cfg.TLS.Provider == ports.TLSProviderSelfSigned || isACMEProvider(cfg.TLS.Provider)) {
		domain := cfg.TLS.Domain
		if domain == "" {
			domain = "localhost"
		}
		route["match"] = []map[string]any{
			{"host": []string{domain}},
		}
	}
	return route
}

// buildMainServer assembles the Caddy server configuration for the main
// (HTTPS or plain HTTP) server: listen address, routes, timeouts,
// automatic_https policy, and TLS connection policies.
func buildMainServer(cfg *ports.ProxyConfig, routes []map[string]any) map[string]any {
	server := map[string]any{
		"listen": []string{cfg.ListenAddr},
		"routes": routes,
	}

	applyServerTimeouts(server, cfg.ServerTimeouts)
	applyAutomaticHTTPS(server, cfg.TLS)

	if cfg.TLS.Enabled {
		server["tls_connection_policies"] = buildTLSPolicy(cfg.TLS)
	}
	return server
}

// applyServerTimeouts sets read_timeout, write_timeout, and idle_timeout on
// the server map when the corresponding ProxyConfig field is non-zero.
func applyServerTimeouts(server map[string]any, timeouts ports.ServerTimeoutsConfig) {
	if timeouts.ReadTimeout > 0 {
		server["read_timeout"] = int64(timeouts.ReadTimeout)
	}
	if timeouts.WriteTimeout > 0 {
		server["write_timeout"] = int64(timeouts.WriteTimeout)
	}
	if timeouts.IdleTimeout > 0 {
		server["idle_timeout"] = int64(timeouts.IdleTimeout)
	}
}

// applyAutomaticHTTPS sets the automatic_https field on the server map
// according to the TLS provider:
//   - ACME providers: no automatic_https field (Caddy handles HTTP→HTTPS redirects natively).
//   - self-signed/external: disable_redirects=true (manual redirect server is added separately).
//   - TLS disabled: disable=true (fully disables automatic HTTPS).
func applyAutomaticHTTPS(server map[string]any, tls ports.TLSConfig) {
	if tls.Enabled {
		if !isACMEProvider(tls.Provider) {
			server["automatic_https"] = map[string]any{
				"disable_redirects": true,
			}
		}
		// ACME providers: do not set automatic_https — Caddy owns port 80.
	} else {
		server["automatic_https"] = map[string]any{"disable": true}
	}
}

// buildCaddyApps builds the top-level "apps" map including the HTTP servers
// map (with optional redirect server) and the TLS app (when TLS is enabled).
// It also emits the tls-applied log event.
func buildCaddyApps(cfg *ports.ProxyConfig, server map[string]any) (map[string]any, error) {
	httpServers := map[string]any{
		"vibewarden": server,
	}

	if cfg.TLS.Enabled && !isACMEProvider(cfg.TLS.Provider) {
		httpServers["vibewarden_redirect"] = buildHTTPRedirectServer()
	}

	apps := map[string]any{
		"http": map[string]any{
			"servers": httpServers,
		},
	}

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

	return apps, nil
}

// assembleTopLevelConfig constructs the final Caddy top-level configuration map
// from the pre-built apps map. When cfg.TLS.StoragePath is non-empty the
// file_system storage backend is configured at the top level (not inside apps.tls).
func assembleTopLevelConfig(cfg *ports.ProxyConfig, apps map[string]any) (map[string]any, error) {
	caddyCfg := map[string]any{
		"apps": apps,
	}

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
