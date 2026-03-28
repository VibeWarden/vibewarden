package events

import (
	"fmt"
	"time"
)

// RateLimitStoreFallbackParams contains the parameters needed to construct a
// rate_limit.store_fallback event.
type RateLimitStoreFallbackParams struct {
	// Reason is a short description of why the fallback was triggered.
	Reason string
}

// NewRateLimitStoreFallback creates a rate_limit.store_fallback event emitted
// when the Redis store becomes unavailable and the rate limiter switches to
// the in-memory store.
func NewRateLimitStoreFallback(params RateLimitStoreFallbackParams) Event {
	return Event{
		SchemaVersion: SchemaVersion,
		EventType:     EventTypeRateLimitStoreFallback,
		Timestamp:     time.Now().UTC(),
		AISummary: fmt.Sprintf(
			"Rate limiter fell back to in-memory store: %s", params.Reason,
		),
		Payload: map[string]any{
			"reason": params.Reason,
			"store":  "memory",
		},
	}
}

// NewRateLimitStoreRecovered creates a rate_limit.store_recovered event emitted
// when the Redis store becomes reachable again and the rate limiter switches
// back from the in-memory fallback to Redis.
func NewRateLimitStoreRecovered() Event {
	return Event{
		SchemaVersion: SchemaVersion,
		EventType:     EventTypeRateLimitStoreRecovered,
		Timestamp:     time.Now().UTC(),
		AISummary:     "Rate limiter recovered: switched back to Redis store",
		Payload: map[string]any{
			"store": "redis",
		},
	}
}
