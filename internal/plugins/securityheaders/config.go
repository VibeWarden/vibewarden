// Package securityheaders implements the VibeWarden security-headers plugin.
//
// It injects security-related HTTP response headers (HSTS, X-Frame-Options,
// Content-Security-Policy, etc.) into every response via the Caddy headers
// handler. HSTS is only included when the TLS plugin is also enabled so that
// the header is never sent over plain HTTP connections.
package securityheaders

// Config holds all settings for the security-headers plugin.
// It maps to the plugins.security-headers section of vibewarden.yaml.
type Config struct {
	// Enabled toggles the security-headers plugin.
	Enabled bool

	// HSTSMaxAge is the Strict-Transport-Security max-age directive value in
	// seconds. A value of 0 disables HSTS even when TLS is enabled.
	// Default: 31536000 (1 year).
	HSTSMaxAge int

	// HSTSIncludeSubDomains appends the includeSubDomains directive to the
	// Strict-Transport-Security header.
	// Default: true.
	HSTSIncludeSubDomains bool

	// HSTSPreload appends the preload directive to the
	// Strict-Transport-Security header. Requires manual submission to the HSTS
	// preload list and should only be set after careful consideration.
	// Default: false.
	HSTSPreload bool

	// ContentTypeNosniff, when true, sets the X-Content-Type-Options response
	// header to "nosniff".
	// Default: true.
	ContentTypeNosniff bool

	// FrameOption sets the X-Frame-Options response header value.
	// Valid values: "DENY", "SAMEORIGIN", or "" (disabled).
	// Default: "DENY".
	FrameOption string

	// ContentSecurityPolicy sets the Content-Security-Policy response header
	// value. An empty string (the default) disables the header; users opt in
	// by setting an explicit policy in vibewarden.yaml.
	ContentSecurityPolicy string

	// ReferrerPolicy sets the Referrer-Policy response header value.
	// An empty string disables the header.
	// Default: "strict-origin-when-cross-origin".
	ReferrerPolicy string

	// PermissionsPolicy sets the Permissions-Policy response header value.
	// An empty string disables the header.
	// Default: "".
	PermissionsPolicy string

	// CrossOriginOpenerPolicy sets the Cross-Origin-Opener-Policy response
	// header value. Recommended: "same-origin". An empty string disables the
	// header.
	// Default: "same-origin".
	CrossOriginOpenerPolicy string

	// CrossOriginResourcePolicy sets the Cross-Origin-Resource-Policy response
	// header value. Recommended: "same-origin". An empty string disables the
	// header.
	// Default: "same-origin".
	CrossOriginResourcePolicy string

	// PermittedCrossDomainPolicies sets the X-Permitted-Cross-Domain-Policies
	// response header value. Recommended: "none". An empty string disables the
	// header.
	// Default: "none".
	PermittedCrossDomainPolicies string

	// SuppressViaHeader, when true, removes the Via response header that
	// Caddy's reverse proxy adds. Suppressing this header reduces information
	// disclosure about the proxy infrastructure.
	// Default: true.
	SuppressViaHeader bool
}
