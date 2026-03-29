package statesync

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/vibewarden/vibewarden/internal/ports"
)

// RedisConfig holds configuration for the Redis-backed StateSync adapter.
type RedisConfig struct {
	// URL is the Redis connection URL, e.g. "redis://:password@localhost:6379/0".
	// Required.
	URL string

	// Password is the Redis AUTH password. Optional when already embedded in URL.
	Password string

	// DB is the Redis database index. Defaults to 0.
	DB int

	// PoolSize is the maximum number of socket connections held in the pool.
	// Defaults to 10 when zero.
	PoolSize int
}

// RedisStateSync is a Redis-backed implementation of ports.StateSync.
// It uses atomic Redis commands (INCRBY, EXPIRE, SADD, SREM, SISMEMBER, GET)
// to share state across multiple VibeWarden instances that point at the same
// Redis instance. This makes it suitable for distributed rate limiting and
// IP blocklist synchronisation.
//
// RedisStateSync is safe for concurrent use.
type RedisStateSync struct {
	client *redis.Client
}

// NewRedisStateSync creates a RedisStateSync, connects to Redis using cfg,
// and verifies the connection with a PING health check.
//
// The caller must call Close when the adapter is no longer needed so the
// underlying connection pool is released gracefully.
func NewRedisStateSync(ctx context.Context, cfg RedisConfig) (*RedisStateSync, error) {
	if cfg.URL == "" {
		return nil, errors.New("redis statesync: URL must not be empty")
	}

	opts, err := redis.ParseURL(cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("redis statesync: parsing URL: %w", err)
	}

	// Allow explicit override of password and DB after URL parsing.
	if cfg.Password != "" {
		opts.Password = cfg.Password
	}
	if cfg.DB != 0 {
		opts.DB = cfg.DB
	}
	if cfg.PoolSize > 0 {
		opts.PoolSize = cfg.PoolSize
	}

	client := redis.NewClient(opts)

	if err := client.Ping(ctx).Err(); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("redis statesync: health check failed: %w", err)
	}

	return &RedisStateSync{client: client}, nil
}

// IncrementCounter implements ports.StateSync.
//
// It uses INCRBY to atomically add delta to the key and then conditionally
// calls EXPIRE to set the TTL. The GET + EXPIRE combination is not atomic,
// but INCRBY itself is, so the counter value is always consistent. The TTL
// is refreshed on every increment which is the standard pattern for sliding
// windows in Redis-backed rate limiters.
//
// delta must be positive; an error is returned otherwise.
func (r *RedisStateSync) IncrementCounter(ctx context.Context, key string, delta int64, ttl time.Duration) (int64, error) {
	if ctx.Err() != nil {
		return 0, fmt.Errorf("incrementing counter %q: %w", key, ctx.Err())
	}
	if delta <= 0 {
		return 0, errors.New("counter delta must be positive")
	}

	newVal, err := r.client.IncrBy(ctx, key, delta).Result()
	if err != nil {
		return 0, fmt.Errorf("redis statesync: INCRBY %q: %w", key, err)
	}

	if ttl > 0 {
		if err := r.client.Expire(ctx, key, ttl).Err(); err != nil {
			return 0, fmt.Errorf("redis statesync: EXPIRE %q: %w", key, err)
		}
	}

	return newVal, nil
}

// GetCounter implements ports.StateSync.
//
// Returns 0 with a nil error when the key does not exist (Redis returns a
// nil bulk string for missing keys, which go-redis surfaces as redis.Nil).
func (r *RedisStateSync) GetCounter(ctx context.Context, key string) (int64, error) {
	if ctx.Err() != nil {
		return 0, fmt.Errorf("getting counter %q: %w", key, ctx.Err())
	}

	val, err := r.client.Get(ctx, key).Int64()
	if errors.Is(err, redis.Nil) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("redis statesync: GET %q: %w", key, err)
	}
	return val, nil
}

// AddToSet implements ports.StateSync.
//
// Uses SADD to add member to the set. If ttl > 0, EXPIRE is called to
// refresh the TTL on the whole set. Adding an already-present member is
// idempotent (Redis returns 0 but no error).
func (r *RedisStateSync) AddToSet(ctx context.Context, key string, member string, ttl time.Duration) error {
	if ctx.Err() != nil {
		return fmt.Errorf("adding to set %q: %w", key, ctx.Err())
	}
	if member == "" {
		return errors.New("set member must not be empty")
	}

	if err := r.client.SAdd(ctx, key, member).Err(); err != nil {
		return fmt.Errorf("redis statesync: SADD %q %q: %w", key, member, err)
	}

	if ttl > 0 {
		if err := r.client.Expire(ctx, key, ttl).Err(); err != nil {
			return fmt.Errorf("redis statesync: EXPIRE %q: %w", key, err)
		}
	}

	return nil
}

// RemoveFromSet implements ports.StateSync.
//
// Uses SREM to remove member from the set. Removing a non-existent member is
// a no-op (Redis returns 0 but no error).
func (r *RedisStateSync) RemoveFromSet(ctx context.Context, key string, member string) error {
	if ctx.Err() != nil {
		return fmt.Errorf("removing from set %q: %w", key, ctx.Err())
	}
	if member == "" {
		return errors.New("set member must not be empty")
	}

	if err := r.client.SRem(ctx, key, member).Err(); err != nil {
		return fmt.Errorf("redis statesync: SREM %q %q: %w", key, member, err)
	}
	return nil
}

// SetContains implements ports.StateSync.
//
// Uses SISMEMBER. Returns false with a nil error when the key does not exist.
func (r *RedisStateSync) SetContains(ctx context.Context, key string, member string) (bool, error) {
	if ctx.Err() != nil {
		return false, fmt.Errorf("checking set %q: %w", key, ctx.Err())
	}

	ok, err := r.client.SIsMember(ctx, key, member).Result()
	if err != nil {
		return false, fmt.Errorf("redis statesync: SISMEMBER %q %q: %w", key, member, err)
	}
	return ok, nil
}

// Close implements ports.StateSync.
//
// It closes the underlying Redis connection pool. Safe to call more than once;
// subsequent calls return the same (nil) error that go-redis returns after
// the pool has already been closed.
func (r *RedisStateSync) Close() error {
	if err := r.client.Close(); err != nil {
		return fmt.Errorf("redis statesync: closing client: %w", err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Compile-time interface satisfaction check.
// ---------------------------------------------------------------------------

var _ ports.StateSync = (*RedisStateSync)(nil)
