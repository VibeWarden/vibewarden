package tls_test

import (
	"testing"
	"time"

	tlsdomain "github.com/vibewarden/vibewarden/internal/domain/tls"
)

func TestTLSState_Constructors(t *testing.T) {
	expires := time.Date(2026, 7, 21, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name       string
		state      tlsdomain.State
		wantKind   tlsdomain.Kind
		wantExpiry time.Time
		wantDays   int
		wantErr    string
	}{
		{"disabled", tlsdomain.NewDisabled(), tlsdomain.KindDisabled, time.Time{}, 0, ""},
		{"self-signed local", tlsdomain.NewSelfSignedLocal(), tlsdomain.KindSelfSignedLocal, time.Time{}, 0, ""},
		{"obtaining", tlsdomain.NewObtaining(), tlsdomain.KindObtaining, time.Time{}, 0, ""},
		{"obtained", tlsdomain.NewObtained(expires), tlsdomain.KindObtained, expires, 0, ""},
		{"expiring soon", tlsdomain.NewExpiringSoon(5, expires), tlsdomain.KindExpiringSoon, expires, 5, ""},
		{"expiring soon negative clamped", tlsdomain.NewExpiringSoon(-3, expires), tlsdomain.KindExpiringSoon, expires, 0, ""},
		{"failing", tlsdomain.NewFailing("connection refused"), tlsdomain.KindFailing, time.Time{}, 0, "connection refused"},
		{"unknown", tlsdomain.NewUnknown(), tlsdomain.KindUnknown, time.Time{}, 0, ""},
		{"zero value is unknown", tlsdomain.State{}, tlsdomain.KindUnknown, time.Time{}, 0, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.state.Kind(); got != tt.wantKind {
				t.Errorf("Kind() = %v, want %v", got, tt.wantKind)
			}
			if got := tt.state.ExpiresAt(); !got.Equal(tt.wantExpiry) {
				t.Errorf("ExpiresAt() = %v, want %v", got, tt.wantExpiry)
			}
			if got := tt.state.DaysLeft(); got != tt.wantDays {
				t.Errorf("DaysLeft() = %d, want %d", got, tt.wantDays)
			}
			if got := tt.state.LastError(); got != tt.wantErr {
				t.Errorf("LastError() = %q, want %q", got, tt.wantErr)
			}
		})
	}
}

func TestTLSState_String(t *testing.T) {
	expires := time.Date(2026, 7, 21, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name  string
		state tlsdomain.State
		want  string
	}{
		{"disabled", tlsdomain.NewDisabled(), "TLS disabled"},
		{"self-signed", tlsdomain.NewSelfSignedLocal(), "TLS self-signed dev cert (rotates automatically)"},
		{"obtaining", tlsdomain.NewObtaining(), "TLS obtaining (ACME in progress)"},
		{"obtained", tlsdomain.NewObtained(expires), "TLS obtained (expires 2026-07-21)"},
		{"expiring soon", tlsdomain.NewExpiringSoon(3, expires), "TLS near expiry (expires in 3 days)"},
		{"failing with error", tlsdomain.NewFailing("connection refused"), "TLS failing (last error: connection refused)"},
		{"failing empty error", tlsdomain.NewFailing(""), "TLS failing"},
		{"unknown", tlsdomain.NewUnknown(), "TLS state unavailable"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.state.String(); got != tt.want {
				t.Errorf("String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestTLSState_Healthy(t *testing.T) {
	expires := time.Now().Add(90 * 24 * time.Hour)
	tests := []struct {
		name    string
		state   tlsdomain.State
		healthy bool
	}{
		{"disabled healthy", tlsdomain.NewDisabled(), true},
		{"self-signed healthy", tlsdomain.NewSelfSignedLocal(), true},
		{"obtained healthy", tlsdomain.NewObtained(expires), true},
		{"obtaining not healthy", tlsdomain.NewObtaining(), false},
		{"expiring not healthy", tlsdomain.NewExpiringSoon(3, expires), false},
		{"failing not healthy", tlsdomain.NewFailing("x"), false},
		{"unknown not healthy", tlsdomain.NewUnknown(), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.state.Healthy(); got != tt.healthy {
				t.Errorf("Healthy() = %v, want %v", got, tt.healthy)
			}
		})
	}
}

func TestKind_String(t *testing.T) {
	tests := []struct {
		kind tlsdomain.Kind
		want string
	}{
		{tlsdomain.KindUnknown, "unknown"},
		{tlsdomain.KindDisabled, "disabled"},
		{tlsdomain.KindSelfSignedLocal, "self-signed-local"},
		{tlsdomain.KindObtaining, "obtaining"},
		{tlsdomain.KindObtained, "obtained"},
		{tlsdomain.KindExpiringSoon, "expiring-soon"},
		{tlsdomain.KindFailing, "failing"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := tt.kind.String(); got != tt.want {
				t.Errorf("Kind(%d).String() = %q, want %q", int(tt.kind), got, tt.want)
			}
		})
	}
}
