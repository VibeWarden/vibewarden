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

// IncUpstreamRetry implements ports.MetricsCollector and does nothing.
func (NoOpMetricsCollector) IncUpstreamRetry(_ string) {}

// SetActiveConnections implements ports.MetricsCollector and does nothing.
func (NoOpMetricsCollector) SetActiveConnections(_ int) {}

// SetCircuitBreakerState implements ports.MetricsCollector and does nothing.
func (NoOpMetricsCollector) SetCircuitBreakerState(_ context.Context, _ resilience.State) {}

// SetUpstreamHealthy implements ports.MetricsCollectorWithUpstreamHealth and does nothing.
func (NoOpMetricsCollector) SetUpstreamHealthy(_ context.Context, _ bool) {}

// IncWAFDetection implements ports.MetricsCollector and does nothing.
func (NoOpMetricsCollector) IncWAFDetection(_, _ string) {}

// IncEgressRequestTotal implements ports.MetricsCollector and does nothing.
func (NoOpMetricsCollector) IncEgressRequestTotal(_, _, _ string) {}

// ObserveEgressDuration implements ports.MetricsCollector and does nothing.
func (NoOpMetricsCollector) ObserveEgressDuration(_, _ string, _ time.Duration) {}

// IncEgressErrorTotal implements ports.MetricsCollector and does nothing.
func (NoOpMetricsCollector) IncEgressErrorTotal(_ string) {}

// SetTLSCertExpirySeconds implements ports.MetricsCollector and does nothing.
func (NoOpMetricsCollector) SetTLSCertExpirySeconds(_ string, _ float64) {}
