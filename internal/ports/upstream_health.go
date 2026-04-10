package ports

import (
	"context"

	"github.com/vibewarden/vibewarden/internal/domain/health"
)

// UpstreamHealthChecker is the outbound port for monitoring the health of the
// upstream application. Implementations probe the upstream on a configured
// interval and maintain thread-safe state that can be read at any time.
type UpstreamHealthChecker interface {
	// Start begins background health probing. It must return promptly; the
	// probing loop runs in a goroutine. Probing continues until the provided
	// context is cancelled.
	Start(ctx context.Context) error

	// Stop gracefully shuts down the health checker. It must honour the context
	// deadline. After Stop returns no more probes will be issued.
	Stop(ctx context.Context) error

	// CurrentStatus returns the most recently computed health status.
	// It is safe for concurrent use and must not block.
	CurrentStatus() health.UpstreamStatus

	// Snapshot returns a point-in-time snapshot of the full health state.
	// It is safe for concurrent use and must not block.
	Snapshot() UpstreamHealthSnapshot
}

// UpstreamHealthSnapshot is an immutable point-in-time view of the upstream
// health state, suitable for serving from the /_vibewarden/health endpoint.
type UpstreamHealthSnapshot struct {
	// Status is the current health status: "unknown", "healthy", or "unhealthy".
	Status string `json:"status"`

	// ConsecutiveSuccesses is the number of consecutive successful probes.
	ConsecutiveSuccesses int `json:"consecutive_successes"`

	// ConsecutiveFailures is the number of consecutive failed probes.
	ConsecutiveFailures int `json:"consecutive_failures"`

	// LastError is the error message from the most recent failed probe.
	// Empty when the last probe succeeded.
	LastError string `json:"last_error,omitempty"`
}

// MetricsCollectorWithUpstreamHealth extends MetricsCollector with the
// vibewarden_upstream_healthy gauge. Adapters that support this metric
// implement this interface.
type MetricsCollectorWithUpstreamHealth interface {
	MetricsCollector

	// SetUpstreamHealthy sets the vibewarden_upstream_healthy gauge.
	// 1 = healthy, 0 = unhealthy or unknown.
	SetUpstreamHealthy(ctx context.Context, healthy bool)
}
