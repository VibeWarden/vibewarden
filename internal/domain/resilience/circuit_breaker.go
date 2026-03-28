// Package resilience contains the domain model for upstream resilience features.
// This package has zero external dependencies — only Go stdlib.
package resilience

import (
	"errors"
	"time"
)

// State represents the state of a circuit breaker.
type State int

const (
	// StateClosed is the normal operating state. Requests pass through and
	// failures are counted. Once the failure threshold is reached the circuit
	// transitions to StateOpen.
	StateClosed State = iota

	// StateOpen means the circuit has tripped. All requests are short-circuited
	// immediately with an error — no upstream contact is made. After the
	// configured timeout the circuit transitions to StateHalfOpen.
	StateOpen

	// StateHalfOpen is the probing state. A single trial request is allowed
	// through to test whether the upstream has recovered. If the trial
	// succeeds the circuit returns to StateClosed; if it fails, it goes back
	// to StateOpen.
	StateHalfOpen
)

// String returns a human-readable name for the state.
func (s State) String() string {
	switch s {
	case StateClosed:
		return "closed"
	case StateOpen:
		return "open"
	case StateHalfOpen:
		return "half_open"
	default:
		return "unknown"
	}
}

// ErrCircuitOpen is returned when a request is rejected because the circuit
// breaker is in the open state.
var ErrCircuitOpen = errors.New("circuit breaker is open")

// CircuitBreakerConfig holds the parameters that drive a CircuitBreaker.
type CircuitBreakerConfig struct {
	// Threshold is the number of consecutive failures required to trip the
	// circuit from Closed to Open.
	// Must be > 0.
	Threshold int

	// Timeout is how long the circuit stays Open before transitioning to
	// HalfOpen to allow a probe request.
	// Must be > 0.
	Timeout time.Duration
}

// Validate returns an error when the configuration is invalid.
func (c CircuitBreakerConfig) Validate() error {
	if c.Threshold <= 0 {
		return errors.New("circuit breaker threshold must be greater than zero")
	}
	if c.Timeout <= 0 {
		return errors.New("circuit breaker timeout must be greater than zero")
	}
	return nil
}

// CircuitBreaker is a domain entity that implements the three-state circuit
// breaker pattern (Closed → Open → HalfOpen → Closed).
//
// CircuitBreaker is not safe for concurrent use on its own — callers must
// synchronise access externally (e.g. via the in-memory adapter which wraps
// it in a mutex). The methods are intentionally pure to keep domain logic
// free of concurrency primitives.
type CircuitBreaker struct {
	cfg CircuitBreakerConfig

	state    State
	failures int
	openedAt time.Time
}

// NewCircuitBreaker creates a new CircuitBreaker in the Closed state with the
// given configuration. Returns an error when the configuration is invalid.
func NewCircuitBreaker(cfg CircuitBreakerConfig) (*CircuitBreaker, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &CircuitBreaker{cfg: cfg, state: StateClosed}, nil
}

// State returns the current circuit state.
func (cb *CircuitBreaker) State() State {
	return cb.state
}

// Config returns the configuration used by this circuit breaker.
func (cb *CircuitBreaker) Config() CircuitBreakerConfig {
	return cb.cfg
}

// IsOpen returns true when requests should be rejected without contacting the
// upstream. It evaluates the timeout and advances state from Open to HalfOpen
// when enough time has passed. The provided now value should be time.Now() from
// the caller, allowing deterministic testing without real clocks.
func (cb *CircuitBreaker) IsOpen(now time.Time) bool {
	if cb.state == StateClosed {
		return false
	}
	if cb.state == StateHalfOpen {
		// HalfOpen: the probe slot is already consumed — block further requests.
		return true
	}
	// StateOpen: check whether the timeout has expired.
	if now.After(cb.openedAt.Add(cb.cfg.Timeout)) {
		cb.state = StateHalfOpen
		return false // allow the probe through
	}
	return true
}

// RecordSuccess records a successful upstream response. When in HalfOpen state
// the circuit closes and the failure counter is reset. Returns the previous
// state so callers can detect transitions.
func (cb *CircuitBreaker) RecordSuccess() (previous State) {
	previous = cb.state
	cb.failures = 0
	cb.state = StateClosed
	return previous
}

// RecordFailure records a failed upstream response. In Closed state it
// increments the failure counter and trips the circuit when the threshold is
// reached. In HalfOpen state a single failure re-opens the circuit immediately.
// Returns the previous state and whether a state transition occurred.
func (cb *CircuitBreaker) RecordFailure(now time.Time) (previous State, transitioned bool) {
	previous = cb.state
	switch cb.state {
	case StateClosed:
		cb.failures++
		if cb.failures >= cb.cfg.Threshold {
			cb.state = StateOpen
			cb.openedAt = now
			return previous, true
		}
	case StateHalfOpen:
		cb.state = StateOpen
		cb.openedAt = now
		return previous, true
	}
	return previous, false
}

// Failures returns the current consecutive failure count.
func (cb *CircuitBreaker) Failures() int {
	return cb.failures
}

// OpenedAt returns the time the circuit was last opened. The zero value is
// returned when the circuit has never been opened.
func (cb *CircuitBreaker) OpenedAt() time.Time {
	return cb.openedAt
}
