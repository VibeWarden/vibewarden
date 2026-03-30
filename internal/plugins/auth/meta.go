package auth

// Description returns a short description of the auth plugin.
func (p *Plugin) Description() string {
	return "Authentication middleware supporting Ory Kratos, JWT, and API key strategies"
}

// ConfigSchema returns the configuration field descriptions for the auth plugin.
func (p *Plugin) ConfigSchema() map[string]string {
	return map[string]string{
		"enabled":             "Enable authentication middleware (default: false)",
		"mode":                "Authentication strategy: none (default), kratos, jwt, api-key",
		"kratos_public_url":   "Base URL of the Kratos public API (required when mode is kratos)",
		"kratos_admin_url":    "Base URL of the Kratos admin API",
		"session_cookie_name": "Name of the Kratos session cookie (default: ory_kratos_session)",
		"login_url":           "Redirect URL for unauthenticated users (default: /self-service/login/browser)",
		"public_paths":        "List of URL path glob patterns that bypass authentication",
		"identity_schema":     "Identity schema: email_password, email_only, username_password, or file path",
	}
}

// Critical returns true because the auth plugin enforces authentication on
// every proxied request. A failure to initialise auth means the sidecar
// would forward unauthenticated traffic, which is a security violation.
func (p *Plugin) Critical() bool { return true }

// Example returns an example YAML configuration for the auth plugin.
func (p *Plugin) Example() string {
	return `  auth:
    enabled: true
    mode: kratos
    kratos_public_url: http://127.0.0.1:4433`
}
