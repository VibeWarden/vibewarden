// Package usermgmt implements the VibeWarden user-management plugin.
//
// The user-management plugin migrates the standalone admin API wiring from
// serve.go into the plugin system. It creates a Kratos admin adapter, an
// admin application service, and an internal HTTP server that serves the
// admin user management API. Caddy reverse-proxies /_vibewarden/admin/* to
// the internal server after the admin auth handler validates the bearer token.
package usermgmt

// Config holds all settings for the user-management plugin.
// It maps to the plugins.user-management section of vibewarden.yaml.
type Config struct {
	// Enabled toggles the user-management plugin. When false all methods are no-ops.
	Enabled bool

	// AdminToken is the static bearer token clients must supply in the
	// X-Admin-Key request header to access /_vibewarden/admin/* endpoints.
	// Required when Enabled is true.
	// Can be set via VIBEWARDEN_ADMIN_TOKEN env var.
	AdminToken string

	// KratosAdminURL is the base URL of the Ory Kratos admin API
	// (e.g. "http://127.0.0.1:4434"). Used to manage user identities.
	// Required when Enabled is true.
	KratosAdminURL string

	// DatabaseURL is a libpq-compatible connection string for the audit log
	// database (e.g. "postgres://user:pass@localhost:5432/vibewarden?sslmode=disable").
	// When empty, audit logging is skipped.
	DatabaseURL string
}
