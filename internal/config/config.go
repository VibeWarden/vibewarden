// Package config provides configuration loading and validation for VibeWarden.
package config

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/spf13/viper"
)

// Config holds all configuration for VibeWarden.
// Fields are loaded from vibewarden.yaml and can be overridden by environment variables.
type Config struct {
	// Name is the project name used to derive the Docker Compose project name
	// and remote deploy directory. When empty, the project name is derived from
	// the directory containing vibewarden.yaml.
	Name string `mapstructure:"name"`

	// Profile selects the deployment profile: "dev", "tls", or "prod".
	// Affects TLS settings, credential handling, and validation rules.
	// Defaults to "dev".
	Profile string `mapstructure:"profile"`

	// Server configuration
	Server ServerConfig `mapstructure:"server"`

	// Upstream application configuration
	Upstream UpstreamConfig `mapstructure:"upstream"`

	// App configures how the user's application is included in the generated
	// Docker Compose file. By default, app.image is set to the project name
	// with ":latest" tag. Set app.build to build from source instead.
	// When neither is set, no app service is rendered and VibeWarden falls back
	// to forwarding to host.docker.internal.
	App AppConfig `mapstructure:"app"`

	// TLS configuration
	TLS TLSConfig `mapstructure:"tls"`

	// Kratos (identity) configuration
	Kratos KratosConfig `mapstructure:"kratos"`

	// Auth middleware configuration
	Auth AuthConfig `mapstructure:"auth"`

	// Rate limiting configuration
	RateLimit RateLimitConfig `mapstructure:"rate_limit"`

	// Logging configuration
	Log LogConfig `mapstructure:"log"`

	// Admin API configuration
	Admin AdminConfig `mapstructure:"admin"`

	// Security headers configuration
	SecurityHeaders SecurityHeadersConfig `mapstructure:"security_headers"`

	// Metrics configuration (DEPRECATED: use Telemetry instead).
	// This field remains for backward compatibility. On load, MigrateLegacyMetrics
	// copies any customised metrics settings into Telemetry and logs a deprecation warning.
	Metrics MetricsConfig `mapstructure:"metrics"`

	// Telemetry configures all telemetry export settings (Prometheus and OTLP).
	// This replaces the narrower Metrics config and is the preferred section.
	Telemetry TelemetryConfig `mapstructure:"telemetry"`

	// Database configuration
	Database DatabaseConfig `mapstructure:"database"`

	// BodySize configures request body size limits.
	BodySize BodySizeConfig `mapstructure:"body_size"`

	// IPFilter configures IP-based access control.
	IPFilter IPFilterConfig `mapstructure:"ip_filter"`

	// Webhooks configures outbound webhook delivery.
	Webhooks WebhooksConfig `mapstructure:"webhooks"`

	// Secrets configures the secret management plugin (OpenBao integration).
	Secrets SecretsConfig `mapstructure:"secrets"`

	// Overrides provides escape hatches for advanced users who need to supply
	// hand-crafted config files instead of relying on VibeWarden's generation.
	Overrides OverridesConfig `mapstructure:"overrides"`

	// Resilience configures upstream resilience features such as request timeouts.
	Resilience ResilienceConfig `mapstructure:"resilience"`

	// CORS configures the Cross-Origin Resource Sharing plugin.
	CORS CORSConfig `mapstructure:"cors"`

	// Observability configures the optional observability stack (Prometheus,
	// Grafana, Loki, Promtail) generated under the "observability" compose profile.
	Observability ObservabilityConfig `mapstructure:"observability"`

	// Audit configures the security audit log sink.
	Audit AuditConfig `mapstructure:"audit"`

	// WAF configures the Web Application Firewall plugins.
	WAF WAFConfig `mapstructure:"waf"`

	// InputValidation configures request input size limit enforcement.
	InputValidation InputValidationConfig `mapstructure:"input_validation"`

	// Egress configures the egress proxy plugin for outbound API call control.
	Egress EgressConfig `mapstructure:"egress"`

	// ErrorPages configures custom error page responses for specific HTTP status codes.
	ErrorPages ErrorPagesConfig `mapstructure:"error_pages"`

	// Maintenance configures the maintenance mode plugin.
	Maintenance MaintenanceConfig `mapstructure:"maintenance"`

	// Compression configures response body compression.
	Compression CompressionConfig `mapstructure:"compression"`

	// ResponseHeaders configures arbitrary response header modifications applied
	// after all other middleware including security headers.
	ResponseHeaders ResponseHeadersConfig `mapstructure:"response_headers"`

	// Watch configures the config file watcher for hot reload.
	Watch WatchConfig `mapstructure:"watch"`

	// Deploy configures `vibew bundle` deploy-target settings. Fields here
	// describe the deploy *target* and have no effect on the running sidecar.
	Deploy DeployConfig `mapstructure:"deploy"`

	// DeployMode is set to true by the deploy service when generating files for
	// a deploy bundle. Templates use this to adjust paths (e.g. build context
	// is the original App.Build value in deploy mode instead of the resolved
	// ProjectRoot in dev mode). This field is not loaded from YAML — it is set
	// programmatically.
	DeployMode bool `mapstructure:"-"`

	// ProjectRoot is the absolute path to the project directory (i.e. the
	// directory containing vibewarden.yaml). Set by loadInternal (and therefore
	// by Load, LoadRaw, and LoadStrict) to the directory that contains the
	// resolved config file. Callers of ComposeProjectName() MUST use the Config
	// returned by a loader function — do not set this field manually.
	// This field is not loaded from YAML.
	ProjectRoot string `mapstructure:"-"`
}

// InternalNetworkName returns the Docker network name used for internal
// service-to-service communication. When network isolation is enabled it
// returns "vibewarden-internal"; otherwise it returns the legacy single
// network name "vibewarden".
func (c *Config) InternalNetworkName() string {
	if c.Egress.IsNetworkIsolationEnabled() {
		return "vibewarden-internal"
	}
	return "vibewarden"
}

// IsProdProfile reports whether the deployment profile is "prod".
// It is used by templates to select production-grade service configuration
// (e.g. OpenBao server mode instead of dev mode).
func (c *Config) IsProdProfile() bool {
	return c.Profile == "prod"
}

// sanitizeProjectName lowercases and replaces non-alphanumeric characters with
// hyphens, matching Docker Compose's project name rules.
func sanitizeProjectName(name string) string {
	name = strings.ToLower(name)
	var b strings.Builder
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			b.WriteRune(r)
		} else {
			b.WriteRune('-')
		}
	}
	return strings.Trim(b.String(), "-")
}

// ComposeProjectName returns the Docker Compose project name to use in the
// generated docker-compose.yml. This prevents Docker Compose from deriving the
// project name from the directory name (which would be "generated" for files
// under .vibewarden/generated/) and avoids stale image names like "generated-app".
//
// Derivation order:
//  1. The explicit Name field (set via vibewarden.yaml name: or vibew init/wrap).
//     Since vibew init and vibew wrap always populate name:, this branch fires
//     for all projects created with v0.19.0+.
//  2. The project directory name (from ProjectRoot), lowercased and sanitized.
//     Defensive fallback for projects that pre-date the unconditional name: write.
//  3. "vibewarden" as a last-resort fallback (should not happen in practice).
func (c *Config) ComposeProjectName() string {
	if c.Name != "" {
		// sanitizeProjectName is applied so that a user-supplied name like
		// "My App" is normalised to "my-app" before Docker Compose sees it.
		// Docker Compose requires [a-z0-9_-]+ for project names.
		return sanitizeProjectName(c.Name)
	}
	if c.ProjectRoot != "" {
		if name := sanitizeProjectName(filepath.Base(c.ProjectRoot)); name != "" {
			return name
		}
	}
	return "vibewarden"
}

// EgressNoProxy builds the NO_PROXY value for the app service based on which
// internal services are enabled in the configuration. The value always includes
// localhost and the vibewarden service name. Additional services are appended
// based on configuration flags (e.g., kratos, openbao, redis).
func (c *Config) EgressNoProxy() string {
	parts := []string{"localhost", "127.0.0.1", "vibewarden"}

	kratosMode := c.Auth.Active() && c.Auth.Mode == AuthModeKratos && !c.Kratos.External
	if kratosMode {
		parts = append(parts, "kratos")
		if c.Database.ExternalURL == "" {
			parts = append(parts, "kratos-db")
		}
	}

	if c.Secrets.Enabled {
		parts = append(parts, "openbao")
	}

	if c.RateLimit.Store == "redis" && !c.RateLimit.Redis.HasExternalURL() {
		parts = append(parts, "redis")
	}

	if c.Observability.Enabled {
		parts = append(parts, "prometheus", "loki", "promtail", "otel-collector", "jaeger", "grafana")
	}

	return strings.Join(parts, ",")
}

// ResolvedBuildContext returns the Docker build context path for the app
// service. In deploy mode it returns App.Build verbatim (a relative path
// that Docker Compose resolves from the compose file directory). In
// generate mode it returns the absolute build context by joining ProjectRoot
// with the App.Build subdirectory (stripping a leading "./" prefix first).
// When App.Build is "." (project root), ProjectRoot alone is returned.
func (c *Config) ResolvedBuildContext() string {
	if c.DeployMode {
		return c.App.Build
	}
	if c.App.Build == "" || c.App.Build == "." {
		return c.ProjectRoot
	}
	sub := strings.TrimPrefix(c.App.Build, "./")
	return c.ProjectRoot + "/" + sub
}

// Validate checks the loaded configuration for logical consistency.
// It returns a combined error listing all violations found.
// Call Validate after Load to catch misconfiguration early.
func (c *Config) Validate() error {
	var errs []string

	// Profile validation. An empty string is allowed (defaults to "dev" via Load).
	validProfiles := map[string]bool{"": true, "dev": true, "tls": true, "prod": true}
	if !validProfiles[c.Profile] {
		errs = append(errs, fmt.Sprintf("profile must be 'dev', 'tls', or 'prod', got %q", c.Profile))
	}

	errs = append(errs, validateTLS(c)...)
	errs = append(errs, validateAuth(c)...)
	errs = append(errs, validateSidecar(c)...)
	errs = append(errs, validateWebhooks(c)...)
	errs = append(errs, validateRateLimit(c)...)
	errs = append(errs, validateTelemetry(c)...)
	errs = append(errs, validateUpstream(c)...)
	errs = append(errs, validateCORS(c)...)
	errs = append(errs, validateObservability(c)...)
	errs = append(errs, validateDatabase(c)...)
	if c.Egress.Enabled {
		errs = append(errs, validateEgressConfig(c.Egress)...)
	}

	if len(errs) > 0 {
		return fmt.Errorf("%s", strings.Join(errs, "; "))
	}
	return nil
}

// Load reads configuration from file and environment variables.
// Config file path can be specified; defaults to "./vibewarden.yaml".
// Environment variables override file values using VIBEWARDEN_ prefix.
// Example: VIBEWARDEN_SERVER_PORT=9090 overrides server.port.
func Load(configPath string) (*Config, error) {
	return loadInternal(configPath, true)
}

// setDefaults applies all viper defaults for every config key. This function
// is called by loadInternal and shared between Load and LoadRaw.
func setDefaults(v *viper.Viper) {
	v.SetDefault("name", "")
	v.SetDefault("profile", "dev")
	v.SetDefault("server.host", "127.0.0.1")
	v.SetDefault("server.port", 8443)
	v.SetDefault("upstream.host", "127.0.0.1")
	v.SetDefault("upstream.port", 3000)
	v.SetDefault("tls.enabled", true)
	v.SetDefault("tls.provider", "self-signed")
	v.SetDefault("tls.cert_monitoring.enabled", true)
	v.SetDefault("tls.cert_monitoring.check_interval", "6h")
	v.SetDefault("tls.cert_monitoring.warning_threshold", "720h")
	v.SetDefault("tls.cert_monitoring.critical_threshold", "168h")
	v.SetDefault("kratos.public_url", "http://127.0.0.1:4433")
	v.SetDefault("kratos.admin_url", "http://127.0.0.1:4434")
	v.SetDefault("kratos.dsn", "")
	v.SetDefault("kratos.smtp.host", "localhost")
	v.SetDefault("kratos.smtp.port", 1025)
	v.SetDefault("kratos.smtp.from", "no-reply@vibewarden.local")
	v.SetDefault("auth.api_key.openbao_path", "")
	v.SetDefault("auth.api_key.cache_ttl", "5m")
	v.SetDefault("auth.mode", "none")
	v.SetDefault("auth.identity_schema", "email_password")
	v.SetDefault("auth.public_paths", []string{})
	v.SetDefault("auth.session_cookie_name", "ory_kratos_session")
	v.SetDefault("auth.login_url", "")
	v.SetDefault("auth.on_kratos_unavailable", "503")
	v.SetDefault("auth.social_providers", []SocialProviderConfig{})
	v.SetDefault("auth.ui.mode", "built-in")
	v.SetDefault("auth.ui.app_name", "")
	v.SetDefault("auth.ui.logo_url", "")
	v.SetDefault("auth.ui.primary_color", "#7C3AED")
	v.SetDefault("auth.ui.background_color", "#1a1a2e")
	v.SetDefault("auth.ui.login_url", "")
	v.SetDefault("auth.ui.registration_url", "")
	v.SetDefault("auth.ui.settings_url", "")
	v.SetDefault("auth.ui.recovery_url", "")
	v.SetDefault("rate_limit.enabled", true)
	v.SetDefault("rate_limit.store", "memory")
	v.SetDefault("rate_limit.redis.url", "")
	v.SetDefault("rate_limit.redis.address", "")
	v.SetDefault("rate_limit.redis.password", "")
	v.SetDefault("rate_limit.redis.db", 0)
	v.SetDefault("rate_limit.redis.pool_size", 0)
	v.SetDefault("rate_limit.redis.key_prefix", "vibewarden")
	v.SetDefault("rate_limit.redis.fallback", true)
	v.SetDefault("rate_limit.redis.health_check_interval", "30s")
	v.SetDefault("rate_limit.per_ip.requests_per_second", 10)
	v.SetDefault("rate_limit.per_ip.burst", 20)
	v.SetDefault("rate_limit.per_user.requests_per_second", 100)
	v.SetDefault("rate_limit.per_user.burst", 200)
	v.SetDefault("rate_limit.trust_proxy_headers", false)
	v.SetDefault("rate_limit.exempt_paths", []string{})
	v.SetDefault("log.level", "info")
	v.SetDefault("log.format", "json")
	v.SetDefault("log.access_log", true)
	v.SetDefault("admin.enabled", false)
	v.SetDefault("admin.token", "")
	v.SetDefault("security_headers.enabled", true)
	v.SetDefault("security_headers.hsts_max_age", 31536000)
	v.SetDefault("security_headers.hsts_include_subdomains", true)
	v.SetDefault("security_headers.hsts_preload", false)
	v.SetDefault("security_headers.content_type_nosniff", true)
	v.SetDefault("security_headers.frame_option", "DENY")
	v.SetDefault("security_headers.content_security_policy", "")
	v.SetDefault("security_headers.referrer_policy", "strict-origin-when-cross-origin")
	v.SetDefault("security_headers.permissions_policy", "")
	v.SetDefault("security_headers.cross_origin_opener_policy", "same-origin")
	v.SetDefault("security_headers.cross_origin_resource_policy", "same-origin")
	v.SetDefault("security_headers.permitted_cross_domain_policies", "none")
	v.SetDefault("security_headers.suppress_via_header", true)
	v.SetDefault("metrics.enabled", true)
	v.SetDefault("metrics.path_patterns", []string{})
	v.SetDefault("telemetry.enabled", true)
	v.SetDefault("telemetry.path_patterns", []string{})
	v.SetDefault("telemetry.prometheus.enabled", true)
	v.SetDefault("telemetry.otlp.enabled", false)
	v.SetDefault("telemetry.otlp.endpoint", "")
	v.SetDefault("telemetry.otlp.headers", map[string]string{})
	v.SetDefault("telemetry.otlp.interval", "30s")
	v.SetDefault("telemetry.otlp.protocol", "http")
	v.SetDefault("telemetry.logs.otlp", false)
	v.SetDefault("body_size.max", "1MB")
	v.SetDefault("body_size.overrides", []BodySizeOverrideConfig{})
	v.SetDefault("ip_filter.enabled", false)
	v.SetDefault("ip_filter.mode", "blocklist")
	v.SetDefault("ip_filter.addresses", []string{})
	v.SetDefault("ip_filter.trust_proxy_headers", false)
	v.SetDefault("database.url", "")
	v.SetDefault("database.external_url", "")
	v.SetDefault("database.tls_mode", "require")
	v.SetDefault("database.pool.max_conns", 10)
	v.SetDefault("database.pool.min_conns", 2)
	v.SetDefault("database.connect_timeout", "10s")
	v.SetDefault("webhooks.endpoints", []WebhookEndpointConfig{})
	v.SetDefault("webhooks.signature_verification.enabled", false)
	v.SetDefault("webhooks.signature_verification.paths", []WebhookSignaturePathConfig{})
	v.SetDefault("secrets.enabled", false)
	v.SetDefault("secrets.store", "builtin")
	v.SetDefault("secrets.provider", "openbao")
	v.SetDefault("secrets.builtin.path", ".vibewarden/secrets.enc")
	v.SetDefault("secrets.builtin.key_file", "")
	v.SetDefault("secrets.openbao.address", "")
	v.SetDefault("secrets.openbao.auth.method", "token")
	v.SetDefault("secrets.openbao.auth.token", "")
	v.SetDefault("secrets.openbao.auth.role_id", "")
	v.SetDefault("secrets.openbao.auth.secret_id", "")
	v.SetDefault("secrets.openbao.mount_path", "secret")
	v.SetDefault("secrets.inject.headers", []SecretsHeaderInjection{})
	v.SetDefault("secrets.inject.env_file", "")
	v.SetDefault("secrets.inject.env", []SecretsEnvInjection{})
	v.SetDefault("secrets.dynamic.postgres.enabled", false)
	v.SetDefault("secrets.dynamic.postgres.roles", []SecretsDynamicRole{})
	v.SetDefault("secrets.cache_ttl", "5m")
	v.SetDefault("secrets.health.check_interval", "6h")
	v.SetDefault("secrets.health.max_static_age", "2160h")
	v.SetDefault("secrets.health.weak_patterns", []string{"password", "changeme", "secret", "123456", "admin", "letmein"})
	v.SetDefault("overrides.kratos_config", "")
	v.SetDefault("overrides.compose_file", "")
	v.SetDefault("overrides.identity_schema", "")
	v.SetDefault("cors.enabled", false)
	v.SetDefault("cors.allowed_origins", []string{})
	v.SetDefault("cors.allowed_methods", []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"})
	v.SetDefault("cors.allowed_headers", []string{"Content-Type", "Authorization"})
	v.SetDefault("cors.exposed_headers", []string{})
	v.SetDefault("cors.allow_credentials", false)
	v.SetDefault("cors.max_age", 0)
	v.SetDefault("resilience.timeout", "30s")
	v.SetDefault("upstream.health.enabled", true)
	v.SetDefault("upstream.health.path", "/health")
	v.SetDefault("upstream.health.interval", "5s")
	v.SetDefault("upstream.health.timeout", "2s")
	v.SetDefault("upstream.health.unhealthy_threshold", 3)
	v.SetDefault("upstream.health.healthy_threshold", 2)
	v.SetDefault("observability.enabled", false)
	v.SetDefault("observability.grafana_port", 3001)
	v.SetDefault("observability.prometheus_port", 9090)
	v.SetDefault("observability.loki_port", 3100)
	v.SetDefault("observability.retention_days", 7)
	v.SetDefault("input_validation.enabled", false)
	v.SetDefault("input_validation.max_url_length", 2048)
	v.SetDefault("input_validation.max_query_string_length", 2048)
	v.SetDefault("input_validation.max_header_count", 100)
	v.SetDefault("input_validation.max_header_size", 8192)
	v.SetDefault("input_validation.path_overrides", []InputValidationPathOverrideConfig{})
	v.SetDefault("waf.content_type_validation.enabled", false)
	v.SetDefault("waf.content_type_validation.allowed", []string{
		"application/json",
		"application/x-www-form-urlencoded",
		"multipart/form-data",
	})
	v.SetDefault("waf.enabled", true)
	v.SetDefault("waf.mode", "detect")
	v.SetDefault("waf.rules.sqli", true)
	v.SetDefault("waf.rules.xss", true)
	v.SetDefault("waf.rules.path_traversal", true)
	v.SetDefault("waf.rules.command_injection", true)
	v.SetDefault("waf.acknowledge_log_mode", false)
	v.SetDefault("egress.enabled", false)
	v.SetDefault("egress.listen", "127.0.0.1:8081")
	v.SetDefault("egress.default_policy", "deny")
	v.SetDefault("egress.default_timeout", "30s")
	v.SetDefault("egress.dns.block_private", true)
	v.SetDefault("egress.dns.allowed_private", []string{})
	v.SetDefault("egress.routes", []EgressRouteConfig{})
	v.SetDefault("error_pages.enabled", false)
	v.SetDefault("error_pages.directory", "")
	v.SetDefault("compression.enabled", true)
	v.SetDefault("compression.algorithms", []string{"zstd", "gzip"})
	v.SetDefault("watch.enabled", true)
	v.SetDefault("watch.debounce", "500ms")
	v.SetDefault("deploy.target_platform", "linux/amd64")
}

// rejectRemovedAuthEnabled returns a load-time error when the user's YAML
// carries the removed auth.enabled key. The check walks viper.AllSettings()
// (the raw unmarshalled YAML map) so it can detect key presence
// independently of whether the caller wrote true or false. The error
// message names the replacement inline — see ADR-065.
func rejectRemovedAuthEnabled(v *viper.Viper) error {
	auth, ok := v.AllSettings()["auth"].(map[string]any)
	if !ok {
		return nil
	}
	if _, present := auth["enabled"]; !present {
		return nil
	}
	return fmt.Errorf("invalid config: %s", authEnabledRemovedMessage)
}

// authEnabledRemovedMessage is the exact text returned when auth.enabled is
// present in a loaded config. The wording is load-bearing — it is the only
// hint a user sees to migrate off the removed key — and is pinned by
// TestLoad_RejectsAuthEnabled.
const authEnabledRemovedMessage = `auth.enabled is no longer a recognised config key (removed in v0.11.0; see ADR-065). Use auth.mode as the single source of truth: set auth.mode: "none" to disable auth, or auth.mode: "kratos" | "jwt" | "api-key" to enable a strategy.

Canonical form:

  auth:
    mode: "none"          # disable auth
    # mode: "kratos"      # enable Ory Kratos session auth
    # mode: "jwt"         # enable JWT / OIDC bearer auth
    # mode: "api-key"     # enable API key header auth`
