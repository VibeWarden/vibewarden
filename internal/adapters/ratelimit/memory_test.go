package ratelimit

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/vibewarden/vibewarden/internal/ports"
)

// newTestStore is a helper that creates a MemoryStore with a very long cleanup interval
// so the background goroutine does not interfere with tests. Call Close when done.
func newTestStore(rps float64, burst int) *MemoryStore {
	rule := ports.RateLimitRule{RequestsPerSecond: rps, Burst: burst}
	return NewMemoryStore(rule, time.Hour, time.Hour)
}

// newFastGCStore creates a MemoryStore with a short cleanup interval for GC tests.
func newFastGCStore(rps float64, burst int, cleanupInterval, entryTTL time.Duration) *MemoryStore {
	rule := ports.RateLimitRule{RequestsPerSecond: rps, Burst: burst}
	return NewMemoryStore(rule, cleanupInterval, entryTTL)
}

func TestMemoryStore_Allow_WithinLimit(t *testing.T) {
	store := newTestStore(10, 5)
	t.Cleanup(func() { _ = store.Close() })

	ctx := context.Background()

	// First request must be allowed when tokens are available.
	result := store.Allow(ctx, "192.168.1.1")

	if !result.Allowed {
		t.Fatalf("expected first request to be allowed, got denied (RetryAfter=%v)", result.RetryAfter)
	}
	if result.Limit != 10 {
		t.Errorf("expected Limit=10, got %v", result.Limit)
	}
	if result.Burst != 5 {
		t.Errorf("expected Burst=5, got %v", result.Burst)
	}
}

func TestMemoryStore_Allow_ExceedsLimit(t *testing.T) {
	// Burst of 1 means only 1 token in the bucket initially.
	// A very low rate ensures no refill during the test.
	store := newTestStore(0.001, 1)
	t.Cleanup(func() { _ = store.Close() })

	ctx := context.Background()
	key := "192.168.1.2"

	// First request: consumes the single token.
	first := store.Allow(ctx, key)
	if !first.Allowed {
		t.Fatalf("expected first request to be allowed")
	}

	// Second request: bucket empty, must be denied.
	second := store.Allow(ctx, key)
	if second.Allowed {
		t.Fatal("expected second request to be denied after burst exhausted")
	}
	if second.RetryAfter <= 0 {
		t.Errorf("expected RetryAfter > 0, got %v", second.RetryAfter)
	}
	if second.Remaining != 0 {
		t.Errorf("expected Remaining=0 when denied, got %v", second.Remaining)
	}
}

func TestMemoryStore_Allow_BurstHandling(t *testing.T) {
	burst := 5
	store := newTestStore(0.001, burst)
	t.Cleanup(func() { _ = store.Close() })

	ctx := context.Background()
	key := "10.0.0.1"

	// All burst requests should be allowed.
	for i := range burst {
		result := store.Allow(ctx, key)
		if !result.Allowed {
			t.Fatalf("request %d/%d expected allowed, got denied", i+1, burst)
		}
	}

	// Next request must be denied.
	result := store.Allow(ctx, key)
	if result.Allowed {
		t.Fatal("expected request after burst to be denied")
	}
}

func TestMemoryStore_Allow_DifferentKeysAreIndependent(t *testing.T) {
	// Burst of 1: exhausted after the first request per key.
	store := newTestStore(0.001, 1)
	t.Cleanup(func() { _ = store.Close() })

	ctx := context.Background()
	keyA := "10.0.0.1"
	keyB := "10.0.0.2"

	// Exhaust key A.
	store.Allow(ctx, keyA)
	deniedA := store.Allow(ctx, keyA)
	if deniedA.Allowed {
		t.Fatal("expected key A to be denied after burst")
	}

	// Key B should still have tokens.
	allowedB := store.Allow(ctx, keyB)
	if !allowedB.Allowed {
		t.Fatal("expected key B to be allowed independently of key A")
	}
}

func TestMemoryStore_Allow_ResultFields(t *testing.T) {
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
			name:            "single request with capacity",
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
			requests:        3, // one beyond burst
			wantLastAllowed: false,
			wantLimit:       0.001,
			wantBurst:       2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := newTestStore(tt.rps, tt.burst)
			t.Cleanup(func() { _ = store.Close() })

			ctx := context.Background()
			var result ports.RateLimitResult
			for range tt.requests {
				result = store.Allow(ctx, "key")
			}

			if result.Allowed != tt.wantLastAllowed {
				t.Errorf("Allowed: got %v, want %v", result.Allowed, tt.wantLastAllowed)
			}
			if result.Limit != tt.wantLimit {
				t.Errorf("Limit: got %v, want %v", result.Limit, tt.wantLimit)
			}
			if result.Burst != tt.wantBurst {
				t.Errorf("Burst: got %v, want %v", result.Burst, tt.wantBurst)
			}
		})
	}
}

func TestMemoryStore_GC_EvictsExpiredEntries(t *testing.T) {
	// Short TTL so the entry expires quickly.
	entryTTL := 50 * time.Millisecond
	cleanupInterval := 20 * time.Millisecond

	store := newFastGCStore(10, 5, cleanupInterval, entryTTL)
	t.Cleanup(func() { _ = store.Close() })

	ctx := context.Background()
	key := "evict-me"

	store.Allow(ctx, key)

	// Verify entry exists.
	if _, ok := store.limiters.Load(key); !ok {
		t.Fatal("expected entry to exist after Allow")
	}

	// Wait for entry to expire and GC to run.
	time.Sleep(entryTTL + 3*cleanupInterval)

	if _, ok := store.limiters.Load(key); ok {
		t.Error("expected entry to be evicted after TTL")
	}
}

func TestMemoryStore_GC_DoesNotEvictActiveEntries(t *testing.T) {
	entryTTL := 100 * time.Millisecond
	cleanupInterval := 20 * time.Millisecond

	store := newFastGCStore(100, 200, cleanupInterval, entryTTL)
	t.Cleanup(func() { _ = store.Close() })

	ctx := context.Background()
	key := "keep-me"

	// Keep touching the entry so it stays fresh.
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case <-time.After(10 * time.Millisecond):
				store.Allow(ctx, key)
			case <-store.done:
				return
			}
		}
	}()

	time.Sleep(entryTTL + 3*cleanupInterval)

	if _, ok := store.limiters.Load(key); !ok {
		t.Error("expected active entry to survive GC")
	}

	_ = store.Close()
	<-done
}

func TestMemoryStore_Close_StopsGC(t *testing.T) {
	// A very short cleanup interval to ensure GC would have run many times.
	store := newFastGCStore(10, 5, 5*time.Millisecond, time.Hour)

	if err := store.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}

	// Calling Close a second time must be a no-op (no panic, no error).
	if err := store.Close(); err != nil {
		t.Fatalf("second Close returned error: %v", err)
	}
}

func TestMemoryStore_Close_WaitsForGCToFinish(t *testing.T) {
	store := newFastGCStore(10, 5, 5*time.Millisecond, time.Hour)

	closeDone := make(chan struct{})
	go func() {
		_ = store.Close()
		close(closeDone)
	}()

	select {
	case <-closeDone:
		// Good: Close returned promptly.
	case <-time.After(2 * time.Second):
		t.Fatal("Close did not return within 2 seconds — possible goroutine leak")
	}
}

func TestMemoryStore_ConcurrentAccess(t *testing.T) {
	// Verified by the race detector when run with -race.
	store := newTestStore(1000, 10000)
	t.Cleanup(func() { _ = store.Close() })

	ctx := context.Background()
	keys := []string{"a", "b", "c", "d", "e"}
	const goroutines = 50
	const requestsPerGoroutine = 100

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := range goroutines {
		go func(i int) {
			defer wg.Done()
			key := keys[i%len(keys)]
			for range requestsPerGoroutine {
				store.Allow(ctx, key)
			}
		}(i)
	}
	wg.Wait()
}

func TestMemoryFactory_NewLimiter_ImplementsInterface(t *testing.T) {
	factory := NewDefaultMemoryFactory()

	rule := ports.RateLimitRule{RequestsPerSecond: 5, Burst: 10}
	limiter := factory.NewLimiter(rule)

	if limiter == nil {
		t.Fatal("NewLimiter returned nil")
	}

	// Verify it satisfies the interface at compile time — the cast below panics if not.
	_ = limiter.(ports.RateLimiter)

	_ = limiter.Close()
}

func TestMemoryFactory_NewLimiter_RespectsRule(t *testing.T) {
	factory := NewMemoryFactory(time.Hour, time.Hour)

	rps := 2.0
	burst := 3
	rule := ports.RateLimitRule{RequestsPerSecond: rps, Burst: burst}

	limiter := factory.NewLimiter(rule)
	t.Cleanup(func() { _ = limiter.Close() })

	ctx := context.Background()

	// Should allow exactly burst requests before denying.
	for i := range burst {
		result := limiter.Allow(ctx, "key")
		if !result.Allowed {
			t.Fatalf("request %d/%d expected allowed", i+1, burst)
		}
		if result.Limit != rps {
			t.Errorf("request %d: Limit got %v want %v", i+1, result.Limit, rps)
		}
		if result.Burst != burst {
			t.Errorf("request %d: Burst got %v want %v", i+1, result.Burst, burst)
		}
	}
}

// Compile-time interface satisfaction checks.
var (
	_ ports.RateLimiter        = (*MemoryStore)(nil)
	_ ports.RateLimiterFactory = (*MemoryFactory)(nil)
)
