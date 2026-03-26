// Package config provides configuration loading and validation for VibeWarden.
package config

import (
	"fmt"
	"strings"

	"github.com/spf13/viper"
)

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
	Auth AuthMiddlewareConfig `mapstructure:"auth"`

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

// KratosConfig holds Ory Kratos connection settings.
type KratosConfig struct {
	// PublicURL is the Kratos public API URL
	PublicURL string `mapstructure:"public_url"`
	// AdminURL is the Kratos admin API URL
	AdminURL string `mapstructure:"admin_url"`
}

// AuthMiddlewareConfig holds auth middleware settings.
// Authentication is enabled automatically when Kratos.PublicURL is non-empty.
type AuthMiddlewareConfig struct {
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
	v.SetDefault("auth.public_paths", []string{})
	v.SetDefault("auth.session_cookie_name", "ory_kratos_session")
	v.SetDefault("auth.login_url", "")
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
	v.SetDefault("metrics.enabled", true)
	v.SetDefault("metrics.path_patterns", []string{})
	v.SetDefault("database.url", "")

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

	return &cfg, nil
}
