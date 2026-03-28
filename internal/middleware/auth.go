package middleware

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"sync/atomic"

	"github.com/vibewarden/vibewarden/internal/domain/audit"
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
//  7. If the identity provider is unavailable, the behavior depends on
//     cfg.OnKratosUnavailable:
//     - "503" (default): return HTTP 503 Service Unavailable (fail-closed).
//     - "allow_public": already handled at step 2; protected paths get 503.
//  8. On a valid session, store the session in the request context and
//     call the next handler.
//
// When Kratos transitions between unavailable and available, structured events
// (auth.provider_unavailable / auth.provider_recovered) are emitted exactly
// once per transition to avoid log flooding.
//
// The eventLogger receives structured auth events following the VibeWarden
// schema (auth.success and auth.failed event types). If eventLogger is nil,
// event logging is skipped silently.
//
// The auditLogger receives security audit events (audit.auth.success,
// audit.auth.failure). Audit events are always emitted regardless of
// operational log level. If auditLogger is nil, audit logging is skipped
// silently.
func AuthMiddleware(
	checker ports.SessionChecker,
	cfg ports.AuthConfig,
	logger *slog.Logger,
	eventLogger ports.EventLogger,
	auditLogger ports.AuditEventLogger,
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

	// unavailableState tracks Kratos availability transitions.
	// 0 = last known healthy; 1 = last known unavailable.
	// Using atomic int32 so the middleware closure is safe for concurrent use.
	var unavailableState atomic.Int32

	kratosURL := cfg.KratosPublicURL

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
				emitAuditAuthFailure(r, auditLogger, "", "missing session cookie")
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
					// Kratos is reachable but the session is invalid/missing.
					// If we were previously unhealthy, record the recovery.
					if unavailableState.CompareAndSwap(1, 0) {
						emitKratosRecovered(r, eventLogger, kratosURL)
					}
					emitAuthFailed(r, eventLogger, "invalid or missing session", "")
					emitAuditAuthFailure(r, auditLogger, "", "invalid or missing session")
					http.Redirect(w, r, loginURL, http.StatusFound)

				case errors.Is(err, ports.ErrAuthProviderUnavailable):
					// Emit availability event only on transition to unavailable.
					if unavailableState.CompareAndSwap(0, 1) {
						emitKratosUnavailable(r, eventLogger, kratosURL, err.Error())
					}
					emitAuthFailed(r, eventLogger, "auth provider unavailable", err.Error())
					emitAuditAuthFailure(r, auditLogger, "", "auth provider unavailable")
					WriteErrorResponse(w, r, http.StatusServiceUnavailable, "auth_provider_unavailable", "authentication service is temporarily unavailable")

				default:
					// Unknown error — fail closed.
					emitAuthFailed(r, eventLogger, "unexpected auth error", err.Error())
					emitAuditAuthFailure(r, auditLogger, "", "unexpected auth error")
					WriteErrorResponse(w, r, http.StatusServiceUnavailable, "auth_provider_unavailable", "authentication service is temporarily unavailable")
				}
				return
			}

			// Session is valid — record recovery if we were previously unhealthy.
			if unavailableState.CompareAndSwap(1, 0) {
				emitKratosRecovered(r, eventLogger, kratosURL)
			}

			// Step 8: Valid session — store in context and proceed.
			emitAuthSuccess(r, eventLogger, session)
			emitAuditAuthSuccess(r, auditLogger, session.Identity.ID, session.Identity.Email)
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

// emitKratosUnavailable logs an auth.provider_unavailable event.
// If eventLogger is nil the call is a no-op.
func emitKratosUnavailable(r *http.Request, eventLogger ports.EventLogger, providerURL, errMsg string) {
	if eventLogger == nil {
		return
	}
	ev := events.NewAuthProviderUnavailable(events.AuthProviderUnavailableParams{
		ProviderURL:  providerURL,
		Error:        errMsg,
		AffectedPath: r.URL.Path,
	})
	_ = eventLogger.Log(r.Context(), ev)
}

// emitKratosRecovered logs an auth.provider_recovered event.
// If eventLogger is nil the call is a no-op.
func emitKratosRecovered(r *http.Request, eventLogger ports.EventLogger, providerURL string) {
	if eventLogger == nil {
		return
	}
	ev := events.NewAuthProviderRecovered(events.AuthProviderRecoveredParams{
		ProviderURL: providerURL,
	})
	_ = eventLogger.Log(r.Context(), ev)
}

// emitAuditAuthSuccess emits an audit.auth.success event via the AuditEventLogger port.
// If auditLogger is nil the call is a no-op.
func emitAuditAuthSuccess(r *http.Request, auditLogger ports.AuditEventLogger, userID, email string) {
	if auditLogger == nil {
		return
	}
	ev, err := audit.NewAuditEvent(
		audit.EventTypeAuthSuccess,
		audit.Actor{
			IP:     ExtractClientIP(r, false),
			UserID: userID,
		},
		audit.Target{Path: r.URL.Path},
		audit.OutcomeSuccess,
		CorrelationID(r.Context()),
		map[string]any{
			"method": r.Method,
			"email":  email,
		},
	)
	if err != nil {
		return
	}
	// Best-effort: ignore logging errors so request processing is never blocked.
	_ = auditLogger.Log(r.Context(), ev)
}

// emitAuditAuthFailure emits an audit.auth.failure event via the AuditEventLogger port.
// If auditLogger is nil the call is a no-op.
func emitAuditAuthFailure(r *http.Request, auditLogger ports.AuditEventLogger, userID, reason string) {
	if auditLogger == nil {
		return
	}
	ev, err := audit.NewAuditEvent(
		audit.EventTypeAuthFailure,
		audit.Actor{
			IP:     ExtractClientIP(r, false),
			UserID: userID,
		},
		audit.Target{Path: r.URL.Path},
		audit.OutcomeFailure,
		CorrelationID(r.Context()),
		map[string]any{
			"method": r.Method,
			"reason": reason,
		},
	)
	if err != nil {
		return
	}
	// Best-effort: ignore logging errors so request processing is never blocked.
	_ = auditLogger.Log(r.Context(), ev)
}
