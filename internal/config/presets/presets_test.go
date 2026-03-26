package presets_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/vibewarden/vibewarden/internal/config/presets"
)

func TestResolve_BuiltinPresets(t *testing.T) {
	tests := []struct {
		name       string
		preset     string
		wantSubstr string
	}{
		{
			name:       "email_password preset",
			preset:     presets.PresetEmailPassword,
			wantSubstr: `"email"`,
		},
		{
			name:       "email_only preset",
			preset:     presets.PresetEmailOnly,
			wantSubstr: `"email"`,
		},
		{
			name:       "username_password preset",
			preset:     presets.PresetUsernamePassword,
			wantSubstr: `"username"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := presets.Resolve(tt.preset)
			if err != nil {
				t.Fatalf("Resolve(%q) unexpected error: %v", tt.preset, err)
			}
			if len(data) == 0 {
				t.Errorf("Resolve(%q) returned empty data", tt.preset)
			}
			if !containsString(data, tt.wantSubstr) {
				t.Errorf("Resolve(%q) data does not contain %q", tt.preset, tt.wantSubstr)
			}
		})
	}
}

func TestResolve_CustomPath(t *testing.T) {
	customSchema := `{"$schema":"http://json-schema.org/draft-07/schema#","type":"object"}`
	dir := t.TempDir()
	schemaPath := filepath.Join(dir, "custom.json")
	if err := os.WriteFile(schemaPath, []byte(customSchema), 0600); err != nil {
		t.Fatalf("writing custom schema: %v", err)
	}

	data, err := presets.Resolve(schemaPath)
	if err != nil {
		t.Fatalf("Resolve(%q) unexpected error: %v", schemaPath, err)
	}
	if string(data) != customSchema {
		t.Errorf("Resolve(%q) = %q, want %q", schemaPath, string(data), customSchema)
	}
}

func TestResolve_UnknownPresetName_TreatedAsPath(t *testing.T) {
	_, err := presets.Resolve("nonexistent_preset_name_that_is_not_a_path")
	if err == nil {
		t.Error("Resolve(unknown) expected error, got nil")
	}
}

func TestResolve_EmptyName_ReturnsError(t *testing.T) {
	_, err := presets.Resolve("")
	if err == nil {
		t.Error("Resolve(\"\") expected error, got nil")
	}
}

func TestIsPreset(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"email_password", presets.PresetEmailPassword, true},
		{"email_only", presets.PresetEmailOnly, true},
		{"username_password", presets.PresetUsernamePassword, true},
		{"custom path", "/path/to/schema.json", false},
		{"unknown name", "magic_link", false},
		{"empty", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := presets.IsPreset(tt.input)
			if got != tt.want {
				t.Errorf("IsPreset(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

// containsString reports whether data contains substr.
func containsString(data []byte, substr string) bool {
	for i := 0; i <= len(data)-len(substr); i++ {
		if string(data[i:i+len(substr)]) == substr {
			return true
		}
	}
	return false
}
