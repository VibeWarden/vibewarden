// Package tlspreflight contains the domain types for the Let's Encrypt
// rate-limit preflight check. It is a pure domain package — the only
// external dependency is the Go standard library. No adapters, no ports.
package tlspreflight

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// Status is the verdict of a single preflight check.
type Status string

const (
	// StatusOK means the domain has enough LE budget to issue a certificate.
	StatusOK Status = "OK"
	// StatusWarn means the domain is at 4/5 — one slot remaining this week.
	StatusWarn Status = "WARN"
	// StatusFail means the LE rate limit is exhausted for the domain.
	StatusFail Status = "FAIL"
)

// Budget constants encode the Let's Encrypt per-registered-domain rate limit.
// Values are frozen by ADR-090 — changing them requires a new ADR.
const (
	// BudgetWindow is the LE rate-limit rolling window.
	BudgetWindow = 168 * time.Hour
	// BudgetCapacity is the maximum number of certificates LE issues per
	// registered domain per BudgetWindow.
	BudgetCapacity = 5
	// WarnRemainingAt is the remaining-budget threshold at which the check
	// transitions to WARN (remaining <= WarnRemainingAt → WARN).
	WarnRemainingAt = 1
	// FailRemainingAt is the remaining-budget threshold at which the check
	// transitions to FAIL (remaining <= FailRemainingAt → FAIL).
	FailRemainingAt = 0
)

// Error sentinels returned by the crt.sh adapter and consumed by the
// application service to map network failures to WARN results.
var (
	// ErrCTUnavailable is returned when crt.sh is unreachable (network
	// timeout, DNS failure, connection refused, or non-200/non-429 HTTP status).
	ErrCTUnavailable = errors.New("crt.sh unreachable")
	// ErrCTResponseMalformed is returned when crt.sh returns a response
	// that cannot be decoded as JSON or has an unexpected Content-Type.
	ErrCTResponseMalformed = errors.New("crt.sh response malformed")
	// ErrCTThrottled is returned when crt.sh responds with HTTP 429.
	ErrCTThrottled = errors.New("crt.sh throttled request")
)

// Result is the outcome of checking a single registered domain's LE budget.
// It is a value object — all fields are set once and never mutated.
//
// When Err is non-nil the query failed; Status is StatusWarn and all numeric
// fields are zero-valued except Domain.
type Result struct {
	// Domain is the eTLD+1 that was queried, e.g. "example.com".
	Domain string
	// Status is the verdict: OK, WARN, or FAIL.
	Status Status
	// IssuedInWindow is the count of LE certificates issued within the last
	// 168 hours for Domain, as reported by crt.sh.
	IssuedInWindow int
	// RemainingBudget is BudgetCapacity minus IssuedInWindow, floored at 0.
	RemainingBudget int
	// OldestInWindow is the not_before of the oldest LE certificate still
	// inside the 168-hour window. Zero when no certificates were found.
	OldestInWindow time.Time
	// NextSlotAvailableAt is OldestInWindow + 168h — the earliest moment LE
	// will accept a new issuance. Zero when RemainingBudget > 0.
	NextSlotAvailableAt time.Time
	// Detail is the rendered human-readable summary used as CheckResult.Detail.
	Detail string
	// Err is non-nil when the CT query failed; Status is WARN in that case.
	Err error
}

// RenderDetail builds the Detail string for a Result.
// It is a pure function — the result is deterministic for a given input.
// now is used to format relative times; callers may inject a fixed clock for tests.
func RenderDetail(r Result) string {
	if r.Err != nil {
		return renderErrorDetail(r.Err)
	}
	switch r.Status {
	case StatusFail:
		next := r.NextSlotAvailableAt.Format(time.RFC1123)
		if r.IssuedInWindow > BudgetCapacity {
			return fmt.Sprintf(
				"LE rate limit exhausted for %s (observed %d issuances); next slot at %s; use --skip-le-preflight to bypass",
				r.Domain, r.IssuedInWindow, next,
			)
		}
		return fmt.Sprintf(
			"LE rate limit exhausted for %s; next slot at %s; use --skip-le-preflight to bypass",
			r.Domain, next,
		)
	case StatusWarn:
		return fmt.Sprintf("1 of 5 slots remaining this week for %s", r.Domain)
	default:
		remaining := r.RemainingBudget
		if remaining == BudgetCapacity {
			return fmt.Sprintf("5 of 5 slots free for %s", r.Domain)
		}
		return fmt.Sprintf("%d of 5 slots remaining for %s", remaining, r.Domain)
	}
}

// renderErrorDetail maps the error sentinel to a user-facing detail string.
func renderErrorDetail(err error) string {
	switch {
	case errors.Is(err, ErrCTThrottled):
		return "crt.sh throttled — try again in a few minutes; run with --skip-le-preflight to suppress"
	case errors.Is(err, ErrCTResponseMalformed):
		return "crt.sh returned unexpected response — rate-limit check skipped"
	default:
		// ErrCTUnavailable or any other wrapped network error.
		return "crt.sh unreachable — rate-limit check skipped; run with --skip-le-preflight to suppress"
	}
}

// CannotDeriveRegisteredDomainDetail returns the Detail string used when
// publicsuffix.EffectiveTLDPlusOne fails for a given input FQDN.
func CannotDeriveRegisteredDomainDetail(fqdn string) string {
	return fmt.Sprintf("cannot derive registered domain from %q — rate-limit check skipped", fqdn)
}

// NewFromQuery builds a Result from a raw query count and oldest-in-window time.
// It applies the severity thresholds defined by the LE budget constants.
func NewFromQuery(domain string, issued int, oldest time.Time) Result {
	remaining := BudgetCapacity - issued
	if remaining < 0 {
		remaining = 0
	}

	var status Status
	var nextSlot time.Time

	switch {
	case remaining <= FailRemainingAt:
		status = StatusFail
		if !oldest.IsZero() {
			nextSlot = oldest.Add(BudgetWindow)
		}
	case remaining <= WarnRemainingAt:
		status = StatusWarn
	default:
		status = StatusOK
	}

	r := Result{
		Domain:              domain,
		Status:              status,
		IssuedInWindow:      issued,
		RemainingBudget:     remaining,
		OldestInWindow:      oldest,
		NextSlotAvailableAt: nextSlot,
	}
	r.Detail = RenderDetail(r)
	return r
}

// NewFromError builds a Result for a failed CT query. Status is always WARN.
func NewFromError(domain string, err error) Result {
	r := Result{
		Domain: domain,
		Status: StatusWarn,
		Err:    err,
	}
	r.Detail = RenderDetail(r)
	return r
}

// NewSkipped builds a WARN Result for a domain where the registered-domain
// could not be derived (e.g. a single-label hostname).
func NewSkipped(fqdn string) Result {
	detail := CannotDeriveRegisteredDomainDetail(fqdn)
	return Result{
		Domain: fqdn,
		Status: StatusWarn,
		Detail: detail,
		Err:    fmt.Errorf("%s", strings.TrimSuffix(detail, " — rate-limit check skipped")),
	}
}
