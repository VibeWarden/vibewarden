// Package config provides configuration loading and validation for VibeWarden.
package config

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/spf13/viper"
)

// hexColorRE matches valid CSS hex color values: #RGB or #RRGGBB.
var hexColorRE = regexp.MustCompile(`^#([0-9A-Fa-f]{3}|[0-9A-Fa-f]{6})$`)

// Config holds all configuration for VibeWarden.
// Fields are loaded from vibewarden.yaml and can be overridden by environment variables.
type Config struct {
	// Profile selects the deployment profile: "dev", "tls", or "prod".
	// Affects TLS settings, credential handling, and validation rules.
	// Defaults to "dev".
	Profile string `mapstructure:"profile"`

	// Server configuration
	Server ServerConfig `mapstructure:"server"`

	// Upstream application configuration
	Upstream UpstreamConfig `mapstructure:"upstream"`

	// App configures how the user's application is included in the generated
	// Docker Compose file. When neither App.Build nor App.Image is set, no app
	// service is rendered and the existing host.docker.internal fallback is used.
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
}

// DatabaseConfig holds PostgreSQL connection settings used for audit logging
// and other persistence features.
type DatabaseConfig struct {
	// URL is a libpq-compatible connection string or URL.
	// Example: "postgres://user:pass@localhost:5432/vibewarden?sslmode=disable"
	// Can be set via VIBEWARDEN_DATABASE_URL env var.
	URL string `mapstructure:"url"`
}

// ServerConfig holds server-related settings.
type ServerConfig struct {
	// Host to bind to (default: "127.0.0.1")
	Host string `mapstructure:"host"`
	// Port to listen on (default: 8080)
	Port int `mapstructure:"port"`
}

// UpstreamConfig holds settings for the upstream application being protected.
type UpstreamConfig struct {
	// Host of the upstream application (default: "127.0.0.1")
	Host string `mapstructure:"host"`
	// Port of the upstream application (default: 3000)
	Port int `mapstructure:"port"`
}

// AppConfig configures the user's application in the generated Docker Compose.
// Either Build or Image should be set, depending on whether the user wants
// to build from source (dev) or use a pre-built image (prod).
// When both are set, Build takes precedence (dev-first workflow).
// When neither is set, no app service is rendered and VibeWarden falls back
// to forwarding to host.docker.internal.
type AppConfig struct {
	// Build is the Docker build context path (e.g., "." for the current directory).
	// Used in dev/tls profiles. When set, the app service is rendered with
	// a build: context directive.
	Build string `mapstructure:"build"`

	// Image is the Docker image reference (e.g., "ghcr.io/org/myapp:latest").
	// Used in prod profile. Can be overridden at runtime via the
	// VIBEWARDEN_APP_IMAGE environment variable.
	Image string `mapstructure:"image"`
}

// TLSConfig holds TLS-related settings.
type TLSConfig struct {
	// Enabled toggles TLS (default: false for local dev)
	Enabled bool `mapstructure:"enabled"`
	// Domain for TLS certificate (required if enabled with provider "letsencrypt")
	Domain string `mapstructure:"domain"`
	// Provider: "letsencrypt", "self-signed", or "external"
	Provider string `mapstructure:"provider"`
	// CertPath is the path to a PEM-encoded certificate file.
	// Required when Provider is "external".
	CertPath string `mapstructure:"cert_path"`
	// KeyPath is the path to a PEM-encoded private key file.
	// Required when Provider is "external".
	KeyPath string `mapstructure:"key_path"`
	// StoragePath is the directory where Caddy stores ACME certificates.
	// Only applies when Provider is "letsencrypt".
	StoragePath string `mapstructure:"storage_path"`
}

// KratosSMTPConfig holds SMTP settings used by Ory Kratos to send emails.
type KratosSMTPConfig struct {
	// Host is the SMTP server hostname (default: "localhost").
	Host string `mapstructure:"host"`
	// Port is the SMTP server port (default: 1025).
	Port int `mapstructure:"port"`
	// From is the sender address for Kratos emails (default: "no-reply@vibewarden.local").
	From string `mapstructure:"from"`
}

// KratosConfig holds Ory Kratos connection settings.
// These values are used both for the auth middleware and for generating
// the Kratos config file under .vibewarden/generated/.
type KratosConfig struct {
	// PublicURL is the Kratos public API URL (default: "http://127.0.0.1:4433")
	PublicURL string `mapstructure:"public_url"`
	// AdminURL is the Kratos admin API URL (default: "http://127.0.0.1:4434")
	AdminURL string `mapstructure:"admin_url"`
	// DSN is the data source name for the Kratos database.
	// Example: "postgres://kratos:secret@localhost:5432/kratos?sslmode=disable"
	DSN string `mapstructure:"dsn"`
	// SMTP holds email delivery settings for Kratos.
	SMTP KratosSMTPConfig `mapstructure:"smtp"`
}

// SupportedSocialProviders is the set of accepted provider names for social login.
// The special value "oidc" indicates a generic OpenID Connect provider.
var SupportedSocialProviders = map[string]bool{
	"google":    true,
	"github":    true,
	"apple":     true,
	"facebook":  true,
	"microsoft": true,
	"gitlab":    true,
	"discord":   true,
	"slack":     true,
	"spotify":   true,
	"oidc":      true,
}

// SocialProviderConfig holds OAuth2/OIDC settings for a single social login provider.
// It is used as an element of AuthConfig.SocialProviders.
type SocialProviderConfig struct {
	// Provider is the provider name.
	// Accepted values: google, github, apple, facebook, microsoft, gitlab, discord, slack, spotify, oidc.
	Provider string `mapstructure:"provider"`

	// ClientID is the OAuth2 client ID issued by the provider. Required.
	ClientID string `mapstructure:"client_id"`

	// ClientSecret is the OAuth2 client secret issued by the provider. Required.
	// Supports environment variable substitution via ${VAR} syntax in the YAML file.
	ClientSecret string `mapstructure:"client_secret"`

	// Scopes is an optional list of OAuth2 scopes to request.
	// When empty, provider-specific defaults are used.
	Scopes []string `mapstructure:"scopes"`

	// Label is an optional custom label shown on the login button (e.g. "Sign in with Acme").
	Label string `mapstructure:"label"`

	// TeamID is the Apple Developer Team ID. Required when Provider is "apple".
	TeamID string `mapstructure:"team_id"`

	// KeyID is the Apple private key ID. Required when Provider is "apple".
	KeyID string `mapstructure:"key_id"`

	// ID is the unique identifier for the OIDC provider entry (e.g. "acme-oidc").
	// Required when Provider is "oidc".
	ID string `mapstructure:"id"`

	// IssuerURL is the OIDC issuer URL (e.g. "https://accounts.google.com").
	// Required when Provider is "oidc".
	IssuerURL string `mapstructure:"issuer_url"`
}

// AuthUIConfig holds theme and URL settings for the built-in authentication UI.
// It configures the visual appearance of the login, registration, recovery, and
// settings pages rendered by VibeWarden, as well as the optional custom URL
// overrides used when mode is "custom".
type AuthUIConfig struct {
	// Mode selects whether VibeWarden renders its own auth pages or defers to
	// custom URLs. Accepted values: "built-in" (default) or "custom".
	Mode string `mapstructure:"mode"`

	// AppName is the application name shown on the built-in login page.
	AppName string `mapstructure:"app_name"`

	// LogoURL is an optional URL to a logo image displayed on built-in pages.
	LogoURL string `mapstructure:"logo_url"`

	// PrimaryColor is the accent color used on built-in pages (hex, default: "#7C3AED").
	PrimaryColor string `mapstructure:"primary_color"`

	// BackgroundColor is the page background color for built-in pages (hex, default: "#1a1a2e").
	BackgroundColor string `mapstructure:"background_color"`

	// LoginURL is the URL of the custom login page.
	// Required when Mode is "custom".
	LoginURL string `mapstructure:"login_url"`

	// RegistrationURL is the URL of the custom registration page.
	// Only used when Mode is "custom".
	RegistrationURL string `mapstructure:"registration_url"`

	// SettingsURL is the URL of the custom account settings page.
	// Only used when Mode is "custom".
	SettingsURL string `mapstructure:"settings_url"`

	// RecoveryURL is the URL of the custom account recovery page.
	// Only used when Mode is "custom".
	RecoveryURL string `mapstructure:"recovery_url"`
}

// AuthConfig holds auth middleware settings.
// Authentication is enabled automatically when Kratos.PublicURL is non-empty.
type AuthConfig struct {
	// Enabled toggles the authentication middleware (default: false).
	// When true, all requests must present a valid Kratos session cookie unless
	// the path matches one of the PublicPaths patterns.
	Enabled bool `mapstructure:"enabled"`

	// IdentitySchema selects the identity schema to use.
	// Accepted values: "email_password" (default), "email_only", "username_password",
	// "social", or a filesystem path to a custom JSON schema file.
	// When social_providers are configured and this field is left at its default
	// ("email_password"), the generate service automatically upgrades to the
	// "social" schema so that name and picture traits are available.
	IdentitySchema string `mapstructure:"identity_schema"`

	// PublicPaths is a list of URL path glob patterns that bypass auth.
	// The /_vibewarden/* prefix is always public (added automatically).
	// Supports * for single-segment wildcards (e.g. "/static/*").
	PublicPaths []string `mapstructure:"public_paths"`

	// SessionCookieName is the name of the Kratos session cookie.
	// Defaults to "ory_kratos_session".
	SessionCookieName string `mapstructure:"session_cookie_name"`

	// LoginURL is the redirect destination for unauthenticated users.
	// Defaults to "/self-service/login/browser" when empty.
	LoginURL string `mapstructure:"login_url"`

	// OnKratosUnavailable controls behavior when Kratos cannot be reached.
	// Accepted values:
	//   "503"          (default) — return 503 for all protected requests (fail-closed).
	//   "allow_public" — serve requests to public paths; block protected paths with 503.
	OnKratosUnavailable string `mapstructure:"on_kratos_unavailable"`

	// SocialProviders is a list of OAuth2/OIDC social login providers to enable.
	// Each entry requires at minimum a provider name, client_id, and client_secret.
	SocialProviders []SocialProviderConfig `mapstructure:"social_providers"`

	// UI holds theme and URL settings for the built-in or custom auth pages.
	UI AuthUIConfig `mapstructure:"ui"`
}

// RateLimitConfig holds rate limiting settings.
type RateLimitConfig struct {
	// Enabled toggles rate limiting (default: true)
	Enabled bool `mapstructure:"enabled"`

	// Store selects the backing store for limiter state.
	// Accepted values: "memory" (default), "redis".
	// "memory" uses a per-process token bucket — no external dependencies.
	// "redis" uses a Redis-backed distributed token bucket.
	Store string `mapstructure:"store"`

	// Redis holds connection settings for the Redis store.
	// Only used when Store is "redis".
	Redis RateLimitRedisConfig `mapstructure:"redis"`

	// PerIP configures per-IP rate limits applied to all requests.
	PerIP RateLimitRuleConfig `mapstructure:"per_ip"`

	// PerUser configures per-user rate limits applied to authenticated requests only.
	PerUser RateLimitRuleConfig `mapstructure:"per_user"`

	// TrustProxyHeaders enables reading X-Forwarded-For to determine the real client IP.
	// Only enable when VibeWarden is behind a trusted reverse proxy.
	TrustProxyHeaders bool `mapstructure:"trust_proxy_headers"`

	// ExemptPaths is a list of glob patterns for paths that bypass rate limiting.
	// The /_vibewarden/* prefix is always exempt and is added automatically.
	ExemptPaths []string `mapstructure:"exempt_paths"`
}

// RateLimitRedisConfig holds Redis connection settings for the rate limit store.
type RateLimitRedisConfig struct {
	// Address is the Redis server address in host:port form (default: "localhost:6379").
	// Required when rate_limit.store is "redis".
	Address string `mapstructure:"address"`

	// Password is the Redis AUTH password (default: empty, no auth).
	Password string `mapstructure:"password"`

	// DB is the Redis logical database index (default: 0).
	DB int `mapstructure:"db"`

	// KeyPrefix is the namespace prefix prepended to every Redis key
	// (default: "vibewarden").
	KeyPrefix string `mapstructure:"key_prefix"`

	// Fallback controls whether the rate limiter falls back to the in-memory
	// store when Redis is unavailable (default: true — fail-open).
	// Set to false to enable fail-closed mode: requests are denied when
	// Redis is unreachable.
	Fallback bool `mapstructure:"fallback"`

	// HealthCheckInterval is how often the background goroutine probes Redis
	// for recovery after a failure, expressed as a duration string (e.g. "30s").
	// Default: "30s".
	HealthCheckInterval string `mapstructure:"health_check_interval"`
}

// RateLimitRuleConfig holds the sustained rate and burst size for a rate limit.
type RateLimitRuleConfig struct {
	// RequestsPerSecond is the sustained request rate (default: 10 for IP, 100 for user).
	RequestsPerSecond float64 `mapstructure:"requests_per_second"`

	// Burst is the maximum number of requests allowed in a burst
	// above the sustained rate (default: 20 for IP, 200 for user).
	Burst int `mapstructure:"burst"`
}

// LogConfig holds logging settings.
type LogConfig struct {
	// Level: "debug", "info", "warn", "error" (default: "info")
	Level string `mapstructure:"level"`
	// Format: "json" or "text" (default: "json")
	Format string `mapstructure:"format"`
}

// AdminConfig holds admin API settings.
type AdminConfig struct {
	// Enabled toggles the admin API (default: false)
	Enabled bool `mapstructure:"enabled"`
	// Token is the bearer token for admin API authentication.
	// Can be set via VIBEWARDEN_ADMIN_TOKEN env var.
	Token string `mapstructure:"token"`
}

// SecurityHeadersConfig holds security header settings.
type SecurityHeadersConfig struct {
	// Enabled toggles security headers middleware (default: true)
	Enabled bool `mapstructure:"enabled"`

	// HSTSMaxAge is the Strict-Transport-Security max-age in seconds (default: 31536000 = 1 year)
	HSTSMaxAge int `mapstructure:"hsts_max_age"`
	// HSTSIncludeSubDomains includes the includeSubDomains directive (default: true)
	HSTSIncludeSubDomains bool `mapstructure:"hsts_include_subdomains"`
	// HSTSPreload includes the preload directive (default: false — requires manual submission)
	HSTSPreload bool `mapstructure:"hsts_preload"`

	// ContentTypeNosniff sets X-Content-Type-Options: nosniff (default: true)
	ContentTypeNosniff bool `mapstructure:"content_type_nosniff"`

	// FrameOption sets X-Frame-Options value: "DENY", "SAMEORIGIN", or "" to disable (default: "DENY")
	FrameOption string `mapstructure:"frame_option"`

	// ContentSecurityPolicy sets Content-Security-Policy value (default: "default-src 'self'")
	ContentSecurityPolicy string `mapstructure:"content_security_policy"`

	// ReferrerPolicy sets Referrer-Policy value (default: "strict-origin-when-cross-origin")
	ReferrerPolicy string `mapstructure:"referrer_policy"`

	// PermissionsPolicy sets Permissions-Policy value (default: "")
	PermissionsPolicy string `mapstructure:"permissions_policy"`

	// CrossOriginOpenerPolicy sets Cross-Origin-Opener-Policy value (default: "same-origin")
	CrossOriginOpenerPolicy string `mapstructure:"cross_origin_opener_policy"`

	// CrossOriginResourcePolicy sets Cross-Origin-Resource-Policy value (default: "same-origin")
	CrossOriginResourcePolicy string `mapstructure:"cross_origin_resource_policy"`

	// PermittedCrossDomainPolicies sets X-Permitted-Cross-Domain-Policies value (default: "none")
	PermittedCrossDomainPolicies string `mapstructure:"permitted_cross_domain_policies"`

	// SuppressViaHeader removes the Via header from proxied responses (default: true)
	SuppressViaHeader bool `mapstructure:"suppress_via_header"`
}

// CORSConfig holds Cross-Origin Resource Sharing settings.
type CORSConfig struct {
	// Enabled toggles the CORS plugin (default: false).
	Enabled bool `mapstructure:"enabled"`

	// AllowedOrigins is the list of origins permitted to make cross-origin
	// requests. Use ["*"] to allow all origins (development only).
	AllowedOrigins []string `mapstructure:"allowed_origins"`

	// AllowedMethods is the list of HTTP methods permitted in cross-origin
	// requests (default: GET, POST, PUT, DELETE, OPTIONS).
	AllowedMethods []string `mapstructure:"allowed_methods"`

	// AllowedHeaders is the list of request headers permitted in cross-origin
	// requests (default: Content-Type, Authorization).
	AllowedHeaders []string `mapstructure:"allowed_headers"`

	// ExposedHeaders is the list of response headers exposed to the browser
	// via Access-Control-Expose-Headers (default: []).
	ExposedHeaders []string `mapstructure:"exposed_headers"`

	// AllowCredentials, when true, sets Access-Control-Allow-Credentials: true.
	// Must not be combined with AllowedOrigins: ["*"] (default: false).
	AllowCredentials bool `mapstructure:"allow_credentials"`

	// MaxAge is the number of seconds the browser may cache the preflight
	// response (Access-Control-Max-Age). Zero omits the header (default: 0).
	MaxAge int `mapstructure:"max_age"`
}

// MetricsConfig holds Prometheus metrics settings.
//
// Deprecated: Use TelemetryConfig instead. MetricsConfig remains only for backward
// compatibility. Settings are migrated to TelemetryConfig at startup via MigrateLegacyMetrics.
type MetricsConfig struct {
	// Enabled toggles metrics collection and the /_vibewarden/metrics endpoint (default: true).
	Enabled bool `mapstructure:"enabled"`

	// PathPatterns is a list of URL path normalization patterns using :param syntax.
	// Example: "/users/:id", "/api/v1/items/:item_id/comments/:comment_id"
	// Paths that don't match any pattern are recorded as "other".
	PathPatterns []string `mapstructure:"path_patterns"`
}

// TelemetryConfig holds all telemetry export settings.
// This replaces the narrower MetricsConfig and supports both pull (Prometheus)
// and push (OTLP) export modes.
//
// Prometheus is the automatic fallback: when no telemetry block is present in
// vibewarden.yaml, prometheus.enabled defaults to true and otlp.enabled defaults
// to false. This means /_vibewarden/metrics always works out of the box, with
// zero configuration required. Existing Prometheus scrapers and Grafana dashboards
// continue to work without any changes.
//
// To add OTLP push export, set otlp.enabled = true and provide an endpoint.
// Both exporters can run simultaneously.
type TelemetryConfig struct {
	// Enabled toggles telemetry collection entirely (default: true).
	Enabled bool `mapstructure:"enabled"`

	// PathPatterns is a list of URL path normalization patterns using :param syntax.
	// Example: "/users/:id", "/api/v1/items/:item_id/comments/:comment_id"
	// Paths that don't match any pattern are recorded as "other".
	PathPatterns []string `mapstructure:"path_patterns"`

	// Prometheus configures the pull-based Prometheus exporter.
	Prometheus PrometheusExporterConfig `mapstructure:"prometheus"`

	// OTLP configures the push-based OTLP exporter.
	OTLP OTLPExporterConfig `mapstructure:"otlp"`

	// Logs configures structured event log export settings.
	Logs LogsConfig `mapstructure:"logs"`

	// Traces configures distributed tracing settings.
	Traces TracesConfig `mapstructure:"traces"`
}

// TracesConfig holds distributed tracing settings.
type TracesConfig struct {
	// Enabled toggles distributed tracing (default: false).
	// When enabled, a span is created for each HTTP request and exported via OTLP.
	// Requires telemetry.otlp.enabled to be true.
	Enabled bool `mapstructure:"enabled"`
}

// LogsConfig holds log export settings.
type LogsConfig struct {
	// OTLP toggles OTLP log export (default: false).
	// When enabled, structured events are exported to the same OTLP endpoint as metrics.
	// Requires telemetry.otlp.endpoint to be configured.
	OTLP bool `mapstructure:"otlp"`
}

// PrometheusExporterConfig configures the Prometheus pull-based exporter.
type PrometheusExporterConfig struct {
	// Enabled toggles the Prometheus exporter (default: true).
	// When enabled, metrics are served at /_vibewarden/metrics.
	Enabled bool `mapstructure:"enabled"`
}

// OTLPExporterConfig configures the OTLP push-based exporter.
type OTLPExporterConfig struct {
	// Enabled toggles the OTLP exporter (default: false).
	Enabled bool `mapstructure:"enabled"`

	// Endpoint is the OTLP HTTP endpoint URL (e.g., "http://localhost:4318").
	// Required when Enabled is true.
	Endpoint string `mapstructure:"endpoint"`

	// Headers are optional HTTP headers for authentication.
	// Example: {"Authorization": "Bearer <token>"}
	Headers map[string]string `mapstructure:"headers"`

	// Interval is the export interval as a duration string (default: "30s").
	// Metrics are batched and pushed at this interval.
	Interval string `mapstructure:"interval"`

	// Protocol is "http" or "grpc" (default: "http").
	// Only "http" is supported in this version.
	Protocol string `mapstructure:"protocol"`
}

// ResilienceConfig holds resilience settings for upstream connections.
type ResilienceConfig struct {
	// Timeout is the maximum time to wait for the upstream application to
	// respond, expressed as a duration string (e.g. "30s", "1m").
	// A value of "0" or "" disables the timeout (no limit).
	// Default: "30s".
	Timeout string `mapstructure:"timeout"`

	// CircuitBreaker configures the circuit breaker middleware.
	CircuitBreaker CircuitBreakerConfig `mapstructure:"circuit_breaker"`

	// Retry configures the retry-with-exponential-backoff middleware.
	Retry RetryConfig `mapstructure:"retry"`
}

// RetryConfig holds retry-with-exponential-backoff settings.
type RetryConfig struct {
	// Enabled toggles the retry middleware (default: false).
	Enabled bool `mapstructure:"enabled"`

	// MaxAttempts is the total number of attempts including the initial request.
	// Must be >= 2 when Enabled is true. Default: 3.
	MaxAttempts int `mapstructure:"max_attempts"`

	// InitialBackoff is the wait before the first retry, as a duration string
	// (e.g. "100ms", "500ms"). Default: "100ms".
	InitialBackoff string `mapstructure:"backoff"`

	// MaxBackoff is the upper bound on the computed backoff, as a duration string
	// (e.g. "10s"). Default: "10s".
	MaxBackoff string `mapstructure:"max_backoff"`

	// RetryOn is the list of HTTP status codes that trigger a retry.
	// Default: [502, 503, 504].
	RetryOn []int `mapstructure:"retry_on"`
}

// CircuitBreakerConfig holds circuit breaker settings.
type CircuitBreakerConfig struct {
	// Enabled toggles the circuit breaker middleware.
	Enabled bool `mapstructure:"enabled"`

	// Threshold is the number of consecutive failures required to trip the
	// circuit from Closed to Open. Must be > 0 when Enabled is true.
	// Default: 5.
	Threshold int `mapstructure:"threshold"`

	// Timeout is how long the circuit stays Open before transitioning to
	// HalfOpen to allow a probe request, expressed as a duration string
	// (e.g. "60s", "1m"). Must be > 0 when Enabled is true.
	// Default: "60s".
	Timeout string `mapstructure:"timeout"`
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

// WebhooksConfig holds all webhook delivery settings.
// It maps to the webhooks section of vibewarden.yaml.
type WebhooksConfig struct {
	// Endpoints is the list of webhook endpoints to deliver events to.
	Endpoints []WebhookEndpointConfig `mapstructure:"endpoints"`
}

// WebhookEndpointConfig holds the settings for a single webhook endpoint.
type WebhookEndpointConfig struct {
	// URL is the HTTP(S) endpoint to POST events to. Required.
	URL string `mapstructure:"url"`

	// Events is the list of event types to deliver to this endpoint.
	// Use "*" to subscribe to all events.
	Events []string `mapstructure:"events"`

	// Format selects the payload format: "raw" (default), "slack", or "discord".
	Format string `mapstructure:"format"`

	// TimeoutSeconds is the per-request HTTP timeout in seconds (default: 10).
	TimeoutSeconds int `mapstructure:"timeout_seconds"`
}

// SecretsConfig holds all settings for the secret management plugin.
// It maps to the secrets section of vibewarden.yaml.
type SecretsConfig struct {
	// Enabled toggles the secrets plugin (default: false).
	Enabled bool `mapstructure:"enabled"`

	// Provider selects the secret store backend (default: "openbao").
	Provider string `mapstructure:"provider"`

	// OpenBao holds connection and authentication settings for the OpenBao server.
	OpenBao SecretsOpenBaoConfig `mapstructure:"openbao"`

	// Inject defines how fetched secrets are surfaced to the upstream app.
	Inject SecretsInjectConfig `mapstructure:"inject"`

	// Dynamic holds settings for dynamic credential generation.
	Dynamic SecretsDynamicConfig `mapstructure:"dynamic"`

	// Health holds settings for the secret health check subsystem.
	Health SecretsHealthConfig `mapstructure:"health"`

	// CacheTTL is how long fetched secrets are held in memory (default: "5m").
	CacheTTL string `mapstructure:"cache_ttl"`
}

// SecretsOpenBaoConfig holds connection settings for the OpenBao server.
type SecretsOpenBaoConfig struct {
	// Address is the OpenBao server URL (e.g. "http://openbao:8200").
	Address string `mapstructure:"address"`

	// Auth holds the authentication credentials.
	Auth SecretsOpenBaoAuthConfig `mapstructure:"auth"`

	// MountPath is the KV v2 mount path (default: "secret").
	MountPath string `mapstructure:"mount_path"`
}

// SecretsOpenBaoAuthConfig holds authentication credentials for OpenBao.
type SecretsOpenBaoAuthConfig struct {
	// Method is the auth method: "token" or "approle".
	Method string `mapstructure:"method"`

	// Token is the static token. Used when Method is "token".
	Token string `mapstructure:"token"`

	// RoleID is the AppRole role_id. Used when Method is "approle".
	RoleID string `mapstructure:"role_id"`

	// SecretID is the AppRole secret_id. Used when Method is "approle".
	SecretID string `mapstructure:"secret_id"`
}

// SecretsInjectConfig defines how secrets are injected into proxied requests.
type SecretsInjectConfig struct {
	// Headers is the list of secrets to inject as HTTP request headers.
	Headers []SecretsHeaderInjection `mapstructure:"headers"`

	// EnvFile is the path to write a .env file containing secret values.
	EnvFile string `mapstructure:"env_file"`

	// Env is the list of secrets to write into the env file.
	Env []SecretsEnvInjection `mapstructure:"env"`
}

// SecretsHeaderInjection maps a secret key to an HTTP request header name.
type SecretsHeaderInjection struct {
	// SecretPath is the KV path of the secret.
	SecretPath string `mapstructure:"secret_path"`

	// SecretKey is the key within the secret map.
	SecretKey string `mapstructure:"secret_key"`

	// Header is the HTTP header name to set.
	Header string `mapstructure:"header"`
}

// SecretsEnvInjection maps a secret key to an environment variable name.
type SecretsEnvInjection struct {
	// SecretPath is the KV path of the secret.
	SecretPath string `mapstructure:"secret_path"`

	// SecretKey is the key within the secret map.
	SecretKey string `mapstructure:"secret_key"`

	// EnvVar is the environment variable name to write in the env file.
	EnvVar string `mapstructure:"env_var"`
}

// SecretsDynamicConfig holds settings for dynamic credential generation.
type SecretsDynamicConfig struct {
	// Postgres configures the OpenBao database engine for Postgres dynamic creds.
	Postgres SecretsDynamicPostgresConfig `mapstructure:"postgres"`
}

// SecretsDynamicPostgresConfig holds settings for dynamic Postgres credential generation.
type SecretsDynamicPostgresConfig struct {
	// Enabled toggles dynamic Postgres credential generation.
	Enabled bool `mapstructure:"enabled"`

	// Roles is the list of OpenBao database roles to request credentials for.
	Roles []SecretsDynamicRole `mapstructure:"roles"`
}

// SecretsDynamicRole defines a single OpenBao database role and where to inject its credentials.
type SecretsDynamicRole struct {
	// Name is the OpenBao database role name.
	Name string `mapstructure:"name"`

	// EnvVarUser is the env var name to write the generated username into.
	EnvVarUser string `mapstructure:"env_var_user"`

	// EnvVarPassword is the env var name to write the generated password into.
	EnvVarPassword string `mapstructure:"env_var_password"`
}

// SecretsHealthConfig holds settings for the secret health check subsystem.
type SecretsHealthConfig struct {
	// CheckInterval is how often to run health checks (default: "6h").
	CheckInterval string `mapstructure:"check_interval"`

	// MaxStaticAge is the maximum acceptable age of a static secret (default: "2160h").
	MaxStaticAge string `mapstructure:"max_static_age"`

	// WeakPatterns is the list of substrings that indicate a weak/default secret.
	WeakPatterns []string `mapstructure:"weak_patterns"`
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

// ObservabilityConfig holds settings for the optional observability stack.
// When enabled, vibewarden generate produces Prometheus, Grafana, Loki, and
// Promtail configs under .vibewarden/generated/observability/.
type ObservabilityConfig struct {
	// Enabled toggles generation of the observability stack (default: false).
	Enabled bool `mapstructure:"enabled"`

	// GrafanaPort is the host port Grafana binds to (default: 3001).
	// This avoids conflict with common app ports like 3000.
	GrafanaPort int `mapstructure:"grafana_port"`

	// PrometheusPort is the host port Prometheus binds to (default: 9090).
	PrometheusPort int `mapstructure:"prometheus_port"`

	// LokiPort is the host port Loki binds to (default: 3100).
	LokiPort int `mapstructure:"loki_port"`

	// RetentionDays is how long Loki retains log data (default: 7).
	RetentionDays int `mapstructure:"retention_days"`
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

	// TLS external provider requires cert_path and key_path.
	if c.TLS.Enabled && c.TLS.Provider == "external" {
		if c.TLS.CertPath == "" {
			errs = append(errs, "tls.cert_path is required when tls.provider is \"external\"")
		}
		if c.TLS.KeyPath == "" {
			errs = append(errs, "tls.key_path is required when tls.provider is \"external\"")
		}
	}

	// Social providers: validate each entry.
	for i, sp := range c.Auth.SocialProviders {
		prefix := fmt.Sprintf("social_providers[%d]", i)

		if !SupportedSocialProviders[sp.Provider] {
			errs = append(errs, fmt.Sprintf("%s.provider %q is not supported; accepted values: google, github, apple, facebook, microsoft, gitlab, discord, slack, spotify, oidc", prefix, sp.Provider))
		}
		if sp.ClientID == "" {
			errs = append(errs, fmt.Sprintf("%s.client_id is required", prefix))
		}
		if sp.ClientSecret == "" {
			errs = append(errs, fmt.Sprintf("%s.client_secret is required", prefix))
		}
		if sp.Provider == "apple" {
			if sp.TeamID == "" {
				errs = append(errs, fmt.Sprintf("%s.team_id is required for provider \"apple\"", prefix))
			}
			if sp.KeyID == "" {
				errs = append(errs, fmt.Sprintf("%s.key_id is required for provider \"apple\"", prefix))
			}
		}
		if sp.Provider == "oidc" {
			if sp.ID == "" {
				errs = append(errs, fmt.Sprintf("%s.id is required for provider \"oidc\"", prefix))
			}
			if sp.IssuerURL == "" {
				errs = append(errs, fmt.Sprintf("%s.issuer_url is required for provider \"oidc\"", prefix))
			}
		}
	}

	// auth.on_kratos_unavailable validation.
	if c.Auth.OnKratosUnavailable != "" &&
		c.Auth.OnKratosUnavailable != "503" &&
		c.Auth.OnKratosUnavailable != "allow_public" {
		errs = append(errs, fmt.Sprintf(
			"auth.on_kratos_unavailable %q is invalid; accepted values: \"503\", \"allow_public\"",
			c.Auth.OnKratosUnavailable,
		))
	}

	// Auth UI validation.
	ui := c.Auth.UI
	if ui.Mode != "" && ui.Mode != "built-in" && ui.Mode != "custom" {
		errs = append(errs, fmt.Sprintf("auth.ui.mode %q is invalid; accepted values: \"built-in\", \"custom\"", ui.Mode))
	}
	if ui.PrimaryColor != "" && !hexColorRE.MatchString(ui.PrimaryColor) {
		errs = append(errs, fmt.Sprintf("auth.ui.primary_color %q is not a valid hex color (expected #RGB or #RRGGBB)", ui.PrimaryColor))
	}
	if ui.BackgroundColor != "" && !hexColorRE.MatchString(ui.BackgroundColor) {
		errs = append(errs, fmt.Sprintf("auth.ui.background_color %q is not a valid hex color (expected #RGB or #RRGGBB)", ui.BackgroundColor))
	}
	if ui.Mode == "custom" && ui.LoginURL == "" {
		errs = append(errs, "auth.ui.login_url is required when auth.ui.mode is \"custom\"")
	}

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

	// webhooks.endpoints validation.
	validFormats := map[string]bool{"": true, "raw": true, "slack": true, "discord": true}
	for i, ep := range c.Webhooks.Endpoints {
		prefix := fmt.Sprintf("webhooks.endpoints[%d]", i)
		if ep.URL == "" {
			errs = append(errs, fmt.Sprintf("%s.url is required", prefix))
		}
		if len(ep.Events) == 0 {
			errs = append(errs, fmt.Sprintf("%s.events must have at least one entry", prefix))
		}
		if !validFormats[ep.Format] {
			errs = append(errs, fmt.Sprintf("%s.format %q is invalid; accepted values: \"raw\", \"slack\", \"discord\"", prefix, ep.Format))
		}
		if ep.TimeoutSeconds < 0 {
			errs = append(errs, fmt.Sprintf("%s.timeout_seconds must be >= 0", prefix))
		}
	}

	// rate_limit.store validation.
	switch c.RateLimit.Store {
	case "", "memory":
		// valid — "memory" is the default
	case "redis":
		if c.RateLimit.Redis.Address == "" {
			errs = append(errs, "rate_limit.redis.address is required when rate_limit.store is \"redis\"")
		}
	default:
		errs = append(errs, fmt.Sprintf(
			"rate_limit.store %q is invalid; accepted values: \"memory\", \"redis\"",
			c.RateLimit.Store,
		))
	}

	// telemetry.logs.otlp requires telemetry.otlp.endpoint.
	if c.Telemetry.Logs.OTLP && c.Telemetry.OTLP.Endpoint == "" {
		errs = append(errs, "telemetry.logs.otlp requires telemetry.otlp.endpoint")
	}

	// cors validation.
	if c.CORS.Enabled && c.CORS.AllowCredentials {
		for _, o := range c.CORS.AllowedOrigins {
			if o == "*" {
				errs = append(errs, "cors.allow_credentials: true cannot be combined with cors.allowed_origins: [\"*\"]; browsers reject credentialed requests to wildcard origins")
				break
			}
		}
	}

	// observability validation.
	if c.Observability.Enabled {
		if c.Observability.GrafanaPort <= 0 || c.Observability.GrafanaPort > 65535 {
			errs = append(errs, fmt.Sprintf(
				"observability.grafana_port %d is invalid; must be 1-65535",
				c.Observability.GrafanaPort,
			))
		}
		if c.Observability.PrometheusPort <= 0 || c.Observability.PrometheusPort > 65535 {
			errs = append(errs, fmt.Sprintf(
				"observability.prometheus_port %d is invalid; must be 1-65535",
				c.Observability.PrometheusPort,
			))
		}
		if c.Observability.LokiPort <= 0 || c.Observability.LokiPort > 65535 {
			errs = append(errs, fmt.Sprintf(
				"observability.loki_port %d is invalid; must be 1-65535",
				c.Observability.LokiPort,
			))
		}
		if c.Observability.RetentionDays <= 0 {
			errs = append(errs, fmt.Sprintf(
				"observability.retention_days %d is invalid; must be > 0",
				c.Observability.RetentionDays,
			))
		}
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
	v := viper.New()

	// Set defaults
	v.SetDefault("profile", "dev")
	v.SetDefault("server.host", "127.0.0.1")
	v.SetDefault("server.port", 8080)
	v.SetDefault("upstream.host", "127.0.0.1")
	v.SetDefault("upstream.port", 3000)
	v.SetDefault("tls.enabled", false)
	v.SetDefault("tls.provider", "self-signed")
	v.SetDefault("kratos.public_url", "http://127.0.0.1:4433")
	v.SetDefault("kratos.admin_url", "http://127.0.0.1:4434")
	v.SetDefault("kratos.dsn", "")
	v.SetDefault("kratos.smtp.host", "localhost")
	v.SetDefault("kratos.smtp.port", 1025)
	v.SetDefault("kratos.smtp.from", "no-reply@vibewarden.local")
	v.SetDefault("auth.enabled", false)
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
	v.SetDefault("rate_limit.redis.address", "")
	v.SetDefault("rate_limit.redis.password", "")
	v.SetDefault("rate_limit.redis.db", 0)
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
	v.SetDefault("admin.enabled", false)
	v.SetDefault("admin.token", "")
	v.SetDefault("security_headers.enabled", true)
	v.SetDefault("security_headers.hsts_max_age", 31536000)
	v.SetDefault("security_headers.hsts_include_subdomains", true)
	v.SetDefault("security_headers.hsts_preload", false)
	v.SetDefault("security_headers.content_type_nosniff", true)
	v.SetDefault("security_headers.frame_option", "DENY")
	v.SetDefault("security_headers.content_security_policy", "default-src 'self'")
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
	v.SetDefault("webhooks.endpoints", []WebhookEndpointConfig{})
	v.SetDefault("secrets.enabled", false)
	v.SetDefault("secrets.provider", "openbao")
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
	v.SetDefault("observability.enabled", false)
	v.SetDefault("observability.grafana_port", 3001)
	v.SetDefault("observability.prometheus_port", 9090)
	v.SetDefault("observability.loki_port", 3100)
	v.SetDefault("observability.retention_days", 7)

	// Config file
	if configPath != "" {
		v.SetConfigFile(configPath)
	} else {
		v.SetConfigName("vibewarden")
		v.SetConfigType("yaml")
		v.AddConfigPath(".")
		v.AddConfigPath("/etc/vibewarden")
	}

	// Environment variables
	v.SetEnvPrefix("VIBEWARDEN")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	// Read config file (ignore "not found" error — env vars may be sufficient)
	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, fmt.Errorf("reading config file: %w", err)
		}
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("unmarshaling config: %w", err)
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	return &cfg, nil
}
