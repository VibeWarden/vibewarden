// Package ports defines the interfaces (ports) for VibeWarden's hexagonal architecture.
package ports

import (
	"context"

	"github.com/vibewarden/vibewarden/internal/domain/generate"
)

// CredentialGenerator generates cryptographically secure random credentials.
// Implementations must use crypto/rand for all randomness.
type CredentialGenerator interface {
	// Generate creates a new set of random credentials.
	Generate(ctx context.Context) (*generate.GeneratedCredentials, error)
}

// CredentialStore persists and retrieves generated credentials.
// The store is responsible for file permissions and atomic writes.
type CredentialStore interface {
	// Write persists credentials to the backing store. Overwrites any existing data.
	Write(ctx context.Context, creds *generate.GeneratedCredentials, outputDir string) error

	// Read loads previously generated credentials. Returns os.ErrNotExist if none.
	Read(ctx context.Context, outputDir string) (*generate.GeneratedCredentials, error)
}
