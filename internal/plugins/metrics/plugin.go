package metrics

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	metricsadapter "github.com/vibewarden/vibewarden/internal/adapters/metrics"
	oteladapter "github.com/vibewarden/vibewarden/internal/adapters/otel"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// Plugin is the metrics plugin for VibeWarden.
// It implements ports.Plugin, ports.CaddyContributor, and ports.InternalServerPlugin.
//
// On Init the plugin creates an OTel provider and an OTelAdapter using the
// configured exporters (Prometheus, OTLP, or both).
// On Start the plugin starts an internal HTTP server that serves the Prometheus
// handler at /metrics on a random localhost port (only when Prometheus exporter is enabled).
// On Stop the internal server is shut down gracefully and the OTel SDK is shut down.
// ContributeCaddyRoutes returns a route that reverse-proxies the public path
// /_vibewarden/metrics to the internal server, rewriting the path to /metrics.
// This route is only contributed when the Prometheus exporter is active.
// InternalAddr returns the internal server address after a successful Start.
// Health reports whether the internal server is running.
type Plugin struct {
	cfg          Config
	logger       *slog.Logger
	otelProvider *oteladapter.Provider
	adapter      *metricsadapter.OTelAdapter
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

// Init creates the OTel provider and OTelAdapter.
// It must be called once before Start.
func (p *Plugin) Init(ctx context.Context) error {
	if !p.cfg.Enabled {
		return nil
	}

	// Build ports.TelemetryConfig from plugin config.
	telemetryCfg, err := p.buildTelemetryConfig()
	if err != nil {
		return fmt.Errorf("metrics plugin: building telemetry config: %w", err)
	}

	// Initialize OTel provider.
	p.otelProvider = oteladapter.NewProvider()
	if err := p.otelProvider.Init(ctx, "vibewarden", Version, telemetryCfg); err != nil {
		return fmt.Errorf("metrics plugin: initializing otel provider: %w", err)
	}

	// Create the OTel-backed metrics adapter.
	adapter, err := metricsadapter.NewOTelAdapter(p.otelProvider, p.cfg.PathPatterns)
	if err != nil {
		return fmt.Errorf("metrics plugin: creating otel adapter: %w", err)
	}
	p.adapter = adapter

	p.logger.Info("metrics plugin initialised with OTel SDK",
		slog.Int("path_patterns", len(p.cfg.PathPatterns)),
		slog.Bool("prometheus_enabled", p.otelProvider.PrometheusEnabled()),
		slog.Bool("otlp_enabled", p.otelProvider.OTLPEnabled()),
	)
	return nil
}

// Start creates and starts the internal metrics HTTP server (Prometheus exporter only).
// When only the OTLP exporter is enabled, Start is a no-op for the HTTP server —
// the OTLP PeriodicReader handles export automatically.
// Start must only be called after a successful Init.
func (p *Plugin) Start(ctx context.Context) error {
	if !p.cfg.Enabled {
		return nil
	}
	// Only start the internal HTTP server when Prometheus exporter is active.
	if p.otelProvider == nil || !p.otelProvider.PrometheusEnabled() {
		return nil
	}
	p.server = metricsadapter.NewServer(p.adapter.Handler(), p.logger)
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

// Stop gracefully shuts down the internal metrics server and the OTel SDK.
func (p *Plugin) Stop(ctx context.Context) error {
	if p.server != nil {
		p.running = false
		if err := p.server.Stop(ctx); err != nil {
			return fmt.Errorf("metrics plugin: stopping internal server: %w", err)
		}
	}
	if p.otelProvider != nil {
		if err := p.otelProvider.Shutdown(ctx); err != nil {
			return fmt.Errorf("metrics plugin: shutting down otel provider: %w", err)
		}
	}
	return nil
}

// Health returns the current health status of the metrics plugin.
// When disabled, Health reports healthy with a "metrics disabled" message.
// When enabled and running (Prometheus), Health reports healthy with the internal address.
// When enabled with OTLP only, Health reports healthy with an "otlp-only" message.
// When enabled but not yet started, Health reports healthy with a "not started" message.
func (p *Plugin) Health() ports.HealthStatus {
	if !p.cfg.Enabled {
		return ports.HealthStatus{
			Healthy: true,
			Message: "metrics disabled",
		}
	}
	if p.otelProvider != nil && p.otelProvider.OTLPEnabled() && !p.otelProvider.PrometheusEnabled() {
		return ports.HealthStatus{
			Healthy: true,
			Message: "metrics running (otlp-only, push-based)",
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
// /_vibewarden/metrics to the internal metrics server at InternalAddr.
// A rewrite handler translates /_vibewarden/metrics to /metrics before
// the reverse_proxy handler forwards the request.
// Returns nil when the plugin is disabled, the Prometheus exporter is inactive,
// or the plugin has not been started yet.
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
// The address is only valid after a successful Start with Prometheus enabled.
func (p *Plugin) InternalAddr() string { return p.internalAddr }

// Collector returns the MetricsCollector for use by middleware.
// Returns a NoOpMetricsCollector if the plugin is disabled or not initialized.
func (p *Plugin) Collector() ports.MetricsCollector {
	if p.adapter == nil {
		return metricsadapter.NoOpMetricsCollector{}
	}
	return p.adapter
}

// ---------------------------------------------------------------------------
// Internal helpers — pure functions, no side effects.
// ---------------------------------------------------------------------------

// buildTelemetryConfig converts the plugin Config into a ports.TelemetryConfig,
// parsing duration strings and applying defaults.
func (p *Plugin) buildTelemetryConfig() (ports.TelemetryConfig, error) {
	cfg := ports.TelemetryConfig{
		Prometheus: ports.PrometheusExporterConfig{
			Enabled: p.cfg.PrometheusEnabled,
		},
		OTLP: ports.OTLPExporterConfig{
			Enabled:  p.cfg.OTLPEnabled,
			Endpoint: p.cfg.OTLPEndpoint,
			Headers:  p.cfg.OTLPHeaders,
			Protocol: p.cfg.OTLPProtocol,
		},
	}

	// Parse OTLP interval if provided.
	if p.cfg.OTLPInterval != "" {
		d, err := time.ParseDuration(p.cfg.OTLPInterval)
		if err != nil {
			return ports.TelemetryConfig{}, fmt.Errorf("invalid interval duration %q: %w", p.cfg.OTLPInterval, err)
		}
		cfg.OTLP.Interval = d
	}

	return cfg, nil
}

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
