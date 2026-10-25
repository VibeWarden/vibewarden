package caddy

import (
	"fmt"
	"log/slog"
	"time"

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
//	StripUserHeaders → ResponseHeaders → AdminAuth →
//	[ExtraHandlers from plugins, sorted by Priority] →
//	BodySize → RateLimit → CircuitBreaker → Retry → Timeout → Compression → ReverseProxy
//
// The header strip handler is always first so that spoofed X-User-* headers
// sent by clients are removed before any other handler (including auth) runs.
//
// Security headers are NOT part of this chain: they are emitted as a global
// first route by buildSecurityHeadersRoute so that VibeWarden's own routes
// (login UI, admin API, Kratos proxy) carry them too. See #1540.
func buildCatchAllHandlers(cfg *ports.ProxyConfig) ([]map[string]any, error) {
	reverseProxyHandler := map[string]any{
		"handler": "reverse_proxy",
		"upstreams": []map[string]any{
			{"dial": cfg.UpstreamAddr},
		},
	}

	handlers := []map[string]any{buildUserHeaderStripHandler()}

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

// buildHealthRoute builds the Caddy route for /_vibewarden/health. The route
// uses the vibewarden_health custom module, which reads the cached upstream
// probe state from RuntimeServices and applies the worst-component-wins
// aggregation at request time. The version and probe state are both sourced
// from RuntimeServices — no baked-in values.
func buildHealthRoute() map[string]any {
	return map[string]any{
		"match": []map[string]any{
			{"path": []string{"/_vibewarden/health"}},
		},
		"handle": []map[string]any{
			{"handler": "vibewarden_health"},
		},
	}
}

// buildRoutes assembles the full ordered route list:
// security-headers → health → ready → metrics → kratos-flow → me → admin →
// config → docs → extra routes → catch-all.
//
// The security-headers route is unmatched and non-terminal: it sets the
// configured response headers and falls through to the routes below, so every
// response the sidecar serves carries the same set — its own login UI, the
// admin API, the Kratos proxy surface, and the upstream app alike (#1540).
//
// When cfg.Admin.Enabled is true the admin route AND the config route are both
// emitted with the vibewarden_admin_auth handler inlined before reverse_proxy
// so the gate runs in-path for every /_vibewarden/admin/* and
// /_vibewarden/config(/*) request. The public docs route is emitted separately
// with no auth handler.
func buildRoutes(cfg *ports.ProxyConfig, handlers []map[string]any) ([]map[string]any, error) {
	var routes []map[string]any

	if cfg.SecurityHeaders.Enabled {
		routes = append(routes, buildSecurityHeadersRoute(cfg.SecurityHeaders, cfg.TLS.Enabled))
	}

	healthRoute := buildHealthRoute()

	var readyRoute map[string]any
	if cfg.Readiness.Enabled && cfg.Readiness.InternalAddr != "" {
		readyRoute = buildDynamicReadyRoute(cfg.Readiness.InternalAddr)
	} else {
		readyRoute = buildStaticReadyRoute()
	}

	routes = append(routes, healthRoute, readyRoute)

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
		adminRoute, err := buildAdminRoute(cfg.Admin.InternalAddr, cfg.AdminAuth)
		if err != nil {
			return nil, fmt.Errorf("building admin route: %w", err)
		}
		routes = append(routes, adminRoute)

		configRoute, err := buildConfigRoute(cfg.Admin.InternalAddr, cfg.AdminAuth)
		if err != nil {
			return nil, fmt.Errorf("building config route: %w", err)
		}
		routes = append(routes, configRoute)

		routes = append(routes, buildDocsRoute(cfg.Admin.InternalAddr))
	}

	for _, r := range cfg.ExtraRoutes {
		routes = append(routes, buildExtraRoute(r))
	}

	routes = append(routes, buildCatchAllRoute(cfg, handlers))
	return routes, nil
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
	applyConnectionLimit(server, cfg.MaxConnections)
	applyMaxHeaderBytes(server, cfg.MaxHeaderBytes)
	applyAutomaticHTTPS(server, cfg.TLS)

	if cfg.TLS.Enabled {
		server["tls_connection_policies"] = buildTLSPolicy(cfg.TLS)
	}
	return server
}

// applyServerTimeouts sets the Caddy server timeouts. A positive duration is
// emitted as nanoseconds. A negative one (ports.TimeoutDisabled) is emitted as
// -1: Caddy fills in its own default for a missing or zero idle_timeout (5m),
// read_header_timeout, read_idle_timeout and write_idle_timeout (1m each), so
// omitting the key would not turn the timeout off. Zero omits the key and
// leaves that default in place.
func applyServerTimeouts(server map[string]any, timeouts ports.ServerTimeoutsConfig) {
	for key, d := range map[string]time.Duration{
		"read_timeout":        timeouts.ReadTimeout,
		"write_timeout":       timeouts.WriteTimeout,
		"idle_timeout":        timeouts.IdleTimeout,
		"read_header_timeout": timeouts.ReadHeaderTimeout,
		"read_idle_timeout":   timeouts.ReadIdleTimeout,
		"write_idle_timeout":  timeouts.WriteIdleTimeout,
	} {
		switch {
		case d > 0:
			server[key] = int64(d)
		case d < 0:
			server[key] = int64(-1)
		}
	}
}

// applyMaxHeaderBytes caps the request header block. Zero or less omits the
// key and leaves the embedded Caddy's 16 KiB default.
func applyMaxHeaderBytes(server map[string]any, maxBytes int) {
	if maxBytes <= 0 {
		return
	}
	server["max_header_bytes"] = maxBytes
}

// applyConnectionLimit adds the vibewarden_conn_limit listener wrapper to the
// server map, capping the number of concurrent connections on that listener.
// A max of 0 or less omits the wrapper entirely (unlimited).
//
// The trailing {"wrapper": "tls"} entry is load-bearing: it marks where TLS
// terminates in the wrapper chain, so the connection cap runs on the raw TCP
// listener. Without it Caddy prepends the TLS placeholder itself
// (modules/caddyhttp/app.go), silently moving every wrapper after the
// handshake. The placeholder's WrapListener is a no-op, so the entry is
// harmless when TLS is off. See ADR-110.
func applyConnectionLimit(server map[string]any, max int) {
	if max <= 0 {
		return
	}
	server["listener_wrappers"] = []map[string]any{
		{"wrapper": "vibewarden_conn_limit", "max_connections": max},
		{"wrapper": "tls"},
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
		httpServers["vibewarden_redirect"] = buildHTTPRedirectServer(cfg.MaxConnections)
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
