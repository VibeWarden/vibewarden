// Package log provides the slog-based adapter for the ports.EventLogger interface.
package log

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/vibewarden/vibewarden/internal/domain/events"
	"github.com/vibewarden/vibewarden/internal/ports"
)

const (
	// DefaultRingBufferCapacity is the number of events the ring buffer holds
	// when no explicit capacity is given.
	DefaultRingBufferCapacity = 1000
)

// Compile-time assertion: RingBuffer implements both ports.EventLogger and
// ports.EventRingBuffer.
var _ ports.EventLogger = (*RingBuffer)(nil)
var _ ports.EventRingBuffer = (*RingBuffer)(nil)

// RingBuffer is a thread-safe, fixed-capacity circular buffer that stores the
// most recent structured events. When the buffer is full, the oldest event is
// overwritten. It implements ports.EventLogger so it can be used as an
// additional event sink alongside the primary stdout logger, and
// ports.EventRingBuffer so the admin API can query recent events.
//
// The ring buffer is purely in-memory and provides no durability guarantees.
// It is intended for the admin API live-query endpoint only.
type RingBuffer struct {
	mu       sync.RWMutex
	slots    []ports.StoredEvent
	capacity int

	// head is the index of the next slot to write into (mod capacity).
	head int

	// count tracks how many events have been stored (capped at capacity).
	count int

	// counter is the next cursor value to assign, incremented atomically.
	counter atomic.Uint64
}

// NewRingBuffer creates a RingBuffer with the given capacity.
// If capacity is less than 1, DefaultRingBufferCapacity is used.
func NewRingBuffer(capacity int) *RingBuffer {
	if capacity < 1 {
		capacity = DefaultRingBufferCapacity
	}
	return &RingBuffer{
		slots:    make([]ports.StoredEvent, capacity),
		capacity: capacity,
	}
}

// Log stores the event in the ring buffer. It implements ports.EventLogger.
// Log never returns an error — the ring buffer is best-effort and in-memory.
func (r *RingBuffer) Log(_ context.Context, event events.Event) error {
	cursor := r.counter.Add(1) // monotonically increasing; starts at 1

	r.mu.Lock()
	r.slots[r.head] = ports.StoredEvent{Cursor: cursor, Event: event}
	r.head = (r.head + 1) % r.capacity
	if r.count < r.capacity {
		r.count++
	}
	r.mu.Unlock()

	return nil
}

// Query returns up to limit events whose cursor is strictly greater than since,
// filtered to the given event types. Pass nil or an empty slice to types to
// return all event types. Pass 0 to since to return the oldest available events.
//
// The returned slice is sorted oldest-first (ascending cursor). The second
// return value is the cursor of the last returned event; pass it as since in
// the next call to resume from where you left off. If no events are returned,
// the returned cursor equals since.
func (r *RingBuffer) Query(since uint64, types []string, limit int) ([]ports.StoredEvent, uint64) {
	if limit < 1 {
		limit = 1
	}

	typeSet := buildTypeSet(types)

	r.mu.RLock()
	defer r.mu.RUnlock()

	// Collect events in order (oldest first). The ring buffer is circular;
	// when count == capacity, the oldest entry sits at head; otherwise at 0.
	start := 0
	if r.count == r.capacity {
		start = r.head
	}

	result := make([]ports.StoredEvent, 0, min(r.count, limit))
	for i := 0; i < r.count; i++ {
		idx := (start + i) % r.capacity
		se := r.slots[idx]

		if se.Cursor <= since {
			continue
		}
		if len(typeSet) > 0 && !typeSet[se.Event.EventType] {
			continue
		}
		result = append(result, se)
		if len(result) == limit {
			break
		}
	}

	newCursor := since
	if len(result) > 0 {
		newCursor = result[len(result)-1].Cursor
	}
	return result, newCursor
}

// buildTypeSet converts a slice of event type strings into a set (map) for
// O(1) membership tests. Returns nil (not an empty map) when types is empty so
// that callers can distinguish "no filter" from "empty filter".
func buildTypeSet(types []string) map[string]bool {
	if len(types) == 0 {
		return nil
	}
	s := make(map[string]bool, len(types))
	for _, t := range types {
		t = strings.TrimSpace(t)
		if t != "" {
			s[t] = true
		}
	}
	if len(s) == 0 {
		return nil
	}
	return s
}
