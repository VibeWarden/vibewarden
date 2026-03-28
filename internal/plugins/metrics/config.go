// Package metrics implements the VibeWarden metrics plugin.
//
// It creates a Prometheus adapter and an internal HTTP server, then contributes
// a Caddy route that reverse-proxies /_vibewarden/metrics to that server.
// The plugin implements ports.Plugin, ports.CaddyContributor, and
// ports.InternalServerPlugin.
package metrics

// Config holds all settings for the metrics plugin.
// It maps to the telemetry: section of vibewarden.yaml (and falls back to the
// deprecated metrics: section via MigrateLegacyMetrics during config loading).
type Config struct {
	// Enabled toggles the metrics plugin and the /_vibewarden/metrics endpoint.
	// Default: true.
	Enabled bool

	// PathPatterns is a list of URL path normalization patterns using :param
	// syntax (e.g. "/users/:id"). Paths that match no pattern are recorded
	// as "other". An empty slice disables path normalization.
	PathPatterns []string

	// PrometheusEnabled toggles the Prometheus pull-based exporter.
	// When enabled, metrics are served at /_vibewarden/metrics.
	// Default: true.
	PrometheusEnabled bool

	// OTLPEnabled toggles the OTLP push-based exporter.
	// When enabled, metrics are pushed to OTLPEndpoint on the configured interval.
	// Default: false.
	OTLPEnabled bool

	// OTLPEndpoint is the OTLP HTTP endpoint URL (e.g., "http://localhost:4318").
	// Required when OTLPEnabled is true.
	OTLPEndpoint string

	// OTLPHeaders are optional HTTP headers for OTLP authentication.
	OTLPHeaders map[string]string

	// OTLPInterval is the export interval for the OTLP exporter as a duration
	// string (e.g., "30s", "1m"). Zero or empty string defaults to 30s.
	OTLPInterval string

	// OTLPProtocol is the OTLP protocol. Only "http" is supported in this version.
	// Default: "http".
	OTLPProtocol string
}
