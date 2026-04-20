package secret_test

import (
	"testing"

	"github.com/vibewarden/vibewarden/internal/domain/secret"
)

func TestParseURI(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantPath string
		wantKey  string
		wantErr  bool
	}{
		{
			name:     "valid two-segment path",
			input:    "secret://auth/google/client_id",
			wantPath: "auth/google",
			wantKey:  "client_id",
		},
		{
			name:     "valid single-segment path",
			input:    "secret://database/password",
			wantPath: "database",
			wantKey:  "password",
		},
		{
			name:     "valid deep path",
			input:    "secret://a/b/c/d/key",
			wantPath: "a/b/c/d",
			wantKey:  "key",
		},
		{
			name:    "missing scheme",
			input:   "auth/google/client_id",
			wantErr: true,
		},
		{
			name:    "wrong scheme",
			input:   "vault://auth/google/client_id",
			wantErr: true,
		},
		{
			name:    "empty after scheme",
			input:   "secret://",
			wantErr: true,
		},
		{
			name:    "no slash separator (key only)",
			input:   "secret://onlykeynoseparator",
			wantErr: true,
		},
		{
			name:    "empty path",
			input:   "secret:///key",
			wantErr: true,
		},
		{
			name:    "empty key (trailing slash)",
			input:   "secret://auth/google/",
			wantErr: true,
		},
		{
			name:    "empty string",
			input:   "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			uri, err := secret.ParseURI(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ParseSecretURI(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if uri.Path() != tt.wantPath {
				t.Errorf("Path() = %q, want %q", uri.Path(), tt.wantPath)
			}
			if uri.Key() != tt.wantKey {
				t.Errorf("Key() = %q, want %q", uri.Key(), tt.wantKey)
			}
		})
	}
}

func TestURI_String(t *testing.T) {
	uri, err := secret.ParseURI("secret://auth/google/client_id")
	if err != nil {
		t.Fatalf("ParseSecretURI failed: %v", err)
	}

	got := uri.String()
	want := "secret://auth/google/client_id"
	if got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
}

func TestIsURI(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"valid secret URI", "secret://auth/google/client_id", true},
		{"secret scheme only", "secret://", true},
		{"plain string", "just-a-string", false},
		{"env var", "${VIBEWARDEN_FLEET_KEY}", false},
		{"empty", "", false},
		{"http URL", "http://example.com", false},
		{"partial match", "secret:/", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := secret.IsURI(tt.input)
			if got != tt.want {
				t.Errorf("IsSecretURI(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}
