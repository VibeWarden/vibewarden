package webhooks

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/vibewarden/vibewarden/internal/adapters/webhook"
	"github.com/vibewarden/vibewarden/internal/domain/events"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// Plugin is the VibeWarden webhook delivery plugin.
// It wraps a webhook.Dispatcher and exposes it as a ports.Plugin and a
// ports.EventLogger so that the application layer can route events through it.
//
// The plugin itself acts as an EventLogger decorator: it passes each event
// to the underlying logger and then dispatches it to all configured webhooks.
type Plugin struct {
	cfg        Config
	dispatcher *webhook.Dispatcher
	underlying ports.EventLogger
	logger     *slog.Logger
	healthy    bool
	healthMsg  string
}

// New creates a new webhooks Plugin. The underlying EventLogger receives every
// event first; the plugin then dispatches asynchronously to webhook endpoints.
// Pass nil for underlying when no chaining is needed (the plugin logs nothing
// to the slog chain on its own).
func New(cfg Config, underlying ports.EventLogger, logger *slog.Logger) *Plugin {
	return &Plugin{
		cfg:        cfg,
		underlying: underlying,
		logger:     logger,
		healthMsg:  "not initialised",
	}
}

// Name returns the canonical plugin identifier "webhooks".
func (p *Plugin) Name() string { return "webhooks" }

// Init validates the configuration and constructs the dispatcher.
func (p *Plugin) Init(_ context.Context) error {
	if len(p.cfg.Endpoints) == 0 {
		p.healthy = true
		p.healthMsg = "webhooks disabled: no endpoints configured"
		p.logger.Info("webhooks plugin: no endpoints configured, plugin is a no-op")
		return nil
	}

	d, err := webhook.NewDispatcher(p.cfg.Endpoints, p.logger)
	if err != nil {
		p.healthy = false
		p.healthMsg = fmt.Sprintf("init failed: %s", err.Error())
		return fmt.Errorf("webhooks plugin init: %w", err)
	}
	p.dispatcher = d
	p.healthy = true
	p.healthMsg = fmt.Sprintf("active (%d endpoints)", len(p.cfg.Endpoints))
	p.logger.Info("webhooks plugin initialised",
		slog.Int("endpoints", len(p.cfg.Endpoints)),
	)
	return nil
}

// Start is a no-op. All delivery work is triggered via Log calls.
func (p *Plugin) Start(_ context.Context) error { return nil }

// Stop is a no-op. In-flight goroutines will drain naturally; the plugin does
// not maintain a worker pool that needs explicit shutdown.
func (p *Plugin) Stop(_ context.Context) error {
	p.healthy = false
	p.healthMsg = "stopped"
	return nil
}

// Health returns the current health status of the webhooks plugin.
func (p *Plugin) Health() ports.HealthStatus {
	return ports.HealthStatus{
		Healthy: p.healthy,
		Message: p.healthMsg,
	}
}

// Log implements ports.EventLogger. It first forwards event to the underlying
// logger (if non-nil), then dispatches it asynchronously to all matching
// webhook endpoints. Dispatch errors are logged but never propagated so that
// webhook failures cannot block the event pipeline.
func (p *Plugin) Log(ctx context.Context, event events.Event) error {
	// Forward to the underlying logger first.
	if p.underlying != nil {
		if err := p.underlying.Log(ctx, event); err != nil {
			return fmt.Errorf("underlying event logger: %w", err)
		}
	}

	// Skip dispatch when no dispatcher is configured (no endpoints).
	if p.dispatcher == nil {
		return nil
	}

	// Dispatch is always non-blocking per the port contract.
	if err := p.dispatcher.Dispatch(ctx, event); err != nil {
		// Should never happen — Dispatch always returns nil.
		p.logger.WarnContext(ctx, "webhook dispatch returned unexpected error",
			slog.String("error", err.Error()),
			slog.String("event_type", event.EventType),
		)
	}
	return nil
}

// Interface guards.
var (
	_ ports.Plugin      = (*Plugin)(nil)
	_ ports.EventLogger = (*Plugin)(nil)
)
