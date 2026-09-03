package config

import "fmt"

// ServerConfig holds server-related settings.
type ServerConfig struct {
	// Host to bind to (default: "127.0.0.1")
	Host string `mapstructure:"host"`
	// Port to listen on (default: 8443)
	Port int `mapstructure:"port"`

	// ReadTimeout is the maximum duration for reading the entire incoming
	// request including body, expressed as a Go duration string (e.g. "30s").
	// A value of "0" or "" disables the timeout (no limit).
	// Default: "30s".
	ReadTimeout string `mapstructure:"read_timeout"`

	// WriteTimeout is the maximum duration before timing out writes of the
	// response, expressed as a Go duration string (e.g. "60s").
	// A value of "0" or "" disables the timeout (no limit).
	// Default: "60s".
	WriteTimeout string `mapstructure:"write_timeout"`

	// IdleTimeout is the maximum amount of time to wait for the next request
	// when keep-alives are enabled, expressed as a Go duration string (e.g. "120s").
	// A value of "0" or "" disables the timeout (no limit).
	// Default: "120s".
	IdleTimeout string `mapstructure:"idle_timeout"`

	// MaxConnections caps concurrent inbound connections to the sidecar's
	// listener. When the cap is reached, new connections are refused
	// (accepted and immediately closed) until an existing connection ends;
	// established connections and in-flight requests are unaffected.
	// A value of 0 explicitly disables the cap (unlimited). Negative values
	// are rejected by validation.
	// Default: 1000.
	MaxConnections int `mapstructure:"max_connections"`
}

// AppConfig configures the user's application in the generated Docker Compose.
// The default is to use a pre-built image (Image field). Build-from-source is
// opt-in via the Build field. When both are set, Build takes precedence.
// When neither is set, no app service is rendered and VibeWarden falls back
// to forwarding to host.docker.internal.
type AppConfig struct {
	// Build is the Docker build context path (e.g., "." for the current directory).
	// Opt-in: set this only when you want Compose to build the image from source.
	// When set, the app service is rendered with a build: context directive.
	Build string `mapstructure:"build"`

	// Image is the Docker image reference (e.g., "myapp:latest" or "ghcr.io/org/myapp:latest").
	// Default: derived from the project name by `vibew init`/`vibew wrap` (e.g., "myapp:latest").
	// Can be overridden at runtime via the VIBEWARDEN_APP_IMAGE environment variable.
	Image string `mapstructure:"image"`

	// Healthcheck is the Docker healthcheck command for the app container.
	// When empty, the default probe is chosen based on Language:
	//   python     → python -c "import urllib.request; ..."
	//   typescript → node -e "require('http').get(...)"
	//   go/kotlin  → wget -q --spider ...  (Alpine images ship wget)
	//   (default)  → wget -q --spider ...
	// Set to "none" to disable the healthcheck entirely.
	Healthcheck string `mapstructure:"healthcheck"`

	// Language is the app's primary language/runtime. Used to select the
	// appropriate Docker health check probe when Healthcheck is not set.
	// Values: "go", "python", "typescript", "kotlin", or empty (auto-detect
	// at init/wrap time; falls back to wget).
	Language string `mapstructure:"language"`

	// Environment is a map of custom environment variables injected into the
	// app container in the generated Docker Compose file. Use this for runtime
	// configuration (e.g. DATABASE_URL, API keys). Values are rendered as
	// KEY=VALUE entries in the environment list of the app service.
	Environment map[string]string `mapstructure:"environment"`
}

// LogConfig holds logging settings.
type LogConfig struct {
	// Level: "debug", "info", "warn", "error" (default: "info")
	Level string `mapstructure:"level"`
	// Format: "json" or "text" (default: "json")
	Format string `mapstructure:"format"`
	// AccessLog enables access logging of every completed HTTP request (default: true).
	// When true, each request is logged at INFO level with method, path, status,
	// duration, client IP, request ID, user agent, and bytes written.
	AccessLog bool `mapstructure:"access_log"`
}

// AdminConfig holds admin API settings.
type AdminConfig struct {
	// Enabled toggles the admin API (default: false)
	Enabled bool `mapstructure:"enabled"`
	// Token is the bearer token for admin API authentication.
	// Can be set via VIBEWARDEN_ADMIN_TOKEN env var.
	Token string `mapstructure:"token"`
}

// WatchConfig holds settings for the config file watcher.
type WatchConfig struct {
	// Enabled toggles automatic config reload on file changes. Default: true.
	Enabled bool `mapstructure:"enabled"`

	// Debounce is the duration to wait after the last file change before
	// triggering a reload, expressed as a Go duration string (e.g. "500ms").
	// Default: "500ms".
	Debounce string `mapstructure:"debounce"`
}

// OverridesConfig provides escape hatches for users who need to supply
// hand-crafted configuration files instead of relying on VibeWarden's
// auto-generation. All fields are optional.
type OverridesConfig struct {
	// KratosConfig is the path to a custom kratos.yml file.
	// When non-empty, VibeWarden uses this file instead of generating one.
	KratosConfig string `mapstructure:"kratos_config"`

	// ComposeFile is the path to a custom docker-compose.yml file.
	// When non-empty, VibeWarden uses this file instead of generating one.
	ComposeFile string `mapstructure:"compose_file"`

	// IdentitySchema is the path to a custom Kratos identity schema JSON file.
	// When non-empty, this file is used instead of the preset selected by auth.identity_schema.
	IdentitySchema string `mapstructure:"identity_schema"`
}

// AuditConfig holds settings for the security audit log sink.
type AuditConfig struct {
	// Enabled toggles the audit log sink (default: true).
	// When true, every security-relevant event is written to the configured output.
	Enabled bool `mapstructure:"enabled"`

	// Output selects the write destination.
	// Accepted values:
	//   "stdout"       — write JSONL to standard output (default)
	//   <file path>   — append JSONL to the file at the given path; the file is
	//                   created if it does not exist.
	// When Output is empty, "stdout" is assumed.
	Output string `mapstructure:"output"`
}

// IPFilterConfig holds IP-based access control settings.
type IPFilterConfig struct {
	// Enabled toggles the IP filter plugin (default: false).
	Enabled bool `mapstructure:"enabled"`

	// Mode selects the filter behaviour: "allowlist" or "blocklist" (default: "blocklist").
	// allowlist: only listed IPs/CIDRs may access the service.
	// blocklist: listed IPs/CIDRs are blocked; all others are permitted.
	Mode string `mapstructure:"mode"`

	// Addresses is the list of IP addresses or CIDR ranges to match against.
	// Examples: "10.0.0.0/8", "192.168.1.100", "2001:db8::/32".
	Addresses []string `mapstructure:"addresses"`

	// TrustProxyHeaders enables reading X-Forwarded-For for the real client IP.
	// Only enable when VibeWarden runs behind a trusted reverse proxy.
	TrustProxyHeaders bool `mapstructure:"trust_proxy_headers"`
}

// ErrorPagesConfig holds configuration for custom error page responses.
// When enabled, VibeWarden serves files from Directory instead of the default
// JSON error body for matching HTTP status codes.
//
// File naming convention: <status_code>.<ext> (e.g., 401.html, 403.json, 429.html).
// Content-Type is inferred from the file extension:
//   - .html  → text/html
//   - .json  → application/json
//
// When no file matches a given status code, VibeWarden falls back to the
// built-in JSON error response.
type ErrorPagesConfig struct {
	// Enabled toggles the custom error pages feature (default: false).
	Enabled bool `mapstructure:"enabled"`

	// Directory is the path to the directory containing custom error page files.
	// The directory must be readable at startup. Required when Enabled is true.
	Directory string `mapstructure:"directory"`
}

// MaintenanceConfig holds all settings for the maintenance mode plugin.
// It maps to the maintenance section of vibewarden.yaml.
type MaintenanceConfig struct {
	// Enabled toggles maintenance mode (default: false).
	// When true, all requests except those to /_vibewarden/* paths receive
	// a 503 Service Unavailable response.
	Enabled bool `mapstructure:"enabled"`

	// Message is the human-readable message returned to clients in the 503 body.
	// Defaults to "Service is under maintenance" when empty.
	Message string `mapstructure:"message"`
}

// CompressionConfig holds settings for response body compression.
// Maps to the compression section of vibewarden.yaml.
type CompressionConfig struct {
	// Enabled toggles response compression. Default: true.
	Enabled bool `mapstructure:"enabled"`

	// Algorithms is the ordered list of compression algorithms to offer.
	// Caddy negotiates the best match with the client via Accept-Encoding.
	// Valid values: "gzip", "zstd".
	// Default: ["zstd", "gzip"].
	Algorithms []string `mapstructure:"algorithms"`
}

// ResponseHeadersConfig holds settings for arbitrary response header modification.
// Maps to the response_headers section of vibewarden.yaml.
// Operations are applied in the order: remove, then set, then add.
type ResponseHeadersConfig struct {
	// Set maps header names to values that overwrite any existing value (or create
	// the header when absent). Values may reference environment variables using
	// ${VAR} syntax; Caddy resolves these at request time.
	Set map[string]string `mapstructure:"set"`

	// Add maps header names to values that are appended to any existing value (or
	// create the header when absent). Values may reference environment variables
	// using ${VAR} syntax.
	Add map[string]string `mapstructure:"add"`

	// Remove is the list of header names to delete from every response.
	Remove []string `mapstructure:"remove"`
}

// BodySizeConfig holds request body size limit settings.
type BodySizeConfig struct {
	// Max is the global default maximum request body size as a human-readable
	// string (e.g. "1MB", "512KB"). Parsed at startup.
	// An empty string or "0" means no limit.
	Max string `mapstructure:"max"`

	// Overrides defines per-path body size limits.
	// Each entry can increase or decrease the global limit for a specific path.
	Overrides []BodySizeOverrideConfig `mapstructure:"overrides"`
}

// BodySizeOverrideConfig defines a per-path body size limit.
type BodySizeOverrideConfig struct {
	// Path is the URL path prefix to match (e.g. "/api/upload").
	Path string `mapstructure:"path"`

	// Max is the maximum request body size for this path as a human-readable
	// string (e.g. "50MB"). An empty string or "0" means no limit for this path.
	Max string `mapstructure:"max"`
}

// validateSidecar validates sidecar-shell configuration (body_size, ip_filter,
// error_pages) and returns a slice of error strings.
func validateSidecar(c *Config) []string {
	var errs []string

	// body_size.max validation.
	if c.BodySize.Max != "" {
		if _, err := ParseBodySize(c.BodySize.Max); err != nil {
			errs = append(errs, fmt.Sprintf("body_size.max: %s", err.Error()))
		}
	}

	// ip_filter.mode validation.
	if c.IPFilter.Enabled {
		switch c.IPFilter.Mode {
		case "", "allowlist", "blocklist":
			// valid
		default:
			errs = append(errs, fmt.Sprintf(
				"ip_filter.mode %q is invalid; accepted values: \"allowlist\", \"blocklist\"",
				c.IPFilter.Mode,
			))
		}
	}

	// body_size.overrides validation.
	for i, ov := range c.BodySize.Overrides {
		prefix := fmt.Sprintf("body_size.overrides[%d]", i)
		if ov.Path == "" {
			errs = append(errs, fmt.Sprintf("%s.path is required", prefix))
		}
		if ov.Max != "" {
			if _, err := ParseBodySize(ov.Max); err != nil {
				errs = append(errs, fmt.Sprintf("%s.max: %s", prefix, err.Error()))
			}
		}
	}

	// server.max_connections validation.
	if c.Server.MaxConnections < 0 {
		errs = append(errs, fmt.Sprintf(
			"server.max_connections must be >= 0 (0 disables the limit), got %d",
			c.Server.MaxConnections,
		))
	}

	// error_pages validation.
	if c.ErrorPages.Enabled && c.ErrorPages.Directory == "" {
		errs = append(errs, "error_pages.directory is required when error_pages.enabled is true")
	}

	return errs
}
