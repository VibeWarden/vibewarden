package config

import (
	"fmt"
	"net/url"
	"strings"
	"time"
)

// DatabasePoolConfig holds connection pool settings for PostgreSQL.
type DatabasePoolConfig struct {
	// MaxConns is the maximum number of open connections in the pool.
	// Default: 10.
	MaxConns int `mapstructure:"max_conns"`

	// MinConns is the minimum number of idle connections kept open.
	// Default: 2.
	MinConns int `mapstructure:"min_conns"`
}

// DatabaseConfig holds PostgreSQL connection settings used for audit logging
// and other persistence features.
type DatabaseConfig struct {
	// URL is a libpq-compatible connection string or URL.
	// Example: "postgres://user:pass@localhost:5432/vibewarden?sslmode=disable"
	// Can be set via VIBEWARDEN_DATABASE_URL env var.
	URL string `mapstructure:"url"`

	// ExternalURL is the connection URL for an external PostgreSQL instance.
	// When set, the generated Docker Compose omits the local kratos-db container
	// and uses this URL as the Kratos DSN instead of the local Postgres service.
	// Must be a valid postgres:// URL.
	// Example: "postgres://user:pass@db.example.com:5432/kratos?sslmode=require"
	// Can be set via VIBEWARDEN_DATABASE_EXTERNAL_URL env var.
	ExternalURL string `mapstructure:"external_url"`

	// TLSMode controls the PostgreSQL SSL/TLS negotiation mode when building
	// a DSN. Accepted values: "disable", "require", "verify-ca", "verify-full".
	// Default: "require".
	// This value is appended as sslmode=<value> when building the DSN unless
	// the URL already contains an sslmode query parameter.
	TLSMode string `mapstructure:"tls_mode"`

	// Pool holds connection pool settings.
	Pool DatabasePoolConfig `mapstructure:"pool"`

	// ConnectTimeout is the maximum time to wait when establishing a new
	// Postgres connection, expressed as a Go duration string (e.g. "10s", "30s").
	// Default: "10s".
	ConnectTimeout string `mapstructure:"connect_timeout"`
}

// BuildDSN returns the external URL with connection resilience parameters
// (sslmode, connect_timeout, pool_max_conns) appended as query parameters.
// Parameters already present in ExternalURL are not overwritten.
// Returns an empty string when ExternalURL is empty.
func (d DatabaseConfig) BuildDSN() string {
	if d.ExternalURL == "" {
		return ""
	}

	u, err := url.Parse(d.ExternalURL)
	if err != nil {
		// ExternalURL has already been validated; this path should not be reached.
		return d.ExternalURL
	}

	q := u.Query()

	if _, ok := q["sslmode"]; !ok {
		mode := d.TLSMode
		if mode == "" {
			mode = "require"
		}
		q.Set("sslmode", mode)
	}

	if _, ok := q["connect_timeout"]; !ok {
		timeout := d.ConnectTimeout
		if timeout == "" {
			timeout = "10s"
		}
		// Postgres connect_timeout is in seconds (integer). Strip the "s" suffix if present.
		secs := strings.TrimSuffix(timeout, "s")
		q.Set("connect_timeout", secs)
	}

	if _, ok := q["pool_max_conns"]; !ok {
		maxConns := d.Pool.MaxConns
		if maxConns <= 0 {
			maxConns = 10
		}
		q.Set("pool_max_conns", fmt.Sprintf("%d", maxConns))
	}

	u.RawQuery = q.Encode()
	return u.String()
}

// ResolveURL returns the database connection URL to use for migrations and
// other direct database access. It prefers the explicit URL field; when that
// is empty it falls back to BuildDSN (which derives a URL from ExternalURL
// with resilience parameters appended). Returns an empty string when neither
// URL nor ExternalURL is configured.
func (d DatabaseConfig) ResolveURL() string {
	if d.URL != "" {
		return d.URL
	}
	return d.BuildDSN()
}

// validatePostgresURL returns an error if s is not a valid postgres:// URL.
// It accepts both "postgres://" and "postgresql://" schemes.
func validatePostgresURL(s string) error {
	u, err := url.Parse(s)
	if err != nil {
		return fmt.Errorf("must be a valid URL: %w", err)
	}
	if u.Scheme != "postgres" && u.Scheme != "postgresql" {
		return fmt.Errorf("must use postgres:// or postgresql:// scheme, got %q", u.Scheme)
	}
	if u.Host == "" {
		return fmt.Errorf("must include a host")
	}
	return nil
}

// validateRedisURL returns an error if s is not a valid Redis connection URL.
// It accepts "redis://" (plain) and "rediss://" (TLS) schemes.
func validateRedisURL(s string) error {
	u, err := url.Parse(s)
	if err != nil {
		return fmt.Errorf("must be a valid URL: %w", err)
	}
	if u.Scheme != "redis" && u.Scheme != "rediss" {
		return fmt.Errorf("must use redis:// or rediss:// scheme, got %q", u.Scheme)
	}
	if u.Host == "" {
		return fmt.Errorf("must include a host")
	}
	return nil
}

// validateDatabase validates database configuration and returns a slice of error strings.
func validateDatabase(c *Config) []string {
	var errs []string

	// database.external_url validation: when set, must be a valid postgres:// URL.
	if c.Database.ExternalURL != "" {
		if err := validatePostgresURL(c.Database.ExternalURL); err != nil {
			errs = append(errs, fmt.Sprintf("database.external_url: %s", err.Error()))
		}
	}

	// database.tls_mode validation.
	validTLSModes := map[string]bool{
		"":            true, // empty means use default ("require")
		"disable":     true,
		"require":     true,
		"verify-ca":   true,
		"verify-full": true,
	}
	if !validTLSModes[c.Database.TLSMode] {
		errs = append(errs, fmt.Sprintf(
			"database.tls_mode %q is invalid; accepted values: \"disable\", \"require\", \"verify-ca\", \"verify-full\"",
			c.Database.TLSMode,
		))
	}

	// database.pool validation.
	if c.Database.Pool.MaxConns < 0 {
		errs = append(errs, fmt.Sprintf(
			"database.pool.max_conns %d is invalid; must be >= 0",
			c.Database.Pool.MaxConns,
		))
	}
	if c.Database.Pool.MinConns < 0 {
		errs = append(errs, fmt.Sprintf(
			"database.pool.min_conns %d is invalid; must be >= 0",
			c.Database.Pool.MinConns,
		))
	}
	if c.Database.Pool.MaxConns > 0 && c.Database.Pool.MinConns > c.Database.Pool.MaxConns {
		errs = append(errs, fmt.Sprintf(
			"database.pool.min_conns (%d) must be <= database.pool.max_conns (%d)",
			c.Database.Pool.MinConns, c.Database.Pool.MaxConns,
		))
	}

	// database.connect_timeout validation.
	if c.Database.ConnectTimeout != "" {
		if _, err := time.ParseDuration(c.Database.ConnectTimeout); err != nil {
			errs = append(errs, fmt.Sprintf(
				"database.connect_timeout %q is not a valid duration: %s",
				c.Database.ConnectTimeout, err.Error(),
			))
		}
	}

	return errs
}
