package middleware

import (
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
)

// IdentityHeadersMiddleware returns HTTP middleware that injects user
// identity headers into the proxied request.
//
// It reads the identity placed in the request context by AuthMiddleware and
// adds the following headers before forwarding to the upstream application:
//
//   - X-User-Id: the authenticated user's identity ID
//   - X-User-Email: the user's primary email address
//   - X-User-Verified: "true" or "false" depending on email verification
//
// The middleware first checks for a domain Identity (stored by the new
// IdentityProvider-based auth flow). When absent it falls back to the
// deprecated ports.Session (stored by the legacy SessionChecker flow).
//
// IMPORTANT: Any incoming X-User-* headers must have already been stripped
// by AuthMiddleware before this middleware runs. IdentityHeadersMiddleware
// does NOT strip them itself — it relies on the correct middleware ordering.
//
// If no identity is present in the context (public path or auth disabled)
// the middleware is a no-op and simply calls the next handler.
func IdentityHeadersMiddleware(logger *slog.Logger) func(http.Handler) http.Handler {
	return IdentityHeadersMiddlewareWithClaims(logger, nil)
}

// IdentityHeadersMiddlewareWithClaims returns HTTP middleware that injects user
// identity headers into the proxied request, including additional headers mapped
// from JWT claims.
//
// The claimsToHeaders map controls additional claim-to-header mappings applied
// when a domain Identity is present in the request context. For example,
// {"name": "X-User-Name", "roles": "X-User-Roles"} injects those headers from
// the identity's claims. Nil or empty map disables additional claim injection.
//
// The standard headers (X-User-Id, X-User-Email, X-User-Verified) are always
// injected when an identity or session is present.
func IdentityHeadersMiddlewareWithClaims(logger *slog.Logger, claimsToHeaders map[string]string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Prefer the new domain Identity stored by the IdentityProvider flow.
			if ident, ok := IdentityFromContext(r.Context()); ok {
				r = r.Clone(r.Context())
				r.Header.Set("X-User-Id", ident.ID())
				r.Header.Set("X-User-Email", ident.Email())
				r.Header.Set("X-User-Verified", strconv.FormatBool(ident.EmailVerified()))

				// Inject additional claims as configured by the JWT adapter.
				for claim, header := range claimsToHeaders {
					if val := ident.Claim(claim); val != nil {
						r.Header.Set(header, claimValueToString(val))
					}
				}

				logger.DebugContext(r.Context(), "identity headers injected",
					slog.String("identity_id", ident.ID()),
					slog.String("email", ident.Email()),
				)
				next.ServeHTTP(w, r)
				return
			}

			// Fall back to the deprecated ports.Session for backward compatibility.
			if session, ok := SessionFromContext(r.Context()); ok && session != nil {
				r = r.Clone(r.Context())
				r.Header.Set("X-User-Id", session.Identity.ID)
				r.Header.Set("X-User-Email", session.Identity.Email)
				r.Header.Set("X-User-Verified", strconv.FormatBool(session.Identity.EmailVerified))

				logger.DebugContext(r.Context(), "identity headers injected (legacy session)",
					slog.String("identity_id", session.Identity.ID),
					slog.String("email", session.Identity.Email),
				)
			}

			next.ServeHTTP(w, r)
		})
	}
}

// claimValueToString converts a JWT claim value to a string suitable for use as
// an HTTP header value. Strings are returned as-is; other types are formatted
// with fmt.Sprintf using the %v verb.
func claimValueToString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", v)
}
