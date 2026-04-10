// Package auth defines domain types for API key authentication.
// This package has zero external dependencies — only the Go standard library.
package auth

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"time"
)

// Scope represents a named permission scope granted to an API key.
// Scopes are opaque strings; their meaning is defined by the upstream
// application. VibeWarden forwards scope information in the request context
// but does not interpret it.
type Scope string

// APIKey is an entity that represents a named API key registered with
// VibeWarden. The plaintext key is never stored; only the SHA-256 hex digest
// of the key is retained.
//
// APIKey has identity: two keys are the same entity if and only if their
// KeyHash fields are equal.
type APIKey struct {
	// Name is a human-readable label for the key (e.g. "ci-deploy", "mobile-app").
	Name string

	// KeyHash is the hex-encoded SHA-256 digest of the original plaintext key.
	// It is used for constant-time comparison during validation.
	KeyHash string

	// Scopes is the list of permission scopes granted to this key.
	// May be empty, meaning the key has no explicit scope restrictions.
	Scopes []Scope

	// Active indicates whether the key may be used for authentication.
	// Inactive keys are rejected as if they did not exist.
	Active bool

	// CreatedAt is when the key was registered. Zero value means unknown.
	CreatedAt time.Time
}

// HashKey returns the hex-encoded SHA-256 digest of the given plaintext key.
// Use this function to compute KeyHash when registering a new key, and when
// looking up a key presented by a caller.
func HashKey(plaintextKey string) string {
	sum := sha256.Sum256([]byte(plaintextKey))
	return hex.EncodeToString(sum[:])
}

// Matches reports whether the given plaintext key, when hashed, matches the
// stored KeyHash. The comparison is performed in constant time to prevent
// timing-based key extraction.
func (k *APIKey) Matches(plaintextKey string) bool {
	candidateHash := HashKey(plaintextKey)
	// subtle.ConstantTimeCompare requires equal-length slices; both are fixed
	// 64-character lowercase hex strings so lengths are always equal.
	return subtle.ConstantTimeCompare([]byte(k.KeyHash), []byte(candidateHash)) == 1
}

// Validate returns an error if the API key entity is in an inconsistent state.
// This is a domain invariant check — callers should validate before persisting.
func (k *APIKey) Validate() error {
	if k.Name == "" {
		return errors.New("api key name cannot be empty")
	}
	if k.KeyHash == "" {
		return errors.New("api key hash cannot be empty")
	}
	return nil
}
