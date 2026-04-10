// Package secrets implements the VibeWarden secret management plugin.
//
// It connects to an OpenBao (open-source Vault fork) server and provides:
//   - Static secret fetching from KV v2 with in-memory caching
//   - Header injection: secrets added to proxied requests as HTTP headers
//   - Env file writing: secrets written to a .env file the upstream app can read
//   - Dynamic Postgres credentials via OpenBao's database engine
//   - Secret health checks: expiry, weakness, and staleness detection
package secrets

import "time"

// Config holds all settings for the secrets plugin.
// It maps to the secrets section of vibewarden.yaml.
type Config struct {
	// Enabled toggles the secrets plugin.
	Enabled bool

	// Provider selects the secret store backend. Currently only "openbao" is supported.
	Provider string

	// OpenBao holds connection and authentication settings for the OpenBao server.
	OpenBao OpenBaoConfig

	// Inject defines how fetched secrets are surfaced to the upstream app.
	Inject InjectConfig

	// Dynamic holds settings for dynamic credential generation.
	Dynamic DynamicConfig

	// Health holds settings for the secret health check subsystem.
	Health HealthConfig

	// CacheTTL is how long fetched static secrets are held in memory before
	// being re-fetched from the store. Default: 5 minutes.
	CacheTTL time.Duration
}

// OpenBaoConfig holds connection settings for the OpenBao server.
type OpenBaoConfig struct {
	// Address is the OpenBao server URL (e.g. "http://openbao:8200").
	Address string

	// Auth holds the authentication credentials.
	Auth OpenBaoAuthConfig

	// MountPath is the KV v2 mount path (default: "secret").
	MountPath string
}

// OpenBaoAuthConfig holds authentication credentials for OpenBao.
type OpenBaoAuthConfig struct {
	// Method is the auth method: "token" or "approle".
	Method string

	// Token is the static token. Used when Method is "token".
	Token string

	// RoleID is the AppRole role_id. Used when Method is "approle".
	RoleID string

	// SecretID is the AppRole secret_id. Used when Method is "approle".
	SecretID string
}

// InjectConfig defines how secrets are injected into proxied requests or
// written for the upstream app to consume.
type InjectConfig struct {
	// Headers is the list of secrets to inject as HTTP request headers.
	Headers []HeaderInjection

	// EnvFile is the path to write a .env file containing secret values.
	// The upstream app must read this file on startup.
	// When empty, no env file is written.
	EnvFile string

	// Env is the list of secrets to write into the env file.
	Env []EnvInjection
}

// HeaderInjection maps a secret key to an HTTP request header name.
type HeaderInjection struct {
	// SecretPath is the KV path of the secret (e.g. "app/internal-api").
	SecretPath string

	// SecretKey is the key within the secret map.
	SecretKey string

	// Header is the HTTP header name to set (e.g. "X-Internal-Token").
	Header string
}

// EnvInjection maps a secret key to an environment variable name.
type EnvInjection struct {
	// SecretPath is the KV path of the secret (e.g. "app/database").
	SecretPath string

	// SecretKey is the key within the secret map.
	SecretKey string

	// EnvVar is the environment variable name to write in the env file.
	EnvVar string
}

// DynamicConfig holds settings for dynamic credential generation.
type DynamicConfig struct {
	// Postgres configures the OpenBao database engine for Postgres dynamic creds.
	Postgres DynamicPostgresConfig
}

// DynamicPostgresConfig holds settings for dynamic Postgres credential generation.
type DynamicPostgresConfig struct {
	// Enabled toggles dynamic Postgres credential generation.
	Enabled bool

	// Roles is the list of OpenBao database roles to request credentials for.
	Roles []DynamicRole
}

// DynamicRole defines a single OpenBao database role and where to inject its credentials.
type DynamicRole struct {
	// Name is the OpenBao database role name (e.g. "app-readwrite").
	Name string

	// EnvVarUser is the env var name to write the generated username into.
	EnvVarUser string

	// EnvVarPassword is the env var name to write the generated password into.
	EnvVarPassword string
}

// HealthConfig holds settings for the secret health check subsystem.
type HealthConfig struct {
	// CheckInterval is how often to run health checks (default: 6 hours).
	CheckInterval time.Duration

	// MaxStaticAge is the maximum acceptable age of a static secret before
	// it is considered stale and a warning is emitted. Default: 90 days.
	MaxStaticAge time.Duration

	// WeakPatterns is the list of substrings that indicate a weak/default secret.
	// Matching is case-insensitive.
	WeakPatterns []string
}
