package config

import "fmt"

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
	// URL is the Redis connection URL (e.g. "redis://:password@localhost:6379/0"
	// or "rediss://user:password@redis.example.com:6380/1" for TLS).
	// When set, URL takes precedence over Address, Password, and DB.
	// Use a redis:// URL for unencrypted connections and a rediss:// URL for TLS.
	// Optional: if omitted, Address is used to build the connection.
	URL string `mapstructure:"url"`

	// Address is the Redis server address in host:port form (default: "localhost:6379").
	// Used when URL is empty. At least one of URL or Address is required when
	// rate_limit.store is "redis".
	Address string `mapstructure:"address"`

	// Password is the Redis AUTH password (default: empty, no auth).
	// Ignored when URL is set (embed credentials in the URL instead).
	Password string `mapstructure:"password"`

	// DB is the Redis logical database index (default: 0).
	// Ignored when URL is set (embed the DB index in the URL path instead).
	DB int `mapstructure:"db"`

	// PoolSize is the maximum number of socket connections held in the pool
	// (default: 0, which lets go-redis choose a sensible value based on CPU count).
	PoolSize int `mapstructure:"pool_size"`

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

// HasExternalURL reports whether an explicit Redis URL has been configured.
// When true, VibeWarden connects to that external Redis instance directly and
// the generated Docker Compose file omits the local redis container.
func (r RateLimitRedisConfig) HasExternalURL() bool {
	return r.URL != ""
}

// RateLimitRuleConfig holds the sustained rate and burst size for a rate limit.
type RateLimitRuleConfig struct {
	// RequestsPerSecond is the sustained request rate (default: 10 for IP, 100 for user).
	RequestsPerSecond float64 `mapstructure:"requests_per_second"`

	// Burst is the maximum number of requests allowed in a burst
	// above the sustained rate (default: 20 for IP, 200 for user).
	Burst int `mapstructure:"burst"`
}

// validateRateLimit validates rate limit configuration and returns a slice of error strings.
func validateRateLimit(c *Config) []string {
	var errs []string
	switch c.RateLimit.Store {
	case "", "memory":
		// valid — "memory" is the default
	case "redis":
		if c.RateLimit.Redis.URL == "" && c.RateLimit.Redis.Address == "" {
			errs = append(errs, "rate_limit.redis.address is required when rate_limit.store is \"redis\" and rate_limit.redis.url is not set — "+
				"set rate_limit.redis.address to your Redis host:port (e.g. \"127.0.0.1:6379\") or set rate_limit.redis.url to a redis:// URL")
		}
		if c.RateLimit.Redis.URL != "" {
			if err := validateRedisURL(c.RateLimit.Redis.URL); err != nil {
				errs = append(errs, fmt.Sprintf("rate_limit.redis.url: %s", err.Error()))
			}
		}
	default:
		errs = append(errs, fmt.Sprintf(
			"rate_limit.store %q is invalid; accepted values: \"memory\", \"redis\" — "+
				"set rate_limit.store to \"memory\" for a single-process limiter or \"redis\" for a distributed limiter",
			c.RateLimit.Store,
		))
	}
	return errs
}
