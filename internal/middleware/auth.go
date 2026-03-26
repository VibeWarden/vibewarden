package middleware

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/vibewarden/vibewarden/internal/domain/events"
	"github.com/vibewarden/vibewarden/internal/ports"
)

const (
	// defaultSessionCookieName is the cookie name used by Ory Kratos.
	defaultSessionCookieName = "ory_kratos_session"

	// defaultLoginURL is the Kratos self-service browser login endpoint.
	defaultLoginURL = "/self-service/login/browser"
)

// AuthMiddleware returns HTTP middleware that enforces session-based
// authentication on every request.
//
// Request handling flow:
//  1. Strip all incoming X-User-* headers to prevent client spoofing.
//  2. If the request path matches any public path pattern (including the
//     automatic /_vibewarden/* prefix), call the next handler immediately.
//  3. Extract the session cookie from the request.
//  4. If the cookie is absent, redirect to the configured login URL.
//  5. Call SessionChecker.CheckSession to validate the session with the
//     identity provider.
//  6. If the session is invalid or not found, redirect to the login URL.
//  7. If the identity provider is unavailable, return 503 Service
//     Unavailable (fail closed — never fail open).
//  8. On a valid session, store the session in the request context and
//     call the next handler.
//
// The eventLogger receives structured auth events following the VibeWarden
// schema (auth.success and auth.failed event types). If eventLogger is nil,
// event logging is skipped silently.
func AuthMiddleware(
	checker ports.SessionChecker,
	cfg ports.AuthConfig,
	logger *slog.Logger,
	eventLogger ports.EventLogger,
) func(http.Handler) http.Handler {
	cookieName := cfg.SessionCookieName
	if cookieName == "" {
		cookieName = defaultSessionCookieName
	}

	loginURL := cfg.LoginURL
	if loginURL == "" {
		loginURL = defaultLoginURL
	}

	matcher, err := NewPublicPathMatcher(cfg.PublicPaths)
	if err != nil {
		// Configuration error: patterns were invalid. Use an empty matcher
		// that only makes /_vibewarden/* public. Log the error and continue
		// with a fallback; the middleware must not panic.
		logger.Error("auth middleware: invalid public path patterns, falling back to empty list",
			slog.String("error", err.Error()),
		)
		// Safe fallback: only the always-public /_vibewarden/* is matched.
		matcher, _ = NewPublicPathMatcher(nil) //nolint:errcheck // nil patterns are always valid
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Step 1: Strip incoming X-User-* headers unconditionally to
			// prevent identity spoofing by malicious clients.
			stripXUserHeaders(r)

			// Step 2: Public path bypass.
			if matcher.Matches(r.URL.Path) {
				next.ServeHTTP(w, r)
				return
			}

			// Step 3: Extract session cookie.
			cookie, err := r.Cookie(cookieName)
			if err != nil {
				// http.ErrNoCookie is the only error Cookie returns.
				emitAuthFailed(r, eventLogger, "missing session cookie", "")
				http.Redirect(w, r, loginURL, http.StatusFound)
				return
			}
			sessionCookie := cookieName + "=" + cookie.Value

			// Step 4–7: Validate session with the identity provider.
			session, err := checker.CheckSession(r.Context(), sessionCookie)
			if err != nil {
				switch {
				case errors.Is(err, ports.ErrSessionNotFound),
					errors.Is(err, ports.ErrSessionInvalid):
					emitAuthFailed(r, eventLogger, "invalid or missing session", "")
					http.Redirect(w, r, loginURL, http.StatusFound)

				case errors.Is(err, ports.ErrAuthProviderUnavailable):
					emitAuthFailed(r, eventLogger, "auth provider unavailable", err.Error())
					http.Error(w, "Service Unavailable", http.StatusServiceUnavailable)

				default:
					// Unknown error — fail closed.
					emitAuthFailed(r, eventLogger, "unexpected auth error", err.Error())
					http.Error(w, "Service Unavailable", http.StatusServiceUnavailable)
				}
				return
			}

			// Step 8: Valid session — store in context and proceed.
			emitAuthSuccess(r, eventLogger, session)
			ctx := contextWithSession(r.Context(), session)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// stripXUserHeaders removes all X-User-* headers from the incoming request
// headers to prevent client-side identity spoofing.
func stripXUserHeaders(r *http.Request) {
	for key := range r.Header {
		if strings.HasPrefix(strings.ToLower(key), "x-user-") {
			r.Header.Del(key)
		}
	}
}

// emitAuthSuccess logs an auth.success event via the EventLogger port.
// If eventLogger is nil the call is a no-op.
func emitAuthSuccess(r *http.Request, eventLogger ports.EventLogger, session *ports.Session) {
	if eventLogger == nil {
		return
	}
	ev := events.NewAuthSuccess(events.AuthSuccessParams{
		Method:     r.Method,
		Path:       r.URL.Path,
		SessionID:  session.ID,
		IdentityID: session.Identity.ID,
		Email:      session.Identity.Email,
	})
	// Best-effort: ignore logging errors so request processing is never blocked.
	_ = eventLogger.Log(r.Context(), ev)
}

// emitAuthFailed logs an auth.failed event via the EventLogger port.
// If eventLogger is nil the call is a no-op.
func emitAuthFailed(r *http.Request, eventLogger ports.EventLogger, reason, detail string) {
	if eventLogger == nil {
		return
	}
	ev := events.NewAuthFailed(events.AuthFailedParams{
		Method: r.Method,
		Path:   r.URL.Path,
		Reason: reason,
		Detail: detail,
	})
	// Best-effort: ignore logging errors so request processing is never blocked.
	_ = eventLogger.Log(r.Context(), ev)
}
