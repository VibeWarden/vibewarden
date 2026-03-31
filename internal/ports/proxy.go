// Package ports defines the interfaces (ports) for VibeWarden's hexagonal architecture.
package ports

import (
	"context"
	"time"
)

// ProxyServer defines the interface for the reverse proxy server.
// Implementations handle incoming HTTP(S) requests and forward them to upstream.
type ProxyServer interface {
	// Start begins listening for incoming requests.
	// Blocks until the context is cancelled or an error occurs.
	Start(ctx context.Context) error

	// Stop gracefully shuts down the proxy server.
	// The provided context controls the shutdown timeout.
	Stop(ctx context.Context) error

	// Reload applies configuration changes without dropping connections.
	// Not all implementations may support reload; they should return an error if not.
	Reload(ctx context.Context) error
}

// ProxyConfig holds configuration for the proxy server.
// This is a domain-agnostic configuration that ports can depend on.
type ProxyConfig struct {
	// ListenAddr is the address to bind to (e.g., "127.0.0.1:8080")
	ListenAddr string

	// UpstreamAddr is the address of the upstream application (e.g., "127.0.0.1:3000")
	UpstreamAddr string

	// Version is the binary version string, used in health check responses.
	Version string

	// TLS configuration
	TLS TLSConfig

	// SecurityHeaders configuration
	SecurityHeaders SecurityHeadersConfig

	// Auth configuration — controls Kratos session validation and flow proxying.
	Auth AuthConfig

	// RateLimit configuration — controls per-IP and per-user rate limiting.
	RateLimit RateLimitConfig

	// Metrics configuration — controls the Prometheus metrics endpoint.
	Metrics MetricsProxyConfig

	// AdminAuth configuration — controls bearer-token protection of
	// /_vibewarden/admin/* endpoints.
	AdminAuth AdminAuthConfig

	// Admin configuration — controls the admin HTTP API server that serves
	// /_vibewarden/admin/* routes via an internal reverse proxy.
	Admin AdminProxyConfig

	// BodySize configuration — controls request body size limiting.
	BodySize BodySizeConfig

	// IPFilter configuration — controls IP-based access control.
	IPFilter IPFilterConfig

	// Resilience configuration — controls upstream timeout and similar
	// protective features.
	Resilience ResilienceConfig

	// Readiness configuration — controls the /_vibewarden/ready endpoint that
	// reports whether all plugins are initialised and the upstream is reachable.
	Readiness ReadinessProxyConfig

	// Compression configuration — controls response body compression.
	Compression CompressionConfig
}

// CompressionConfig holds configuration for response body compression.
// Caddy's native encode handler is used to perform the compression, so no
// additional dependencies are required.
type CompressionConfig struct {
	// Enabled toggles response compression. Defaults to true.
	Enabled bool

	// Algorithms is the ordered list of compression algorithms to offer.
	// Caddy negotiates the best algorithm with the client via Accept-Encoding.
	// Valid values: "gzip", "zstd".
	// Defaults to ["zstd", "gzip"] when empty.
	Algorithms []string
}

// ReadinessProxyConfig holds configuration for exposing the readiness probe
// endpoint through the Caddy reverse proxy.
type ReadinessProxyConfig struct {
	// Enabled toggles the readiness endpoint at /_vibewarden/ready.
	Enabled bool

	// InternalAddr is the host:port of the internal HTTP server that serves
	// the readiness handler (e.g. "127.0.0.1:9093"). Caddy reverse-proxies
	// /_vibewarden/ready to this address.
	// This field must be set when Enabled is true.
	InternalAddr string
}

// ResilienceConfig holds configuration for upstream resilience features.
type ResilienceConfig struct {
	// Timeout is the maximum duration to wait for the upstream application to
	// respond before returning 504 Gateway Timeout.
	// A zero value disables the timeout (no limit).
	Timeout time.Duration

	// CircuitBreaker holds configuration for the circuit breaker middleware.
	CircuitBreaker CircuitBreakerConfig

	// Retry holds configuration for the retry-with-backoff middleware.
	Retry RetryConfig
}

// RetryConfig holds configuration for the retry-with-exponential-backoff middleware.
type RetryConfig struct {
	// Enabled toggles the retry middleware.
	Enabled bool

	// MaxAttempts is the total number of attempts (including the initial request).
	// Must be >= 2 when Enabled is true. Defaults to 3.
	MaxAttempts int

	// InitialBackoff is the wait duration before the first retry.
	// Subsequent retries double the previous wait (capped at MaxBackoff). Defaults to 100ms.
	InitialBackoff time.Duration

	// MaxBackoff is the upper bound on the backoff duration. Defaults to 10s.
	MaxBackoff time.Duration

	// RetryOn is the set of HTTP status codes that should trigger a retry.
	// Defaults to [502, 503, 504].
	RetryOn []int
}

// IPFilterConfig holds configuration for IP-based access control.
type IPFilterConfig struct {
	// Enabled toggles IP filtering.
	Enabled bool

	// Mode selects the filter behaviour: "allowlist" or "blocklist".
	Mode string

	// Addresses is the list of IP addresses or CIDR ranges to evaluate.
	Addresses []string

	// TrustProxyHeaders, when true, reads X-Forwarded-For for the real client IP.
	TrustProxyHeaders bool
}

// BodySizeConfig holds configuration for the request body size limiting middleware.
type BodySizeConfig struct {
	// Enabled toggles body size limiting.
	Enabled bool

	// MaxBytes is the global default maximum request body size in bytes.
	// Requests with a larger body receive 413 Payload Too Large.
	// A value of 0 means no limit.
	MaxBytes int64

	// Overrides defines per-path body size limits that take precedence over MaxBytes.
	Overrides []BodySizeOverride
}

// BodySizeOverride defines a path-specific body size limit.
type BodySizeOverride struct {
	// Path is the URL path prefix to match (e.g. "/api/upload").
	Path string

	// MaxBytes is the maximum request body size for this path in bytes.
	// A value of 0 means no limit for this path.
	MaxBytes int64
}

// AdminProxyConfig holds configuration for exposing the admin API through
// the Caddy reverse proxy.
type AdminProxyConfig struct {
	// Enabled toggles the admin API routes at /_vibewarden/admin/*.
	Enabled bool

	// InternalAddr is the host:port of the internal HTTP server that serves
	// the admin API handlers (e.g., "127.0.0.1:9092"). Caddy reverse-proxies
	// /_vibewarden/admin/* to this address.
	// This field must be set when Enabled is true.
	InternalAddr string
}

// MetricsProxyConfig holds configuration for exposing the Prometheus metrics
// endpoint through the Caddy reverse proxy.
type MetricsProxyConfig struct {
	// Enabled toggles the metrics endpoint at /_vibewarden/metrics.
	Enabled bool

	// InternalAddr is the host:port of the internal HTTP server that serves the
	// Prometheus handler (e.g., "127.0.0.1:9091"). Caddy reverse-proxies
	// /_vibewarden/metrics to this address.
	// This field must be set when Enabled is true.
	InternalAddr string
}

// TLSProvider identifies how TLS certificates are provisioned.
// Use the TLSProvider* constants for valid values.
type TLSProvider string

const (
	// TLSProviderLetsEncrypt provisions certificates automatically via ACME (Let's Encrypt).
	TLSProviderLetsEncrypt TLSProvider = "letsencrypt"

	// TLSProviderSelfSigned instructs Caddy to generate a self-signed certificate.
	// Intended for local development and testing only.
	TLSProviderSelfSigned TLSProvider = "self-signed"

	// TLSProviderExternal means the operator supplies the certificate and key files.
	// Use CertPath and KeyPath to specify the file paths.
	TLSProviderExternal TLSProvider = "external"
)

// TLSConfig holds TLS-specific settings.
type TLSConfig struct {
	// Enabled toggles TLS termination.
	Enabled bool

	// Provider selects how certificates are provisioned.
	// Valid values: "letsencrypt", "self-signed", "external".
	// Defaults to "self-signed" when empty and Enabled is true.
	Provider TLSProvider

	// Domain is the hostname for certificate provisioning.
	// Required when Provider is TLSProviderLetsEncrypt.
	Domain string

	// CertPath is the path to a PEM-encoded certificate file.
	// Required when Provider is TLSProviderExternal.
	CertPath string

	// KeyPath is the path to a PEM-encoded private key file.
	// Required when Provider is TLSProviderExternal.
	KeyPath string

	// StoragePath is where Caddy stores ACME certificates on disk.
	// Uses the Caddy default when empty (applicable to TLSProviderLetsEncrypt only).
	StoragePath string
}

// SecurityHeadersConfig holds security header settings.
type SecurityHeadersConfig struct {
	// Enabled toggles security headers middleware
	Enabled bool

	// HSTSMaxAge is the max-age in seconds (default: 31536000 = 1 year)
	HSTSMaxAge int
	// HSTSIncludeSubDomains includes the includeSubDomains directive
	HSTSIncludeSubDomains bool
	// HSTSPreload includes the preload directive
	HSTSPreload bool

	// ContentTypeNosniff sets X-Content-Type-Options: nosniff
	ContentTypeNosniff bool

	// FrameOption sets X-Frame-Options value: "DENY", "SAMEORIGIN", or "" (disabled)
	FrameOption string

	// ContentSecurityPolicy sets the Content-Security-Policy value (empty = disabled)
	ContentSecurityPolicy string

	// ReferrerPolicy sets the Referrer-Policy value (empty = disabled)
	ReferrerPolicy string

	// PermissionsPolicy sets the Permissions-Policy value (empty = disabled)
	PermissionsPolicy string

	// CrossOriginOpenerPolicy sets the Cross-Origin-Opener-Policy value.
	// Recommended: "same-origin". Empty string disables the header.
	CrossOriginOpenerPolicy string

	// CrossOriginResourcePolicy sets the Cross-Origin-Resource-Policy value.
	// Recommended: "same-origin". Empty string disables the header.
	CrossOriginResourcePolicy string

	// PermittedCrossDomainPolicies sets the X-Permitted-Cross-Domain-Policies value.
	// Recommended: "none". Empty string disables the header.
	PermittedCrossDomainPolicies string

	// SuppressViaHeader, when true, removes the Via header that Caddy's
	// reverse proxy adds to forwarded responses. Suppressing this header
	// reduces information disclosure about the proxy infrastructure.
	SuppressViaHeader bool
}
