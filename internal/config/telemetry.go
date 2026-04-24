package config

import "fmt"

// MetricsConfig holds Prometheus metrics settings.
//
// Deprecated: Use TelemetryConfig instead. MetricsConfig remains only for backward
// compatibility. Settings are migrated to TelemetryConfig at startup via MigrateLegacyMetrics.
type MetricsConfig struct {
	// Enabled toggles metrics collection and the /_vibewarden/metrics endpoint (default: true).
	Enabled bool `mapstructure:"enabled"`

	// PathPatterns is a list of URL path normalization patterns using :param syntax.
	// Example: "/users/:id", "/api/v1/items/:item_id/comments/:comment_id"
	// Paths that don't match any pattern are recorded as "other".
	PathPatterns []string `mapstructure:"path_patterns"`
}

// TelemetryConfig holds all telemetry export settings.
// This replaces the narrower MetricsConfig and supports both pull (Prometheus)
// and push (OTLP) export modes.
//
// Prometheus is the automatic fallback: when no telemetry block is present in
// vibewarden.yaml, prometheus.enabled defaults to true and otlp.enabled defaults
// to false. This means /_vibewarden/metrics always works out of the box, with
// zero configuration required. Existing Prometheus scrapers and Grafana dashboards
// continue to work without any changes.
//
// To add OTLP push export, set otlp.enabled = true and provide an endpoint.
// Both exporters can run simultaneously.
type TelemetryConfig struct {
	// Enabled toggles telemetry collection entirely (default: true).
	Enabled bool `mapstructure:"enabled"`

	// PathPatterns is a list of URL path normalization patterns using :param syntax.
	// Example: "/users/:id", "/api/v1/items/:item_id/comments/:comment_id"
	// Paths that don't match any pattern are recorded as "other".
	PathPatterns []string `mapstructure:"path_patterns"`

	// Prometheus configures the pull-based Prometheus exporter.
	Prometheus PrometheusExporterConfig `mapstructure:"prometheus"`

	// OTLP configures the push-based OTLP exporter.
	OTLP OTLPExporterConfig `mapstructure:"otlp"`

	// Logs configures structured event log export settings.
	Logs LogsConfig `mapstructure:"logs"`

	// Traces configures distributed tracing settings.
	Traces TracesConfig `mapstructure:"traces"`
}

// TracesConfig holds distributed tracing settings.
type TracesConfig struct {
	// Enabled toggles distributed tracing (default: false).
	// When enabled, a span is created for each HTTP request and exported via OTLP.
	// Requires telemetry.otlp.enabled to be true.
	Enabled bool `mapstructure:"enabled"`
}

// LogsConfig holds log export settings.
type LogsConfig struct {
	// OTLP toggles OTLP log export (default: false).
	// When enabled, structured events are exported to the same OTLP endpoint as metrics.
	// Requires telemetry.otlp.endpoint to be configured.
	OTLP bool `mapstructure:"otlp"`
}

// PrometheusExporterConfig configures the Prometheus pull-based exporter.
type PrometheusExporterConfig struct {
	// Enabled toggles the Prometheus exporter (default: true).
	// When enabled, metrics are served at /_vibewarden/metrics.
	Enabled bool `mapstructure:"enabled"`
}

// OTLPExporterConfig configures the OTLP push-based exporter.
type OTLPExporterConfig struct {
	// Enabled toggles the OTLP exporter (default: false).
	Enabled bool `mapstructure:"enabled"`

	// Endpoint is the OTLP HTTP endpoint URL (e.g., "http://localhost:4318").
	// Required when Enabled is true.
	Endpoint string `mapstructure:"endpoint"`

	// Headers are optional HTTP headers for authentication.
	// Example: {"Authorization": "Bearer <token>"}
	Headers map[string]string `mapstructure:"headers"`

	// Interval is the export interval as a duration string (default: "30s").
	// Metrics are batched and pushed at this interval.
	Interval string `mapstructure:"interval"`

	// Protocol is "http" or "grpc" (default: "http").
	// Only "http" is supported in this version.
	Protocol string `mapstructure:"protocol"`
}

// ResilienceConfig holds resilience settings for upstream connections.
type ResilienceConfig struct {
	// Timeout is the maximum time to wait for the upstream application to
	// respond, expressed as a duration string (e.g. "30s", "1m").
	// A value of "0" or "" disables the timeout (no limit).
	// Default: "30s".
	Timeout string `mapstructure:"timeout"`

	// CircuitBreaker configures the circuit breaker middleware.
	CircuitBreaker CircuitBreakerConfig `mapstructure:"circuit_breaker"`

	// Retry configures the retry-with-exponential-backoff middleware.
	Retry RetryConfig `mapstructure:"retry"`
}

// RetryConfig holds retry-with-exponential-backoff settings.
type RetryConfig struct {
	// Enabled toggles the retry middleware (default: false).
	Enabled bool `mapstructure:"enabled"`

	// MaxAttempts is the total number of attempts including the initial request.
	// Must be >= 2 when Enabled is true. Default: 3.
	MaxAttempts int `mapstructure:"max_attempts"`

	// InitialBackoff is the wait before the first retry, as a duration string
	// (e.g. "100ms", "500ms"). Default: "100ms".
	InitialBackoff string `mapstructure:"backoff"`

	// MaxBackoff is the upper bound on the computed backoff, as a duration string
	// (e.g. "10s"). Default: "10s".
	MaxBackoff string `mapstructure:"max_backoff"`

	// RetryOn is the list of HTTP status codes that trigger a retry.
	// Default: [502, 503, 504].
	RetryOn []int `mapstructure:"retry_on"`
}

// CircuitBreakerConfig holds circuit breaker settings.
type CircuitBreakerConfig struct {
	// Enabled toggles the circuit breaker middleware.
	Enabled bool `mapstructure:"enabled"`

	// Threshold is the number of consecutive failures required to trip the
	// circuit from Closed to Open. Must be > 0 when Enabled is true.
	// Default: 5.
	Threshold int `mapstructure:"threshold"`

	// Timeout is how long the circuit stays Open before transitioning to
	// HalfOpen to allow a probe request, expressed as a duration string
	// (e.g. "60s", "1m"). Must be > 0 when Enabled is true.
	// Default: "60s".
	Timeout string `mapstructure:"timeout"`
}

// ObservabilityConfig holds settings for the optional observability stack.
// When enabled, vibewarden generate produces Prometheus, Grafana, Loki, and
// Promtail configs under .vibewarden/generated/observability/.
type ObservabilityConfig struct {
	// Enabled toggles generation of the observability stack (default: false).
	Enabled bool `mapstructure:"enabled"`

	// GrafanaPort is the host port Grafana binds to (default: 3001).
	// This avoids conflict with common app ports like 3000.
	GrafanaPort int `mapstructure:"grafana_port"`

	// PrometheusPort is the host port Prometheus binds to (default: 9090).
	PrometheusPort int `mapstructure:"prometheus_port"`

	// LokiPort is the host port Loki binds to (default: 3100).
	LokiPort int `mapstructure:"loki_port"`

	// RetentionDays is how long Loki retains log data (default: 7).
	RetentionDays int `mapstructure:"retention_days"`
}

// validateTelemetry validates telemetry configuration and returns a slice of error strings.
func validateTelemetry(c *Config) []string {
	var errs []string
	// telemetry.logs.otlp requires telemetry.otlp.endpoint.
	if c.Telemetry.Logs.OTLP && c.Telemetry.OTLP.Endpoint == "" {
		errs = append(errs, "telemetry.logs.otlp requires telemetry.otlp.endpoint")
	}
	return errs
}

// validateObservability validates observability configuration and returns a slice of error strings.
func validateObservability(c *Config) []string {
	var errs []string
	if c.Observability.Enabled {
		if c.Observability.GrafanaPort <= 0 || c.Observability.GrafanaPort > 65535 {
			errs = append(errs, fmt.Sprintf(
				"observability.grafana_port %d is invalid; must be 1-65535",
				c.Observability.GrafanaPort,
			))
		}
		if c.Observability.PrometheusPort <= 0 || c.Observability.PrometheusPort > 65535 {
			errs = append(errs, fmt.Sprintf(
				"observability.prometheus_port %d is invalid; must be 1-65535",
				c.Observability.PrometheusPort,
			))
		}
		if c.Observability.LokiPort <= 0 || c.Observability.LokiPort > 65535 {
			errs = append(errs, fmt.Sprintf(
				"observability.loki_port %d is invalid; must be 1-65535",
				c.Observability.LokiPort,
			))
		}
		if c.Observability.RetentionDays <= 0 {
			errs = append(errs, fmt.Sprintf(
				"observability.retention_days %d is invalid; must be > 0",
				c.Observability.RetentionDays,
			))
		}
	}
	return errs
}
