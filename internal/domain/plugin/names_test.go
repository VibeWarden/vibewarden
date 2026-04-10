package plugin_test

import (
	"testing"

	"github.com/vibewarden/vibewarden/internal/domain/plugin"
)

func TestNewName(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{
			name:    "valid name",
			input:   "tls",
			want:    "tls",
			wantErr: false,
		},
		{
			name:    "valid hyphenated name",
			input:   "rate-limiting",
			want:    "rate-limiting",
			wantErr: false,
		},
		{
			name:    "empty name returns error",
			input:   "",
			want:    "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := plugin.NewName(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewName(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}
			if !tt.wantErr && got.String() != tt.want {
				t.Errorf("NewName(%q).String() = %q, want %q", tt.input, got.String(), tt.want)
			}
		})
	}
}

func TestName_String(t *testing.T) {
	n, err := plugin.NewName("fleet")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n.String() != "fleet" {
		t.Errorf("String() = %q, want %q", n.String(), "fleet")
	}
}

func TestNameConstants(t *testing.T) {
	// Ensure the predefined constants are non-empty strings — they are used
	// as map keys and must never be blank.
	constants := []string{
		plugin.NameTLS,
		plugin.NameUserManagement,
		plugin.NameRateLimiting,
		plugin.NameGrafana,
		plugin.NameFleet,
	}
	for _, c := range constants {
		if c == "" {
			t.Errorf("plugin name constant must not be empty")
		}
	}
}
