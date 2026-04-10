package ratelimit

import (
	"fmt"

	"github.com/redis/go-redis/v9"

	"github.com/vibewarden/vibewarden/internal/ports"
)

// RedisFactory implements ports.RateLimiterFactory and creates RedisStore
// instances. All stores created by this factory share the same Redis client
// but receive their own rule and key prefix.
type RedisFactory struct {
	client    *redis.Client
	keyPrefix string
	limiterN  int // sequential counter used to generate unique key prefixes
}

// NewRedisFactory creates a RedisFactory.
//
//   - client    — an already-connected *redis.Client (the caller must Close it).
//   - keyPrefix — base key prefix; each limiter appends a sub-prefix.
//     Recommended value: "vibewarden:ratelimit".
func NewRedisFactory(client *redis.Client, keyPrefix string) *RedisFactory {
	return &RedisFactory{
		client:    client,
		keyPrefix: keyPrefix,
	}
}

// NewLimiter implements ports.RateLimiterFactory.
// It returns a new RedisStore configured with the supplied rule.
// The key prefix is composed as "<base>:<n>" where n is an incrementing counter
// so that IP and user limiters do not share key space.
func (f *RedisFactory) NewLimiter(rule ports.RateLimitRule) ports.RateLimiter {
	f.limiterN++
	prefix := fmt.Sprintf("%s:%d", f.keyPrefix, f.limiterN)
	return NewRedisStore(f.client, rule, prefix)
}

// Client returns the underlying Redis client.
// Use this to perform health checks or to close the client on shutdown.
func (f *RedisFactory) Client() *redis.Client {
	return f.client
}

// ---------------------------------------------------------------------------
// Interface guard.
// ---------------------------------------------------------------------------

var _ ports.RateLimiterFactory = (*RedisFactory)(nil)
