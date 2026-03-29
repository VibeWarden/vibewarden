package events

import (
	"fmt"
	"time"
)

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
