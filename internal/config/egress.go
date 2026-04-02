package config

import "net"

// EgressConfig holds configuration for the egress proxy plugin.
// When enabled, the egress proxy listens on a separate port and forwards
// outbound HTTP requests from the wrapped application to external services,
// applying allowlisting, secret injection, rate limiting, and circuit breaking.
type EgressConfig struct {
	// Enabled toggles the egress proxy plugin (default: false).
	Enabled bool `mapstructure:"enabled"`

	// Listen is the address the egress proxy binds to (default: "127.0.0.1:8081").
	Listen string `mapstructure:"listen"`

	// NetworkIsolation controls whether the generated Docker Compose file uses
	// a dual-network topology to enforce egress isolation at the network level.
	// When nil, defaults to true if Enabled is true.
	// When true, the app service is placed on an internal-only Docker network
	// with no internet access, and VibeWarden bridges internal and external networks.
	// When false, all services share a single bridge network (legacy behavior).
	NetworkIsolation *bool `mapstructure:"network_isolation"`

	// DefaultPolicy determines the disposition for outbound requests that do not
	// match any configured route. Accepted values: "allow", "deny" (default: "deny").
	DefaultPolicy string `mapstructure:"default_policy"`

	// AllowInsecure, when true, permits plain HTTP egress requests globally.
	// By default only HTTPS targets are allowed. Individual routes can also
	// override this with their own allow_insecure field.
	AllowInsecure bool `mapstructure:"allow_insecure"`

	// DefaultTimeout is the global request timeout applied when a route does not
	// specify its own timeout. Accepts Go duration strings (e.g. "30s"). Default: "30s".
	DefaultTimeout string `mapstructure:"default_timeout"`

	// DefaultBodySizeLimit is the global maximum allowed request body size applied
	// when a route does not specify its own body_size_limit. Human-readable string
	// (e.g. "50MB"). Empty string means no global limit.
	DefaultBodySizeLimit string `mapstructure:"default_body_size_limit"`

	// DefaultResponseSizeLimit is the global maximum allowed response body size
	// applied when a route does not specify its own response_size_limit.
	// Human-readable string (e.g. "50MB"). Empty string means no global limit.
	DefaultResponseSizeLimit string `mapstructure:"default_response_size_limit"`

	// DNS holds DNS-level protection settings.
	DNS EgressDNSConfig `mapstructure:"dns"`

	// Routes is the ordered list of egress route definitions.
	// Routes are evaluated in declaration order; the first matching route wins.
	Routes []EgressRouteConfig `mapstructure:"routes"`
}

// EgressDNSConfig holds DNS-level protection settings for the egress proxy.
type EgressDNSConfig struct {
	// BlockPrivate, when true, prevents the egress proxy from forwarding requests
	// to private or loopback IP addresses (RFC 1918, RFC 4193, loopback).
	// This mitigates SSRF attacks. Default: true.
	BlockPrivate bool `mapstructure:"block_private"`

	// AllowedPrivate is an optional list of CIDR ranges that are exempt from the
	// private-IP block enforced by BlockPrivate. Use this to permit egress to
	// specific internal services when running inside a private network.
	// Each entry must be a valid CIDR in dotted-decimal or IPv6 notation.
	// Example: ["10.0.0.0/8", "192.168.100.0/24"]
	AllowedPrivate []string `mapstructure:"allowed_private"`
}

// EgressRouteConfig describes a single egress allowlist entry in the YAML config.
type EgressRouteConfig struct {
	// Name is the unique human-readable identifier for this route (required).
	Name string `mapstructure:"name"`

	// Pattern is the URL glob pattern matched against outbound request URLs
	// (required). Must start with "http://" or "https://".
	// Example: "https://api.stripe.com/**"
	Pattern string `mapstructure:"pattern"`

	// Methods restricts the route to the specified HTTP methods (e.g. ["GET", "POST"]).
	// When empty, all methods are matched.
	Methods []string `mapstructure:"methods"`

	// Timeout is the per-route request timeout as a duration string (e.g. "10s").
	// When empty, EgressConfig.DefaultTimeout is used.
	Timeout string `mapstructure:"timeout"`

	// Secret is the name of the OpenBao secret to fetch and inject.
	Secret string `mapstructure:"secret"`

	// SecretHeader is the HTTP request header into which the secret value is injected.
	// Example: "Authorization"
	SecretHeader string `mapstructure:"secret_header"`

	// SecretFormat is the value template. The literal "{value}" is replaced with the
	// resolved secret value. Example: "Bearer {value}"
	SecretFormat string `mapstructure:"secret_format"`

	// RateLimit is the rate limit expression for this route (e.g. "100/s").
	// When empty, no per-route rate limit is applied.
	RateLimit string `mapstructure:"rate_limit"`

	// CircuitBreaker holds circuit breaker settings for this route.
	CircuitBreaker EgressCircuitBreakerConfig `mapstructure:"circuit_breaker"`

	// Retries holds retry-with-backoff settings for this route.
	Retries EgressRetryConfig `mapstructure:"retries"`

	// BodySizeLimit is the maximum allowed request body size as a human-readable
	// string (e.g. "50MB"). When empty, EgressConfig.DefaultBodySizeLimit is used.
	BodySizeLimit string `mapstructure:"body_size_limit"`

	// ResponseSizeLimit is the maximum allowed response body size as a
	// human-readable string (e.g. "50MB"). When the upstream response body exceeds
	// this limit it is truncated and a warning header is added to the response.
	// When empty, EgressConfig.DefaultResponseSizeLimit is used.
	ResponseSizeLimit string `mapstructure:"response_size_limit"`

	// AllowInsecure, when true, permits plain HTTP egress requests for this
	// specific route, overriding the global egress.allow_insecure setting.
	// When false (default), only HTTPS targets are accepted.
	AllowInsecure bool `mapstructure:"allow_insecure"`

	// ValidateResponse holds per-route upstream response validation settings.
	// When non-zero, each upstream response is checked against the configured
	// allowed status code ranges and content types. Responses that fail
	// validation are dropped and the caller receives a 502 Bad Gateway.
	ValidateResponse EgressResponseValidationConfig `mapstructure:"validate_response"`

	// Headers holds per-route header injection and stripping rules.
	// Use this to add static request headers before forwarding, strip sensitive
	// request headers from reaching the upstream, or suppress server-fingerprinting
	// response headers from being returned to the caller.
	Headers EgressHeadersConfig `mapstructure:"headers"`

	// Cache holds per-route in-memory response caching settings.
	// Only GET and HEAD requests that receive a 2xx response are cached.
	Cache EgressCacheConfig `mapstructure:"cache"`

	// Sanitize holds per-route PII redaction rules.
	// Header values are redacted in structured log output, query parameters are
	// stripped before forwarding, and JSON body fields are replaced with "[REDACTED]"
	// before the request is sent upstream.
	Sanitize EgressSanitizeConfig `mapstructure:"sanitize"`

	// MTLS holds mutual-TLS client certificate settings for this route.
	// When set, the egress proxy presents the configured certificate during
	// the TLS handshake with the upstream.
	MTLS EgressMTLSConfig `mapstructure:"mtls"`
}

// EgressHeadersConfig holds per-route header manipulation rules as parsed from
// vibewarden.yaml.
type EgressHeadersConfig struct {
	// Add is a map of header name to value. Each entry is set on the outbound
	// request before forwarding. If the header already exists, its value is
	// overwritten.
	// Example: {"X-Api-Version": "2", "X-Source": "vibewarden"}
	Add map[string]string `mapstructure:"add"`

	// RemoveRequest is the list of request header names removed before the
	// request is forwarded to the upstream.
	// Example: ["Cookie", "Authorization"]
	RemoveRequest []string `mapstructure:"remove_request"`

	// RemoveResponse is the list of response header names removed from the
	// upstream response before it is returned to the caller.
	// Example: ["X-Powered-By", "Server"]
	RemoveResponse []string `mapstructure:"remove_response"`
}

// EgressCacheConfig holds per-route in-memory response caching parameters as
// parsed from vibewarden.yaml.
type EgressCacheConfig struct {
	// Enabled activates response caching for this route.
	Enabled bool `mapstructure:"enabled"`

	// TTL is how long a cached entry remains valid as a duration string
	// (e.g. "60s", "5m"). An empty string means entries never expire.
	TTL string `mapstructure:"ttl"`

	// MaxSize is the maximum number of bytes allowed for a single cached
	// response body as a human-readable string (e.g. "1MB"). When empty,
	// no per-entry size limit is enforced.
	MaxSize string `mapstructure:"max_size"`
}

// EgressSanitizeConfig holds per-route PII redaction rules as parsed from
// vibewarden.yaml.
type EgressSanitizeConfig struct {
	// Headers is the list of request header names whose values are redacted in
	// structured log events (e.g. "Authorization", "Cookie"). The header value
	// is preserved in the actual forwarded request.
	Headers []string `mapstructure:"headers"`

	// QueryParams is the list of query parameter names stripped from the
	// request URL before forwarding (e.g. "api_key", "token").
	QueryParams []string `mapstructure:"query_params"`

	// BodyFields is the list of JSON field names replaced with "[REDACTED]" in
	// the request body before forwarding (e.g. "password", "ssn").
	// Redaction applies only when Content-Type is application/json.
	BodyFields []string `mapstructure:"body_fields"`
}

// EgressMTLSConfig holds mutual-TLS client certificate parameters for an egress
// route as parsed from vibewarden.yaml.
type EgressMTLSConfig struct {
	// CertPath is the filesystem path to the PEM-encoded client certificate.
	// Must be set together with KeyPath.
	CertPath string `mapstructure:"cert_path"`

	// KeyPath is the filesystem path to the PEM-encoded private key for the
	// client certificate. Must be set together with CertPath.
	KeyPath string `mapstructure:"key_path"`

	// CAPath is an optional filesystem path to a PEM-encoded CA certificate
	// bundle used to verify the server certificate. When empty, the system
	// root CA pool is used.
	CAPath string `mapstructure:"ca_path"`
}

// EgressResponseValidationConfig holds per-route upstream response validation
// parameters, as parsed from vibewarden.yaml.
type EgressResponseValidationConfig struct {
	// StatusCodes is a list of allowed HTTP status code range expressions.
	// Supported formats: exact code ("200"), class wildcard ("2xx", "3xx", "4xx",
	// "5xx"). When empty, no status code validation is performed.
	// Example: ["2xx", "301", "302"]
	StatusCodes []string `mapstructure:"status_codes"`

	// ContentTypes is a list of allowed MIME type prefixes for the upstream
	// response Content-Type header (parameters such as charset are ignored).
	// When empty, no Content-Type validation is performed.
	// Example: ["application/json", "text/plain"]
	ContentTypes []string `mapstructure:"content_types"`
}

// EgressCircuitBreakerConfig holds circuit breaker parameters for an egress route.
type EgressCircuitBreakerConfig struct {
	// Threshold is the number of consecutive failures required to trip the circuit.
	// Must be > 0 when the circuit breaker is configured.
	Threshold int `mapstructure:"threshold"`

	// ResetAfter is how long the circuit stays open before allowing a probe request,
	// as a duration string (e.g. "30s"). Must be > 0 when the circuit breaker is configured.
	ResetAfter string `mapstructure:"reset_after"`
}

// EgressRetryConfig holds retry-with-backoff parameters for an egress route.
type EgressRetryConfig struct {
	// Max is the maximum number of retry attempts (not counting the initial request).
	// Must be >= 1 when retries are configured.
	Max int `mapstructure:"max"`

	// Methods is the set of HTTP methods eligible for retry (e.g. ["GET", "PUT"]).
	// When empty, all methods are retried.
	Methods []string `mapstructure:"methods"`

	// Backoff selects the backoff strategy: "exponential" or "fixed".
	// Defaults to "exponential" when empty.
	Backoff string `mapstructure:"backoff"`
}

// IsNetworkIsolationEnabled returns true when network-level egress isolation
// should be applied in the generated Docker Compose file. The logic is:
//   - If egress is disabled, isolation is always off (no proxy to route through).
//   - If NetworkIsolation is explicitly set, that value is used.
//   - If NetworkIsolation is nil (not set), it defaults to true when egress is enabled.
func (e EgressConfig) IsNetworkIsolationEnabled() bool {
	if !e.Enabled {
		return false
	}
	if e.NetworkIsolation != nil {
		return *e.NetworkIsolation
	}
	return true
}

// ListenPort extracts the port number from the Listen address field.
// If the address cannot be parsed, it returns the default egress port "8081".
func (e EgressConfig) ListenPort() string {
	if e.Listen == "" {
		return "8081"
	}
	_, port, err := net.SplitHostPort(e.Listen)
	if err != nil || port == "" {
		return "8081"
	}
	return port
}

// EgressWarnings returns a list of warning messages relevant to the current
// egress configuration. These warnings are informational and do not prevent
// generation, but they alert the user to potentially surprising behavior.
func (e EgressConfig) EgressWarnings() []string {
	var warnings []string

	if e.Enabled && !e.IsNetworkIsolationEnabled() {
		warnings = append(warnings, "Network isolation disabled: app can bypass egress proxy via direct connections")
	}

	if !e.Enabled && e.NetworkIsolation != nil && *e.NetworkIsolation {
		warnings = append(warnings, "Network isolation has no effect without egress proxy enabled")
	}

	return warnings
}
