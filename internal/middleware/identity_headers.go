package middleware

import (
	"log/slog"
	"net/http"
	"strconv"
)

// IdentityHeadersMiddleware returns HTTP middleware that injects user
// identity headers into the proxied request.
//
// It reads the session placed in the request context by AuthMiddleware and
// adds the following headers before forwarding to the upstream application:
//
//   - X-User-Id: the Kratos identity UUID
//   - X-User-Email: the user's primary email address
//   - X-User-Verified: "true" or "false" depending on email verification
//
// IMPORTANT: Any incoming X-User-* headers must have already been stripped
// by AuthMiddleware before this middleware runs. IdentityHeadersMiddleware
// does NOT strip them itself — it relies on the correct middleware ordering.
//
// If no session is present in the context (public path or auth disabled)
// the middleware is a no-op and simply calls the next handler.
func IdentityHeadersMiddleware(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			session, ok := SessionFromContext(r.Context())
			if ok && session != nil {
				r = r.Clone(r.Context())
				r.Header.Set("X-User-Id", session.Identity.ID)
				r.Header.Set("X-User-Email", session.Identity.Email)
				r.Header.Set("X-User-Verified", strconv.FormatBool(session.Identity.EmailVerified))

				logger.DebugContext(r.Context(), "identity headers injected",
					slog.String("identity_id", session.Identity.ID),
					slog.String("email", session.Identity.Email),
				)
			}

			next.ServeHTTP(w, r)
		})
	}
}
