package resilience_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/vibewarden/vibewarden/internal/adapters/resilience"
	"github.com/vibewarden/vibewarden/internal/domain/audit"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// fakeAuditEventLogger is a spy that captures audit events for circuit breaker tests.
type fakeAuditEventLogger struct {
	mu     sync.Mutex
	events []audit.AuditEvent
}

func (f *fakeAuditEventLogger) Log(_ context.Context, ev audit.AuditEvent) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, ev)
	return nil
}

func (f *fakeAuditEventLogger) EventTypes() []audit.EventType {
	f.mu.Lock()
	defer f.mu.Unlock()
	types := make([]audit.EventType, len(f.events))
	for i, e := range f.events {
		types[i] = e.EventType
	}
	return types
}

func (f *fakeAuditEventLogger) Count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.events)
}

// hasAuditEventType returns true if the spy received at least one event of the given type.
func (f *fakeAuditEventLogger) hasAuditEventType(eventType audit.EventType) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, e := range f.events {
		if e.EventType == eventType {
			return true
		}
	}
	return false
}

// Compile-time check.
var _ ports.AuditEventLogger = (*fakeAuditEventLogger)(nil)

// newCBWithAudit creates an InMemoryCircuitBreaker with both operational and
// audit event loggers. The callers receive both spies for assertion.
func newCBWithAudit(t *testing.T, threshold int, timeout time.Duration) (
	*resilience.InMemoryCircuitBreaker,
	*fakeEventLogger,
	*fakeAuditEventLogger,
) {
	t.Helper()
	el := &fakeEventLogger{}
	al := &fakeAuditEventLogger{}
	cfg := ports.CircuitBreakerConfig{
		Enabled:   true,
		Threshold: threshold,
		Timeout:   timeout,
	}
	cb, err := resilience.NewInMemoryCircuitBreaker(cfg, nil, el, nil)
	if err != nil {
		t.Fatalf("NewInMemoryCircuitBreaker: %v", err)
	}
	cb.WithAuditLogger(al)
	return cb, el, al
}

func TestInMemoryCircuitBreaker_EmitsAuditOpenedEvent(t *testing.T) {
	cb, _, al := newCBWithAudit(t, 2, time.Minute)

	cb.RecordFailure()
	if al.hasAuditEventType(audit.EventTypeCircuitBreakerOpened) {
		t.Error("should not emit audit.circuit_breaker.opened before threshold is reached")
	}

	cb.RecordFailure() // second failure — trips the circuit
	if !al.hasAuditEventType(audit.EventTypeCircuitBreakerOpened) {
		t.Error("expected audit.circuit_breaker.opened event after threshold, got none")
	}

	types := al.EventTypes()
	if len(types) != 1 || types[0] != audit.EventTypeCircuitBreakerOpened {
		t.Errorf("audit event types = %v, want [%s]", types, audit.EventTypeCircuitBreakerOpened)
	}

	ev := al.events[0]
	if ev.Outcome != audit.OutcomeSuccess {
		t.Errorf("outcome = %q, want %q", ev.Outcome, audit.OutcomeSuccess)
	}
	if ev.Details["threshold"] == nil {
		t.Error("details.threshold should be set")
	}
}

func TestInMemoryCircuitBreaker_EmitsAuditHalfOpenEvent(t *testing.T) {
	cb, _, al := newCBWithAudit(t, 1, 50*time.Millisecond)

	cb.RecordFailure() // trips
	if !cb.IsOpen() {
		t.Fatal("expected circuit to be open")
	}

	time.Sleep(100 * time.Millisecond) // wait for timeout

	if cb.IsOpen() { // triggers Open → HalfOpen transition
		t.Error("expected circuit to allow probe after timeout")
	}

	if !al.hasAuditEventType(audit.EventTypeCircuitBreakerHalfOpen) {
		t.Error("expected audit.circuit_breaker.half_open event but none was logged")
	}
}

func TestInMemoryCircuitBreaker_EmitsAuditClosedEvent(t *testing.T) {
	cb, _, al := newCBWithAudit(t, 1, 50*time.Millisecond)

	cb.RecordFailure()
	time.Sleep(100 * time.Millisecond)
	cb.IsOpen() // → HalfOpen

	cb.RecordSuccess() // → Closed

	if !al.hasAuditEventType(audit.EventTypeCircuitBreakerClosed) {
		t.Error("expected audit.circuit_breaker.closed event but none was logged")
	}
}

func TestInMemoryCircuitBreaker_NilAuditLoggerDoesNotPanic(t *testing.T) {
	cfg := ports.CircuitBreakerConfig{
		Enabled:   true,
		Threshold: 1,
		Timeout:   time.Minute,
	}
	cb, err := resilience.NewInMemoryCircuitBreaker(cfg, nil, nil, nil)
	if err != nil {
		t.Fatalf("NewInMemoryCircuitBreaker: %v", err)
	}
	// No WithAuditLogger call — auditLogger is nil.

	// Must not panic.
	cb.RecordFailure() // triggers Closed → Open
}

func TestInMemoryCircuitBreaker_WithAuditLogger_ReturnsReceiver(t *testing.T) {
	cfg := ports.CircuitBreakerConfig{
		Enabled:   true,
		Threshold: 1,
		Timeout:   time.Minute,
	}
	cb, err := resilience.NewInMemoryCircuitBreaker(cfg, nil, nil, nil)
	if err != nil {
		t.Fatalf("NewInMemoryCircuitBreaker: %v", err)
	}
	al := &fakeAuditEventLogger{}
	returned := cb.WithAuditLogger(al)
	if returned != cb {
		t.Error("WithAuditLogger should return the receiver for method chaining")
	}
}
