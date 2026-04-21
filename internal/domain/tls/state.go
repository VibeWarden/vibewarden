package tls

import (
	"fmt"
	"time"
)

// CaddyLocalIssuerCN is the Common Name Caddy stamps on internal self-signed
// dev leaf certificates. It is the authoritative signal that the sidecar is
// serving a KindSelfSignedLocal cert and that expiry heuristics must not be
// applied. The constant lives in the domain layer so it can be imported by
// both the caddy adapter and the app-layer fallback resolver without creating
// an adapter → app or app → adapter dependency.
const CaddyLocalIssuerCN = "Caddy Local Authority"

// Kind identifies the TLS state variant.
type Kind int

const (
	// KindUnknown means the resolver has no signal (handshake unreachable,
	// no peer certs, etc.). Renderers should surface a neutral message, not
	// a warn or fail.
	KindUnknown Kind = iota
	// KindDisabled means TLS is turned off in config (cfg.TLS.Enabled == false).
	KindDisabled
	// KindSelfSignedLocal means the sidecar is serving a leaf issued by
	// Caddy's internal "Caddy Local Authority". The leaf rotates on a short
	// TTL (~12h) and is considered healthy regardless of NotAfter. Callers
	// MUST NOT inspect NotAfter for this variant.
	KindSelfSignedLocal
	// KindObtaining means TLS is enabled with an ACME provider but no leaf
	// is in the cert cache yet (ACME exchange in progress).
	KindObtaining
	// KindObtained means a valid non-self-signed leaf is present and has
	// more than 7 days left until expiry.
	KindObtained
	// KindExpiringSoon means a valid non-self-signed leaf is present with
	// 7 or fewer days left until expiry.
	KindExpiringSoon
	// KindFailing means the ACME exchange failed on the most recent
	// attempt. LastError carries the first line of the upstream error.
	KindFailing
)

// String returns a short lowercase token for the kind — useful for logs and
// tests. It is not the human-readable display form; see State.String for
// that.
func (k Kind) String() string {
	switch k {
	case KindDisabled:
		return "disabled"
	case KindSelfSignedLocal:
		return "self-signed-local"
	case KindObtaining:
		return "obtaining"
	case KindObtained:
		return "obtained"
	case KindExpiringSoon:
		return "expiring-soon"
	case KindFailing:
		return "failing"
	case KindUnknown:
		return "unknown"
	default:
		return fmt.Sprintf("kind(%d)", int(k))
	}
}

// State is an immutable value object describing the current state of the
// sidecar's TLS configuration. It is produced by a StateResolver and
// consumed by status and doctor renderers.
//
// Construct instances via the NewXxx constructors; the zero value is a
// valid KindUnknown state. Fields are unexported to preserve invariants.
type State struct {
	kind      Kind
	expiresAt time.Time
	daysLeft  int
	lastError string
}

// NewDisabled returns a Disabled TLS state.
func NewDisabled() State { return State{kind: KindDisabled} }

// NewSelfSignedLocal returns a state indicating the sidecar is serving a
// leaf issued by Caddy's internal CA. NotAfter is intentionally not tracked
// because the internal issuer rotates leaves on a short TTL.
func NewSelfSignedLocal() State { return State{kind: KindSelfSignedLocal} }

// NewObtaining returns a state indicating an ACME exchange is in progress
// (TLS enabled, provider is ACME-like, no leaf in cache yet).
func NewObtaining() State { return State{kind: KindObtaining} }

// NewObtained returns a state for a healthy non-self-signed leaf with the
// given expiry time.
func NewObtained(expiresAt time.Time) State {
	return State{kind: KindObtained, expiresAt: expiresAt}
}

// NewExpiringSoon returns a state for a non-self-signed leaf that is within
// the expiry warning window. daysLeft is clamped to >= 0.
func NewExpiringSoon(daysLeft int, expiresAt time.Time) State {
	if daysLeft < 0 {
		daysLeft = 0
	}
	return State{kind: KindExpiringSoon, daysLeft: daysLeft, expiresAt: expiresAt}
}

// NewFailing returns a state indicating the last ACME attempt failed.
// lastError is the first line of the upstream error message.
func NewFailing(lastError string) State {
	return State{kind: KindFailing, lastError: lastError}
}

// NewUnknown returns a state indicating no signal is available (e.g. the
// handshake fallback could not reach the sidecar).
func NewUnknown() State { return State{kind: KindUnknown} }

// Kind returns the state variant.
func (s State) Kind() Kind { return s.kind }

// ExpiresAt returns the leaf NotAfter time for Obtained and ExpiringSoon;
// zero time for other variants.
func (s State) ExpiresAt() time.Time { return s.expiresAt }

// DaysLeft returns the remaining days for ExpiringSoon; 0 otherwise.
func (s State) DaysLeft() int { return s.daysLeft }

// LastError returns the first line of the last ACME error for Failing;
// empty string otherwise.
func (s State) LastError() string { return s.lastError }

// String returns the human-readable display string specified in the PM
// spec for #1090 / #1078. It is the canonical user-facing rendering for
// a State and is shared between the status dashboard and the doctor
// check detail.
func (s State) String() string {
	switch s.kind {
	case KindDisabled:
		return "TLS disabled"
	case KindSelfSignedLocal:
		return "TLS self-signed dev cert (rotates automatically)"
	case KindObtaining:
		return "TLS obtaining (ACME in progress)"
	case KindObtained:
		return fmt.Sprintf("TLS obtained (expires %s)", s.expiresAt.Format("2006-01-02"))
	case KindExpiringSoon:
		return fmt.Sprintf("TLS near expiry (expires in %d days)", s.daysLeft)
	case KindFailing:
		if s.lastError == "" {
			return "TLS failing"
		}
		return fmt.Sprintf("TLS failing (last error: %s)", s.lastError)
	case KindUnknown:
		return "TLS state unavailable"
	default:
		return fmt.Sprintf("TLS state %s", s.kind)
	}
}

// Healthy reports whether this state should be displayed as a healthy
// green tick. Disabled and SelfSignedLocal are healthy; Obtained is
// healthy. ExpiringSoon, Obtaining, Failing and Unknown are not.
func (s State) Healthy() bool {
	switch s.kind {
	case KindDisabled, KindSelfSignedLocal, KindObtained:
		return true
	default:
		return false
	}
}
