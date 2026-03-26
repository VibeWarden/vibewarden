// Package auth implements the VibeWarden auth plugin.
//
// The auth plugin encapsulates all Ory Kratos wiring: session validation,
// Kratos self-service flow proxying, and identity header injection.
// It implements ports.Plugin and ports.CaddyContributor.
package auth

// Config holds all settings for the auth plugin.
// It maps to the plugins.auth section of vibewarden.yaml.
//
// Legacy fallback: when this section is absent the plugin reads from the
// top-level kratos.* and auth.* keys and emits a deprecation warning.
type Config struct {
	// Enabled toggles the auth plugin. When false all methods are no-ops.
	Enabled bool

	// KratosPublicURL is the base URL of the Kratos public API
	// (e.g. "http://127.0.0.1:4433"). Used for session validation and
	// to proxy self-service flow requests.
	// Required when Enabled is true.
	KratosPublicURL string

	// KratosAdminURL is the base URL of the Kratos admin API
	// (e.g. "http://127.0.0.1:4434"). Reserved for future admin operations.
	KratosAdminURL string

	// SessionCookieName is the name of the Kratos session cookie.
	// Defaults to "ory_kratos_session".
	SessionCookieName string

	// LoginURL is the URL unauthenticated users are redirected to.
	// Defaults to "/self-service/login/browser" when empty.
	LoginURL string

	// PublicPaths is a list of URL path glob patterns that bypass
	// authentication. The /_vibewarden/* prefix is always public.
	// Supports * for single-segment wildcards (e.g. "/public/*").
	PublicPaths []string

	// IdentitySchema selects the Kratos identity schema.
	// Accepted values: "email_password" (default), "email_only",
	// "username_password", or a filesystem path to a custom JSON file.
	IdentitySchema string

	// UI holds configuration for the built-in auth UI pages.
	// When UI.Mode is "built-in" (the default), VibeWarden serves its own
	// login, registration, recovery, and verification pages.
	// When UI.Mode is "custom", the operator provides their own pages and
	// the built-in handler is not mounted.
	UI UIConfig
}

// UIConfig holds theming and mode settings for the built-in auth UI pages.
// It maps to the plugins.auth.ui section of vibewarden.yaml.
type UIConfig struct {
	// Mode selects the UI serving strategy.
	// Accepted values: "built-in" (default), "custom".
	Mode string

	// PrimaryColor is the CSS color value for the --vw-primary custom property.
	// Defaults to "#7C3AED" (VibeWarden purple) when empty.
	PrimaryColor string

	// BackgroundColor is the CSS color value for the --vw-bg custom property.
	// Defaults to "#F3F4F6" when empty.
	BackgroundColor string

	// TextColor is the CSS color value for the --vw-text custom property.
	// Defaults to "#111827" when empty.
	TextColor string

	// ErrorColor is the CSS color value for the --vw-error custom property.
	// Defaults to "#DC2626" when empty.
	ErrorColor string
}

// defaultSessionCookieName is used when SessionCookieName is not set.
const defaultSessionCookieName = "ory_kratos_session"

// defaultLoginURL is the Kratos self-service browser login flow URL.
const defaultLoginURL = "/self-service/login/browser"
