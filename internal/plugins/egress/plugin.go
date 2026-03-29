package egress

import (
	"context"
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"

	egressadapter "github.com/vibewarden/vibewarden/internal/adapters/egress"
	domainegress "github.com/vibewarden/vibewarden/internal/domain/egress"
	"github.com/vibewarden/vibewarden/internal/ports"
)

const (
	// pluginName is the canonical identifier used in vibewarden.yaml.
	pluginName = "egress"

	// defaultListen is the default TCP address for the egress proxy listener.
	defaultListen = "127.0.0.1:8081"

	// defaultPolicy is the disposition applied when no route matches.
	defaultPolicy = "deny"
)

// Plugin is the VibeWarden egress proxy plugin.
// It implements ports.Plugin and ports.PluginMeta.
//
// Lifecycle:
//
//  1. Init — validates config, builds domain Route objects, creates the proxy.
//  2. Start — binds the TCP listener and begins serving egress requests.
//  3. Stop — gracefully shuts down the HTTP server.
//
// The plugin is a no-op when Enabled is false.
type Plugin struct {
	cfg         Config
	logger      *slog.Logger
	eventLogger ports.EventLogger

	proxy   *egressadapter.Proxy
	running atomic.Bool
}

// New creates a new egress Plugin.
// eventLogger may be nil; when non-nil, structured events are emitted through it.
// logger may be nil; when nil, slog.Default() is used.
func New(cfg Config, eventLogger ports.EventLogger, logger *slog.Logger) *Plugin {
	applyDefaults(&cfg)
	if logger == nil {
		logger = slog.Default()
	}
	return &Plugin{
		cfg:         cfg,
		logger:      logger,
		eventLogger: eventLogger,
	}
}

// applyDefaults fills zero-value fields with sensible defaults.
func applyDefaults(cfg *Config) {
	if cfg.Listen == "" {
		cfg.Listen = defaultListen
	}
	if cfg.DefaultPolicy == "" {
		cfg.DefaultPolicy = defaultPolicy
	}
	if cfg.DefaultTimeout == 0 {
		cfg.DefaultTimeout = 30 * time.Second
	}
}

// Name returns the canonical plugin identifier "egress".
func (p *Plugin) Name() string { return pluginName }

// Init validates the configuration and prepares the egress proxy.
// Returns an error when the plugin is enabled and the configuration is invalid.
func (p *Plugin) Init(ctx context.Context) error {
	if !p.cfg.Enabled {
		p.logger.InfoContext(ctx, "egress plugin disabled — skipping")
		return nil
	}

	routes, err := buildRoutes(p.cfg.Routes)
	if err != nil {
		return fmt.Errorf("egress plugin init: building routes: %w", err)
	}

	policy := domainegress.Policy(p.cfg.DefaultPolicy)
	if policy != domainegress.PolicyAllow && policy != domainegress.PolicyDeny {
		return fmt.Errorf("egress plugin init: unknown default_policy %q (must be \"allow\" or \"deny\")", p.cfg.DefaultPolicy)
	}

	proxyCfg := egressadapter.ProxyConfig{
		Listen:                   p.cfg.Listen,
		DefaultPolicy:            policy,
		DefaultTimeout:           p.cfg.DefaultTimeout,
		DefaultBodySizeLimit:     p.cfg.DefaultBodySizeLimit,
		DefaultResponseSizeLimit: p.cfg.DefaultResponseSizeLimit,
		Routes:                   routes,
		AllowInsecure:            p.cfg.AllowInsecure,
		EventLogger:              p.eventLogger,
	}

	// Wire SSRF guard when private-IP blocking is enabled.
	if p.cfg.BlockPrivate {
		guard, err := egressadapter.NewSSRFGuard(egressadapter.SSRFGuardConfig{
			BlockPrivate:   true,
			AllowedPrivate: p.cfg.AllowedPrivate,
		})
		if err != nil {
			return fmt.Errorf("egress plugin init: creating SSRF guard: %w", err)
		}
		proxyCfg.SSRFGuard = guard
	}

	// Wire per-route circuit breakers and rate limiters.
	proxyCfg.CircuitBreakers = egressadapter.NewCircuitBreakerRegistry(p.logger, p.eventLogger)
	proxyCfg.RateLimiters = egressadapter.NewRateLimiterRegistry(p.logger, p.eventLogger)

	resolver := egressadapter.NewRouteResolver(routes)
	p.proxy = egressadapter.NewProxy(proxyCfg, resolver, nil, p.logger)

	p.logger.InfoContext(ctx, "egress plugin initialised",
		slog.String("listen", p.cfg.Listen),
		slog.String("default_policy", p.cfg.DefaultPolicy),
		slog.Int("routes", len(routes)),
		slog.Bool("block_private", p.cfg.BlockPrivate),
	)
	return nil
}

// Start binds the TCP listener and begins serving egress requests.
// It returns immediately; the server runs in the background until Stop is called.
func (p *Plugin) Start(ctx context.Context) error {
	if !p.cfg.Enabled {
		return nil
	}

	if err := p.proxy.Start(); err != nil {
		return fmt.Errorf("egress plugin start: %w", err)
	}

	p.running.Store(true)
	p.logger.InfoContext(ctx, "egress proxy started",
		slog.String("listen", p.proxy.Addr()),
	)
	return nil
}

// Stop gracefully shuts down the egress proxy.
// It honours the deadline on the provided context.
func (p *Plugin) Stop(ctx context.Context) error {
	if !p.cfg.Enabled || p.proxy == nil {
		return nil
	}

	p.running.Store(false)

	if err := p.proxy.Stop(ctx); err != nil {
		return fmt.Errorf("egress plugin stop: %w", err)
	}

	p.logger.InfoContext(ctx, "egress proxy stopped")
	return nil
}

// Health returns the current health status of the egress plugin.
// It is safe for concurrent use.
func (p *Plugin) Health() ports.HealthStatus {
	if !p.cfg.Enabled {
		return ports.HealthStatus{Healthy: true, Message: "egress plugin disabled"}
	}
	if p.running.Load() {
		return ports.HealthStatus{Healthy: true, Message: fmt.Sprintf("listening on %s", p.cfg.Listen)}
	}
	return ports.HealthStatus{Healthy: false, Message: "egress proxy not running"}
}

// buildRoutes converts the plugin RouteConfig slice into domain Route values.
// Returns an error if any route has an invalid name, pattern, or configuration.
func buildRoutes(cfgs []RouteConfig) ([]domainegress.Route, error) {
	routes := make([]domainegress.Route, 0, len(cfgs))
	for _, rc := range cfgs {
		opts, err := routeOptions(rc)
		if err != nil {
			return nil, fmt.Errorf("route %q: %w", rc.Name, err)
		}
		r, err := domainegress.NewRoute(rc.Name, rc.Pattern, opts...)
		if err != nil {
			return nil, fmt.Errorf("route %q: %w", rc.Name, err)
		}
		routes = append(routes, r)
	}
	return routes, nil
}

// routeOptions converts a RouteConfig to the corresponding domain RouteOption
// slice. Returns an error when the circuit breaker or retry configuration is
// present but invalid.
func routeOptions(rc RouteConfig) ([]domainegress.RouteOption, error) {
	var opts []domainegress.RouteOption

	if len(rc.Methods) > 0 {
		opts = append(opts, domainegress.WithMethods(rc.Methods...))
	}

	if rc.Timeout > 0 {
		opts = append(opts, domainegress.WithTimeout(rc.Timeout))
	}

	if rc.Secret != "" {
		opts = append(opts, domainegress.WithSecret(domainegress.SecretConfig{
			Name:   rc.Secret,
			Header: rc.SecretHeader,
			Format: rc.SecretFormat,
		}))
	}

	if rc.RateLimit != "" {
		opts = append(opts, domainegress.WithRateLimit(rc.RateLimit))
	}

	if rc.CircuitBreaker.Threshold > 0 || rc.CircuitBreaker.ResetAfter > 0 {
		if rc.CircuitBreaker.Threshold <= 0 {
			return nil, fmt.Errorf("circuit_breaker.threshold must be > 0")
		}
		if rc.CircuitBreaker.ResetAfter <= 0 {
			return nil, fmt.Errorf("circuit_breaker.reset_after must be > 0")
		}
		opts = append(opts, domainegress.WithCircuitBreaker(domainegress.CircuitBreakerConfig{
			Threshold:  rc.CircuitBreaker.Threshold,
			ResetAfter: rc.CircuitBreaker.ResetAfter,
		}))
	}

	if rc.Retries.Max > 0 {
		backoff := domainegress.RetryBackoff(rc.Retries.Backoff)
		if backoff == "" {
			backoff = domainegress.RetryBackoffExponential
		}
		opts = append(opts, domainegress.WithRetry(domainegress.RetryConfig{
			Max:            rc.Retries.Max,
			Methods:        rc.Retries.Methods,
			Backoff:        backoff,
			InitialBackoff: rc.Retries.InitialBackoff,
		}))
	}

	if rc.BodySizeLimit > 0 {
		opts = append(opts, domainegress.WithBodySizeLimit(rc.BodySizeLimit))
	}

	if rc.ResponseSizeLimit > 0 {
		opts = append(opts, domainegress.WithResponseSizeLimit(rc.ResponseSizeLimit))
	}

	if rc.AllowInsecure {
		opts = append(opts, domainegress.WithAllowInsecure(true))
	}

	return opts, nil
}

// Interface guards — compile-time verification that Plugin implements the required interfaces.
var (
	_ ports.Plugin     = (*Plugin)(nil)
	_ ports.PluginMeta = (*Plugin)(nil)
)
