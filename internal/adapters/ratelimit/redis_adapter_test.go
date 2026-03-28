package ratelimit

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/vibewarden/vibewarden/internal/ports"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// newTestRedisStore creates a RedisStore for unit tests.
// It connects to the address set in REDIS_ADDR; if that variable is empty
// the test is skipped because it requires a real Redis instance.
// For proper isolation use the integration test in redis_adapter_integration_test.go.
func newTestRedisStore(t *testing.T, rps float64, burst int, prefix string) (*RedisStore, *redis.Client) {
	t.Helper()

	addr := redisTestAddr(t)
	client := redis.NewClient(&redis.Options{Addr: addr})

	t.Cleanup(func() {
		// Remove all test keys.
		ctx := context.Background()
		keys, _ := client.Keys(ctx, prefix+":*").Result()
		if len(keys) > 0 {
			client.Del(ctx, keys...)
		}
		client.Close()
	})

	rule := ports.RateLimitRule{RequestsPerSecond: rps, Burst: burst}
	store := NewRedisStore(client, rule, prefix)
	return store, client
}

// redisTestAddr returns the Redis address to use in tests.
// Returns empty string (causes skip) unless a real Redis is available.
func redisTestAddr(t *testing.T) string {
	t.Helper()
	// We use a fixed address for unit test skipping logic.
	// Integration tests bring up a real Redis via testcontainers.
	addr := "localhost:6379"

	client := redis.NewClient(&redis.Options{Addr: addr})
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		t.Skipf("no Redis available at %s (%v) — skipping unit Redis test", addr, err)
	}
	return addr
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestRedisStore_Close_IsNoop(t *testing.T) {
	store := NewRedisStore(nil, ports.RateLimitRule{}, "pfx")
	if err := store.Close(); err != nil {
		t.Errorf("Close() error = %v, want nil", err)
	}
}

func TestRedisStore_Allow_WithinBurst(t *testing.T) {
	store, _ := newTestRedisStore(t, 10, 5, fmt.Sprintf("vw:rl:test:%d", time.Now().UnixNano()))
	ctx := context.Background()

	result := store.Allow(ctx, "ip1")
	if !result.Allowed {
		t.Fatalf("expected first request to be allowed")
	}
	if result.Limit != 10 {
		t.Errorf("Limit = %v, want 10", result.Limit)
	}
	if result.Burst != 5 {
		t.Errorf("Burst = %v, want 5", result.Burst)
	}
}

func TestRedisStore_Allow_ExceedsBurst(t *testing.T) {
	prefix := fmt.Sprintf("vw:rl:test:%d", time.Now().UnixNano())
	// Burst of 1, near-zero rate so no refill during test.
	store, _ := newTestRedisStore(t, 0.001, 1, prefix)
	ctx := context.Background()
	key := "ip2"

	first := store.Allow(ctx, key)
	if !first.Allowed {
		t.Fatalf("expected first request to be allowed")
	}

	second := store.Allow(ctx, key)
	if second.Allowed {
		t.Fatal("expected second request to be denied after burst exhausted")
	}
	if second.Remaining != 0 {
		t.Errorf("Remaining = %v, want 0 when denied", second.Remaining)
	}
	if second.RetryAfter <= 0 {
		t.Errorf("RetryAfter = %v, want > 0 when denied", second.RetryAfter)
	}
}

func TestRedisStore_Allow_BurstAllowed(t *testing.T) {
	burst := 3
	prefix := fmt.Sprintf("vw:rl:test:%d", time.Now().UnixNano())
	store, _ := newTestRedisStore(t, 0.001, burst, prefix)
	ctx := context.Background()
	key := "ip3"

	for i := range burst {
		r := store.Allow(ctx, key)
		if !r.Allowed {
			t.Fatalf("request %d/%d expected allowed, got denied", i+1, burst)
		}
	}
	r := store.Allow(ctx, key)
	if r.Allowed {
		t.Fatal("expected request beyond burst to be denied")
	}
}

func TestRedisStore_Allow_DifferentKeysAreIndependent(t *testing.T) {
	prefix := fmt.Sprintf("vw:rl:test:%d", time.Now().UnixNano())
	store, _ := newTestRedisStore(t, 0.001, 1, prefix)
	ctx := context.Background()

	store.Allow(ctx, "keyA")
	deniedA := store.Allow(ctx, "keyA")
	if deniedA.Allowed {
		t.Fatal("expected keyA to be denied after burst exhausted")
	}

	allowedB := store.Allow(ctx, "keyB")
	if !allowedB.Allowed {
		t.Fatal("expected keyB to be allowed independently of keyA")
	}
}

func TestRedisStore_Allow_ResultFields(t *testing.T) {
	tests := []struct {
		name            string
		rps             float64
		burst           int
		requests        int
		wantLastAllowed bool
		wantLimit       float64
		wantBurst       int
	}{
		{
			name:            "within burst",
			rps:             10,
			burst:           3,
			requests:        1,
			wantLastAllowed: true,
			wantLimit:       10,
			wantBurst:       3,
		},
		{
			name:            "burst exhausted",
			rps:             0.001,
			burst:           2,
			requests:        3,
			wantLastAllowed: false,
			wantLimit:       0.001,
			wantBurst:       2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prefix := fmt.Sprintf("vw:rl:test:%d", time.Now().UnixNano())
			store, _ := newTestRedisStore(t, tt.rps, tt.burst, prefix)
			ctx := context.Background()

			var result ports.RateLimitResult
			for range tt.requests {
				result = store.Allow(ctx, "key")
			}

			if result.Allowed != tt.wantLastAllowed {
				t.Errorf("Allowed = %v, want %v", result.Allowed, tt.wantLastAllowed)
			}
			if result.Limit != tt.wantLimit {
				t.Errorf("Limit = %v, want %v", result.Limit, tt.wantLimit)
			}
			if result.Burst != tt.wantBurst {
				t.Errorf("Burst = %v, want %v", result.Burst, tt.wantBurst)
			}
		})
	}
}

func TestRedisFactory_NewLimiter(t *testing.T) {
	addr := redisTestAddr(t)
	client := redis.NewClient(&redis.Options{Addr: addr})
	t.Cleanup(func() { client.Close() })

	factory := NewRedisFactory(client, "vw:rl")
	rule := ports.RateLimitRule{RequestsPerSecond: 5, Burst: 10}
	limiter := factory.NewLimiter(rule)
	if limiter == nil {
		t.Fatal("NewLimiter returned nil")
	}
	_ = limiter.Close()
}

func TestRedisFactory_NewLimiter_UniqueKeyPrefixes(t *testing.T) {
	addr := redisTestAddr(t)
	client := redis.NewClient(&redis.Options{Addr: addr})
	t.Cleanup(func() { client.Close() })

	factory := NewRedisFactory(client, "vw:rl")
	rule := ports.RateLimitRule{RequestsPerSecond: 100, Burst: 100}

	limiterA := factory.NewLimiter(rule).(*RedisStore)
	limiterB := factory.NewLimiter(rule).(*RedisStore)

	if limiterA.keyPrefix == limiterB.keyPrefix {
		t.Errorf("expected unique key prefixes, both got %q", limiterA.keyPrefix)
	}
}

// Compile-time interface satisfaction checks.
var (
	_ ports.RateLimiter        = (*RedisStore)(nil)
	_ ports.RateLimiterFactory = (*RedisFactory)(nil)
)
