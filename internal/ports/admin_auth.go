package ports

// AdminAuthConfig holds configuration for the admin authentication middleware.
// Admin endpoints are protected by a static bearer token supplied in the
// X-Admin-Key request header.
type AdminAuthConfig struct {
	// Enabled toggles the admin API. When false the middleware returns 404
	// for all /_vibewarden/admin/* requests so the existence of the admin
	// surface is not disclosed.
	Enabled bool

	// Token is the secret bearer token clients must supply in the X-Admin-Key
	// header to access /_vibewarden/admin/* endpoints. Must be non-empty when
	// Enabled is true; if it is empty with Enabled true the middleware returns
	// 500 to surface the misconfiguration.
	Token string

	// ConfigPath is an additional path prefix that the middleware protects
	// with the same bearer token. When empty only AdminPath is protected.
	// Used to guard /_vibewarden/config/* alongside /_vibewarden/admin/*.
	ConfigPath string
}
