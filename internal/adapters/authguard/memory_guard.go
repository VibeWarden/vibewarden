// Package authguard provides in-memory implementations of the
// ports.AuthLockoutGuard port.
package authguard

import (
	"errors"
	"fmt"
	"sync"
	"time"

	domain "github.com/vibewarden/vibewarden/internal/domain/authguard"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// DefaultMaxEntries is the default cap on the number of tracked client keys.
// At roughly 100–200 bytes per entry this bounds the guard to a couple of
// megabytes even under a source-IP-cycling flood.
const DefaultMaxEntries = 10_000

// entry pairs the domain state machine for one key with the last time that key
// was touched, which drives eviction when the map reaches its cap.
type entry struct {
	attempts *domain.Attempts
	lastSeen time.Time
}

// MemoryGuard is a process-local, concurrency-safe ports.AuthLockoutGuard.
//
// It holds one domain.Attempts per client key behind a single mutex. Memory is
// bounded by maxEntries: when a new key would exceed the cap, idle entries are
// swept and, if that frees nothing, the least recently seen entry is evicted.
// Eviction is inline — deliberately not a background goroutine, because the
// guard is constructed by a Caddy handler lifecycle that offers no matching
// teardown hook.
//
// A MemoryGuard must be created with NewMemoryGuard; the zero value is not
// usable.
type MemoryGuard struct {
	policy     domain.Policy
	maxEntries int
	now        func() time.Time

	mu      sync.Mutex
	entries map[string]*entry
}

// Option customises a MemoryGuard at construction time.
type Option func(*MemoryGuard)

// WithClock replaces the guard's time source. It exists for deterministic
// tests; production callers omit it and get time.Now. A nil clock is ignored.
func WithClock(clock func() time.Time) Option {
	return func(g *MemoryGuard) {
		if clock != nil {
			g.now = clock
		}
	}
}

// NewMemoryGuard creates a MemoryGuard enforcing the given policy and tracking
// at most maxEntries distinct client keys. It returns an error when the policy
// is invalid or maxEntries is not positive.
func NewMemoryGuard(policy domain.Policy, maxEntries int, opts ...Option) (*MemoryGuard, error) {
	if err := policy.Validate(); err != nil {
		return nil, fmt.Errorf("auth lockout policy: %w", err)
	}
	if maxEntries <= 0 {
		return nil, errors.New("auth lockout max entries must be greater than zero")
	}

	g := &MemoryGuard{
		policy:     policy,
		maxEntries: maxEntries,
		now:        time.Now,
		entries:    make(map[string]*entry),
	}
	for _, opt := range opts {
		opt(g)
	}
	return g, nil
}

// NewDefaultMemoryGuard creates a MemoryGuard with the shipped default policy
// and DefaultMaxEntries.
func NewDefaultMemoryGuard() (*MemoryGuard, error) {
	return NewMemoryGuard(domain.DefaultPolicy(), DefaultMaxEntries)
}

// Status implements ports.AuthLockoutGuard. It never creates an entry and
// never increments a failure count; it only lazily expires an elapsed lockout.
func (g *MemoryGuard) Status(key string) ports.LockoutStatus {
	if key == "" {
		return g.statusOf(nil)
	}

	g.mu.Lock()
	defer g.mu.Unlock()

	e, ok := g.entries[key]
	if !ok {
		return g.statusOf(nil)
	}

	now := g.now()
	locked, retryAfter := e.attempts.LockedOut(now)
	e.lastSeen = now

	st := g.statusOf(e.attempts)
	st.LockedOut = locked
	st.RetryAfter = retryAfter
	return st
}

// RecordFailure implements ports.AuthLockoutGuard. It is the only method that
// allocates tracking state, so no traffic pattern other than real
// authentication failures can grow the map.
func (g *MemoryGuard) RecordFailure(key string) ports.LockoutStatus {
	if key == "" {
		return g.statusOf(nil)
	}

	g.mu.Lock()
	defer g.mu.Unlock()

	now := g.now()
	e, ok := g.entries[key]
	if !ok {
		g.makeRoomLocked(now)
		e = &entry{attempts: &domain.Attempts{}}
		g.entries[key] = e
	}

	tripped := e.attempts.RecordFailure(now, g.policy)
	e.lastSeen = now

	locked, retryAfter := e.attempts.LockedOut(now)

	st := g.statusOf(e.attempts)
	st.Tripped = tripped
	// A tripped call still returns the 401 path, so LockedOut stays false on
	// the call that arms the lockout; RetryAfter is reported either way.
	st.LockedOut = locked && !tripped
	st.RetryAfter = retryAfter
	return st
}

// RecordSuccess implements ports.AuthLockoutGuard. It clears the key's state
// and drops the entry entirely, keeping the map as small as possible.
func (g *MemoryGuard) RecordSuccess(key string) {
	if key == "" {
		return
	}

	g.mu.Lock()
	defer g.mu.Unlock()

	if e, ok := g.entries[key]; ok {
		e.attempts.Reset()
		delete(g.entries, key)
	}
}

// Len returns the number of tracked keys. It exists for tests and diagnostics.
func (g *MemoryGuard) Len() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return len(g.entries)
}

// makeRoomLocked ensures there is space for one more entry. The caller must
// hold g.mu.
//
// It first sweeps every entry the domain considers idle (no active lockout and
// no live failure streak). If the map is still full, the least recently seen
// entry is evicted so that an active lockout is only ever dropped when every
// tracked key is itself under an active lockout.
func (g *MemoryGuard) makeRoomLocked(now time.Time) {
	if len(g.entries) < g.maxEntries {
		return
	}

	for k, e := range g.entries {
		if e.attempts.Idle(now, g.policy) {
			delete(g.entries, k)
		}
	}
	if len(g.entries) < g.maxEntries {
		return
	}

	var oldestKey string
	var oldestSeen time.Time
	for k, e := range g.entries {
		if oldestKey == "" || e.lastSeen.Before(oldestSeen) {
			oldestKey = k
			oldestSeen = e.lastSeen
		}
	}
	if oldestKey != "" {
		delete(g.entries, oldestKey)
	}
}

// statusOf builds a LockoutStatus carrying the guard's policy. A nil attempts
// yields the zero-failure status used for unknown keys.
func (g *MemoryGuard) statusOf(a *domain.Attempts) ports.LockoutStatus {
	st := ports.LockoutStatus{
		Threshold: g.policy.Threshold,
		Window:    g.policy.Window,
		Cooldown:  g.policy.Cooldown,
	}
	if a != nil {
		st.Failures = a.Failures()
	}
	return st
}

// Interface guard.
var _ ports.AuthLockoutGuard = (*MemoryGuard)(nil)
