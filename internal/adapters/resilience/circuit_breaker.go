// Package resilience provides adapters for upstream resilience features.
package resilience

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/vibewarden/vibewarden/internal/domain/audit"
	"github.com/vibewarden/vibewarden/internal/domain/events"
	domainresilience "github.com/vibewarden/vibewarden/internal/domain/resilience"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// InMemoryCircuitBreaker is a ports.CircuitBreaker implementation backed by the
// domain CircuitBreaker entity. All state transitions are protected by a mutex
// so concurrent requests are safe.
//
// On state transition the adapter emits a structured event via ports.EventLogger
// and, when available, updates the vibewarden_circuit_breaker_state gauge via
// ports.MetricsCollectorWithCircuitBreaker.
//
// When an auditLogger is provided, audit events (audit.circuit_breaker.opened,
// audit.circuit_breaker.half_open, audit.circuit_breaker.closed) are emitted on
// every state transition, regardless of operational log level.
type InMemoryCircuitBreaker struct {
	mu     sync.Mutex
	cb     *domainresilience.CircuitBreaker
	logger *slog.Logger
	events ports.EventLogger
	// metrics is optional; may be nil when metrics are disabled.
	metrics ports.MetricsCollectorWithCircuitBreaker
	// auditLogger is optional; may be nil when audit logging is disabled.
	auditLogger ports.AuditEventLogger
}

// NewInMemoryCircuitBreaker creates an InMemoryCircuitBreaker from a ports config.
// Returns an error when the configuration is invalid (threshold ≤ 0 or timeout ≤ 0).
func NewInMemoryCircuitBreaker(
	cfg ports.CircuitBreakerConfig,
	logger *slog.Logger,
	eventLogger ports.EventLogger,
	metrics ports.MetricsCollectorWithCircuitBreaker,
) (*InMemoryCircuitBreaker, error) {
	domainCfg := domainresilience.CircuitBreakerConfig{
		Threshold: cfg.Threshold,
		Timeout:   cfg.Timeout,
	}
	cb, err := domainresilience.NewCircuitBreaker(domainCfg)
	if err != nil {
		return nil, err
	}
	return &InMemoryCircuitBreaker{
		cb:      cb,
		logger:  logger,
		events:  eventLogger,
		metrics: metrics,
	}, nil
}

// WithAuditLogger returns a new InMemoryCircuitBreaker that emits audit events
// on every state transition via auditLogger. The original is not modified.
// This follows the functional options pattern used for optional dependencies.
func (a *InMemoryCircuitBreaker) WithAuditLogger(auditLogger ports.AuditEventLogger) *InMemoryCircuitBreaker {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.auditLogger = auditLogger
	return a
}

// IsOpen implements ports.CircuitBreaker. It is safe for concurrent use.
// When the circuit transitions from Open to HalfOpen (because the timeout
// expired) a circuit_breaker.half_open event is emitted.
func (a *InMemoryCircuitBreaker) IsOpen() bool {
	a.mu.Lock()
	defer a.mu.Unlock()

	prevState := a.cb.State()
	open := a.cb.IsOpen(time.Now())

	// Detect Open → HalfOpen transition.
	if prevState == domainresilience.StateOpen && a.cb.State() == domainresilience.StateHalfOpen {
		a.emitEvent(events.NewCircuitBreakerHalfOpen(events.CircuitBreakerHalfOpenParams{
			TimeoutSeconds: a.cb.Config().Timeout.Seconds(),
		}))
		a.emitAuditEvent(context.Background(), audit.EventTypeCircuitBreakerHalfOpen,
			map[string]any{"timeout_seconds": a.cb.Config().Timeout.Seconds()})
		a.recordStateMetric(context.Background())
	}

	return open
}

// RecordSuccess implements ports.CircuitBreaker. It is safe for concurrent use.
// When the circuit was HalfOpen and transitions back to Closed a
// circuit_breaker.closed event is emitted.
func (a *InMemoryCircuitBreaker) RecordSuccess() {
	a.mu.Lock()
	defer a.mu.Unlock()

	previous := a.cb.RecordSuccess()
	if previous == domainresilience.StateHalfOpen {
		// Transition: HalfOpen → Closed.
		a.emitEvent(events.NewCircuitBreakerClosed())
		a.emitAuditEvent(context.Background(), audit.EventTypeCircuitBreakerClosed, nil)
		a.recordStateMetric(context.Background())
	}
}

// RecordFailure implements ports.CircuitBreaker. It is safe for concurrent use.
// When the failure threshold is reached (Closed → Open) or a probe fails
// (HalfOpen → Open) a circuit_breaker.opened event is emitted.
func (a *InMemoryCircuitBreaker) RecordFailure() {
	a.mu.Lock()
	defer a.mu.Unlock()

	_, transitioned := a.cb.RecordFailure(time.Now())
	if transitioned && a.cb.State() == domainresilience.StateOpen {
		a.emitEvent(events.NewCircuitBreakerOpened(events.CircuitBreakerOpenedParams{
			Threshold:      a.cb.Config().Threshold,
			TimeoutSeconds: a.cb.Config().Timeout.Seconds(),
		}))
		a.emitAuditEvent(context.Background(), audit.EventTypeCircuitBreakerOpened,
			map[string]any{
				"threshold":       a.cb.Config().Threshold,
				"timeout_seconds": a.cb.Config().Timeout.Seconds(),
			})
		a.recordStateMetric(context.Background())
	}
}

// State implements ports.CircuitBreaker. It is safe for concurrent use.
func (a *InMemoryCircuitBreaker) State() domainresilience.State {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.cb.State()
}

// emitEvent sends a structured event. Failures are logged but do not interrupt
// request handling. Must be called with a.mu held.
func (a *InMemoryCircuitBreaker) emitEvent(ev events.Event) {
	if a.events == nil {
		return
	}
	if err := a.events.Log(context.Background(), ev); err != nil {
		if a.logger != nil {
			a.logger.Error("circuit_breaker: failed to emit event",
				slog.String("event_type", ev.EventType),
				slog.String("error", err.Error()),
			)
		}
	}
}

// emitAuditEvent sends a security audit event for a circuit breaker state
// transition. Failures are logged but do not interrupt request handling.
// Must be called with a.mu held.
func (a *InMemoryCircuitBreaker) emitAuditEvent(ctx context.Context, eventType audit.EventType, details map[string]any) {
	if a.auditLogger == nil {
		return
	}
	auditEv, err := audit.NewAuditEvent(
		eventType,
		audit.Actor{},
		audit.Target{},
		audit.OutcomeSuccess,
		"",
		details,
	)
	if err != nil {
		if a.logger != nil {
			a.logger.Error("circuit_breaker: failed to build audit event",
				slog.String("event_type", string(eventType)),
				slog.String("error", err.Error()),
			)
		}
		return
	}
	if err := a.auditLogger.Log(ctx, auditEv); err != nil {
		if a.logger != nil {
			a.logger.Error("circuit_breaker: failed to emit audit event",
				slog.String("event_type", string(eventType)),
				slog.String("error", err.Error()),
			)
		}
	}
}

// recordStateMetric updates the circuit breaker state gauge. Must be called with a.mu held.
func (a *InMemoryCircuitBreaker) recordStateMetric(ctx context.Context) {
	if a.metrics == nil {
		return
	}
	a.metrics.SetCircuitBreakerState(ctx, a.cb.State())
}

// Compile-time assertion that InMemoryCircuitBreaker satisfies ports.CircuitBreaker.
var _ ports.CircuitBreaker = (*InMemoryCircuitBreaker)(nil)
