package metrics

import (
	"context"
	"time"

	"github.com/vibewarden/vibewarden/internal/domain/resilience"
)

// NoOpMetricsCollector is a MetricsCollector implementation that discards all
// observations. Use it when metrics collection is disabled to satisfy the
// ports.MetricsCollector interface without incurring any overhead.
type NoOpMetricsCollector struct{}

// IncRequestTotal implements ports.MetricsCollector and does nothing.
func (NoOpMetricsCollector) IncRequestTotal(_, _, _ string) {}

// ObserveRequestDuration implements ports.MetricsCollector and does nothing.
func (NoOpMetricsCollector) ObserveRequestDuration(_, _ string, _ time.Duration) {}

// IncRateLimitHit implements ports.MetricsCollector and does nothing.
func (NoOpMetricsCollector) IncRateLimitHit(_ string) {}

// IncAuthDecision implements ports.MetricsCollector and does nothing.
func (NoOpMetricsCollector) IncAuthDecision(_ string) {}

// IncUpstreamError implements ports.MetricsCollector and does nothing.
func (NoOpMetricsCollector) IncUpstreamError() {}

// IncUpstreamTimeout implements ports.MetricsCollector and does nothing.
func (NoOpMetricsCollector) IncUpstreamTimeout() {}

// SetActiveConnections implements ports.MetricsCollector and does nothing.
func (NoOpMetricsCollector) SetActiveConnections(_ int) {}

// SetCircuitBreakerState implements ports.MetricsCollector and does nothing.
func (NoOpMetricsCollector) SetCircuitBreakerState(_ context.Context, _ resilience.State) {}
