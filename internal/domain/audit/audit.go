// Package audit defines the domain model for security audit events.
// This package has zero external dependencies — only the Go standard library.
//
// An AuditEvent is a structured, immutable record of a security-relevant action
// that occurred within VibeWarden. Every intercepted request that results in an
// authentication decision, rate-limit enforcement, IP filter decision, circuit
// breaker state change, or admin operation produces exactly one AuditEvent.
//
// Audit events are always emitted — they are not subject to log-level filtering.
// The domain layer produces events; I/O is handled by the ports.AuditLogger port.
package audit

import (
	"errors"
	"time"
)

// EventType identifies the kind of security event being recorded.
// Values are stable strings that form part of the public schema contract.
// Changing or removing a constant is a breaking change.
type EventType string

// Audit event type constants — grouped by subsystem.
// All values use the prefix "audit." followed by the subsystem and action.
const (
	// --- auth ---

	// EventTypeAuthSuccess is recorded when a request is authenticated
	// successfully (session cookie or API key).
	EventTypeAuthSuccess EventType = "audit.auth.success"

	// EventTypeAuthFailure is recorded when a request is rejected because the
	// presented credentials are missing, invalid, or expired.
	EventTypeAuthFailure EventType = "audit.auth.failure"

	// EventTypeAuthAPIKeySuccess is recorded when a request is authenticated
	// successfully via an API key.
	EventTypeAuthAPIKeySuccess EventType = "audit.auth.api_key.success"

	// EventTypeAuthAPIKeyFailure is recorded when a request is rejected because
	// the presented API key is missing, invalid, or belongs to an inactive key.
	EventTypeAuthAPIKeyFailure EventType = "audit.auth.api_key.failure"

	// EventTypeAuthAPIKeyForbidden is recorded when a valid API key is presented
	// but lacks the required scopes to access the requested path + method.
	EventTypeAuthAPIKeyForbidden EventType = "audit.auth.api_key.forbidden"

	// --- rate_limit ---

	// EventTypeRateLimitHit is recorded when a request is rejected because the
	// caller exceeded its per-IP or per-user rate limit.
	EventTypeRateLimitHit EventType = "audit.rate_limit.hit"

	// EventTypeRateLimitUnidentified is recorded when a request is rejected
	// because the client IP address could not be determined.
	EventTypeRateLimitUnidentified EventType = "audit.rate_limit.unidentified_client"

	// --- ip_filter ---

	// EventTypeIPFilterBlocked is recorded when a request is rejected by the IP
	// filter plugin because the client IP is not in the allowlist or is in the
	// blocklist.
	EventTypeIPFilterBlocked EventType = "audit.ip_filter.blocked"

	// --- circuit_breaker ---

	// EventTypeCircuitBreakerOpened is recorded when the circuit breaker trips
	// from Closed to Open because consecutive failures reached the threshold.
	EventTypeCircuitBreakerOpened EventType = "audit.circuit_breaker.opened"

	// EventTypeCircuitBreakerHalfOpen is recorded when the circuit breaker
	// transitions from Open to HalfOpen because the open timeout expired.
	EventTypeCircuitBreakerHalfOpen EventType = "audit.circuit_breaker.half_open"

	// EventTypeCircuitBreakerClosed is recorded when the circuit breaker returns
	// to Closed because the upstream probe succeeded.
	EventTypeCircuitBreakerClosed EventType = "audit.circuit_breaker.closed"

	// --- admin ---

	// EventTypeAdminUserCreated is recorded when an admin creates a new user
	// identity in the identity provider.
	EventTypeAdminUserCreated EventType = "audit.admin.user_created"

	// EventTypeAdminUserDeactivated is recorded when an admin deactivates a user
	// identity, preventing further authentication.
	EventTypeAdminUserDeactivated EventType = "audit.admin.user_deactivated"

	// EventTypeAdminUserDeleted is recorded when an admin permanently deletes a
	// user identity from the identity provider.
	EventTypeAdminUserDeleted EventType = "audit.admin.user_deleted"

	// EventTypeAdminAPIKeyCreated is recorded when an admin registers a new API
	// key.
	EventTypeAdminAPIKeyCreated EventType = "audit.admin.api_key_created"

	// EventTypeAdminAPIKeyRevoked is recorded when an admin revokes (deactivates)
	// an API key.
	EventTypeAdminAPIKeyRevoked EventType = "audit.admin.api_key_revoked"

	// --- waf ---

	// EventTypeWAFDetection is recorded when the WAF rule engine matches an
	// attack pattern in detect mode. The request is passed through to the
	// upstream application unchanged.
	EventTypeWAFDetection EventType = "audit.waf.detection"

	// EventTypeWAFBlocked is recorded when the WAF rule engine matches an
	// attack pattern in block mode. The request is rejected with 403 Forbidden.
	EventTypeWAFBlocked EventType = "audit.waf.blocked"
)

// Outcome describes whether the audited action succeeded or failed.
type Outcome string

const (
	// OutcomeSuccess indicates the action completed successfully.
	OutcomeSuccess Outcome = "success"

	// OutcomeFailure indicates the action was rejected or failed.
	OutcomeFailure Outcome = "failure"
)

// Actor identifies who (or what) triggered the audited action.
// All fields are optional — set only the ones available for the given event.
type Actor struct {
	// IP is the client IP address, if available.
	IP string

	// UserID is the identity provider UUID of the authenticated user, if any.
	UserID string

	// APIKeyName is the human-readable name of the API key used to authenticate,
	// if any.
	APIKeyName string
}

// Target describes the resource or endpoint that was the subject of the action.
type Target struct {
	// Path is the HTTP URL path of the request (e.g. "/api/orders").
	Path string

	// Resource is an optional higher-level resource identifier
	// (e.g. "user:id-abc123", "api_key:ci-deploy").
	Resource string
}

// AuditEvent is an immutable value object that records a single
// security-relevant action within VibeWarden.
//
// AuditEvent equality is by value: two events are equal if all fields are equal.
// Callers must not mutate the Details map after construction.
type AuditEvent struct {
	// Timestamp is when the event occurred, always in UTC.
	Timestamp time.Time

	// EventType identifies the kind of security event (e.g. "audit.auth.success").
	EventType EventType

	// Actor identifies who triggered the action.
	Actor Actor

	// Target describes the resource or endpoint acted upon.
	Target Target

	// Outcome indicates whether the action succeeded or failed.
	Outcome Outcome

	// TraceID is the distributed trace ID for correlating the event with the
	// originating request across systems. May be empty if tracing is not enabled.
	TraceID string

	// Details holds event-specific structured data. Values must be JSON-serialisable.
	// Callers must not mutate the map after passing it to NewAuditEvent.
	Details map[string]any
}

// NewAuditEvent constructs a validated AuditEvent.
// Timestamp is always set to the current UTC time.
// Returns an error if eventType is empty or outcome is empty.
func NewAuditEvent(
	eventType EventType,
	actor Actor,
	target Target,
	outcome Outcome,
	traceID string,
	details map[string]any,
) (AuditEvent, error) {
	if eventType == "" {
		return AuditEvent{}, errors.New("audit event type cannot be empty")
	}
	if outcome == "" {
		return AuditEvent{}, errors.New("audit event outcome cannot be empty")
	}
	if details == nil {
		details = map[string]any{}
	}
	return AuditEvent{
		Timestamp: time.Now().UTC(),
		EventType: eventType,
		Actor:     actor,
		Target:    target,
		Outcome:   outcome,
		TraceID:   traceID,
		Details:   details,
	}, nil
}
