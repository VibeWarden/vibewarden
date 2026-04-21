package ops

import (
	"testing"
	"time"

	tlsdomain "github.com/vibewarden/vibewarden/internal/domain/tls"
)

func TestRenderTLSStatusLine(t *testing.T) {
	expires := time.Date(2026, 7, 21, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name        string
		state       tlsdomain.State
		wantDetail  string
		wantHealthy bool
	}{
		{"disabled", tlsdomain.NewDisabled(), "disabled", true},
		{"self-signed", tlsdomain.NewSelfSignedLocal(), "self-signed dev cert (rotates automatically)", true},
		{"obtaining", tlsdomain.NewObtaining(), "obtaining (ACME in progress)", true},
		{"obtained", tlsdomain.NewObtained(expires), "obtained (expires 2026-07-21)", true},
		{"expiring soon", tlsdomain.NewExpiringSoon(3, expires), "near expiry (expires in 3 days)", false},
		{"failing with error", tlsdomain.NewFailing("connection refused"), "failing (last error: connection refused)", false},
		{"failing empty error", tlsdomain.NewFailing(""), "failing", false},
		{"unknown", tlsdomain.NewUnknown(), "state unavailable — start 'vibew dev'", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			detail, healthy := renderTLSStatusLine(tt.state)
			if detail != tt.wantDetail {
				t.Errorf("detail = %q, want %q", detail, tt.wantDetail)
			}
			if healthy != tt.wantHealthy {
				t.Errorf("healthy = %v, want %v", healthy, tt.wantHealthy)
			}
		})
	}
}
