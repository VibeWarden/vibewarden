package middleware

import (
	"context"

	"github.com/vibewarden/vibewarden/internal/ports"
)

// contextKey is an unexported type for context keys used by this package.
// Using a package-local type prevents key collisions with other packages.
type contextKey int

const (
	// sessionContextKey is the context key under which the validated
	// ports.Session is stored by AuthMiddleware.
	sessionContextKey contextKey = iota
)

// SessionFromContext retrieves the authenticated session stored in the
// context by AuthMiddleware. It returns (nil, false) when no session is
// present (i.e. the request was unauthenticated or auth was bypassed).
func SessionFromContext(ctx context.Context) (*ports.Session, bool) {
	s, ok := ctx.Value(sessionContextKey).(*ports.Session)
	return s, ok && s != nil
}

// contextWithSession returns a new context carrying the given session.
func contextWithSession(ctx context.Context, s *ports.Session) context.Context {
	return context.WithValue(ctx, sessionContextKey, s)
}
