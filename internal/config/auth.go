package config

import (
	"fmt"
	"regexp"
	"time"

	"github.com/vibewarden/vibewarden/internal/domain/identity"
)

// hexColorRE matches valid CSS hex color values: #RGB or #RRGGBB.
var hexColorRE = regexp.MustCompile(`^#([0-9A-Fa-f]{3}|[0-9A-Fa-f]{6})$`)

// KratosSMTPConfig holds SMTP settings used by Ory Kratos to send emails.
type KratosSMTPConfig struct {
	// Host is the SMTP server hostname (default: "localhost").
	Host string `mapstructure:"host"`
	// Port is the SMTP server port (default: 1025).
	Port int `mapstructure:"port"`
	// From is the sender address for Kratos emails (default: "no-reply@vibewarden.local").
	From string `mapstructure:"from"`
}

// KratosConfig holds Ory Kratos connection settings.
// These values are used both for the auth middleware and for generating
// the Kratos config file under .vibewarden/generated/.
type KratosConfig struct {
	// PublicURL is the Kratos public API URL (default: "http://127.0.0.1:4433")
	PublicURL string `mapstructure:"public_url"`
	// AdminURL is the Kratos admin API URL (default: "http://127.0.0.1:4434")
	AdminURL string `mapstructure:"admin_url"`
	// DSN is the data source name for the Kratos database.
	// Example: "postgres://kratos:secret@localhost:5432/kratos?sslmode=disable"
	DSN string `mapstructure:"dsn"`
	// SMTP holds email delivery settings for Kratos.
	SMTP KratosSMTPConfig `mapstructure:"smtp"`
	// External indicates that VibeWarden should connect to a user-managed Kratos
	// instance instead of spinning up a local one.
	// When true, the generated Docker Compose file omits the kratos,
	// kratos-migrate, kratos-db, and seed-users containers.
	// Requires PublicURL and AdminURL to be set to the external instance URLs.
	// Default: false.
	External bool `mapstructure:"external"`
}

// SupportedSocialProviders is the set of accepted provider names for social login.
// The special value "oidc" indicates a generic OpenID Connect provider.
var SupportedSocialProviders = map[string]bool{
	"google":    true,
	"github":    true,
	"apple":     true,
	"facebook":  true,
	"microsoft": true,
	"gitlab":    true,
	"discord":   true,
	"slack":     true,
	"spotify":   true,
	"oidc":      true,
}

// SocialProviderConfig holds OAuth2/OIDC settings for a single social login provider.
// It is used as an element of AuthConfig.SocialProviders.
type SocialProviderConfig struct {
	// Provider is the provider name.
	// Accepted values: google, github, apple, facebook, microsoft, gitlab, discord, slack, spotify, oidc.
	Provider string `mapstructure:"provider"`

	// ClientID is the OAuth2 client ID issued by the provider. Required.
	ClientID string `mapstructure:"client_id"`

	// ClientSecret is the OAuth2 client secret issued by the provider. Required.
	// Supports environment variable substitution via ${VAR} syntax in the YAML file.
	ClientSecret string `mapstructure:"client_secret"`

	// Scopes is an optional list of OAuth2 scopes to request.
	// When empty, provider-specific defaults are used.
	Scopes []string `mapstructure:"scopes"`

	// Label is an optional custom label shown on the login button (e.g. "Sign in with Acme").
	Label string `mapstructure:"label"`

	// TeamID is the Apple Developer Team ID. Required when Provider is "apple".
	TeamID string `mapstructure:"team_id"`

	// KeyID is the Apple private key ID. Required when Provider is "apple".
	KeyID string `mapstructure:"key_id"`

	// ID is the unique identifier for the OIDC provider entry (e.g. "acme-oidc").
	// Required when Provider is "oidc".
	ID string `mapstructure:"id"`

	// IssuerURL is the OIDC issuer URL (e.g. "https://accounts.google.com").
	// Required when Provider is "oidc".
	IssuerURL string `mapstructure:"issuer_url"`
}

// AuthUIConfig holds theme and URL settings for the built-in authentication UI.
// It configures the visual appearance of the login, registration, recovery, and
// settings pages rendered by VibeWarden, as well as the optional custom URL
// overrides used when mode is "custom".
type AuthUIConfig struct {
	// Mode selects whether VibeWarden renders its own auth pages or defers to
	// custom URLs. Accepted values: "built-in" (default) or "custom".
	Mode string `mapstructure:"mode"`

	// AppName is the application name shown on the built-in login page.
	AppName string `mapstructure:"app_name"`

	// LogoURL is an optional URL to a logo image displayed on built-in pages.
	LogoURL string `mapstructure:"logo_url"`

	// PrimaryColor is the accent color used on built-in pages (hex, default: "#7C3AED").
	PrimaryColor string `mapstructure:"primary_color"`

	// BackgroundColor is the page background color for built-in pages (hex, default: "#1a1a2e").
	BackgroundColor string `mapstructure:"background_color"`

	// LoginURL is the URL of the custom login page.
	// Required when Mode is "custom".
	LoginURL string `mapstructure:"login_url"`

	// RegistrationURL is the URL of the custom registration page.
	// Only used when Mode is "custom".
	RegistrationURL string `mapstructure:"registration_url"`

	// SettingsURL is the URL of the custom account settings page.
	// Only used when Mode is "custom".
	SettingsURL string `mapstructure:"settings_url"`

	// RecoveryURL is the URL of the custom account recovery page.
	// Only used when Mode is "custom".
	RecoveryURL string `mapstructure:"recovery_url"`
}

// AuthMode selects the active authentication strategy for incoming requests.
type AuthMode string

const (
	// AuthModeKratos uses Ory Kratos session-cookie authentication.
	// Users must opt in explicitly by setting auth.mode: "kratos".
	AuthModeKratos AuthMode = "kratos"

	// AuthModeJWT uses JWT/OIDC Bearer token authentication.
	// Configure the jwt.* sub-section when using this mode.
	AuthModeJWT AuthMode = "jwt"

	// AuthModeAPIKey uses API key header authentication.
	AuthModeAPIKey AuthMode = "api-key"

	// AuthModeNone disables authentication entirely. Use only in trusted
	// environments or when authentication is handled upstream.
	AuthModeNone AuthMode = "none"
)

// JWTConfig holds JWT/OIDC authentication settings.
// It is used when auth.mode is "jwt".
type JWTConfig struct {
	// JWKSURL is the URL to fetch the JSON Web Key Set.
	// Mutually exclusive with IssuerURL: if both are set, JWKSURL takes precedence.
	// Example: "https://example.auth0.com/.well-known/jwks.json"
	JWKSURL string `mapstructure:"jwks_url"`

	// IssuerURL is the OIDC issuer URL for auto-discovery.
	// When set (and JWKSURL is empty), the JWKS URL is discovered from
	// /.well-known/openid-configuration.
	// Example: "https://example.auth0.com/"
	IssuerURL string `mapstructure:"issuer_url"`

	// Issuer is the expected "iss" claim value.
	// Required when mode is "jwt".
	// Example: "https://example.auth0.com/"
	Issuer string `mapstructure:"issuer"`

	// Audience is the expected "aud" claim value.
	// Required when mode is "jwt".
	// Example: "my-api"
	Audience string `mapstructure:"audience"`

	// ClaimsToHeaders maps JWT claim names to HTTP header names.
	// The mapped claims are injected into requests forwarded to the upstream app.
	// Default: {"sub": "X-User-Id", "email": "X-User-Email", "email_verified": "X-User-Verified"}
	// Example:
	//   claims_to_headers:
	//     name: X-User-Name
	//     roles: X-User-Roles
	ClaimsToHeaders map[string]string `mapstructure:"claims_to_headers"`

	// AllowedAlgorithms restricts which signing algorithms are accepted.
	// Default: ["RS256", "ES256"].
	// Never include "none" or symmetric algorithms (HS256) in production.
	AllowedAlgorithms []string `mapstructure:"allowed_algorithms"`

	// CacheTTL is how long to cache the JWKS before refreshing.
	// Accepts Go duration strings (e.g. "1h", "30m"). Default: "1h".
	CacheTTL time.Duration `mapstructure:"cache_ttl"`
}

// AuthAPIKeyConfig holds settings specific to the API key authentication mode.
type AuthAPIKeyConfig struct {
	// Header is the request header from which the API key is extracted.
	// Defaults to "X-API-Key" when empty.
	Header string `mapstructure:"header"`

	// Keys is the list of pre-shared API keys recognized by the validator.
	// Each entry must supply a name, a SHA-256 hex hash of the plaintext key,
	// and an optional list of scopes.
	Keys []APIKeyEntry `mapstructure:"keys"`

	// OpenBaoPath is the KV path inside OpenBao where API keys are stored.
	// When set, the OpenBao adapter is used instead of the static config adapter.
	// The KV secret at this path must contain string fields whose keys are the
	// key names and whose values are the SHA-256 hex hashes of the plaintext keys.
	// Example: openbao_path: "auth/api-keys"
	OpenBaoPath string `mapstructure:"openbao_path"`

	// CacheTTL is how long the keys fetched from OpenBao are held in memory
	// before the next refresh. Accepts Go duration strings (e.g. "5m", "1h").
	// Defaults to "5m". Ignored when OpenBaoPath is empty.
	CacheTTL string `mapstructure:"cache_ttl"`

	// ScopeRules is an ordered list of path+method authorization rules applied
	// after successful key validation. The first matching rule determines the
	// required scopes. When no rule matches, the request is allowed.
	//
	// Example:
	//   scope_rules:
	//     - path: "/api/v1/*"
	//       methods: [GET, HEAD]
	//       required_scopes: ["read"]
	//     - path: "/admin/*"
	//       required_scopes: ["admin"]
	ScopeRules []ScopeRuleConfig `mapstructure:"scope_rules"`
}

// ScopeRuleConfig describes a single scope-based authorization rule for API
// key requests. Rules are evaluated in order; the first match wins.
type ScopeRuleConfig struct {
	// Path is a glob pattern (stdlib path.Match syntax) matched against the
	// request URL path. Example: "/api/v1/*"
	Path string `mapstructure:"path"`

	// Methods is the set of HTTP methods this rule applies to (e.g. ["GET",
	// "HEAD"]). When empty, the rule applies to all HTTP methods.
	Methods []string `mapstructure:"methods"`

	// RequiredScopes is the set of scope strings the API key must possess for
	// the request to be permitted.
	RequiredScopes []string `mapstructure:"required_scopes"`
}

// APIKeyEntry represents a single registered API key in the configuration.
type APIKeyEntry struct {
	// Name is a human-readable label for the key (e.g. "ci-deploy").
	Name string `mapstructure:"name"`

	// Hash is the hex-encoded SHA-256 digest of the plaintext key.
	// Generate with: echo -n "<key>" | sha256sum
	Hash string `mapstructure:"hash"`

	// Scopes is an optional list of permission scopes granted to this key.
	Scopes []string `mapstructure:"scopes"`
}

// AuthConfig holds auth middleware settings.
//
// Mode is the single source of truth for whether authentication is on.
// Set Mode to "none" (or leave it empty) to disable authentication entirely;
// set it to "kratos", "jwt", or "api-key" to enable the matching strategy.
// Use the Active method to derive the on/off state at call sites.
//
// The legacy auth.enabled field was removed in v0.11.0 per ADR-065. The
// config loader explicitly rejects any YAML that still sets it.
type AuthConfig struct {
	// Mode selects the authentication strategy.
	// Accepted values: "none" (default), "kratos", "jwt", "api-key".
	// "none" disables all authentication — use only in trusted environments.
	// "kratos" activates Ory Kratos session-cookie authentication.
	// When empty, "none" is used.
	Mode AuthMode `mapstructure:"mode"`

	// JWT holds settings used when Mode is "jwt".
	JWT JWTConfig `mapstructure:"jwt"`

	// APIKey holds settings used when Mode is "api-key".
	APIKey AuthAPIKeyConfig `mapstructure:"api_key"`

	// IdentitySchema selects the identity schema to use.
	// Accepted values: "email_password" (default), "email_only", "username_password",
	// "social", or a filesystem path to a custom JSON schema file.
	// When social_providers are configured and this field is left at its default
	// ("email_password"), the generate service automatically upgrades to the
	// "social" schema so that name and picture traits are available.
	IdentitySchema string `mapstructure:"identity_schema"`

	// PublicPaths is a list of URL path glob patterns that bypass auth.
	// The /_vibewarden/* prefix is always public (added automatically).
	// Supports * for single-segment wildcards (e.g. "/static/*").
	PublicPaths []string `mapstructure:"public_paths"`

	// SessionCookieName is the name of the Kratos session cookie.
	// Defaults to "ory_kratos_session".
	SessionCookieName string `mapstructure:"session_cookie_name"`

	// LoginURL is the redirect destination for unauthenticated users.
	// Defaults to "/self-service/login/browser" when empty.
	LoginURL string `mapstructure:"login_url"`

	// OnKratosUnavailable controls behavior when Kratos cannot be reached.
	// Accepted values:
	//   "503"          (default) — return 503 for all protected requests (fail-closed).
	//   "allow_public" — serve requests to public paths; block protected paths with 503.
	OnKratosUnavailable string `mapstructure:"on_kratos_unavailable"`

	// SocialProviders is a list of OAuth2/OIDC social login providers to enable.
	// Each entry requires at minimum a provider name, client_id, and client_secret.
	SocialProviders []SocialProviderConfig `mapstructure:"social_providers"`

	// RolePaths maps role names to URL path patterns that require that role.
	// When configured, authenticated users whose Kratos identity trait "role"
	// does not match the required role for a path receive HTTP 403 Forbidden.
	// Only valid when Mode is "kratos".
	//
	// Example:
	//   role_paths:
	//     admin:
	//       - /admin/*
	//     moderator:
	//       - /admin/moderation/*
	RolePaths map[string][]string `mapstructure:"role_paths"`

	// SeedDemoUsers controls whether the bundle generator writes
	// scripts/seed-users.sh into the generated output directory.
	// When true (and auth.mode is "kratos" and kratos.external is false),
	// the script is generated and mounted into the seed-users init container.
	// It seeds demo Kratos identities (demo@vibewarden.dev, alice@vibewarden.dev)
	// on first boot and is only useful for demos or local testing.
	//
	// Default: false. Never enable in production.
	SeedDemoUsers bool `mapstructure:"seed_demo_users"`

	// UI holds theme and URL settings for the built-in or custom auth pages.
	UI AuthUIConfig `mapstructure:"ui"`
}

// Active reports whether the authentication middleware should be active.
//
// Auth is active when Mode is a non-empty, non-"none" value. An empty Mode
// (which viper normalises to AuthModeNone via SetDefault) and AuthModeNone
// both mean "auth disabled".
//
// This helper is the single derivation for "is auth on" and every consumer
// that needs that signal should call it rather than testing Mode directly.
// See ADR-065.
func (a AuthConfig) Active() bool {
	return a.Mode != "" && a.Mode != AuthModeNone
}

// validateAuth validates auth-related configuration fields and returns a slice
// of error strings (one per violation). An empty slice means the configuration
// is valid.
func validateAuth(c *Config) []string {
	var errs []string

	// Social providers: validate each entry.
	for i, sp := range c.Auth.SocialProviders {
		prefix := fmt.Sprintf("social_providers[%d]", i)

		if !SupportedSocialProviders[sp.Provider] {
			errs = append(errs, fmt.Sprintf("%s.provider %q is not supported; accepted values: google, github, apple, facebook, microsoft, gitlab, discord, slack, spotify, oidc", prefix, sp.Provider))
		}
		if sp.ClientID == "" {
			errs = append(errs, fmt.Sprintf("%s.client_id is required", prefix))
		}
		if sp.ClientSecret == "" {
			errs = append(errs, fmt.Sprintf("%s.client_secret is required", prefix))
		}
		if sp.Provider == "apple" {
			if sp.TeamID == "" {
				errs = append(errs, fmt.Sprintf("%s.team_id is required for provider \"apple\"", prefix))
			}
			if sp.KeyID == "" {
				errs = append(errs, fmt.Sprintf("%s.key_id is required for provider \"apple\"", prefix))
			}
		}
		if sp.Provider == "oidc" {
			if sp.ID == "" {
				errs = append(errs, fmt.Sprintf("%s.id is required for provider \"oidc\"", prefix))
			}
			if sp.IssuerURL == "" {
				errs = append(errs, fmt.Sprintf("%s.issuer_url is required for provider \"oidc\"", prefix))
			}
		}
	}

	// auth.mode validation. Empty string is accepted here for robustness
	// (SetDefault ensures "none" is used in practice).
	switch c.Auth.Mode {
	case "", AuthModeKratos, AuthModeJWT, AuthModeAPIKey, AuthModeNone:
		// valid
	default:
		errs = append(errs, fmt.Sprintf(
			"auth.mode %q is invalid; accepted values: \"none\", \"kratos\", \"jwt\", \"api-key\" — "+
				"set auth.mode to one of those values (use \"none\" to disable authentication)",
			c.Auth.Mode,
		))
	}

	// auth.jwt validation (only when mode is "jwt").
	if c.Auth.Mode == AuthModeJWT {
		jwt := c.Auth.JWT
		devJWKSMode := jwt.JWKSURL == "" && jwt.IssuerURL == ""

		// In prod profile, local dev JWKS mode is not allowed — a real JWKS URL
		// or OIDC issuer URL is required for production deployments.
		if devJWKSMode && c.Profile == "prod" {
			errs = append(errs, "auth.jwt: either jwks_url or issuer_url is required when auth.mode is \"jwt\" and profile is \"prod\"")
		}

		// Issuer and audience are required when a real JWKS source is configured.
		// In dev JWKS mode they default to "vibewarden-dev" and "dev" respectively.
		if !devJWKSMode {
			if jwt.Issuer == "" {
				errs = append(errs, "auth.jwt.issuer is required when auth.mode is \"jwt\"")
			}
			if jwt.Audience == "" {
				errs = append(errs, "auth.jwt.audience is required when auth.mode is \"jwt\"")
			}
		}
	}

	// kratos.external validation: when external is true, public_url and admin_url
	// must point to the external instance (they cannot be empty).
	if c.Auth.Mode == AuthModeKratos && c.Kratos.External {
		if c.Kratos.PublicURL == "" {
			errs = append(errs, "kratos.public_url is required when kratos.external is true")
		}
		if c.Kratos.AdminURL == "" {
			errs = append(errs, "kratos.admin_url is required when kratos.external is true")
		}
	}

	// auth.on_kratos_unavailable validation.
	if c.Auth.OnKratosUnavailable != "" &&
		c.Auth.OnKratosUnavailable != "503" &&
		c.Auth.OnKratosUnavailable != "allow_public" {
		errs = append(errs, fmt.Sprintf(
			"auth.on_kratos_unavailable %q is invalid; accepted values: \"503\", \"allow_public\"",
			c.Auth.OnKratosUnavailable,
		))
	}

	// auth.role_paths validation: only valid when mode is "kratos".
	if len(c.Auth.RolePaths) > 0 && c.Auth.Mode != AuthModeKratos {
		errs = append(errs, "auth.role_paths is only valid when auth.mode is \"kratos\"")
	}
	// Validate that role names in role_paths are recognised values.
	// Delegates to identity.NewRole to keep the valid-role list in one place.
	for roleName, paths := range c.Auth.RolePaths {
		if _, err := identity.NewRole(roleName); err != nil {
			errs = append(errs, fmt.Sprintf(
				"auth.role_paths: role %q is invalid; accepted values: user, admin, moderator",
				roleName,
			))
		}
		if len(paths) == 0 {
			errs = append(errs, fmt.Sprintf("auth.role_paths.%s: must have at least one path", roleName))
		}
	}

	// Auth UI validation.
	ui := c.Auth.UI
	if ui.Mode != "" && ui.Mode != "built-in" && ui.Mode != "custom" {
		errs = append(errs, fmt.Sprintf("auth.ui.mode %q is invalid; accepted values: \"built-in\", \"custom\"", ui.Mode))
	}
	if ui.PrimaryColor != "" && !hexColorRE.MatchString(ui.PrimaryColor) {
		errs = append(errs, fmt.Sprintf("auth.ui.primary_color %q is not a valid hex color (expected #RGB or #RRGGBB)", ui.PrimaryColor))
	}
	if ui.BackgroundColor != "" && !hexColorRE.MatchString(ui.BackgroundColor) {
		errs = append(errs, fmt.Sprintf("auth.ui.background_color %q is not a valid hex color (expected #RGB or #RRGGBB)", ui.BackgroundColor))
	}
	if ui.Mode == "custom" && ui.LoginURL == "" {
		errs = append(errs, "auth.ui.login_url is required when auth.ui.mode is \"custom\"")
	}

	return errs
}
