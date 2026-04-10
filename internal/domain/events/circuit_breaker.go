package events

import (
	"fmt"
	"time"
)

// Event type constants for circuit breaker state transitions.
const (
	// EventTypeCircuitBreakerOpened is emitted when the circuit breaker trips
	// from Closed to Open because consecutive failures reached the threshold.
	EventTypeCircuitBreakerOpened = "circuit_breaker.opened"

	// EventTypeCircuitBreakerHalfOpen is emitted when the circuit breaker
	// transitions from Open to HalfOpen because the open timeout expired and a
	// probe request is allowed through.
	EventTypeCircuitBreakerHalfOpen = "circuit_breaker.half_open"

	// EventTypeCircuitBreakerClosed is emitted when the circuit breaker returns
	// to Closed because the upstream probe succeeded.
	EventTypeCircuitBreakerClosed = "circuit_breaker.closed"
)

// CircuitBreakerOpenedParams contains the parameters needed to construct a
// circuit_breaker.opened event.
type CircuitBreakerOpenedParams struct {
	// Threshold is the consecutive failure count that tripped the circuit.
	Threshold int

	// TimeoutSeconds is the duration the circuit will remain open before
	// allowing a probe, in seconds.
	TimeoutSeconds float64
}

// NewCircuitBreakerOpened creates a circuit_breaker.opened event indicating
// that the circuit breaker has tripped and is now blocking upstream traffic.
func NewCircuitBreakerOpened(params CircuitBreakerOpenedParams) Event {
	return Event{
		SchemaVersion: SchemaVersion,
		EventType:     EventTypeCircuitBreakerOpened,
		Timestamp:     time.Now().UTC(),
		Severity:      SeverityHigh,
		Category:      CategoryResilience,
		AISummary: fmt.Sprintf(
			"Circuit breaker opened after %d consecutive failures; upstream blocked for %.0fs",
			params.Threshold, params.TimeoutSeconds,
		),
		Payload: map[string]any{
			"threshold":       params.Threshold,
			"timeout_seconds": params.TimeoutSeconds,
		},
	}
}

// CircuitBreakerHalfOpenParams contains the parameters needed to construct a
// circuit_breaker.half_open event.
type CircuitBreakerHalfOpenParams struct {
	// TimeoutSeconds is the open timeout that elapsed before the probe, in seconds.
	TimeoutSeconds float64
}

// NewCircuitBreakerHalfOpen creates a circuit_breaker.half_open event
// indicating that the open timeout expired and a probe request is allowed
// through to test whether the upstream has recovered.
func NewCircuitBreakerHalfOpen(params CircuitBreakerHalfOpenParams) Event {
	return Event{
		SchemaVersion: SchemaVersion,
		EventType:     EventTypeCircuitBreakerHalfOpen,
		Timestamp:     time.Now().UTC(),
		Severity:      SeverityInfo,
		Category:      CategoryResilience,
		AISummary: fmt.Sprintf(
			"Circuit breaker entered half-open state after %.0fs timeout; probe request allowed",
			params.TimeoutSeconds,
		),
		Payload: map[string]any{
			"timeout_seconds": params.TimeoutSeconds,
		},
	}
}

// NewCircuitBreakerClosed creates a circuit_breaker.closed event indicating
// that the upstream probe succeeded and the circuit breaker has returned to
// normal operation.
func NewCircuitBreakerClosed() Event {
	return Event{
		SchemaVersion: SchemaVersion,
		EventType:     EventTypeCircuitBreakerClosed,
		Timestamp:     time.Now().UTC(),
		Severity:      SeverityInfo,
		Category:      CategoryResilience,
		AISummary:     "Circuit breaker closed; upstream recovered and traffic is flowing normally",
		Payload:       map[string]any{},
	}
}
