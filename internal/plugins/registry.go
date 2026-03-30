// Package plugins provides the plugin registry and lifecycle management for
// VibeWarden. It initialises, starts, stops, and health-checks all registered
// plugins in the correct order.
package plugins

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/vibewarden/vibewarden/internal/ports"
)

// Registry manages the lifecycle of all VibeWarden plugins.
// Plugins are registered at startup and then driven through Init → Start → Stop.
// Stop is always called in the reverse of registration order so that
// dependent plugins shut down before their dependencies.
type Registry struct {
	plugins []ports.Plugin
	logger  *slog.Logger
}

// NewRegistry creates a Registry that uses logger for lifecycle events.
func NewRegistry(logger *slog.Logger) *Registry {
	return &Registry{logger: logger}
}

// Register adds p to the registry. It must be called before InitAll.
// Plugins are started and stopped in registration order / reverse order.
func (r *Registry) Register(p ports.Plugin) {
	r.plugins = append(r.plugins, p)
}

// Plugins returns a shallow copy of the registered plugin slice.
func (r *Registry) Plugins() []ports.Plugin {
	result := make([]ports.Plugin, len(r.plugins))
	copy(result, r.plugins)
	return result
}

// CaddyContributors returns every registered plugin that implements
// ports.CaddyContributor.
func (r *Registry) CaddyContributors() []ports.CaddyContributor {
	var contributors []ports.CaddyContributor
	for _, p := range r.plugins {
		if c, ok := p.(ports.CaddyContributor); ok {
			contributors = append(contributors, c)
		}
	}
	return contributors
}

// InitAll calls Init on every registered plugin in registration order.
// If any plugin's Init returns an error the function returns immediately
// with that error; subsequent plugins are not initialised.
func (r *Registry) InitAll(ctx context.Context) error {
	for _, p := range r.plugins {
		r.logger.InfoContext(ctx, "initialising plugin", slog.String("plugin", p.Name()))
		if err := p.Init(ctx); err != nil {
			return fmt.Errorf("init plugin %q: %w", p.Name(), err)
		}
	}
	return nil
}

// StartAll calls Start on every registered plugin in registration order.
// If any plugin's Start returns an error the function returns immediately
// with that error; subsequent plugins are not started.
func (r *Registry) StartAll(ctx context.Context) error {
	for _, p := range r.plugins {
		r.logger.InfoContext(ctx, "starting plugin", slog.String("plugin", p.Name()))
		if err := p.Start(ctx); err != nil {
			return fmt.Errorf("start plugin %q: %w", p.Name(), err)
		}
	}
	return nil
}

// StopAll calls Stop on every registered plugin in reverse registration order.
// It collects all errors and returns them combined so that a single failure
// does not prevent the remaining plugins from being stopped.
func (r *Registry) StopAll(ctx context.Context) error {
	var errs []error
	for i := len(r.plugins) - 1; i >= 0; i-- {
		p := r.plugins[i]
		r.logger.InfoContext(ctx, "stopping plugin", slog.String("plugin", p.Name()))
		if err := p.Stop(ctx); err != nil {
			errs = append(errs, fmt.Errorf("stop plugin %q: %w", p.Name(), err))
		}
	}
	if len(errs) == 1 {
		return errs[0]
	}
	if len(errs) > 1 {
		return fmt.Errorf("multiple stop errors: %w", joinErrors(errs))
	}
	return nil
}

// HealthAll returns a map from plugin name to its current HealthStatus.
func (r *Registry) HealthAll() map[string]ports.HealthStatus {
	result := make(map[string]ports.HealthStatus, len(r.plugins))
	for _, p := range r.plugins {
		result[p.Name()] = p.Health()
	}
	return result
}

// ReadinessChecker returns a ports.ReadinessChecker that evaluates the health
// of all registered plugins and, optionally, the upstream application.
//
// upstreamChecker may be nil; when nil the readiness check does not require an
// upstream probe and UpstreamReachable is always reported as true.
func (r *Registry) ReadinessChecker(upstreamChecker ports.UpstreamHealthChecker) ports.ReadinessChecker {
	return &registryReadinessChecker{
		registry:        r,
		upstreamChecker: upstreamChecker,
	}
}

// registryReadinessChecker implements ports.ReadinessChecker using the plugin
// registry and an optional upstream health checker.
type registryReadinessChecker struct {
	registry        *Registry
	upstreamChecker ports.UpstreamHealthChecker
}

// Ready returns true when all plugins are healthy and the upstream is reachable.
// It is safe for concurrent use and does not block.
func (rc *registryReadinessChecker) Ready() bool {
	rs := rc.ReadinessStatus()
	return rs.PluginsReady && rs.UpstreamReachable
}

// ReadinessStatus returns a snapshot of per-plugin health and upstream status.
// It is safe for concurrent use and does not block.
func (rc *registryReadinessChecker) ReadinessStatus() ports.ReadinessStatus {
	pluginStatuses := rc.registry.HealthAll()

	pluginsReady := true
	for _, hs := range pluginStatuses {
		if !hs.Healthy {
			pluginsReady = false
			break
		}
	}

	upstreamReachable := true
	if rc.upstreamChecker != nil {
		status := rc.upstreamChecker.CurrentStatus()
		upstreamReachable = status.String() == "healthy"
	}

	return ports.ReadinessStatus{
		PluginsReady:      pluginsReady,
		UpstreamReachable: upstreamReachable,
		Plugins:           pluginStatuses,
	}
}

// joinErrors combines multiple errors into a single error whose message is the
// concatenation of each error's message. Using a simple join keeps the
// implementation free of non-stdlib dependencies.
func joinErrors(errs []error) error {
	msg := errs[0].Error()
	for _, e := range errs[1:] {
		msg += "; " + e.Error()
	}
	return fmt.Errorf("%s", msg)
}
