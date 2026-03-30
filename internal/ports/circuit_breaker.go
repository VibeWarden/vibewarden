// Package ports defines the interfaces (ports) for VibeWarden's hexagonal architecture.
package ports

import (
	"context"
	"time"

	"github.com/vibewarden/vibewarden/internal/domain/resilience"
)

// CircuitBreaker is the outbound port for the circuit breaker pattern.
// Implementations wrap the domain entity in a concurrency-safe adapter.
// All methods must be safe for concurrent use.
type CircuitBreaker interface {
	// IsOpen returns true when the circuit is open and the request should be
	// rejected immediately without contacting the upstream. Implementations
	// must evaluate the open timeout internally and advance to HalfOpen when
	// it expires.
	IsOpen() bool

	// RecordSuccess records a successful upstream response. When the circuit
	// is in HalfOpen state this transitions it back to Closed.
	RecordSuccess()

	// RecordFailure records a failed upstream response. When the consecutive
	// failure count reaches the threshold the circuit trips to Open.
	RecordFailure()

	// State returns the current circuit state.
	State() resilience.State
}

// CircuitBreakerConfig holds configuration for the circuit breaker in the
// ports layer. It mirrors the domain config but lives in the ports package so
// that config builders do not need to import the domain package.
type CircuitBreakerConfig struct {
	// Enabled toggles the circuit breaker middleware.
	Enabled bool

	// Threshold is the number of consecutive failures required to trip the
	// circuit from Closed to Open.
	Threshold int

	// Timeout is how long the circuit stays Open before transitioning to
	// HalfOpen to allow a probe request.
	Timeout time.Duration
}

// MetricsCollectorWithCircuitBreaker extends MetricsCollector with a circuit
// breaker state gauge. Adapters that expose this gauge implement this interface;
// others embed a no-op to satisfy MetricsCollector without the extra method.
//
// SetCircuitBreakerState sets the vibewarden_circuit_breaker_state gauge.
// The state values match the schema: 0=closed, 1=open, 2=half_open.
type MetricsCollectorWithCircuitBreaker interface {
	MetricsCollector

	// SetCircuitBreakerState records the current circuit breaker state as a gauge.
	// state: 0=closed, 1=open, 2=half_open (matches resilience.State constants).
	SetCircuitBreakerState(ctx context.Context, state resilience.State)
}
