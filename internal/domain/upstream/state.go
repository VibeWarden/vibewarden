// Package upstream contains the domain value object for upstream component state
// as surfaced on the /_vibewarden/health wire format.
//
// This package has zero external dependencies — only Go stdlib.
package upstream

// Kind identifies the upstream state variant.
type Kind int

const (
	// KindUnknown means no probe has completed yet, or the probe is disabled
	// with no signal. Renderers should surface a neutral "unknown" token.
	KindUnknown Kind = iota

	// KindOk means the background probe is reporting the upstream healthy.
	KindOk

	// KindDegraded means the probe was explicitly disabled by operator config
	// (upstream.health.enabled: false). It is a neutral operator opt-out, not
	// a fault. The outer aggregator treats this as healthy to avoid false pages.
	KindDegraded

	// KindFailing means the background probe is reporting the upstream unhealthy.
	KindFailing
)

// State is an immutable value object describing the current upstream component
// state as observed by the background health probe. It is produced by the
// application layer (mapping from domain/health.UpstreamStatus) and consumed by
// the Caddy health handler and healthsummary aggregator.
//
// Construct instances via the NewXxx constructors; the zero value is a valid
// KindUnknown state. Fields are unexported to preserve invariants.
type State struct {
	kind      Kind
	lastError string
}

// NewUnknown returns a State indicating no probe signal is available.
func NewUnknown() State { return State{kind: KindUnknown} }

// NewOk returns a State indicating the upstream probe is reporting healthy.
func NewOk() State { return State{kind: KindOk} }

// NewDegraded returns a State indicating the probe was disabled by operator
// config. The outer aggregator treats this as healthy (explicit opt-out, not
// a fault).
func NewDegraded() State { return State{kind: KindDegraded} }

// NewFailing returns a State indicating the upstream probe is reporting
// unhealthy. lastError is the error string from the most recent failed probe.
func NewFailing(lastError string) State { return State{kind: KindFailing, lastError: lastError} }

// Kind returns the state variant.
func (s State) Kind() Kind { return s.kind }

// LastError returns the error string from the most recent failed probe.
// Empty for all variants except KindFailing.
func (s State) LastError() string { return s.lastError }

// Healthy reports whether this component should NOT degrade the outer status.
// Ok is healthy. Degraded (probe disabled) is treated as healthy — it is an
// explicit operator opt-out, not a fault. Unknown and Failing are not healthy
// and will degrade the outer aggregated status.
func (s State) Healthy() bool {
	switch s.kind {
	case KindOk, KindDegraded:
		return true
	default:
		return false
	}
}

// String returns the lowercase token used in the JSON components.upstream field.
// These strings are the stable wire format; callers depend on them.
func (s State) String() string {
	switch s.kind {
	case KindOk:
		return "ok"
	case KindDegraded:
		return "degraded"
	case KindFailing:
		return "failing"
	default:
		return "unknown"
	}
}
