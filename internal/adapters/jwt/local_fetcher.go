package jwt

import (
	"context"
	"crypto/rsa"

	domjwks "github.com/vibewarden/vibewarden/internal/domain/jwks"
)

// LocalJWKSFetcher implements ports.JWKSFetcher for the local dev JWKS mode.
// It serves the public key directly from memory — no HTTP round-trip is needed
// because the key pair is owned by the same process.
type LocalJWKSFetcher struct {
	keySet *domjwks.KeySet
}

// NewLocalJWKSFetcher creates a LocalJWKSFetcher that returns a single-entry
// KeySet containing the public part of key, identified by DevKID.
func NewLocalJWKSFetcher(key *rsa.PublicKey) *LocalJWKSFetcher {
	ks := &domjwks.KeySet{
		Keys: []domjwks.Key{
			{
				KID:       DevKID,
				Algorithm: "RS256",
				PublicKey: key,
			},
		},
	}
	return &LocalJWKSFetcher{keySet: ks}
}

// FetchKeys implements ports.JWKSFetcher.
// It always returns the in-memory key set without any I/O.
func (f *LocalJWKSFetcher) FetchKeys(_ context.Context) (*domjwks.KeySet, error) {
	return f.keySet, nil
}

// GetKey implements ports.JWKSFetcher.
// It looks up kid in the in-memory key set and returns the matching key
// or an error if not found.
func (f *LocalJWKSFetcher) GetKey(_ context.Context, kid string) (*domjwks.Key, error) {
	key := f.keySet.FindByKID(kid)
	if key == nil {
		// Use the same error prefix as HTTPJWKSFetcher so that
		// isKeyNotFoundError detects it correctly.
		return nil, makeKeyNotFoundError(kid)
	}
	return key, nil
}

// makeKeyNotFoundError returns the sentinel error for a missing key ID.
// The format must match the prefix checked by isKeyNotFoundError in adapter.go.
func makeKeyNotFoundError(kid string) error {
	// Use fmt-style formatting so there are no extra imports needed.
	// The prefix "jwks: key not found:" must match isKeyNotFoundError.
	return &keyNotFoundError{kid: kid}
}

type keyNotFoundError struct{ kid string }

func (e *keyNotFoundError) Error() string {
	return "jwks: key not found: " + e.kid
}
