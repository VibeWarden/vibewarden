package usermgmt

// Description returns a short description of the user-management plugin.
func (p *Plugin) Description() string {
	return "Admin API for user CRUD operations via Ory Kratos"
}

// ConfigSchema returns the configuration field descriptions for the user-management plugin.
func (p *Plugin) ConfigSchema() map[string]string {
	return map[string]string{
		"enabled":          "Enable user management admin API (default: false)",
		"admin_token":      "Bearer token for /_vibewarden/admin/* endpoints (required when enabled)",
		"kratos_admin_url": "Base URL of the Kratos admin API (required when enabled)",
		"database_url":     "PostgreSQL connection string for audit logging (optional)",
	}
}

// Example returns an example YAML configuration for the user-management plugin.
func (p *Plugin) Example() string {
	return `  user-management:
    enabled: true
    admin_token: ${VIBEWARDEN_ADMIN_TOKEN}
    kratos_admin_url: http://127.0.0.1:4434`
}
