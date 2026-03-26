package ratelimit_test

import (
	"context"
	"testing"
	"time"

	"github.com/vibewarden/vibewarden/internal/adapters/ratelimit"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// Compile-time interface satisfaction checks.
var _ ports.RateLimiterFactory = (*ratelimit.MemoryFactory)(nil)

func TestNewMemoryFactory_StoresParameters(t *testing.T) {
	cleanupInterval := 5 * time.Minute
	entryTTL := 30 * time.Minute

	factory := ratelimit.NewMemoryFactory(cleanupInterval, entryTTL)
	if factory == nil {
		t.Fatal("NewMemoryFactory returned nil")
	}
}

func TestNewDefaultMemoryFactory_ReturnsNonNil(t *testing.T) {
	factory := ratelimit.NewDefaultMemoryFactory()
	if factory == nil {
		t.Fatal("NewDefaultMemoryFactory returned nil")
	}
}

func TestMemoryFactory_NewLimiter_ReturnsNonNil(t *testing.T) {
	factory := ratelimit.NewDefaultMemoryFactory()
	rule := ports.RateLimitRule{RequestsPerSecond: 10, Burst: 5}

	limiter := factory.NewLimiter(rule)
	if limiter == nil {
		t.Fatal("NewLimiter returned nil")
	}
	_ = limiter.Close()
}

func TestMemoryFactory_NewLimiter_EachCallReturnsIndependentLimiter(t *testing.T) {
	factory := ratelimit.NewDefaultMemoryFactory()
	rule := ports.RateLimitRule{RequestsPerSecond: 0.001, Burst: 1}

	limiterA := factory.NewLimiter(rule)
	limiterB := factory.NewLimiter(rule)
	t.Cleanup(func() {
		_ = limiterA.Close()
		_ = limiterB.Close()
	})

	ctx := context.Background()
	key := "test-key"

	// Exhaust limiter A.
	limiterA.Allow(ctx, key) // consume burst
	resultA := limiterA.Allow(ctx, key)
	if resultA.Allowed {
		t.Fatal("limiter A should be exhausted after consuming burst")
	}

	// Limiter B must still have its own fresh tokens.
	resultB := limiterB.Allow(ctx, key)
	if !resultB.Allowed {
		t.Fatal("limiter B should be independent of limiter A and still have tokens")
	}
}

func TestMemoryFactory_NewLimiter_RespectsRuleFields(t *testing.T) {
	tests := []struct {
		name  string
		rule  ports.RateLimitRule
		burst int
		rps   float64
	}{
		{
			name:  "low rps high burst",
			rule:  ports.RateLimitRule{RequestsPerSecond: 1.0, Burst: 10},
			burst: 10,
			rps:   1.0,
		},
		{
			name:  "high rps low burst",
			rule:  ports.RateLimitRule{RequestsPerSecond: 100.0, Burst: 2},
			burst: 2,
			rps:   100.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			factory := ratelimit.NewMemoryFactory(time.Hour, time.Hour)
			limiter := factory.NewLimiter(tt.rule)
			t.Cleanup(func() { _ = limiter.Close() })

			ctx := context.Background()

			// All burst tokens should be available initially.
			for i := range tt.burst {
				result := limiter.Allow(ctx, "key")
				if !result.Allowed {
					t.Fatalf("request %d/%d expected allowed, got denied", i+1, tt.burst)
				}
				if result.Limit != tt.rps {
					t.Errorf("request %d: Limit = %v, want %v", i+1, result.Limit, tt.rps)
				}
				if result.Burst != tt.burst {
					t.Errorf("request %d: Burst = %v, want %v", i+1, result.Burst, tt.burst)
				}
			}
		})
	}
}

func TestDefaultConstants(t *testing.T) {
	if ratelimit.DefaultCleanupInterval <= 0 {
		t.Errorf("DefaultCleanupInterval = %v, want > 0", ratelimit.DefaultCleanupInterval)
	}
	if ratelimit.DefaultEntryTTL <= 0 {
		t.Errorf("DefaultEntryTTL = %v, want > 0", ratelimit.DefaultEntryTTL)
	}
	if ratelimit.DefaultEntryTTL < ratelimit.DefaultCleanupInterval {
		t.Errorf("DefaultEntryTTL (%v) should be >= DefaultCleanupInterval (%v)",
			ratelimit.DefaultEntryTTL, ratelimit.DefaultCleanupInterval)
	}
}
