package egress

import (
	"errors"
	"fmt"
	"path"
	"strings"
	"time"
)

// CircuitBreakerConfig holds the parameters that drive per-route circuit breaking.
type CircuitBreakerConfig struct {
	// Threshold is the number of consecutive failures required to trip the circuit.
	// Must be > 0 when used.
	Threshold int

	// ResetAfter is how long the circuit stays open before allowing a probe request.
	// Must be > 0 when used.
	ResetAfter time.Duration
}

// RetryBackoff selects the backoff strategy for retry attempts.
type RetryBackoff string

const (
	// RetryBackoffExponential doubles the wait time on each retry.
	RetryBackoffExponential RetryBackoff = "exponential"

	// RetryBackoffFixed uses a constant wait time between retries.
	RetryBackoffFixed RetryBackoff = "fixed"
)

// RetryConfig holds retry-with-backoff parameters for a route.
type RetryConfig struct {
	// Max is the maximum number of retry attempts (not counting the initial request).
	// Must be >= 1 when used.
	Max int

	// Methods is the set of HTTP methods eligible for retry (e.g. ["GET", "PUT"]).
	// When empty the default idempotent set is used (GET, HEAD, PUT, DELETE).
	Methods []string

	// Backoff selects the backoff strategy. Defaults to exponential when empty.
	Backoff RetryBackoff

	// InitialBackoff is the base wait duration before the first retry.
	// Defaults to 100 ms when zero.
	InitialBackoff time.Duration
}

// defaultIdempotentMethods is the set of HTTP methods that are safe to retry
// by default because they are idempotent per RFC 9110.
var defaultIdempotentMethods = map[string]struct{}{
	"GET":    {},
	"HEAD":   {},
	"PUT":    {},
	"DELETE": {},
}

// IsRetryableMethod reports whether method is eligible for retry under this
// RetryConfig. When Methods is empty the default idempotent set is used
// (GET, HEAD, PUT, DELETE). Otherwise only methods listed in Methods are
// eligible.
func (r RetryConfig) IsRetryableMethod(method string) bool {
	upper := strings.ToUpper(method)
	if len(r.Methods) == 0 {
		_, ok := defaultIdempotentMethods[upper]
		return ok
	}
	for _, m := range r.Methods {
		if strings.ToUpper(m) == upper {
			return true
		}
	}
	return false
}

// MTLSConfig holds mutual-TLS client certificate parameters for a route.
// When non-zero, the egress proxy presents the configured client certificate
// during the TLS handshake with the upstream.
type MTLSConfig struct {
	// CertPath is the filesystem path to the PEM-encoded client certificate.
	// Must be set together with KeyPath.
	CertPath string

	// KeyPath is the filesystem path to the PEM-encoded private key for the
	// client certificate. Must be set together with CertPath.
	KeyPath string

	// CAPath is an optional filesystem path to a PEM-encoded CA certificate
	// bundle used to verify the server's certificate. When empty the system
	// root CA pool is used.
	CAPath string
}

// IsZero reports whether this MTLSConfig is the zero value (no mTLS configured).
func (m MTLSConfig) IsZero() bool {
	return m.CertPath == "" && m.KeyPath == "" && m.CAPath == ""
}

// CacheConfig holds per-route response caching parameters.
// Caching applies only to GET and HEAD requests that receive a 2xx response.
type CacheConfig struct {
	// Enabled activates response caching for this route.
	Enabled bool

	// TTL is how long a cached entry remains valid before it is evicted.
	// A zero value means the cache entry never expires (not recommended in
	// production — always set an explicit TTL).
	TTL time.Duration

	// MaxSize is the maximum number of bytes allowed for a single cached
	// response body. Responses larger than this value are not cached.
	// A value of 0 means no per-entry size limit is enforced.
	MaxSize int64
}

// IsZero reports whether this CacheConfig is the zero value (caching disabled).
func (c CacheConfig) IsZero() bool {
	return !c.Enabled
}

// SecretConfig holds secret injection parameters for a route.
type SecretConfig struct {
	// Name is the OpenBao secret name to fetch and inject.
	Name string

	// Header is the HTTP request header into which the secret value is injected.
	// Example: "Authorization"
	Header string

	// Format is the value template. The literal "{value}" is replaced with the
	// resolved secret value.
	// Example: "Bearer {value}"
	Format string
}

// Route is an immutable value object that describes a single egress allowlist
// entry. A route matches outbound requests by URL glob pattern and applies the
// configured security, resilience, and observability settings.
//
// Routes are equal when their names are equal — name is the natural key.
type Route struct {
	name              string
	pattern           string
	methods           []string
	timeout           time.Duration
	secret            SecretConfig
	headers           HeadersConfig
	rateLimit         string
	circuitBreaker    CircuitBreakerConfig
	retry             RetryConfig
	bodySizeLimit     int64
	responseSizeLimit int64
	allowInsecure     bool
	sanitize          SanitizeConfig
	mtls              MTLSConfig
	cache             CacheConfig
}

// routeOptions carries optional fields supplied via functional options.
type routeOptions struct {
	methods           []string
	timeout           time.Duration
	secret            SecretConfig
	headers           HeadersConfig
	rateLimit         string
	circuitBreaker    CircuitBreakerConfig
	retry             RetryConfig
	bodySizeLimit     int64
	responseSizeLimit int64
	allowInsecure     bool
	sanitize          SanitizeConfig
	mtls              MTLSConfig
	cache             CacheConfig
}

// RouteOption is a functional option for NewRoute.
type RouteOption func(*routeOptions)

// WithMethods restricts the route to the given HTTP methods (e.g. "GET", "POST").
// An empty slice means all methods are matched.
func WithMethods(methods ...string) RouteOption {
	return func(o *routeOptions) { o.methods = methods }
}

// WithTimeout sets the per-route request timeout. A zero value means no timeout.
func WithTimeout(d time.Duration) RouteOption {
	return func(o *routeOptions) { o.timeout = d }
}

// WithSecret configures secret injection for requests matched by this route.
func WithSecret(cfg SecretConfig) RouteOption {
	return func(o *routeOptions) { o.secret = cfg }
}

// WithRateLimit sets the rate limit expression for this route (e.g. "100/s").
func WithRateLimit(expr string) RouteOption {
	return func(o *routeOptions) { o.rateLimit = expr }
}

// WithCircuitBreaker configures the circuit breaker for this route.
func WithCircuitBreaker(cfg CircuitBreakerConfig) RouteOption {
	return func(o *routeOptions) { o.circuitBreaker = cfg }
}

// WithRetry configures retry-with-backoff for this route.
func WithRetry(cfg RetryConfig) RouteOption {
	return func(o *routeOptions) { o.retry = cfg }
}

// WithBodySizeLimit sets the maximum allowed request body size in bytes for this
// route. Requests whose body exceeds this limit are rejected with 413. A value
// of 0 means no per-route limit (the proxy default applies).
func WithBodySizeLimit(bytes int64) RouteOption {
	return func(o *routeOptions) { o.bodySizeLimit = bytes }
}

// WithResponseSizeLimit sets the maximum allowed response body size in bytes for
// this route. When the upstream response body exceeds this limit the body is
// truncated and a warning header is added. A value of 0 means no per-route limit
// (the proxy default applies).
func WithResponseSizeLimit(bytes int64) RouteOption {
	return func(o *routeOptions) { o.responseSizeLimit = bytes }
}

// WithHeaders configures per-route header injection and stripping rules.
func WithHeaders(cfg HeadersConfig) RouteOption {
	return func(o *routeOptions) { o.headers = cfg }
}

// WithAllowInsecure permits plain HTTP egress requests for this route,
// overriding the global egress.allow_insecure setting.
// When not set, the proxy-level default governs whether HTTP is allowed.
func WithAllowInsecure(allow bool) RouteOption {
	return func(o *routeOptions) { o.allowInsecure = allow }
}

// WithSanitize configures the per-route PII redaction rules. These rules
// govern which headers are redacted in log output, which query parameters are
// stripped before forwarding, and which JSON body fields are replaced with
// "[REDACTED]" before the request is sent to the upstream.
func WithSanitize(cfg SanitizeConfig) RouteOption {
	return func(o *routeOptions) { o.sanitize = cfg }
}

// WithMTLS configures mutual-TLS client certificate authentication for this
// route. When set, the egress proxy presents the given client certificate
// during the TLS handshake with the upstream. The cert/key files must contain
// valid PEM-encoded data; the CA file is optional and, when provided, is used
// to verify the server's certificate instead of the system root CA pool.
func WithMTLS(cfg MTLSConfig) RouteOption {
	return func(o *routeOptions) { o.mtls = cfg }
}

// WithCache enables in-memory response caching for this route.
// Only GET and HEAD responses with a 2xx status code are cached.
// Entries are evicted after cfg.TTL elapses or when the body size exceeds
// cfg.MaxSize.
func WithCache(cfg CacheConfig) RouteOption {
	return func(o *routeOptions) { o.cache = cfg }
}

// NewRoute constructs a Route value object.
// Returns an error when name is empty, pattern is empty, or the pattern is
// not a valid URL glob (as accepted by path.Match).
func NewRoute(name, pattern string, opts ...RouteOption) (Route, error) {
	if name == "" {
		return Route{}, errors.New("route name cannot be empty")
	}
	if pattern == "" {
		return Route{}, errors.New("route pattern cannot be empty")
	}
	if err := validatePattern(pattern); err != nil {
		return Route{}, fmt.Errorf("route pattern %q: %w", pattern, err)
	}

	o := &routeOptions{}
	for _, opt := range opts {
		opt(o)
	}

	return Route{
		name:              name,
		pattern:           pattern,
		methods:           o.methods,
		timeout:           o.timeout,
		secret:            o.secret,
		headers:           o.headers,
		rateLimit:         o.rateLimit,
		circuitBreaker:    o.circuitBreaker,
		retry:             o.retry,
		bodySizeLimit:     o.bodySizeLimit,
		responseSizeLimit: o.responseSizeLimit,
		allowInsecure:     o.allowInsecure,
		sanitize:          o.sanitize,
		mtls:              o.mtls,
		cache:             o.cache,
	}, nil
}

// Name returns the unique identifier for this route.
func (r Route) Name() string { return r.name }

// Pattern returns the URL glob pattern used to match outbound requests.
func (r Route) Pattern() string { return r.pattern }

// Methods returns the HTTP methods this route applies to.
// An empty slice means all methods are matched.
func (r Route) Methods() []string { return r.methods }

// Timeout returns the per-route request timeout.
// A zero value means no timeout override — the global default is used.
func (r Route) Timeout() time.Duration { return r.timeout }

// Secret returns the secret injection configuration for this route.
func (r Route) Secret() SecretConfig { return r.secret }

// RateLimit returns the rate limit expression for this route (e.g. "100/s").
// An empty string means no per-route rate limit.
func (r Route) RateLimit() string { return r.rateLimit }

// CircuitBreaker returns the circuit breaker configuration for this route.
func (r Route) CircuitBreaker() CircuitBreakerConfig { return r.circuitBreaker }

// Retry returns the retry configuration for this route.
func (r Route) Retry() RetryConfig { return r.retry }

// BodySizeLimit returns the maximum allowed request body size in bytes for this
// route. A value of 0 means no per-route limit.
func (r Route) BodySizeLimit() int64 { return r.bodySizeLimit }

// ResponseSizeLimit returns the maximum allowed response body size in bytes for
// this route. A value of 0 means no per-route limit.
func (r Route) ResponseSizeLimit() int64 { return r.responseSizeLimit }

// Headers returns the per-route header manipulation configuration.
func (r Route) Headers() HeadersConfig { return r.headers }

// AllowInsecure reports whether plain HTTP egress requests are permitted for
// this route. When true, HTTP targets are accepted regardless of the proxy-level
// default. When false, the proxy-level setting governs.
func (r Route) AllowInsecure() bool { return r.allowInsecure }

// Sanitize returns the per-route PII redaction configuration.
// A zero SanitizeConfig means no sanitization rules are applied.
func (r Route) Sanitize() SanitizeConfig { return r.sanitize }

// MTLS returns the mutual-TLS client certificate configuration for this route.
// A zero MTLSConfig means no mTLS is configured.
func (r Route) MTLS() MTLSConfig { return r.mtls }

// Cache returns the per-route response caching configuration.
// A zero CacheConfig (Enabled == false) means no caching is configured.
func (r Route) Cache() CacheConfig { return r.cache }

// MatchesMethod reports whether the given HTTP method is allowed by this route.
// When Methods is empty, all methods are considered a match.
func (r Route) MatchesMethod(method string) bool {
	if len(r.methods) == 0 {
		return true
	}
	upper := strings.ToUpper(method)
	for _, m := range r.methods {
		if strings.ToUpper(m) == upper {
			return true
		}
	}
	return false
}

// MatchesURL reports whether rawURL matches this route's glob pattern.
// It returns false when rawURL is malformed.
func (r Route) MatchesURL(rawURL string) bool {
	matched, err := path.Match(r.pattern, rawURL)
	if err != nil {
		return false
	}
	return matched
}

// validatePattern returns an error when the pattern is not a valid path.Match
// glob expression, or is missing the URL scheme prefix.
func validatePattern(pattern string) error {
	// path.Match returns ErrBadPattern on malformed glob syntax.
	if _, err := path.Match(pattern, ""); err != nil {
		return fmt.Errorf("invalid glob syntax: %w", err)
	}
	if !strings.HasPrefix(pattern, "http://") && !strings.HasPrefix(pattern, "https://") {
		return errors.New("pattern must start with http:// or https://")
	}
	return nil
}
