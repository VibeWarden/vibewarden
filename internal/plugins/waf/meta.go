package waf

// Description returns a short description of the WAF plugin.
func (p *Plugin) Description() string {
	return "WAF: Content-Type validation and built-in attack detection (SQLi, XSS, path traversal, command injection)"
}

// ConfigSchema returns the configuration field descriptions for the WAF plugin.
func (p *Plugin) ConfigSchema() map[string]string {
	return map[string]string{
		"content_type_validation.enabled": "Enable Content-Type validation on POST, PUT, PATCH requests (default: false)",
		"content_type_validation.allowed": "List of permitted media types (default: application/json, application/x-www-form-urlencoded, multipart/form-data)",
		"engine.enabled":                  "Enable the built-in WAF rule engine (default: false)",
		"engine.mode":                     "WAF engine mode: \"block\" (default) returns 403 on detection; \"detect\" logs and passes through",
		"engine.rules.sqli":               "Enable SQL injection detection rules (default: true)",
		"engine.rules.xss":                "Enable XSS detection rules (default: true)",
		"engine.rules.path_traversal":     "Enable path traversal detection rules (default: true)",
		"engine.rules.cmd_injection":      "Enable command injection detection rules (default: true)",
		"engine.exempt_paths":             "URL path glob patterns that bypass WAF scanning (/_vibewarden/* is always exempt)",
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
        - "multipart/form-data"
    engine:
      enabled: true
      mode: block         # or: detect
      rules:
        sqli: true
        xss: true
        path_traversal: true
        cmd_injection: true
      exempt_paths:
        - "/healthz"
        - "/readyz"`
}
