// Package ports defines the interfaces (ports) for VibeWarden's hexagonal architecture.
package ports

import (
	"context"
	"time"

	"github.com/vibewarden/vibewarden/internal/domain/resilience"
)

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

	// IncUpstreamTimeout increments the upstream timeout counter.
	// It is called once per request that is terminated with a 504 because the
	// upstream did not respond within the configured resilience.timeout.
	IncUpstreamTimeout()

	// IncUpstreamRetry increments the upstream retry counter.
	// It is called once for each retry attempt (not the initial request).
	// The method label is the HTTP method of the retried request.
	IncUpstreamRetry(method string)

	// SetActiveConnections sets the current number of active connections.
	SetActiveConnections(n int)

	// SetCircuitBreakerState records the current circuit breaker state as a
	// gauge. The mapping is: 0=closed, 1=open, 2=half_open, matching the
	// resilience.State constants.
	SetCircuitBreakerState(ctx context.Context, state resilience.State)

	// IncWAFDetection increments the WAF detection counter.
	// Parameters:
	//   - rule: the rule name that fired (e.g. "sqli-union-select")
	//   - mode: "block" or "detect"
	IncWAFDetection(rule, mode string)

	// IncEgressRequestTotal increments the egress request counter.
	// Parameters:
	//   - route: the matched egress route name, or "unmatched" when no route matched
	//   - method: HTTP method (e.g. "GET", "POST")
	//   - statusCode: HTTP response status code as string ("200", "404", etc.),
	//     or "error" when a transport-level error occurred before a response was received
	IncEgressRequestTotal(route, method, statusCode string)

	// ObserveEgressDuration records the duration of a completed egress request.
	// Parameters:
	//   - route: the matched egress route name, or "unmatched" when no route matched
	//   - method: HTTP method (e.g. "GET", "POST")
	//   - duration: round-trip duration from request start to response received
	ObserveEgressDuration(route, method string, duration time.Duration)

	// IncEgressErrorTotal increments the egress transport-error counter.
	// Parameters:
	//   - route: the matched egress route name, or "unmatched" when no route matched
	IncEgressErrorTotal(route string)
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
