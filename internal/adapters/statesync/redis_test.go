package statesync

import (
	"context"
	"testing"
)

// ---------------------------------------------------------------------------
// NewRedisStateSync — config validation (no Redis required)
// ---------------------------------------------------------------------------

func TestNewRedisStateSync_EmptyURL(t *testing.T) {
	_, err := NewRedisStateSync(context.Background(), RedisConfig{})
	if err == nil {
		t.Fatal("expected error for empty URL, got nil")
	}
}

func TestNewRedisStateSync_InvalidURL(t *testing.T) {
	_, err := NewRedisStateSync(context.Background(), RedisConfig{URL: "not-a-url"})
	if err == nil {
		t.Fatal("expected error for invalid URL, got nil")
	}
}

func TestNewRedisStateSync_UnreachableHost(t *testing.T) {
	// Port 1 is reserved and should always refuse connections quickly.
	_, err := NewRedisStateSync(context.Background(), RedisConfig{
		URL: "redis://localhost:1/0",
	})
	if err == nil {
		t.Fatal("expected error when Redis is unreachable, got nil")
	}
}

// ---------------------------------------------------------------------------
// Argument validation (no Redis required — errors must be returned before
// any network call when the context is cancelled or arguments are invalid)
// ---------------------------------------------------------------------------

func TestRedisStateSync_IncrementCounter_InvalidDelta(t *testing.T) {
	// We cannot construct a valid RedisStateSync without a live Redis, so we
	// directly exercise the argument-validation path by checking the error that
	// is returned before any Redis I/O.
	tests := []struct {
		name  string
		delta int64
	}{
		{"zero delta", 0},
		{"negative delta", -5},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Use a nil-client adapter: argument validation fires before any
			// client call, so we can safely use a zero-value struct here.
			r := &RedisStateSync{}
			_, err := r.IncrementCounter(context.Background(), "k", tt.delta, 0)
			if err == nil {
				t.Errorf("IncrementCounter(%d): expected error, got nil", tt.delta)
			}
		})
	}
}

func TestRedisStateSync_AddToSet_EmptyMember(t *testing.T) {
	r := &RedisStateSync{}
	err := r.AddToSet(context.Background(), "k", "", 0)
	if err == nil {
		t.Error("expected error for empty member, got nil")
	}
}

func TestRedisStateSync_RemoveFromSet_EmptyMember(t *testing.T) {
	r := &RedisStateSync{}
	err := r.RemoveFromSet(context.Background(), "k", "")
	if err == nil {
		t.Error("expected error for empty member, got nil")
	}
}

func TestRedisStateSync_CancelledContext(t *testing.T) {
	r := &RedisStateSync{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := r.IncrementCounter(ctx, "k", 1, 0); err == nil {
		t.Error("IncrementCounter: expected error for cancelled context")
	}
	if _, err := r.GetCounter(ctx, "k"); err == nil {
		t.Error("GetCounter: expected error for cancelled context")
	}
	if err := r.AddToSet(ctx, "k", "v", 0); err == nil {
		t.Error("AddToSet: expected error for cancelled context")
	}
	if err := r.RemoveFromSet(ctx, "k", "v"); err == nil {
		t.Error("RemoveFromSet: expected error for cancelled context")
	}
	if _, err := r.SetContains(ctx, "k", "v"); err == nil {
		t.Error("SetContains: expected error for cancelled context")
	}
}
