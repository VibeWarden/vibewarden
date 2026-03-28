package ratelimit

import (
	"context"
	"log/slog"

	"github.com/vibewarden/vibewarden/internal/ports"
)

// FallbackFactory implements ports.RateLimiterFactory and creates FallbackStore
// instances. Each limiter created by this factory delegates to a primary
// (Redis) limiter and falls back to a secondary (memory) limiter on failure.
type FallbackFactory struct {
	primary   ports.RateLimiterFactory
	secondary ports.RateLimiterFactory
	probe     func(ctx context.Context) error
	cfg       FallbackStoreConfig
	logger    *slog.Logger
	eventLog  ports.EventLogger
}

// NewFallbackFactory creates a FallbackFactory.
//
//   - primary   — factory for the preferred store (typically RedisFactory).
//   - secondary — factory for the fallback store (typically MemoryFactory).
//   - probe     — health probe called by each FallbackStore's background goroutine.
//   - cfg       — runtime configuration shared by all created FallbackStores.
//   - logger    — structured logger; must not be nil.
//   - eventLog  — event logger for domain events; may be nil.
func NewFallbackFactory(
	primary, secondary ports.RateLimiterFactory,
	probe func(ctx context.Context) error,
	cfg FallbackStoreConfig,
	logger *slog.Logger,
	eventLog ports.EventLogger,
) *FallbackFactory {
	return &FallbackFactory{
		primary:   primary,
		secondary: secondary,
		probe:     probe,
		cfg:       cfg,
		logger:    logger,
		eventLog:  eventLog,
	}
}

// NewLimiter implements ports.RateLimiterFactory.
// It creates a FallbackStore wrapping a primary and secondary limiter.
func (f *FallbackFactory) NewLimiter(rule ports.RateLimitRule) ports.RateLimiter {
	return NewFallbackStore(
		f.primary.NewLimiter(rule),
		f.secondary.NewLimiter(rule),
		f.probe,
		f.cfg,
		f.logger,
		f.eventLog,
	)
}

// ---------------------------------------------------------------------------
// Interface guard.
// ---------------------------------------------------------------------------

var _ ports.RateLimiterFactory = (*FallbackFactory)(nil)
