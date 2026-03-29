// Package identity provides domain types for authenticated user identity.
// This package has zero external dependencies — only the Go standard library.
package identity

import (
	"errors"
	"strings"
)

// Identity is a value object representing an authenticated user's identity.
// It is provider-agnostic: the same Identity type is returned whether the user
// authenticated via Kratos session, JWT, API key, or any future mechanism.
//
// Identity is immutable. Create new instances via NewIdentity.
type Identity struct {
	// id is the unique identifier for the user. Format depends on the provider:
	// - Kratos: UUID string
	// - OIDC: "sub" claim value
	// - API key: key name or hash
	id string

	// email is the user's primary email address. May be empty for non-human
	// identities (e.g., service accounts, API keys).
	email string

	// emailVerified indicates whether the email has been verified by the provider.
	emailVerified bool

	// provider identifies which identity provider authenticated this user.
	// Examples: "kratos", "oidc", "jwt", "apikey".
	provider string

	// claims contains additional attributes from the provider. Keys are
	// claim names, values are claim values (typically string, []string, or bool).
	// For Kratos: traits
	// For JWT/OIDC: all claims except reserved ones (sub, iss, aud, exp, iat, nbf)
	// For API keys: scopes as {"scopes": []string{...}}
	claims map[string]any
}

// NewIdentity creates a new Identity with the given attributes.
// Returns an error if required fields are invalid.
func NewIdentity(id, email, provider string, emailVerified bool, claims map[string]any) (Identity, error) {
	if id == "" {
		return Identity{}, errors.New("identity id cannot be empty")
	}
	if provider == "" {
		return Identity{}, errors.New("identity provider cannot be empty")
	}
	// Email validation: if provided, must contain @
	if email != "" && !strings.Contains(email, "@") {
		return Identity{}, errors.New("invalid email format")
	}

	// Defensive copy of claims to ensure immutability.
	claimsCopy := make(map[string]any, len(claims))
	for k, v := range claims {
		claimsCopy[k] = v
	}

	return Identity{
		id:            id,
		email:         email,
		emailVerified: emailVerified,
		provider:      provider,
		claims:        claimsCopy,
	}, nil
}

// ID returns the user's unique identifier.
func (i Identity) ID() string { return i.id }

// Email returns the user's email address. May be empty.
func (i Identity) Email() string { return i.email }

// EmailVerified returns true if the email has been verified.
func (i Identity) EmailVerified() bool { return i.emailVerified }

// Provider returns the name of the identity provider that authenticated this user.
func (i Identity) Provider() string { return i.provider }

// Claims returns a copy of the additional claims map.
// Modifying the returned map does not affect the Identity.
func (i Identity) Claims() map[string]any {
	out := make(map[string]any, len(i.claims))
	for k, v := range i.claims {
		out[k] = v
	}
	return out
}

// Claim returns the value of a specific claim, or nil if not present.
func (i Identity) Claim(name string) any {
	return i.claims[name]
}

// HasClaim reports whether the identity has the named claim.
func (i Identity) HasClaim(name string) bool {
	_, ok := i.claims[name]
	return ok
}

// IsZero reports whether this is the zero value (no identity).
func (i Identity) IsZero() bool {
	return i.id == ""
}
