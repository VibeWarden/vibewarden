// Package ports defines the interfaces (ports) for VibeWarden's hexagonal architecture.
package ports

import "context"

// Identity represents an authenticated user's identity information.
// This is a read-only view of the user's identity from the auth provider.
type Identity struct {
	// ID is the unique identifier for the user (Kratos identity UUID).
	ID string

	// Email is the user's primary email address.
	Email string

	// EmailVerified indicates whether the email has been verified.
	EmailVerified bool

	// Traits contains additional identity attributes from the schema.
	Traits map[string]any
}

// Session represents an authenticated session.
type Session struct {
	// ID is the session identifier.
	ID string

	// Identity is the user's identity information.
	Identity Identity

	// Active indicates whether the session is currently active.
	Active bool

	// AuthenticatedAt is when the session was authenticated (RFC3339 format).
	AuthenticatedAt string

	// ExpiresAt is when the session expires (RFC3339 format, empty if not set).
	ExpiresAt string
}

// SessionChecker validates sessions against an identity provider.
//
// Deprecated: Use IdentityProvider instead. SessionChecker will be removed in v2.
// The Kratos adapter implements both interfaces during the migration period.
type SessionChecker interface {
	// CheckSession validates the given session cookie and returns the session if valid.
	// Returns ErrSessionInvalid if the session is invalid or expired.
	// Returns ErrSessionNotFound if no session exists for the cookie.
	// Returns ErrAuthProviderUnavailable when the identity provider cannot be reached.
	CheckSession(ctx context.Context, sessionCookie string) (*Session, error)
}

// KratosUnavailableBehavior controls how the auth middleware responds when
// the Kratos identity provider is unreachable.
type KratosUnavailableBehavior string

const (
	// KratosUnavailable503 returns HTTP 503 Service Unavailable for all
	// protected requests when Kratos is down. This is the default (fail-closed).
	KratosUnavailable503 KratosUnavailableBehavior = "503"

	// KratosUnavailableAllowPublic allows requests to public paths through
	// even when Kratos is down, but blocks all protected paths with HTTP 503.
	// This is a softer degradation mode for use cases where public content
	// must remain accessible during identity provider outages.
	KratosUnavailableAllowPublic KratosUnavailableBehavior = "allow_public"
)

// AuthConfig holds configuration for the auth middleware.
type AuthConfig struct {
	// Enabled toggles auth middleware (default: true when Kratos is configured).
	Enabled bool

	// KratosPublicURL is the Kratos public API URL for session validation.
	KratosPublicURL string

	// KratosAdminURL is the Kratos admin API URL (for future admin operations).
	KratosAdminURL string

	// SessionCookieName is the name of the session cookie (default: "ory_kratos_session").
	SessionCookieName string

	// PublicPaths is a list of glob patterns for paths that bypass auth.
	PublicPaths []string

	// LoginURL is the URL to redirect unauthenticated users to.
	// If empty, defaults to the Kratos self-service login flow URL.
	LoginURL string

	// OnKratosUnavailable controls the degradation behavior when Kratos cannot
	// be reached. Accepted values: "503" (default, fail-closed) or
	// "allow_public" (serve public paths, block protected paths with 503).
	OnKratosUnavailable KratosUnavailableBehavior
}
