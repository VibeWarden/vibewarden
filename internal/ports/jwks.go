// Package ports defines the interfaces (ports) for VibeWarden's hexagonal architecture.
package ports

import (
	"context"

	"github.com/vibewarden/vibewarden/internal/domain/jwks"
)

// JWKSFetcher retrieves JSON Web Key Sets from a remote endpoint.
// Implementations handle HTTP transport, caching, and key rotation.
type JWKSFetcher interface {
	// FetchKeys retrieves the current JWKS from the configured endpoint.
	// The implementation should cache keys and refresh them periodically or
	// when a key is not found (key rotation scenario).
	//
	// Returns the JWKS or an error if the endpoint cannot be reached.
	FetchKeys(ctx context.Context) (*jwks.KeySet, error)

	// GetKey retrieves a specific key by key ID (kid).
	// If the key is not in the cache, the implementation should attempt a
	// refresh before returning an error.
	GetKey(ctx context.Context, kid string) (*jwks.Key, error)
}
