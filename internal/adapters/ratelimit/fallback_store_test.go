package ratelimit

import (
	"context"
	"errors"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/vibewarden/vibewarden/internal/ports"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// fakeStore is a minimal ports.RateLimiter implementation for tests.
type fakeStore struct {
	result ports.RateLimitResult
	closed bool
	callN  atomic.Int64
}

func (f *fakeStore) Allow(_ context.Context, _ string) ports.RateLimitResult {
	f.callN.Add(1)
	return f.result
}

func (f *fakeStore) Close() error {
	f.closed = true
	return nil
}

// fakeErrorStore.Close returns an error for error-path tests.
type fakeErrorStore struct{ fakeStore }

func (f *fakeErrorStore) Close() error { return errors.New("close error") }

// discardSlogLogger returns an slog.Logger that discards all output.
func discardSlogLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(noopSlogWriter{}, nil))
}

type noopSlogWriter struct{}

func (noopSlogWriter) Write(p []byte) (int, error) { return len(p), nil }

// ---------------------------------------------------------------------------
// FallbackStore unit tests
// ---------------------------------------------------------------------------

func TestFallbackStore_Allow_DelegatesToPrimaryWhenHealthy(t *testing.T) {
	primary := &fakeStore{result: ports.RateLimitResult{Allowed: true, Remaining: 9}}
	secondary := &fakeStore{result: ports.RateLimitResult{Allowed: false}}
	probe := func(_ context.Context) error { return nil }

	fs := NewFallbackStore(primary, secondary, probe,
		FallbackStoreConfig{HealthCheckInterval: time.Hour},
		discardSlogLogger(), nil,
	)
	t.Cleanup(func() { _ = fs.Close() })

	result := fs.Allow(context.Background(), "key")
	if !result.Allowed {
		t.Fatal("expected primary result (Allowed=true)")
	}
	if primary.callN.Load() != 1 {
		t.Errorf("primary called %d times, want 1", primary.callN.Load())
	}
	if secondary.callN.Load() != 0 {
		t.Errorf("secondary called %d times, want 0", secondary.callN.Load())
	}
}

func TestFallbackStore_Allow_DelegatesToSecondaryWhenUnhealthy(t *testing.T) {
	primary := &fakeStore{result: ports.RateLimitResult{Allowed: true}}
	secondary := &fakeStore{result: ports.RateLimitResult{Allowed: true, Remaining: 7}}
	probe := func(_ context.Context) error { return errors.New("redis down") }

	fs := NewFallbackStore(primary, secondary, probe,
		FallbackStoreConfig{HealthCheckInterval: time.Hour},
		discardSlogLogger(), nil,
	)
	t.Cleanup(func() { _ = fs.Close() })

	// Manually mark as unhealthy.
	fs.healthy.Store(false)

	result := fs.Allow(context.Background(), "key")
	if !result.Allowed {
		t.Fatal("expected secondary result (Allowed=true)")
	}
	if secondary.callN.Load() != 1 {
		t.Errorf("secondary called %d times, want 1", secondary.callN.Load())
	}
	if primary.callN.Load() != 0 {
		t.Errorf("primary called %d times, want 0", primary.callN.Load())
	}
}

func TestFallbackStore_Allow_FailClosed_DeniesWhenUnhealthy(t *testing.T) {
	primary := &fakeStore{result: ports.RateLimitResult{Allowed: true}}
	secondary := &fakeStore{result: ports.RateLimitResult{Allowed: true}}

	fs := NewFallbackStore(primary, secondary, func(_ context.Context) error { return nil },
		FallbackStoreConfig{
			HealthCheckInterval: time.Hour,
			FailClosed:          true,
		},
		discardSlogLogger(), nil,
	)
	t.Cleanup(func() { _ = fs.Close() })

	// Mark unhealthy.
	fs.healthy.Store(false)

	result := fs.Allow(context.Background(), "key")
	if result.Allowed {
		t.Fatal("fail-closed mode: expected deny when unhealthy")
	}
	if primary.callN.Load() != 0 {
		t.Errorf("primary called when unhealthy")
	}
	if secondary.callN.Load() != 0 {
		t.Errorf("secondary called in fail-closed mode")
	}
}

func TestFallbackStore_IsHealthy_ReflectsState(t *testing.T) {
	primary := &fakeStore{}
	secondary := &fakeStore{}
	fs := NewFallbackStore(primary, secondary, func(_ context.Context) error { return nil },
		FallbackStoreConfig{HealthCheckInterval: time.Hour},
		discardSlogLogger(), nil,
	)
	t.Cleanup(func() { _ = fs.Close() })

	if !fs.IsHealthy() {
		t.Fatal("expected IsHealthy=true after construction")
	}

	fs.healthy.Store(false)
	if fs.IsHealthy() {
		t.Fatal("expected IsHealthy=false after marking unhealthy")
	}
}

func TestFallbackStore_Close_ClosesBothStores(t *testing.T) {
	primary := &fakeStore{}
	secondary := &fakeStore{}
	fs := NewFallbackStore(primary, secondary, func(_ context.Context) error { return nil },
		FallbackStoreConfig{HealthCheckInterval: time.Hour},
		discardSlogLogger(), nil,
	)

	if err := fs.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if !primary.closed {
		t.Error("expected primary to be closed")
	}
	if !secondary.closed {
		t.Error("expected secondary to be closed")
	}
}

func TestFallbackStore_HealthCheck_TransitionToUnhealthy(t *testing.T) {
	primary := &fakeStore{result: ports.RateLimitResult{Allowed: true}}
	secondary := &fakeStore{result: ports.RateLimitResult{Allowed: true}}

	probeErr := errors.New("redis unavailable")
	probe := func(_ context.Context) error { return probeErr }

	interval := 20 * time.Millisecond
	fs := NewFallbackStore(primary, secondary, probe,
		FallbackStoreConfig{HealthCheckInterval: interval},
		discardSlogLogger(), nil,
	)
	t.Cleanup(func() { _ = fs.Close() })

	// Wait for at least one health check cycle.
	time.Sleep(3 * interval)

	if fs.IsHealthy() {
		t.Error("expected IsHealthy=false after failed probe")
	}
}

func TestFallbackStore_HealthCheck_RecoveryFromUnhealthy(t *testing.T) {
	primary := &fakeStore{result: ports.RateLimitResult{Allowed: true}}
	secondary := &fakeStore{result: ports.RateLimitResult{Allowed: true}}

	// failing is true while the probe should return an error.
	var failing atomic.Bool
	failing.Store(true)

	probe := func(_ context.Context) error {
		if failing.Load() {
			return errors.New("redis unavailable")
		}
		return nil
	}

	interval := 20 * time.Millisecond
	fs := NewFallbackStore(primary, secondary, probe,
		FallbackStoreConfig{HealthCheckInterval: interval},
		discardSlogLogger(), nil,
	)
	t.Cleanup(func() { _ = fs.Close() })

	// Wait for at least one failed health check.
	time.Sleep(3 * interval)
	if fs.IsHealthy() {
		t.Fatal("expected IsHealthy=false after failed probe")
	}

	// Signal recovery and wait for the health check goroutine to detect it.
	failing.Store(false)
	time.Sleep(3 * interval)

	if !fs.IsHealthy() {
		t.Error("expected IsHealthy=true after probe succeeds")
	}
}

func TestFallbackFactory_NewLimiter(t *testing.T) {
	primary := &fakeFactory{result: ports.RateLimitResult{Allowed: true}}
	secondary := &fakeFactory{result: ports.RateLimitResult{Allowed: true}}
	probe := func(_ context.Context) error { return nil }

	factory := NewFallbackFactory(
		primary, secondary, probe,
		FallbackStoreConfig{HealthCheckInterval: time.Hour},
		discardSlogLogger(), nil,
	)

	rule := ports.RateLimitRule{RequestsPerSecond: 10, Burst: 20}
	limiter := factory.NewLimiter(rule)
	if limiter == nil {
		t.Fatal("NewLimiter returned nil")
	}
	_ = limiter.Close()
}

// fakeFactory implements ports.RateLimiterFactory for testing FallbackFactory.
type fakeFactory struct {
	result ports.RateLimitResult
}

func (f *fakeFactory) NewLimiter(_ ports.RateLimitRule) ports.RateLimiter {
	return &fakeStore{result: f.result}
}

// Compile-time interface satisfaction checks.
var (
	_ ports.RateLimiter        = (*FallbackStore)(nil)
	_ ports.RateLimiterFactory = (*FallbackFactory)(nil)
)
