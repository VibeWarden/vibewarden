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
// The WAF plugin bundles two independent features:
//   - Content-Type validation: body-bearing requests (POST, PUT, PATCH) that omit
//     or supply a disallowed Content-Type are rejected with 415 Unsupported Media Type.
//   - Rule-based engine: scans URL parameters, selected headers, and the first 8 KB
//     of request bodies against built-in attack patterns. Operates in "block" mode
//     (403 Forbidden) or "detect" mode (log only, pass through).
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
	if p.cfg.ContentTypeValidation.Enabled {
		if len(p.cfg.ContentTypeValidation.Allowed) == 0 {
			return fmt.Errorf("waf: content_type_validation.allowed must contain at least one media type when enabled")
		}
	}

	if p.cfg.Engine.Enabled {
		mode := p.cfg.Engine.Mode
		if mode != ModeBlock && mode != ModeDetect && mode != "" {
			return fmt.Errorf("waf: engine.mode must be %q or %q, got %q", ModeBlock, ModeDetect, mode)
		}
	}

	p.logger.Info("waf plugin initialised",
		slog.Bool("content_type_validation", p.cfg.ContentTypeValidation.Enabled),
		slog.Bool("engine", p.cfg.Engine.Enabled),
		slog.String("engine_mode", string(p.cfg.Engine.Mode)),
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
	if !p.cfg.ContentTypeValidation.Enabled && !p.cfg.Engine.Enabled {
		return ports.HealthStatus{
			Healthy: true,
			Message: "waf disabled",
		}
	}

	features := make([]string, 0, 2)
	if p.cfg.ContentTypeValidation.Enabled {
		features = append(features, fmt.Sprintf("content_type_validation: %d allowed types",
			len(p.cfg.ContentTypeValidation.Allowed)))
	}
	if p.cfg.Engine.Enabled {
		mode := p.cfg.Engine.Mode
		if mode == "" {
			mode = ModeBlock
		}
		features = append(features, fmt.Sprintf("engine: mode=%s", mode))
	}

	msg := "waf active"
	if len(features) > 0 {
		msg = "waf active ("
		for i, f := range features {
			if i > 0 {
				msg += ", "
			}
			msg += f
		}
		msg += ")"
	}
	return ports.HealthStatus{
		Healthy: true,
		Message: msg,
	}
}

// ContributeCaddyRoutes returns nil. The WAF plugin does not add named routes.
func (p *Plugin) ContributeCaddyRoutes() []ports.CaddyRoute { return nil }

// ContributeCaddyHandlers returns the Caddy handler fragments for all enabled
// WAF features. Returns an empty slice when all features are disabled.
//
// Handlers are ordered by feature:
//  1. vibewarden_waf_content_type (if content_type_validation.enabled)
//  2. vibewarden_waf_engine (if engine.enabled)
//
// Both handlers carry priority 25 so they run after security headers (20)
// but before admin auth (30) in the catch-all handler chain.
func (p *Plugin) ContributeCaddyHandlers() []ports.CaddyHandler {
	var handlers []ports.CaddyHandler

	if p.cfg.ContentTypeValidation.Enabled {
		handler, err := buildContentTypeHandlerJSON(p.cfg.ContentTypeValidation)
		if err != nil {
			p.logger.Error("waf plugin: building content type handler JSON",
				slog.String("err", err.Error()))
		} else {
			handlers = append(handlers, ports.CaddyHandler{
				Handler:  handler,
				Priority: 25,
			})
		}
	}

	if p.cfg.Engine.Enabled {
		handler, err := buildEngineHandlerJSON(p.cfg.Engine)
		if err != nil {
			p.logger.Error("waf plugin: building engine handler JSON",
				slog.String("err", err.Error()))
		} else {
			handlers = append(handlers, ports.CaddyHandler{
				Handler:  handler,
				Priority: 25,
			})
		}
	}

	return handlers
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

// engineHandlerConfig is the JSON-serialisable configuration sent to the
// vibewarden_waf_engine Caddy module.
type engineHandlerConfig struct {
	// Mode is "block" or "detect".
	Mode string `json:"mode"`

	// Rules toggles individual categories. Absent keys default to enabled.
	Rules struct {
		SQLInjection  bool `json:"sqli"`
		XSS           bool `json:"xss"`
		PathTraversal bool `json:"path_traversal"`
		CmdInjection  bool `json:"cmd_injection"`
	} `json:"rules"`

	// ExemptPaths is the list of exempt URL path glob patterns.
	ExemptPaths []string `json:"exempt_paths"`
}

// buildEngineHandlerJSON serialises cfg into the Caddy handler JSON map
// expected by the vibewarden_waf_engine module.
func buildEngineHandlerJSON(cfg WAFEngineConfig) (map[string]any, error) {
	mode := string(cfg.Mode)
	if mode == "" {
		mode = string(ModeBlock)
	}

	hcfg := engineHandlerConfig{
		Mode:        mode,
		ExemptPaths: cfg.ExemptPaths,
	}
	hcfg.Rules.SQLInjection = cfg.Rules.SQLInjection
	hcfg.Rules.XSS = cfg.Rules.XSS
	hcfg.Rules.PathTraversal = cfg.Rules.PathTraversal
	hcfg.Rules.CmdInjection = cfg.Rules.CommandInjection

	cfgBytes, err := json.Marshal(hcfg)
	if err != nil {
		return nil, fmt.Errorf("marshaling engine handler config: %w", err)
	}

	return map[string]any{
		"handler": "vibewarden_waf_engine",
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
