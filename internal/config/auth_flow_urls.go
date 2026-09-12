package config

import (
	"fmt"
	"strings"
)

// Accepted values for auth.ui.mode.
const (
	// AuthUIModeBuiltIn means VibeWarden serves its own auth pages under
	// /_vibewarden/. This is the default.
	AuthUIModeBuiltIn = "built-in"
	// AuthUIModeCustom means the operator serves their own auth pages at the
	// URLs configured under auth.ui.*_url.
	AuthUIModeCustom = "custom"
)

// Built-in auth UI paths served by the sidecar.
//
// These must stay in sync with the routes registered by
// internal/adapters/authui (Handler.registerRoutes) and with the path match
// list contributed by internal/plugins/auth (ContributeCaddyRoutes). A path
// emitted here that the sidecar does not serve falls through to the upstream
// app, which silently breaks the flow — see issue #1516. The invariant is
// pinned by test/architecture/kratos_ui_urls_test.go.
const (
	// AuthUIPathLogin is the built-in login page path.
	AuthUIPathLogin = "/_vibewarden/login"
	// AuthUIPathRegistration is the built-in registration page path.
	AuthUIPathRegistration = "/_vibewarden/registration"
	// AuthUIPathRecovery is the built-in account recovery page path.
	AuthUIPathRecovery = "/_vibewarden/recovery"
	// AuthUIPathVerification is the built-in email verification page path.
	AuthUIPathVerification = "/_vibewarden/verification"
	// AuthUIPathSettings is the built-in account settings page path.
	AuthUIPathSettings = "/_vibewarden/settings"
)

// AuthFlowURLs holds the absolute browser URLs Kratos redirects to for each
// self-service flow. They are rendered into the generated kratos.yml as the
// selfservice.flows.*.ui_url values.
type AuthFlowURLs struct {
	// Login is the ui_url of the login flow.
	Login string
	// Registration is the ui_url of the registration flow.
	Registration string
	// Recovery is the ui_url of the account recovery flow.
	Recovery string
	// Verification is the ui_url of the email verification flow.
	Verification string
	// Settings is the ui_url of the account settings flow.
	Settings string
	// Error is the ui_url of the error flow.
	Error string
	// LogoutReturn is the browser return URL after a successful logout.
	LogoutReturn string
}

// PublicBaseURL returns the scheme://host[:port] the sidecar is reachable at
// from a browser. When tls.domain is set the public URL is https://<domain>;
// otherwise it is localhost on server.port, with the scheme following
// tls.enabled.
func (c *Config) PublicBaseURL() string {
	if c.TLS.Domain != "" {
		return "https://" + c.TLS.Domain
	}
	scheme := "http"
	if c.TLS.Enabled {
		scheme = "https"
	}
	return fmt.Sprintf("%s://localhost:%d", scheme, c.Server.Port)
}

// AuthFlowURLs returns the absolute ui_url for every Kratos self-service flow.
//
// In the default "built-in" UI mode the URLs point at the auth pages the
// sidecar itself serves under /_vibewarden/. In "custom" mode they point at the
// operator's own pages configured under auth.ui.*; a custom URL may be absolute
// (https://app.example.com/login) or a path (/login), in which case it is
// resolved against PublicBaseURL.
//
// Two flows have no page of their own and always fall back to the login URL:
// the error flow (the sidecar serves no error page, and sending the user back
// to login is recoverable, whereas a path the sidecar does not serve falls
// through to the upstream app) and, in custom mode only, the verification flow
// (auth.ui has no verification_url key).
func (c *Config) AuthFlowURLs() AuthFlowURLs {
	base := c.PublicBaseURL()

	if c.Auth.UI.Mode != AuthUIModeCustom {
		login := base + AuthUIPathLogin
		return AuthFlowURLs{
			Login:        login,
			Registration: base + AuthUIPathRegistration,
			Recovery:     base + AuthUIPathRecovery,
			Verification: base + AuthUIPathVerification,
			Settings:     base + AuthUIPathSettings,
			Error:        login,
			LogoutReturn: login,
		}
	}

	// Custom mode. auth.ui.login_url is required by Validate, so it is the
	// fallback for every flow whose own URL is not configured.
	login := resolveUIURL(base, c.Auth.UI.LoginURL, "")
	return AuthFlowURLs{
		Login:        login,
		Registration: resolveUIURL(base, c.Auth.UI.RegistrationURL, login),
		Recovery:     resolveUIURL(base, c.Auth.UI.RecoveryURL, login),
		Verification: login,
		Settings:     resolveUIURL(base, c.Auth.UI.SettingsURL, login),
		Error:        login,
		LogoutReturn: login,
	}
}

// resolveUIURL turns a configured auth.ui URL into an absolute URL. Absolute
// http(s) URLs are returned unchanged; a path is joined onto base. An empty
// raw value yields fallback.
func resolveUIURL(base, raw, fallback string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fallback
	}
	lower := strings.ToLower(raw)
	if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") {
		return raw
	}
	return base + "/" + strings.TrimPrefix(raw, "/")
}
