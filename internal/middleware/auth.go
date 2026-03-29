package middleware

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"sync/atomic"

	"github.com/vibewarden/vibewarden/internal/domain/audit"
	"github.com/vibewarden/vibewarden/internal/domain/events"
	"github.com/vibewarden/vibewarden/internal/domain/identity"
	"github.com/vibewarden/vibewarden/internal/ports"
)

const (
	// defaultSessionCookieName is the cookie name used by Ory Kratos.
	defaultSessionCookieName = "ory_kratos_session"

	// defaultLoginURL is the Kratos self-service browser login endpoint.
	defaultLoginURL = "/self-service/login/browser"
)

// AuthMiddleware returns HTTP middleware that enforces authentication on every
// request using the provided IdentityProvider.
//
// Request handling flow:
//  1. Strip all incoming X-User-* headers to prevent client spoofing.
//  2. If the request path matches any public path pattern (including the
//     automatic /_vibewarden/* prefix), call the next handler immediately.
//  3. Call provider.Authenticate to validate credentials in the request.
//  4. If the result has Reason "no_credentials", redirect to the login URL.
//  5. If the result has Reason "session_invalid" or "session_not_found",
//     redirect to the login URL.
//  6. If the result has Reason "provider_unavailable", return HTTP 503.
//  7. On a successful result, store the Identity in the request context and
//     call the next handler.
//
// When the provider transitions between unavailable and available, structured
// events (auth.provider_unavailable / auth.provider_recovered) are emitted
// exactly once per transition to avoid log flooding.
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
	provider ports.IdentityProvider,
	cfg ports.AuthConfig,
	logger *slog.Logger,
	eventLogger ports.EventLogger,
	auditLogger ports.AuditEventLogger,
) func(http.Handler) http.Handler {
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

	// unavailableState tracks provider availability transitions.
	// 0 = last known healthy; 1 = last known unavailable.
	// Using atomic int32 so the middleware closure is safe for concurrent use.
	var unavailableState atomic.Int32

	providerURL := cfg.KratosPublicURL

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

			// Step 3: Authenticate the request.
			result := provider.Authenticate(r.Context(), r)

			if !result.Authenticated {
				switch result.Reason {
				case "no_credentials", "session_not_found", "session_invalid":
					// Kratos is reachable but credentials are absent or invalid.
					// If we were previously unhealthy, record the recovery.
					if unavailableState.CompareAndSwap(1, 0) {
						emitKratosRecovered(r, eventLogger, providerURL)
					}
					emitAuthFailed(r, eventLogger, result.Message, "")
					emitAuditAuthFailure(r, auditLogger, "", result.Message)
					http.Redirect(w, r, loginURL, http.StatusFound)

				case "provider_unavailable":
					// Emit availability event only on transition to unavailable.
					if unavailableState.CompareAndSwap(0, 1) {
						emitKratosUnavailable(r, eventLogger, providerURL, result.Message)
					}
					emitAuthFailed(r, eventLogger, "auth provider unavailable", result.Message)
					emitAuditAuthFailure(r, auditLogger, "", "auth provider unavailable")
					WriteErrorResponse(w, r, http.StatusServiceUnavailable, "auth_provider_unavailable", "authentication service is temporarily unavailable")

				default:
					// Unknown failure — fail closed.
					emitAuthFailed(r, eventLogger, "unexpected auth error", result.Message)
					emitAuditAuthFailure(r, auditLogger, "", "unexpected auth error")
					WriteErrorResponse(w, r, http.StatusServiceUnavailable, "auth_provider_unavailable", "authentication service is temporarily unavailable")
				}
				return
			}

			// Authentication succeeded — record recovery if we were previously unhealthy.
			if unavailableState.CompareAndSwap(1, 0) {
				emitKratosRecovered(r, eventLogger, providerURL)
			}

			// Step 7: Valid identity — store in context and proceed.
			emitAuthSuccessIdentity(r, eventLogger, result.Identity)
			emitAuditAuthSuccess(r, auditLogger, result.Identity.ID(), result.Identity.Email())
			ctx := contextWithIdentity(r.Context(), result.Identity)
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

// emitAuthSuccessIdentity logs an auth.success event via the EventLogger port
// using the domain Identity value object.
// If eventLogger is nil the call is a no-op.
func emitAuthSuccessIdentity(r *http.Request, eventLogger ports.EventLogger, ident identity.Identity) {
	if eventLogger == nil {
		return
	}
	ev := events.NewAuthSuccess(events.AuthSuccessParams{
		Method:     r.Method,
		Path:       r.URL.Path,
		SessionID:  "",
		IdentityID: ident.ID(),
		Email:      ident.Email(),
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

// sessionCheckerAdapter wraps a deprecated ports.SessionChecker to implement
// ports.IdentityProvider. This allows legacy code that constructs an
// AuthMiddleware with a SessionChecker to continue working.
//
// Deprecated: Remove once all callers use IdentityProvider directly.
type sessionCheckerAdapter struct {
	checker    ports.SessionChecker
	cookieName string
}

// SessionCheckerToIdentityProvider wraps a deprecated SessionChecker as an
// IdentityProvider. The cookieName parameter specifies which cookie holds
// the session token (defaults to "ory_kratos_session" if empty).
//
// Deprecated: Use a native IdentityProvider implementation instead. This
// wrapper exists only for backward compatibility during the migration period.
func SessionCheckerToIdentityProvider(checker ports.SessionChecker, cookieName string) ports.IdentityProvider {
	if cookieName == "" {
		cookieName = defaultSessionCookieName
	}
	return &sessionCheckerAdapter{checker: checker, cookieName: cookieName}
}

// Name implements ports.IdentityProvider.
func (s *sessionCheckerAdapter) Name() string { return "kratos" }

// Authenticate implements ports.IdentityProvider by delegating to the wrapped
// SessionChecker. It extracts the session cookie, calls CheckSession, and maps
// the result to an identity.AuthResult.
func (s *sessionCheckerAdapter) Authenticate(ctx context.Context, r *http.Request) identity.AuthResult {
	cookie, err := r.Cookie(s.cookieName)
	if err != nil {
		return identity.Failure("no_credentials", "no session cookie")
	}

	sessionCookie := s.cookieName + "=" + cookie.Value
	session, err := s.checker.CheckSession(ctx, sessionCookie)
	if err != nil {
		switch {
		case errors.Is(err, ports.ErrSessionInvalid):
			return identity.Failure("session_invalid", "session is invalid or expired")
		case errors.Is(err, ports.ErrSessionNotFound):
			return identity.Failure("session_not_found", "session does not exist")
		case errors.Is(err, ports.ErrAuthProviderUnavailable):
			return identity.Failure("provider_unavailable", err.Error())
		default:
			return identity.Failure("auth_error", err.Error())
		}
	}

	ident, err := identity.NewIdentity(
		session.Identity.ID,
		session.Identity.Email,
		"kratos",
		session.Identity.EmailVerified,
		session.Identity.Traits,
	)
	if err != nil {
		return identity.Failure("invalid_identity", err.Error())
	}

	return identity.Success(ident)
}
