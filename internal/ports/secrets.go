// Package ports defines the interfaces (ports) for VibeWarden's hexagonal architecture.
package ports

import (
	"context"

	domainsecret "github.com/vibewarden/vibewarden/internal/domain/secret"
)

// SecretStore is the outbound port for reading and writing secrets in an
// external secret store (e.g. OpenBao / HashiCorp Vault KV v2).
//
// Implementations must be safe for concurrent use. All path arguments are
// store-relative (e.g. "app/database") and must not start with a slash.
type SecretStore interface {
	// Get fetches all key/value pairs stored at path. Returns an error when
	// the path does not exist or the store is unreachable.
	Get(ctx context.Context, path string) (map[string]string, error)

	// Put writes data at path, creating or updating the secret version.
	Put(ctx context.Context, path string, data map[string]string) error

	// Delete removes the secret at path (all versions).
	Delete(ctx context.Context, path string) error

	// List returns the keys (child paths) beneath prefix.
	// Keys ending in "/" denote sub-directories.
	List(ctx context.Context, prefix string) ([]string, error)

	// Health performs a live connectivity probe against the secret store.
	// Returns nil when the store is reachable and unsealed.
	Health(ctx context.Context) error
}

// SecretRetriever provides read-only access to secrets from multiple sources.
// It tries OpenBao first, then falls back to the credentials file.
type SecretRetriever interface {
	// Get retrieves a secret by alias or path. Tries OpenBao first, then
	// falls back to the credentials file. Returns ErrSecretNotFound when
	// neither source has the secret.
	Get(ctx context.Context, aliasOrPath string) (*domainsecret.RetrievedSecret, error)

	// List returns all managed secret paths from both sources.
	List(ctx context.Context) ([]string, error)
}
