package middleware

import (
	"context"

	"github.com/vibewarden/vibewarden/internal/domain/identity"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// contextKey is an unexported type for context keys used by this package.
// Using a package-local type prevents key collisions with other packages.
type contextKey int

const (
	// sessionContextKey is the context key under which the validated
	// ports.Session is stored by AuthMiddleware (deprecated).
	sessionContextKey contextKey = iota

	// identityContextKey is the context key under which the authenticated
	// domain identity.Identity is stored by AuthMiddleware.
	identityContextKey
)

// SessionFromContext retrieves the authenticated session stored in the
// context by AuthMiddleware. It returns (nil, false) when no session is
// present (i.e. the request was unauthenticated, auth was bypassed, or the
// new IdentityProvider-based flow is in use).
//
// Deprecated: Use IdentityFromContext instead. SessionFromContext will return
// (nil, false) when the middleware is configured with an IdentityProvider.
func SessionFromContext(ctx context.Context) (*ports.Session, bool) {
	s, ok := ctx.Value(sessionContextKey).(*ports.Session)
	return s, ok && s != nil
}

// contextWithSession returns a new context carrying the given session.
//
// Deprecated: Use contextWithIdentity instead.
func contextWithSession(ctx context.Context, s *ports.Session) context.Context {
	return context.WithValue(ctx, sessionContextKey, s)
}

// IdentityFromContext retrieves the authenticated domain Identity stored in the
// context by AuthMiddleware. It returns (zero Identity, false) when no identity
// is present (i.e. the request was unauthenticated or auth was bypassed).
func IdentityFromContext(ctx context.Context) (identity.Identity, bool) {
	ident, ok := ctx.Value(identityContextKey).(identity.Identity)
	if !ok || ident.IsZero() {
		return identity.Identity{}, false
	}
	return ident, true
}

// contextWithIdentity returns a new context carrying the given identity.
func contextWithIdentity(ctx context.Context, ident identity.Identity) context.Context {
	return context.WithValue(ctx, identityContextKey, ident)
}
