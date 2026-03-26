// Package ports defines the interfaces (ports) for VibeWarden's hexagonal architecture.
package ports

import "time"

// MetricsCollector is the outbound port for recording application metrics.
// Implementations emit metrics to a backend (e.g., Prometheus registry).
// All methods are safe for concurrent use.
type MetricsCollector interface {
	// IncRequestTotal increments the total request counter.
	// Parameters:
	//   - method: HTTP method (GET, POST, etc.)
	//   - statusCode: HTTP response status code as string ("200", "404", etc.)
	//   - pathPattern: Normalized path pattern ("/users/:id", not "/users/123")
	IncRequestTotal(method, statusCode, pathPattern string)

	// ObserveRequestDuration records the duration of a request.
	// Parameters:
	//   - method: HTTP method
	//   - pathPattern: Normalized path pattern
	//   - duration: Request processing time
	ObserveRequestDuration(method, pathPattern string, duration time.Duration)

	// IncRateLimitHit increments the rate limit hit counter.
	// Parameters:
	//   - limitType: "ip" or "user"
	IncRateLimitHit(limitType string)

	// IncAuthDecision increments the auth decision counter.
	// Parameters:
	//   - decision: "allowed" or "blocked"
	IncAuthDecision(decision string)

	// IncUpstreamError increments the upstream error counter.
	IncUpstreamError()

	// SetActiveConnections sets the current number of active connections.
	SetActiveConnections(n int)
}

// MetricsConfig holds configuration for the metrics subsystem.
type MetricsConfig struct {
	// Enabled toggles metrics collection and endpoint (default: true).
	Enabled bool

	// PathPatterns is a list of path normalization patterns.
	// Example: "/users/:id", "/api/v1/items/:item_id/comments/:comment_id"
	// Paths that don't match any pattern are recorded as "other".
	PathPatterns []string
}

// NoOpMetricsCollector is a MetricsCollector that does nothing.
// Use when metrics are disabled.
type NoOpMetricsCollector struct{}

// IncRequestTotal implements MetricsCollector and does nothing.
func (NoOpMetricsCollector) IncRequestTotal(_, _, _ string) {}

// ObserveRequestDuration implements MetricsCollector and does nothing.
func (NoOpMetricsCollector) ObserveRequestDuration(_, _ string, _ time.Duration) {}

// IncRateLimitHit implements MetricsCollector and does nothing.
func (NoOpMetricsCollector) IncRateLimitHit(_ string) {}

// IncAuthDecision implements MetricsCollector and does nothing.
func (NoOpMetricsCollector) IncAuthDecision(_ string) {}

// IncUpstreamError implements MetricsCollector and does nothing.
func (NoOpMetricsCollector) IncUpstreamError() {}

// SetActiveConnections implements MetricsCollector and does nothing.
func (NoOpMetricsCollector) SetActiveConnections(_ int) {}
