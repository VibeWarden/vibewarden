package identity

import (
	"testing"
)

func TestNewIdentity_ValidInputs(t *testing.T) {
	tests := []struct {
		name          string
		id            string
		email         string
		provider      string
		emailVerified bool
		claims        map[string]any
	}{
		{
			name:          "kratos identity with email",
			id:            "user-abc-123",
			email:         "alice@example.com",
			provider:      "kratos",
			emailVerified: true,
			claims:        map[string]any{"role": "admin"},
		},
		{
			name:     "identity without email (service account)",
			id:       "svc-001",
			email:    "",
			provider: "apikey",
			claims:   nil,
		},
		{
			name:     "identity with nil claims",
			id:       "user-xyz",
			email:    "bob@example.com",
			provider: "oidc",
			claims:   nil,
		},
		{
			name:     "identity with empty claims map",
			id:       "user-xyz",
			email:    "bob@example.com",
			provider: "oidc",
			claims:   map[string]any{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NewIdentity(tt.id, tt.email, tt.provider, tt.emailVerified, tt.claims)
			if err != nil {
				t.Fatalf("NewIdentity() unexpected error: %v", err)
			}
			if got.ID() != tt.id {
				t.Errorf("ID() = %q, want %q", got.ID(), tt.id)
			}
			if got.Email() != tt.email {
				t.Errorf("Email() = %q, want %q", got.Email(), tt.email)
			}
			if got.Provider() != tt.provider {
				t.Errorf("Provider() = %q, want %q", got.Provider(), tt.provider)
			}
			if got.EmailVerified() != tt.emailVerified {
				t.Errorf("EmailVerified() = %v, want %v", got.EmailVerified(), tt.emailVerified)
			}
		})
	}
}

func TestNewIdentity_ValidationErrors(t *testing.T) {
	tests := []struct {
		name     string
		id       string
		email    string
		provider string
	}{
		{
			name:     "empty id",
			id:       "",
			email:    "user@example.com",
			provider: "kratos",
		},
		{
			name:     "empty provider",
			id:       "user-123",
			email:    "user@example.com",
			provider: "",
		},
		{
			name:     "invalid email missing @",
			id:       "user-123",
			email:    "notanemail",
			provider: "kratos",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewIdentity(tt.id, tt.email, tt.provider, false, nil)
			if err == nil {
				t.Errorf("NewIdentity() expected error, got nil")
			}
		})
	}
}

func TestIdentity_ClaimsImmutability(t *testing.T) {
	original := map[string]any{"role": "user", "scope": "read"}
	ident, err := NewIdentity("id-1", "x@example.com", "kratos", false, original)
	if err != nil {
		t.Fatalf("NewIdentity() unexpected error: %v", err)
	}

	// Mutating the original map must not affect the stored claims.
	original["role"] = "superadmin"
	original["new_key"] = "new_value"

	if ident.Claim("role") != "user" {
		t.Errorf("Claim(role) = %v, want %q after mutating original map", ident.Claim("role"), "user")
	}
	if ident.HasClaim("new_key") {
		t.Error("HasClaim(new_key) = true, expected false — original map mutation leaked into identity")
	}
}

func TestIdentity_ClaimsCopyImmutability(t *testing.T) {
	ident, err := NewIdentity("id-1", "x@example.com", "kratos", false, map[string]any{"k": "v"})
	if err != nil {
		t.Fatalf("NewIdentity() unexpected error: %v", err)
	}

	// Mutating the returned Claims map must not affect the identity.
	c := ident.Claims()
	c["k"] = "mutated"
	c["extra"] = "injected"

	if ident.Claim("k") != "v" {
		t.Errorf("Claim(k) = %v after mutating returned Claims map, want %q", ident.Claim("k"), "v")
	}
	if ident.HasClaim("extra") {
		t.Error("HasClaim(extra) = true after mutating returned Claims map, expected false")
	}
}

func TestIdentity_IsZero(t *testing.T) {
	var zero Identity
	if !zero.IsZero() {
		t.Error("zero Identity.IsZero() = false, want true")
	}

	ident, _ := NewIdentity("id-1", "", "kratos", false, nil)
	if ident.IsZero() {
		t.Error("non-zero Identity.IsZero() = true, want false")
	}
}

func TestIdentity_HasClaim(t *testing.T) {
	ident, _ := NewIdentity("id-1", "a@b.com", "kratos", false, map[string]any{
		"role": "admin",
	})

	if !ident.HasClaim("role") {
		t.Error("HasClaim(role) = false, want true")
	}
	if ident.HasClaim("nonexistent") {
		t.Error("HasClaim(nonexistent) = true, want false")
	}
}

func TestIdentity_ClaimReturnsNilForMissing(t *testing.T) {
	ident, _ := NewIdentity("id-1", "a@b.com", "kratos", false, nil)
	if got := ident.Claim("missing"); got != nil {
		t.Errorf("Claim(missing) = %v, want nil", got)
	}
}
