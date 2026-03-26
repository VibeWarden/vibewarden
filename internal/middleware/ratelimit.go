package middleware

import (
	"encoding/json"
	"log/slog"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/vibewarden/vibewarden/internal/domain/events"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// rateLimitErrorBody is the JSON body returned when a rate limit is exceeded.
type rateLimitErrorBody struct {
	Error             string `json:"error"`
	RetryAfterSeconds int    `json:"retry_after_seconds"`
}

// RateLimitMiddleware returns HTTP middleware that enforces rate limits.
// It applies per-IP limits to all requests and per-user limits to authenticated
// requests (identified by the X-User-Id header injected by IdentityHeadersMiddleware).
//
// Request handling flow:
//  1. If the request path matches any exempt path pattern (including /_vibewarden/*),
//     bypass rate limiting entirely.
//  2. Extract the client IP (from X-Forwarded-For if trusted, otherwise RemoteAddr).
//  3. Check the per-IP rate limit. If exceeded, return 429 Too Many Requests.
//  4. If the request is authenticated (X-User-Id header is present), check the
//     per-user rate limit. If exceeded, return 429 Too Many Requests.
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
func RateLimitMiddleware(
	ipLimiter ports.RateLimiter,
	userLimiter ports.RateLimiter,
	cfg ports.RateLimitConfig,
	logger *slog.Logger,
	eventLogger ports.EventLogger,
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
				emitRateLimitUnidentified(r, eventLogger)
				http.Error(w, "Forbidden", http.StatusForbidden)
				return
			}

			// Step 3: Per-IP rate limit check.
			ipResult := ipLimiter.Allow(r.Context(), clientIP)
			if !ipResult.Allowed {
				emitRateLimitHit(r, eventLogger, "ip", clientIP, "", ipResult)
				writeRateLimitResponse(w, ipResult)
				return
			}

			// Step 4: Per-user rate limit check (authenticated requests only).
			userID := r.Header.Get("X-User-Id")
			if userID != "" {
				userResult := userLimiter.Allow(r.Context(), userID)
				if !userResult.Allowed {
					emitRateLimitHit(r, eventLogger, "user", userID, clientIP, userResult)
					writeRateLimitResponse(w, userResult)
					return
				}
			}

			// Step 5: Pass through to the next handler.
			next.ServeHTTP(w, r)
		})
	}
}

// writeRateLimitResponse writes the 429 Too Many Requests HTTP response.
// It sets the Retry-After header and a JSON body.
func writeRateLimitResponse(w http.ResponseWriter, result ports.RateLimitResult) {
	retrySeconds := retryAfterSeconds(result.RetryAfter)

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Retry-After", strconv.Itoa(retrySeconds))
	w.WriteHeader(http.StatusTooManyRequests)

	body := rateLimitErrorBody{
		Error:             "rate_limit_exceeded",
		RetryAfterSeconds: retrySeconds,
	}

	// Encoding errors on w are unrecoverable at this point; we have already
	// written the status code. Ignore the error — best effort.
	_ = json.NewEncoder(w).Encode(body)
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
	limitType string,
	identifier string,
	clientIP string,
	result ports.RateLimitResult,
) {
	if eventLogger == nil {
		return
	}
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
	// Best-effort: ignore logging errors so request processing is never blocked.
	_ = eventLogger.Log(r.Context(), ev)
}

// emitRateLimitUnidentified emits a rate_limit.unidentified_client event via
// the EventLogger port. If eventLogger is nil the call is a no-op.
func emitRateLimitUnidentified(r *http.Request, eventLogger ports.EventLogger) {
	if eventLogger == nil {
		return
	}
	ev := events.NewRateLimitUnidentified(events.RateLimitUnidentifiedParams{
		Path:   r.URL.Path,
		Method: r.Method,
	})
	// Best-effort: ignore logging errors so request processing is never blocked.
	_ = eventLogger.Log(r.Context(), ev)
}
