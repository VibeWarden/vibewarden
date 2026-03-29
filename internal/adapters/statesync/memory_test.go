package statesync

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/vibewarden/vibewarden/internal/ports"
)

// ---------------------------------------------------------------------------
// IncrementCounter
// ---------------------------------------------------------------------------

func TestMemoryStateSync_IncrementCounter(t *testing.T) {
	tests := []struct {
		name      string
		key       string
		delta     int64
		wantValue int64
		wantErr   bool
	}{
		{"increment by one", "key1", 1, 1, false},
		{"increment by ten", "key2", 10, 10, false},
		{"zero delta", "key3", 0, 0, true},
		{"negative delta", "key4", -1, 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewMemoryStateSync()
			got, err := m.IncrementCounter(context.Background(), tt.key, tt.delta, 0)
			if (err != nil) != tt.wantErr {
				t.Errorf("IncrementCounter() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err == nil && got != tt.wantValue {
				t.Errorf("IncrementCounter() = %d, want %d", got, tt.wantValue)
			}
		})
	}
}

func TestMemoryStateSync_IncrementCounter_Accumulates(t *testing.T) {
	m := NewMemoryStateSync()
	ctx := context.Background()
	key := "acc"

	for i := int64(1); i <= 3; i++ {
		got, err := m.IncrementCounter(ctx, key, i, 0)
		if err != nil {
			t.Fatalf("step %d: unexpected error: %v", i, err)
		}
		want := i * (i + 1) / 2
		if got != want {
			t.Errorf("step %d: got %d, want %d", i, got, want)
		}
	}
}

func TestMemoryStateSync_IncrementCounter_TTL(t *testing.T) {
	m := NewMemoryStateSync()
	ctx := context.Background()
	key := "ttl-counter"
	ttl := 50 * time.Millisecond

	_, err := m.IncrementCounter(ctx, key, 5, ttl)
	if err != nil {
		t.Fatalf("IncrementCounter: %v", err)
	}

	// Value should be present before TTL.
	got, err := m.GetCounter(ctx, key)
	if err != nil {
		t.Fatalf("GetCounter before TTL: %v", err)
	}
	if got != 5 {
		t.Errorf("before TTL: got %d, want 5", got)
	}

	time.Sleep(ttl + 20*time.Millisecond)

	// After TTL the counter should look absent (0).
	got, err = m.GetCounter(ctx, key)
	if err != nil {
		t.Fatalf("GetCounter after TTL: %v", err)
	}
	if got != 0 {
		t.Errorf("after TTL: got %d, want 0", got)
	}
}

func TestMemoryStateSync_IncrementCounter_CancelledContext(t *testing.T) {
	m := NewMemoryStateSync()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := m.IncrementCounter(ctx, "key", 1, 0)
	if err == nil {
		t.Error("expected error for cancelled context, got nil")
	}
}

// ---------------------------------------------------------------------------
// GetCounter
// ---------------------------------------------------------------------------

func TestMemoryStateSync_GetCounter_NonExistent(t *testing.T) {
	m := NewMemoryStateSync()
	got, err := m.GetCounter(context.Background(), "missing")
	if err != nil {
		t.Fatalf("GetCounter: %v", err)
	}
	if got != 0 {
		t.Errorf("GetCounter(missing) = %d, want 0", got)
	}
}

func TestMemoryStateSync_GetCounter_CancelledContext(t *testing.T) {
	m := NewMemoryStateSync()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := m.GetCounter(ctx, "key")
	if err == nil {
		t.Error("expected error for cancelled context, got nil")
	}
}

// ---------------------------------------------------------------------------
// AddToSet / RemoveFromSet / SetContains
// ---------------------------------------------------------------------------

func TestMemoryStateSync_Set_AddContainsRemove(t *testing.T) {
	m := NewMemoryStateSync()
	ctx := context.Background()
	key := "blocklist"

	// Initially empty.
	ok, err := m.SetContains(ctx, key, "1.2.3.4")
	if err != nil || ok {
		t.Fatalf("expected false/nil, got %v/%v", ok, err)
	}

	// Add a member.
	if err := m.AddToSet(ctx, key, "1.2.3.4", 0); err != nil {
		t.Fatalf("AddToSet: %v", err)
	}
	ok, err = m.SetContains(ctx, key, "1.2.3.4")
	if err != nil {
		t.Fatalf("SetContains: %v", err)
	}
	if !ok {
		t.Error("expected Contains=true after Add")
	}

	// Remove the member.
	if err := m.RemoveFromSet(ctx, key, "1.2.3.4"); err != nil {
		t.Fatalf("RemoveFromSet: %v", err)
	}
	ok, err = m.SetContains(ctx, key, "1.2.3.4")
	if err != nil {
		t.Fatalf("SetContains after Remove: %v", err)
	}
	if ok {
		t.Error("expected Contains=false after Remove")
	}
}

func TestMemoryStateSync_AddToSet_EmptyMember(t *testing.T) {
	m := NewMemoryStateSync()
	err := m.AddToSet(context.Background(), "key", "", 0)
	if err == nil {
		t.Error("expected error for empty member, got nil")
	}
}

func TestMemoryStateSync_RemoveFromSet_EmptyMember(t *testing.T) {
	m := NewMemoryStateSync()
	err := m.RemoveFromSet(context.Background(), "key", "")
	if err == nil {
		t.Error("expected error for empty member, got nil")
	}
}

func TestMemoryStateSync_RemoveFromSet_NonExistentKey(t *testing.T) {
	m := NewMemoryStateSync()
	err := m.RemoveFromSet(context.Background(), "ghost", "member")
	if err != nil {
		t.Errorf("RemoveFromSet on non-existent key: unexpected error: %v", err)
	}
}

func TestMemoryStateSync_AddToSet_Idempotent(t *testing.T) {
	m := NewMemoryStateSync()
	ctx := context.Background()
	key := "idem"

	_ = m.AddToSet(ctx, key, "a", 0)
	_ = m.AddToSet(ctx, key, "a", 0)

	// Entry should exist exactly once — no duplicates possible.
	m.mu.Lock()
	size := len(m.sets[key].members)
	m.mu.Unlock()
	if size != 1 {
		t.Errorf("after duplicate Add: member count = %d, want 1", size)
	}
}

func TestMemoryStateSync_Set_TTL(t *testing.T) {
	m := NewMemoryStateSync()
	ctx := context.Background()
	key := "ttl-set"
	ttl := 50 * time.Millisecond

	_ = m.AddToSet(ctx, key, "member", ttl)

	ok, err := m.SetContains(ctx, key, "member")
	if err != nil || !ok {
		t.Fatalf("before TTL: expected true/nil, got %v/%v", ok, err)
	}

	time.Sleep(ttl + 20*time.Millisecond)

	ok, err = m.SetContains(ctx, key, "member")
	if err != nil {
		t.Fatalf("after TTL: unexpected error: %v", err)
	}
	if ok {
		t.Error("after TTL: expected Contains=false, got true")
	}
}

func TestMemoryStateSync_Set_CancelledContext(t *testing.T) {
	m := NewMemoryStateSync()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := m.AddToSet(ctx, "k", "v", 0); err == nil {
		t.Error("AddToSet: expected error for cancelled context")
	}
	if err := m.RemoveFromSet(ctx, "k", "v"); err == nil {
		t.Error("RemoveFromSet: expected error for cancelled context")
	}
	if _, err := m.SetContains(ctx, "k", "v"); err == nil {
		t.Error("SetContains: expected error for cancelled context")
	}
}

// ---------------------------------------------------------------------------
// Close
// ---------------------------------------------------------------------------

func TestMemoryStateSync_Close(t *testing.T) {
	m := NewMemoryStateSync()
	if err := m.Close(); err != nil {
		t.Errorf("Close() unexpected error: %v", err)
	}
	// Second call must also return nil.
	if err := m.Close(); err != nil {
		t.Errorf("second Close() unexpected error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Concurrency
// ---------------------------------------------------------------------------

func TestMemoryStateSync_ConcurrentAccess(t *testing.T) {
	m := NewMemoryStateSync()
	ctx := context.Background()

	const goroutines = 50
	const ops = 100

	var wg sync.WaitGroup
	wg.Add(goroutines * 2)

	// Concurrent counter increments.
	for range goroutines {
		go func() {
			defer wg.Done()
			for range ops {
				_, _ = m.IncrementCounter(ctx, "shared-counter", 1, 0)
			}
		}()
	}

	// Concurrent set operations.
	for i := range goroutines {
		go func(i int) {
			defer wg.Done()
			member := "member"
			for range ops {
				_ = m.AddToSet(ctx, "shared-set", member, 0)
				_, _ = m.SetContains(ctx, "shared-set", member)
				_ = m.RemoveFromSet(ctx, "shared-set", member)
			}
		}(i)
	}

	wg.Wait()
}

// ---------------------------------------------------------------------------
// Compile-time interface satisfaction
// ---------------------------------------------------------------------------

var _ ports.StateSync = (*MemoryStateSync)(nil)
