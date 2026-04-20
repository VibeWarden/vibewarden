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

func TestContainsPlaceholder(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{
			name:  "single placeholder",
			input: "postgres://user:${secret://db/password}@host:5432/db",
			want:  true,
		},
		{
			name:  "multiple placeholders",
			input: "${secret://db/user}:${secret://db/password}",
			want:  true,
		},
		{
			name:  "placeholder at start",
			input: "${secret://auth/token}-suffix",
			want:  true,
		},
		{
			name:  "placeholder at end",
			input: "prefix-${secret://auth/token}",
			want:  true,
		},
		{
			name:  "no placeholder plain string",
			input: "just a plain string",
			want:  false,
		},
		{
			name:  "full-field secret URI not a placeholder",
			input: "secret://db/password",
			want:  false,
		},
		{
			name:  "escaped placeholder",
			input: "literal $${secret://db/password} text",
			want:  false,
		},
		{
			name:  "env var syntax no collision",
			input: "${SOME_ENV_VAR}",
			want:  false,
		},
		{
			name:  "empty string",
			input: "",
			want:  false,
		},
		{
			name:  "malformed URI inside placeholder",
			input: "${secret://nokeyseparator}",
			want:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := secret.ContainsPlaceholder(tt.input)
			if got != tt.want {
				t.Errorf("ContainsPlaceholder(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestFindPlaceholders(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantLen  int
		wantRaws []string
		wantURIs []string
	}{
		{
			name:     "single placeholder in composite string",
			input:    "postgres://user:${secret://db/password}@host:5432/db",
			wantLen:  1,
			wantRaws: []string{"${secret://db/password}"},
			wantURIs: []string{"secret://db/password"},
		},
		{
			name:     "two placeholders",
			input:    "${secret://db/user}:${secret://db/password}",
			wantLen:  2,
			wantRaws: []string{"${secret://db/user}", "${secret://db/password}"},
			wantURIs: []string{"secret://db/user", "secret://db/password"},
		},
		{
			name:     "placeholder with deep path",
			input:    "Bearer ${secret://auth/google/api/token}",
			wantLen:  1,
			wantRaws: []string{"${secret://auth/google/api/token}"},
			wantURIs: []string{"secret://auth/google/api/token"},
		},
		{
			name:    "no placeholders",
			input:   "just a string with no secrets",
			wantLen: 0,
		},
		{
			name:    "escaped placeholder excluded",
			input:   "$${secret://db/password}",
			wantLen: 0,
		},
		{
			name:     "mix of escaped and unescaped",
			input:    "${secret://db/user} and $${secret://db/password}",
			wantLen:  1,
			wantRaws: []string{"${secret://db/user}"},
			wantURIs: []string{"secret://db/user"},
		},
		{
			name:    "empty string",
			input:   "",
			wantLen: 0,
		},
		{
			name:    "malformed URI in placeholder skipped",
			input:   "${secret://nokeyseparator}",
			wantLen: 0,
		},
		{
			name:    "full-field URI not a placeholder",
			input:   "secret://db/password",
			wantLen: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			placeholders := secret.FindPlaceholders(tt.input)
			if len(placeholders) != tt.wantLen {
				t.Fatalf("FindPlaceholders(%q) returned %d placeholders, want %d", tt.input, len(placeholders), tt.wantLen)
			}
			for i, p := range placeholders {
				if i < len(tt.wantRaws) && p.Raw != tt.wantRaws[i] {
					t.Errorf("placeholder[%d].Raw = %q, want %q", i, p.Raw, tt.wantRaws[i])
				}
				if i < len(tt.wantURIs) && p.URI.String() != tt.wantURIs[i] {
					t.Errorf("placeholder[%d].URI = %q, want %q", i, p.URI.String(), tt.wantURIs[i])
				}
			}
		})
	}
}

func TestUnescapePlaceholders(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "single escaped placeholder",
			input: "$${secret://db/password}",
			want:  "${secret://db/password}",
		},
		{
			name:  "multiple escaped placeholders",
			input: "$${secret://db/user}:$${secret://db/password}",
			want:  "${secret://db/user}:${secret://db/password}",
		},
		{
			name:  "no escaped placeholders",
			input: "just a plain string",
			want:  "just a plain string",
		},
		{
			name:  "unescaped placeholder unchanged",
			input: "${secret://db/password}",
			want:  "${secret://db/password}",
		},
		{
			name:  "mix of escaped and unescaped",
			input: "${secret://db/user} and $${secret://db/password}",
			want:  "${secret://db/user} and ${secret://db/password}",
		},
		{
			name:  "empty string",
			input: "",
			want:  "",
		},
		{
			name:  "escaped with surrounding text",
			input: "literal $${secret://auth/token} text here",
			want:  "literal ${secret://auth/token} text here",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := secret.UnescapePlaceholders(tt.input)
			if got != tt.want {
				t.Errorf("UnescapePlaceholders(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
