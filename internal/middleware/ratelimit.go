package middleware

import (
	"log/slog"
	"math"
	"net/http"
	"time"

	"github.com/vibewarden/vibewarden/internal/domain/audit"
	"github.com/vibewarden/vibewarden/internal/domain/events"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// RateLimitMiddleware returns HTTP middleware that enforces rate limits.
// It applies per-IP limits to all requests and per-user limits to authenticated
// requests.
//
// User identity resolution (step 4) uses two sources in priority order:
//  1. The domain Identity stored in the request context by AuthMiddleware
//     (via IdentityFromContext). This is the preferred source and removes any
//     dependency on IdentityHeadersMiddleware running first.
//  2. The X-User-Id request header, as a fallback for wiring paths where
//     AuthMiddleware does not store identity in context (e.g. the Caddy
//     vibewarden_authentication handler path).
//
// This dual-source approach means RateLimitMiddleware is robust regardless of
// whether it runs before or after IdentityHeadersMiddleware in the chain.
//
// Request handling flow:
//  1. If the request path matches any exempt path pattern (including /_vibewarden/*),
//     bypass rate limiting entirely.
//  2. Extract the client IP (from X-Forwarded-For if trusted, otherwise RemoteAddr).
//  3. Check the per-IP rate limit. If exceeded, return 429 Too Many Requests.
//  4. Resolve the authenticated user ID: prefer IdentityFromContext; fall back
//     to the X-User-Id header. If a user ID is found, check the per-user rate
//     limit. If exceeded, return 429 Too Many Requests.
//  5. Call the next handler.
//
// On a 429 response:
//   - Sets the Retry-After header with the number of seconds to wait.
//   - Returns Content-Type: application/json.
//   - Returns body: {"error":"rate_limit_exceeded","retry_after_seconds":N}
//   - Emits a structured log event with event_type "rate_limit.hit".
//
// The eventLogger receives structured rate limit events following the VibeWarden
// schema. If eventLogger is nil, event logging is skipped silently.
//
// The auditLogger receives a security audit event (audit.rate_limit.hit) when a
// request is rejected. Audit events are always emitted regardless of operational
// log level. If auditLogger is nil, audit logging is skipped silently.
func RateLimitMiddleware(
	ipLimiter ports.RateLimiter,
	userLimiter ports.RateLimiter,
	cfg ports.RateLimitConfig,
	logger *slog.Logger,
	eventLogger ports.EventLogger,
	auditLogger ports.AuditEventLogger,
	drops ports.EventLogDropCounter,
) func(http.Handler) http.Handler {
	matcher, err := NewExemptPathMatcher(cfg.ExemptPaths)
	if err != nil {
		// Configuration error: patterns were invalid. Fall back to only the
		// automatic /_vibewarden/* exemption. Log and continue — never panic.
		logger.Error("rate limit middleware: invalid exempt path patterns, falling back to empty list",
			slog.String("error", err.Error()),
		)
		matcher, _ = NewExemptPathMatcher(nil) //nolint:errcheck // nil patterns are always valid
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Step 1: Exempt path bypass.
			if matcher.Matches(r.URL.Path) {
				next.ServeHTTP(w, r)
				return
			}

			// Step 2: Extract client IP.
			// Fail closed: if we cannot identify the client we must not let
			// the request through unrated — that would collapse all such
			// requests into a shared "" bucket, undermining per-IP limits.
			clientIP := ExtractClientIP(r, cfg.TrustProxyHeaders)
			if clientIP == "" {
				emitRateLimitUnidentified(r, eventLogger, drops)
				WriteErrorResponse(w, r, http.StatusForbidden, "forbidden", "client IP could not be determined")
				return
			}

			// Step 3: Per-IP rate limit check.
			ipResult := ipLimiter.Allow(r.Context(), clientIP)
			if !ipResult.Allowed {
				emitRateLimitHit(r, eventLogger, drops, "ip", clientIP, "", ipResult)
				emitAuditRateLimitHit(r, auditLogger, drops, "ip", clientIP, ipResult)
				writeRateLimitResponse(w, r, ipResult)
				return
			}

			// Step 4: Per-user rate limit check (authenticated requests only).
			// Prefer the domain Identity stored in context by AuthMiddleware; this
			// works regardless of whether IdentityHeadersMiddleware has run yet.
			// Fall back to the X-User-Id header for the Caddy wiring path where
			// the auth handler (vibewarden_authentication) writes headers directly
			// but does not store identity in Go context.
			userID := resolveUserID(r)
			if userID != "" {
				userResult := userLimiter.Allow(r.Context(), userID)
				if !userResult.Allowed {
					emitRateLimitHit(r, eventLogger, drops, "user", userID, clientIP, userResult)
					emitAuditRateLimitHit(r, auditLogger, drops, "user", clientIP, userResult)
					writeRateLimitResponse(w, r, userResult)
					return
				}
			}

			// Step 5: Pass through to the next handler.
			next.ServeHTTP(w, r)
		})
	}
}

// resolveUserID returns the authenticated user ID for the current request.
//
// Resolution order:
//  1. Domain Identity in the request context (set by AuthMiddleware via
//     contextWithIdentity). This is the preferred source: it is populated as
//     soon as AuthMiddleware runs, before IdentityHeadersMiddleware has had a
//     chance to inject the X-User-Id header.
//  2. X-User-Id request header (set by IdentityHeadersMiddleware or by the
//     Caddy vibewarden_authentication handler). Used as a fallback when no
//     context identity is present.
//
// Returns "" when the request is unauthenticated in both sources.
func resolveUserID(r *http.Request) string {
	if ident, ok := IdentityFromContext(r.Context()); ok {
		return ident.ID()
	}
	return r.Header.Get("X-User-Id")
}

// writeRateLimitResponse writes the 429 Too Many Requests HTTP response.
// It delegates to WriteRateLimitResponse which sets the Retry-After header,
// Content-Type: application/json, and a JSON body with a correlation ID.
func writeRateLimitResponse(w http.ResponseWriter, r *http.Request, result ports.RateLimitResult) {
	WriteRateLimitResponse(w, r, retryAfterSeconds(result.RetryAfter))
}

// retryAfterSeconds converts a retry duration to whole seconds, always rounding
// up so clients never retry before the limit has actually reset.
func retryAfterSeconds(d time.Duration) int {
	if d <= 0 {
		return 0
	}
	return int(math.Ceil(d.Seconds()))
}

// emitRateLimitHit emits a rate_limit.hit structured event via the EventLogger port.
// If eventLogger is nil the call is a no-op.
func emitRateLimitHit(
	r *http.Request,
	eventLogger ports.EventLogger,
	drops ports.EventLogDropCounter,
	limitType string,
	identifier string,
	clientIP string,
	result ports.RateLimitResult,
) {
	ev := events.NewRateLimitHit(events.RateLimitHitParams{
		LimitType:         limitType,
		Identifier:        identifier,
		RequestsPerSecond: result.Limit,
		Burst:             result.Burst,
		RetryAfterSeconds: retryAfterSeconds(result.RetryAfter),
		Path:              r.URL.Path,
		Method:            r.Method,
		ClientIP:          clientIP,
	})
	logEvent(r.Context(), eventLogger, drops, "ratelimit", ev)
}

// emitRateLimitUnidentified emits a rate_limit.unidentified_client event via
// the EventLogger port. If eventLogger is nil the call is a no-op.
func emitRateLimitUnidentified(r *http.Request, eventLogger ports.EventLogger, drops ports.EventLogDropCounter) {
	ev := events.NewRateLimitUnidentified(events.RateLimitUnidentifiedParams{
		Path:   r.URL.Path,
		Method: r.Method,
	})
	logEvent(r.Context(), eventLogger, drops, "ratelimit", ev)
}

// emitAuditRateLimitHit emits an audit.rate_limit.hit event via the
// AuditEventLogger port. If auditLogger is nil the call is a no-op.
func emitAuditRateLimitHit(
	r *http.Request,
	auditLogger ports.AuditEventLogger,
	drops ports.EventLogDropCounter,
	limitType string,
	clientIP string,
	result ports.RateLimitResult,
) {
	ev, err := audit.NewAuditEvent(
		audit.EventTypeRateLimitHit,
		audit.Actor{IP: clientIP},
		audit.Target{Path: r.URL.Path},
		audit.OutcomeFailure,
		CorrelationID(r.Context()),
		map[string]any{
			"method":              r.Method,
			"limit_type":          limitType,
			"retry_after_seconds": retryAfterSeconds(result.RetryAfter),
		},
	)
	if err != nil {
		return
	}
	logAudit(r.Context(), auditLogger, drops, "ratelimit", ev)
}
