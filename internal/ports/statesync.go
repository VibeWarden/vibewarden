package ports

import (
	"context"
	"time"
)

// StateSync is the outbound port for distributed state synchronisation.
// Implementations provide atomic counter and set operations whose state is
// visible to all VibeWarden instances that share the same backend (e.g. Redis).
//
// In single-instance deployments the no-op implementation is used, so all
// operations fall back to the local in-process store without any network I/O.
//
// The interface is intentionally narrow: it expresses the operations needed by
// the current set of plugins (rate limiting, IP blocklists, circuit breakers).
// Future state types (e.g. distributed session flags) are added by extending
// the interface in a backwards-compatible way.
type StateSync interface {
	// IncrementCounter atomically adds delta to the counter identified by key
	// and returns the new value. If the counter does not yet exist it is created
	// starting at zero before applying the delta.
	//
	// ttl is the suggested expiry for the counter entry. Passing a zero duration
	// means no expiry. The implementation may round the TTL to its internal
	// precision (e.g. Redis integer seconds).
	//
	// delta must be positive; implementations must return an error when delta ≤ 0.
	IncrementCounter(ctx context.Context, key string, delta int64, ttl time.Duration) (int64, error)

	// GetCounter returns the current value of the counter identified by key.
	// If the counter does not exist the returned value is 0 with a nil error.
	GetCounter(ctx context.Context, key string) (int64, error)

	// AddToSet adds member to the set identified by key.
	// If the set does not yet exist it is created. Adding a member that is
	// already present is a no-op (returns nil).
	//
	// ttl is the suggested expiry for the set entry. Zero means no expiry.
	AddToSet(ctx context.Context, key string, member string, ttl time.Duration) error

	// RemoveFromSet removes member from the set identified by key.
	// Removing a member that is not present is a no-op (returns nil).
	RemoveFromSet(ctx context.Context, key string, member string) error

	// SetContains reports whether member is present in the set identified by key.
	// If the set does not exist the result is false with a nil error.
	SetContains(ctx context.Context, key string, member string) (bool, error)

	// Close releases any resources held by the implementation, such as
	// connection pools or background goroutines. Should be called on graceful
	// shutdown. Safe to call more than once.
	Close() error
}
