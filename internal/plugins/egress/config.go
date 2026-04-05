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

	// ValidateResponse holds per-route upstream response validation settings.
	// When non-zero, each upstream response is checked against the configured
	// allowed status code ranges and content types. Responses that fail
	// validation are dropped and the caller receives a 502 Bad Gateway.
	ValidateResponse ResponseValidationConfig

	// Headers holds per-route header injection and stripping rules applied before
	// forwarding the request and before returning the response to the caller.
	Headers HeadersConfig

	// Cache holds per-route in-memory response caching settings.
	// Only GET and HEAD requests that receive a 2xx response are cached.
	Cache CacheConfig

	// Sanitize holds per-route PII redaction rules applied to outbound requests
	// before forwarding and to structured log output.
	Sanitize SanitizeConfig

	// MTLS holds mutual-TLS client certificate settings for this route.
	// When non-zero, the egress proxy presents the configured certificate during
	// the TLS handshake with the upstream.
	MTLS MTLSConfig

	// LLMResponseValidation holds per-route LLM response schema validation
	// settings. When Enabled is true, upstream JSON responses are validated
	// against the configured JSON Schema before being returned to the caller.
	LLMResponseValidation LLMResponseValidationConfig
}

// HeadersConfig holds per-route header injection and stripping rules for the
// egress plugin configuration layer. Fields mirror the domain HeadersConfig.
type HeadersConfig struct {
	// Add is a map of header name to value injected into the outbound request.
	// If the header already exists, its value is overwritten.
	Add map[string]string

	// RemoveRequest is the list of request header names removed before forwarding.
	RemoveRequest []string

	// RemoveResponse is the list of response header names removed before
	// returning the upstream response to the caller.
	RemoveResponse []string
}

// CacheConfig holds per-route in-memory response caching parameters for the
// egress plugin configuration layer.
type CacheConfig struct {
	// Enabled activates response caching for this route.
	Enabled bool

	// TTL is how long a cached entry remains valid. Zero means entries never expire.
	TTL time.Duration

	// MaxSize is the maximum number of bytes for a single cached response body.
	// Zero means no per-entry size limit.
	MaxSize int64
}

// SanitizeConfig holds per-route PII redaction rules for the egress plugin
// configuration layer.
type SanitizeConfig struct {
	// Headers is the list of request header names redacted in log output.
	// The actual forwarded header value is preserved unchanged.
	Headers []string

	// QueryParams is the list of query parameter names stripped from the
	// request URL before forwarding.
	QueryParams []string

	// BodyFields is the list of JSON field names replaced with "[REDACTED]"
	// in the request body before forwarding.
	BodyFields []string
}

// MTLSConfig holds mutual-TLS client certificate parameters for the egress
// plugin configuration layer.
type MTLSConfig struct {
	// CertPath is the filesystem path to the PEM-encoded client certificate.
	CertPath string

	// KeyPath is the filesystem path to the PEM-encoded private key.
	KeyPath string

	// CAPath is an optional path to a PEM-encoded CA certificate bundle used
	// to verify the server certificate. Empty means use the system root CA pool.
	CAPath string
}

// ResponseValidationConfig holds per-route upstream response validation
// parameters for the egress proxy.
type ResponseValidationConfig struct {
	// StatusCodes is a list of allowed HTTP status code range expressions.
	// Supported formats: exact code ("200"), class wildcard ("2xx", "3xx", "4xx",
	// "5xx"). When empty, no status code validation is performed.
	// Example: ["2xx", "301", "302"]
	StatusCodes []string

	// ContentTypes is a list of allowed MIME type prefixes for the upstream
	// response Content-Type header (parameters are ignored).
	// When empty, no Content-Type validation is performed.
	// Example: ["application/json", "text/plain"]
	ContentTypes []string
}

// LLMResponseValidationConfig holds per-route LLM response schema validation
// parameters for the egress plugin configuration layer.
//
// When Enabled is true, each upstream response with Content-Type
// application/json is validated against the provided JSON Schema. Responses
// that fail validation are either blocked (502 Bad Gateway) or passed through
// with a warning logged, depending on Action.
type LLMResponseValidationConfig struct {
	// Enabled activates LLM response schema validation for this route.
	Enabled bool

	// Action controls what happens when validation fails.
	// Accepted values: "block" (default) or "warn".
	Action string

	// Schema is the JSON Schema document used to validate upstream response
	// bodies. Must be a non-nil map representing a valid JSON Schema object.
	Schema map[string]any
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
