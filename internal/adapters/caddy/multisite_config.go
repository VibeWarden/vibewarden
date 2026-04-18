package caddy

import (
	"fmt"
	"log/slog"
	"sort"

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
// perSiteHandlers maps each site name to the Caddy handlers contributed by
// that site's plugin registry (e.g. WAF, auth, CORS). When non-nil, these
// handlers are inserted into the site's handler chain before the reverse
// proxy, sorted by ascending priority. Pass nil to omit plugin handlers
// (backward-compatible with the previous signature).
//
// TLS automation policies are generated per domain so that each site gets
// its own ACME certificate. A single Caddy server instance handles all
// sites on the same listen address.
func BuildMultiSiteConfig(sites []*site.Site, globalCfg site.GlobalConfig, perSiteHandlers map[string][]ports.CaddyHandler, logger *slog.Logger) (map[string]any, error) {
	if len(sites) == 0 {
		return nil, fmt.Errorf("no sites provided")
	}

	listenAddr := fmt.Sprintf("%s:%d", globalCfg.ListenHost, globalCfg.ListenPort)

	var (
		routes     []map[string]any
		tlsEntries []multiSiteTLSEntry
		skipped    int
	)

	for _, s := range sites {
		if !s.IsHealthy() {
			logger.Warn("skipping unhealthy site",
				slog.String("site", s.Name()),
				slog.String("status", s.Status().String()),
			)
			skipped++
			continue
		}

		cfg := s.Config()
		if cfg == nil {
			logger.Warn("skipping site with nil config",
				slog.String("site", s.Name()),
			)
			skipped++
			continue
		}

		domain := cfg.TLS.Domain
		if domain == "" {
			logger.Warn("skipping site without TLS domain",
				slog.String("site", s.Name()),
			)
			skipped++
			continue
		}

		var extraHandlers []ports.CaddyHandler
		if perSiteHandlers != nil {
			extraHandlers = perSiteHandlers[s.Name()]
		}

		siteRoutes, err := buildSiteRoutes(s, domain, extraHandlers)
		if err != nil {
			logger.Error("skipping site due to route build error",
				slog.String("site", s.Name()),
				slog.String("error", err.Error()),
			)
			skipped++
			continue
		}

		routes = append(routes, siteRoutes...)
		tlsEntries = append(tlsEntries, multiSiteTLSEntry{
			domain:   domain,
			provider: cfg.TLS.Provider,
			certPath: cfg.TLS.CertPath,
			keyPath:  cfg.TLS.KeyPath,
		})
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

	// Build TLS app with per-domain automation policies.
	tlsApp := buildMultiSiteTLSApp(tlsEntries, globalCfg.ACMEEmail)
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
// extraHandlers contains plugin-contributed handlers (e.g. WAF, auth, CORS)
// that are inserted into the handler chain before the reverse proxy, sorted
// by ascending priority. This mirrors the single-site ExtraHandlers mechanism
// in BuildCaddyConfig.
//
// The returned slice contains the site's health check route and catch-all
// proxy route, both scoped to the site's domain via host matchers.
func buildSiteRoutes(s *site.Site, domain string, extraHandlers []ports.CaddyHandler) ([]map[string]any, error) {
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

	// Insert extra handlers contributed by per-site plugins (e.g. WAF, auth,
	// CORS, IP filter). These run after security headers but before rate
	// limiting, matching the single-site handler chain order in BuildCaddyConfig.
	// Handlers are sorted by ascending priority so that lower-priority plugins
	// (e.g. WAF at 25) run before higher-priority ones (e.g. rate limiting at 50).
	if len(extraHandlers) > 0 {
		sorted := make([]ports.CaddyHandler, len(extraHandlers))
		copy(sorted, extraHandlers)
		sort.Slice(sorted, func(i, j int) bool {
			return sorted[i].Priority < sorted[j].Priority
		})
		for _, eh := range sorted {
			handlers = append(handlers, eh.Handler)
		}
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

// multiSiteTLSEntry pairs a domain with its TLS provider so that
// buildMultiSiteTLSApp can generate the correct issuer configuration
// (ACME for letsencrypt, internal for self-signed, load_files for external).
type multiSiteTLSEntry struct {
	domain   string
	provider string
	certPath string // only used when provider is "external"
	keyPath  string // only used when provider is "external"
}

// buildMultiSiteTLSApp constructs the Caddy TLS app with per-domain
// automation policies. Each domain gets its own policy entry. The issuer
// module is chosen based on the site's TLS provider:
//   - "self-signed" (or empty) uses Caddy's internal issuer (self-signed CA).
//   - "letsencrypt" (or "acme") uses the ACME issuer for public certificates.
//
// When acmeEmail is non-empty and the issuer is ACME, it is included in the
// issuer configuration.
func buildMultiSiteTLSApp(entries []multiSiteTLSEntry, acmeEmail string) map[string]any {
	if len(entries) == 0 {
		return nil
	}

	var (
		policies  []map[string]any
		loadFiles []map[string]any
	)
	for _, entry := range entries {
		var issuer map[string]any

		switch entry.provider {
		case string(ports.TLSProviderSelfSigned), "":
			issuer = map[string]any{
				"module": "internal",
			}
		case string(ports.TLSProviderExternal):
			// External provider: operator supplies cert + key files.
			// No issuer — certificates are loaded via the load_files block below.
			// Skip adding an issuer-based policy; add a load_files entry instead.
			if entry.certPath != "" && entry.keyPath != "" {
				loadFiles = append(loadFiles, map[string]any{
					"certificate": entry.certPath,
					"key":         entry.keyPath,
					"tags":        []string{"vibewarden_external_" + entry.domain},
				})
			}
			continue // skip the issuer-based policy for this entry
		default:
			// letsencrypt / acme — use ACME issuer.
			issuer = map[string]any{
				"module": "acme",
			}
			if acmeEmail != "" {
				issuer["email"] = acmeEmail
			}
		}

		policy := map[string]any{
			"subjects": []string{entry.domain},
			"issuers":  []map[string]any{issuer},
		}
		policies = append(policies, policy)
	}

	tlsApp := map[string]any{}

	if len(policies) > 0 {
		tlsApp["automation"] = map[string]any{
			"policies": policies,
		}
	}

	if len(loadFiles) > 0 {
		tlsApp["certificates"] = map[string]any{
			"load_files": loadFiles,
		}
	}

	if len(tlsApp) == 0 {
		return nil
	}
	return tlsApp
}
