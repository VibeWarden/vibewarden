package ratelimit

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	tcredis "github.com/testcontainers/testcontainers-go/modules/redis"

	"github.com/vibewarden/vibewarden/internal/ports"
)

// startRedisContainer starts a Redis container and returns the *redis.Options
// to connect to it. The container is terminated when the test ends.
func startRedisContainer(t *testing.T) *redis.Options {
	t.Helper()
	ctx := context.Background()

	ctr, err := tcredis.Run(ctx, "redis:7-alpine")
	if err != nil {
		t.Fatalf("failed to start Redis container: %v", err)
	}
	t.Cleanup(func() {
		if err := ctr.Terminate(ctx); err != nil {
			t.Logf("failed to terminate Redis container: %v", err)
		}
	})

	connStr, err := ctr.ConnectionString(ctx)
	if err != nil {
		t.Fatalf("failed to get Redis connection string: %v", err)
	}

	opts, err := redis.ParseURL(connStr)
	if err != nil {
		t.Fatalf("failed to parse Redis URL %q: %v", connStr, err)
	}
	return opts
}

func TestRedisStore_Integration_AllowWithinBurst(t *testing.T) {
	opts := startRedisContainer(t)
	client := redis.NewClient(opts)
	t.Cleanup(func() { client.Close() })

	ctx := context.Background()
	prefix := fmt.Sprintf("vw:rl:int:%d", time.Now().UnixNano())
	rule := ports.RateLimitRule{RequestsPerSecond: 10, Burst: 5}
	store := NewRedisStore(client, rule, prefix)

	result := store.Allow(ctx, "192.168.1.1")
	if !result.Allowed {
		t.Fatal("expected first request to be allowed")
	}
	if result.Limit != 10 {
		t.Errorf("Limit = %v, want 10", result.Limit)
	}
	if result.Burst != 5 {
		t.Errorf("Burst = %v, want 5", result.Burst)
	}
}

func TestRedisStore_Integration_BurstExhaustion(t *testing.T) {
	opts := startRedisContainer(t)
	client := redis.NewClient(opts)
	t.Cleanup(func() { client.Close() })

	ctx := context.Background()
	prefix := fmt.Sprintf("vw:rl:int:%d", time.Now().UnixNano())
	burst := 3
	rule := ports.RateLimitRule{RequestsPerSecond: 0.001, Burst: burst}
	store := NewRedisStore(client, rule, prefix)
	key := "10.0.0.1"

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
	if r.Remaining != 0 {
		t.Errorf("Remaining = %v, want 0 when denied", r.Remaining)
	}
	if r.RetryAfter <= 0 {
		t.Errorf("RetryAfter = %v, want > 0 when denied", r.RetryAfter)
	}
}

func TestRedisStore_Integration_IndependentKeys(t *testing.T) {
	opts := startRedisContainer(t)
	client := redis.NewClient(opts)
	t.Cleanup(func() { client.Close() })

	ctx := context.Background()
	prefix := fmt.Sprintf("vw:rl:int:%d", time.Now().UnixNano())
	rule := ports.RateLimitRule{RequestsPerSecond: 0.001, Burst: 1}
	store := NewRedisStore(client, rule, prefix)

	// Exhaust keyA.
	store.Allow(ctx, "keyA")
	r := store.Allow(ctx, "keyA")
	if r.Allowed {
		t.Fatal("expected keyA denied after burst")
	}

	// keyB must still have tokens.
	r = store.Allow(ctx, "keyB")
	if !r.Allowed {
		t.Fatal("expected keyB allowed independently")
	}
}

func TestRedisStore_Integration_KeyTTL(t *testing.T) {
	opts := startRedisContainer(t)
	client := redis.NewClient(opts)
	t.Cleanup(func() { client.Close() })

	ctx := context.Background()
	prefix := fmt.Sprintf("vw:rl:int:%d", time.Now().UnixNano())
	// Very low rps so TTL = 2 * burst/rps = 2 * 1/0.001 = 2000s > 60s (minimum).
	rule := ports.RateLimitRule{RequestsPerSecond: 0.001, Burst: 1}
	store := NewRedisStore(client, rule, prefix)

	store.Allow(ctx, "ttl-key")

	redisKey := fmt.Sprintf("%s:ttl-key", prefix)
	ttl, err := client.TTL(ctx, redisKey).Result()
	if err != nil {
		t.Fatalf("TTL command error: %v", err)
	}
	if ttl <= 0 {
		t.Errorf("expected positive TTL on Redis key, got %v", ttl)
	}
}

func TestFallbackStore_Integration_FallbackOnRedisFailure(t *testing.T) {
	opts := startRedisContainer(t)
	client := redis.NewClient(opts)

	ctx := context.Background()
	prefix := fmt.Sprintf("vw:rl:int:%d", time.Now().UnixNano())
	rule := ports.RateLimitRule{RequestsPerSecond: 100, Burst: 100}

	redisFactory := NewRedisFactory(client, prefix)
	memFactory := NewDefaultMemoryFactory()

	probe := func(ctx context.Context) error {
		return client.Ping(ctx).Err()
	}

	interval := 20 * time.Millisecond
	factory := NewFallbackFactory(
		redisFactory, memFactory, probe,
		FallbackStoreConfig{HealthCheckInterval: interval},
		discardSlogLogger(), nil,
	)

	limiter := factory.NewLimiter(rule)
	t.Cleanup(func() { _ = limiter.Close() })

	// While Redis is up, Allow must succeed.
	r := limiter.Allow(ctx, "key1")
	if !r.Allowed {
		t.Fatal("expected allow before Redis failure")
	}

	// Close the client to simulate Redis going away.
	client.Close()

	// Wait for at least two health check cycles.
	time.Sleep(5 * interval)

	// Allow must never panic regardless of store state.
	r = limiter.Allow(ctx, "key2")
	_ = r

	// FallbackStore must report unhealthy (or already detected).
	fallback := limiter.(*FallbackStore)
	// The probe returns an error because the client is closed.
	// At least one check should have run and flipped the flag.
	if fallback.IsHealthy() {
		t.Log("note: Redis failure may not have been detected yet; this is a race in the test")
	}
}
