package waf

// Description returns a short description of the WAF plugin.
func (p *Plugin) Description() string {
	return "WAF: Content-Type validation blocks body requests with missing or disallowed media types"
}

// ConfigSchema returns the configuration field descriptions for the WAF plugin.
func (p *Plugin) ConfigSchema() map[string]string {
	return map[string]string{
		"content_type_validation.enabled": "Enable Content-Type validation on POST, PUT, PATCH requests (default: false)",
		"content_type_validation.allowed": "List of permitted media types (default: application/json, application/x-www-form-urlencoded, multipart/form-data)",
	}
}

// Example returns an example YAML configuration for the WAF plugin.
func (p *Plugin) Example() string {
	return `  waf:
    content_type_validation:
      enabled: true
      allowed:
        - "application/json"
        - "application/x-www-form-urlencoded"
        - "multipart/form-data"`
}
