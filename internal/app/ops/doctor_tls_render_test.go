package ops

import (
	"testing"
	"time"

	tlsdomain "github.com/vibewarden/vibewarden/internal/domain/tls"
)

func TestRenderTLSDoctorCheck(t *testing.T) {
	fixedNow := time.Date(2026, 4, 20, 12, 0, 0, 0, time.UTC)
	origNow := nowFn
	nowFn = func() time.Time { return fixedNow }
	t.Cleanup(func() { nowFn = origNow })

	expiryFar := fixedNow.Add(90 * 24 * time.Hour)
	expirySoon := fixedNow.Add(3 * 24 * time.Hour)

	tests := []struct {
		name         string
		state        tlsdomain.State
		wantSeverity Severity
		wantDetail   string
	}{
		{"disabled → OK", tlsdomain.NewDisabled(), SeverityOK, "TLS plugin disabled"},
		{"self-signed → OK", tlsdomain.NewSelfSignedLocal(), SeverityOK, "self-signed dev cert (rotates automatically)"},
		{"obtained → OK", tlsdomain.NewObtained(expiryFar), SeverityOK, "valid until 2026-07-19 (90 days)"},
		{"expiring → WARN", tlsdomain.NewExpiringSoon(3, expirySoon), SeverityWarn, "expires in 3 day(s) on 2026-04-23"},
		{"obtaining → WARN", tlsdomain.NewObtaining(), SeverityWarn, "ACME exchange in progress — rerun in 1-2 minutes"},
		{"failing → FAIL", tlsdomain.NewFailing("dns error"), SeverityFail, "ACME exchange failed: dns error"},
		{"failing empty → FAIL", tlsdomain.NewFailing(""), SeverityFail, "ACME exchange failed"},
		{"unknown → WARN", tlsdomain.NewUnknown(), SeverityWarn, "sidecar not reachable — start 'vibew dev'"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := renderTLSDoctorCheck(tt.state)
			if got.Name != "TLS certificate" {
				t.Errorf("Name = %q, want %q", got.Name, "TLS certificate")
			}
			if got.Severity != tt.wantSeverity {
				t.Errorf("Severity = %v, want %v", got.Severity, tt.wantSeverity)
			}
			if got.Detail != tt.wantDetail {
				t.Errorf("Detail = %q, want %q", got.Detail, tt.wantDetail)
			}
		})
	}
}

// TestRenderTLSDoctorCheck_NoExpiringZeroDaysForSelfSigned locks bug #1078:
// SelfSignedLocal must never produce a "expires 0 day(s)" message.
func TestRenderTLSDoctorCheck_NoExpiringZeroDaysForSelfSigned(t *testing.T) {
	origNow := nowFn
	nowFn = func() time.Time { return time.Date(2026, 4, 20, 0, 0, 0, 0, time.UTC) }
	t.Cleanup(func() { nowFn = origNow })

	got := renderTLSDoctorCheck(tlsdomain.NewSelfSignedLocal())
	if got.Severity != SeverityOK {
		t.Errorf("SelfSignedLocal severity = %v, want OK", got.Severity)
	}
	for _, bad := range []string{"expires", "day(s)", "0 day", "expired"} {
		if contains(got.Detail, bad) {
			t.Errorf("SelfSignedLocal detail %q must not contain %q", got.Detail, bad)
		}
	}
}

func contains(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
