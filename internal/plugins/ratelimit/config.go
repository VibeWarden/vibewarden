// Package ratelimit implements the VibeWarden rate-limiting plugin.
//
// It enforces per-IP and per-user token-bucket rate limits on every proxied
// request via the vibewarden_rate_limit Caddy handler module. Limiters are
// created from a factory on Init and closed on Stop to release background
// resources. The factory is selected based on the Store field: "memory" uses
// an in-process token bucket; "redis" uses a Redis-backed distributed bucket
// with optional in-memory fallback.
package ratelimit

// Config holds all settings for the rate-limiting plugin.
// It maps to the plugins.rate-limiting section of vibewarden.yaml.
type Config struct {
	// Enabled toggles the rate-limiting plugin.
	Enabled bool

	// Store names the backing store for limiter state.
	// Accepted values: "memory" (default), "redis".
	Store string

	// Redis holds connection settings for the Redis store.
	// Only used when Store is "redis".
	Redis RedisConfig

	// PerIP configures the per-IP rate limit applied to every request.
	PerIP RuleConfig

	// PerUser configures the per-user rate limit applied to authenticated
	// requests only. Unauthenticated requests are not subject to this limit.
	PerUser RuleConfig

	// TrustProxyHeaders enables reading the X-Forwarded-For header to
	// determine the real client IP address. Only enable this when
	// VibeWarden is behind a trusted reverse proxy.
	TrustProxyHeaders bool

	// ExemptPaths is a list of URL path glob patterns that bypass rate
	// limiting entirely. The /_vibewarden/* prefix is always exempt and
	// is added automatically by the middleware.
	ExemptPaths []string
}

// RedisConfig holds connection settings for the Redis rate limit store.
type RedisConfig struct {
	// URL is the Redis connection URL (e.g. "redis://:password@localhost:6379/0"
	// or "rediss://user:pass@redis.example.com:6380/1" for TLS).
	// When set, URL takes precedence over Address, Password, and DB.
	// Supports both redis:// (plain) and rediss:// (TLS) schemes.
	URL string

	// Address is the Redis server address in host:port form.
	// Used when URL is empty. At least one of URL or Address is required
	// when Store is "redis".
	Address string

	// Password is the Redis AUTH password.
	// Ignored when URL is set (embed credentials in the URL instead).
	Password string

	// DB is the Redis logical database index.
	// Ignored when URL is set (embed the DB index in the URL path instead).
	DB int

	// PoolSize is the maximum number of socket connections held in the pool.
	// Defaults to 0 (go-redis picks a sensible value based on CPU count).
	PoolSize int

	// KeyPrefix is the namespace prefix prepended to every Redis key.
	KeyPrefix string

	// Fallback controls whether requests fall back to the in-memory store
	// when Redis is unavailable. Default: true (fail-open).
	Fallback bool

	// HealthCheckInterval is how often the background goroutine probes Redis
	// for recovery after a failure. Default: 30 seconds.
	HealthCheckInterval string
}

// RuleConfig defines the sustained request rate and burst size for one rate
// limit dimension (per-IP or per-user).
type RuleConfig struct {
	// RequestsPerSecond is the sustained request rate allowed per key.
	// A value of 0 disables this limit dimension.
	RequestsPerSecond float64

	// Burst is the maximum number of requests allowed in a burst above the
	// sustained rate.
	Burst int
}
