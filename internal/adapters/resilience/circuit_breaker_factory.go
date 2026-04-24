package resilience

import (
	"fmt"
	"log/slog"

	"github.com/vibewarden/vibewarden/internal/ports"
)

// InMemoryCircuitBreakerFactory implements ports.CircuitBreakerFactory. It
// holds the shared dependencies (logger, event logger, metrics, audit logger)
// that are wired once at startup and reused for every circuit breaker instance
// it creates.
type InMemoryCircuitBreakerFactory struct {
	logger      *slog.Logger
	eventLogger ports.EventLogger
	metrics     ports.MetricsCollectorWithCircuitBreaker
	auditLogger ports.AuditEventLogger
}

// NewInMemoryCircuitBreakerFactory creates an InMemoryCircuitBreakerFactory with
// the supplied dependencies. All fields are optional (nil is accepted) except
// that nil metrics simply disables gauge reporting.
func NewInMemoryCircuitBreakerFactory(
	logger *slog.Logger,
	eventLogger ports.EventLogger,
	metrics ports.MetricsCollectorWithCircuitBreaker,
	auditLogger ports.AuditEventLogger,
) *InMemoryCircuitBreakerFactory {
	return &InMemoryCircuitBreakerFactory{
		logger:      logger,
		eventLogger: eventLogger,
		metrics:     metrics,
		auditLogger: auditLogger,
	}
}

// NewCircuitBreaker implements ports.CircuitBreakerFactory. It returns a new
// InMemoryCircuitBreaker configured with cfg. The factory's pre-wired logger,
// event logger, metrics, and audit logger are attached to every instance.
//
// Returns an error when cfg contains an invalid threshold or timeout (threshold
// ≤ 0 or timeout ≤ 0).
func (f *InMemoryCircuitBreakerFactory) NewCircuitBreaker(cfg ports.CircuitBreakerConfig) (ports.CircuitBreaker, error) {
	cb, err := NewInMemoryCircuitBreaker(cfg, f.logger, f.eventLogger, f.metrics)
	if err != nil {
		return nil, fmt.Errorf("creating circuit breaker: %w", err)
	}
	if f.auditLogger != nil {
		cb.WithAuditLogger(f.auditLogger)
	}
	return cb, nil
}

// Compile-time assertion that InMemoryCircuitBreakerFactory satisfies the port.
var _ ports.CircuitBreakerFactory = (*InMemoryCircuitBreakerFactory)(nil)
