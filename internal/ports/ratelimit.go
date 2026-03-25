package ports

import (
	"context"
	"time"
)

// RateLimitConfig holds configuration for the rate limiting middleware.
type RateLimitConfig struct {
	// Enabled toggles rate limiting (default: true).
	Enabled bool

	// PerIP configures per-IP rate limits applied to all requests.
	PerIP RateLimitRule

	// PerUser configures per-user rate limits applied to authenticated requests only.
	PerUser RateLimitRule

	// TrustProxyHeaders enables reading X-Forwarded-For to determine the real client IP.
	// Only enable when VibeWarden is behind a trusted reverse proxy.
	TrustProxyHeaders bool

	// ExemptPaths is a list of glob patterns for paths that bypass rate limiting.
	// The /_vibewarden/* prefix is always exempt and is added automatically.
	ExemptPaths []string
}

// RateLimitRule defines the sustained rate and burst size for a rate limit.
type RateLimitRule struct {
	// RequestsPerSecond is the sustained request rate allowed per key.
	RequestsPerSecond float64

	// Burst is the maximum number of requests allowed in a burst above the sustained rate.
	Burst int
}

// RateLimitResult represents the outcome of a rate limit check.
type RateLimitResult struct {
	// Allowed is true if the request should proceed.
	Allowed bool

	// Remaining is the number of requests remaining in the current window.
	Remaining int

	// RetryAfter is the duration the client should wait before retrying.
	// Only meaningful when Allowed is false.
	RetryAfter time.Duration

	// Limit is the configured sustained rate in requests per second.
	Limit float64

	// Burst is the configured burst size.
	Burst int
}

// RateLimiter is the outbound port for checking whether a request should be rate limited.
// Implementations are responsible for tracking per-key state and cleaning up expired entries.
type RateLimiter interface {
	// Allow checks whether a request identified by key should be allowed through.
	// The key is typically a client IP address or authenticated user ID.
	// Returns a RateLimitResult containing the decision and retry information.
	Allow(ctx context.Context, key string) RateLimitResult

	// Close releases any resources held by the rate limiter, such as background
	// cleanup goroutines. Should be called on graceful shutdown.
	Close() error
}

// RateLimiterFactory creates RateLimiter instances configured with a specific rule.
type RateLimiterFactory interface {
	// NewLimiter creates a new RateLimiter configured with the given rule.
	NewLimiter(rule RateLimitRule) RateLimiter
}
