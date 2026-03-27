package cors

// Description returns a short description of the CORS plugin.
func (p *Plugin) Description() string {
	return "CORS: sets Access-Control-* headers and handles OPTIONS preflight requests"
}

// ConfigSchema returns the configuration field descriptions for the CORS plugin.
func (p *Plugin) ConfigSchema() map[string]string {
	return map[string]string{
		"enabled":           "Enable CORS middleware (default: false)",
		"allowed_origins":   "List of allowed origins, or [\"*\"] for all origins (development only)",
		"allowed_methods":   "HTTP methods allowed in cross-origin requests (default: GET, POST, PUT, DELETE, OPTIONS)",
		"allowed_headers":   "Request headers allowed in cross-origin requests (default: Content-Type, Authorization)",
		"exposed_headers":   "Response headers exposed to the browser via Access-Control-Expose-Headers",
		"allow_credentials": "Set Access-Control-Allow-Credentials: true; must not be used with allowed_origins: [\"*\"]",
		"max_age":           "Seconds to cache preflight response (Access-Control-Max-Age); 0 omits the header",
	}
}

// Example returns an example YAML configuration for the CORS plugin.
func (p *Plugin) Example() string {
	return `  cors:
    enabled: true
    allowed_origins:
      - "https://example.com"
    allowed_methods: ["GET", "POST", "PUT", "DELETE"]
    allowed_headers: ["Content-Type", "Authorization"]
    exposed_headers: ["X-Request-Id"]
    allow_credentials: true
    max_age: 3600`
}
