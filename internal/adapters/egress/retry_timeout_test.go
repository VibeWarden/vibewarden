package egress_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	egressadapter "github.com/vibewarden/vibewarden/internal/adapters/egress"
	domainegress "github.com/vibewarden/vibewarden/internal/domain/egress"
)

// TestForward_PerRouteTimeout_Returns504 verifies that a per-route timeout that
// is shorter than the upstream response time causes the proxy to return 504 to
// the caller via the HTTP handler.
func TestForward_PerRouteTimeout_Returns504(t *testing.T) {
	// Upstream sleeps longer than the route timeout.
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	route := newTestRoute(t, "slow", upstream.URL+"/v1/*",
		domainegress.WithTimeout(50*time.Millisecond),
	)

	resolver := egressadapter.NewRouteResolver([]domainegress.Route{route})
	cfg := egressadapter.ProxyConfig{
		Listen:         "127.0.0.1:0",
		DefaultPolicy:  domainegress.PolicyDeny,
		DefaultTimeout: 5 * time.Second,
		Routes:         []domainegress.Route{route},
	}
	proxy := egressadapter.NewProxy(cfg, resolver, upstream.Client(), nil)

	if err := proxy.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer proxy.Stop(context.Background()) //nolint:errcheck

	proxyURL := "http://" + proxy.Addr() + "/"
	proxyClient := &http.Client{Timeout: 5 * time.Second}

	httpReq, err := http.NewRequest(http.MethodGet, proxyURL, nil)
	if err != nil {
		t.Fatalf("http.NewRequest: %v", err)
	}
	httpReq.Header.Set("X-Egress-URL", upstream.URL+"/v1/resource")

	resp, err := proxyClient.Do(httpReq)
	if err != nil {
		t.Fatalf("proxy request: %v", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusGatewayTimeout {
		t.Errorf("StatusCode = %d, want %d (504 Gateway Timeout)", resp.StatusCode, http.StatusGatewayTimeout)
	}
}

// TestForward_DefaultTimeout_Returns504 verifies that the global default timeout
// also causes 504 when the upstream is too slow.
func TestForward_DefaultTimeout_Returns504(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	route := newTestRoute(t, "slow", upstream.URL+"/v1/*")

	resolver := egressadapter.NewRouteResolver([]domainegress.Route{route})
	cfg := egressadapter.ProxyConfig{
		Listen:         "127.0.0.1:0",
		DefaultPolicy:  domainegress.PolicyDeny,
		DefaultTimeout: 50 * time.Millisecond, // very short global timeout
		Routes:         []domainegress.Route{route},
	}
	proxy := egressadapter.NewProxy(cfg, resolver, upstream.Client(), nil)

	if err := proxy.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer proxy.Stop(context.Background()) //nolint:errcheck

	proxyURL := "http://" + proxy.Addr() + "/"
	proxyClient := &http.Client{Timeout: 5 * time.Second}

	httpReq, err := http.NewRequest(http.MethodGet, proxyURL, nil)
	if err != nil {
		t.Fatalf("http.NewRequest: %v", err)
	}
	httpReq.Header.Set("X-Egress-URL", upstream.URL+"/v1/resource")

	resp, err := proxyClient.Do(httpReq)
	if err != nil {
		t.Fatalf("proxy request: %v", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusGatewayTimeout {
		t.Errorf("StatusCode = %d, want %d (504 Gateway Timeout)", resp.StatusCode, http.StatusGatewayTimeout)
	}
}

// TestForward_RetryOnTransientStatus verifies that a route with RetryConfig
// retries on a retryable status code and ultimately returns the successful
// response. The X-Egress-Attempts header must reflect total attempt count.
func TestForward_RetryOnTransientStatus(t *testing.T) {
	var callCount atomic.Int32

	// First two calls return 503, third returns 200.
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := callCount.Add(1)
		if n < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer upstream.Close()

	route := newTestRoute(t, "api", upstream.URL+"/v1/*",
		domainegress.WithRetry(domainegress.RetryConfig{
			Max:            3,
			Methods:        []string{"GET"},
			Backoff:        domainegress.RetryBackoffFixed,
			InitialBackoff: 5 * time.Millisecond,
		}),
	)

	resolver := egressadapter.NewRouteResolver([]domainegress.Route{route})
	cfg := egressadapter.ProxyConfig{
		Listen:         "127.0.0.1:0",
		DefaultPolicy:  domainegress.PolicyDeny,
		DefaultTimeout: 5 * time.Second,
		Routes:         []domainegress.Route{route},
	}
	proxy := egressadapter.NewProxy(cfg, resolver, upstream.Client(), nil)

	if err := proxy.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer proxy.Stop(context.Background()) //nolint:errcheck

	proxyURL := "http://" + proxy.Addr() + "/"
	proxyClient := &http.Client{Timeout: 5 * time.Second}

	httpReq, err := http.NewRequest(http.MethodGet, proxyURL, nil)
	if err != nil {
		t.Fatalf("http.NewRequest: %v", err)
	}
	httpReq.Header.Set("X-Egress-URL", upstream.URL+"/v1/resource")

	resp, err := proxyClient.Do(httpReq)
	if err != nil {
		t.Fatalf("proxy request: %v", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		t.Errorf("StatusCode = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	body, _ := io.ReadAll(resp.Body)
	if string(body) != "ok" {
		t.Errorf("body = %q, want %q", string(body), "ok")
	}

	// Three upstream calls should have been made (2 failures + 1 success).
	if got := callCount.Load(); got != 3 {
		t.Errorf("upstream call count = %d, want 3", got)
	}

	// X-Egress-Attempts must be set to 3.
	if got := resp.Header.Get("X-Egress-Attempts"); got != "3" {
		t.Errorf("X-Egress-Attempts = %q, want %q", got, "3")
	}
}

// TestForward_NoRetryOnNonIdempotentMethod verifies that POST requests are NOT
// retried by default (POST is not idempotent).
func TestForward_NoRetryOnNonIdempotentMethod(t *testing.T) {
	var callCount atomic.Int32

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		callCount.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer upstream.Close()

	route := newTestRoute(t, "api", upstream.URL+"/v1/*",
		domainegress.WithRetry(domainegress.RetryConfig{
			Max:            3,
			Backoff:        domainegress.RetryBackoffFixed,
			InitialBackoff: 5 * time.Millisecond,
			// Methods is empty — defaults to idempotent set (GET, HEAD, PUT, DELETE)
		}),
	)

	resolver := egressadapter.NewRouteResolver([]domainegress.Route{route})
	cfg := egressadapter.ProxyConfig{
		Listen:         "127.0.0.1:0",
		DefaultPolicy:  domainegress.PolicyDeny,
		DefaultTimeout: 5 * time.Second,
		Routes:         []domainegress.Route{route},
	}
	proxy := egressadapter.NewProxy(cfg, resolver, upstream.Client(), nil)

	if err := proxy.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer proxy.Stop(context.Background()) //nolint:errcheck

	proxyURL := "http://" + proxy.Addr() + "/"
	proxyClient := &http.Client{Timeout: 5 * time.Second}

	httpReq, err := http.NewRequest(http.MethodPost, proxyURL, nil)
	if err != nil {
		t.Fatalf("http.NewRequest: %v", err)
	}
	httpReq.Header.Set("X-Egress-URL", upstream.URL+"/v1/resource")

	resp, err := proxyClient.Do(httpReq)
	if err != nil {
		t.Fatalf("proxy request: %v", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	// POST must not be retried — upstream should be called exactly once.
	if got := callCount.Load(); got != 1 {
		t.Errorf("upstream call count = %d, want 1 (POST must not be retried)", got)
	}

	// X-Egress-Attempts must be 1 (no retries).
	if got := resp.Header.Get("X-Egress-Attempts"); got != "1" {
		t.Errorf("X-Egress-Attempts = %q, want %q", got, "1")
	}
}

// TestForward_RetryExhausted verifies that when all retry attempts fail, the
// last error response is returned to the caller (not an error).
func TestForward_RetryExhausted(t *testing.T) {
	var callCount atomic.Int32

	// Always return 503.
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		callCount.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer upstream.Close()

	route := newTestRoute(t, "api", upstream.URL+"/v1/*",
		domainegress.WithRetry(domainegress.RetryConfig{
			Max:            2,
			Methods:        []string{"GET"},
			Backoff:        domainegress.RetryBackoffFixed,
			InitialBackoff: 5 * time.Millisecond,
		}),
	)

	proxy := newTestProxy(t, []domainegress.Route{route}, upstream.Client(), domainegress.PolicyDeny)

	req, err := domainegress.NewEgressRequest("GET", upstream.URL+"/v1/resource", nil, nil)
	if err != nil {
		t.Fatalf("NewEgressRequest: %v", err)
	}

	resp, err := proxy.HandleRequest(context.Background(), req)
	if err != nil {
		t.Fatalf("HandleRequest returned unexpected error: %v", err)
	}

	// After exhausting 3 total attempts (1 initial + 2 retries), the last 503
	// is returned as the response (not an error).
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("StatusCode = %d, want %d", resp.StatusCode, http.StatusServiceUnavailable)
	}

	if got := callCount.Load(); got != 3 {
		t.Errorf("upstream call count = %d, want 3", got)
	}

	if resp.Attempts != 3 {
		t.Errorf("Attempts = %d, want 3", resp.Attempts)
	}
}

// TestForward_NoRetry_AttemptsIsOne verifies that when no retry is configured,
// Attempts is set to 1 on a successful response.
func TestForward_NoRetry_AttemptsIsOne(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	route := newTestRoute(t, "api", upstream.URL+"/v1/*")
	proxy := newTestProxy(t, []domainegress.Route{route}, upstream.Client(), domainegress.PolicyDeny)

	req, err := domainegress.NewEgressRequest("GET", upstream.URL+"/v1/resource", nil, nil)
	if err != nil {
		t.Fatalf("NewEgressRequest: %v", err)
	}

	resp, err := proxy.HandleRequest(context.Background(), req)
	if err != nil {
		t.Fatalf("HandleRequest: %v", err)
	}

	if resp.Attempts != 1 {
		t.Errorf("Attempts = %d, want 1", resp.Attempts)
	}
}

// TestForward_ExponentialBackoff verifies that exponential backoff doubles the
// wait time on each attempt. We test computeBackoff indirectly by measuring
// total elapsed time: 3 attempts with 10ms base should take at least 10+20=30ms.
func TestForward_ExponentialBackoff(t *testing.T) {
	var callCount atomic.Int32

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := callCount.Add(1)
		if n < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	route := newTestRoute(t, "api", upstream.URL+"/v1/*",
		domainegress.WithRetry(domainegress.RetryConfig{
			Max:            3,
			Methods:        []string{"GET"},
			Backoff:        domainegress.RetryBackoffExponential,
			InitialBackoff: 10 * time.Millisecond,
		}),
	)

	proxy := newTestProxy(t, []domainegress.Route{route}, upstream.Client(), domainegress.PolicyDeny)

	req, err := domainegress.NewEgressRequest("GET", upstream.URL+"/v1/resource", nil, nil)
	if err != nil {
		t.Fatalf("NewEgressRequest: %v", err)
	}

	start := time.Now()
	resp, err := proxy.HandleRequest(context.Background(), req)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("HandleRequest: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("StatusCode = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	// 2 retries with exponential backoff: 10ms + 20ms = at least 30ms.
	const minElapsed = 25 * time.Millisecond // slight tolerance
	if elapsed < minElapsed {
		t.Errorf("elapsed %v < %v — exponential backoff not applied", elapsed, minElapsed)
	}
}

// TestForward_RetryIdempotentMethods verifies that only GET, HEAD, PUT, DELETE
// are retried by default when Methods is empty.
func TestForward_RetryIdempotentMethods(t *testing.T) {
	tests := []struct {
		method      string
		shouldRetry bool
	}{
		{"GET", true},
		{"HEAD", true},
		{"PUT", true},
		{"DELETE", true},
		{"POST", false},
		{"PATCH", false},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("%s shouldRetry=%v", tt.method, tt.shouldRetry), func(t *testing.T) {
			var callCount atomic.Int32

			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				callCount.Add(1)
				// HEAD must not write a body per RFC 9110.
				if r.Method == http.MethodHead {
					w.WriteHeader(http.StatusServiceUnavailable)
					return
				}
				w.WriteHeader(http.StatusServiceUnavailable)
			}))
			defer upstream.Close()

			route := newTestRoute(t, "api", upstream.URL+"/v1/*",
				domainegress.WithRetry(domainegress.RetryConfig{
					Max:            2,
					Backoff:        domainegress.RetryBackoffFixed,
					InitialBackoff: 2 * time.Millisecond,
					// Methods empty — uses default idempotent set.
				}),
			)

			proxy := newTestProxy(t, []domainegress.Route{route}, upstream.Client(), domainegress.PolicyDeny)

			req, err := domainegress.NewEgressRequest(tt.method, upstream.URL+"/v1/resource", nil, nil)
			if err != nil {
				t.Fatalf("NewEgressRequest: %v", err)
			}

			_, _ = proxy.HandleRequest(context.Background(), req)

			got := callCount.Load()
			var want int32
			if tt.shouldRetry {
				want = 3 // 1 initial + 2 retries
			} else {
				want = 1
			}
			if got != want {
				t.Errorf("callCount = %d, want %d (method %s)", got, want, tt.method)
			}
		})
	}
}
