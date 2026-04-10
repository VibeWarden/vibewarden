package resilience_test

import (
	"context"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/vibewarden/vibewarden/internal/adapters/resilience"
	"github.com/vibewarden/vibewarden/internal/domain/events"
	domainresilience "github.com/vibewarden/vibewarden/internal/domain/resilience"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// fakeEventLogger records emitted events for test assertions.
type fakeEventLogger struct {
	mu     sync.Mutex
	events []events.Event
}

func (f *fakeEventLogger) Log(_ context.Context, ev events.Event) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, ev)
	return nil
}

func (f *fakeEventLogger) EventTypes() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	types := make([]string, len(f.events))
	for i, e := range f.events {
		types[i] = e.EventType
	}
	return types
}

func (f *fakeEventLogger) Count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.events)
}

// fakeMetrics records circuit breaker state updates.
type fakeMetrics struct {
	mu     sync.Mutex
	states []domainresilience.State
}

func (f *fakeMetrics) SetCircuitBreakerState(_ context.Context, s domainresilience.State) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.states = append(f.states, s)
}

func (f *fakeMetrics) LastState() (domainresilience.State, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.states) == 0 {
		return 0, false
	}
	return f.states[len(f.states)-1], true
}

// fakeMetrics also needs to implement ports.MetricsCollectorWithCircuitBreaker
// (which embeds ports.MetricsCollector). Provide no-ops for all other methods.
func (f *fakeMetrics) IncRequestTotal(_, _, _ string)                      {}
func (f *fakeMetrics) ObserveRequestDuration(_, _ string, _ time.Duration) {}
func (f *fakeMetrics) IncRateLimitHit(_ string)                            {}
func (f *fakeMetrics) IncAuthDecision(_ string)                            {}
func (f *fakeMetrics) IncUpstreamError()                                   {}
func (f *fakeMetrics) IncUpstreamTimeout()                                 {}
func (f *fakeMetrics) IncUpstreamRetry(_ string)                           {}
func (f *fakeMetrics) SetActiveConnections(_ int)                          {}
func (f *fakeMetrics) IncWAFDetection(_, _ string)                         {}
func (f *fakeMetrics) IncEgressRequestTotal(_, _, _ string)                {}
func (f *fakeMetrics) ObserveEgressDuration(_, _ string, _ time.Duration)  {}
func (f *fakeMetrics) IncEgressErrorTotal(_ string)                        {}
func (f *fakeMetrics) SetTLSCertExpirySeconds(_ string, _ float64)         {}

var _ ports.MetricsCollectorWithCircuitBreaker = (*fakeMetrics)(nil)

func newCB(t *testing.T, threshold int, timeout time.Duration) (*resilience.InMemoryCircuitBreaker, *fakeEventLogger, *fakeMetrics) {
	t.Helper()
	el := &fakeEventLogger{}
	m := &fakeMetrics{}
	cfg := ports.CircuitBreakerConfig{
		Enabled:   true,
		Threshold: threshold,
		Timeout:   timeout,
	}
	cb, err := resilience.NewInMemoryCircuitBreaker(cfg, slog.Default(), el, m)
	if err != nil {
		t.Fatalf("NewInMemoryCircuitBreaker: %v", err)
	}
	return cb, el, m
}

func TestInMemoryCircuitBreaker_InvalidConfig(t *testing.T) {
	cfg := ports.CircuitBreakerConfig{
		Enabled:   true,
		Threshold: 0, // invalid
		Timeout:   time.Minute,
	}
	_, err := resilience.NewInMemoryCircuitBreaker(cfg, nil, nil, nil)
	if err == nil {
		t.Error("expected error for zero threshold")
	}
}

func TestInMemoryCircuitBreaker_InitiallyClosed(t *testing.T) {
	cb, _, _ := newCB(t, 3, time.Minute)
	if cb.IsOpen() {
		t.Error("expected circuit to be closed initially")
	}
	if cb.State() != domainresilience.StateClosed {
		t.Errorf("initial state = %v, want Closed", cb.State())
	}
}

func TestInMemoryCircuitBreaker_TripsAfterThreshold(t *testing.T) {
	cb, el, m := newCB(t, 3, time.Minute)

	cb.RecordFailure()
	cb.RecordFailure()
	if cb.IsOpen() {
		t.Error("should not be open after 2 failures (threshold=3)")
	}

	cb.RecordFailure() // third failure trips the circuit
	if !cb.IsOpen() {
		t.Error("expected circuit open after threshold")
	}
	if cb.State() != domainresilience.StateOpen {
		t.Errorf("state = %v, want Open", cb.State())
	}

	// circuit_breaker.opened event must have been emitted.
	types := el.EventTypes()
	if len(types) == 0 || types[len(types)-1] != events.EventTypeCircuitBreakerOpened {
		t.Errorf("event types = %v, want last = circuit_breaker.opened", types)
	}

	// Gauge must have been updated.
	if s, ok := m.LastState(); !ok || s != domainresilience.StateOpen {
		t.Errorf("metrics last state = %v, want Open", s)
	}
}

func TestInMemoryCircuitBreaker_OpenTransitionsToHalfOpen(t *testing.T) {
	// Use a very short timeout so we can test the transition without sleeping.
	// The transition is driven by time.Now() inside IsOpen; we cannot inject a
	// clock into the adapter directly, so we sleep briefly.
	cb, el, _ := newCB(t, 1, 50*time.Millisecond)

	cb.RecordFailure() // trips
	if !cb.IsOpen() {
		t.Fatal("expected circuit open")
	}

	// Wait for the open timeout to expire.
	time.Sleep(100 * time.Millisecond)

	// First IsOpen call after timeout should return false (probe allowed).
	if cb.IsOpen() {
		t.Error("expected circuit to allow probe after timeout (HalfOpen)")
	}
	if cb.State() != domainresilience.StateHalfOpen {
		t.Errorf("state = %v, want HalfOpen", cb.State())
	}

	// circuit_breaker.half_open event must have been emitted.
	types := el.EventTypes()
	found := false
	for _, et := range types {
		if et == events.EventTypeCircuitBreakerHalfOpen {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("event types = %v, expected circuit_breaker.half_open", types)
	}
}

func TestInMemoryCircuitBreaker_HalfOpenSuccessCloses(t *testing.T) {
	cb, el, m := newCB(t, 1, 50*time.Millisecond)

	cb.RecordFailure()
	time.Sleep(100 * time.Millisecond)
	cb.IsOpen() // transitions to HalfOpen

	cb.RecordSuccess()
	if cb.State() != domainresilience.StateClosed {
		t.Errorf("state = %v, want Closed after probe success", cb.State())
	}
	if cb.IsOpen() {
		t.Error("expected circuit closed after probe success")
	}

	types := el.EventTypes()
	found := false
	for _, et := range types {
		if et == events.EventTypeCircuitBreakerClosed {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("event types = %v, expected circuit_breaker.closed", types)
	}

	if s, ok := m.LastState(); !ok || s != domainresilience.StateClosed {
		t.Errorf("metrics last state = %v, want Closed", s)
	}
}

func TestInMemoryCircuitBreaker_HalfOpenFailureReopens(t *testing.T) {
	cb, el, _ := newCB(t, 1, 50*time.Millisecond)

	cb.RecordFailure()
	time.Sleep(100 * time.Millisecond)
	cb.IsOpen() // transitions to HalfOpen

	cb.RecordFailure() // probe fails → back to Open
	if cb.State() != domainresilience.StateOpen {
		t.Errorf("state = %v, want Open after probe failure", cb.State())
	}
	if !cb.IsOpen() {
		t.Error("expected circuit open after probe failure")
	}

	// Should have two circuit_breaker.opened events (initial trip + re-open).
	count := 0
	for _, et := range el.EventTypes() {
		if et == events.EventTypeCircuitBreakerOpened {
			count++
		}
	}
	if count < 2 {
		t.Errorf("expected at least 2 circuit_breaker.opened events, got %d", count)
	}
}

func TestInMemoryCircuitBreaker_Concurrent(t *testing.T) {
	cb, _, _ := newCB(t, 10, time.Minute)

	const goroutines = 50
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := range goroutines {
		go func(i int) {
			defer wg.Done()
			if i%2 == 0 {
				cb.RecordFailure()
			} else {
				cb.RecordSuccess()
			}
			_ = cb.IsOpen()
			_ = cb.State()
		}(i)
	}
	wg.Wait()
	// No assertion on the final state — just verifying no race / panic.
}
