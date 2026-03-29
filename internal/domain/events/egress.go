package events

import (
	"fmt"
	"time"
)

// Egress request/response event type constants.
const (
	// EventTypeEgressRequest is emitted when the egress proxy begins forwarding
	// an outbound request to an external service. The payload includes the route
	// name, HTTP method, and destination URL (sanitised — no auth tokens).
	EventTypeEgressRequest = "egress.request"

	// EventTypeEgressResponse is emitted when the egress proxy receives a
	// complete response from the external service, including the final status
	// code, duration, and total attempt count.
	EventTypeEgressResponse = "egress.response"

	// EventTypeEgressBlocked is emitted when the egress proxy refuses to
	// forward a request because the default policy is deny and no route matched,
	// or a security rule (SSRF, TLS enforcement) blocked it.
	EventTypeEgressBlocked = "egress.blocked"

	// EventTypeEgressError is emitted when the egress proxy encounters a
	// transport-level error (timeout, DNS failure, connection refused) and
	// cannot return a response to the caller.
	EventTypeEgressError = "egress.error"
)

// EgressRequestParams contains the parameters needed to construct an
// egress.request event.
type EgressRequestParams struct {
	// Route is the matched egress route name, or empty string if unmatched (allow policy).
	Route string

	// Method is the HTTP method of the outbound request (e.g. "GET", "POST").
	Method string

	// URL is the destination URL of the outbound request.
	// Must not include bearer tokens or credentials.
	URL string

	// TraceID is the W3C trace-id of the inbound request that triggered this
	// egress call. Empty when no inbound trace context is available.
	TraceID string
}

// NewEgressRequest creates an egress.request event indicating that the egress
// proxy is about to forward an outbound request to an external service.
func NewEgressRequest(params EgressRequestParams) Event {
	return Event{
		SchemaVersion: SchemaVersion,
		EventType:     EventTypeEgressRequest,
		Timestamp:     time.Now().UTC(),
		AISummary: fmt.Sprintf(
			"Egress request started: %s %s via route %q",
			params.Method, params.URL, params.Route,
		),
		Payload: map[string]any{
			"route":    params.Route,
			"method":   params.Method,
			"url":      params.URL,
			"trace_id": params.TraceID,
		},
	}
}

// EgressResponseParams contains the parameters needed to construct an
// egress.response event.
type EgressResponseParams struct {
	// Route is the matched egress route name, or empty string if unmatched.
	Route string

	// Method is the HTTP method of the outbound request (e.g. "GET", "POST").
	Method string

	// URL is the destination URL of the outbound request.
	URL string

	// StatusCode is the HTTP status code returned by the external service.
	StatusCode int

	// DurationSeconds is how long the complete round-trip took, in seconds.
	DurationSeconds float64

	// Attempts is the total number of upstream attempts (1 = no retries).
	Attempts int

	// TraceID is the W3C trace-id of the inbound request that triggered this
	// egress call. Empty when no inbound trace context is available.
	TraceID string
}

// NewEgressResponse creates an egress.response event indicating that the egress
// proxy received a complete response from the external service.
func NewEgressResponse(params EgressResponseParams) Event {
	return Event{
		SchemaVersion: SchemaVersion,
		EventType:     EventTypeEgressResponse,
		Timestamp:     time.Now().UTC(),
		AISummary: fmt.Sprintf(
			"Egress request completed: %s %s via route %q — status %d in %.3fs (%d attempt(s))",
			params.Method, params.URL, params.Route,
			params.StatusCode, params.DurationSeconds, params.Attempts,
		),
		Payload: map[string]any{
			"route":            params.Route,
			"method":           params.Method,
			"url":              params.URL,
			"status_code":      params.StatusCode,
			"duration_seconds": params.DurationSeconds,
			"attempts":         params.Attempts,
			"trace_id":         params.TraceID,
		},
	}
}

// EgressBlockedParams contains the parameters needed to construct an
// egress.blocked event.
type EgressBlockedParams struct {
	// Route is the matched route name, or empty string when no route matched.
	Route string

	// Method is the HTTP method of the blocked outbound request.
	Method string

	// URL is the destination URL of the blocked outbound request.
	URL string

	// Reason is a short human-readable description of why the request was blocked
	// (e.g. "no route matched default deny policy", "plain HTTP not allowed").
	Reason string

	// TraceID is the W3C trace-id of the inbound request that triggered this
	// egress call. Empty when no inbound trace context is available.
	TraceID string
}

// NewEgressBlocked creates an egress.blocked event indicating that the egress
// proxy refused to forward an outbound request due to a policy or security rule.
func NewEgressBlocked(params EgressBlockedParams) Event {
	return Event{
		SchemaVersion: SchemaVersion,
		EventType:     EventTypeEgressBlocked,
		Timestamp:     time.Now().UTC(),
		AISummary: fmt.Sprintf(
			"Egress request blocked: %s %s via route %q — %s",
			params.Method, params.URL, params.Route, params.Reason,
		),
		Payload: map[string]any{
			"route":    params.Route,
			"method":   params.Method,
			"url":      params.URL,
			"reason":   params.Reason,
			"trace_id": params.TraceID,
		},
	}
}

// EgressErrorParams contains the parameters needed to construct an egress.error event.
type EgressErrorParams struct {
	// Route is the matched egress route name, or empty string if unmatched.
	Route string

	// Method is the HTTP method of the failed outbound request.
	Method string

	// URL is the destination URL of the failed outbound request.
	URL string

	// Error is the human-readable error message. Must not include credentials.
	Error string

	// Attempts is the total number of upstream attempts made before failing.
	Attempts int

	// TraceID is the W3C trace-id of the inbound request that triggered this
	// egress call. Empty when no inbound trace context is available.
	TraceID string
}

// NewEgressError creates an egress.error event indicating that the egress proxy
// encountered a transport-level error and could not return a response.
func NewEgressError(params EgressErrorParams) Event {
	return Event{
		SchemaVersion: SchemaVersion,
		EventType:     EventTypeEgressError,
		Timestamp:     time.Now().UTC(),
		AISummary: fmt.Sprintf(
			"Egress request failed: %s %s via route %q after %d attempt(s) — %s",
			params.Method, params.URL, params.Route, params.Attempts, params.Error,
		),
		Payload: map[string]any{
			"route":    params.Route,
			"method":   params.Method,
			"url":      params.URL,
			"error":    params.Error,
			"attempts": params.Attempts,
			"trace_id": params.TraceID,
		},
	}
}

// Egress circuit breaker event type constants.
const (
	// EventTypeEgressCircuitBreakerOpened is emitted when a per-route egress
	// circuit breaker trips from Closed to Open because consecutive upstream
	// failures reached the configured threshold.
	EventTypeEgressCircuitBreakerOpened = "egress.circuit_breaker.opened"

	// EventTypeEgressCircuitBreakerClosed is emitted when a per-route egress
	// circuit breaker returns to Closed after a successful probe request confirms
	// that the upstream has recovered.
	EventTypeEgressCircuitBreakerClosed = "egress.circuit_breaker.closed"
)

// EgressCircuitBreakerOpenedParams contains the parameters needed to construct
// an egress.circuit_breaker.opened event.
type EgressCircuitBreakerOpenedParams struct {
	// Route is the egress route name whose circuit tripped.
	Route string

	// Threshold is the consecutive failure count that tripped the circuit.
	Threshold int

	// TimeoutSeconds is the duration the circuit will remain open before
	// allowing a probe request, in seconds.
	TimeoutSeconds float64
}

// NewEgressCircuitBreakerOpened creates an egress.circuit_breaker.opened event
// indicating that the per-route circuit breaker has tripped and all outbound
// requests to that route are being short-circuited with 503.
func NewEgressCircuitBreakerOpened(params EgressCircuitBreakerOpenedParams) Event {
	return Event{
		SchemaVersion: SchemaVersion,
		EventType:     EventTypeEgressCircuitBreakerOpened,
		Timestamp:     time.Now().UTC(),
		AISummary: fmt.Sprintf(
			"Egress circuit breaker opened for route %q after %d consecutive failures; upstream blocked for %.0fs",
			params.Route, params.Threshold, params.TimeoutSeconds,
		),
		Payload: map[string]any{
			"route":           params.Route,
			"threshold":       params.Threshold,
			"timeout_seconds": params.TimeoutSeconds,
		},
	}
}

// EgressCircuitBreakerClosedParams contains the parameters needed to construct
// an egress.circuit_breaker.closed event.
type EgressCircuitBreakerClosedParams struct {
	// Route is the egress route name whose circuit closed.
	Route string
}

// NewEgressCircuitBreakerClosed creates an egress.circuit_breaker.closed event
// indicating that the per-route circuit breaker has returned to normal operation
// after a successful upstream probe.
func NewEgressCircuitBreakerClosed(params EgressCircuitBreakerClosedParams) Event {
	return Event{
		SchemaVersion: SchemaVersion,
		EventType:     EventTypeEgressCircuitBreakerClosed,
		Timestamp:     time.Now().UTC(),
		AISummary: fmt.Sprintf(
			"Egress circuit breaker closed for route %q; upstream recovered and traffic is flowing normally",
			params.Route,
		),
		Payload: map[string]any{
			"route": params.Route,
		},
	}
}

// EventTypeEgressRateLimitHit is emitted when an outbound request to a named
// egress route is rejected because the per-route rate limit has been exceeded.
const EventTypeEgressRateLimitHit = "egress.rate_limit_hit"

// EgressRateLimitHitParams contains the parameters needed to construct an
// egress.rate_limit_hit event.
type EgressRateLimitHitParams struct {
	// Route is the egress route name whose rate limit was exceeded.
	Route string

	// Limit is the configured rate limit in requests per second.
	Limit float64

	// RetryAfterSeconds is how many seconds the caller should wait before retrying.
	RetryAfterSeconds float64
}

// NewEgressRateLimitHit creates an egress.rate_limit_hit event indicating that
// an outbound request was rejected because the per-route rate limit was exceeded.
func NewEgressRateLimitHit(params EgressRateLimitHitParams) Event {
	return Event{
		SchemaVersion: SchemaVersion,
		EventType:     EventTypeEgressRateLimitHit,
		Timestamp:     time.Now().UTC(),
		AISummary: fmt.Sprintf(
			"Egress rate limit exceeded for route %q (limit %.2f req/s); retry after %.0fs",
			params.Route, params.Limit, params.RetryAfterSeconds,
		),
		Payload: map[string]any{
			"route":               params.Route,
			"limit":               params.Limit,
			"retry_after_seconds": params.RetryAfterSeconds,
		},
	}
}
