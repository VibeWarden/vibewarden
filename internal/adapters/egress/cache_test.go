package egress_test

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	egressadapter "github.com/vibewarden/vibewarden/internal/adapters/egress"
	domainegress "github.com/vibewarden/vibewarden/internal/domain/egress"
)

// ---- ResponseCache unit tests ----

// TestResponseCache_GetMiss verifies that a cache returns nothing for an
// unknown key.
func TestResponseCache_GetMiss(t *testing.T) {
	c := egressadapter.NewResponseCache(8)
	_, hit := c.Get("GET", "https://example.com/", time.Now())
	if hit {
		t.Error("expected cache miss, got hit")
	}
}

// TestResponseCache_SetAndGet verifies that a stored entry can be retrieved.
func TestResponseCache_SetAndGet(t *testing.T) {
	c := egressadapter.NewResponseCache(8)
	entry := egressadapter.NewCacheEntry("GET", "https://example.com/", 200, time.Time{})

	c.Set(entry)

	got, hit := c.Get("GET", "https://example.com/", time.Now())
	if !hit {
		t.Fatal("expected cache hit")
	}
	if got.StatusCode != 200 {
		t.Errorf("StatusCode = %d, want 200", got.StatusCode)
	}
}

// TestResponseCache_ExpiredEntry verifies that an expired entry is treated as
// a miss and removed from the cache.
func TestResponseCache_ExpiredEntry(t *testing.T) {
	c := egressadapter.NewResponseCache(8)
	past := time.Now().Add(-time.Minute) // already expired
	entry := egressadapter.NewCacheEntry("GET", "https://example.com/ttl", 200, past)
	c.Set(entry)

	_, hit := c.Get("GET", "https://example.com/ttl", time.Now())
	if hit {
		t.Error("expected expired entry to be a miss")
	}
	if c.Len() != 0 {
		t.Errorf("Len = %d after expiry, want 0", c.Len())
	}
}

// TestResponseCache_ZeroExpiresAt verifies that an entry with zero ExpiresAt
// never expires.
func TestResponseCache_ZeroExpiresAt(t *testing.T) {
	c := egressadapter.NewResponseCache(8)
	entry := egressadapter.NewCacheEntry("GET", "https://example.com/noexp", 200, time.Time{})
	c.Set(entry)

	far := time.Now().Add(365 * 24 * time.Hour)
	_, hit := c.Get("GET", "https://example.com/noexp", far)
	if !hit {
		t.Error("expected non-expiring entry to remain valid far in the future")
	}
}

// TestResponseCache_LRUEviction verifies that the least-recently-used entry is
// evicted when the cache is at capacity.
func TestResponseCache_LRUEviction(t *testing.T) {
	c := egressadapter.NewResponseCache(2)
	e1 := egressadapter.NewCacheEntry("GET", "https://example.com/1", 200, time.Time{})
	e2 := egressadapter.NewCacheEntry("GET", "https://example.com/2", 200, time.Time{})
	e3 := egressadapter.NewCacheEntry("GET", "https://example.com/3", 200, time.Time{})

	c.Set(e1)
	c.Set(e2)

	// Access e1 to make it most-recently-used, so e2 becomes LRU.
	_, _ = c.Get("GET", "https://example.com/1", time.Now())

	// Adding e3 should evict e2 (LRU).
	c.Set(e3)

	_, hit1 := c.Get("GET", "https://example.com/1", time.Now())
	_, hit2 := c.Get("GET", "https://example.com/2", time.Now())
	_, hit3 := c.Get("GET", "https://example.com/3", time.Now())

	if !hit1 {
		t.Error("expected e1 to still be cached (was recently accessed)")
	}
	if hit2 {
		t.Error("expected e2 to have been evicted (LRU)")
	}
	if !hit3 {
		t.Error("expected e3 to be cached (just inserted)")
	}
}

// TestResponseCache_Replace verifies that setting an entry for the same key
// replaces the previous value and does not grow the cache.
func TestResponseCache_Replace(t *testing.T) {
	c := egressadapter.NewResponseCache(8)
	e1 := egressadapter.NewCacheEntry("GET", "https://example.com/r", 200, time.Time{})
	e2 := egressadapter.NewCacheEntry("GET", "https://example.com/r", 201, time.Time{})

	c.Set(e1)
	c.Set(e2)

	if c.Len() != 1 {
		t.Errorf("Len = %d after replace, want 1", c.Len())
	}
	got, hit := c.Get("GET", "https://example.com/r", time.Now())
	if !hit || got.StatusCode != 201 {
		t.Errorf("expected updated entry with status 201, got hit=%v status=%d", hit, got.StatusCode)
	}
}

// TestResponseCache_MaxEntriesClamped verifies that a zero or negative maxEntries
// value is clamped to 1 and the cache still accepts at least one entry.
func TestResponseCache_MaxEntriesClamped(t *testing.T) {
	for _, max := range []int{0, -1, -100} {
		c := egressadapter.NewResponseCache(max)
		e := egressadapter.NewCacheEntry("GET", "https://example.com/", 200, time.Time{})
		c.Set(e)
		if c.Len() != 1 {
			t.Errorf("maxEntries=%d: Len = %d, want 1", max, c.Len())
		}
	}
}

// ---- Proxy integration tests for caching ----

// TestProxy_CacheMiss_SetsHeader verifies that a non-cached response carries
// X-Egress-Cache: MISS.
func TestProxy_CacheMiss_SetsHeader(t *testing.T) {
	upstream, requests := newCountingUpstream(t, http.StatusOK, []byte("body"))

	route := newTestRouteWithCache(t, "api", upstream.URL+"/v1/*", domainegress.CacheConfig{
		Enabled: true,
		TTL:     time.Minute,
	})
	proxy := newTestProxyWithCache(t, route)

	resp := mustHandleRequest(t, proxy, "GET", upstream.URL+"/v1/resource")

	if got := resp.Header.Get("X-Egress-Cache"); got != "MISS" {
		t.Errorf("X-Egress-Cache = %q, want MISS", got)
	}
	if *requests != 1 {
		t.Errorf("upstream requests = %d, want 1", *requests)
	}
}

// TestProxy_CacheHit_SetsHeader verifies that the second identical request is
// served from cache and carries X-Egress-Cache: HIT.
func TestProxy_CacheHit_SetsHeader(t *testing.T) {
	upstream, requests := newCountingUpstream(t, http.StatusOK, []byte("cached-body"))

	route := newTestRouteWithCache(t, "api", upstream.URL+"/v1/*", domainegress.CacheConfig{
		Enabled: true,
		TTL:     time.Minute,
	})
	proxy := newTestProxyWithCache(t, route)

	// First request — cache MISS, stores entry.
	mustHandleRequest(t, proxy, "GET", upstream.URL+"/v1/resource")

	// Second request — should be served from cache.
	resp2 := mustHandleRequest(t, proxy, "GET", upstream.URL+"/v1/resource")

	if got := resp2.Header.Get("X-Egress-Cache"); got != "HIT" {
		t.Errorf("X-Egress-Cache = %q, want HIT", got)
	}
	if *requests != 1 {
		t.Errorf("upstream received %d requests, want 1 (second should be served from cache)", *requests)
	}
}

// TestProxy_CacheHit_Body verifies that the cached body bytes match what the
// upstream originally returned.
func TestProxy_CacheHit_Body(t *testing.T) {
	want := []byte("hello cache")
	upstream, _ := newCountingUpstream(t, http.StatusOK, want)

	route := newTestRouteWithCache(t, "api", upstream.URL+"/v1/*", domainegress.CacheConfig{
		Enabled: true,
		TTL:     time.Minute,
	})
	proxy := newTestProxyWithCache(t, route)

	mustHandleRequest(t, proxy, "GET", upstream.URL+"/v1/item") // prime cache
	resp := mustHandleRequest(t, proxy, "GET", upstream.URL+"/v1/item")

	body := readBody(t, resp)
	if !bytes.Equal(body, want) {
		t.Errorf("body = %q, want %q", body, want)
	}
}

// TestProxy_CacheExpiry verifies that an expired entry results in a fresh
// upstream request after the TTL elapses.
func TestProxy_CacheExpiry(t *testing.T) {
	upstream, requests := newCountingUpstream(t, http.StatusOK, []byte("body"))

	route := newTestRouteWithCache(t, "api", upstream.URL+"/v1/*", domainegress.CacheConfig{
		Enabled: true,
		TTL:     50 * time.Millisecond,
	})
	proxy := newTestProxyWithCache(t, route)

	mustHandleRequest(t, proxy, "GET", upstream.URL+"/v1/exp")
	time.Sleep(100 * time.Millisecond) // let TTL elapse

	resp := mustHandleRequest(t, proxy, "GET", upstream.URL+"/v1/exp")
	if got := resp.Header.Get("X-Egress-Cache"); got != "MISS" {
		t.Errorf("X-Egress-Cache = %q after expiry, want MISS", got)
	}
	if *requests != 2 {
		t.Errorf("upstream requests = %d after expiry, want 2", *requests)
	}
}

// TestProxy_CacheNotCacheable_POST verifies that POST requests are never
// cached and always reach the upstream.
func TestProxy_CacheNotCacheable_POST(t *testing.T) {
	upstream, requests := newCountingUpstream(t, http.StatusOK, []byte("ok"))

	route := newTestRouteWithCache(t, "api", upstream.URL+"/v1/*", domainegress.CacheConfig{
		Enabled: true,
		TTL:     time.Minute,
	})
	proxy := newTestProxyWithCache(t, route)

	for i := 0; i < 3; i++ {
		mustHandleRequest(t, proxy, "POST", upstream.URL+"/v1/action")
	}
	if *requests != 3 {
		t.Errorf("upstream requests = %d, want 3 (POST must never be cached)", *requests)
	}
}

// TestProxy_CacheDisabled_NoCacheHeaders verifies that routes without
// cache.enabled=true never set X-Egress-Cache headers.
func TestProxy_CacheDisabled_NoCacheHeaders(t *testing.T) {
	upstream, _ := newCountingUpstream(t, http.StatusOK, []byte("ok"))

	route := newTestRoute(t, "api", upstream.URL+"/v1/*")
	proxy := newTestProxy(t, []domainegress.Route{route}, upstream.Client(), domainegress.PolicyDeny)

	resp := mustHandleRequest(t, proxy, "GET", upstream.URL+"/v1/resource")
	if h := resp.Header.Get("X-Egress-Cache"); h != "" {
		t.Errorf("X-Egress-Cache = %q on non-cached route, want empty", h)
	}
}

// TestProxy_CacheMaxSize_OversizedBodyNotCached verifies that responses whose
// body exceeds cache.max_size are not stored, so the next request hits the
// upstream again.
func TestProxy_CacheMaxSize_OversizedBodyNotCached(t *testing.T) {
	body := bytes.Repeat([]byte("x"), 100)
	upstream, requests := newCountingUpstream(t, http.StatusOK, body)

	route := newTestRouteWithCache(t, "api", upstream.URL+"/v1/*", domainegress.CacheConfig{
		Enabled: true,
		TTL:     time.Minute,
		MaxSize: 10, // 10 bytes — well below the 100-byte response body
	})
	proxy := newTestProxyWithCache(t, route)

	mustHandleRequest(t, proxy, "GET", upstream.URL+"/v1/big")
	mustHandleRequest(t, proxy, "GET", upstream.URL+"/v1/big")

	if *requests != 2 {
		t.Errorf("upstream requests = %d, want 2 (oversized body must not be cached)", *requests)
	}
}

// TestProxy_CacheNon2xx_NotCached verifies that 4xx and 5xx responses are
// never stored in the cache.
func TestProxy_CacheNon2xx_NotCached(t *testing.T) {
	upstream, requests := newCountingUpstream(t, http.StatusNotFound, []byte("not found"))

	route := newTestRouteWithCache(t, "api", upstream.URL+"/v1/*", domainegress.CacheConfig{
		Enabled: true,
		TTL:     time.Minute,
	})
	proxy := newTestProxyWithCache(t, route)

	for i := 0; i < 3; i++ {
		mustHandleRequest(t, proxy, "GET", upstream.URL+"/v1/gone")
	}
	if *requests != 3 {
		t.Errorf("upstream requests = %d, want 3 (404 must not be cached)", *requests)
	}
}

// TestProxy_CacheHead_Cached verifies that HEAD requests are eligible for
// caching just like GET requests.
func TestProxy_CacheHead_Cached(t *testing.T) {
	upstream, requests := newCountingUpstream(t, http.StatusOK, nil)

	route := newTestRouteWithCache(t, "api", upstream.URL+"/v1/*", domainegress.CacheConfig{
		Enabled: true,
		TTL:     time.Minute,
	})
	proxy := newTestProxyWithCache(t, route)

	mustHandleRequest(t, proxy, "HEAD", upstream.URL+"/v1/check")
	resp2 := mustHandleRequest(t, proxy, "HEAD", upstream.URL+"/v1/check")

	if got := resp2.Header.Get("X-Egress-Cache"); got != "HIT" {
		t.Errorf("X-Egress-Cache = %q on second HEAD, want HIT", got)
	}
	if *requests != 1 {
		t.Errorf("upstream received %d requests, want 1", *requests)
	}
}

// TestProxy_CacheDifferentURLs verifies that entries for different URLs are
// stored independently.
func TestProxy_CacheDifferentURLs(t *testing.T) {
	upstream, requests := newCountingUpstream(t, http.StatusOK, []byte("data"))

	route := newTestRouteWithCache(t, "api", upstream.URL+"/v1/*", domainegress.CacheConfig{
		Enabled: true,
		TTL:     time.Minute,
	})
	proxy := newTestProxyWithCache(t, route)

	mustHandleRequest(t, proxy, "GET", upstream.URL+"/v1/a")
	mustHandleRequest(t, proxy, "GET", upstream.URL+"/v1/b")

	// Both are fresh — two upstream calls expected.
	if *requests != 2 {
		t.Errorf("upstream requests = %d, want 2 (different URLs are different cache keys)", *requests)
	}

	// Repeat — both should now hit cache.
	mustHandleRequest(t, proxy, "GET", upstream.URL+"/v1/a")
	mustHandleRequest(t, proxy, "GET", upstream.URL+"/v1/b")
	if *requests != 2 {
		t.Errorf("upstream requests = %d after cache prime, want still 2", *requests)
	}
}

// ---- helpers ----

func newTestRouteWithCache(t *testing.T, name, pattern string, cfg domainegress.CacheConfig) domainegress.Route {
	t.Helper()
	r, err := domainegress.NewRoute(name, pattern, domainegress.WithCache(cfg))
	if err != nil {
		t.Fatalf("NewRoute: %v", err)
	}
	return r
}

func newTestProxyWithCache(t *testing.T, route domainegress.Route) *egressadapter.Proxy {
	t.Helper()
	resolver := egressadapter.NewRouteResolver([]domainegress.Route{route})
	cfg := egressadapter.ProxyConfig{
		Listen:         "127.0.0.1:0",
		DefaultPolicy:  domainegress.PolicyDeny,
		DefaultTimeout: 5 * time.Second,
		Routes:         []domainegress.Route{route},
		AllowInsecure:  true,
		ResponseCaches: egressadapter.NewResponseCacheRegistry(),
	}
	return egressadapter.NewProxy(cfg, resolver, nil, nil)
}

// newCountingUpstream creates a test HTTP server that always responds with the
// given status and body. It returns the server and a pointer to a counter of
// requests received. The server is closed automatically via t.Cleanup.
func newCountingUpstream(t *testing.T, status int, body []byte) (*httptest.Server, *int) {
	t.Helper()
	count := new(int)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		*count++
		w.WriteHeader(status)
		if len(body) > 0 {
			_, _ = w.Write(body)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, count
}

func mustHandleRequest(t *testing.T, proxy *egressadapter.Proxy, method, rawURL string) domainegress.EgressResponse {
	t.Helper()
	req, err := domainegress.NewEgressRequest(method, rawURL, nil, nil)
	if err != nil {
		t.Fatalf("NewEgressRequest(%s %s): %v", method, rawURL, err)
	}
	resp, err := proxy.HandleRequest(t.Context(), req)
	if err != nil {
		t.Fatalf("HandleRequest(%s %s): %v", method, rawURL, err)
	}
	return resp
}

func readBody(t *testing.T, resp domainegress.EgressResponse) []byte {
	t.Helper()
	rc, ok := resp.BodyRef.(io.ReadCloser)
	if !ok || rc == nil {
		return nil
	}
	defer rc.Close() //nolint:errcheck
	data, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("reading body: %v", err)
	}
	return data
}
