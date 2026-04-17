package caddy

import (
	"fmt"
	"log/slog"

	"github.com/vibewarden/vibewarden/internal/domain/site"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// BuildMultiSiteConfig constructs a Caddy JSON configuration that serves
// multiple sites, each with host-matched routes and independent middleware
// chains. Sites in error state or without a TLS domain are skipped — broken
// sites do not prevent healthy ones from serving.
//
// The globalCfg provides sidecar-wide settings (listen address, ACME email).
// Each site's config.Config is converted to a per-site ProxyConfig and then
// to a Caddy route with a host matcher restricting it to that site's domain.
//
// TLS automation policies are generated per domain so that each site gets
// its own ACME certificate. A single Caddy server instance handles all
// sites on the same listen address.
func BuildMultiSiteConfig(sites []*site.Site, globalCfg site.GlobalConfig) (map[string]any, error) {
	if len(sites) == 0 {
		return nil, fmt.Errorf("no sites provided")
	}

	listenAddr := fmt.Sprintf("%s:%d", globalCfg.ListenHost, globalCfg.ListenPort)

	var (
		routes     []map[string]any
		tlsDomains []string
		skipped    int
	)

	for _, s := range sites {
		if !s.IsHealthy() {
			slog.Default().Warn("skipping unhealthy site",
				slog.String("site", s.Name()),
				slog.String("status", s.Status().String()),
			)
			skipped++
			continue
		}

		cfg := s.Config()
		if cfg == nil {
			slog.Default().Warn("skipping site with nil config",
				slog.String("site", s.Name()),
			)
			skipped++
			continue
		}

		domain := cfg.TLS.Domain
		if domain == "" {
			slog.Default().Warn("skipping site without TLS domain",
				slog.String("site", s.Name()),
			)
			skipped++
			continue
		}

		siteRoutes, err := buildSiteRoutes(s, domain)
		if err != nil {
			slog.Default().Error("skipping site due to route build error",
				slog.String("site", s.Name()),
				slog.String("error", err.Error()),
			)
			skipped++
			continue
		}

		routes = append(routes, siteRoutes...)
		tlsDomains = append(tlsDomains, domain)
	}

	if len(routes) == 0 {
		return nil, fmt.Errorf("no healthy sites with valid domains (skipped %d)", skipped)
	}

	// Build the main HTTPS server.
	server := map[string]any{
		"listen": []string{listenAddr},
		"routes": routes,
	}

	httpServers := map[string]any{
		"vibewarden": server,
	}

	apps := map[string]any{
		"http": map[string]any{
			"servers": httpServers,
		},
	}

	// Build TLS app with per-domain ACME policies.
	tlsApp := buildMultiSiteTLSApp(tlsDomains, globalCfg.ACMEEmail)
	if tlsApp != nil {
		apps["tls"] = tlsApp
	}

	caddyCfg := map[string]any{
		"apps": apps,
	}

	return caddyCfg, nil
}

// buildSiteRoutes generates Caddy routes for a single site. Each route is
// host-matched to the site's domain so that requests to different domains
// are routed to different upstreams with independent middleware chains.
//
// The returned slice contains the site's health check route and catch-all
// proxy route, both scoped to the site's domain via host matchers.
func buildSiteRoutes(s *site.Site, domain string) ([]map[string]any, error) {
	cfg := s.Config()

	upstreamAddr := fmt.Sprintf("%s:%d", cfg.Upstream.Host, cfg.Upstream.Port)
	if cfg.Upstream.Host == "" || cfg.Upstream.Port == 0 {
		return nil, fmt.Errorf("site %q has invalid upstream address", s.Name())
	}

	// Build the reverse proxy handler for this site's upstream.
	reverseProxyHandler := map[string]any{
		"handler": "reverse_proxy",
		"upstreams": []map[string]any{
			{"dial": upstreamAddr},
		},
	}

	// Build the middleware chain — same order as single-site mode.
	handlers := []map[string]any{buildUserHeaderStripHandler()}

	if cfg.SecurityHeaders.Enabled {
		// Multi-site always has TLS enabled (domain is required).
		handlers = append(handlers, buildSecurityHeadersHandler(ports.SecurityHeadersConfig{
			Enabled:                      cfg.SecurityHeaders.Enabled,
			HSTSMaxAge:                   cfg.SecurityHeaders.HSTSMaxAge,
			HSTSIncludeSubDomains:        cfg.SecurityHeaders.HSTSIncludeSubDomains,
			HSTSPreload:                  cfg.SecurityHeaders.HSTSPreload,
			ContentTypeNosniff:           cfg.SecurityHeaders.ContentTypeNosniff,
			FrameOption:                  cfg.SecurityHeaders.FrameOption,
			ContentSecurityPolicy:        cfg.SecurityHeaders.ContentSecurityPolicy,
			ReferrerPolicy:               cfg.SecurityHeaders.ReferrerPolicy,
			PermissionsPolicy:            cfg.SecurityHeaders.PermissionsPolicy,
			CrossOriginOpenerPolicy:      cfg.SecurityHeaders.CrossOriginOpenerPolicy,
			CrossOriginResourcePolicy:    cfg.SecurityHeaders.CrossOriginResourcePolicy,
			PermittedCrossDomainPolicies: cfg.SecurityHeaders.PermittedCrossDomainPolicies,
			SuppressViaHeader:            cfg.SecurityHeaders.SuppressViaHeader,
		}, true))
	}

	if cfg.RateLimit.Enabled {
		rlHandler, err := buildRateLimitHandlerJSON(ports.RateLimitConfig{
			Enabled:           cfg.RateLimit.Enabled,
			TrustProxyHeaders: cfg.RateLimit.TrustProxyHeaders,
			ExemptPaths:       cfg.RateLimit.ExemptPaths,
			PerIP: ports.RateLimitRule{
				RequestsPerSecond: cfg.RateLimit.PerIP.RequestsPerSecond,
				Burst:             cfg.RateLimit.PerIP.Burst,
			},
			PerUser: ports.RateLimitRule{
				RequestsPerSecond: cfg.RateLimit.PerUser.RequestsPerSecond,
				Burst:             cfg.RateLimit.PerUser.Burst,
			},
		})
		if err != nil {
			return nil, fmt.Errorf("building rate limit handler for site %q: %w", s.Name(), err)
		}
		handlers = append(handlers, rlHandler)
	}

	if cfg.Compression.Enabled {
		handlers = append(handlers, buildCompressionHandlerJSON(ports.CompressionConfig{
			Enabled:    cfg.Compression.Enabled,
			Algorithms: cfg.Compression.Algorithms,
		}))
	}

	// Add reverse proxy as final handler.
	handlers = append(handlers, reverseProxyHandler)

	// Per-site health check route scoped to the site's domain.
	healthBody := fmt.Sprintf(`{"status":"ok","site":%q,"components":{"sidecar":"ok","upstream":"unknown"}}`, s.Name())
	healthRoute := map[string]any{
		"match": []map[string]any{
			{
				"host": []string{domain},
				"path": []string{"/_vibewarden/health"},
			},
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

	// Catch-all proxy route scoped to the site's domain.
	catchAllRoute := map[string]any{
		"match": []map[string]any{
			{"host": []string{domain}},
		},
		"handle": handlers,
	}

	return []map[string]any{healthRoute, catchAllRoute}, nil
}

// buildMultiSiteTLSApp constructs the Caddy TLS app with per-domain ACME
// automation policies. Each domain gets its own policy entry so Caddy
// obtains separate certificates for each site.
//
// When acmeEmail is non-empty, it is included in the ACME issuer
// configuration for all policies.
func buildMultiSiteTLSApp(domains []string, acmeEmail string) map[string]any {
	if len(domains) == 0 {
		return nil
	}

	var policies []map[string]any
	for _, domain := range domains {
		issuer := map[string]any{
			"module": "acme",
		}
		if acmeEmail != "" {
			issuer["email"] = acmeEmail
		}

		policy := map[string]any{
			"subjects": []string{domain},
			"issuers":  []map[string]any{issuer},
		}
		policies = append(policies, policy)
	}

	return map[string]any{
		"automation": map[string]any{
			"policies": policies,
		},
	}
}
