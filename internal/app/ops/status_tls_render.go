package ops

import (
	"fmt"

	tlsdomain "github.com/vibewarden/vibewarden/internal/domain/tls"
)

// renderTLSStatusLine renders a TLSState as a (detail, healthy) pair for
// display in the `vibew status` component table. The detail string matches
// the PM spec for #1090 (e.g. "obtained (expires 2026-07-21)"). Healthy
// controls the green/red mark.
func renderTLSStatusLine(state tlsdomain.State) (detail string, healthy bool) {
	switch state.Kind() {
	case tlsdomain.KindDisabled:
		return "disabled", true
	case tlsdomain.KindSelfSignedLocal:
		return "self-signed dev cert (rotates automatically)", true
	case tlsdomain.KindObtaining:
		return "obtaining (ACME in progress)", true
	case tlsdomain.KindObtained:
		return fmt.Sprintf("obtained (expires %s)", state.ExpiresAt().Format("2006-01-02")), true
	case tlsdomain.KindExpiringSoon:
		return fmt.Sprintf("near expiry (expires in %d days)", state.DaysLeft()), false
	case tlsdomain.KindFailing:
		if state.LastError() == "" {
			return "failing", false
		}
		return fmt.Sprintf("failing (last error: %s)", state.LastError()), false
	case tlsdomain.KindUnknown:
		return "state unavailable — start 'vibew dev'", true
	default:
		return state.String(), true
	}
}
