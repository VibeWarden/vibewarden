package ops

import (
	"fmt"

	tlsdomain "github.com/vibewarden/vibewarden/internal/domain/tls"
)

// renderTLSStatusLine maps a TLSState to a (detail, StatusState) pair for
// display in the `vibew status` component table.
//
// All states except KindFailing are mapped to StatusOK — near-expiry is an
// annotation, not a failure. A cert that is expiring soon is still serving
// traffic; the operator gets a genuine StatusFAIL only when renewal has
// failed (KindFailing). See ADR-095 for the full rationale.
func renderTLSStatusLine(state tlsdomain.State) (detail string, status StatusState) {
	switch state.Kind() {
	case tlsdomain.KindDisabled:
		return "disabled", StatusOK
	case tlsdomain.KindSelfSignedLocal:
		return "self-signed (dev)", StatusOK
	case tlsdomain.KindObtaining:
		return "obtaining (ACME in progress)", StatusOK
	case tlsdomain.KindObtained:
		return fmt.Sprintf("obtained (expires %s)", state.ExpiresAt().Format("2006-01-02")), StatusOK
	case tlsdomain.KindExpiringSoon:
		// Near-expiry is an annotation, never a failure — ADR-095.
		return fmt.Sprintf("obtained (expires in %d days)", state.DaysLeft()), StatusOK
	case tlsdomain.KindFailing:
		if state.LastError() == "" {
			return "failing", StatusFAIL
		}
		return fmt.Sprintf("failing (last error: %s)", state.LastError()), StatusFAIL
	case tlsdomain.KindUnknown:
		return "state unavailable — start 'vibew dev'", StatusOK
	default:
		return state.String(), StatusOK
	}
}
