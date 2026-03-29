package jwt

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/go-jose/go-jose/v4"
)

// maxJWKSBodySize is the maximum number of bytes read from a JWKS response body.
// Protects against excessively large responses.
const maxJWKSBodySize = 1 << 20 // 1 MiB

// HTTPJWKSFetcher implements ports.JWKSFetcher using HTTP.
// It fetches JSON Web Key Sets from a remote URL and caches them in memory.
// Cache refreshes are serialised to prevent thundering-herd on key rotation.
type HTTPJWKSFetcher struct {
	jwksURL  string
	client   *http.Client
	logger   *slog.Logger
	cacheTTL time.Duration

	mu        sync.RWMutex
	cache     *jose.JSONWebKeySet
	cachedAt  time.Time
	refreshMu sync.Mutex // serialises concurrent refresh requests
}

// NewHTTPJWKSFetcher creates a new HTTPJWKSFetcher for the given JWKS URL.
//
// If timeout is zero, it defaults to 10 seconds.
// If cacheTTL is zero, it defaults to 1 hour.
func NewHTTPJWKSFetcher(jwksURL string, timeout, cacheTTL time.Duration, logger *slog.Logger) *HTTPJWKSFetcher {
	if timeout == 0 {
		timeout = 10 * time.Second
	}
	if cacheTTL == 0 {
		cacheTTL = time.Hour
	}
	return &HTTPJWKSFetcher{
		jwksURL:  jwksURL,
		client:   &http.Client{Timeout: timeout},
		logger:   logger,
		cacheTTL: cacheTTL,
	}
}

// FetchKeys implements ports.JWKSFetcher.
// It returns the cached key set if it is still within the TTL, otherwise it
// fetches a fresh copy from the remote endpoint.
func (f *HTTPJWKSFetcher) FetchKeys(ctx context.Context) (*jose.JSONWebKeySet, error) {
	f.mu.RLock()
	if f.cache != nil && time.Since(f.cachedAt) < f.cacheTTL {
		jwks := f.cache
		f.mu.RUnlock()
		return jwks, nil
	}
	f.mu.RUnlock()

	return f.refresh(ctx)
}

// GetKey implements ports.JWKSFetcher.
// It returns the key with the given key ID from the cache, refreshing if the
// key is not found (supports transparent key rotation).
//
// On a key-miss the implementation forces a cache-bypassing fetch even if the
// cached JWKS is within its TTL. This is necessary to handle key rotation
// without waiting for the TTL to expire.
func (f *HTTPJWKSFetcher) GetKey(ctx context.Context, kid string) (*jose.JSONWebKey, error) {
	jwks, err := f.FetchKeys(ctx)
	if err != nil {
		return nil, err
	}

	if key := findKey(jwks, kid); key != nil {
		return key, nil
	}

	// Key not found in cache — force a refresh to handle key rotation.
	// We bypass the TTL-based double-check so that a recently cached (but now
	// stale) JWKS does not prevent us from discovering newly added keys.
	jwks, err = f.forceRefresh(ctx)
	if err != nil {
		return nil, err
	}

	if key := findKey(jwks, kid); key != nil {
		return key, nil
	}

	return nil, fmt.Errorf("jwks: key not found: %s", kid)
}

// refresh fetches a fresh JWKS from the remote endpoint. It uses a mutex to
// serialise concurrent refreshes so that at most one HTTP request is in-flight
// at any time. A double-check after acquiring the lock avoids redundant fetches
// when multiple goroutines race to refresh.
func (f *HTTPJWKSFetcher) refresh(ctx context.Context) (*jose.JSONWebKeySet, error) {
	f.refreshMu.Lock()
	defer f.refreshMu.Unlock()

	// Double-check: another goroutine may have refreshed while we waited.
	f.mu.RLock()
	if f.cache != nil && time.Since(f.cachedAt) < f.cacheTTL {
		jwks := f.cache
		f.mu.RUnlock()
		return jwks, nil
	}
	f.mu.RUnlock()

	return f.doFetch(ctx)
}

// forceRefresh unconditionally fetches a fresh JWKS from the remote endpoint,
// bypassing the TTL-based cache check. It is used for key-rotation scenarios
// where a key ID is missing from a freshly cached JWKS.
func (f *HTTPJWKSFetcher) forceRefresh(ctx context.Context) (*jose.JSONWebKeySet, error) {
	f.refreshMu.Lock()
	defer f.refreshMu.Unlock()
	return f.doFetch(ctx)
}

// doFetch performs the actual HTTP fetch and updates the cache. Must be called
// with refreshMu held.
func (f *HTTPJWKSFetcher) doFetch(ctx context.Context) (*jose.JSONWebKeySet, error) {

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, f.jwksURL, nil)
	if err != nil {
		return nil, fmt.Errorf("jwks: creating request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := f.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("jwks: fetching %s: %w", f.jwksURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("jwks: endpoint returned %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxJWKSBodySize))
	if err != nil {
		return nil, fmt.Errorf("jwks: reading response body: %w", err)
	}

	var jwks jose.JSONWebKeySet
	if err := json.Unmarshal(body, &jwks); err != nil {
		return nil, fmt.Errorf("jwks: parsing key set: %w", err)
	}

	f.mu.Lock()
	f.cache = &jwks
	f.cachedAt = time.Now()
	f.mu.Unlock()

	f.logger.Info("JWKS cache refreshed",
		slog.String("url", f.jwksURL),
		slog.Int("key_count", len(jwks.Keys)),
	)

	return &jwks, nil
}

// findKey looks up a key by ID in the key set and returns the first match, or
// nil when no key with the given ID is present.
func findKey(jwks *jose.JSONWebKeySet, kid string) *jose.JSONWebKey {
	keys := jwks.Key(kid)
	if len(keys) == 0 {
		return nil
	}
	return &keys[0]
}
