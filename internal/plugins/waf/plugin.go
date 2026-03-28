package waf

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/vibewarden/vibewarden/internal/ports"
)

// Plugin is the WAF (Web Application Firewall) plugin for VibeWarden.
// It implements ports.Plugin and ports.CaddyContributor.
//
// In v1 the only WAF feature is Content-Type validation: body-bearing requests
// (POST, PUT, PATCH) that omit or supply a disallowed Content-Type header are
// rejected with 415 Unsupported Media Type before they reach the upstream app.
//
// Start and Stop are no-ops; the plugin is fully stateless. Health reports
// whether each feature is enabled.
type Plugin struct {
	cfg    Config
	logger *slog.Logger
}

// New creates a new WAF Plugin.
func New(cfg Config, logger *slog.Logger) *Plugin {
	return &Plugin{cfg: cfg, logger: logger}
}

// Name returns the canonical plugin identifier "waf".
func (p *Plugin) Name() string { return "waf" }

// Priority returns 25 — WAF runs after security headers (20) but before
// admin auth (30), rate limiting (50), and auth (40) middleware.
func (p *Plugin) Priority() int { return 25 }

// Init validates the plugin configuration. It is a no-op when all features
// are disabled.
func (p *Plugin) Init(_ context.Context) error {
	if !p.cfg.ContentTypeValidation.Enabled {
		return nil
	}
	if len(p.cfg.ContentTypeValidation.Allowed) == 0 {
		return fmt.Errorf("waf: content_type_validation.allowed must contain at least one media type when enabled")
	}
	p.logger.Info("waf plugin initialised",
		slog.Bool("content_type_validation", p.cfg.ContentTypeValidation.Enabled),
		slog.Int("allowed_content_types", len(p.cfg.ContentTypeValidation.Allowed)),
	)
	return nil
}

// Start is a no-op for the WAF plugin.
func (p *Plugin) Start(_ context.Context) error { return nil }

// Stop is a no-op for the WAF plugin.
func (p *Plugin) Stop(_ context.Context) error { return nil }

// Health returns the current health status of the WAF plugin.
func (p *Plugin) Health() ports.HealthStatus {
	if !p.cfg.ContentTypeValidation.Enabled {
		return ports.HealthStatus{
			Healthy: true,
			Message: "waf disabled",
		}
	}
	return ports.HealthStatus{
		Healthy: true,
		Message: fmt.Sprintf(
			"waf active (content_type_validation: %d allowed types)",
			len(p.cfg.ContentTypeValidation.Allowed),
		),
	}
}

// ContributeCaddyRoutes returns nil. The WAF plugin does not add named routes.
func (p *Plugin) ContributeCaddyRoutes() []ports.CaddyRoute { return nil }

// ContributeCaddyHandlers returns the vibewarden_waf_content_type Caddy handler
// fragment that enforces Content-Type validation on every request.
// Returns an empty slice when the plugin is disabled.
//
// The returned handler has Priority 25 so it is placed after security
// headers (20) but before admin auth (30) in the catch-all handler chain.
func (p *Plugin) ContributeCaddyHandlers() []ports.CaddyHandler {
	if !p.cfg.ContentTypeValidation.Enabled {
		return nil
	}

	handler, err := buildContentTypeHandlerJSON(p.cfg.ContentTypeValidation)
	if err != nil {
		// buildContentTypeHandlerJSON only fails on JSON marshal of a known struct;
		// this cannot happen in practice. Log and return nothing to avoid panicking.
		p.logger.Error("waf plugin: building handler JSON", slog.String("err", err.Error()))
		return nil
	}

	return []ports.CaddyHandler{
		{
			Handler:  handler,
			Priority: 25,
		},
	}
}

// ---------------------------------------------------------------------------
// Internal builders — pure functions, no side effects.
// ---------------------------------------------------------------------------

// contentTypeHandlerConfig is the JSON-serialisable configuration sent to the
// vibewarden_waf_content_type Caddy module.
type contentTypeHandlerConfig struct {
	// Allowed is the list of permitted media types.
	Allowed []string `json:"allowed"`
}

// buildContentTypeHandlerJSON serialises cfg into the Caddy handler JSON map
// expected by the vibewarden_waf_content_type module.
func buildContentTypeHandlerJSON(cfg ContentTypeValidationConfig) (map[string]any, error) {
	hcfg := contentTypeHandlerConfig{
		Allowed: cfg.Allowed,
	}

	cfgBytes, err := json.Marshal(hcfg)
	if err != nil {
		return nil, fmt.Errorf("marshaling content type handler config: %w", err)
	}

	return map[string]any{
		"handler": "vibewarden_waf_content_type",
		"config":  json.RawMessage(cfgBytes),
	}, nil
}

// ---------------------------------------------------------------------------
// Interface guards.
// ---------------------------------------------------------------------------

var (
	_ ports.Plugin           = (*Plugin)(nil)
	_ ports.CaddyContributor = (*Plugin)(nil)
)
