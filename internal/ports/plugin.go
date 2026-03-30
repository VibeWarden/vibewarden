package ports

import (
	"context"
	"time"
)

// HealthStatus reports the current health of a plugin or dependency.
type HealthStatus struct {
	// Healthy is true when the plugin is operating normally.
	Healthy bool

	// Message provides a human-readable explanation of the current status.
	// It should describe the problem when Healthy is false.
	Message string
}

// DependencyStatus is the detailed health report for a single named dependency.
// It is included in the health endpoint response when dependency checking is enabled.
type DependencyStatus struct {
	// Status is "healthy" or "unhealthy".
	Status string `json:"status"`

	// LatencyMS is the round-trip latency of the last health probe in milliseconds.
	// Zero when the check failed before a response was received.
	LatencyMS int64 `json:"latency_ms"`

	// Error is set to a short error description when Status is "unhealthy".
	// Empty when Status is "healthy".
	Error string `json:"error,omitempty"`
}

// DependencyChecker is an optional interface implemented by plugins that expose
// a live connectivity probe for the health endpoint. Unlike Health(), which
// returns cached state, CheckDependency performs a real probe and must be
// called with an appropriate timeout.
type DependencyChecker interface {
	// CheckDependency performs a live health probe and returns a
	// DependencyStatus. The latency is measured from the start of the call.
	// Implementations must honour the context deadline.
	CheckDependency(ctx context.Context) DependencyStatus

	// DependencyName returns a short identifier for this dependency
	// (e.g. "kratos", "postgres"). Used as the key in the health response.
	DependencyName() string
}

// HealthSummaryStatus is the overall status of the health endpoint.
type HealthSummaryStatus string

const (
	// HealthSummaryOK means all monitored dependencies are healthy.
	HealthSummaryOK HealthSummaryStatus = "ok"

	// HealthSummaryDegraded means some dependencies are unhealthy but the
	// sidecar is still serving requests in a degraded mode.
	HealthSummaryDegraded HealthSummaryStatus = "degraded"

	// HealthSummaryUnhealthy means critical dependencies are down and the
	// sidecar cannot serve protected requests correctly.
	HealthSummaryUnhealthy HealthSummaryStatus = "unhealthy"
)

// HealthCheckCache caches dependency health results for a configured TTL to
// avoid hammering dependencies on every health endpoint request.
type HealthCheckCache struct {
	// TTL is how long cached results are considered fresh.
	TTL time.Duration
}

// Plugin is the core interface that all VibeWarden plugins must implement.
// The lifecycle is: Init → Start → (running) → Stop.
// A plugin is only started when its config has Enabled: true.
type Plugin interface {
	// Name returns the canonical plugin identifier (e.g. "tls", "rate-limiting").
	// Must match the key used in vibewarden.yaml under plugins:.
	Name() string

	// Init prepares the plugin using its configuration. It is called once
	// before Start and must not block. Validate config and allocate resources here.
	Init(ctx context.Context) error

	// Start begins the plugin's background work. It must return promptly;
	// long-running work must be launched in a goroutine. The provided context
	// is for the startup phase only — use a stored context or channel for
	// ongoing work.
	Start(ctx context.Context) error

	// Stop gracefully shuts down the plugin. It must honour the context
	// deadline and return promptly when the context is cancelled.
	Stop(ctx context.Context) error

	// Health returns the current health status of the plugin. It must be
	// safe to call concurrently and must not block.
	Health() HealthStatus
}

// CaddyRoute represents a single route entry to inject into the Caddy config.
// Lower Priority values are placed earlier in the route chain.
type CaddyRoute struct {
	// MatchPath is the URL path prefix or pattern for this route.
	MatchPath string

	// Handler is the raw Caddy handler JSON object for this route.
	Handler map[string]any

	// Priority controls ordering. Lower numbers appear first. Use multiples
	// of 10 (10, 20, 30…) so other plugins can insert between existing entries.
	Priority int
}

// CaddyHandler represents an additional handler to append to the catch-all
// route's handler chain (e.g. middleware applied to every request).
type CaddyHandler struct {
	// Handler is the raw Caddy handler JSON object.
	Handler map[string]any

	// Priority controls ordering within the catch-all handler chain.
	// Lower numbers run first.
	Priority int
}

// CaddyContributor is an optional interface implemented by plugins that need
// to inject routes or handlers into the Caddy configuration. The registry
// collects contributions from all enabled plugins before applying the config.
type CaddyContributor interface {
	// ContributeCaddyRoutes returns the list of routes this plugin adds to
	// the Caddy server block. Called after Init and before Start.
	ContributeCaddyRoutes() []CaddyRoute

	// ContributeCaddyHandlers returns the list of handlers this plugin adds
	// to the catch-all route. Called after Init and before Start.
	ContributeCaddyHandlers() []CaddyHandler
}

// InternalServerPlugin is an optional interface implemented by plugins that
// expose an internal HTTP server (e.g. an admin API or metrics endpoint).
// Caddy reverse-proxies external requests to InternalAddr.
type InternalServerPlugin interface {
	// InternalAddr returns the host:port of the plugin's internal HTTP server
	// (e.g. "127.0.0.1:9092"). The address must be stable after Init returns.
	InternalAddr() string
}

// ReadinessChecker is the outbound port for checking whether all plugins are
// initialised and the upstream application is reachable. It is used by the
// /_vibewarden/ready endpoint to report readiness distinct from liveness.
//
// Implementations should aggregate the health of every registered plugin and,
// when an upstream health checker is configured, the current upstream status.
// A process is considered ready only when all plugins report healthy and the
// upstream is reachable.
type ReadinessChecker interface {
	// Ready returns true when every plugin is healthy and the upstream is
	// reachable. It must be safe for concurrent use and must not block.
	Ready() bool

	// ReadinessStatus returns a detailed per-plugin health map and the current
	// upstream status. The map key is the plugin name; the value is its
	// HealthStatus. upstreamReachable is true when the upstream health checker
	// reports "healthy".
	ReadinessStatus() ReadinessStatus
}

// ReadinessStatus is a point-in-time snapshot of plugin and upstream readiness.
type ReadinessStatus struct {
	// PluginsReady is true when every registered plugin reports healthy.
	PluginsReady bool

	// UpstreamReachable is true when the upstream health checker reports
	// "healthy". It is also true when no upstream checker is configured
	// (readiness does not require an upstream checker to be present).
	UpstreamReachable bool

	// Plugins maps each plugin name to its current HealthStatus.
	Plugins map[string]HealthStatus

	// DegradedPlugins maps the name of each plugin that failed Init or Start
	// to the reason string recorded at failure time. Non-critical plugins that
	// degrade during startup are listed here so callers can surface a
	// "degraded" overall status instead of "unhealthy".
	DegradedPlugins map[string]string
}

// CriticalPlugin is an optional interface implemented by plugins whose
// failure must abort sidecar startup. When a plugin does not implement this
// interface it is treated as non-critical: Init/Start failures are logged as
// errors and the plugin is marked degraded, but startup continues.
//
// Critical plugins: auth, tls, rate-limiting.
// Non-critical plugins: metrics, WAF, secrets, egress, webhooks, body-size,
// CORS, security-headers, ip-filter, user-management.
type CriticalPlugin interface {
	// Critical returns true when the plugin must start successfully for the
	// sidecar to serve requests safely. Returning false (or not implementing
	// this interface at all) means the plugin may degrade gracefully.
	Critical() bool
}

// PluginMeta is an optional interface implemented by plugins that expose
// metadata for CLI display (vibewarden plugins, vibewarden plugins show).
// All compiled-in plugins implement this interface.
type PluginMeta interface {
	// Description returns a short, one-line description of the plugin.
	Description() string

	// ConfigSchema returns a map of field name to field description for use
	// in "vibewarden plugins show <name>" output. Fields should be listed in
	// logical order; the caller is responsible for display ordering.
	ConfigSchema() map[string]string

	// Example returns an example YAML snippet (indented under "plugins:")
	// illustrating a minimal enabled configuration.
	Example() string
}
