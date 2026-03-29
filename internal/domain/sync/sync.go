// Package sync contains the domain model for cross-instance state synchronisation.
// All types in this package are pure domain objects with zero external dependencies.
package sync

import (
	"errors"
	"time"
)

// StateType is a discriminator that identifies the kind of state being synchronised.
// It is used as a type discriminator in SyncMessage so that consumers can route
// payloads to the correct handler without type-switching on interface values.
type StateType string

const (
	// StateTypeRateLimit identifies counter state used by the rate-limiting plugin.
	StateTypeRateLimit StateType = "rate_limit"

	// StateTypeIPBlocklist identifies set state used by the IP blocklist plugin.
	StateTypeIPBlocklist StateType = "ip_blocklist"

	// StateTypeCircuitBreaker identifies counter/flag state used by the circuit
	// breaker plugin.
	StateTypeCircuitBreaker StateType = "circuit_breaker"
)

// ErrUnknownStateType is returned when a StateType value is not recognised.
var ErrUnknownStateType = errors.New("unknown state type")

// Validate returns ErrUnknownStateType when s is not one of the declared constants.
func (s StateType) Validate() error {
	switch s {
	case StateTypeRateLimit, StateTypeIPBlocklist, StateTypeCircuitBreaker:
		return nil
	default:
		return ErrUnknownStateType
	}
}

// String returns the string form of the StateType.
func (s StateType) String() string { return string(s) }

// Counter is a syncable integer counter that supports increment and read operations.
// It is the domain model for per-key rate-limit counters, failure counts in circuit
// breakers, and any other integer accumulator that needs to be shared across instances.
//
// Counter is not safe for concurrent use on its own. Adapters must serialise access.
type Counter struct {
	value int64
}

// NewCounter creates a Counter with an initial value of zero.
func NewCounter() Counter {
	return Counter{}
}

// NewCounterWithValue creates a Counter seeded with the supplied initial value.
// Returns an error when value is negative.
func NewCounterWithValue(value int64) (Counter, error) {
	if value < 0 {
		return Counter{}, errors.New("counter initial value must be non-negative")
	}
	return Counter{value: value}, nil
}

// Increment adds delta to the counter and returns the resulting value.
// delta must be positive; passing zero or a negative delta returns an error.
func (c *Counter) Increment(delta int64) (int64, error) {
	if delta <= 0 {
		return c.value, errors.New("counter delta must be positive")
	}
	c.value += delta
	return c.value, nil
}

// Get returns the current counter value.
func (c *Counter) Get() int64 {
	return c.value
}

// Reset sets the counter back to zero and returns the value it held before reset.
func (c *Counter) Reset() int64 {
	prev := c.value
	c.value = 0
	return prev
}

// Set is a syncable collection of string members. It supports add, remove, and
// membership-test operations and is the domain model for IP blocklists and
// similar allow/deny collections.
//
// Set is not safe for concurrent use on its own. Adapters must serialise access.
type Set struct {
	members map[string]struct{}
}

// NewSet creates an empty Set.
func NewSet() Set {
	return Set{members: make(map[string]struct{})}
}

// Add inserts member into the set. Adding an already-present member is a no-op.
// Returns an error when member is empty.
func (s *Set) Add(member string) error {
	if member == "" {
		return errors.New("set member must not be empty")
	}
	s.members[member] = struct{}{}
	return nil
}

// Remove deletes member from the set. Removing a non-existent member is a no-op.
// Returns an error when member is empty.
func (s *Set) Remove(member string) error {
	if member == "" {
		return errors.New("set member must not be empty")
	}
	delete(s.members, member)
	return nil
}

// Contains reports whether member is present in the set.
func (s *Set) Contains(member string) bool {
	_, ok := s.members[member]
	return ok
}

// Size returns the number of members currently in the set.
func (s *Set) Size() int {
	return len(s.members)
}

// StateUpdate carries a delta change to a single named state entry.
// It is the unit of information passed to StateSync.Publish so that
// consumers can apply incremental updates rather than full snapshots.
type StateUpdate struct {
	// Type identifies which state subsystem owns this update.
	Type StateType

	// Key is the per-entry identifier, for example a client IP address or a
	// circuit breaker name.
	Key string

	// Delta is the signed integer change for counter-type state.
	// For set-type state it is not used; use Members instead.
	Delta int64

	// Members carries the full member list for set-type state updates.
	// Nil means this is a counter-type update.
	Members []string

	// TTL is the suggested expiry for this state entry. Zero means no expiry.
	TTL time.Duration
}

// Validate returns an error when the StateUpdate is structurally invalid.
func (u StateUpdate) Validate() error {
	if err := u.Type.Validate(); err != nil {
		return err
	}
	if u.Key == "" {
		return errors.New("state update key must not be empty")
	}
	return nil
}

// SyncMessage wraps a StateUpdate with routing metadata so that subscribers
// can quickly filter messages without deserialising the full payload.
type SyncMessage struct {
	// Type mirrors Update.Type for fast routing without unpacking Update.
	Type StateType

	// InstanceID identifies the VibeWarden instance that produced this message.
	// Consumers must ignore messages whose InstanceID matches their own to
	// avoid feedback loops.
	InstanceID string

	// Update is the actual state change being communicated.
	Update StateUpdate
}

// Validate returns an error when the SyncMessage is structurally invalid.
func (m SyncMessage) Validate() error {
	if m.InstanceID == "" {
		return errors.New("sync message instance ID must not be empty")
	}
	return m.Update.Validate()
}
