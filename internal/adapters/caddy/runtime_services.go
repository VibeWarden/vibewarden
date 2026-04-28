package caddy

import (
	"log/slog"
	"sync/atomic"

	"github.com/vibewarden/vibewarden/internal/ports"
)

// RuntimeServices bundles the live service dependencies required by
// VibeWarden's Caddy HTTP handler modules. The composition root
// (cmd/vibewarden) builds these once at startup and publishes them through
// SetRuntimeServices before caddy.Load runs for the first time. Handlers
// retrieve the current set during Provision.
//
// Individual fields may be nil — handlers must check and degrade gracefully
// (log a warning, skip the optional behaviour). This is the same contract
// already honoured by the existing middleware helpers.
type RuntimeServices struct {
	// Logger is the structured logger shared across all handlers.
	Logger *slog.Logger

	// EventLogger is the operational event sink (stdout + OTel + ring buffer
	// in production, an in-memory spy in tests).
	EventLogger ports.EventLogger

	// AuditEventLogger is the security audit sink. When nil, handlers emit a
	// one-time Warn log entry and skip the audit event.
	AuditEventLogger ports.AuditEventLogger

	// RateLimiterFactory creates per-handler rate limiter instances. Required
	// by RateLimitHandler — ProvisionWith returns an error when nil.
	RateLimiterFactory ports.RateLimiterFactory

	// CircuitBreakerFactory creates per-handler circuit breaker instances.
	// Required by CircuitBreakerHandler — ProvisionWith returns an error when nil.
	CircuitBreakerFactory ports.CircuitBreakerFactory

	// UpstreamHealthChecker is the cached upstream background probe. May be nil
	// when the probe is disabled — the health handler renders
	// "upstream":"unknown" in that case and degrades the outer status.
	UpstreamHealthChecker ports.UpstreamHealthChecker

	// SidecarVersion is the running binary version string, used by the health
	// handler to render the "version" field without a separate plumbing path.
	SidecarVersion string
}

// runtimeServicesRegistry is the package-scope atomic pointer. It is written
// only by SetRuntimeServices (the composition root) and read only by
// currentServices (within the caddy adapter package). The zero value of the
// pointer is nil, which currentServices converts to a zero RuntimeServices.
var runtimeServicesRegistry atomic.Pointer[RuntimeServices]

// SetRuntimeServices publishes the services to the atomic registry.
// Safe for concurrent use. Calling this after Provision has run for a handler
// has no effect on already-provisioned handlers; those keep the first set
// (Caddy's handler lifecycle calls Provision exactly once per config load).
func SetRuntimeServices(s RuntimeServices) {
	runtimeServicesRegistry.Store(&s)
}

// currentServices returns the most recently published services, or a
// zero-value RuntimeServices if SetRuntimeServices has never been called.
// Unexported — only the caddy adapter package may read the registry.
func currentServices() RuntimeServices {
	p := runtimeServicesRegistry.Load()
	if p == nil {
		return RuntimeServices{}
	}
	return *p
}
