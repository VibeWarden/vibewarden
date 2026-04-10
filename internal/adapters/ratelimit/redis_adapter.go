package ratelimit

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/vibewarden/vibewarden/internal/ports"
)

// tokenBucketScript is a Lua script that implements an atomic token bucket
// check-and-consume operation in Redis.
//
// Keys:
//
//	KEYS[1] — the Redis key for this bucket (e.g. "vibewarden:ratelimit:ip:1.2.3.4")
//
// Args:
//
//	ARGV[1] — burst capacity (integer, max tokens)
//	ARGV[2] — refill rate in tokens per microsecond (float, as string)
//	ARGV[3] — TTL in seconds (integer)
//	ARGV[4] — current time in microseconds (integer)
//
// Returns a two-element array:
//
//	[0] — 1 if allowed, 0 if denied
//	[1] — remaining tokens (integer, floor)
//
// Algorithm: token bucket with lazy refill.
// The bucket stores {tokens, last_refill_us} as a Redis hash.
// On each call we compute how many tokens have been refilled since last_refill_us,
// cap at burst, subtract 1, and persist the result.
var tokenBucketScript = redis.NewScript(`
local key       = KEYS[1]
local burst     = tonumber(ARGV[1])
local rate_us   = tonumber(ARGV[2])
local ttl_secs  = tonumber(ARGV[3])
local now_us    = tonumber(ARGV[4])

local data = redis.call("HMGET", key, "tokens", "last_us")
local tokens  = tonumber(data[1])
local last_us = tonumber(data[2])

if tokens == nil then
  tokens  = burst
  last_us = now_us
else
  local elapsed = now_us - last_us
  if elapsed < 0 then elapsed = 0 end
  local refill = elapsed * rate_us
  tokens = tokens + refill
  if tokens > burst then tokens = burst end
  last_us = now_us
end

local allowed   = 0
local remaining = 0

if tokens >= 1 then
  tokens  = tokens - 1
  allowed = 1
end

remaining = math.floor(tokens)

redis.call("HMSET", key, "tokens", tokens, "last_us", last_us)
redis.call("EXPIRE", key, ttl_secs)

return {allowed, remaining}
`)

// RedisStore implements ports.RateLimiter using Redis for distributed,
// atomic token-bucket rate limiting. It uses a Lua script to guarantee
// atomicity without MULTI/EXEC round-trips.
//
// Key format: vibewarden:ratelimit:{prefix}:{key}
// TTL on each key matches the token bucket refill window.
type RedisStore struct {
	client    redis.Cmdable
	rule      ports.RateLimitRule
	keyPrefix string
}

// NewRedisStore creates a RedisStore backed by the provided Redis client.
//
//   - client    — a connected *redis.Client or *redis.ClusterClient.
//   - rule      — the rate limit rule (requests per second, burst).
//   - keyPrefix — namespace prefix appended to every key
//     (e.g. "vibewarden:ratelimit:ip").
func NewRedisStore(client redis.Cmdable, rule ports.RateLimitRule, keyPrefix string) *RedisStore {
	return &RedisStore{
		client:    client,
		rule:      rule,
		keyPrefix: keyPrefix,
	}
}

// Allow implements ports.RateLimiter.
// It executes the Lua token bucket script against Redis.
// On any Redis error it returns an allow decision so that infrastructure
// failures are not propagated to callers as request blocks.
func (s *RedisStore) Allow(ctx context.Context, key string) ports.RateLimitResult {
	r := s.rule
	burst := r.Burst
	rps := r.RequestsPerSecond

	// TTL is set to 2× the refill window to guarantee cleanup while keeping
	// keys alive for active clients.  With burst=B and rate=R the window is B/R
	// seconds; floor at 60s so low-traffic keys still expire within a minute.
	ttlSecs := int64(math.Ceil(float64(burst)/rps) * 2)
	if ttlSecs < 60 {
		ttlSecs = 60
	}

	// Rate expressed in tokens per microsecond for the Lua script.
	ratePerMicro := rps / 1e6

	nowUS := time.Now().UnixMicro()
	redisKey := fmt.Sprintf("%s:%s", s.keyPrefix, key)

	res, err := tokenBucketScript.Run(
		ctx,
		s.client,
		[]string{redisKey},
		burst,
		ratePerMicro,
		ttlSecs,
		nowUS,
	).Int64Slice()

	if err != nil {
		// On Redis error, allow the request (fail-open is the default;
		// the FallbackStore wrapper handles fail-closed if configured).
		return ports.RateLimitResult{
			Allowed:   true,
			Remaining: burst,
			Limit:     rps,
			Burst:     burst,
		}
	}

	allowed := res[0] == 1
	remaining := int(res[1])

	retryAfter := time.Duration(0)
	if !allowed && rps > 0 {
		// Estimate wait: one token / rate.
		retryAfter = time.Duration(float64(time.Second) / rps)
	}

	return ports.RateLimitResult{
		Allowed:    allowed,
		Remaining:  remaining,
		RetryAfter: retryAfter,
		Limit:      rps,
		Burst:      burst,
	}
}

// Close is a no-op for RedisStore.
// The caller owns the Redis client lifecycle and must close it separately.
// Implements ports.RateLimiter.
func (s *RedisStore) Close() error { return nil }

// ---------------------------------------------------------------------------
// Interface guard.
// ---------------------------------------------------------------------------

var _ ports.RateLimiter = (*RedisStore)(nil)
