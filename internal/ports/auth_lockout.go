package ports

import "time"

// LockoutStatus is the outcome of a single lockout evaluation for one client
// key. It is a value object: implementations return it by value and callers
// must not assume any field is meaningful outside the documented conditions.
type LockoutStatus struct {
	// LockedOut is true when the client key is in its cooldown period and the
	// request must be rejected with 429 Too Many Requests without evaluating
	// the credential.
	LockedOut bool

	// Tripped is set only by RecordFailure, and only on the call that arms the
	// lockout. It is the signal for emitting exactly one lockout audit event
	// per episode.
	Tripped bool

	// RetryAfter is how long the caller should wait before retrying. It is
	// meaningful only when LockedOut or Tripped is true.
	RetryAfter time.Duration

	// Failures is the number of consecutive failures recorded for the key
	// after this call.
	Failures int

	// Threshold is the configured failure threshold, exposed so that callers
	// can populate audit payloads without importing the domain policy.
	Threshold int

	// Window is the configured failure-streak window, exposed for the same
	// reason as Threshold.
	Window time.Duration

	// Cooldown is the configured lockout duration, exposed for the same reason
	// as Threshold.
	Cooldown time.Duration
}

// AuthLockoutGuard throttles repeated authentication failures per client key.
//
// All methods must be safe for concurrent use. No method takes a
// context.Context: implementations are in-memory and sit on the request path,
// mirroring CircuitBreaker rather than RateLimiter (which may be backed by a
// network store).
//
// In v1 the only consumer is the admin-token middleware and the key is always
// a client IP address. Callers must never pass an empty key: an unresolvable
// client would otherwise share one bucket with every other unresolvable
// client, letting a single attacker lock them all out.
type AuthLockoutGuard interface {
	// Status reports whether key is currently locked out. It may lazily expire
	// state, but it must never increment a failure count and must never create
	// an entry for an unknown key.
	Status(key string) LockoutStatus

	// RecordFailure records one failed authentication attempt for key and
	// returns the resulting status. It is the only method allowed to allocate
	// tracking state.
	RecordFailure(key string) LockoutStatus

	// RecordSuccess clears all failure and lockout state for key.
	RecordSuccess(key string)
}
