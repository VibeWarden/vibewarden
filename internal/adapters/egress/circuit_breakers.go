// Package egress implements the HTTP listener and request forwarding adapter
// for the egress proxy plugin.
package egress

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	domainegress "github.com/vibewarden/vibewarden/internal/domain/egress"
	"github.com/vibewarden/vibewarden/internal/domain/events"
	domainresilience "github.com/vibewarden/vibewarden/internal/domain/resilience"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// circuitBreakerEntry holds the concurrency-safe state for a single per-route
// circuit breaker backed by the domain entity.
type circuitBreakerEntry struct {
	mu        sync.Mutex
	cb        *domainresilience.CircuitBreaker
	routeName string
	cfg       domainresilience.CircuitBreakerConfig
}

// isOpen returns true when the circuit is open and requests should be rejected.
// It advances the state from Open to HalfOpen when the reset timeout expires.
func (e *circuitBreakerEntry) isOpen() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.cb.IsOpen(time.Now())
}

// recordSuccess records a successful upstream response and returns the previous
// state so callers can detect HalfOpen→Closed transitions.
func (e *circuitBreakerEntry) recordSuccess() domainresilience.State {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.cb.RecordSuccess()
}

// recordFailure records a failed upstream response and returns the previous
// state together with a boolean indicating whether a state transition occurred.
func (e *circuitBreakerEntry) recordFailure() (previous domainresilience.State, transitioned bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.cb.RecordFailure(time.Now())
}

// CircuitBreakerRegistry maintains one circuit breaker per named egress route.
// It is safe for concurrent use. Each entry is created lazily on first access
// and is only created when the route has a non-zero CircuitBreakerConfig.
type CircuitBreakerRegistry struct {
	mu      sync.Mutex
	entries map[string]*circuitBreakerEntry

	logger  *slog.Logger
	eventFn ports.EventLogger
}

// NewCircuitBreakerRegistry creates a CircuitBreakerRegistry. Pass nil for
// logger to use slog.Default(). Pass nil for eventFn to disable structured
// event emission.
func NewCircuitBreakerRegistry(logger *slog.Logger, eventFn ports.EventLogger) *CircuitBreakerRegistry {
	if logger == nil {
		logger = slog.Default()
	}
	return &CircuitBreakerRegistry{
		entries: make(map[string]*circuitBreakerEntry),
		logger:  logger,
		eventFn: eventFn,
	}
}

// getOrCreate returns the circuit breaker for the given route, creating it if
// it does not yet exist. Returns (nil, nil) when the route has no circuit
// breaker configuration (Threshold == 0 || ResetAfter == 0).
func (r *CircuitBreakerRegistry) getOrCreate(route domainegress.Route) (*circuitBreakerEntry, error) {
	cbCfg := route.CircuitBreaker()
	if cbCfg.Threshold <= 0 || cbCfg.ResetAfter <= 0 {
		return nil, nil
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if e, ok := r.entries[route.Name()]; ok {
		return e, nil
	}

	domainCfg := domainresilience.CircuitBreakerConfig{
		Threshold: cbCfg.Threshold,
		Timeout:   cbCfg.ResetAfter,
	}
	cb, err := domainresilience.NewCircuitBreaker(domainCfg)
	if err != nil {
		return nil, fmt.Errorf("creating circuit breaker for route %q: %w", route.Name(), err)
	}

	entry := &circuitBreakerEntry{
		cb:        cb,
		routeName: route.Name(),
		cfg:       domainCfg,
	}
	r.entries[route.Name()] = entry
	return entry, nil
}

// IsOpen returns true when the per-route circuit breaker for the matched route
// is in the open state and the request should be rejected immediately. It
// returns false when the route has no circuit breaker configured.
func (r *CircuitBreakerRegistry) IsOpen(ctx context.Context, route domainegress.Route) (bool, error) {
	entry, err := r.getOrCreate(route)
	if err != nil {
		return false, err
	}
	if entry == nil {
		return false, nil
	}

	prevState := func() domainresilience.State {
		entry.mu.Lock()
		defer entry.mu.Unlock()
		return entry.cb.State()
	}()

	open := entry.isOpen()

	// Detect Open → HalfOpen transition to emit the structured event.
	newState := func() domainresilience.State {
		entry.mu.Lock()
		defer entry.mu.Unlock()
		return entry.cb.State()
	}()
	if prevState == domainresilience.StateOpen && newState == domainresilience.StateHalfOpen {
		r.logger.InfoContext(ctx, "egress.circuit_breaker.half_open",
			slog.String("event_type", "egress.circuit_breaker.half_open"),
			slog.String("route", route.Name()),
		)
	}

	return open, nil
}

// RecordSuccess records a successful upstream response for the given route's
// circuit breaker. When the circuit was in HalfOpen state and transitions to
// Closed, a structured egress.circuit_breaker.closed event is emitted.
// Has no effect when the route has no circuit breaker configured.
func (r *CircuitBreakerRegistry) RecordSuccess(ctx context.Context, route domainegress.Route) {
	entry, err := r.getOrCreate(route)
	if err != nil {
		r.logger.ErrorContext(ctx, "egress circuit breaker: failed to get entry on success",
			slog.String("route", route.Name()),
			slog.String("err", err.Error()),
		)
		return
	}
	if entry == nil {
		return
	}

	previous := entry.recordSuccess()
	if previous == domainresilience.StateHalfOpen {
		// HalfOpen → Closed transition.
		r.logger.InfoContext(ctx, "egress.circuit_breaker.closed",
			slog.String("event_type", events.EventTypeEgressCircuitBreakerClosed),
			slog.String("route", route.Name()),
		)
		r.emitEvent(ctx, events.NewEgressCircuitBreakerClosed(events.EgressCircuitBreakerClosedParams{
			Route: route.Name(),
		}))
	}
}

// RecordFailure records a failed upstream response for the given route's circuit
// breaker. When the failure threshold is reached (Closed→Open) or a probe fails
// (HalfOpen→Open), a structured egress.circuit_breaker.opened event is emitted.
// Has no effect when the route has no circuit breaker configured.
func (r *CircuitBreakerRegistry) RecordFailure(ctx context.Context, route domainegress.Route) {
	entry, err := r.getOrCreate(route)
	if err != nil {
		r.logger.ErrorContext(ctx, "egress circuit breaker: failed to get entry on failure",
			slog.String("route", route.Name()),
			slog.String("err", err.Error()),
		)
		return
	}
	if entry == nil {
		return
	}

	_, transitioned := entry.recordFailure()
	if transitioned {
		// Closed→Open or HalfOpen→Open.
		r.logger.WarnContext(ctx, "egress.circuit_breaker.opened",
			slog.String("event_type", events.EventTypeEgressCircuitBreakerOpened),
			slog.String("route", route.Name()),
			slog.Int("threshold", entry.cfg.Threshold),
			slog.Float64("timeout_seconds", entry.cfg.Timeout.Seconds()),
		)
		r.emitEvent(ctx, events.NewEgressCircuitBreakerOpened(events.EgressCircuitBreakerOpenedParams{
			Route:          route.Name(),
			Threshold:      entry.cfg.Threshold,
			TimeoutSeconds: entry.cfg.Timeout.Seconds(),
		}))
	}
}

// emitEvent sends a structured event via the EventLogger. Failures are logged
// but do not interrupt request handling.
func (r *CircuitBreakerRegistry) emitEvent(ctx context.Context, ev events.Event) {
	if r.eventFn == nil {
		return
	}
	if err := r.eventFn.Log(ctx, ev); err != nil {
		r.logger.Error("egress circuit breaker: failed to emit event",
			slog.String("event_type", ev.EventType),
			slog.String("err", err.Error()),
		)
	}
}
