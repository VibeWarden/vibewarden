package apikey

import (
	"context"
	"testing"

	"github.com/vibewarden/vibewarden/internal/config"
	"github.com/vibewarden/vibewarden/internal/domain/auth"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// validEntries returns a slice of well-formed APIKeyEntry values for use in
// tests. The plaintext keys are "key-alpha" and "key-beta".
func validEntries() []config.APIKeyEntry {
	return []config.APIKeyEntry{
		{
			Name:   "alpha",
			Hash:   auth.HashKey("key-alpha"),
			Scopes: []string{"read:metrics"},
		},
		{
			Name:   "beta",
			Hash:   auth.HashKey("key-beta"),
			Scopes: []string{"write:config", "read:metrics"},
		},
	}
}

func TestNewConfigValidator_Valid(t *testing.T) {
	v, err := NewConfigValidator(validEntries())
	if err != nil {
		t.Fatalf("NewConfigValidator returned unexpected error: %v", err)
	}
	if v == nil {
		t.Fatal("NewConfigValidator returned nil validator")
	}
}

func TestNewConfigValidator_EmptySlice(t *testing.T) {
	v, err := NewConfigValidator(nil)
	if err != nil {
		t.Fatalf("NewConfigValidator(nil) returned unexpected error: %v", err)
	}
	if v == nil {
		t.Fatal("expected non-nil validator for empty config")
	}
}

func TestNewConfigValidator_InvalidEntries(t *testing.T) {
	tests := []struct {
		name    string
		entries []config.APIKeyEntry
	}{
		{
			name: "missing name",
			entries: []config.APIKeyEntry{
				{Hash: auth.HashKey("some-key")},
			},
		},
		{
			name: "missing hash",
			entries: []config.APIKeyEntry{
				{Name: "no-hash"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewConfigValidator(tt.entries)
			if err == nil {
				t.Error("expected error for invalid entry, got nil")
			}
		})
	}
}

func TestConfigValidator_Validate_Success(t *testing.T) {
	v, err := NewConfigValidator(validEntries())
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	tests := []struct {
		name         string
		plaintextKey string
		wantKeyName  string
		wantScopes   []auth.Scope
	}{
		{
			name:         "alpha key",
			plaintextKey: "key-alpha",
			wantKeyName:  "alpha",
			wantScopes:   []auth.Scope{"read:metrics"},
		},
		{
			name:         "beta key",
			plaintextKey: "key-beta",
			wantKeyName:  "beta",
			wantScopes:   []auth.Scope{"write:config", "read:metrics"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := v.Validate(context.Background(), tt.plaintextKey)
			if err != nil {
				t.Fatalf("Validate(%q) unexpected error: %v", tt.plaintextKey, err)
			}
			if got == nil {
				t.Fatal("Validate returned nil key")
			}
			if got.Name != tt.wantKeyName {
				t.Errorf("Name = %q, want %q", got.Name, tt.wantKeyName)
			}
			if len(got.Scopes) != len(tt.wantScopes) {
				t.Fatalf("Scopes len = %d, want %d", len(got.Scopes), len(tt.wantScopes))
			}
			for i, s := range tt.wantScopes {
				if got.Scopes[i] != s {
					t.Errorf("Scopes[%d] = %q, want %q", i, got.Scopes[i], s)
				}
			}
		})
	}
}

func TestConfigValidator_Validate_InvalidKey(t *testing.T) {
	v, err := NewConfigValidator(validEntries())
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	tests := []struct {
		name string
		key  string
	}{
		{"unknown key", "not-a-registered-key"},
		{"empty key", ""},
		{"partial match", "key-alph"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := v.Validate(context.Background(), tt.key)
			if err == nil {
				t.Errorf("Validate(%q) expected error, got nil", tt.key)
			}
			if err != ports.ErrAPIKeyInvalid {
				t.Errorf("Validate(%q) error = %v, want %v", tt.key, err, ports.ErrAPIKeyInvalid)
			}
		})
	}
}

func TestConfigValidator_Validate_EmptyStore(t *testing.T) {
	v, err := NewConfigValidator(nil)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	_, err = v.Validate(context.Background(), "any-key")
	if err == nil {
		t.Error("expected ErrAPIKeyInvalid when store is empty, got nil")
	}
}
