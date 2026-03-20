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

	// Rate limiting configuration
	RateLimit RateLimitConfig `mapstructure:"rate_limit"`

	// Logging configuration
	Log LogConfig `mapstructure:"log"`

	// Admin API configuration
	Admin AdminConfig `mapstructure:"admin"`
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
	// Domain for TLS certificate (required if enabled)
	Domain string `mapstructure:"domain"`
	// Provider: "letsencrypt", "self-signed", or "external"
	Provider string `mapstructure:"provider"`
}

// KratosConfig holds Ory Kratos connection settings.
type KratosConfig struct {
	// PublicURL is the Kratos public API URL
	PublicURL string `mapstructure:"public_url"`
	// AdminURL is the Kratos admin API URL
	AdminURL string `mapstructure:"admin_url"`
}

// RateLimitConfig holds rate limiting settings.
type RateLimitConfig struct {
	// Enabled toggles rate limiting (default: true)
	Enabled bool `mapstructure:"enabled"`
	// RequestsPerSecond is the default rate limit (default: 100)
	RequestsPerSecond int `mapstructure:"requests_per_second"`
	// BurstSize is the maximum burst size (default: 50)
	BurstSize int `mapstructure:"burst_size"`
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
	v.SetDefault("rate_limit.enabled", true)
	v.SetDefault("rate_limit.requests_per_second", 100)
	v.SetDefault("rate_limit.burst_size", 50)
	v.SetDefault("log.level", "info")
	v.SetDefault("log.format", "json")
	v.SetDefault("admin.enabled", false)
	v.SetDefault("admin.token", "")

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
