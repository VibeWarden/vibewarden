// Package ports defines the interfaces (ports) for VibeWarden's hexagonal architecture.
package ports

import "github.com/vibewarden/vibewarden/internal/domain/events"

// StoredEvent is a single event persisted in a ring buffer along with its
// monotonically-increasing cursor identifier.
type StoredEvent struct {
	// Cursor is the auto-incrementing ID assigned when the event was stored.
	// Cursor values are unique and strictly increasing within a single process
	// lifetime.
	Cursor uint64

	// Event is the original structured event.
	Event events.Event
}

// EventRingBuffer is the outbound port for querying recently-stored structured
// events. Implementations hold the most recent events in a fixed-capacity
// circular buffer and expose cursor-based pagination.
type EventRingBuffer interface {
	// Query returns up to limit events whose cursor is strictly greater than
	// since, filtered to the given event types. Pass nil or an empty slice to
	// types to return all event types. Pass 0 to since to return the oldest
	// available events.
	//
	// Events are returned oldest-first (ascending cursor). The second return
	// value is the cursor of the last returned event; pass it as since on the
	// next call to resume from that position. If no events are returned, the
	// cursor equals since.
	Query(since uint64, types []string, limit int) ([]StoredEvent, uint64)
}
