package ratelimit

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/vibewarden/vibewarden/internal/ports"
)

// Plugin is the rate-limiting plugin for VibeWarden.
// It implements ports.Plugin and ports.CaddyContributor.
//
// On Init the plugin creates per-IP and per-user MemoryStore rate limiters.
// On Stop it closes both limiters to release the background GC goroutines.
// ContributeCaddyHandlers returns the vibewarden_rate_limit Caddy handler
// fragment so the middleware is injected into the catch-all handler chain.
// Health reports whether the plugin is enabled.
type Plugin struct {
	cfg         Config
	factory     ports.RateLimiterFactory
	ipLimiter   ports.RateLimiter
	userLimiter ports.RateLimiter
	logger      *slog.Logger
}

// New creates a new rate-limiting Plugin.
// factory is the RateLimiterFactory used to create the per-IP and per-user
// limiters during Init. Pass ratelimitadapter.NewDefaultMemoryFactory() for
// production use.
func New(cfg Config, factory ports.RateLimiterFactory, logger *slog.Logger) *Plugin {
	return &Plugin{
		cfg:     cfg,
		factory: factory,
		logger:  logger,
	}
}

// Name returns the canonical plugin identifier "rate-limiting".
// This must match the key used under plugins: in vibewarden.yaml.
func (p *Plugin) Name() string { return "rate-limiting" }

// Priority returns the plugin's initialisation priority.
// Rate limiting is assigned priority 50 so it runs after TLS (10) and
// security-headers (20) but before application-layer middleware.
func (p *Plugin) Priority() int { return 50 }

// Init creates the per-IP and per-user rate limiters from the configured
// factory. It is a no-op when the plugin is disabled.
// Init must be called before ContributeCaddyHandlers.
func (p *Plugin) Init(_ context.Context) error {
	if !p.cfg.Enabled {
		return nil
	}

	p.ipLimiter = p.factory.NewLimiter(ports.RateLimitRule{
		RequestsPerSecond: p.cfg.PerIP.RequestsPerSecond,
		Burst:             p.cfg.PerIP.Burst,
	})

	p.userLimiter = p.factory.NewLimiter(ports.RateLimitRule{
		RequestsPerSecond: p.cfg.PerUser.RequestsPerSecond,
		Burst:             p.cfg.PerUser.Burst,
	})

	p.logger.Info("rate-limiting plugin initialised",
		slog.Float64("per_ip_rps", p.cfg.PerIP.RequestsPerSecond),
		slog.Int("per_ip_burst", p.cfg.PerIP.Burst),
		slog.Float64("per_user_rps", p.cfg.PerUser.RequestsPerSecond),
		slog.Int("per_user_burst", p.cfg.PerUser.Burst),
	)
	return nil
}

// Start is a no-op for the rate-limiting plugin.
// Limiters are created during Init; no additional background work is started
// here. The MemoryStore's own cleanup goroutine is managed internally.
func (p *Plugin) Start(_ context.Context) error { return nil }

// Stop closes the per-IP and per-user limiters to release the background GC
// goroutines. It is safe to call Stop when the plugin is disabled.
func (p *Plugin) Stop(_ context.Context) error {
	if p.ipLimiter != nil {
		if err := p.ipLimiter.Close(); err != nil {
			return fmt.Errorf("closing IP rate limiter: %w", err)
		}
	}
	if p.userLimiter != nil {
		if err := p.userLimiter.Close(); err != nil {
			return fmt.Errorf("closing user rate limiter: %w", err)
		}
	}
	return nil
}

// Health returns the current health status of the rate-limiting plugin.
// The plugin is always healthy; it reports whether it is enabled or disabled.
func (p *Plugin) Health() ports.HealthStatus {
	if !p.cfg.Enabled {
		return ports.HealthStatus{
			Healthy: true,
			Message: "rate-limiting disabled",
		}
	}
	return ports.HealthStatus{
		Healthy: true,
		Message: fmt.Sprintf(
			"rate-limiting active (per-IP: %.1f rps burst %d, per-user: %.1f rps burst %d)",
			p.cfg.PerIP.RequestsPerSecond, p.cfg.PerIP.Burst,
			p.cfg.PerUser.RequestsPerSecond, p.cfg.PerUser.Burst,
		),
	}
}

// ContributeCaddyRoutes returns nil.
// The rate-limiting plugin does not add named routes; it contributes a
// catch-all handler via ContributeCaddyHandlers.
func (p *Plugin) ContributeCaddyRoutes() []ports.CaddyRoute { return nil }

// ContributeCaddyHandlers returns the Caddy vibewarden_rate_limit handler
// fragment that enforces rate limiting on every request. Returns an empty
// slice when the plugin is disabled.
//
// The returned handler has Priority 50 so it is placed after security-headers
// (20) but before the reverse proxy in the catch-all handler chain.
func (p *Plugin) ContributeCaddyHandlers() []ports.CaddyHandler {
	if !p.cfg.Enabled {
		return nil
	}
	handler, err := buildRateLimitHandlerJSON(p.cfg)
	if err != nil {
		// buildRateLimitHandlerJSON only fails on JSON marshal of a known
		// struct — this cannot happen in practice. Log and return nothing so
		// we do not panic in library code.
		p.logger.Error("rate-limiting plugin: building handler JSON", slog.String("err", err.Error()))
		return nil
	}
	return []ports.CaddyHandler{
		{
			Handler:  handler,
			Priority: 50,
		},
	}
}

// ---------------------------------------------------------------------------
// Internal builders — pure functions, no side effects.
// ---------------------------------------------------------------------------

// rateLimitHandlerConfig is the JSON-serialisable configuration sent to the
// vibewarden_rate_limit Caddy module. It mirrors the fields on
// caddy.RateLimitHandlerConfig so the module can unmarshal them correctly.
type rateLimitHandlerConfig struct {
	Enabled           bool              `json:"enabled"`
	PerIP             rateLimitRuleJSON `json:"per_ip"`
	PerUser           rateLimitRuleJSON `json:"per_user"`
	TrustProxyHeaders bool              `json:"trust_proxy_headers"`
	ExemptPaths       []string          `json:"exempt_paths,omitempty"`
}

// rateLimitRuleJSON is the JSON shape of one rate-limit rule dimension.
type rateLimitRuleJSON struct {
	RequestsPerSecond float64 `json:"requests_per_second"`
	Burst             int     `json:"burst"`
}

// buildRateLimitHandlerJSON serialises cfg into the Caddy handler JSON map
// expected by the vibewarden_rate_limit module.
func buildRateLimitHandlerJSON(cfg Config) (map[string]any, error) {
	hcfg := rateLimitHandlerConfig{
		Enabled:           cfg.Enabled,
		TrustProxyHeaders: cfg.TrustProxyHeaders,
		ExemptPaths:       cfg.ExemptPaths,
		PerIP: rateLimitRuleJSON{
			RequestsPerSecond: cfg.PerIP.RequestsPerSecond,
			Burst:             cfg.PerIP.Burst,
		},
		PerUser: rateLimitRuleJSON{
			RequestsPerSecond: cfg.PerUser.RequestsPerSecond,
			Burst:             cfg.PerUser.Burst,
		},
	}

	cfgBytes, err := json.Marshal(hcfg)
	if err != nil {
		return nil, fmt.Errorf("marshaling rate limit handler config: %w", err)
	}

	return map[string]any{
		"handler": "vibewarden_rate_limit",
		"config":  json.RawMessage(cfgBytes),
	}, nil
}

// ---------------------------------------------------------------------------
// Interface guards.
// ---------------------------------------------------------------------------

var (
	_ ports.Plugin           = (*Plugin)(nil)
	_ ports.CaddyContributor = (*Plugin)(nil)
)
