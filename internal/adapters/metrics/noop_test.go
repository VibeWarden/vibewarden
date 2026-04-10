package metrics_test

import (
	"context"
	"testing"
	"time"

	"github.com/vibewarden/vibewarden/internal/adapters/metrics"
	"github.com/vibewarden/vibewarden/internal/domain/resilience"
	"github.com/vibewarden/vibewarden/internal/ports"
)

func TestNoOpMetricsCollector_SatisfiesInterface(t *testing.T) {
	// Compile-time assertion: NoOpMetricsCollector must satisfy ports.MetricsCollector.
	var _ ports.MetricsCollector = metrics.NoOpMetricsCollector{}
}

func TestNoOpMetricsCollector_AllMethodsAreNoOps(t *testing.T) {
	// All methods must be callable without panic and produce no observable side effects.
	var mc ports.MetricsCollector = metrics.NoOpMetricsCollector{}

	mc.IncRequestTotal("GET", "200", "/health")
	mc.IncRequestTotal("POST", "404", "/api/v1/items/:id")
	mc.ObserveRequestDuration("GET", "/health", time.Second)
	mc.ObserveRequestDuration("POST", "/api/v1/items/:id", 250*time.Millisecond)
	mc.IncRateLimitHit("ip")
	mc.IncRateLimitHit("user")
	mc.IncAuthDecision("allowed")
	mc.IncAuthDecision("blocked")
	mc.IncUpstreamError()
	mc.IncUpstreamTimeout()
	mc.SetActiveConnections(0)
	mc.SetActiveConnections(42)
	mc.SetCircuitBreakerState(context.Background(), resilience.StateClosed)
	mc.SetCircuitBreakerState(context.Background(), resilience.StateOpen)
	mc.SetCircuitBreakerState(context.Background(), resilience.StateHalfOpen)
	mc.IncWAFDetection("sqli-union-select", "block")
	mc.IncWAFDetection("xss-script-tag", "detect")
	mc.IncEgressRequestTotal("stripe", "POST", "200")
	mc.IncEgressRequestTotal("unmatched", "GET", "error")
	mc.ObserveEgressDuration("stripe", "POST", 150*time.Millisecond)
	mc.IncEgressErrorTotal("stripe")
	mc.IncEgressErrorTotal("unmatched")
	mc.SetTLSCertExpirySeconds("example.com", 2592000)
	mc.SetTLSCertExpirySeconds("example.com", -1)
}
