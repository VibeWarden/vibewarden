// Package egress implements the HTTP listener and request forwarding adapter
// for the egress proxy plugin.
package egress

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"

	domainegress "github.com/vibewarden/vibewarden/internal/domain/egress"
	"github.com/vibewarden/vibewarden/internal/domain/events"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// rateLimiterEntry holds the concurrency-safe token-bucket limiter for a single
// named egress route.
type rateLimiterEntry struct {
	mu        sync.Mutex
	limiter   *rate.Limiter
	rps       float64
	routeName string
}

// allow reports whether the next request token is available. It is safe for
// concurrent use.
func (e *rateLimiterEntry) allow() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.limiter.Allow()
}

// retryAfter returns how many seconds the caller should wait until one token
// is available.
func (e *rateLimiterEntry) retryAfter() float64 {
	e.mu.Lock()
	defer e.mu.Unlock()
	// Reserve a future token to get the delay, then cancel the reservation so
	// that the actual token bucket is not drained.
	r := e.limiter.Reserve()
	delay := r.Delay()
	r.Cancel()
	if delay <= 0 {
		return 0
	}
	return delay.Seconds()
}

// RateLimiterRegistry maintains one token-bucket limiter per named egress route.
// Entries are created lazily on first access and are only created when the route
// carries a non-empty rate limit expression. It is safe for concurrent use.
type RateLimiterRegistry struct {
	mu      sync.Mutex
	entries map[string]*rateLimiterEntry

	logger  *slog.Logger
	eventFn ports.EventLogger
}

// NewRateLimiterRegistry creates a RateLimiterRegistry. Pass nil for logger to
// use slog.Default(). Pass nil for eventFn to disable structured event emission.
func NewRateLimiterRegistry(logger *slog.Logger, eventFn ports.EventLogger) *RateLimiterRegistry {
	if logger == nil {
		logger = slog.Default()
	}
	return &RateLimiterRegistry{
		entries: make(map[string]*rateLimiterEntry),
		logger:  logger,
		eventFn: eventFn,
	}
}

// parseRateLimit parses a rate limit expression and returns requests per second.
// Supported formats:
//
//	"<n>/s"  — n requests per second
//	"<n>/m"  — n requests per minute  (converted to per-second)
//	"<n>/h"  — n requests per hour    (converted to per-second)
//
// Returns an error when the expression is empty, malformed, or non-positive.
func parseRateLimit(expr string) (float64, error) {
	if expr == "" {
		return 0, fmt.Errorf("rate limit expression is empty")
	}
	parts := strings.SplitN(expr, "/", 2)
	if len(parts) != 2 {
		return 0, fmt.Errorf("rate limit expression %q: expected format <n>/<unit> (e.g. \"100/s\")", expr)
	}
	n, err := strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("rate limit expression %q: count must be a positive number", expr)
	}
	unit := strings.TrimSpace(strings.ToLower(parts[1]))
	switch unit {
	case "s":
		return n, nil
	case "m":
		return n / 60.0, nil
	case "h":
		return n / 3600.0, nil
	default:
		return 0, fmt.Errorf("rate limit expression %q: unsupported unit %q (use s, m, or h)", expr, unit)
	}
}

// getOrCreate returns the rate limiter entry for the given route, creating it
// lazily on first access. Returns (nil, nil) when the route has no rate limit
// configured (empty RateLimit() string).
func (r *RateLimiterRegistry) getOrCreate(route domainegress.Route) (*rateLimiterEntry, error) {
	expr := route.RateLimit()
	if expr == "" {
		return nil, nil
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if e, ok := r.entries[route.Name()]; ok {
		return e, nil
	}

	rps, err := parseRateLimit(expr)
	if err != nil {
		return nil, fmt.Errorf("parsing rate limit for route %q: %w", route.Name(), err)
	}

	// Burst is set to the ceiling of rps so that small-rps routes still accept
	// at least one request per token-bucket fill, and high-rps routes get a
	// proportional burst. Minimum burst is 1.
	burst := int(math.Ceil(rps))
	if burst < 1 {
		burst = 1
	}

	entry := &rateLimiterEntry{
		limiter:   rate.NewLimiter(rate.Limit(rps), burst),
		rps:       rps,
		routeName: route.Name(),
	}
	r.entries[route.Name()] = entry
	return entry, nil
}

// Allow reports whether the request is within the rate limit for the given
// route. It returns (true, nil) when the route has no rate limit configured.
// It returns (false, nil) when the token bucket is exhausted. It returns a
// non-nil error only when the rate limit expression is malformed.
func (r *RateLimiterRegistry) Allow(ctx context.Context, route domainegress.Route) (bool, error) {
	entry, err := r.getOrCreate(route)
	if err != nil {
		return false, err
	}
	if entry == nil {
		return true, nil
	}

	allowed := entry.allow()
	if !allowed {
		retryAfter := entry.retryAfter()
		r.logger.WarnContext(ctx, "egress.rate_limit_hit",
			slog.String("event_type", events.EventTypeEgressRateLimitHit),
			slog.String("route", route.Name()),
			slog.Float64("limit_rps", entry.rps),
			slog.Float64("retry_after_seconds", retryAfter),
		)
		r.emitEvent(ctx, events.NewEgressRateLimitHit(events.EgressRateLimitHitParams{
			Route:             route.Name(),
			Limit:             entry.rps,
			RetryAfterSeconds: retryAfter,
		}))
	}
	return allowed, nil
}

// RetryAfterSeconds returns how many seconds until one token is available for
// the given route. Returns 0 for routes without a rate limit configuration.
// This value is used to populate the Retry-After response header.
func (r *RateLimiterRegistry) RetryAfterSeconds(route domainegress.Route) (float64, error) {
	entry, err := r.getOrCreate(route)
	if err != nil {
		return 0, err
	}
	if entry == nil {
		return 0, nil
	}
	d := entry.retryAfter()
	// Always return at least 1 second so Retry-After is meaningful.
	if d < 1 {
		return 1, nil
	}
	return math.Ceil(d), nil
}

// emitEvent sends a structured event via the EventLogger. Failures are logged
// but do not interrupt request handling.
func (r *RateLimiterRegistry) emitEvent(ctx context.Context, ev events.Event) {
	if r.eventFn == nil {
		return
	}
	if err := r.eventFn.Log(ctx, ev); err != nil {
		r.logger.Error("egress rate limiter: failed to emit event",
			slog.String("event_type", ev.EventType),
			slog.String("err", err.Error()),
		)
	}
}

// retryAfterHeader formats a float64 number of seconds as an integer string
// suitable for the Retry-After HTTP response header (RFC 9110 §10.2.4).
func retryAfterHeader(seconds float64) string {
	s := int(math.Ceil(seconds))
	if s < 1 {
		s = 1
	}
	return strconv.Itoa(s)
}

// Interface guard — RateLimiterRegistry must satisfy the types used by Proxy.
var _ interface {
	Allow(ctx context.Context, route domainegress.Route) (bool, error)
	RetryAfterSeconds(route domainegress.Route) (float64, error)
} = (*RateLimiterRegistry)(nil)

// rateLimiterClock is a small helper so tests can substitute a fake clock for
// time.Now() calls inside the rate limiter. Only used internally.
var rateLimiterClock = time.Now //nolint:unused
