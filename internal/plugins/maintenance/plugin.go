package maintenance

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/vibewarden/vibewarden/internal/ports"
)

// Plugin is the maintenance mode plugin for VibeWarden.
// It implements ports.Plugin and ports.CaddyContributor.
//
// When enabled, it injects a vibewarden_maintenance Caddy handler at priority 5
// (before all other middleware) so that all non-internal requests are rejected
// with 503 Service Unavailable before any further processing occurs.
type Plugin struct {
	cfg    Config
	logger *slog.Logger
}

// New creates a new maintenance mode Plugin.
func New(cfg Config, logger *slog.Logger) *Plugin {
	return &Plugin{cfg: cfg, logger: logger}
}

// Name returns the canonical plugin identifier "maintenance".
// This must match the key used under the maintenance section in vibewarden.yaml.
func (p *Plugin) Name() string { return "maintenance" }

// Priority returns 5 — maintenance mode must run before all other middleware
// including IP filter (15) so that the sidecar is immediately quiesced.
func (p *Plugin) Priority() int { return 5 }

// Init validates the configuration and logs the plugin status.
func (p *Plugin) Init(_ context.Context) error {
	if !p.cfg.Enabled {
		return nil
	}
	msg := p.cfg.Message
	if msg == "" {
		msg = "Service is under maintenance"
	}
	p.logger.Info("maintenance mode plugin initialised — all requests will be blocked",
		slog.String("message", msg),
	)
	return nil
}

// Start is a no-op for the maintenance plugin. No background work is needed.
func (p *Plugin) Start(_ context.Context) error { return nil }

// Stop is a no-op for the maintenance plugin. No resources need releasing.
func (p *Plugin) Stop(_ context.Context) error { return nil }

// Health returns the current health status of the maintenance plugin.
func (p *Plugin) Health() ports.HealthStatus {
	if !p.cfg.Enabled {
		return ports.HealthStatus{
			Healthy: true,
			Message: "maintenance mode disabled",
		}
	}
	return ports.HealthStatus{
		Healthy: true,
		Message: "maintenance mode active — traffic is blocked",
	}
}

// ContributeCaddyRoutes returns nil.
// The maintenance plugin does not add named routes.
func (p *Plugin) ContributeCaddyRoutes() []ports.CaddyRoute { return nil }

// ContributeCaddyHandlers returns the vibewarden_maintenance Caddy handler
// fragment at priority 5. Returns an empty slice when the plugin is disabled.
func (p *Plugin) ContributeCaddyHandlers() []ports.CaddyHandler {
	if !p.cfg.Enabled {
		return nil
	}

	handler, err := buildMaintenanceHandlerJSON(p.cfg)
	if err != nil {
		p.logger.Error("maintenance plugin: building handler JSON", slog.String("err", err.Error()))
		return nil
	}

	return []ports.CaddyHandler{
		{
			Handler:  handler,
			Priority: 5,
		},
	}
}

// ---------------------------------------------------------------------------
// Internal builder — pure function, no side effects.
// ---------------------------------------------------------------------------

// maintenanceHandlerConfig is the JSON-serialisable config sent to the
// vibewarden_maintenance Caddy module.
type maintenanceHandlerConfig struct {
	Message string `json:"message"`
}

// buildMaintenanceHandlerJSON serialises cfg into the Caddy handler JSON map
// expected by the vibewarden_maintenance module.
func buildMaintenanceHandlerJSON(cfg Config) (map[string]any, error) {
	msg := cfg.Message
	if msg == "" {
		msg = "Service is under maintenance"
	}

	hcfg := maintenanceHandlerConfig{
		Message: msg,
	}

	cfgBytes, err := json.Marshal(hcfg)
	if err != nil {
		return nil, fmt.Errorf("marshaling maintenance handler config: %w", err)
	}

	return map[string]any{
		"handler": "vibewarden_maintenance",
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
