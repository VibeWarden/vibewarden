package statesync

import (
	"context"
	"fmt"
	"testing"
	"time"

	tcredis "github.com/testcontainers/testcontainers-go/modules/redis"
)

// startRedisContainerForStateSync starts a Redis container and returns a
// connection URL. The container is terminated when the test ends.
func startRedisContainerForStateSync(t *testing.T) string {
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
	return connStr
}

func TestRedisStateSync_Integration_IncrementCounter(t *testing.T) {
	url := startRedisContainerForStateSync(t)
	ctx := context.Background()

	r, err := NewRedisStateSync(ctx, RedisConfig{URL: url})
	if err != nil {
		t.Fatalf("NewRedisStateSync: %v", err)
	}
	t.Cleanup(func() { _ = r.Close() })

	key := fmt.Sprintf("vw:ss:test:counter:%d", time.Now().UnixNano())

	tests := []struct {
		name      string
		delta     int64
		wantTotal int64
		wantErr   bool
	}{
		{"first increment", 1, 1, false},
		{"second increment", 4, 5, false},
		{"zero delta", 0, 0, true},
		{"negative delta", -1, 0, true},
	}

	total := int64(0)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := r.IncrementCounter(ctx, key, tt.delta, 0)
			if (err != nil) != tt.wantErr {
				t.Errorf("IncrementCounter(%d) error = %v, wantErr %v", tt.delta, err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				total += tt.delta
				if got != total {
					t.Errorf("IncrementCounter(%d) = %d, want %d", tt.delta, got, total)
				}
			}
		})
	}
}

func TestRedisStateSync_Integration_GetCounter_Missing(t *testing.T) {
	url := startRedisContainerForStateSync(t)
	ctx := context.Background()

	r, err := NewRedisStateSync(ctx, RedisConfig{URL: url})
	if err != nil {
		t.Fatalf("NewRedisStateSync: %v", err)
	}
	t.Cleanup(func() { _ = r.Close() })

	got, err := r.GetCounter(ctx, "does-not-exist")
	if err != nil {
		t.Fatalf("GetCounter on missing key: %v", err)
	}
	if got != 0 {
		t.Errorf("GetCounter(missing) = %d, want 0", got)
	}
}

func TestRedisStateSync_Integration_IncrementCounter_TTL(t *testing.T) {
	url := startRedisContainerForStateSync(t)
	ctx := context.Background()

	r, err := NewRedisStateSync(ctx, RedisConfig{URL: url})
	if err != nil {
		t.Fatalf("NewRedisStateSync: %v", err)
	}
	t.Cleanup(func() { _ = r.Close() })

	key := fmt.Sprintf("vw:ss:test:ttl-counter:%d", time.Now().UnixNano())
	// Redis EXPIRE minimum granularity is 1 second; use 2s so the test is
	// robust regardless of sub-second scheduling jitter.
	ttl := 2 * time.Second

	if _, err := r.IncrementCounter(ctx, key, 7, ttl); err != nil {
		t.Fatalf("IncrementCounter: %v", err)
	}

	// Value must be present before TTL expires.
	got, err := r.GetCounter(ctx, key)
	if err != nil {
		t.Fatalf("GetCounter before TTL: %v", err)
	}
	if got != 7 {
		t.Errorf("before TTL: got %d, want 7", got)
	}

	time.Sleep(ttl + 500*time.Millisecond)

	// After TTL the counter must look absent (0).
	got, err = r.GetCounter(ctx, key)
	if err != nil {
		t.Fatalf("GetCounter after TTL: %v", err)
	}
	if got != 0 {
		t.Errorf("after TTL: got %d, want 0", got)
	}
}

func TestRedisStateSync_Integration_Set_AddContainsRemove(t *testing.T) {
	url := startRedisContainerForStateSync(t)
	ctx := context.Background()

	r, err := NewRedisStateSync(ctx, RedisConfig{URL: url})
	if err != nil {
		t.Fatalf("NewRedisStateSync: %v", err)
	}
	t.Cleanup(func() { _ = r.Close() })

	key := fmt.Sprintf("vw:ss:test:set:%d", time.Now().UnixNano())
	member := "192.168.1.1"

	// Initially absent.
	ok, err := r.SetContains(ctx, key, member)
	if err != nil || ok {
		t.Fatalf("expected false/nil before Add, got %v/%v", ok, err)
	}

	// Add.
	if err := r.AddToSet(ctx, key, member, 0); err != nil {
		t.Fatalf("AddToSet: %v", err)
	}
	ok, err = r.SetContains(ctx, key, member)
	if err != nil {
		t.Fatalf("SetContains after Add: %v", err)
	}
	if !ok {
		t.Error("expected Contains=true after Add")
	}

	// Idempotent re-add.
	if err := r.AddToSet(ctx, key, member, 0); err != nil {
		t.Fatalf("second AddToSet: %v", err)
	}

	// Remove.
	if err := r.RemoveFromSet(ctx, key, member); err != nil {
		t.Fatalf("RemoveFromSet: %v", err)
	}
	ok, err = r.SetContains(ctx, key, member)
	if err != nil {
		t.Fatalf("SetContains after Remove: %v", err)
	}
	if ok {
		t.Error("expected Contains=false after Remove")
	}
}

func TestRedisStateSync_Integration_Set_TTL(t *testing.T) {
	url := startRedisContainerForStateSync(t)
	ctx := context.Background()

	r, err := NewRedisStateSync(ctx, RedisConfig{URL: url})
	if err != nil {
		t.Fatalf("NewRedisStateSync: %v", err)
	}
	t.Cleanup(func() { _ = r.Close() })

	key := fmt.Sprintf("vw:ss:test:set-ttl:%d", time.Now().UnixNano())
	// Redis EXPIRE minimum granularity is 1 second; use 2s so the test is
	// robust regardless of sub-second scheduling jitter.
	ttl := 2 * time.Second

	if err := r.AddToSet(ctx, key, "member", ttl); err != nil {
		t.Fatalf("AddToSet: %v", err)
	}

	ok, err := r.SetContains(ctx, key, "member")
	if err != nil || !ok {
		t.Fatalf("before TTL: expected true/nil, got %v/%v", ok, err)
	}

	time.Sleep(ttl + 500*time.Millisecond)

	ok, err = r.SetContains(ctx, key, "member")
	if err != nil {
		t.Fatalf("after TTL: %v", err)
	}
	if ok {
		t.Error("expected Contains=false after TTL")
	}
}

func TestRedisStateSync_Integration_RemoveFromSet_NonExistentKey(t *testing.T) {
	url := startRedisContainerForStateSync(t)
	ctx := context.Background()

	r, err := NewRedisStateSync(ctx, RedisConfig{URL: url})
	if err != nil {
		t.Fatalf("NewRedisStateSync: %v", err)
	}
	t.Cleanup(func() { _ = r.Close() })

	// Removing from a key that does not exist must be a no-op.
	if err := r.RemoveFromSet(ctx, "ghost-key", "member"); err != nil {
		t.Errorf("RemoveFromSet on non-existent key: unexpected error: %v", err)
	}
}

func TestRedisStateSync_Integration_MultipleInstances(t *testing.T) {
	url := startRedisContainerForStateSync(t)
	ctx := context.Background()

	// Simulate two VibeWarden instances sharing the same Redis.
	r1, err := NewRedisStateSync(ctx, RedisConfig{URL: url})
	if err != nil {
		t.Fatalf("instance 1 NewRedisStateSync: %v", err)
	}
	t.Cleanup(func() { _ = r1.Close() })

	r2, err := NewRedisStateSync(ctx, RedisConfig{URL: url})
	if err != nil {
		t.Fatalf("instance 2 NewRedisStateSync: %v", err)
	}
	t.Cleanup(func() { _ = r2.Close() })

	key := fmt.Sprintf("vw:ss:test:multi:%d", time.Now().UnixNano())

	// r1 increments the counter.
	v1, err := r1.IncrementCounter(ctx, key, 3, 0)
	if err != nil {
		t.Fatalf("r1 IncrementCounter: %v", err)
	}
	if v1 != 3 {
		t.Errorf("r1 IncrementCounter = %d, want 3", v1)
	}

	// r2 must see the value incremented by r1.
	got, err := r2.GetCounter(ctx, key)
	if err != nil {
		t.Fatalf("r2 GetCounter: %v", err)
	}
	if got != 3 {
		t.Errorf("r2 GetCounter = %d, want 3", got)
	}

	// r2 increments further.
	v2, err := r2.IncrementCounter(ctx, key, 2, 0)
	if err != nil {
		t.Fatalf("r2 IncrementCounter: %v", err)
	}
	if v2 != 5 {
		t.Errorf("r2 IncrementCounter = %d, want 5", v2)
	}

	// r1 must see the combined total.
	got, err = r1.GetCounter(ctx, key)
	if err != nil {
		t.Fatalf("r1 GetCounter after r2 increment: %v", err)
	}
	if got != 5 {
		t.Errorf("r1 GetCounter = %d, want 5", got)
	}

	// Set cross-instance: r1 adds, r2 checks.
	setKey := fmt.Sprintf("vw:ss:test:multi-set:%d", time.Now().UnixNano())
	if err := r1.AddToSet(ctx, setKey, "blocked-ip", 0); err != nil {
		t.Fatalf("r1 AddToSet: %v", err)
	}
	ok, err := r2.SetContains(ctx, setKey, "blocked-ip")
	if err != nil {
		t.Fatalf("r2 SetContains: %v", err)
	}
	if !ok {
		t.Error("r2: expected Contains=true for member added by r1")
	}
}

func TestRedisStateSync_Integration_PoolSize(t *testing.T) {
	url := startRedisContainerForStateSync(t)
	ctx := context.Background()

	// Verify that a custom PoolSize is accepted without error.
	r, err := NewRedisStateSync(ctx, RedisConfig{URL: url, PoolSize: 5})
	if err != nil {
		t.Fatalf("NewRedisStateSync with PoolSize=5: %v", err)
	}
	t.Cleanup(func() { _ = r.Close() })

	key := fmt.Sprintf("vw:ss:test:pool:%d", time.Now().UnixNano())
	if _, err := r.IncrementCounter(ctx, key, 1, 0); err != nil {
		t.Errorf("IncrementCounter with custom pool: %v", err)
	}
}
