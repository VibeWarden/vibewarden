package metrics

import (
	"context"
	"fmt"
	"log/slog"

	metricsadapter "github.com/vibewarden/vibewarden/internal/adapters/metrics"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// Plugin is the metrics plugin for VibeWarden.
// It implements ports.Plugin, ports.CaddyContributor, and ports.InternalServerPlugin.
//
// On Init the plugin creates a PrometheusAdapter.
// On Start the plugin starts an internal HTTP server that serves the Prometheus
// handler at /metrics on a random localhost port.
// On Stop the internal server is shut down gracefully.
// ContributeCaddyRoutes returns a route that reverse-proxies the public path
// /_vibewarden/metrics to the internal server, rewriting the path to /metrics.
// InternalAddr returns the internal server address after a successful Start.
// Health reports whether the internal server is running.
type Plugin struct {
	cfg          Config
	logger       *slog.Logger
	prometheus   *metricsadapter.PrometheusAdapter
	server       *metricsadapter.Server
	internalAddr string
	running      bool
}

// New creates a new metrics Plugin with the given configuration and logger.
func New(cfg Config, logger *slog.Logger) *Plugin {
	return &Plugin{cfg: cfg, logger: logger}
}

// Name returns the canonical plugin identifier "metrics".
// This must match the key used under plugins: in vibewarden.yaml.
func (p *Plugin) Name() string { return "metrics" }

// Priority returns the plugin's initialization priority.
// Metrics is assigned priority 30 — after TLS (10) and security-headers (20).
func (p *Plugin) Priority() int { return 30 }

// Init creates the PrometheusAdapter.
// It must be called once before Start.
func (p *Plugin) Init(_ context.Context) error {
	if !p.cfg.Enabled {
		return nil
	}
	p.prometheus = metricsadapter.NewPrometheusAdapter(p.cfg.PathPatterns)
	p.logger.Info("metrics plugin initialised",
		slog.Int("path_patterns", len(p.cfg.PathPatterns)),
	)
	return nil
}

// Start creates and starts the internal metrics HTTP server.
// The server binds a random localhost port; Caddy reverse-proxies
// /_vibewarden/metrics to it.
// Start must only be called after a successful Init.
func (p *Plugin) Start(_ context.Context) error {
	if !p.cfg.Enabled {
		return nil
	}
	p.server = metricsadapter.NewServer(p.prometheus.Handler(), p.logger)
	if err := p.server.Start(); err != nil {
		return fmt.Errorf("metrics plugin: starting internal server: %w", err)
	}
	p.internalAddr = p.server.Addr()
	p.running = true
	p.logger.Info("metrics plugin started",
		slog.String("internal_addr", p.internalAddr),
	)
	return nil
}

// Stop gracefully shuts down the internal metrics server.
func (p *Plugin) Stop(ctx context.Context) error {
	if p.server == nil {
		return nil
	}
	p.running = false
	if err := p.server.Stop(ctx); err != nil {
		return fmt.Errorf("metrics plugin: stopping internal server: %w", err)
	}
	return nil
}

// Health returns the current health status of the metrics plugin.
// When disabled, Health reports healthy with a "metrics disabled" message.
// When enabled and running, Health reports healthy with the internal address.
// When enabled but not yet started, Health reports healthy with a "not started" message.
func (p *Plugin) Health() ports.HealthStatus {
	if !p.cfg.Enabled {
		return ports.HealthStatus{
			Healthy: true,
			Message: "metrics disabled",
		}
	}
	if !p.running {
		return ports.HealthStatus{
			Healthy: true,
			Message: "metrics not started",
		}
	}
	return ports.HealthStatus{
		Healthy: true,
		Message: fmt.Sprintf("metrics running at %s", p.internalAddr),
	}
}

// ContributeCaddyRoutes returns a single route that reverse-proxies
// /_vibewarden/metrics to the internal Prometheus server at InternalAddr.
// A rewrite handler translates /_vibewarden/metrics to /metrics before
// the reverse_proxy handler forwards the request.
// Returns nil when the plugin is disabled or has not been started yet.
func (p *Plugin) ContributeCaddyRoutes() []ports.CaddyRoute {
	if !p.cfg.Enabled || p.internalAddr == "" {
		return nil
	}
	return []ports.CaddyRoute{
		{
			MatchPath: "/_vibewarden/metrics",
			Handler:   buildMetricsRoute(p.internalAddr),
			Priority:  30,
		},
	}
}

// ContributeCaddyHandlers returns nil.
// The metrics plugin does not add catch-all handlers; it uses a named route.
func (p *Plugin) ContributeCaddyHandlers() []ports.CaddyHandler { return nil }

// InternalAddr returns the host:port of the internal metrics HTTP server.
// The address is only valid after a successful Start.
func (p *Plugin) InternalAddr() string { return p.internalAddr }

// ---------------------------------------------------------------------------
// Internal builders — pure functions, no side effects.
// ---------------------------------------------------------------------------

// buildMetricsRoute constructs the Caddy route map that reverse-proxies
// /_vibewarden/metrics to the internal server at internalAddr.
// The rewrite handler translates the public path to /metrics before proxying.
func buildMetricsRoute(internalAddr string) map[string]any {
	return map[string]any{
		"match": []map[string]any{
			{"path": []string{"/_vibewarden/metrics"}},
		},
		"handle": []map[string]any{
			{
				"handler": "rewrite",
				"uri":     "/metrics",
			},
			{
				"handler": "reverse_proxy",
				"upstreams": []map[string]any{
					{"dial": internalAddr},
				},
			},
		},
	}
}
