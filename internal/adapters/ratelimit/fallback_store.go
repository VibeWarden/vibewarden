package ratelimit

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/vibewarden/vibewarden/internal/domain/events"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// FallbackStore is a ports.RateLimiter decorator that delegates to a primary
// (Redis) store when it is healthy and transparently falls back to a secondary
// (in-memory) store on failure.
//
// A background goroutine periodically probes the primary store. When the probe
// succeeds after a period of failure, the store switches back to primary and
// emits a rate_limit.store_recovered event.
//
// Fail-closed mode (FailClosed: true) suppresses the fallback — if the primary
// store fails, Allow returns denied for every request instead of routing to the
// secondary store. Use this when correctness of rate limiting is more important
// than availability.
type FallbackStore struct {
	primary   ports.RateLimiter
	secondary ports.RateLimiter
	probe     func(ctx context.Context) error

	failClosed  bool
	healthy     atomic.Bool
	checkTicker *time.Ticker
	done        chan struct{}
	wg          sync.WaitGroup

	logger   *slog.Logger
	eventLog ports.EventLogger
	logCtx   context.Context //nolint:containedctx // stored for background goroutine
}

// FallbackStoreConfig holds configuration for NewFallbackStore.
type FallbackStoreConfig struct {
	// HealthCheckInterval is how often the background goroutine probes the
	// primary store. Default: 30 seconds.
	HealthCheckInterval time.Duration

	// FailClosed disables the fallback behaviour. When true, requests are
	// denied rather than routed to the secondary store during primary outages.
	FailClosed bool
}

// NewFallbackStore creates a FallbackStore.
//
//   - primary   — the preferred store (typically Redis).
//   - secondary — the fallback store (typically in-memory).
//   - probe     — a function called by the health check goroutine; should
//     return nil if the primary store is reachable.
//   - cfg       — runtime configuration.
//   - logger    — structured logger; must not be nil.
//   - eventLog  — event logger for structured domain events; may be nil
//     (events are silently dropped when nil).
//
// The caller must call Close to stop the background goroutine.
func NewFallbackStore(
	primary, secondary ports.RateLimiter,
	probe func(ctx context.Context) error,
	cfg FallbackStoreConfig,
	logger *slog.Logger,
	eventLog ports.EventLogger,
) *FallbackStore {
	interval := cfg.HealthCheckInterval
	if interval <= 0 {
		interval = 30 * time.Second
	}

	fs := &FallbackStore{
		primary:     primary,
		secondary:   secondary,
		probe:       probe,
		failClosed:  cfg.FailClosed,
		checkTicker: time.NewTicker(interval),
		done:        make(chan struct{}),
		logger:      logger,
		eventLog:    eventLog,
		logCtx:      context.Background(),
	}
	fs.healthy.Store(true) // assume healthy until first check proves otherwise

	fs.wg.Add(1)
	go fs.runHealthCheck()

	return fs
}

// Allow implements ports.RateLimiter.
// It delegates to the primary store when healthy, and to the secondary store
// (or denies all, in fail-closed mode) when the primary is unhealthy.
func (fs *FallbackStore) Allow(ctx context.Context, key string) ports.RateLimitResult {
	if fs.healthy.Load() {
		result := fs.primary.Allow(ctx, key)
		// A Redis error in RedisStore.Allow returns Allowed=true with no
		// indicator of the error — the RedisStore is fail-open by design.
		// We rely on the health check goroutine to detect prolonged outages.
		return result
	}

	if fs.failClosed {
		return ports.RateLimitResult{
			Allowed:    false,
			Remaining:  0,
			RetryAfter: time.Second,
			Limit:      0,
			Burst:      0,
		}
	}

	return fs.secondary.Allow(ctx, key)
}

// Close stops the health check goroutine and closes both the primary and
// secondary stores. Implements ports.RateLimiter.
func (fs *FallbackStore) Close() error {
	fs.checkTicker.Stop()
	close(fs.done)
	fs.wg.Wait()

	var firstErr error
	if err := fs.primary.Close(); err != nil && firstErr == nil {
		firstErr = err
	}
	if err := fs.secondary.Close(); err != nil && firstErr == nil {
		firstErr = err
	}
	return firstErr
}

// IsHealthy reports whether the primary store is currently considered healthy.
// Useful for health endpoint reporting.
func (fs *FallbackStore) IsHealthy() bool {
	return fs.healthy.Load()
}

// runHealthCheck is the background goroutine that probes the primary store.
func (fs *FallbackStore) runHealthCheck() {
	defer fs.wg.Done()

	for {
		select {
		case <-fs.checkTicker.C:
			fs.check()
		case <-fs.done:
			return
		}
	}
}

// check probes the primary store and updates the healthy flag.
func (fs *FallbackStore) check() {
	ctx, cancel := context.WithTimeout(fs.logCtx, 5*time.Second)
	defer cancel()

	err := fs.probe(ctx)
	wasHealthy := fs.healthy.Load()

	if err != nil {
		if wasHealthy {
			fs.healthy.Store(false)
			fs.logger.Warn("rate limiter: primary store unavailable, switched to fallback",
				slog.String("error", err.Error()),
			)
			fs.emitEvent(events.NewRateLimitStoreFallback(events.RateLimitStoreFallbackParams{
				Reason: err.Error(),
			}))
		}
		return
	}

	if !wasHealthy {
		fs.healthy.Store(true)
		fs.logger.Info("rate limiter: primary store recovered, switched back to primary")
		fs.emitEvent(events.NewRateLimitStoreRecovered())
	}
}

// emitEvent logs a domain event if an event logger was configured.
func (fs *FallbackStore) emitEvent(e events.Event) {
	if fs.eventLog == nil {
		return
	}
	_ = fs.eventLog.Log(fs.logCtx, e)
}

// ---------------------------------------------------------------------------
// Interface guard.
// ---------------------------------------------------------------------------

var _ ports.RateLimiter = (*FallbackStore)(nil)
