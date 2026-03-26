// Package metrics implements the VibeWarden metrics plugin.
//
// It creates a Prometheus adapter and an internal HTTP server, then contributes
// a Caddy route that reverse-proxies /_vibewarden/metrics to that server.
// The plugin implements ports.Plugin, ports.CaddyContributor, and
// ports.InternalServerPlugin.
package metrics

// Config holds all settings for the metrics plugin.
// It maps to the plugins.metrics section of vibewarden.yaml and falls back
// to the legacy top-level metrics.* configuration keys.
type Config struct {
	// Enabled toggles the metrics plugin and the /_vibewarden/metrics endpoint.
	// Default: true.
	Enabled bool

	// PathPatterns is a list of URL path normalization patterns using :param
	// syntax (e.g. "/users/:id"). Paths that match no pattern are recorded
	// as "other". An empty slice disables path normalization.
	PathPatterns []string
}
