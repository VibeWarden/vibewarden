package plugins

// PluginDescriptor holds static metadata about a compiled-in plugin.
// It is used by the CLI to list and describe plugins without instantiating them.
type PluginDescriptor struct {
	// Name is the canonical plugin identifier that matches the key in
	// vibewarden.yaml under plugins:.
	Name string

	// Description is a short, one-line summary of what the plugin does.
	Description string

	// ConfigSchema maps configuration field names to their descriptions.
	// Used by "vibewarden plugins show <name>".
	ConfigSchema map[string]string

	// Example is a minimal YAML snippet illustrating an enabled configuration.
	// The snippet is indented as it would appear under the top-level plugins: key.
	Example string
}

// Catalog is the static list of all plugins compiled into the VibeWarden binary.
// It is the authoritative source of truth for "vibewarden plugins" output.
// The order reflects the recommended initialisation priority.
var Catalog = []PluginDescriptor{
	{
		Name:        "cors",
		Description: "CORS: sets Access-Control-* headers and handles OPTIONS preflight requests",
		ConfigSchema: map[string]string{
			"enabled":           "Enable CORS middleware (default: false)",
			"allowed_origins":   "List of allowed origins, or [\"*\"] for all origins (development only)",
			"allowed_methods":   "HTTP methods allowed in cross-origin requests (default: GET, POST, PUT, DELETE, OPTIONS)",
			"allowed_headers":   "Request headers allowed in cross-origin requests (default: Content-Type, Authorization)",
			"exposed_headers":   "Response headers exposed to the browser via Access-Control-Expose-Headers",
			"allow_credentials": "Set Access-Control-Allow-Credentials: true; must not be used with allowed_origins: [\"*\"]",
			"max_age":           "Seconds to cache preflight response (Access-Control-Max-Age); 0 omits the header",
		},
		Example: `  cors:
    enabled: true
    allowed_origins:
      - "https://example.com"
    allowed_methods: ["GET", "POST", "PUT", "DELETE"]
    allowed_headers: ["Content-Type", "Authorization"]
    exposed_headers: ["X-Request-Id"]
    allow_credentials: true
    max_age: 3600`,
	},
	{
		Name:        "ip-filter",
		Description: "IP allowlist/blocklist filter: reject or permit requests by client IP or CIDR range",
		ConfigSchema: map[string]string{
			"enabled":             "Enable IP filtering (default: false)",
			"mode":                "Filter mode: \"allowlist\" (only listed IPs allowed) or \"blocklist\" (listed IPs blocked, default: \"blocklist\")",
			"addresses":           "List of IP addresses or CIDR ranges to match (e.g. \"10.0.0.0/8\", \"192.168.1.100\")",
			"trust_proxy_headers": "Read X-Forwarded-For for real client IP when behind a trusted proxy (default: false)",
		},
		Example: `  ip_filter:
    enabled: true
    mode: blocklist
    addresses:
      - "10.0.0.0/8"
      - "192.168.1.100"`,
	},
	{
		Name:        "tls",
		Description: "TLS termination with Let's Encrypt, self-signed, or external certificates",
		ConfigSchema: map[string]string{
			"enabled":      "Enable TLS (default: false)",
			"provider":     "Certificate provider: letsencrypt, self-signed, external",
			"domain":       "Domain for certificate (required for letsencrypt)",
			"cert_path":    "Path to certificate file (external provider)",
			"key_path":     "Path to key file (external provider)",
			"storage_path": "Directory for certificate storage",
		},
		Example: `  tls:
    enabled: true
    provider: letsencrypt
    domain: app.example.com`,
	},
	{
		Name:        "security-headers",
		Description: "Security headers: HSTS, X-Frame-Options, CSP, Referrer-Policy, and more",
		ConfigSchema: map[string]string{
			"enabled":                 "Enable security headers middleware (default: true)",
			"hsts_max_age":            "Strict-Transport-Security max-age in seconds (default: 31536000)",
			"hsts_include_subdomains": "Append includeSubDomains to HSTS header (default: true)",
			"hsts_preload":            "Append preload to HSTS header (default: false)",
			"content_type_nosniff":    "Set X-Content-Type-Options: nosniff (default: true)",
			"frame_option":            "X-Frame-Options value: DENY, SAMEORIGIN, or empty (default: DENY)",
			"content_security_policy": "Content-Security-Policy header value (default: default-src 'self')",
			"referrer_policy":         "Referrer-Policy header value (default: strict-origin-when-cross-origin)",
			"permissions_policy":      "Permissions-Policy header value (default: empty/disabled)",
		},
		Example: `  security-headers:
    enabled: true
    frame_option: DENY
    content_security_policy: "default-src 'self'"`,
	},
	{
		Name:        "body-size",
		Description: "Request body size limiting with global default and per-path overrides",
		ConfigSchema: map[string]string{
			"max":              "Global default maximum body size (e.g. \"1MB\", \"512KB\"; default: \"1MB\")",
			"overrides[].path": "URL path prefix for the override (e.g. \"/api/upload\")",
			"overrides[].max":  "Maximum body size for the path (e.g. \"50MB\")",
		},
		Example: `  body_size:
    max: "1MB"
    overrides:
      - path: /api/upload
        max: "50MB"`,
	},
	{
		Name:        "rate-limiting",
		Description: "Per-IP and per-user token-bucket rate limiting on every proxied request",
		ConfigSchema: map[string]string{
			"enabled":                      "Enable rate limiting (default: true)",
			"store":                        "Backing store for limiter state: memory (default)",
			"per_ip.requests_per_second":   "Sustained per-IP request rate (default: 10)",
			"per_ip.burst":                 "Per-IP burst size above the sustained rate (default: 20)",
			"per_user.requests_per_second": "Sustained per-user request rate (default: 100)",
			"per_user.burst":               "Per-user burst size above the sustained rate (default: 200)",
			"trust_proxy_headers":          "Read X-Forwarded-For for real client IP (default: false)",
			"exempt_paths":                 "URL path glob patterns that bypass rate limiting",
		},
		Example: `  rate-limiting:
    enabled: true
    per_ip:
      requests_per_second: 10
      burst: 20`,
	},
	{
		Name:        "auth",
		Description: "Authentication via Ory Kratos session validation",
		ConfigSchema: map[string]string{
			"enabled":             "Enable authentication middleware (default: false)",
			"kratos_public_url":   "Base URL of the Kratos public API (required when enabled)",
			"kratos_admin_url":    "Base URL of the Kratos admin API",
			"session_cookie_name": "Name of the Kratos session cookie (default: ory_kratos_session)",
			"login_url":           "Redirect URL for unauthenticated users",
			"public_paths":        "URL path glob patterns that bypass authentication",
			"identity_schema":     "Identity schema preset or file path",
		},
		Example: `  auth:
    enabled: true
    kratos_public_url: http://127.0.0.1:4433`,
	},
	{
		Name:        "metrics",
		Description: "Prometheus metrics endpoint at /_vibewarden/metrics",
		ConfigSchema: map[string]string{
			"enabled":       "Enable metrics collection and /_vibewarden/metrics endpoint (default: true)",
			"path_patterns": "URL path normalisation patterns using :param syntax (e.g. /users/:id)",
		},
		Example: `  metrics:
    enabled: true
    path_patterns:
      - /users/:id`,
	},
	{
		Name:        "user-management",
		Description: "Admin API for user CRUD operations via Ory Kratos",
		ConfigSchema: map[string]string{
			"enabled":          "Enable user management admin API (default: false)",
			"admin_token":      "Bearer token for /_vibewarden/admin/* endpoints (required)",
			"kratos_admin_url": "Base URL of the Kratos admin API (required)",
			"database_url":     "PostgreSQL connection string for audit logging (optional)",
		},
		Example: `  user-management:
    enabled: true
    admin_token: ${VIBEWARDEN_ADMIN_TOKEN}
    kratos_admin_url: http://127.0.0.1:4434`,
	},
	{
		Name:        "webhooks",
		Description: "Webhook delivery: dispatch security events to Slack, Discord, or any HTTP endpoint",
		ConfigSchema: map[string]string{
			"endpoints[].url":             "HTTP(S) URL to POST events to (required)",
			"endpoints[].events":          "List of event types to send, or [\"*\"] for all events",
			"endpoints[].format":          "Payload format: \"raw\" (default), \"slack\", or \"discord\"",
			"endpoints[].timeout_seconds": "Per-request HTTP timeout in seconds (default: 10)",
		},
		Example: `  webhooks:
    endpoints:
      - url: https://hooks.slack.com/services/xxx/yyy/zzz
        events: ["auth.failed", "rate_limit.hit"]
        format: slack
      - url: https://discord.com/api/webhooks/xxx/yyy
        events: ["*"]
        format: discord`,
	},
	{
		Name:        "secrets",
		Description: "Secret management: fetch static and dynamic secrets from OpenBao and inject them into proxied requests",
		ConfigSchema: map[string]string{
			"enabled":                               "Enable the secrets plugin (default: false)",
			"provider":                              "Secret store backend: \"openbao\" (default: \"openbao\")",
			"openbao.address":                       "OpenBao server URL (e.g. \"http://openbao:8200\")",
			"openbao.auth.method":                   "Auth method: \"token\" or \"approle\" (default: \"token\")",
			"openbao.auth.token":                    "Static token (used when method is \"token\")",
			"openbao.auth.role_id":                  "AppRole role_id (used when method is \"approle\")",
			"openbao.auth.secret_id":                "AppRole secret_id (used when method is \"approle\")",
			"inject.headers[].secret_path":          "KV path of the secret to inject as a header",
			"inject.headers[].secret_key":           "Key within the secret map",
			"inject.headers[].header":               "HTTP request header name",
			"inject.env_file":                       "Path to write a .env file with secret values",
			"inject.env[].secret_path":              "KV path of the secret to write to the env file",
			"inject.env[].env_var":                  "Environment variable name in the .env file",
			"dynamic.postgres.enabled":              "Enable dynamic Postgres credential generation (default: false)",
			"dynamic.postgres.roles[].name":         "OpenBao database role name",
			"dynamic.postgres.roles[].env_var_user": "Env var name for the generated username",
			"dynamic.postgres.roles[].env_var_pass": "Env var name for the generated password",
			"cache_ttl":                             "How long to cache static secrets in memory (default: 5m)",
			"health.check_interval":                 "How often to run secret health checks (default: 6h)",
			"health.max_static_age":                 "Max age of a static secret before stale warning (default: 2160h)",
		},
		Example: `  secrets:
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
          header: X-Internal-Token`,
	},
}

// FindDescriptor returns the PluginDescriptor for the plugin with the given
// name, or (PluginDescriptor{}, false) when no match is found.
func FindDescriptor(name string) (PluginDescriptor, bool) {
	for _, d := range Catalog {
		if d.Name == name {
			return d, true
		}
	}
	return PluginDescriptor{}, false
}
