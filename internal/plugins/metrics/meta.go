package metrics

// Description returns a short description of the metrics plugin.
func (p *Plugin) Description() string {
	return "Prometheus metrics endpoint at /_vibewarden/metrics"
}

// ConfigSchema returns the configuration field descriptions for the metrics plugin.
func (p *Plugin) ConfigSchema() map[string]string {
	return map[string]string{
		"enabled":       "Enable metrics collection and the /_vibewarden/metrics endpoint (default: true)",
		"path_patterns": "URL path normalisation patterns using :param syntax (e.g. /users/:id)",
	}
}

// Example returns an example YAML configuration for the metrics plugin.
func (p *Plugin) Example() string {
	return `  metrics:
    enabled: true
    path_patterns:
      - /users/:id
      - /api/v1/items/:item_id`
}
