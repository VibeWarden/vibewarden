package secrets

// Description returns a short description of the secrets plugin.
func (p *Plugin) Description() string {
	return "Secret management: fetch static and dynamic secrets from OpenBao and inject them into proxied requests"
}

// ConfigSchema returns the configuration field descriptions for the secrets plugin.
func (p *Plugin) ConfigSchema() map[string]string {
	return map[string]string{
		"enabled":                               "Enable the secrets plugin (default: false)",
		"provider":                              "Secret store backend: \"openbao\" (default: \"openbao\")",
		"openbao.address":                       "OpenBao server URL (e.g. \"http://openbao:8200\")",
		"openbao.auth.method":                   "Auth method: \"token\" or \"approle\" (default: \"token\")",
		"openbao.auth.token":                    "Static token (used when method is \"token\")",
		"openbao.auth.role_id":                  "AppRole role_id (used when method is \"approle\")",
		"openbao.auth.secret_id":                "AppRole secret_id (used when method is \"approle\")",
		"openbao.mount_path":                    "KV v2 mount path (default: \"secret\")",
		"inject.headers[].secret_path":          "KV path of the secret to inject as a header",
		"inject.headers[].secret_key":           "Key within the secret map",
		"inject.headers[].header":               "HTTP request header name (e.g. \"X-Internal-Token\")",
		"inject.env_file":                       "Path to write a .env file with secret values (optional)",
		"inject.env[].secret_path":              "KV path of the secret to write to the env file",
		"inject.env[].secret_key":               "Key within the secret map",
		"inject.env[].env_var":                  "Environment variable name in the .env file",
		"dynamic.postgres.enabled":              "Enable dynamic Postgres credential generation (default: false)",
		"dynamic.postgres.roles[].name":         "OpenBao database role name",
		"dynamic.postgres.roles[].env_var_user": "Env var name for the generated username",
		"dynamic.postgres.roles[].env_var_pass": "Env var name for the generated password",
		"cache_ttl":                             "How long to cache static secrets in memory (default: 5m)",
		"health.check_interval":                 "How often to run secret health checks (default: 6h)",
		"health.max_static_age":                 "Max age of a static secret before a stale warning is emitted (default: 2160h)",
		"health.weak_patterns":                  "Substrings that indicate a weak/default secret value",
	}
}

// Example returns an example YAML configuration for the secrets plugin.
func (p *Plugin) Example() string {
	return `  secrets:
    enabled: true
    provider: openbao
    openbao:
      address: http://openbao:8200
      auth:
        method: approle
        role_id: ${OPENBAO_ROLE_ID}
        secret_id: ${OPENBAO_SECRET_ID}
    inject:
      headers:
        - secret_path: app/internal-api
          secret_key: token
          header: X-Internal-Token
      env_file: /run/secrets/.env.secrets
      env:
        - secret_path: app/database
          secret_key: password
          env_var: DATABASE_PASSWORD
    dynamic:
      postgres:
        enabled: false
        roles:
          - name: app-readwrite
            env_var_user: DATABASE_USER
            env_var_password: DATABASE_PASSWORD`
}
