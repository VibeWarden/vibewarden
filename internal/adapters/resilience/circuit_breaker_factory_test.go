package resilience_test

import (
	"testing"
	"time"

	"github.com/vibewarden/vibewarden/internal/adapters/resilience"
	"github.com/vibewarden/vibewarden/internal/domain/audit"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// TestInMemoryCircuitBreakerFactory_NewCircuitBreaker_WiresAuditLogger verifies
// that a factory built with an audit logger produces a circuit breaker that
// emits audit events on state transitions.
func TestInMemoryCircuitBreakerFactory_NewCircuitBreaker_WiresAuditLogger(t *testing.T) {
	al := &fakeAuditEventLogger{}
	factory := resilience.NewInMemoryCircuitBreakerFactory(nil, nil, nil, al)

	cb, err := factory.NewCircuitBreaker(ports.CircuitBreakerConfig{
		Enabled:   true,
		Threshold: 1,
		Timeout:   time.Minute,
	})
	if err != nil {
		t.Fatalf("NewCircuitBreaker() unexpected error: %v", err)
	}

	// Trip the circuit — should emit an audit.circuit_breaker.opened event.
	cb.RecordFailure()

	if !al.hasAuditEventType(audit.EventTypeCircuitBreakerOpened) {
		t.Error("expected audit.circuit_breaker.opened event but none was logged")
	}
}

// TestInMemoryCircuitBreakerFactory_NewCircuitBreaker_RejectsInvalidConfig verifies
// that a zero threshold causes NewCircuitBreaker to return an error.
func TestInMemoryCircuitBreakerFactory_NewCircuitBreaker_RejectsInvalidConfig(t *testing.T) {
	tests := []struct {
		name    string
		cfg     ports.CircuitBreakerConfig
		wantErr bool
	}{
		{
			name: "valid config",
			cfg: ports.CircuitBreakerConfig{
				Enabled:   true,
				Threshold: 5,
				Timeout:   30 * time.Second,
			},
			wantErr: false,
		},
		{
			name: "zero threshold returns error",
			cfg: ports.CircuitBreakerConfig{
				Enabled:   true,
				Threshold: 0,
				Timeout:   30 * time.Second,
			},
			wantErr: true,
		},
		{
			name: "zero timeout returns error",
			cfg: ports.CircuitBreakerConfig{
				Enabled:   true,
				Threshold: 5,
				Timeout:   0,
			},
			wantErr: true,
		},
	}

	factory := resilience.NewInMemoryCircuitBreakerFactory(nil, nil, nil, nil)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := factory.NewCircuitBreaker(tt.cfg)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewCircuitBreaker() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestInMemoryCircuitBreakerFactory_NilAuditLogger verifies that a factory with
// a nil audit logger produces a working circuit breaker without panic.
func TestInMemoryCircuitBreakerFactory_NilAuditLogger(t *testing.T) {
	factory := resilience.NewInMemoryCircuitBreakerFactory(nil, nil, nil, nil)

	cb, err := factory.NewCircuitBreaker(ports.CircuitBreakerConfig{
		Enabled:   true,
		Threshold: 1,
		Timeout:   time.Minute,
	})
	if err != nil {
		t.Fatalf("NewCircuitBreaker() unexpected error: %v", err)
	}

	// Must not panic.
	cb.RecordFailure()
	_ = cb.IsOpen()
}

// TestInMemoryCircuitBreakerFactory_SatisfiesPort is a compile-time check that
// the factory implements ports.CircuitBreakerFactory.
func TestInMemoryCircuitBreakerFactory_SatisfiesPort(t *testing.T) {
	var _ ports.CircuitBreakerFactory = (*resilience.InMemoryCircuitBreakerFactory)(nil)
}
