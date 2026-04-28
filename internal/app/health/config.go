// Package health provides application-layer helpers for the upstream health
// probe lifecycle: configuration translation, default application, and boot
// warning when the probe is disabled.
package health

import (
	"log/slog"
	"time"

	"github.com/vibewarden/vibewarden/internal/config"
	domainheal "github.com/vibewarden/vibewarden/internal/domain/health"
)

// BuildDomainConfig translates the runtime UpstreamHealthConfig into a
// domain-layer Config, applying defaults for any missing fields, and reports
// whether the probe should run. It emits a Warn log when the probe is disabled
// so operators upgrading to v0.18.2+ notice the behaviour change.
//
// Defaults applied when fields are zero:
//   - Interval: 5s
//   - Timeout:  2s
//   - Path:     "/health"
//   - UnhealthyThreshold: 3
//   - HealthyThreshold:   2
//
// Returns (zero Config, false) when Enabled is false.
// Returns (resolved Config, true) when Enabled is true.
func BuildDomainConfig(cfg config.UpstreamHealthConfig, logger *slog.Logger) (domainheal.Config, bool) {
	if !cfg.Enabled {
		if logger != nil {
			logger.Warn("upstream health probe disabled — /_vibewarden/health will report upstream:unknown",
				slog.String("hint", "set upstream.health.enabled: true (default in v0.18.2+)"),
			)
		}
		return domainheal.Config{}, false
	}

	path := cfg.Path
	if path == "" {
		path = "/health"
	}

	interval := mustDuration(cfg.Interval, 5*time.Second, "upstream.health.interval", logger)
	timeout := mustDuration(cfg.Timeout, 2*time.Second, "upstream.health.timeout", logger)

	unhealthy := cfg.UnhealthyThreshold
	if unhealthy <= 0 {
		unhealthy = 3
	}
	healthy := cfg.HealthyThreshold
	if healthy <= 0 {
		healthy = 2
	}

	return domainheal.Config{
		Enabled:            true,
		Path:               path,
		Interval:           interval,
		Timeout:            timeout,
		UnhealthyThreshold: unhealthy,
		HealthyThreshold:   healthy,
	}, true
}

// mustDuration parses a duration string and returns the result. When raw is
// empty, the provided default is returned. When parsing fails, the default is
// returned and a Warn log is emitted (if logger is non-nil).
func mustDuration(raw string, def time.Duration, name string, logger *slog.Logger) time.Duration {
	if raw == "" {
		return def
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		if logger != nil {
			logger.Warn("invalid duration in config — using default",
				slog.String("field", name),
				slog.String("value", raw),
				slog.String("default", def.String()),
				slog.String("error", err.Error()),
			)
		}
		return def
	}
	return d
}
