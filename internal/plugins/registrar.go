// Package plugins provides the plugin registry and lifecycle management for
// VibeWarden. It initialises, starts, stops, and health-checks all registered
// plugins in the correct order.
package plugins

import (
	"log/slog"

	"github.com/vibewarden/vibewarden/internal/config"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// PluginRegistrar is a function that registers plugins with a Registry.
// It is called during serve startup to allow external packages to contribute
// additional plugins alongside the built-in OSS catalog.
//
// The Pro binary uses this to register proprietary plugins without forking the
// OSS codebase. See internal/app/serve.RunServe for how registrars are invoked.
type PluginRegistrar func(
	registry *Registry,
	cfg *config.Config,
	eventLogger ports.EventLogger,
	logger *slog.Logger,
)
