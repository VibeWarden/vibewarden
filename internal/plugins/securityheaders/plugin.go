package securityheaders

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/vibewarden/vibewarden/internal/ports"
)

// Plugin is the security-headers plugin for VibeWarden.
// It implements ports.Plugin and ports.CaddyContributor.
//
// On every HTTP response the plugin injects the following headers (all
// individually toggleable via Config):
//   - Strict-Transport-Security (only when tlsEnabled is true)
//   - X-Content-Type-Options
//   - X-Frame-Options
//   - Content-Security-Policy
//   - Referrer-Policy
//   - Permissions-Policy
//
// Start and Stop are no-ops; the plugin is fully stateless. Health reports
// whether the plugin is enabled.
type Plugin struct {
	cfg        Config
	tlsEnabled bool
	logger     *slog.Logger
}

// New creates a new security-headers Plugin.
// tlsEnabled must be true for the HSTS header to be included in contributions;
// HSTS must not be sent over plain HTTP.
func New(cfg Config, tlsEnabled bool, logger *slog.Logger) *Plugin {
	return &Plugin{cfg: cfg, tlsEnabled: tlsEnabled, logger: logger}
}

// Name returns the canonical plugin identifier "security-headers".
// This must match the key used under plugins: in vibewarden.yaml.
func (p *Plugin) Name() string { return "security-headers" }

// Priority returns the plugin's initialisation priority.
// Security headers are assigned priority 20 so they run after TLS (10) but
// before other middleware.
func (p *Plugin) Priority() int { return 20 }

// Init validates the plugin configuration. It returns an error if
// FrameOption contains an unsupported value.
// Init must be called before ContributeCaddyHandlers.
func (p *Plugin) Init(_ context.Context) error {
	if !p.cfg.Enabled {
		return nil
	}
	if err := validateConfig(p.cfg); err != nil {
		return fmt.Errorf("security-headers plugin init: %w", err)
	}
	p.logger.Info("security-headers plugin initialised",
		slog.Bool("hsts", p.cfg.HSTSMaxAge > 0),
		slog.Bool("tls_enabled", p.tlsEnabled),
	)
	return nil
}

// Start is a no-op for the security-headers plugin.
// Headers are injected at request time by the Caddy handler contributed via
// ContributeCaddyHandlers; no background goroutine is required.
func (p *Plugin) Start(_ context.Context) error { return nil }

// Stop is a no-op for the security-headers plugin.
func (p *Plugin) Stop(_ context.Context) error { return nil }

// Health returns the current health status of the security-headers plugin.
// The plugin is always healthy; it reports whether it is enabled or disabled.
func (p *Plugin) Health() ports.HealthStatus {
	if !p.cfg.Enabled {
		return ports.HealthStatus{
			Healthy: true,
			Message: "security-headers disabled",
		}
	}
	return ports.HealthStatus{
		Healthy: true,
		Message: "security-headers configured",
	}
}

// ContributeCaddyRoutes returns nil.
// The security-headers plugin does not add named routes; it contributes a
// catch-all handler via ContributeCaddyHandlers.
func (p *Plugin) ContributeCaddyRoutes() []ports.CaddyRoute { return nil }

// ContributeCaddyHandlers returns the Caddy headers handler that injects all
// configured security headers into every response. Returns an empty slice when
// the plugin is disabled.
//
// The returned handler has Priority 20 so it is placed early in the catch-all
// handler chain — after TLS connection policies but before rate-limiting.
func (p *Plugin) ContributeCaddyHandlers() []ports.CaddyHandler {
	if !p.cfg.Enabled {
		return nil
	}
	return []ports.CaddyHandler{
		{
			Handler:  buildHeadersHandler(p.cfg, p.tlsEnabled),
			Priority: 20,
		},
	}
}

// ---------------------------------------------------------------------------
// Internal builders — pure functions, no side effects.
// ---------------------------------------------------------------------------

// validateConfig checks that the security-headers configuration is valid.
func validateConfig(cfg Config) error {
	switch cfg.FrameOption {
	case "DENY", "SAMEORIGIN", "":
		// Valid values.
	default:
		return fmt.Errorf("invalid frame_option %q; valid values: DENY, SAMEORIGIN, \"\" (disabled)", cfg.FrameOption)
	}
	return nil
}

// buildHeadersHandler creates the Caddy headers handler map for security
// headers. tlsEnabled must be true for the HSTS header to be included.
func buildHeadersHandler(cfg Config, tlsEnabled bool) map[string]any {
	headers := map[string][]string{}

	// Strict-Transport-Security — only over HTTPS.
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
