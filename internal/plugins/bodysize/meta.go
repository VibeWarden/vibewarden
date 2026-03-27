package bodysize

// Description returns a short description of the body-size plugin.
func (p *Plugin) Description() string {
	return "Request body size limiting with global default and per-path overrides"
}

// ConfigSchema returns the configuration field descriptions for the body-size plugin.
func (p *Plugin) ConfigSchema() map[string]string {
	return map[string]string{
		"enabled":          "Enable body size limiting (default: true)",
		"max":              "Global default maximum body size (e.g. \"1MB\", \"512KB\"; default: \"1MB\")",
		"overrides[].path": "URL path prefix for the override (e.g. \"/api/upload\")",
		"overrides[].max":  "Maximum body size for the path (e.g. \"50MB\")",
	}
}

// Example returns an example YAML configuration for the body-size plugin.
func (p *Plugin) Example() string {
	return `  body_size:
    max: "1MB"
    overrides:
      - path: /api/upload
        max: "50MB"`
}
