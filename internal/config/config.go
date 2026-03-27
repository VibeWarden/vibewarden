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
	// Server configuration
	Server ServerConfig `mapstructure:"server"`

	// Upstream application configuration
	Upstream UpstreamConfig `mapstructure:"upstream"`

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

	// Metrics configuration
	Metrics MetricsConfig `mapstructure:"metrics"`

	// Database configuration
	Database DatabaseConfig `mapstructure:"database"`

	// BodySize configures request body size limits.
	BodySize BodySizeConfig `mapstructure:"body_size"`

	// Overrides provides escape hatches for advanced users who need to supply
	// hand-crafted config files instead of relying on VibeWarden's generation.
	Overrides OverridesConfig `mapstructure:"overrides"`
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

// MetricsConfig holds Prometheus metrics settings.
type MetricsConfig struct {
	// Enabled toggles metrics collection and the /_vibewarden/metrics endpoint (default: true).
	Enabled bool `mapstructure:"enabled"`

	// PathPatterns is a list of URL path normalization patterns using :param syntax.
	// Example: "/users/:id", "/api/v1/items/:item_id/comments/:comment_id"
	// Paths that don't match any pattern are recorded as "other".
	PathPatterns []string `mapstructure:"path_patterns"`
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

// Validate checks the loaded configuration for logical consistency.
// It returns a combined error listing all violations found.
// Call Validate after Load to catch misconfiguration early.
func (c *Config) Validate() error {
	var errs []string

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
	v.SetDefault("body_size.max", "1MB")
	v.SetDefault("body_size.overrides", []BodySizeOverrideConfig{})
	v.SetDefault("database.url", "")
	v.SetDefault("overrides.kratos_config", "")
	v.SetDefault("overrides.compose_file", "")
	v.SetDefault("overrides.identity_schema", "")

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
