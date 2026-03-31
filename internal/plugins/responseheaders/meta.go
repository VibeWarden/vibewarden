package responseheaders

// Description returns a short description of the response-headers plugin.
func (p *Plugin) Description() string {
	return "Response header modification: set, add, or remove arbitrary response headers"
}

// ConfigSchema returns the configuration field descriptions for the
// response-headers plugin, used by "vibewarden plugins show response-headers".
func (p *Plugin) ConfigSchema() map[string]string {
	return map[string]string{
		"set":    "Map of header names to values that overwrite any existing value (or create the header). Values support ${ENV_VAR} substitution.",
		"add":    "Map of header names to values that are appended to any existing value (or create the header). Values support ${ENV_VAR} substitution.",
		"remove": "List of header names to delete from every response.",
	}
}

// Example returns an example YAML configuration for the response-headers plugin.
func (p *Plugin) Example() string {
	return `  response_headers:
    remove:
      - Server
    set:
      X-Service-Version: "${APP_VERSION}"
      X-Frame-Options: SAMEORIGIN
    add:
      Cache-Control: no-store`
}
