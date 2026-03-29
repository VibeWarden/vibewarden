// Package statesync provides implementations of the ports.StateSync interface.
package statesync

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/vibewarden/vibewarden/internal/ports"
)

// counterEntry holds an int64 counter together with an optional expiry time.
type counterEntry struct {
	value     int64
	expiresAt time.Time // zero means no expiry
}

// expired reports whether this entry has passed its TTL.
func (e *counterEntry) expired(now time.Time) bool {
	return !e.expiresAt.IsZero() && now.After(e.expiresAt)
}

// setEntry holds a string set together with an optional expiry time.
type setEntry struct {
	members   map[string]struct{}
	expiresAt time.Time // zero means no expiry
}

// expired reports whether this entry has passed its TTL.
func (e *setEntry) expired(now time.Time) bool {
	return !e.expiresAt.IsZero() && now.After(e.expiresAt)
}

// MemoryStateSync is an in-process implementation of ports.StateSync. It is
// designed for single-instance deployments where cross-instance synchronisation
// is not required. All operations are local and in-memory; nothing is persisted
// or sent over the network.
//
// MemoryStateSync is safe for concurrent use.
type MemoryStateSync struct {
	mu       sync.Mutex
	counters map[string]*counterEntry
	sets     map[string]*setEntry
}

// NewMemoryStateSync creates a new MemoryStateSync ready for use.
// The caller must call Close when the store is no longer needed.
func NewMemoryStateSync() *MemoryStateSync {
	return &MemoryStateSync{
		counters: make(map[string]*counterEntry),
		sets:     make(map[string]*setEntry),
	}
}

// IncrementCounter implements ports.StateSync.
func (m *MemoryStateSync) IncrementCounter(ctx context.Context, key string, delta int64, ttl time.Duration) (int64, error) {
	if ctx.Err() != nil {
		return 0, fmt.Errorf("incrementing counter %q: %w", key, ctx.Err())
	}
	if delta <= 0 {
		return 0, errors.New("counter delta must be positive")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	entry, ok := m.counters[key]
	if !ok || entry.expired(now) {
		entry = &counterEntry{}
		m.counters[key] = entry
	}

	entry.value += delta
	if ttl > 0 {
		entry.expiresAt = now.Add(ttl)
	}

	return entry.value, nil
}

// GetCounter implements ports.StateSync.
func (m *MemoryStateSync) GetCounter(ctx context.Context, key string) (int64, error) {
	if ctx.Err() != nil {
		return 0, fmt.Errorf("getting counter %q: %w", key, ctx.Err())
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	entry, ok := m.counters[key]
	if !ok || entry.expired(time.Now()) {
		return 0, nil
	}
	return entry.value, nil
}

// AddToSet implements ports.StateSync.
func (m *MemoryStateSync) AddToSet(ctx context.Context, key string, member string, ttl time.Duration) error {
	if ctx.Err() != nil {
		return fmt.Errorf("adding to set %q: %w", key, ctx.Err())
	}
	if member == "" {
		return errors.New("set member must not be empty")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	entry, ok := m.sets[key]
	if !ok || entry.expired(now) {
		entry = &setEntry{members: make(map[string]struct{})}
		m.sets[key] = entry
	}

	entry.members[member] = struct{}{}
	if ttl > 0 {
		entry.expiresAt = now.Add(ttl)
	}

	return nil
}

// RemoveFromSet implements ports.StateSync.
func (m *MemoryStateSync) RemoveFromSet(ctx context.Context, key string, member string) error {
	if ctx.Err() != nil {
		return fmt.Errorf("removing from set %q: %w", key, ctx.Err())
	}
	if member == "" {
		return errors.New("set member must not be empty")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	entry, ok := m.sets[key]
	if !ok || entry.expired(time.Now()) {
		return nil
	}
	delete(entry.members, member)
	return nil
}

// SetContains implements ports.StateSync.
func (m *MemoryStateSync) SetContains(ctx context.Context, key string, member string) (bool, error) {
	if ctx.Err() != nil {
		return false, fmt.Errorf("checking set %q: %w", key, ctx.Err())
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	entry, ok := m.sets[key]
	if !ok || entry.expired(time.Now()) {
		return false, nil
	}
	_, found := entry.members[member]
	return found, nil
}

// Close implements ports.StateSync.
// For the in-memory implementation this is a no-op that always returns nil.
func (m *MemoryStateSync) Close() error {
	return nil
}

// Compile-time interface satisfaction check.
var _ ports.StateSync = (*MemoryStateSync)(nil)
