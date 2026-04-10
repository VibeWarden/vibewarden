package bodysize

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/vibewarden/vibewarden/internal/ports"
)

// Plugin is the request body size limiting plugin for VibeWarden.
// It implements ports.Plugin and ports.CaddyContributor.
//
// ContributeCaddyHandlers returns the vibewarden_body_size Caddy handler fragment
// so the middleware is injected into the catch-all handler chain and enforces
// body size limits with optional per-path overrides.
type Plugin struct {
	cfg    Config
	logger *slog.Logger
}

// New creates a new body size limiting Plugin.
func New(cfg Config, logger *slog.Logger) *Plugin {
	return &Plugin{
		cfg:    cfg,
		logger: logger,
	}
}

// Name returns the canonical plugin identifier "body-size".
// This must match the key used under plugins: in vibewarden.yaml.
func (p *Plugin) Name() string { return "body-size" }

// Priority returns the plugin's initialisation priority.
// Body size limiting is assigned priority 45 so it runs after security headers (20)
// and admin auth (30) but before rate limiting (50) and application-layer middleware.
func (p *Plugin) Priority() int { return 45 }

// Init validates the plugin configuration. It is a no-op when the plugin is disabled.
func (p *Plugin) Init(_ context.Context) error {
	if !p.cfg.Enabled {
		return nil
	}

	overrideCount := len(p.cfg.Overrides)
	p.logger.Info("body-size plugin initialised",
		slog.Int64("max_bytes", p.cfg.MaxBytes),
		slog.Int("override_count", overrideCount),
	)
	return nil
}

// Start is a no-op for the body-size plugin. No background work is needed.
func (p *Plugin) Start(_ context.Context) error { return nil }

// Stop is a no-op for the body-size plugin. No resources need releasing.
func (p *Plugin) Stop(_ context.Context) error { return nil }

// Health returns the current health status of the body-size plugin.
func (p *Plugin) Health() ports.HealthStatus {
	if !p.cfg.Enabled {
		return ports.HealthStatus{
			Healthy: true,
			Message: "body-size limiting disabled",
		}
	}
	return ports.HealthStatus{
		Healthy: true,
		Message: fmt.Sprintf(
			"body-size limiting active (global max: %d bytes, %d path overrides)",
			p.cfg.MaxBytes, len(p.cfg.Overrides),
		),
	}
}

// ContributeCaddyRoutes returns nil.
// The body-size plugin does not add named routes; it contributes a catch-all
// handler via ContributeCaddyHandlers.
func (p *Plugin) ContributeCaddyRoutes() []ports.CaddyRoute { return nil }

// ContributeCaddyHandlers returns the Caddy vibewarden_body_size handler
// fragment that enforces body size limits on every request. Returns an empty
// slice when the plugin is disabled.
//
// The returned handler has Priority 45 so it is placed after security headers (20)
// but before rate limiting (50) in the catch-all handler chain.
func (p *Plugin) ContributeCaddyHandlers() []ports.CaddyHandler {
	if !p.cfg.Enabled {
		return nil
	}
	handler, err := buildBodySizeHandlerJSON(p.cfg)
	if err != nil {
		// buildBodySizeHandlerJSON only fails on JSON marshal of a known struct;
		// this cannot happen in practice. Log and return nothing to avoid panicking
		// in library code.
		p.logger.Error("body-size plugin: building handler JSON", slog.String("err", err.Error()))
		return nil
	}
	return []ports.CaddyHandler{
		{
			Handler:  handler,
			Priority: 45,
		},
	}
}

// ---------------------------------------------------------------------------
// Internal builders — pure functions, no side effects.
// ---------------------------------------------------------------------------

// bodySizeHandlerConfig is the JSON-serialisable configuration sent to the
// vibewarden_body_size Caddy module.
type bodySizeHandlerConfig struct {
	MaxBytes  int64                      `json:"max_bytes"`
	Overrides []bodySizeOverrideJSONItem `json:"overrides,omitempty"`
}

// bodySizeOverrideJSONItem is the JSON shape of a single per-path override.
type bodySizeOverrideJSONItem struct {
	Path     string `json:"path"`
	MaxBytes int64  `json:"max_bytes"`
}

// buildBodySizeHandlerJSON serialises cfg into the Caddy handler JSON map
// expected by the vibewarden_body_size module.
func buildBodySizeHandlerJSON(cfg Config) (map[string]any, error) {
	hcfg := bodySizeHandlerConfig{
		MaxBytes: cfg.MaxBytes,
	}
	for _, ov := range cfg.Overrides {
		hcfg.Overrides = append(hcfg.Overrides, bodySizeOverrideJSONItem(ov))
	}

	cfgBytes, err := json.Marshal(hcfg)
	if err != nil {
		return nil, fmt.Errorf("marshaling body size handler config: %w", err)
	}

	return map[string]any{
		"handler": "vibewarden_body_size",
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
