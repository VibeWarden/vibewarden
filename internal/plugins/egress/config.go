// Package egress implements the VibeWarden egress proxy plugin.
//
// The plugin listens on a dedicated localhost port (default: 127.0.0.1:8081)
// and forwards outbound HTTP requests from the wrapped application to external
// services, enforcing allowlists, secret injection, rate limiting, and circuit
// breaking as configured by the user.
package egress

import "time"

// Config holds all settings for the egress proxy plugin.
// It maps to the egress section of vibewarden.yaml.
type Config struct {
	// Enabled toggles the egress proxy plugin (default: false).
	Enabled bool

	// Listen is the TCP address the egress proxy binds to (default: "127.0.0.1:8081").
	Listen string

	// DefaultPolicy is the disposition for outbound requests that do not match
	// any configured route. Accepted values: "allow", "deny" (default: "deny").
	DefaultPolicy string

	// AllowInsecure, when true, permits plain HTTP egress requests globally.
	AllowInsecure bool

	// DefaultTimeout is the global request timeout. Zero means 30 s.
	DefaultTimeout time.Duration

	// DefaultBodySizeLimit is the global max request body in bytes. Zero means no limit.
	DefaultBodySizeLimit int64

	// DefaultResponseSizeLimit is the global max response body in bytes. Zero means no limit.
	DefaultResponseSizeLimit int64

	// BlockPrivate, when true, prevents the egress proxy from forwarding requests
	// to private or loopback IP addresses (SSRF protection). Default: true.
	BlockPrivate bool

	// AllowedPrivate is an optional list of CIDR ranges exempt from BlockPrivate.
	AllowedPrivate []string

	// Routes is the ordered list of egress route definitions.
	Routes []RouteConfig
}

// RouteConfig describes a single egress allowlist entry.
type RouteConfig struct {
	// Name is the unique human-readable identifier for this route (required).
	Name string

	// Pattern is the URL glob matched against outbound request URLs (required).
	// Must start with "http://" or "https://".
	Pattern string

	// Methods restricts the route to specific HTTP methods. Empty means all methods.
	Methods []string

	// Timeout is the per-route request timeout. Zero means use DefaultTimeout.
	Timeout time.Duration

	// Secret is the name of the secret to fetch and inject.
	Secret string

	// SecretHeader is the HTTP header into which the secret value is injected.
	SecretHeader string

	// SecretFormat is the value template; "{value}" is replaced with the resolved
	// secret. Empty means the raw secret value is used.
	SecretFormat string

	// RateLimit is the rate limit expression for this route (e.g. "100/s").
	RateLimit string

	// CircuitBreaker holds circuit breaker settings.
	CircuitBreaker CircuitBreakerConfig

	// Retries holds retry-with-backoff settings.
	Retries RetryConfig

	// BodySizeLimit is the max request body in bytes. Zero means use DefaultBodySizeLimit.
	BodySizeLimit int64

	// ResponseSizeLimit is the max response body in bytes. Zero means use DefaultResponseSizeLimit.
	ResponseSizeLimit int64

	// AllowInsecure permits plain HTTP for this specific route.
	AllowInsecure bool
}

// CircuitBreakerConfig holds circuit breaker parameters for a route.
type CircuitBreakerConfig struct {
	// Threshold is the number of consecutive failures required to open the circuit.
	Threshold int

	// ResetAfter is how long the circuit stays open before allowing a probe.
	ResetAfter time.Duration
}

// RetryConfig holds retry-with-backoff parameters for a route.
type RetryConfig struct {
	// Max is the maximum number of retry attempts (not counting the initial request).
	Max int

	// Methods is the set of HTTP methods eligible for retry. Empty means idempotent only.
	Methods []string

	// Backoff selects the backoff strategy: "exponential" or "fixed".
	Backoff string

	// InitialBackoff is the base wait before the first retry. Zero means 100 ms.
	InitialBackoff time.Duration
}
