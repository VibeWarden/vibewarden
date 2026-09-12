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
// The plugin contributes neither routes nor handlers: the Caddy adapter emits
// the security-headers handler itself, as a global first route built from the
// same configuration (buildSecurityHeadersRoute in
// internal/adapters/caddy/config_routes.go), so that VibeWarden's own routes
// carry the headers too. See ContributeCaddyHandlers and #1540.
//
// What is left here is configuration validation (Init rejects an unsupported
// frame_option) and lifecycle reporting. Start and Stop are no-ops; the plugin
// is fully stateless. Health reports whether the plugin is enabled.
type Plugin struct {
	cfg        Config
	tlsEnabled bool
	logger     *slog.Logger
}

// New creates a new security-headers Plugin.
// tlsEnabled records whether the sidecar terminates TLS; it is reported at
// init time, since HSTS must not be sent over plain HTTP.
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
// Headers are injected at request time by the Caddy adapter's global
// security-headers route; no background goroutine is required.
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
// The security-headers plugin does not add any routes; the Caddy adapter emits
// the global security-headers route from ports.SecurityHeadersConfig.
func (p *Plugin) ContributeCaddyRoutes() []ports.CaddyRoute { return nil }

// ContributeCaddyHandlers returns nil, whether the plugin is enabled or not.
//
// The security headers are emitted by the Caddy adapter as a global first
// route (buildSecurityHeadersRoute), built from the same configuration. A
// second copy contributed here would land in ExtraHandlers, which the adapter
// inserts into the catch-all chain *after* the operator response_headers
// handler, so the plugin copy would run last and silently override operator
// rules on the app route (#1540).
func (p *Plugin) ContributeCaddyHandlers() []ports.CaddyHandler { return nil }

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
