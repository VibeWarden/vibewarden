package egress

import (
	"sync"

	domainegress "github.com/vibewarden/vibewarden/internal/domain/egress"
)

// defaultCacheMaxEntries is the number of LRU entries allocated per route when
// no explicit limit is provided. This bounds memory consumption per route.
const defaultCacheMaxEntries = 256

// ResponseCacheRegistry maintains one in-memory LRU cache per named egress
// route. Caches are created lazily on first access and are only created for
// routes that have cache.enabled=true. It is safe for concurrent use.
type ResponseCacheRegistry struct {
	mu     sync.Mutex
	caches map[string]*ResponseCache
}

// NewResponseCacheRegistry creates an empty ResponseCacheRegistry.
func NewResponseCacheRegistry() *ResponseCacheRegistry {
	return &ResponseCacheRegistry{
		caches: make(map[string]*ResponseCache),
	}
}

// CacheFor returns the ResponseCache for the given route, creating it lazily
// when needed. It returns nil when the route has cache.enabled=false.
func (r *ResponseCacheRegistry) CacheFor(route domainegress.Route) *ResponseCache {
	cfg := route.Cache()
	if cfg.IsZero() {
		return nil
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if c, ok := r.caches[route.Name()]; ok {
		return c
	}

	c := NewResponseCache(defaultCacheMaxEntries)
	r.caches[route.Name()] = c
	return c
}
