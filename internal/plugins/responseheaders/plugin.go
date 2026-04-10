package responseheaders

import (
	"context"
	"log/slog"

	"github.com/vibewarden/vibewarden/internal/ports"
)

// Plugin is the response-headers plugin for VibeWarden.
// It implements ports.Plugin and ports.CaddyContributor.
//
// The plugin is active whenever at least one of Set, Add, or Remove is
// non-empty.  There is no explicit Enabled toggle: an empty configuration is
// a no-op.
//
// Caddy's built-in headers handler applies the modifications in the following
// order: delete (Remove), then set (Set), then add (Add).  This matches the
// documented order of operations.
//
// Start and Stop are no-ops; the plugin is fully stateless.
type Plugin struct {
	cfg    Config
	logger *slog.Logger
}

// New creates a new response-headers Plugin with the given configuration.
func New(cfg Config, logger *slog.Logger) *Plugin {
	return &Plugin{cfg: cfg, logger: logger}
}

// Name returns the canonical plugin identifier "response-headers".
// This must match the key used under plugins: in vibewarden.yaml.
func (p *Plugin) Name() string { return "response-headers" }

// Priority returns the plugin's initialisation priority.
// Response-headers is assigned priority 25 so that it runs after the
// security-headers plugin (priority 20), ensuring that user-supplied rules
// can override security headers when explicitly configured to do so.
func (p *Plugin) Priority() int { return 25 }

// Init is a no-op for the response-headers plugin.
// The configuration requires no validation beyond what the YAML parser enforces.
func (p *Plugin) Init(_ context.Context) error {
	if !p.active() {
		return nil
	}
	p.logger.Info("response-headers plugin initialised",
		slog.Int("set", len(p.cfg.Set)),
		slog.Int("add", len(p.cfg.Add)),
		slog.Int("remove", len(p.cfg.Remove)),
	)
	return nil
}

// Start is a no-op for the response-headers plugin.
func (p *Plugin) Start(_ context.Context) error { return nil }

// Stop is a no-op for the response-headers plugin.
func (p *Plugin) Stop(_ context.Context) error { return nil }

// Health returns the current health status of the response-headers plugin.
// The plugin is always healthy; it reports whether it is active or inactive.
func (p *Plugin) Health() ports.HealthStatus {
	if !p.active() {
		return ports.HealthStatus{
			Healthy: true,
			Message: "response-headers inactive (no rules configured)",
		}
	}
	return ports.HealthStatus{
		Healthy: true,
		Message: "response-headers configured",
	}
}

// ContributeCaddyRoutes returns nil.
// The response-headers plugin does not add named routes; it contributes a
// catch-all handler via ContributeCaddyHandlers.
func (p *Plugin) ContributeCaddyRoutes() []ports.CaddyRoute { return nil }

// ContributeCaddyHandlers returns the Caddy headers handler that applies all
// configured response header modifications to every response.
// Returns an empty slice when no rules are configured.
func (p *Plugin) ContributeCaddyHandlers() []ports.CaddyHandler {
	if !p.active() {
		return nil
	}
	return []ports.CaddyHandler{
		{
			Handler:  buildResponseHeadersHandler(p.cfg),
			Priority: 25,
		},
	}
}

// ---------------------------------------------------------------------------
// Internal helpers — pure functions, no side effects.
// ---------------------------------------------------------------------------

// active reports whether the plugin has at least one rule to apply.
func (p *Plugin) active() bool {
	return len(p.cfg.Set) > 0 || len(p.cfg.Add) > 0 || len(p.cfg.Remove) > 0
}

// buildResponseHeadersHandler creates the Caddy headers handler map that
// applies the remove → set → add operations described in cfg.
//
// The "response" key in the Caddy headers handler supports three sub-keys:
//   - "delete": []string of header names to remove.
//   - "set":    map[string][]string of headers to overwrite.
//   - "add":    map[string][]string of headers to append.
func buildResponseHeadersHandler(cfg Config) map[string]any {
	response := map[string]any{}

	if len(cfg.Remove) > 0 {
		response["delete"] = cfg.Remove
	}

	if len(cfg.Set) > 0 {
		set := make(map[string][]string, len(cfg.Set))
		for k, v := range cfg.Set {
			set[k] = []string{v}
		}
		response["set"] = set
	}

	if len(cfg.Add) > 0 {
		add := make(map[string][]string, len(cfg.Add))
		for k, v := range cfg.Add {
			add[k] = []string{v}
		}
		response["add"] = add
	}

	return map[string]any{
		"handler":  "headers",
		"response": response,
	}
}
