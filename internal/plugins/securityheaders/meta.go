package securityheaders

// Description returns a short description of the security-headers plugin.
func (p *Plugin) Description() string {
	return "Security headers: HSTS, X-Frame-Options, CSP, Referrer-Policy, and more"
}

// ConfigSchema returns the configuration field descriptions for the security-headers plugin.
func (p *Plugin) ConfigSchema() map[string]string {
	return map[string]string{
		"enabled":                 "Enable security headers middleware (default: true)",
		"hsts_max_age":            "Strict-Transport-Security max-age in seconds (default: 31536000)",
		"hsts_include_subdomains": "Append includeSubDomains to HSTS header (default: true)",
		"hsts_preload":            "Append preload to HSTS header (default: false)",
		"content_type_nosniff":    "Set X-Content-Type-Options: nosniff (default: true)",
		"frame_option":            "X-Frame-Options value: DENY, SAMEORIGIN, or empty to disable (default: DENY)",
		"content_security_policy": "Content-Security-Policy header value (default: default-src 'self')",
		"referrer_policy":         "Referrer-Policy header value (default: strict-origin-when-cross-origin)",
		"permissions_policy":      "Permissions-Policy header value (default: empty/disabled)",
	}
}

// Example returns an example YAML configuration for the security-headers plugin.
func (p *Plugin) Example() string {
	return `  security-headers:
    enabled: true
    frame_option: DENY
    content_security_policy: "default-src 'self'"`
}
