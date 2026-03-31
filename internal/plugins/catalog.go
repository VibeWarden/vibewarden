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
		Name:        "maintenance",
		Description: "Maintenance mode: return 503 Service Unavailable for all requests except /_vibewarden/* health endpoints",
		ConfigSchema: map[string]string{
			"enabled": "Enable maintenance mode (default: false)",
			"message": "Message returned to clients in the 503 response body (default: \"Service is under maintenance\")",
		},
		Example: `  maintenance:
    enabled: true
    message: "Scheduled maintenance — back in 30 minutes"`,
	},
	{
		Name:        "input-validation",
		Description: "Input validation: enforce URL length, query string length, header count, and header value size limits before WAF scanning",
		ConfigSchema: map[string]string{
			"enabled":                                  "Enable the input validation middleware (default: false)",
			"max_url_length":                           "Maximum allowed raw request URI length in bytes (default: 2048; 0 disables)",
			"max_query_string_length":                  "Maximum allowed query string length in bytes (default: 2048; 0 disables)",
			"max_header_count":                         "Maximum number of request headers allowed (default: 100; 0 disables)",
			"max_header_size":                          "Maximum allowed byte length of any single header value (default: 8192; 0 disables)",
			"path_overrides[].path":                    "URL path glob pattern (path.Match syntax) for this override",
			"path_overrides[].max_url_length":          "Override max URL length for matching paths (0 inherits global value)",
			"path_overrides[].max_query_string_length": "Override max query string length for matching paths (0 inherits global value)",
			"path_overrides[].max_header_count":        "Override max header count for matching paths (0 inherits global value)",
			"path_overrides[].max_header_size":         "Override max header value size for matching paths (0 inherits global value)",
		},
		Example: `  input_validation:
    enabled: true
    max_url_length: 2048
    max_query_string_length: 2048
    max_header_count: 100
    max_header_size: 8192
    path_overrides:
      - path: /api/upload
        max_query_string_length: 8192`,
	},
	{
		Name:        "waf",
		Description: "WAF: Content-Type validation blocks body requests with missing or disallowed media types",
		ConfigSchema: map[string]string{
			"content_type_validation.enabled": "Enable Content-Type validation on POST, PUT, PATCH requests (default: false)",
			"content_type_validation.allowed": "List of permitted media types (default: application/json, application/x-www-form-urlencoded, multipart/form-data)",
		},
		Example: `  waf:
    content_type_validation:
      enabled: true
      allowed:
        - "application/json"
        - "application/x-www-form-urlencoded"
        - "multipart/form-data"`,
	},
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
			"content_security_policy": "Content-Security-Policy header value (default: empty — header not sent; set explicitly to opt in)",
			"referrer_policy":         "Referrer-Policy header value (default: strict-origin-when-cross-origin)",
			"permissions_policy":      "Permissions-Policy header value (default: empty/disabled)",
		},
		Example: `  security-headers:
    enabled: true
    frame_option: DENY
    # content_security_policy is empty by default (header not sent).
    # Set explicitly to opt in, e.g.: content_security_policy: "default-src 'self'"`,
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
		Name:        "egress",
		Description: "Egress proxy: allowlist outbound API calls with secret injection, rate limiting, and circuit breaking",
		ConfigSchema: map[string]string{
			"enabled":                              "Enable the egress proxy plugin (default: false)",
			"listen":                               "TCP address the egress proxy binds to (default: \"127.0.0.1:8081\")",
			"default_policy":                       "Disposition for unmatched routes: \"allow\" or \"deny\" (default: \"deny\")",
			"allow_insecure":                       "Permit plain HTTP egress requests globally (default: false)",
			"default_timeout":                      "Global request timeout as a Go duration string (default: \"30s\")",
			"dns.block_private":                    "Block requests to private/loopback IPs — SSRF protection (default: true)",
			"dns.allowed_private":                  "CIDR ranges exempt from block_private (e.g. [\"10.0.0.0/8\"])",
			"routes[].name":                        "Unique identifier for the route (required)",
			"routes[].pattern":                     "URL glob pattern, must start with http:// or https:// (required)",
			"routes[].methods":                     "HTTP methods this route applies to; empty means all methods",
			"routes[].timeout":                     "Per-route request timeout as a Go duration string",
			"routes[].secret":                      "OpenBao secret name to fetch and inject",
			"routes[].secret_header":               "HTTP request header to inject the secret value into",
			"routes[].secret_format":               "Value template; \"{value}\" is replaced with the secret value",
			"routes[].rate_limit":                  "Rate limit expression (e.g. \"100/s\", \"1000/m\")",
			"routes[].circuit_breaker.threshold":   "Consecutive failures required to open the circuit",
			"routes[].circuit_breaker.reset_after": "How long the circuit stays open before a probe (Go duration)",
			"routes[].retries.max":                 "Maximum number of retry attempts",
			"routes[].allow_insecure":              "Permit plain HTTP for this route only (default: false)",
		},
		Example: `  egress:
    enabled: true
    listen: "127.0.0.1:8081"
    default_policy: deny
    default_timeout: "30s"
    dns:
      block_private: true
    routes:
      - name: stripe-api
        pattern: "https://api.stripe.com/**"
        methods: ["POST"]
        timeout: "10s"
        secret: app/stripe
        secret_header: Authorization
        secret_format: "Bearer {value}"
        rate_limit: "100/s"
        circuit_breaker:
          threshold: 5
          reset_after: "30s"`,
	},
	{
		Name:        "response-headers",
		Description: "Response header modification: set, add, or remove arbitrary response headers",
		ConfigSchema: map[string]string{
			"set":    "Map of header names to values that overwrite any existing value (or create the header). Values support ${ENV_VAR} substitution.",
			"add":    "Map of header names to values that are appended to any existing value (or create the header). Values support ${ENV_VAR} substitution.",
			"remove": "List of header names to delete from every response.",
		},
		Example: `  response_headers:
    remove:
      - Server
    set:
      X-Service-Version: "${APP_VERSION}"
      X-Frame-Options: SAMEORIGIN
    add:
      Cache-Control: no-store`,
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
