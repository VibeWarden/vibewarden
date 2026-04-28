package config

import (
	"fmt"
	"time"
)

// UpstreamConfig holds settings for the upstream application being protected.
type UpstreamConfig struct {
	// Host of the upstream application (default: "127.0.0.1")
	Host string `mapstructure:"host"`
	// Port of the upstream application (default: 3000)
	Port int `mapstructure:"port"`
	// Health configures the active upstream health checker.
	Health UpstreamHealthConfig `mapstructure:"health"`
}

// UpstreamHealthConfig holds settings for the active upstream health checker.
type UpstreamHealthConfig struct {
	// Enabled toggles the background health checker (default: true since v0.18.2).
	Enabled bool `mapstructure:"enabled"`

	// Path is the HTTP path probed on the upstream (default: "/health").
	// Must return a 2xx status code within Timeout to be considered healthy.
	Path string `mapstructure:"path"`

	// Interval is the time between consecutive probes, as a duration string
	// (e.g. "5s", "1m"). Default: "5s".
	Interval string `mapstructure:"interval"`

	// Timeout is the maximum time to wait for a probe response, as a duration
	// string (e.g. "2s"). Default: "2s".
	Timeout string `mapstructure:"timeout"`

	// UnhealthyThreshold is the number of consecutive failures required to
	// transition to Unhealthy. Default: 3.
	UnhealthyThreshold int `mapstructure:"unhealthy_threshold"`

	// HealthyThreshold is the number of consecutive successes required to
	// transition to Healthy. Default: 2.
	HealthyThreshold int `mapstructure:"healthy_threshold"`
}

// validateUpstream validates upstream configuration and returns a slice of error strings.
func validateUpstream(c *Config) []string {
	var errs []string
	if c.Upstream.Health.Enabled {
		if c.Upstream.Health.Path == "" {
			errs = append(errs, "upstream.health.path must not be empty when upstream.health.enabled is true")
		}
		if c.Upstream.Health.Interval != "" {
			if _, err := time.ParseDuration(c.Upstream.Health.Interval); err != nil {
				errs = append(errs, fmt.Sprintf("upstream.health.interval %q is not a valid duration: %s", c.Upstream.Health.Interval, err.Error()))
			}
		}
		if c.Upstream.Health.Timeout != "" {
			if _, err := time.ParseDuration(c.Upstream.Health.Timeout); err != nil {
				errs = append(errs, fmt.Sprintf("upstream.health.timeout %q is not a valid duration: %s", c.Upstream.Health.Timeout, err.Error()))
			}
		}
		if c.Upstream.Health.UnhealthyThreshold <= 0 {
			errs = append(errs, fmt.Sprintf(
				"upstream.health.unhealthy_threshold %d is invalid; must be > 0",
				c.Upstream.Health.UnhealthyThreshold,
			))
		}
		if c.Upstream.Health.HealthyThreshold <= 0 {
			errs = append(errs, fmt.Sprintf(
				"upstream.health.healthy_threshold %d is invalid; must be > 0",
				c.Upstream.Health.HealthyThreshold,
			))
		}
	}
	return errs
}
