// Package jwks contains domain types for JSON Web Key Sets.
// It has zero external dependencies — only stdlib is permitted here.
package jwks

import "crypto"

// Key represents a single JSON Web Key.
type Key struct {
	// KID is the key identifier.
	KID string

	// Algorithm is the JWA algorithm (e.g. "RS256", "ES256").
	Algorithm string

	// PublicKey is the cryptographic public key.
	PublicKey crypto.PublicKey
}

// KeySet is a collection of JSON Web Keys.
type KeySet struct {
	// Keys is the list of keys in the set.
	Keys []Key
}

// FindByKID returns the first key matching the given key ID, or nil.
func (ks *KeySet) FindByKID(kid string) *Key {
	for i := range ks.Keys {
		if ks.Keys[i].KID == kid {
			return &ks.Keys[i]
		}
	}
	return nil
}
