// Package authguard contains the domain model for throttling repeated
// authentication failures per client key (in v1, a client IP address).
//
// The package is pure: it has zero external dependencies (Go stdlib only) and
// takes the current time as an explicit parameter so that all timing semantics
// are deterministically testable. Concurrency is the caller's responsibility —
// the in-memory adapter wraps these values in a mutex, mirroring the
// resilience.CircuitBreaker contract.
package authguard

import (
	"errors"
	"time"
)

const (
	// DefaultThreshold is the number of consecutive failed authentication
	// attempts from one client key that arms a lockout.
	DefaultThreshold = 10

	// DefaultWindow is how long a streak of consecutive failures stays alive.
	// A failure arriving more than DefaultWindow after the first failure of the
	// current streak restarts the streak at one.
	DefaultWindow = time.Minute

	// DefaultCooldown is how long a client key stays locked out once the
	// threshold is reached. When it elapses, all failure state is cleared.
	DefaultCooldown = time.Minute
)

// Policy is an immutable value object holding the lockout parameters.
type Policy struct {
	// Threshold is the number of consecutive failures that arms the lockout.
	// Must be > 0.
	Threshold int

	// Window is the lifetime of a failure streak, anchored on its first
	// failure. Must be > 0.
	Window time.Duration

	// Cooldown is how long the key stays locked out after the threshold is
	// reached. Must be > 0.
	Cooldown time.Duration
}

// DefaultPolicy returns the shipped policy: DefaultThreshold consecutive
// failures within DefaultWindow arm a lockout lasting DefaultCooldown.
func DefaultPolicy() Policy {
	return Policy{
		Threshold: DefaultThreshold,
		Window:    DefaultWindow,
		Cooldown:  DefaultCooldown,
	}
}

// Validate returns an error when the policy is not usable.
func (p Policy) Validate() error {
	if p.Threshold <= 0 {
		return errors.New("auth lockout threshold must be greater than zero")
	}
	if p.Window <= 0 {
		return errors.New("auth lockout window must be greater than zero")
	}
	if p.Cooldown <= 0 {
		return errors.New("auth lockout cooldown must be greater than zero")
	}
	return nil
}

// Attempts is the per-key entity tracking consecutive authentication failures
// and the resulting lockout.
//
// Attempts is NOT safe for concurrent use; the caller synchronises access.
// Every method that depends on the clock takes now explicitly.
type Attempts struct {
	failures    int
	windowStart time.Time
	lockedUntil time.Time
}

// LockedOut reports whether the key is currently locked out and, when it is,
// how long the caller should wait before retrying.
//
// LockedOut advances state: when a lockout has expired it clears all failure
// state so that the next failure starts a fresh streak. It never increments
// the failure counter.
//
// The comparison is !now.Before(lockedUntil), so a clock that jumps backwards
// extends the lockout rather than releasing it early.
func (a *Attempts) LockedOut(now time.Time) (locked bool, retryAfter time.Duration) {
	if a.lockedUntil.IsZero() {
		return false, 0
	}
	if !now.Before(a.lockedUntil) {
		a.reset()
		return false, 0
	}
	return true, a.lockedUntil.Sub(now)
}

// RecordFailure records one failed authentication attempt.
//
// It returns true only on the call that arms the lockout, so callers can emit
// exactly one event per lockout episode. While a lockout is already active the
// call is a no-op and returns false — the counter does not grow during the
// cooldown.
//
// When the failure arrives more than p.Window after the first failure of the
// current streak, the streak restarts at one.
func (a *Attempts) RecordFailure(now time.Time, p Policy) (tripped bool) {
	if locked, _ := a.LockedOut(now); locked {
		return false
	}

	if a.failures == 0 || now.Sub(a.windowStart) > p.Window {
		a.failures = 1
		a.windowStart = now
	} else {
		a.failures++
	}

	if a.failures >= p.Threshold {
		a.lockedUntil = now.Add(p.Cooldown)
		return true
	}
	return false
}

// Reset clears all failure and lockout state. It is called after a successful
// authentication.
func (a *Attempts) Reset() {
	a.reset()
}

// Failures returns the number of consecutive failures recorded in the current
// streak.
func (a *Attempts) Failures() int {
	return a.failures
}

// LockedUntil returns the instant at which an active lockout expires. The zero
// time is returned when no lockout is armed.
func (a *Attempts) LockedUntil() time.Time {
	return a.lockedUntil
}

// Idle reports whether the entity carries no information at time now: no
// active lockout and no live failure streak. Idle entries are safe to evict.
func (a *Attempts) Idle(now time.Time, p Policy) bool {
	if !a.lockedUntil.IsZero() && now.Before(a.lockedUntil) {
		return false
	}
	if a.failures == 0 {
		return true
	}
	return now.Sub(a.windowStart) > p.Window
}

// reset clears every field back to the zero value.
func (a *Attempts) reset() {
	a.failures = 0
	a.windowStart = time.Time{}
	a.lockedUntil = time.Time{}
}
