package config

import "log/slog"

// MigrateLegacyMetrics converts a legacy metrics: config section to the new
// telemetry: section. If the user has customised the metrics: block (either by
// disabling it or by setting path patterns) but has not set an explicit
// telemetry: block, this function copies the relevant settings over and logs a
// deprecation warning so the user knows to update their configuration.
//
// This function is idempotent: calling it multiple times has no additional effect.
func MigrateLegacyMetrics(cfg *Config, logger *slog.Logger) {
	// Only migrate when the metrics section has been customised.
	// "Customised" means the user explicitly disabled metrics or provided path patterns.
	if cfg.Metrics.Enabled && len(cfg.Metrics.PathPatterns) == 0 {
		// Defaults — nothing to migrate.
		return
	}

	// Copy legacy settings to the new TelemetryConfig.
	cfg.Telemetry.Enabled = cfg.Metrics.Enabled
	cfg.Telemetry.PathPatterns = cfg.Metrics.PathPatterns
	cfg.Telemetry.Prometheus.Enabled = cfg.Metrics.Enabled

	logger.Warn("DEPRECATED: 'metrics:' config section is deprecated, please migrate to 'telemetry:' instead",
		slog.Bool("metrics_enabled", cfg.Metrics.Enabled),
		slog.Int("path_patterns", len(cfg.Metrics.PathPatterns)),
	)
}
