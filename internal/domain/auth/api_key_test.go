package auth

import (
	"testing"
)

func TestHashKey(t *testing.T) {
	tests := []struct {
		name string
		key  string
		want string // pre-computed SHA-256 hex
	}{
		{
			name: "known value",
			key:  "hello",
			// echo -n "hello" | sha256sum
			want: "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824",
		},
		{
			name: "empty string",
			key:  "",
			// echo -n "" | sha256sum
			want: "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := HashKey(tt.key)
			if got != tt.want {
				t.Errorf("HashKey(%q) = %q, want %q", tt.key, got, tt.want)
			}
		})
	}
}

func TestHashKey_Deterministic(t *testing.T) {
	// Same input must always produce the same hash.
	key := "vw_test_key_abc123"
	h1 := HashKey(key)
	h2 := HashKey(key)
	if h1 != h2 {
		t.Errorf("HashKey is non-deterministic: %q != %q", h1, h2)
	}
}

func TestHashKey_Distinct(t *testing.T) {
	// Different inputs must produce different hashes.
	h1 := HashKey("keyA")
	h2 := HashKey("keyB")
	if h1 == h2 {
		t.Error("HashKey produced the same hash for distinct keys")
	}
}

func TestAPIKey_Matches(t *testing.T) {
	plaintext := "vw_secret_key"
	k := &APIKey{
		Name:    "test-key",
		KeyHash: HashKey(plaintext),
		Active:  true,
	}

	tests := []struct {
		name      string
		candidate string
		want      bool
	}{
		{"correct key", plaintext, true},
		{"wrong key", "vw_wrong_key", false},
		{"empty string", "", false},
		{"almost right", "vw_secret_ke", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := k.Matches(tt.candidate)
			if got != tt.want {
				t.Errorf("Matches(%q) = %v, want %v", tt.candidate, got, tt.want)
			}
		})
	}
}

func TestAPIKey_Validate(t *testing.T) {
	tests := []struct {
		name    string
		key     APIKey
		wantErr bool
	}{
		{
			name: "valid key",
			key: APIKey{
				Name:    "ci-deploy",
				KeyHash: HashKey("vw_ci_key"),
				Active:  true,
			},
			wantErr: false,
		},
		{
			name: "missing name",
			key: APIKey{
				KeyHash: HashKey("vw_ci_key"),
				Active:  true,
			},
			wantErr: true,
		},
		{
			name: "missing key hash",
			key: APIKey{
				Name:   "ci-deploy",
				Active: true,
			},
			wantErr: true,
		},
		{
			name: "inactive key is still valid entity",
			key: APIKey{
				Name:    "disabled-key",
				KeyHash: HashKey("vw_old_key"),
				Active:  false,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.key.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestAPIKey_Scopes(t *testing.T) {
	// Verify scopes are carried without modification.
	k := APIKey{
		Name:    "scoped-key",
		KeyHash: HashKey("vw_scoped"),
		Scopes:  []Scope{"read:metrics", "write:config"},
		Active:  true,
	}

	if len(k.Scopes) != 2 {
		t.Fatalf("expected 2 scopes, got %d", len(k.Scopes))
	}
	if k.Scopes[0] != "read:metrics" {
		t.Errorf("Scopes[0] = %q, want %q", k.Scopes[0], "read:metrics")
	}
	if k.Scopes[1] != "write:config" {
		t.Errorf("Scopes[1] = %q, want %q", k.Scopes[1], "write:config")
	}
}
