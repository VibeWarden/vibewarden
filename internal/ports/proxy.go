// Package ports defines the interfaces (ports) for VibeWarden's hexagonal architecture.
package ports

import "context"

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
}
