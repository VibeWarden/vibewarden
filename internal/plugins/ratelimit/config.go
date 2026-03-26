// Package ratelimit implements the VibeWarden rate-limiting plugin.
//
// It enforces per-IP and per-user token-bucket rate limits on every proxied
// request via the vibewarden_rate_limit Caddy handler module. Limiters are
// created from an in-memory factory on Init and closed on Stop to release the
// background GC goroutines.
package ratelimit

// Config holds all settings for the rate-limiting plugin.
// It maps to the plugins.rate-limiting section of vibewarden.yaml.
type Config struct {
	// Enabled toggles the rate-limiting plugin.
	Enabled bool

	// Store names the backing store for limiter state.
	// The only supported value in v1 is "memory" (the default).
	// "redis" is reserved for future work.
	Store string

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
