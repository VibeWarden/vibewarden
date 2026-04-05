package egress

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"sync/atomic"
	"time"

	egressadapter "github.com/vibewarden/vibewarden/internal/adapters/egress"
	domainegress "github.com/vibewarden/vibewarden/internal/domain/egress"
	"github.com/vibewarden/vibewarden/internal/middleware"
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

	proxy             *egressadapter.Proxy
	llmResponseRoutes []middleware.LLMResponseValidationRouteConfig
	running           atomic.Bool
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

	// Build LLM response validation routes for routes that have it enabled.
	llmValInputs := buildLLMResponseValidationInputs(p.cfg.Routes)
	llmValRoutes, err := middleware.BuildLLMResponseValidationRoutes(llmValInputs)
	if err != nil {
		return fmt.Errorf("egress plugin init: building LLM response validation routes: %w", err)
	}
	p.llmResponseRoutes = llmValRoutes

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

	// Wire per-route circuit breakers, rate limiters, and response caches.
	proxyCfg.CircuitBreakers = egressadapter.NewCircuitBreakerRegistry(p.logger, p.eventLogger)
	proxyCfg.RateLimiters = egressadapter.NewRateLimiterRegistry(p.logger, p.eventLogger)
	proxyCfg.ResponseCaches = egressadapter.NewResponseCacheRegistry()

	// Build per-route mTLS clients for routes that have an MTLSConfig.
	mtlsClients, err := egressadapter.BuildMTLSClients(routes, nil)
	if err != nil {
		return fmt.Errorf("egress plugin init: building mTLS clients: %w", err)
	}
	proxyCfg.MTLSClients = mtlsClients

	resolver := egressadapter.NewRouteResolver(routes)
	p.proxy = egressadapter.NewProxy(proxyCfg, resolver, nil, p.logger)

	p.logger.InfoContext(ctx, "egress plugin initialised",
		slog.String("listen", p.cfg.Listen),
		slog.String("default_policy", p.cfg.DefaultPolicy),
		slog.Int("routes", len(routes)),
		slog.Bool("block_private", p.cfg.BlockPrivate),
		slog.Int("llm_response_validation_routes", len(llmValRoutes)),
	)
	return nil
}

// LLMResponseValidationMiddleware returns the LLM response schema validation
// middleware configured from the plugin's route definitions.
//
// The returned middleware intercepts upstream JSON responses and validates them
// against the per-route JSON Schema. On validation failure the behaviour is
// controlled by the route's Action field: "block" returns 502 Bad Gateway and
// "warn" passes the response through while logging an llm.response_invalid event.
//
// Returns a no-op passthrough middleware when no routes have LLM response
// validation enabled.
func (p *Plugin) LLMResponseValidationMiddleware() func(http.Handler) http.Handler {
	return middleware.LLMResponseValidationMiddleware(p.llmResponseRoutes, p.logger, p.eventLogger)
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

// buildLLMResponseValidationInputs converts the plugin RouteConfig slice into
// LLMResponseValidationRouteInput values for routes that have LLM response
// validation enabled. Routes with LLMResponseValidation.Enabled == false are
// skipped by BuildLLMResponseValidationRoutes but are included here for
// completeness.
func buildLLMResponseValidationInputs(cfgs []RouteConfig) []middleware.LLMResponseValidationRouteInput {
	inputs := make([]middleware.LLMResponseValidationRouteInput, 0, len(cfgs))
	for _, rc := range cfgs {
		if !rc.LLMResponseValidation.Enabled {
			continue
		}
		inputs = append(inputs, middleware.LLMResponseValidationRouteInput{
			Name:    rc.Name,
			Pattern: rc.Pattern,
			Enabled: rc.LLMResponseValidation.Enabled,
			Schema:  rc.LLMResponseValidation.Schema,
			Action:  rc.LLMResponseValidation.Action,
		})
	}
	return inputs
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

	if len(rc.ValidateResponse.StatusCodes) > 0 || len(rc.ValidateResponse.ContentTypes) > 0 {
		opts = append(opts, domainegress.WithValidateResponse(domainegress.ResponseValidationConfig{
			StatusCodes:  rc.ValidateResponse.StatusCodes,
			ContentTypes: rc.ValidateResponse.ContentTypes,
		}))
	}

	if len(rc.Headers.Add) > 0 || len(rc.Headers.RemoveRequest) > 0 || len(rc.Headers.RemoveResponse) > 0 {
		opts = append(opts, domainegress.WithHeaders(domainegress.HeadersConfig{
			InjectHeaders:        rc.Headers.Add,
			StripRequestHeaders:  rc.Headers.RemoveRequest,
			StripResponseHeaders: rc.Headers.RemoveResponse,
		}))
	}

	if rc.Cache.Enabled {
		opts = append(opts, domainegress.WithCache(domainegress.CacheConfig{
			Enabled: rc.Cache.Enabled,
			TTL:     rc.Cache.TTL,
			MaxSize: rc.Cache.MaxSize,
		}))
	}

	if len(rc.Sanitize.Headers) > 0 || len(rc.Sanitize.QueryParams) > 0 || len(rc.Sanitize.BodyFields) > 0 {
		opts = append(opts, domainegress.WithSanitize(domainegress.SanitizeConfig{
			Headers:     rc.Sanitize.Headers,
			QueryParams: rc.Sanitize.QueryParams,
			BodyFields:  rc.Sanitize.BodyFields,
		}))
	}

	if rc.MTLS.CertPath != "" || rc.MTLS.KeyPath != "" || rc.MTLS.CAPath != "" {
		opts = append(opts, domainegress.WithMTLS(domainegress.MTLSConfig{
			CertPath: rc.MTLS.CertPath,
			KeyPath:  rc.MTLS.KeyPath,
			CAPath:   rc.MTLS.CAPath,
		}))
	}

	return opts, nil
}

// Interface guards — compile-time verification that Plugin implements the required interfaces.
var (
	_ ports.Plugin     = (*Plugin)(nil)
	_ ports.PluginMeta = (*Plugin)(nil)
)
