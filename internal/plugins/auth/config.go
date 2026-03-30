// Package auth implements the VibeWarden auth plugin.
//
// The auth plugin encapsulates all Ory Kratos wiring: session validation,
// Kratos self-service flow proxying, and identity header injection.
// It implements ports.Plugin and ports.CaddyContributor.
package auth

// Mode selects the active authentication strategy for the auth plugin.
// It mirrors config.AuthMode but is defined here to avoid importing the
// config package from the plugins layer.
type Mode string

const (
	// ModeNone disables authentication entirely. This is the default.
	ModeNone Mode = "none"

	// ModeKratos activates Ory Kratos session-cookie authentication.
	// Requires KratosPublicURL to be set.
	ModeKratos Mode = "kratos"

	// ModeJWT activates JWT/OIDC Bearer token authentication.
	ModeJWT Mode = "jwt"

	// ModeAPIKey activates API key header authentication.
	ModeAPIKey Mode = "api-key"
)

// JWTPluginConfig holds JWT-specific settings for the auth plugin.
// It mirrors a subset of config.JWTConfig but is defined here to keep the
// plugins layer free of a direct config package dependency.
type JWTPluginConfig struct {
	// JWKSURL is the URL to fetch the JSON Web Key Set.
	// When empty and Mode is ModeJWT, VibeWarden auto-generates a local dev
	// key pair and serves a JWKS endpoint at /_vibewarden/jwks.json.
	JWKSURL string

	// IssuerURL is the OIDC issuer URL for auto-discovery.
	IssuerURL string

	// Issuer is the expected "iss" claim value.
	// Defaults to DevIssuer ("vibewarden-dev") when empty in dev mode.
	Issuer string

	// Audience is the expected "aud" claim value.
	// Defaults to DevAudience ("dev") when empty in dev mode.
	Audience string

	// DevKeyDir is the directory where the auto-generated dev key pair is
	// stored. Defaults to ".vibewarden/dev-keys" when empty.
	DevKeyDir string
}

// Config holds all settings for the auth plugin.
// It maps to the plugins.auth section of vibewarden.yaml.
//
// Legacy fallback: when this section is absent the plugin reads from the
// top-level kratos.* and auth.* keys and emits a deprecation warning.
type Config struct {
	// Enabled toggles the auth plugin. When false all methods are no-ops.
	Enabled bool

	// Mode selects the authentication strategy.
	// Accepted values: ModeNone (default), ModeKratos, ModeJWT, ModeAPIKey.
	// Kratos-specific initialisation (URL validation, health checks, Caddy routes)
	// is only performed when Mode is ModeKratos.
	Mode Mode

	// JWT holds settings used when Mode is ModeJWT.
	// When JWT.JWKSURL is empty, local dev JWKS mode is activated automatically.
	JWT JWTPluginConfig

	// KratosPublicURL is the base URL of the Kratos public API
	// (e.g. "http://127.0.0.1:4433"). Used for session validation and
	// to proxy self-service flow requests.
	// Required when Enabled is true and Mode is ModeKratos.
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
	//
	// When Mode is "custom", the built-in auth UI server is not started and no
	// /_vibewarden/ui routes are contributed to Caddy. The auth middleware
	// instead redirects unauthenticated requests to the operator-supplied
	// LoginURL. RegistrationURL, SettingsURL, and RecoveryURL are optional.
	Mode string

	// LoginURL is the URL unauthenticated users are redirected to when
	// Mode is "custom". Required when Mode is "custom".
	// Ignored when Mode is "built-in".
	LoginURL string

	// RegistrationURL is the URL for the registration page when Mode is
	// "custom". Optional.
	RegistrationURL string

	// SettingsURL is the URL for the account settings page when Mode is
	// "custom". Optional.
	SettingsURL string

	// RecoveryURL is the URL for the account recovery page when Mode is
	// "custom". Optional.
	RecoveryURL string

	// PrimaryColor is the CSS color value for the --vw-primary custom property.
	// Defaults to "#7C3AED" (VibeWarden purple) when empty.
	// Only used when Mode is "built-in".
	PrimaryColor string

	// BackgroundColor is the CSS color value for the --vw-bg custom property.
	// Defaults to "#F3F4F6" when empty.
	// Only used when Mode is "built-in".
	BackgroundColor string

	// TextColor is the CSS color value for the --vw-text custom property.
	// Defaults to "#111827" when empty.
	// Only used when Mode is "built-in".
	TextColor string

	// ErrorColor is the CSS color value for the --vw-error custom property.
	// Defaults to "#DC2626" when empty.
	// Only used when Mode is "built-in".
	ErrorColor string
}

// defaultSessionCookieName is used when SessionCookieName is not set.
const defaultSessionCookieName = "ory_kratos_session"

// defaultLoginURL is the Kratos self-service browser login flow URL.
const defaultLoginURL = "/self-service/login/browser"
