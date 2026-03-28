package middleware

import (
	"context"
	"net/http"

	"github.com/vibewarden/vibewarden/internal/domain/audit"
	"github.com/vibewarden/vibewarden/internal/domain/auth"
	"github.com/vibewarden/vibewarden/internal/domain/events"
	"github.com/vibewarden/vibewarden/internal/ports"
)

const (
	// defaultAPIKeyHeader is the request header used to extract the API key
	// when no override is configured.
	defaultAPIKeyHeader = "X-API-Key"

	// apiKeyContextKey is the context key under which the validated *auth.APIKey
	// is stored after successful authentication.
	apiKeyContextKey contextKey = iota + 100
)

// APIKeyMiddleware returns HTTP middleware that authenticates requests using
// an API key extracted from a configurable request header.
//
// Request handling flow:
//  1. Extract the key from the configured header (default: X-API-Key).
//  2. If the header is absent, reject with HTTP 401 and emit an
//     auth.api_key.failed event.
//  3. Call APIKeyValidator.Validate with the raw header value.
//  4. If validation fails (invalid key, inactive key), reject with HTTP 401
//     and emit an auth.api_key.failed event.
//  5. Evaluate cfg.ScopeRules for the request path and method. The first
//     matching rule determines the required scopes. If the key does not hold
//     all required scopes, reject with HTTP 403 and emit an
//     auth.api_key.forbidden event.
//  6. On success, store the *auth.APIKey in the request context, emit an
//     auth.api_key.success event, and call the next handler.
//
// No matching scope rule means the request is allowed (open by default).
// The validator implementation is responsible for constant-time comparison.
// Keys are never logged; only the key name and scopes are emitted in events.
//
// If eventLogger is nil, event logging is skipped silently.
//
// The auditLogger receives security audit events (audit.auth.api_key.success,
// audit.auth.api_key.failure, audit.auth.api_key.forbidden). Audit events are
// always emitted regardless of operational log level. If auditLogger is nil,
// audit logging is skipped silently.
func APIKeyMiddleware(
	validator ports.APIKeyValidator,
	cfg ports.APIKeyConfig,
	eventLogger ports.EventLogger,
	auditLogger ports.AuditEventLogger,
) func(http.Handler) http.Handler {
	header := cfg.Header
	if header == "" {
		header = defaultAPIKeyHeader
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rawKey := r.Header.Get(header)
			if rawKey == "" {
				emitAPIKeyFailed(r, eventLogger, "missing api key")
				emitAuditAPIKeyFailure(r, auditLogger, "", "missing api key")
				WriteErrorResponse(w, r, http.StatusUnauthorized, "unauthorized", "API key required")
				return
			}

			apiKey, err := validator.Validate(r.Context(), rawKey)
			if err != nil {
				emitAPIKeyFailed(r, eventLogger, "invalid or inactive api key")
				emitAuditAPIKeyFailure(r, auditLogger, "", "invalid or inactive api key")
				WriteErrorResponse(w, r, http.StatusUnauthorized, "unauthorized", "invalid or inactive API key")
				return
			}

			// Scope enforcement: find the first matching scope rule and verify
			// that the key holds all required scopes. No matching rule = allow.
			if rule, ok := auth.MatchingScopeRule(cfg.ScopeRules, r.Method, r.URL.Path); ok {
				if !rule.SatisfiedBy(apiKey.Scopes) {
					emitAPIKeyForbidden(r, eventLogger, apiKey, rule.RequiredScopes)
					emitAuditAPIKeyForbidden(r, auditLogger, apiKey, rule.RequiredScopes)
					WriteErrorResponse(w, r, http.StatusForbidden, "forbidden", "insufficient scopes")
					return
				}
			}

			emitAPIKeySuccess(r, eventLogger, apiKey)
			emitAuditAPIKeySuccess(r, auditLogger, apiKey)
			ctx := contextWithAPIKey(r.Context(), apiKey)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// APIKeyFromContext retrieves the authenticated *auth.APIKey stored in the
// context by APIKeyMiddleware. It returns (nil, false) when no API key is
// present (i.e. the request was unauthenticated or used a different auth mode).
func APIKeyFromContext(ctx context.Context) (*auth.APIKey, bool) {
	k, ok := ctx.Value(apiKeyContextKey).(*auth.APIKey)
	return k, ok && k != nil
}

// contextWithAPIKey returns a new context carrying the given API key.
func contextWithAPIKey(ctx context.Context, k *auth.APIKey) context.Context {
	return context.WithValue(ctx, apiKeyContextKey, k)
}

// emitAPIKeySuccess logs an auth.api_key.success event via the EventLogger port.
// If eventLogger is nil the call is a no-op.
func emitAPIKeySuccess(r *http.Request, eventLogger ports.EventLogger, key *auth.APIKey) {
	if eventLogger == nil {
		return
	}
	scopes := make([]string, len(key.Scopes))
	for i, s := range key.Scopes {
		scopes[i] = string(s)
	}
	ev := events.NewAPIKeySuccess(events.APIKeySuccessParams{
		Method:  r.Method,
		Path:    r.URL.Path,
		KeyName: key.Name,
		Scopes:  scopes,
	})
	// Best-effort: ignore logging errors so request processing is never blocked.
	_ = eventLogger.Log(r.Context(), ev)
}

// emitAPIKeyFailed logs an auth.api_key.failed event via the EventLogger port.
// If eventLogger is nil the call is a no-op.
func emitAPIKeyFailed(r *http.Request, eventLogger ports.EventLogger, reason string) {
	if eventLogger == nil {
		return
	}
	ev := events.NewAPIKeyFailed(events.APIKeyFailedParams{
		Method: r.Method,
		Path:   r.URL.Path,
		Reason: reason,
	})
	// Best-effort: ignore logging errors so request processing is never blocked.
	_ = eventLogger.Log(r.Context(), ev)
}

// emitAPIKeyForbidden logs an auth.api_key.forbidden event via the EventLogger
// port when a valid key lacks the required scopes for the requested path+method.
// If eventLogger is nil the call is a no-op.
func emitAPIKeyForbidden(r *http.Request, eventLogger ports.EventLogger, key *auth.APIKey, requiredScopes []string) {
	if eventLogger == nil {
		return
	}
	keyScopes := make([]string, len(key.Scopes))
	for i, s := range key.Scopes {
		keyScopes[i] = string(s)
	}
	ev := events.NewAPIKeyForbidden(events.APIKeyForbiddenParams{
		Method:         r.Method,
		Path:           r.URL.Path,
		KeyName:        key.Name,
		KeyScopes:      keyScopes,
		RequiredScopes: requiredScopes,
	})
	// Best-effort: ignore logging errors so request processing is never blocked.
	_ = eventLogger.Log(r.Context(), ev)
}

// emitAuditAPIKeySuccess emits an audit.auth.api_key.success event via the
// AuditEventLogger port. If auditLogger is nil the call is a no-op.
func emitAuditAPIKeySuccess(r *http.Request, auditLogger ports.AuditEventLogger, key *auth.APIKey) {
	if auditLogger == nil {
		return
	}
	scopes := make([]string, len(key.Scopes))
	for i, s := range key.Scopes {
		scopes[i] = string(s)
	}
	ev, err := audit.NewAuditEvent(
		audit.EventTypeAuthAPIKeySuccess,
		audit.Actor{
			IP:         ExtractClientIP(r, false),
			APIKeyName: key.Name,
		},
		audit.Target{Path: r.URL.Path},
		audit.OutcomeSuccess,
		CorrelationID(r.Context()),
		map[string]any{
			"method": r.Method,
			"scopes": scopes,
		},
	)
	if err != nil {
		return
	}
	// Best-effort: ignore logging errors so request processing is never blocked.
	_ = auditLogger.Log(r.Context(), ev)
}

// emitAuditAPIKeyFailure emits an audit.auth.api_key.failure event via the
// AuditEventLogger port. If auditLogger is nil the call is a no-op.
func emitAuditAPIKeyFailure(r *http.Request, auditLogger ports.AuditEventLogger, keyName, reason string) {
	if auditLogger == nil {
		return
	}
	ev, err := audit.NewAuditEvent(
		audit.EventTypeAuthAPIKeyFailure,
		audit.Actor{
			IP:         ExtractClientIP(r, false),
			APIKeyName: keyName,
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

// emitAuditAPIKeyForbidden emits an audit.auth.api_key.forbidden event via the
// AuditEventLogger port when a valid key lacks the required scopes.
// If auditLogger is nil the call is a no-op.
func emitAuditAPIKeyForbidden(r *http.Request, auditLogger ports.AuditEventLogger, key *auth.APIKey, requiredScopes []string) {
	if auditLogger == nil {
		return
	}
	keyScopes := make([]string, len(key.Scopes))
	for i, s := range key.Scopes {
		keyScopes[i] = string(s)
	}
	ev, err := audit.NewAuditEvent(
		audit.EventTypeAuthAPIKeyForbidden,
		audit.Actor{
			IP:         ExtractClientIP(r, false),
			APIKeyName: key.Name,
		},
		audit.Target{Path: r.URL.Path},
		audit.OutcomeFailure,
		CorrelationID(r.Context()),
		map[string]any{
			"method":          r.Method,
			"key_scopes":      keyScopes,
			"required_scopes": requiredScopes,
		},
	)
	if err != nil {
		return
	}
	// Best-effort: ignore logging errors so request processing is never blocked.
	_ = auditLogger.Log(r.Context(), ev)
}
