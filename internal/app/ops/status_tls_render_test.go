package ops

import (
	"testing"
	"time"

	tlsdomain "github.com/vibewarden/vibewarden/internal/domain/tls"
)

func TestRenderTLSStatusLine(t *testing.T) {
	expires := time.Date(2026, 7, 21, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name       string
		state      tlsdomain.State
		wantDetail string
		wantStatus StatusState
	}{
		{"disabled", tlsdomain.NewDisabled(), "disabled", StatusOK},
		{"self-signed", tlsdomain.NewSelfSignedLocal(), "self-signed (dev)", StatusOK},
		{"obtaining", tlsdomain.NewObtaining(), "obtaining (ACME in progress)", StatusOK},
		{"obtained", tlsdomain.NewObtained(expires), "obtained (expires 2026-07-21)", StatusOK},
		// KindExpiringSoon must be StatusOK — near-expiry is an annotation, not a failure (ADR-095).
		{"expiring soon", tlsdomain.NewExpiringSoon(3, expires), "obtained (expires in 3 days)", StatusOK},
		// Self-signed case: verify no expiry alarm even with 0 days (would fire in old code).
		{"expiring soon 0 days", tlsdomain.NewExpiringSoon(0, expires), "obtained (expires in 0 days)", StatusOK},
		{"failing with error", tlsdomain.NewFailing("connection refused"), "failing (last error: connection refused)", StatusFAIL},
		{"failing empty error", tlsdomain.NewFailing(""), "failing", StatusFAIL},
		{"unknown", tlsdomain.NewUnknown(), "state unavailable — start 'vibew dev'", StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			detail, status := renderTLSStatusLine(tt.state)
			if detail != tt.wantDetail {
				t.Errorf("detail = %q, want %q", detail, tt.wantDetail)
			}
			if status != tt.wantStatus {
				t.Errorf("status = %v, want %v", status, tt.wantStatus)
			}
		})
	}
}
