package ops

import (
	"fmt"
	"time"

	tlsdomain "github.com/vibewarden/vibewarden/internal/domain/tls"
)

// renderTLSDoctorCheck converts a TLSState into a CheckResult for the
// `vibew doctor` report. Severity mapping follows the PM spec for
// #1090 / #1078:
//   - Disabled, SelfSignedLocal, Obtained (>7d) → OK
//   - Obtaining, ExpiringSoon, Unknown        → WARN
//   - Failing                                  → FAIL
//
// The Name is always "TLS certificate" and the Section is left to the
// caller (the doctor service wraps it with the Local Runtime section).
//
// now is injected so callers can fix the clock in tests without mutating
// any package-level variable.
func renderTLSDoctorCheck(state tlsdomain.State, now func() time.Time) CheckResult {
	result := CheckResult{Name: "TLS certificate"}

	switch state.Kind() {
	case tlsdomain.KindDisabled:
		result.Severity = SeverityOK
		result.Detail = "TLS plugin disabled"
	case tlsdomain.KindSelfSignedLocal:
		result.Severity = SeverityOK
		result.Detail = "self-signed dev cert (rotates automatically)"
	case tlsdomain.KindObtained:
		daysLeft := int(state.ExpiresAt().Sub(now()).Hours() / 24)
		if daysLeft < 0 {
			daysLeft = 0
		}
		result.Severity = SeverityOK
		result.Detail = fmt.Sprintf("valid until %s (%d days)", state.ExpiresAt().Format("2006-01-02"), daysLeft)
	case tlsdomain.KindExpiringSoon:
		// An ExpiringSoon state with NotAfter already in the past is
		// really "expired" — escalate to FAIL so automation treats it
		// as a hard failure.
		if !state.ExpiresAt().IsZero() && now().After(state.ExpiresAt()) {
			result.Severity = SeverityFail
			result.Detail = fmt.Sprintf("expired on %s", state.ExpiresAt().Format("2006-01-02"))
		} else {
			result.Severity = SeverityWarn
			result.Detail = fmt.Sprintf("expires in %d day(s) on %s", state.DaysLeft(), state.ExpiresAt().Format("2006-01-02"))
		}
	case tlsdomain.KindObtaining:
		result.Severity = SeverityWarn
		result.Detail = "ACME exchange in progress — rerun in 1-2 minutes"
	case tlsdomain.KindFailing:
		result.Severity = SeverityFail
		if state.LastError() == "" {
			result.Detail = "ACME exchange failed"
		} else {
			result.Detail = fmt.Sprintf("ACME exchange failed: %s", state.LastError())
		}
	case tlsdomain.KindUnknown:
		result.Severity = SeverityWarn
		result.Detail = "sidecar not reachable — start 'vibew dev'"
	default:
		result.Severity = SeverityWarn
		result.Detail = state.String()
	}

	return result
}
