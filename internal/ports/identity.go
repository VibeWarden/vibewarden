package ports

import (
	"context"
	"net/http"

	"github.com/vibewarden/vibewarden/internal/domain/identity"
)

// IdentityProvider validates authentication credentials from an HTTP request
// and returns the authenticated user's identity.
//
// This is the primary authentication port. Implementations include:
//   - Kratos adapter (session cookie validation)
//   - JWT adapter (Bearer token validation)
//   - API key adapter (X-API-Key header validation)
//
// The auth middleware chains multiple IdentityProviders when configured,
// trying each in order until one succeeds or all fail.
type IdentityProvider interface {
	// Name returns the provider identifier (e.g., "kratos", "jwt", "apikey").
	// Used for logging, metrics labels, and the Identity.Provider field.
	Name() string

	// Authenticate extracts credentials from the request and validates them.
	// Returns an AuthResult indicating success or failure.
	//
	// If the provider cannot find any credentials it recognises (e.g., no session
	// cookie for Kratos, no Bearer token for JWT), it returns a Failure result
	// with Reason "no_credentials". This allows the middleware to try the next
	// provider in the chain.
	//
	// If credentials are present but invalid, it returns a Failure result with
	// a specific Reason (e.g., "token_expired", "session_invalid").
	//
	// The context may carry request-scoped values (trace context, etc.).
	// Implementations must honour context cancellation.
	Authenticate(ctx context.Context, r *http.Request) identity.AuthResult
}

// IdentityProviderUnavailable is returned when the underlying identity service
// (e.g., Kratos, JWKS endpoint) cannot be reached. Middleware should handle this
// according to the configured degradation mode (fail-closed vs. allow-public).
type IdentityProviderUnavailable struct {
	// Provider is the name of the unavailable provider (e.g., "kratos").
	Provider string
	// Cause is the underlying error that made the provider unavailable.
	Cause error
}

// Error implements the error interface.
func (e IdentityProviderUnavailable) Error() string {
	return "identity provider " + e.Provider + " unavailable: " + e.Cause.Error()
}

// Unwrap returns the underlying cause for errors.Is/errors.As traversal.
func (e IdentityProviderUnavailable) Unwrap() error {
	return e.Cause
}
