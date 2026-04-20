package tls_test

import (
	"testing"
	"time"

	tlsdomain "github.com/vibewarden/vibewarden/internal/domain/tls"
)

func TestNewCertInfo(t *testing.T) {
	now := time.Date(2026, 4, 20, 0, 0, 0, 0, time.UTC)
	validBefore := now.Add(-24 * time.Hour)
	validAfter := now.Add(90 * 24 * time.Hour)

	tests := []struct {
		name      string
		subject   string
		issuer    string
		notBefore time.Time
		notAfter  time.Time
		sans      []string
		serial    string
		wantErr   bool
	}{
		{
			name:      "valid cert info",
			subject:   "CN=example.com",
			issuer:    "O=Let's Encrypt, CN=R3",
			notBefore: validBefore,
			notAfter:  validAfter,
			sans:      []string{"example.com", "www.example.com"},
			serial:    "03:A1:B2:C3",
			wantErr:   false,
		},
		{
			name:      "empty subject",
			subject:   "",
			issuer:    "O=Let's Encrypt, CN=R3",
			notBefore: validBefore,
			notAfter:  validAfter,
			sans:      nil,
			serial:    "03:A1",
			wantErr:   true,
		},
		{
			name:      "empty issuer",
			subject:   "CN=example.com",
			issuer:    "",
			notBefore: validBefore,
			notAfter:  validAfter,
			sans:      nil,
			serial:    "03:A1",
			wantErr:   true,
		},
		{
			name:      "notAfter before notBefore",
			subject:   "CN=example.com",
			issuer:    "O=Let's Encrypt, CN=R3",
			notBefore: validAfter,
			notAfter:  validBefore,
			sans:      nil,
			serial:    "03:A1",
			wantErr:   true,
		},
		{
			name:      "notAfter equals notBefore",
			subject:   "CN=example.com",
			issuer:    "O=Let's Encrypt, CN=R3",
			notBefore: validBefore,
			notAfter:  validBefore,
			sans:      nil,
			serial:    "03:A1",
			wantErr:   true,
		},
		{
			name:      "nil sans is valid",
			subject:   "CN=example.com",
			issuer:    "O=Let's Encrypt, CN=R3",
			notBefore: validBefore,
			notAfter:  validAfter,
			sans:      nil,
			serial:    "",
			wantErr:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ci, err := tlsdomain.NewCertInfo(tt.subject, tt.issuer, tt.notBefore, tt.notAfter, tt.sans, tt.serial)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewCertInfo() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr {
				return
			}
			if ci.Subject() != tt.subject {
				t.Errorf("Subject() = %q, want %q", ci.Subject(), tt.subject)
			}
			if ci.Issuer() != tt.issuer {
				t.Errorf("Issuer() = %q, want %q", ci.Issuer(), tt.issuer)
			}
			if !ci.NotBefore().Equal(tt.notBefore) {
				t.Errorf("NotBefore() = %v, want %v", ci.NotBefore(), tt.notBefore)
			}
			if !ci.NotAfter().Equal(tt.notAfter) {
				t.Errorf("NotAfter() = %v, want %v", ci.NotAfter(), tt.notAfter)
			}
			if ci.Serial() != tt.serial {
				t.Errorf("Serial() = %q, want %q", ci.Serial(), tt.serial)
			}
		})
	}
}

func TestCertInfo_SANsDefensiveCopy(t *testing.T) {
	now := time.Date(2026, 4, 20, 0, 0, 0, 0, time.UTC)
	sans := []string{"example.com", "www.example.com"}

	ci, err := tlsdomain.NewCertInfo("CN=example.com", "CN=R3", now.Add(-time.Hour), now.Add(90*24*time.Hour), sans, "03:A1")
	if err != nil {
		t.Fatalf("NewCertInfo() unexpected error: %v", err)
	}

	// Mutate the original slice.
	sans[0] = "mutated.com"
	if ci.SANs()[0] == "mutated.com" {
		t.Error("SANs should be a defensive copy; mutating the input affected the value object")
	}

	// Mutate the returned slice.
	returned := ci.SANs()
	returned[0] = "mutated-again.com"
	if ci.SANs()[0] == "mutated-again.com" {
		t.Error("SANs() should return a copy; mutating the return value affected the value object")
	}
}

func TestCertInfo_IsExpired(t *testing.T) {
	notBefore := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	notAfter := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)

	ci, err := tlsdomain.NewCertInfo("CN=test.com", "CN=CA", notBefore, notAfter, nil, "01")
	if err != nil {
		t.Fatalf("NewCertInfo() unexpected error: %v", err)
	}

	tests := []struct {
		name    string
		now     time.Time
		expired bool
	}{
		{"before expiry", time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC), false},
		{"at expiry", time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC), false},
		{"after expiry", time.Date(2026, 4, 2, 0, 0, 0, 0, time.UTC), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ci.IsExpired(tt.now); got != tt.expired {
				t.Errorf("IsExpired(%v) = %v, want %v", tt.now, got, tt.expired)
			}
		})
	}
}

func TestCertInfo_DaysUntilExpiry(t *testing.T) {
	notBefore := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	notAfter := time.Date(2026, 4, 20, 0, 0, 0, 0, time.UTC)

	ci, err := tlsdomain.NewCertInfo("CN=test.com", "CN=CA", notBefore, notAfter, nil, "01")
	if err != nil {
		t.Fatalf("NewCertInfo() unexpected error: %v", err)
	}

	tests := []struct {
		name     string
		now      time.Time
		wantDays int
	}{
		{"30 days left", time.Date(2026, 3, 21, 0, 0, 0, 0, time.UTC), 30},
		{"1 day left", time.Date(2026, 4, 19, 0, 0, 0, 0, time.UTC), 1},
		{"already expired", time.Date(2026, 4, 21, 0, 0, 0, 0, time.UTC), 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ci.DaysUntilExpiry(tt.now); got != tt.wantDays {
				t.Errorf("DaysUntilExpiry(%v) = %d, want %d", tt.now, got, tt.wantDays)
			}
		})
	}
}

func TestCertInfo_ExpiryStatus(t *testing.T) {
	notBefore := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	notAfter := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)

	ci, err := tlsdomain.NewCertInfo("CN=test.com", "CN=CA", notBefore, notAfter, nil, "01")
	if err != nil {
		t.Fatalf("NewCertInfo() unexpected error: %v", err)
	}

	tests := []struct {
		name         string
		now          time.Time
		wantContains string
	}{
		{"expired", time.Date(2026, 5, 2, 0, 0, 0, 0, time.UTC), "EXPIRED"},
		{"critical 5 days", time.Date(2026, 4, 26, 0, 0, 0, 0, time.UTC), "CRITICAL"},
		{"warning 20 days", time.Date(2026, 4, 11, 0, 0, 0, 0, time.UTC), "WARNING"},
		{"ok 60 days", time.Date(2026, 3, 2, 0, 0, 0, 0, time.UTC), "OK"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ci.ExpiryStatus(tt.now)
			if got == "" {
				t.Error("ExpiryStatus() returned empty string")
			}
			if !contains(got, tt.wantContains) {
				t.Errorf("ExpiryStatus(%v) = %q, want it to contain %q", tt.now, got, tt.wantContains)
			}
		})
	}
}

// contains is a simple substring check helper for tests.
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstr(s, substr))
}

func containsSubstr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
