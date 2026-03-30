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

	domjwks "github.com/vibewarden/vibewarden/internal/domain/jwks"
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
	cacheDOM  *domjwks.KeySet
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
func (f *HTTPJWKSFetcher) FetchKeys(ctx context.Context) (*domjwks.KeySet, error) {
	f.mu.RLock()
	if f.cacheDOM != nil && time.Since(f.cachedAt) < f.cacheTTL {
		ks := f.cacheDOM
		f.mu.RUnlock()
		return ks, nil
	}
	f.mu.RUnlock()

	return f.refresh(ctx)
}

// GetKey implements ports.JWKSFetcher.
func (f *HTTPJWKSFetcher) GetKey(ctx context.Context, kid string) (*domjwks.Key, error) {
	ks, err := f.FetchKeys(ctx)
	if err != nil {
		return nil, err
	}

	if key := ks.FindByKID(kid); key != nil {
		return key, nil
	}

	// Key not found — force refresh for key rotation.
	ks, err = f.forceRefresh(ctx)
	if err != nil {
		return nil, err
	}

	if key := ks.FindByKID(kid); key != nil {
		return key, nil
	}

	return nil, fmt.Errorf("jwks: key not found: %s", kid)
}

// GetRawKey retrieves the raw go-jose key for signature verification.
// This is an adapter-internal method not exposed via the port.
func (f *HTTPJWKSFetcher) GetRawKey(ctx context.Context, kid string) (*jose.JSONWebKey, error) {
	f.mu.RLock()
	if f.cache != nil && time.Since(f.cachedAt) < f.cacheTTL {
		if key := findKey(f.cache, kid); key != nil {
			f.mu.RUnlock()
			return key, nil
		}
	}
	f.mu.RUnlock()

	// Refresh and try again.
	if _, err := f.forceRefresh(ctx); err != nil {
		return nil, err
	}

	f.mu.RLock()
	defer f.mu.RUnlock()
	if key := findKey(f.cache, kid); key != nil {
		return key, nil
	}
	return nil, fmt.Errorf("jwks: key not found: %s", kid)
}

func (f *HTTPJWKSFetcher) refresh(ctx context.Context) (*domjwks.KeySet, error) {
	f.refreshMu.Lock()
	defer f.refreshMu.Unlock()

	f.mu.RLock()
	if f.cacheDOM != nil && time.Since(f.cachedAt) < f.cacheTTL {
		ks := f.cacheDOM
		f.mu.RUnlock()
		return ks, nil
	}
	f.mu.RUnlock()

	return f.doFetch(ctx)
}

func (f *HTTPJWKSFetcher) forceRefresh(ctx context.Context) (*domjwks.KeySet, error) {
	f.refreshMu.Lock()
	defer f.refreshMu.Unlock()
	return f.doFetch(ctx)
}

func (f *HTTPJWKSFetcher) doFetch(ctx context.Context) (*domjwks.KeySet, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, f.jwksURL, nil)
	if err != nil {
		return nil, fmt.Errorf("jwks: creating request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := f.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("jwks: fetching %s: %w", f.jwksURL, err)
	}
	defer func() { _ = resp.Body.Close() }() //nolint:errcheck // response body close error is not actionable

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("jwks: endpoint returned %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxJWKSBodySize))
	if err != nil {
		return nil, fmt.Errorf("jwks: reading response body: %w", err)
	}

	var rawJWKS jose.JSONWebKeySet
	if err := json.Unmarshal(body, &rawJWKS); err != nil {
		return nil, fmt.Errorf("jwks: parsing key set: %w", err)
	}

	// Convert to domain type.
	domKeys := make([]domjwks.Key, 0, len(rawJWKS.Keys))
	for _, k := range rawJWKS.Keys {
		domKeys = append(domKeys, domjwks.Key{
			KID:       k.KeyID,
			Algorithm: k.Algorithm,
			PublicKey: k.Key,
		})
	}
	domKS := &domjwks.KeySet{Keys: domKeys}

	f.mu.Lock()
	f.cache = &rawJWKS
	f.cacheDOM = domKS
	f.cachedAt = time.Now()
	f.mu.Unlock()

	f.logger.Info("JWKS cache refreshed",
		slog.String("url", f.jwksURL),
		slog.Int("key_count", len(rawJWKS.Keys)),
	)

	return domKS, nil
}

func findKey(jwks *jose.JSONWebKeySet, kid string) *jose.JSONWebKey {
	keys := jwks.Key(kid)
	if len(keys) == 0 {
		return nil
	}
	return &keys[0]
}
