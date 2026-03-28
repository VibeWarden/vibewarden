// Package apikey provides implementations of the ports.APIKeyValidator interface.
// This file contains the OpenBaoValidator, which reads API key hashes from an
// OpenBao KV path and caches them with a configurable TTL.
package apikey

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/vibewarden/vibewarden/internal/domain/auth"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// KeyStore is the outbound dependency used by OpenBaoValidator to fetch
// key data from a secret store. The map returned by Get must contain entries
// where each key is the API key name and each value is its SHA-256 hex hash.
//
// This interface is satisfied by *openbao.Adapter.Get but is defined locally
// so the adapter package does not import the openbao package directly.
type KeyStore interface {
	// Get returns all fields stored at the given KV path.
	Get(ctx context.Context, path string) (map[string]string, error)
}

// OpenBaoValidator is an implementation of ports.APIKeyValidator that reads
// API key hashes from an OpenBao KV path. Results are cached in memory and
// refreshed after the configured TTL expires.
//
// The KV secret at the configured path must have string fields where each
// field key is the human-readable key name and each field value is the
// hex-encoded SHA-256 hash of the corresponding plaintext API key.
//
// Example KV data at "auth/api-keys":
//
//	{
//	  "ci-deploy": "a4f2...",
//	  "mobile-app": "9b1c..."
//	}
type OpenBaoValidator struct {
	store    KeyStore
	path     string
	cacheTTL time.Duration
	logger   *slog.Logger

	mu          sync.RWMutex
	cachedKeys  []*auth.APIKey
	cacheLoadAt time.Time
}

// NewOpenBaoValidator creates an OpenBaoValidator that reads API key hashes
// from the given OpenBao KV path and caches them for cacheTTL.
// A zero cacheTTL defaults to 5 minutes.
func NewOpenBaoValidator(store KeyStore, path string, cacheTTL time.Duration, logger *slog.Logger) (*OpenBaoValidator, error) {
	if store == nil {
		return nil, fmt.Errorf("openbao api key validator: store cannot be nil")
	}
	if path == "" {
		return nil, fmt.Errorf("openbao api key validator: path cannot be empty")
	}
	if cacheTTL <= 0 {
		cacheTTL = 5 * time.Minute
	}
	return &OpenBaoValidator{
		store:    store,
		path:     path,
		cacheTTL: cacheTTL,
		logger:   logger,
	}, nil
}

// Validate looks up the plaintext key in the cached set of keys fetched from
// OpenBao. The cache is refreshed lazily when it has expired.
// Returns ports.ErrAPIKeyInvalid when the key is not found or the matching
// key is inactive.
func (v *OpenBaoValidator) Validate(ctx context.Context, plaintextKey string) (*auth.APIKey, error) {
	keys, err := v.loadKeys(ctx)
	if err != nil {
		return nil, fmt.Errorf("openbao api key validator: loading keys: %w", err)
	}

	for _, k := range keys {
		if k.Matches(plaintextKey) {
			if !k.Active {
				return nil, ports.ErrAPIKeyInvalid
			}
			return k, nil
		}
	}
	return nil, ports.ErrAPIKeyInvalid
}

// loadKeys returns the cached key list, refreshing it from OpenBao when the
// cache has expired. The refresh is performed under a write lock; concurrent
// callers that find a stale cache will block until the refresh completes.
func (v *OpenBaoValidator) loadKeys(ctx context.Context) ([]*auth.APIKey, error) {
	// Fast path: cache is valid.
	v.mu.RLock()
	if !v.cacheLoadAt.IsZero() && time.Since(v.cacheLoadAt) < v.cacheTTL {
		keys := v.cachedKeys
		v.mu.RUnlock()
		return keys, nil
	}
	v.mu.RUnlock()

	// Slow path: refresh under write lock.
	v.mu.Lock()
	defer v.mu.Unlock()

	// Double-check after acquiring the write lock — another goroutine may have
	// already refreshed the cache while we were waiting.
	if !v.cacheLoadAt.IsZero() && time.Since(v.cacheLoadAt) < v.cacheTTL {
		return v.cachedKeys, nil
	}

	data, err := v.store.Get(ctx, v.path)
	if err != nil {
		// If we have a stale cache, serve it rather than returning an error.
		if len(v.cachedKeys) > 0 {
			v.logger.WarnContext(ctx, "openbao api key validator: refresh failed, serving stale cache",
				slog.String("path", v.path),
				slog.String("error", err.Error()),
			)
			return v.cachedKeys, nil
		}
		return nil, fmt.Errorf("fetching keys from path %q: %w", v.path, err)
	}

	keys := make([]*auth.APIKey, 0, len(data))
	for name, hash := range data {
		k := &auth.APIKey{
			Name:    name,
			KeyHash: hash,
			Active:  true,
		}
		if validateErr := k.Validate(); validateErr != nil {
			v.logger.WarnContext(ctx, "openbao api key validator: skipping invalid entry",
				slog.String("name", name),
				slog.String("error", validateErr.Error()),
			)
			continue
		}
		keys = append(keys, k)
	}

	v.cachedKeys = keys
	v.cacheLoadAt = time.Now()

	v.logger.InfoContext(ctx, "openbao api key validator: cache refreshed",
		slog.String("path", v.path),
		slog.Int("key_count", len(keys)),
	)

	return keys, nil
}
