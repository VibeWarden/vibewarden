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

	// TLS configuration
	TLS TLSConfig

	// SecurityHeaders configuration
	SecurityHeaders SecurityHeadersConfig
}

// TLSConfig holds TLS-specific settings.
type TLSConfig struct {
	// Enabled toggles TLS termination
	Enabled bool

	// Domain for certificate provisioning (required if Enabled && AutoCert)
	Domain string

	// AutoCert enables automatic certificate provisioning via ACME (Let's Encrypt)
	AutoCert bool

	// CertPath is the path to a custom certificate file (if not using AutoCert)
	CertPath string

	// KeyPath is the path to the private key file (if not using AutoCert)
	KeyPath string

	// StoragePath is where Caddy stores certificates (default: system-specific)
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
