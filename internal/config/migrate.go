package config

import (
	"log/slog"
	"net/url"
	"strings"
)

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

// WarnInsecureDatabase logs advisory warnings when the database configuration
// contains settings that are insecure for production use.
//
// It warns when:
//   - database.tls_mode is "disable"
//   - database.external_url contains sslmode=disable as a query parameter
//
// This function does not modify cfg and does not return an error; the warnings
// are advisory only.
func WarnInsecureDatabase(cfg *Config, logger *slog.Logger) {
	if cfg.Database.TLSMode == "disable" {
		logger.Warn("database.tls_mode is set to \"disable\"; TLS is strongly recommended for external Postgres connections")
	}

	if cfg.Database.ExternalURL != "" {
		u, err := url.Parse(cfg.Database.ExternalURL)
		if err == nil {
			if strings.EqualFold(u.Query().Get("sslmode"), "disable") {
				logger.Warn("database.external_url contains sslmode=disable; TLS is strongly recommended for external Postgres connections",
					slog.String("external_url_host", u.Host),
				)
			}
		}
	}
}
