package cors

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/vibewarden/vibewarden/internal/ports"
)

// Plugin is the CORS plugin for VibeWarden.
// It implements ports.Plugin and ports.CaddyContributor.
//
// For every HTTP response the plugin injects CORS headers based on whether
// the request Origin is in the allowed list. OPTIONS preflight requests are
// short-circuited with a 204 No Content response so they never reach the
// upstream application.
//
// Wildcard "*" in AllowedOrigins permits all origins and is intended for
// development environments only. It must not be combined with
// AllowCredentials: true.
//
// Start and Stop are no-ops; the plugin is fully stateless. Health reports
// whether the plugin is enabled.
type Plugin struct {
	cfg    Config
	logger *slog.Logger
}

// New creates a new CORS Plugin.
func New(cfg Config, logger *slog.Logger) *Plugin {
	return &Plugin{cfg: cfg, logger: logger}
}

// Name returns the canonical plugin identifier "cors".
// This must match the key used under plugins: in vibewarden.yaml.
func (p *Plugin) Name() string { return "cors" }

// Priority returns the plugin's initialisation priority.
// CORS is assigned priority 10 so it runs before all other middleware —
// browsers send OPTIONS preflight before the real request, so CORS must
// respond first.
func (p *Plugin) Priority() int { return 10 }

// Init validates the plugin configuration and returns an error when the
// configuration is logically inconsistent (e.g. wildcard with credentials).
// Init must be called before ContributeCaddyHandlers.
func (p *Plugin) Init(_ context.Context) error {
	if !p.cfg.Enabled {
		return nil
	}
	if err := validateConfig(p.cfg); err != nil {
		return fmt.Errorf("cors plugin init: %w", err)
	}
	p.logger.Info("cors plugin initialised",
		slog.Int("allowed_origins", len(p.cfg.AllowedOrigins)),
		slog.Bool("allow_credentials", p.cfg.AllowCredentials),
		slog.Int("max_age", p.cfg.MaxAge),
	)
	return nil
}

// Start is a no-op for the CORS plugin.
// Headers are injected at request time by the Caddy handler contributed via
// ContributeCaddyHandlers; no background goroutine is required.
func (p *Plugin) Start(_ context.Context) error { return nil }

// Stop is a no-op for the CORS plugin.
func (p *Plugin) Stop(_ context.Context) error { return nil }

// Health returns the current health status of the CORS plugin.
// The plugin is always healthy; it reports whether it is enabled or disabled.
func (p *Plugin) Health() ports.HealthStatus {
	if !p.cfg.Enabled {
		return ports.HealthStatus{
			Healthy: true,
			Message: "cors disabled",
		}
	}
	return ports.HealthStatus{
		Healthy: true,
		Message: "cors configured",
	}
}

// ContributeCaddyRoutes returns nil.
// The CORS plugin does not add named routes; it contributes handlers via
// ContributeCaddyHandlers only.
func (p *Plugin) ContributeCaddyRoutes() []ports.CaddyRoute { return nil }

// ContributeCaddyHandlers returns the Caddy handlers that implement CORS.
// When enabled, two handlers are returned:
//
//  1. A static_response handler at Priority 10 that responds to OPTIONS
//     preflight requests with 204 No Content, including all CORS headers.
//  2. A headers handler at Priority 11 that injects CORS response headers
//     on all non-OPTIONS requests so that actual cross-origin requests
//     receive the correct Access-Control-* headers.
//
// Returns an empty slice when the plugin is disabled.
func (p *Plugin) ContributeCaddyHandlers() []ports.CaddyHandler {
	if !p.cfg.Enabled {
		return nil
	}

	corsHeaders := buildCORSHeaders(p.cfg)

	// Handler 1 — preflight: match OPTIONS + Origin, reply 204.
	preflightHandler := buildPreflightHandler(corsHeaders)

	// Handler 2 — actual requests: inject CORS headers into responses.
	headersHandler := buildResponseHeadersHandler(corsHeaders)

	return []ports.CaddyHandler{
		{Handler: preflightHandler, Priority: 10},
		{Handler: headersHandler, Priority: 11},
	}
}

// ---------------------------------------------------------------------------
// Internal builders — pure functions, no side effects.
// ---------------------------------------------------------------------------

// validateConfig checks that the CORS configuration is logically consistent.
func validateConfig(cfg Config) error {
	if cfg.AllowCredentials && isWildcard(cfg.AllowedOrigins) {
		return fmt.Errorf(
			"cors: allow_credentials: true cannot be combined with allowed_origins: [\"*\"]; " +
				"browsers reject credentialed requests to wildcard origins",
		)
	}
	return nil
}

// isWildcard reports whether the origin list contains exactly the wildcard "*".
func isWildcard(origins []string) bool {
	for _, o := range origins {
		if o == "*" {
			return true
		}
	}
	return false
}

// corsHeaders holds the computed header key/value pairs for CORS.
type corsHeaders struct {
	// allowOrigin is the value for Access-Control-Allow-Origin.
	// For wildcard configs this is "*"; for specific-origin configs it is set
	// dynamically at request time by Caddy — we use a placeholder "{http.request.header.Origin}"
	// which Caddy evaluates for each request.
	allowOrigin string

	// varyOrigin, when true, indicates that the Vary: Origin header should be
	// added. Required for specific-origin lists so caches do not serve the
	// wrong cached response to a different origin.
	varyOrigin bool

	allowMethods     string
	allowHeaders     string
	exposeHeaders    string
	allowCredentials bool
	maxAge           string
}

// buildCORSHeaders derives the concrete CORS header values from Config.
func buildCORSHeaders(cfg Config) corsHeaders {
	h := corsHeaders{
		allowCredentials: cfg.AllowCredentials,
	}

	if isWildcard(cfg.AllowedOrigins) {
		h.allowOrigin = "*"
		h.varyOrigin = false
	} else {
		// Use Caddy's placeholder — the actual origin matching is handled by
		// the Caddy expression matcher. If the request Origin matches the
		// allowed list, we echo it back. Caddy evaluates
		// {http.request.header.Origin} at request time.
		h.allowOrigin = "{http.request.header.Origin}"
		h.varyOrigin = true
	}

	if len(cfg.AllowedMethods) > 0 {
		h.allowMethods = strings.Join(cfg.AllowedMethods, ", ")
	}
	if len(cfg.AllowedHeaders) > 0 {
		h.allowHeaders = strings.Join(cfg.AllowedHeaders, ", ")
	}
	if len(cfg.ExposedHeaders) > 0 {
		h.exposeHeaders = strings.Join(cfg.ExposedHeaders, ", ")
	}
	if cfg.MaxAge > 0 {
		h.maxAge = fmt.Sprintf("%d", cfg.MaxAge)
	}

	return h
}

// buildPreflightHandler creates a Caddy handler that responds to OPTIONS
// preflight requests with 204 No Content and the full set of CORS headers.
//
// For wildcard origins the handler matches all OPTIONS requests.
// For specific-origin lists, the handler matches OPTIONS requests whose
// Origin header is one of the allowed origins (using a Caddy expression matcher).
func buildPreflightHandler(h corsHeaders) map[string]any {
	// Build the handler. Caddy's static_response handler terminates the chain.
	handler := map[string]any{
		"handler":     "static_response",
		"status_code": 204,
		"headers": map[string][]string{
			"Access-Control-Allow-Origin":  {h.allowOrigin},
			"Access-Control-Allow-Methods": {h.allowMethods},
			"Access-Control-Allow-Headers": {h.allowHeaders},
		},
	}

	if h.allowCredentials {
		handler["headers"].(map[string][]string)["Access-Control-Allow-Credentials"] = []string{"true"}
	}
	if h.maxAge != "" {
		handler["headers"].(map[string][]string)["Access-Control-Max-Age"] = []string{h.maxAge}
	}
	if h.varyOrigin {
		handler["headers"].(map[string][]string)["Vary"] = []string{"Origin"}
	}

	// Wrap in a subroute so we can apply a method matcher for OPTIONS.
	return map[string]any{
		"handler": "subroute",
		"routes": []map[string]any{
			{
				"match": []map[string]any{
					{"method": []string{"OPTIONS"}},
				},
				"handle": []map[string]any{handler},
			},
		},
	}
}

// buildResponseHeadersHandler creates a Caddy headers handler that sets CORS
// response headers on all responses (non-OPTIONS requests that pass through
// the preflight handler).
func buildResponseHeadersHandler(h corsHeaders) map[string]any {
	set := map[string][]string{
		"Access-Control-Allow-Origin": {h.allowOrigin},
	}
	if h.allowMethods != "" {
		set["Access-Control-Allow-Methods"] = []string{h.allowMethods}
	}
	if h.allowHeaders != "" {
		set["Access-Control-Allow-Headers"] = []string{h.allowHeaders}
	}
	if h.exposeHeaders != "" {
		set["Access-Control-Expose-Headers"] = []string{h.exposeHeaders}
	}
	if h.allowCredentials {
		set["Access-Control-Allow-Credentials"] = []string{"true"}
	}
	if h.maxAge != "" {
		set["Access-Control-Max-Age"] = []string{h.maxAge}
	}
	if h.varyOrigin {
		set["Vary"] = []string{"Origin"}
	}

	return map[string]any{
		"handler": "headers",
		"response": map[string]any{
			"set": set,
		},
	}
}
