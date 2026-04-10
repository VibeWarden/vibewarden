package http_test

import (
	"errors"
	"testing"

	vibehttp "github.com/vibewarden/vibewarden/internal/adapters/http"
	"github.com/vibewarden/vibewarden/internal/ports"
)

func TestValidateEmail(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantErr   bool
		wantEmail string
	}{
		{"simple valid", "user@example.com", false, "user@example.com"},
		{"with display name", "Alice <alice@example.com>", false, "alice@example.com"},
		{"subdomain", "user@mail.example.org", false, "user@mail.example.org"},
		{"empty string", "", true, ""},
		{"missing at", "notanemail", true, ""},
		{"missing domain", "user@", true, ""},
		{"only at", "@example.com", true, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := vibehttp.ValidateEmail(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ValidateEmail(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
			if tt.wantErr {
				if !errors.Is(err, ports.ErrInvalidEmail) {
					t.Errorf("ValidateEmail(%q) error = %v, want ErrInvalidEmail", tt.input, err)
				}
				return
			}
			if got != tt.wantEmail {
				t.Errorf("ValidateEmail(%q) = %q, want %q", tt.input, got, tt.wantEmail)
			}
		})
	}
}

func TestValidateUUID(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"valid lower", "550e8400-e29b-41d4-a716-446655440000", false},
		{"valid upper", "550E8400-E29B-41D4-A716-446655440000", false},
		{"nil UUID", "00000000-0000-0000-0000-000000000000", false},
		{"empty string", "", true},
		{"too short", "550e8400", true},
		{"not hex", "gggggggg-gggg-gggg-gggg-gggggggggggg", true},
		{"random string", "not-a-uuid-at-all", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := vibehttp.ValidateUUID(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ValidateUUID(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
			if tt.wantErr && err != nil {
				if !errors.Is(err, ports.ErrInvalidUUID) {
					t.Errorf("ValidateUUID(%q) error = %v, want ErrInvalidUUID", tt.input, err)
				}
			}
		})
	}
}
