package jwks_test

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"testing"

	"github.com/vibewarden/vibewarden/internal/domain/jwks"
)

// newTestKey is a helper that generates a throwaway ECDSA public key for tests
// that require a non-nil crypto.PublicKey.
func newTestKey(t *testing.T) *ecdsa.PublicKey {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generating test key: %v", err)
	}
	return &priv.PublicKey
}

func TestKeySet_FindByKID(t *testing.T) {
	t.Parallel()

	pub1 := newTestKey(t)
	pub2 := newTestKey(t)
	pub3 := newTestKey(t)

	tests := []struct {
		name    string
		keySet  jwks.KeySet
		kid     string
		wantNil bool
		wantKID string
	}{
		{
			name: "key found",
			keySet: jwks.KeySet{
				Keys: []jwks.Key{
					{KID: "key-1", Algorithm: "ES256", PublicKey: pub1},
				},
			},
			kid:     "key-1",
			wantNil: false,
			wantKID: "key-1",
		},
		{
			name: "key not found",
			keySet: jwks.KeySet{
				Keys: []jwks.Key{
					{KID: "key-1", Algorithm: "ES256", PublicKey: pub1},
				},
			},
			kid:     "key-999",
			wantNil: true,
		},
		{
			name:    "empty key set",
			keySet:  jwks.KeySet{},
			kid:     "key-1",
			wantNil: true,
		},
		{
			name: "multiple keys, correct one returned",
			keySet: jwks.KeySet{
				Keys: []jwks.Key{
					{KID: "key-1", Algorithm: "ES256", PublicKey: pub1},
					{KID: "key-2", Algorithm: "RS256", PublicKey: pub2},
					{KID: "key-3", Algorithm: "ES256", PublicKey: pub3},
				},
			},
			kid:     "key-2",
			wantNil: false,
			wantKID: "key-2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := tt.keySet.FindByKID(tt.kid)

			if tt.wantNil {
				if got != nil {
					t.Errorf("FindByKID(%q) = %+v, want nil", tt.kid, got)
				}
				return
			}

			if got == nil {
				t.Fatalf("FindByKID(%q) = nil, want key with KID %q", tt.kid, tt.wantKID)
			}
			if got.KID != tt.wantKID {
				t.Errorf("FindByKID(%q).KID = %q, want %q", tt.kid, got.KID, tt.wantKID)
			}
		})
	}
}
