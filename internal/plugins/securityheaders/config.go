// Package securityheaders implements the VibeWarden security-headers plugin.
//
// The plugin does not inject headers itself: the Caddy adapter emits a global
// security-headers route built from ports.SecurityHeadersConfig, so that
// VibeWarden's own routes (login UI, admin API, Kratos proxy) carry the same
// set as the proxied app. What is left in the plugin is configuration
// validation and lifecycle reporting, so its Config only carries the fields it
// reads. See internal/adapters/caddy/config_routes.go and #1540.
package securityheaders

// Config holds the settings the security-headers plugin itself reads.
// It maps to a subset of the security_headers section of vibewarden.yaml —
// the header values are carried by ports.SecurityHeadersConfig, which is what
// the Caddy adapter builds the actual handler from.
type Config struct {
	// Enabled toggles the security-headers plugin.
	Enabled bool

	// HSTSMaxAge is the Strict-Transport-Security max-age directive value in
	// seconds. A value of 0 disables HSTS even when TLS is enabled.
	// Default: 31536000 (1 year).
	// Only reported at init time; the header is emitted by the Caddy adapter.
	HSTSMaxAge int

	// FrameOption sets the X-Frame-Options response header value.
	// Valid values: "DENY", "SAMEORIGIN", or "" (disabled).
	// Validated by Init; the header is emitted by the Caddy adapter.
	// Default: "DENY".
	FrameOption string
}
