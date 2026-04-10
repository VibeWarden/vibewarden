package ratelimit

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"

	ratelimitadapter "github.com/vibewarden/vibewarden/internal/adapters/ratelimit"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// Plugin is the rate-limiting plugin for VibeWarden.
// It implements ports.Plugin and ports.CaddyContributor.
//
// On Init the plugin builds the appropriate factory (memory or Redis with
// optional fallback), then creates per-IP and per-user rate limiters.
// On Stop it closes both limiters and the Redis client (if any) to release
// background goroutines and connections.
// ContributeCaddyHandlers returns the vibewarden_rate_limit Caddy handler
// fragment so the middleware is injected into the catch-all handler chain.
// Health reports whether the plugin is enabled and the Redis connection status.
type Plugin struct {
	cfg         Config
	factory     ports.RateLimiterFactory
	ipLimiter   ports.RateLimiter
	userLimiter ports.RateLimiter
	redisClient *redis.Client // non-nil when store == "redis"
	eventLog    ports.EventLogger
	logger      *slog.Logger
}

// New creates a new rate-limiting Plugin.
// factory is the RateLimiterFactory used to create the per-IP and per-user
// limiters during Init. Pass nil to have the plugin build its own factory
// based on the Store configuration; pass a non-nil value to inject a custom
// factory (useful in tests).
// eventLog may be nil — domain events are silently dropped when nil.
func New(cfg Config, factory ports.RateLimiterFactory, logger *slog.Logger) *Plugin {
	return &Plugin{
		cfg:     cfg,
		factory: factory,
		logger:  logger,
	}
}

// NewWithEventLog creates a Plugin that emits structured domain events for
// Redis store transitions (fallback / recovery).
// Equivalent to New when factory is nil — the plugin selects its own factory.
func NewWithEventLog(cfg Config, factory ports.RateLimiterFactory, logger *slog.Logger, eventLog ports.EventLogger) *Plugin {
	p := New(cfg, factory, logger)
	p.eventLog = eventLog
	return p
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
// When no factory was injected at construction time, Init builds one from
// the plugin configuration (memory or Redis with optional fallback).
// Init must be called before ContributeCaddyHandlers.
func (p *Plugin) Init(ctx context.Context) error {
	if !p.cfg.Enabled {
		return nil
	}

	if p.factory == nil {
		f, client, err := p.buildFactory(ctx)
		if err != nil {
			return fmt.Errorf("building rate limiter factory: %w", err)
		}
		p.factory = f
		p.redisClient = client
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
		slog.String("store", p.cfg.Store),
		slog.Float64("per_ip_rps", p.cfg.PerIP.RequestsPerSecond),
		slog.Int("per_ip_burst", p.cfg.PerIP.Burst),
		slog.Float64("per_user_rps", p.cfg.PerUser.RequestsPerSecond),
		slog.Int("per_user_burst", p.cfg.PerUser.Burst),
	)
	return nil
}

// buildRedisClient creates a *redis.Client from the plugin's RedisConfig.
// When cfg.URL is set it is parsed with redis.ParseURL so that both redis://
// and rediss:// (TLS) schemes are supported. Password, DB, and PoolSize fields
// serve as explicit overrides applied on top of whatever the URL specifies.
// When cfg.URL is empty, cfg.Address/Password/DB are used directly.
func buildRedisClient(cfg RedisConfig) (*redis.Client, error) {
	var opts *redis.Options
	if cfg.URL != "" {
		var err error
		opts, err = redis.ParseURL(cfg.URL)
		if err != nil {
			return nil, fmt.Errorf("rate-limiting: parsing redis URL: %w", err)
		}
		// Allow explicit field overrides on top of the URL.
		if cfg.Password != "" {
			opts.Password = cfg.Password
		}
		if cfg.DB != 0 {
			opts.DB = cfg.DB
		}
	} else {
		opts = &redis.Options{
			Addr:     cfg.Address,
			Password: cfg.Password,
			DB:       cfg.DB,
		}
	}
	if cfg.PoolSize > 0 {
		opts.PoolSize = cfg.PoolSize
	}
	return redis.NewClient(opts), nil
}

// buildFactory constructs a RateLimiterFactory based on p.cfg.Store.
// It returns the factory, an optional Redis client (non-nil only for Redis
// stores), and any error encountered during construction.
func (p *Plugin) buildFactory(_ context.Context) (ports.RateLimiterFactory, *redis.Client, error) {
	switch p.cfg.Store {
	case "", "memory":
		return ratelimitadapter.NewDefaultMemoryFactory(), nil, nil

	case "redis":
		rCfg := p.cfg.Redis
		client, err := buildRedisClient(rCfg)
		if err != nil {
			return nil, nil, err
		}

		keyPrefix := rCfg.KeyPrefix
		if keyPrefix == "" {
			keyPrefix = "vibewarden"
		}
		keyPrefix += ":ratelimit"

		redisFactory := ratelimitadapter.NewRedisFactory(client, keyPrefix)

		if !rCfg.Fallback {
			// Fail-closed: use Redis directly, no fallback.
			return redisFactory, client, nil
		}

		// Fail-open: wrap Redis factory with a fallback memory factory.
		memoryFactory := ratelimitadapter.NewDefaultMemoryFactory()

		interval := 30 * time.Second
		if rCfg.HealthCheckInterval != "" {
			d, err := time.ParseDuration(rCfg.HealthCheckInterval)
			if err != nil {
				p.logger.Warn("rate-limiting: invalid health_check_interval, using default 30s",
					slog.String("value", rCfg.HealthCheckInterval),
					slog.String("error", err.Error()),
				)
			} else {
				interval = d
			}
		}

		probe := func(ctx context.Context) error {
			return client.Ping(ctx).Err()
		}

		fallbackFactory := ratelimitadapter.NewFallbackFactory(
			redisFactory,
			memoryFactory,
			probe,
			ratelimitadapter.FallbackStoreConfig{
				HealthCheckInterval: interval,
				FailClosed:          false,
			},
			p.logger,
			p.eventLog,
		)

		return fallbackFactory, client, nil

	default:
		return nil, nil, fmt.Errorf("unknown rate limit store %q", p.cfg.Store)
	}
}

// Start is a no-op for the rate-limiting plugin.
// Limiters are created during Init; no additional background work is started
// here. The MemoryStore's own cleanup goroutine is managed internally.
func (p *Plugin) Start(_ context.Context) error { return nil }

// Stop closes the per-IP and per-user limiters and the Redis client (if any)
// to release background goroutines and connections.
// It is safe to call Stop when the plugin is disabled.
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
	if p.redisClient != nil {
		if err := p.redisClient.Close(); err != nil {
			return fmt.Errorf("closing Redis client: %w", err)
		}
		p.redisClient = nil
	}
	return nil
}

// Health returns the current health status of the rate-limiting plugin.
// When Redis is configured, health reflects whether the Redis connection is
// healthy or whether the fallback store is active.
func (p *Plugin) Health() ports.HealthStatus {
	if !p.cfg.Enabled {
		return ports.HealthStatus{
			Healthy: true,
			Message: "rate-limiting disabled",
		}
	}

	storeStatus := p.storeHealthMessage()

	return ports.HealthStatus{
		Healthy: true,
		Message: fmt.Sprintf(
			"rate-limiting active (per-IP: %.1f rps burst %d, per-user: %.1f rps burst %d%s)",
			p.cfg.PerIP.RequestsPerSecond, p.cfg.PerIP.Burst,
			p.cfg.PerUser.RequestsPerSecond, p.cfg.PerUser.Burst,
			storeStatus,
		),
	}
}

// storeHealthMessage returns a store-specific suffix for the health message.
func (p *Plugin) storeHealthMessage() string {
	if p.cfg.Store != "redis" {
		return ""
	}
	// Check if ip limiter is a FallbackStore and report Redis health.
	if p.ipLimiter == nil {
		return ", store: redis (not initialised)"
	}
	type healthReporter interface {
		IsHealthy() bool
	}
	if hr, ok := p.ipLimiter.(healthReporter); ok {
		if hr.IsHealthy() {
			return ", store: redis (healthy)"
		}
		return ", store: redis (fallback to memory)"
	}
	return ", store: redis"
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
