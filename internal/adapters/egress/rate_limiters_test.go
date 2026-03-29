package egress_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	egressadapter "github.com/vibewarden/vibewarden/internal/adapters/egress"
	domainegress "github.com/vibewarden/vibewarden/internal/domain/egress"
	"github.com/vibewarden/vibewarden/internal/domain/events"
)

// newTestProxyWithRL creates a Proxy wired to the given routes, HTTP client,
// and rate limiter registry.
func newTestProxyWithRL(
	t *testing.T,
	routes []domainegress.Route,
	client *http.Client,
	policy domainegress.Policy,
	rl *egressadapter.RateLimiterRegistry,
) *egressadapter.Proxy {
	t.Helper()
	resolver := egressadapter.NewRouteResolver(routes)
	cfg := egressadapter.ProxyConfig{
		Listen:         "127.0.0.1:0",
		DefaultPolicy:  policy,
		DefaultTimeout: 5 * time.Second,
		Routes:         routes,
		RateLimiters:   rl,
	}
	return egressadapter.NewProxy(cfg, resolver, client, nil)
}

// ---- parseRateLimit (via RateLimiterRegistry.Allow) ----

// TestParseRateLimit_ValidExpressions verifies that well-formed expressions are
// accepted and that no error is returned on the first Allow call.
func TestParseRateLimit_ValidExpressions(t *testing.T) {
	tests := []struct {
		name string
		expr string
	}{
		{"per_second", "60/s"},
		{"per_minute", "120/m"},
		{"per_hour", "3600/h"},
		{"fractional_per_minute", "1/m"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
			}))
			defer upstream.Close()

			route := newTestRoute(t, "api", upstream.URL+"/v1/*", domainegress.WithRateLimit(tt.expr))
			rl := egressadapter.NewRateLimiterRegistry(nil, nil)
			proxy := newTestProxyWithRL(t, []domainegress.Route{route}, upstream.Client(), domainegress.PolicyDeny, rl)

			req, err := domainegress.NewEgressRequest("GET", upstream.URL+"/v1/resource", nil, nil)
			if err != nil {
				t.Fatalf("NewEgressRequest: %v", err)
			}
			resp, err := proxy.HandleRequest(context.Background(), req)
			if err != nil {
				t.Fatalf("HandleRequest returned unexpected error: %v", err)
			}
			if resp.StatusCode != http.StatusOK {
				t.Errorf("StatusCode = %d, want %d", resp.StatusCode, http.StatusOK)
			}
		})
	}
}

// TestRateLimiterRegistry_NoConfig verifies that a route without a rate limit
// configuration passes all requests through unconditionally.
func TestRateLimiterRegistry_NoConfig(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	// Route without any rate limit.
	route := newTestRoute(t, "api", upstream.URL+"/v1/*")
	el := &fakeEventLogger{}
	rl := egressadapter.NewRateLimiterRegistry(nil, el)
	proxy := newTestProxyWithRL(t, []domainegress.Route{route}, upstream.Client(), domainegress.PolicyDeny, rl)

	for i := 0; i < 10; i++ {
		req, err := domainegress.NewEgressRequest("GET", upstream.URL+"/v1/resource", nil, nil)
		if err != nil {
			t.Fatalf("NewEgressRequest: %v", err)
		}
		resp, err := proxy.HandleRequest(context.Background(), req)
		if err != nil {
			t.Fatalf("request %d returned unexpected error: %v", i+1, err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Errorf("request %d: StatusCode = %d, want %d", i+1, resp.StatusCode, http.StatusOK)
		}
	}
	// No rate_limit_hit events should be emitted.
	if got := el.count(); got != 0 {
		t.Errorf("event count = %d, want 0", got)
	}
}

// TestRateLimiterRegistry_ExceedsLimit verifies that after exhausting the token
// bucket, HandleRequest returns ErrRateLimitExceeded.
func TestRateLimiterRegistry_ExceedsLimit(t *testing.T) {
	var callCount atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		callCount.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	// 1 request per second, burst = 1. First request consumes the burst token;
	// second request should be rejected immediately.
	route := newTestRoute(t, "api", upstream.URL+"/v1/*", domainegress.WithRateLimit("1/s"))
	el := &fakeEventLogger{}
	rl := egressadapter.NewRateLimiterRegistry(nil, el)
	proxy := newTestProxyWithRL(t, []domainegress.Route{route}, upstream.Client(), domainegress.PolicyDeny, rl)

	doRequest := func() (int, error) {
		req, err := domainegress.NewEgressRequest("GET", upstream.URL+"/v1/resource", nil, nil)
		if err != nil {
			t.Fatalf("NewEgressRequest: %v", err)
		}
		resp, err := proxy.HandleRequest(context.Background(), req)
		if err != nil {
			return 0, err
		}
		return resp.StatusCode, nil
	}

	// First request: token available — should succeed.
	status, err := doRequest()
	if err != nil {
		t.Fatalf("first request error: %v", err)
	}
	if status != http.StatusOK {
		t.Errorf("first request: StatusCode = %d, want %d", status, http.StatusOK)
	}
	if callCount.Load() != 1 {
		t.Errorf("upstream call count after first request = %d, want 1", callCount.Load())
	}

	// Second request: bucket exhausted — must return ErrRateLimitExceeded.
	_, err = doRequest()
	if err != egressadapter.ErrRateLimitExceeded {
		t.Errorf("second request error = %v, want ErrRateLimitExceeded", err)
	}

	// Upstream must NOT have been called a second time.
	if callCount.Load() != 1 {
		t.Errorf("upstream call count after rate limit = %d, want 1", callCount.Load())
	}

	// An egress.rate_limit_hit event must have been emitted.
	types := el.eventTypes()
	if len(types) != 1 || types[0] != events.EventTypeEgressRateLimitHit {
		t.Errorf("events = %v, want [%s]", types, events.EventTypeEgressRateLimitHit)
	}
}

// TestRateLimiterRegistry_HTTP429OnExceeded verifies that the HTTP handler
// returns 429 with a Retry-After header when the rate limit is exceeded.
func TestRateLimiterRegistry_HTTP429OnExceeded(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	route := newTestRoute(t, "api", upstream.URL+"/v1/*", domainegress.WithRateLimit("1/s"))
	rl := egressadapter.NewRateLimiterRegistry(nil, nil)

	resolver := egressadapter.NewRouteResolver([]domainegress.Route{route})
	cfg := egressadapter.ProxyConfig{
		Listen:         "127.0.0.1:0",
		DefaultPolicy:  domainegress.PolicyDeny,
		DefaultTimeout: 5 * time.Second,
		Routes:         []domainegress.Route{route},
		RateLimiters:   rl,
	}
	proxy := egressadapter.NewProxy(cfg, resolver, upstream.Client(), nil)

	if err := proxy.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer proxy.Stop(context.Background()) //nolint:errcheck

	proxyURL := "http://" + proxy.Addr() + "/"
	proxyClient := &http.Client{Timeout: 5 * time.Second}

	sendRequest := func() *http.Response {
		httpReq, err := http.NewRequest(http.MethodGet, proxyURL, nil)
		if err != nil {
			t.Fatalf("http.NewRequest: %v", err)
		}
		httpReq.Header.Set("X-Egress-URL", upstream.URL+"/v1/resource")
		resp, err := proxyClient.Do(httpReq)
		if err != nil {
			t.Fatalf("proxy request: %v", err)
		}
		return resp
	}

	// First request consumes the burst token.
	resp1 := sendRequest()
	resp1.Body.Close() //nolint:errcheck
	if resp1.StatusCode != http.StatusOK {
		t.Errorf("first request: StatusCode = %d, want %d", resp1.StatusCode, http.StatusOK)
	}

	// Second request: rate limit exceeded → 429.
	resp2 := sendRequest()
	resp2.Body.Close() //nolint:errcheck
	if resp2.StatusCode != http.StatusTooManyRequests {
		t.Errorf("second request: StatusCode = %d, want %d", resp2.StatusCode, http.StatusTooManyRequests)
	}
	if ra := resp2.Header.Get("Retry-After"); ra == "" {
		t.Error("second request: Retry-After header is missing")
	}
}

// TestRateLimiterRegistry_NilRegistrySkipsChecks verifies that when
// RateLimiters is nil in ProxyConfig, all requests proceed normally even when
// the route has a rate limit configured.
func TestRateLimiterRegistry_NilRegistrySkipsChecks(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	route := newTestRoute(t, "api", upstream.URL+"/v1/*", domainegress.WithRateLimit("1/s"))
	// Proxy with nil RateLimiters — rate limiting disabled.
	proxy := newTestProxy(t, []domainegress.Route{route}, upstream.Client(), domainegress.PolicyDeny)

	for i := 0; i < 5; i++ {
		req, err := domainegress.NewEgressRequest("GET", upstream.URL+"/v1/resource", nil, nil)
		if err != nil {
			t.Fatalf("NewEgressRequest: %v", err)
		}
		resp, err := proxy.HandleRequest(context.Background(), req)
		if err != nil {
			t.Fatalf("request %d returned unexpected error: %v", i+1, err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Errorf("request %d: StatusCode = %d, want %d", i+1, resp.StatusCode, http.StatusOK)
		}
	}
}

// TestRateLimiterRegistry_IndependentPerRoute verifies that rate limiters are
// isolated per route — exhausting one route's bucket does not affect another.
func TestRateLimiterRegistry_IndependentPerRoute(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	// routeA has a very tight limit (1/s, burst 1).
	// routeB has a generous limit (1000/s) — effectively unlimited for this test.
	routeA := newTestRoute(t, "a", upstream.URL+"/a/*", domainegress.WithRateLimit("1/s"))
	routeB := newTestRoute(t, "b", upstream.URL+"/b/*", domainegress.WithRateLimit("1000/s"))

	rl := egressadapter.NewRateLimiterRegistry(nil, nil)
	proxy := newTestProxyWithRL(t,
		[]domainegress.Route{routeA, routeB},
		upstream.Client(),
		domainegress.PolicyDeny,
		rl,
	)

	doRequest := func(url string) error {
		req, err := domainegress.NewEgressRequest("GET", url, nil, nil)
		if err != nil {
			t.Fatalf("NewEgressRequest: %v", err)
		}
		_, err = proxy.HandleRequest(context.Background(), req)
		return err
	}

	// Exhaust routeA's bucket.
	if err := doRequest(upstream.URL + "/a/resource"); err != nil {
		t.Fatalf("routeA first request: %v", err)
	}
	if err := doRequest(upstream.URL + "/a/resource"); err != egressadapter.ErrRateLimitExceeded {
		t.Errorf("routeA second request: want ErrRateLimitExceeded, got %v", err)
	}

	// routeB must still accept requests.
	if err := doRequest(upstream.URL + "/b/resource"); err != nil {
		t.Errorf("routeB: unexpected error: %v", err)
	}
}

// TestRateLimiterRegistry_EventPayload verifies that the egress.rate_limit_hit
// event carries the expected payload fields.
func TestRateLimiterRegistry_EventPayload(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	const routeName = "payments"
	route := newTestRoute(t, routeName, upstream.URL+"/v1/*", domainegress.WithRateLimit("1/s"))
	el := &fakeEventLogger{}
	rl := egressadapter.NewRateLimiterRegistry(nil, el)
	proxy := newTestProxyWithRL(t, []domainegress.Route{route}, upstream.Client(), domainegress.PolicyDeny, rl)

	doRequest := func() error {
		req, err := domainegress.NewEgressRequest("GET", upstream.URL+"/v1/resource", nil, nil)
		if err != nil {
			t.Fatalf("NewEgressRequest: %v", err)
		}
		_, err = proxy.HandleRequest(context.Background(), req)
		return err
	}

	// Consume the burst token.
	_ = doRequest()
	// Trigger the rate limit.
	_ = doRequest()

	el.mu.Lock()
	var hitEv events.Event
	for _, e := range el.logged {
		if e.EventType == events.EventTypeEgressRateLimitHit {
			hitEv = e
			break
		}
	}
	el.mu.Unlock()

	if hitEv.EventType == "" {
		t.Fatalf("no %s event found", events.EventTypeEgressRateLimitHit)
	}
	if hitEv.SchemaVersion != "v1" {
		t.Errorf("SchemaVersion = %q, want %q", hitEv.SchemaVersion, "v1")
	}
	if got, ok := hitEv.Payload["route"].(string); !ok || got != routeName {
		t.Errorf("payload.route = %v, want %q", hitEv.Payload["route"], routeName)
	}
	if _, ok := hitEv.Payload["limit"].(float64); !ok {
		t.Errorf("payload.limit is not float64: %T", hitEv.Payload["limit"])
	}
	if _, ok := hitEv.Payload["retry_after_seconds"].(float64); !ok {
		t.Errorf("payload.retry_after_seconds is not float64: %T", hitEv.Payload["retry_after_seconds"])
	}
	if hitEv.AISummary == "" {
		t.Error("AISummary must not be empty")
	}
}
