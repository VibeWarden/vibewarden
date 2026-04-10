// Package egress implements the HTTP listener and request forwarding adapter
// for the egress proxy plugin.
package egress

import (
	"bytes"
	"container/list"
	"io"
	"net/http"
	"sync"
	"time"

	domainegress "github.com/vibewarden/vibewarden/internal/domain/egress"
)

// headerEgressCache is the response header used to report whether a response
// was served from the in-memory cache or fetched from the upstream.
// Values are "HIT" or "MISS".
const headerEgressCache = "X-Egress-Cache"

// cacheKey is the lookup key for a cached response entry.
// Only the HTTP method (GET or HEAD) and the full URL are used.
type cacheKey struct {
	method string
	url    string
}

// CacheEntry holds a single cached upstream response.
// It is exported so that test helpers can build entries via NewCacheEntry.
type CacheEntry struct {
	key        cacheKey
	StatusCode int
	Header     http.Header
	Body       []byte
	ExpiresAt  time.Time
}

// NewCacheEntry creates a CacheEntry for testing. It sets only the key and
// expiry fields — the StatusCode defaults to 200 and Header/Body are empty.
// Use this constructor in external test packages that need to prime the cache
// directly without going through the full proxy pipeline.
func NewCacheEntry(method, url string, statusCode int, expiresAt time.Time) *CacheEntry {
	return &CacheEntry{
		key:        cacheKey{method: method, url: url},
		StatusCode: statusCode,
		Header:     make(http.Header),
		ExpiresAt:  expiresAt,
	}
}

// isExpired reports whether the entry has passed its TTL.
// An entry with a zero ExpiresAt never expires.
func (e *CacheEntry) isExpired(now time.Time) bool {
	if e.ExpiresAt.IsZero() {
		return false
	}
	return now.After(e.ExpiresAt)
}

// ResponseCache is a thread-safe in-memory LRU cache with per-entry TTL
// eviction. It caches egress responses keyed by HTTP method + URL.
//
// The cache uses a doubly-linked list to maintain LRU order alongside a map
// for O(1) look-ups. The maximum number of entries is bounded by maxEntries.
type ResponseCache struct {
	mu         sync.Mutex
	maxEntries int
	items      map[cacheKey]*list.Element
	lru        *list.List
}

// NewResponseCache creates a ResponseCache with the given maximum number of
// entries. maxEntries must be >= 1; values <= 0 are clamped to 1.
func NewResponseCache(maxEntries int) *ResponseCache {
	if maxEntries <= 0 {
		maxEntries = 1
	}
	return &ResponseCache{
		maxEntries: maxEntries,
		items:      make(map[cacheKey]*list.Element),
		lru:        list.New(),
	}
}

// Get looks up the cache for the given method and url at the given time.
// It returns the cached entry and true on a hit, or nil and false on a miss
// or when the entry has expired. Expired entries are removed on access.
func (c *ResponseCache) Get(method, url string, now time.Time) (*CacheEntry, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	k := cacheKey{method: method, url: url}
	el, ok := c.items[k]
	if !ok {
		return nil, false
	}
	entry := el.Value.(*CacheEntry)
	if entry.isExpired(now) {
		c.lru.Remove(el)
		delete(c.items, k)
		return nil, false
	}
	// Move to front (most-recently used).
	c.lru.MoveToFront(el)
	return entry, true
}

// Set inserts or replaces a cache entry for the given key. When the cache is
// at capacity the least-recently-used entry is evicted first.
func (c *ResponseCache) Set(entry *CacheEntry) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Replace existing entry if present.
	if el, ok := c.items[entry.key]; ok {
		c.lru.Remove(el)
		delete(c.items, entry.key)
	}

	// Evict the LRU entry when at capacity.
	for c.lru.Len() >= c.maxEntries {
		back := c.lru.Back()
		if back == nil {
			break
		}
		evicted := back.Value.(*CacheEntry)
		c.lru.Remove(back)
		delete(c.items, evicted.key)
	}

	el := c.lru.PushFront(entry)
	c.items[entry.key] = el
}

// Len returns the current number of entries in the cache.
func (c *ResponseCache) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.lru.Len()
}

// isCacheable reports whether the request method and response status code are
// eligible for caching. Only GET and HEAD methods with a 2xx status are cached.
func isCacheable(method string, statusCode int) bool {
	if method != http.MethodGet && method != http.MethodHead {
		return false
	}
	return statusCode >= 200 && statusCode <= 299
}

// cacheableResponse reads the full response body into memory and builds a
// CacheEntry. It returns the cached entry and a fresh io.ReadCloser that the
// caller can use as the response body (so the original body does not need to
// be re-read). Returns (nil, nil, nil) when the body exceeds cfg.MaxSize —
// the caller receives the full body back but the response is not cached.
// Returns (nil, nil, err) when the body cannot be read.
func cacheableResponse(
	resp domainegress.EgressResponse,
	cfg domainegress.CacheConfig,
	key cacheKey,
	now time.Time,
) (*CacheEntry, io.ReadCloser, error) {
	var raw []byte
	if rc, ok := resp.BodyRef.(io.ReadCloser); ok && rc != nil {
		defer rc.Close() //nolint:errcheck
		var err error
		if cfg.MaxSize > 0 {
			// Read at most MaxSize+1 bytes to detect oversized bodies.
			limited := io.LimitReader(rc, cfg.MaxSize+1)
			raw, err = io.ReadAll(limited)
		} else {
			raw, err = io.ReadAll(rc)
		}
		if err != nil {
			return nil, nil, err
		}
	}

	// Do not cache when the body exceeds the configured limit.
	if cfg.MaxSize > 0 && int64(len(raw)) > cfg.MaxSize {
		return nil, io.NopCloser(bytes.NewReader(raw)), nil
	}

	var expiresAt time.Time
	if cfg.TTL > 0 {
		expiresAt = now.Add(cfg.TTL)
	}

	entry := &CacheEntry{
		key:        key,
		StatusCode: resp.StatusCode,
		Header:     resp.Header.Clone(),
		Body:       raw,
		ExpiresAt:  expiresAt,
	}
	return entry, io.NopCloser(bytes.NewReader(raw)), nil
}

// egressResponseFromCache builds an EgressResponse from a cache hit entry.
// The header of the returned response is a clone of the cached headers so that
// the caller's modifications do not affect the cache.
func egressResponseFromCache(entry *CacheEntry, origDuration time.Duration) domainegress.EgressResponse {
	h := entry.Header.Clone()
	h.Set(headerEgressCache, "HIT")
	resp, _ := domainegress.NewEgressResponse(
		entry.StatusCode,
		h,
		io.NopCloser(bytes.NewReader(entry.Body)),
		origDuration,
	)
	return resp
}
