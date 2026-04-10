package egress_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	egressadapter "github.com/vibewarden/vibewarden/internal/adapters/egress"
	domainegress "github.com/vibewarden/vibewarden/internal/domain/egress"
)

// newTestRoute is a helper that creates a Route or fatals the test.
func newTestRoute(t *testing.T, name, pattern string, opts ...domainegress.RouteOption) domainegress.Route {
	t.Helper()
	r, err := domainegress.NewRoute(name, pattern, opts...)
	if err != nil {
		t.Fatalf("NewRoute(%q, %q): %v", name, pattern, err)
	}
	return r
}

// newTestProxy creates a Proxy wired to the given routes and HTTP client.
// The proxy does not start listening — tests call HandleRequest directly
// or drive the HTTP handler via httptest.
// AllowInsecure is set to true so tests using plain-HTTP test servers are not
// blocked by TLS enforcement.
func newTestProxy(t *testing.T, routes []domainegress.Route, client *http.Client, policy domainegress.Policy) *egressadapter.Proxy {
	t.Helper()
	resolver := egressadapter.NewRouteResolver(routes)
	cfg := egressadapter.ProxyConfig{
		Listen:         "127.0.0.1:0",
		DefaultPolicy:  policy,
		DefaultTimeout: 5 * time.Second,
		Routes:         routes,
		AllowInsecure:  true, // test servers are HTTP
	}
	return egressadapter.NewProxy(cfg, resolver, client, nil)
}

// TestHandleRequest_AllowedRoute verifies that a matched request is forwarded
// and the upstream response is returned unchanged.
func TestHandleRequest_AllowedRoute(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Custom", "yes")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("hello"))
	}))
	defer upstream.Close()

	route := newTestRoute(t, "upstream", upstream.URL+"/v1/*")

	proxy := newTestProxy(t, []domainegress.Route{route}, upstream.Client(), domainegress.PolicyDeny)

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
	if resp.Duration <= 0 {
		t.Error("Duration should be > 0")
	}
}

// TestHandleRequest_DeniedByPolicy verifies that an unmatched request is
// blocked with ErrDeniedByPolicy when default_policy is deny.
func TestHandleRequest_DeniedByPolicy(t *testing.T) {
	proxy := newTestProxy(t, nil, nil, domainegress.PolicyDeny)

	req, err := domainegress.NewEgressRequest("GET", "https://api.unknown.example.com/", nil, nil)
	if err != nil {
		t.Fatalf("NewEgressRequest: %v", err)
	}

	_, err = proxy.HandleRequest(context.Background(), req)
	if err == nil {
		t.Fatal("HandleRequest should have returned an error")
	}
	if err != egressadapter.ErrDeniedByPolicy {
		t.Errorf("error = %v, want ErrDeniedByPolicy", err)
	}
}

// TestHandleRequest_AllowPolicy verifies that an unmatched request is forwarded
// when default_policy is allow.
func TestHandleRequest_AllowPolicy(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer upstream.Close()

	proxy := newTestProxy(t, nil, upstream.Client(), domainegress.PolicyAllow)

	req, err := domainegress.NewEgressRequest("GET", upstream.URL+"/anything", nil, nil)
	if err != nil {
		t.Fatalf("NewEgressRequest: %v", err)
	}

	resp, err := proxy.HandleRequest(context.Background(), req)
	if err != nil {
		t.Fatalf("HandleRequest returned unexpected error: %v", err)
	}
	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("StatusCode = %d, want %d", resp.StatusCode, http.StatusNoContent)
	}
}

// TestHTTPHandler_TransparentRouting verifies that a request with the
// X-Egress-URL header is forwarded to the specified target.
func TestHTTPHandler_TransparentRouting(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The proxy must not forward the X-Egress-URL header upstream.
		if r.Header.Get("X-Egress-URL") != "" {
			t.Error("X-Egress-URL header must be stripped before forwarding")
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("transparent"))
	}))
	defer upstream.Close()

	route := newTestRoute(t, "upstream", upstream.URL+"/v1/*")
	resolver := egressadapter.NewRouteResolver([]domainegress.Route{route})
	cfg := egressadapter.ProxyConfig{
		Listen:         "127.0.0.1:0",
		DefaultPolicy:  domainegress.PolicyDeny,
		DefaultTimeout: 5 * time.Second,
		Routes:         []domainegress.Route{route},
		AllowInsecure:  true, // test server is HTTP
	}
	proxy := egressadapter.NewProxy(cfg, resolver, upstream.Client(), nil)

	if err := proxy.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer proxy.Stop(context.Background()) //nolint:errcheck

	// Send a transparent-style request: any path, X-Egress-URL header set.
	proxyURL := "http://" + proxy.Addr() + "/"
	httpReq, err := http.NewRequest(http.MethodGet, proxyURL, nil)
	if err != nil {
		t.Fatalf("http.NewRequest: %v", err)
	}
	httpReq.Header.Set("X-Egress-URL", upstream.URL+"/v1/resource")

	// Use a plain client that speaks to the proxy (not the upstream client).
	proxyClient := &http.Client{Timeout: 5 * time.Second}
	resp, err := proxyClient.Do(httpReq)
	if err != nil {
		t.Fatalf("proxy request: %v", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		t.Errorf("StatusCode = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "transparent" {
		t.Errorf("body = %q, want %q", string(body), "transparent")
	}
}

// TestHTTPHandler_NamedRouting verifies that a /_egress/{route-name}/path
// request is forwarded to the route's configured endpoint.
func TestHTTPHandler_NamedRouting(t *testing.T) {
	var capturedPath string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		w.WriteHeader(http.StatusAccepted)
	}))
	defer upstream.Close()

	// Route pattern: upstream URL with /v1/* — base is upstream URL + "/v1/"
	route := newTestRoute(t, "myapi", upstream.URL+"/v1/*")
	resolver := egressadapter.NewRouteResolver([]domainegress.Route{route})
	cfg := egressadapter.ProxyConfig{
		Listen:         "127.0.0.1:0",
		DefaultPolicy:  domainegress.PolicyDeny,
		DefaultTimeout: 5 * time.Second,
		Routes:         []domainegress.Route{route},
		AllowInsecure:  true, // test server is HTTP
	}
	proxy := egressadapter.NewProxy(cfg, resolver, upstream.Client(), nil)

	if err := proxy.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer proxy.Stop(context.Background()) //nolint:errcheck

	proxyURL := "http://" + proxy.Addr() + "/_egress/myapi/charges"
	proxyClient := &http.Client{Timeout: 5 * time.Second}
	resp, err := proxyClient.Get(proxyURL)
	if err != nil {
		t.Fatalf("proxy request: %v", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusAccepted {
		t.Errorf("StatusCode = %d, want %d", resp.StatusCode, http.StatusAccepted)
	}
	if capturedPath != "/v1/charges" {
		t.Errorf("upstream received path %q, want %q", capturedPath, "/v1/charges")
	}
}

// TestHTTPHandler_BlockedRequest verifies that an unmatched request returns 403
// when default_policy is deny.
func TestHTTPHandler_BlockedRequest(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("upstream must not be called for blocked requests")
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	// No routes configured — all requests will be unmatched.
	resolver := egressadapter.NewRouteResolver(nil)
	cfg := egressadapter.ProxyConfig{
		Listen:         "127.0.0.1:0",
		DefaultPolicy:  domainegress.PolicyDeny,
		DefaultTimeout: 5 * time.Second,
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
	httpReq.Header.Set("X-Egress-URL", upstream.URL+"/blocked")

	resp, err := proxyClient.Do(httpReq)
	if err != nil {
		t.Fatalf("proxy request: %v", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("StatusCode = %d, want %d", resp.StatusCode, http.StatusForbidden)
	}
}

// TestHTTPHandler_MethodAndBodyPreserved verifies that the HTTP method, request
// body, and custom headers are preserved when forwarding to the upstream.
func TestHTTPHandler_MethodAndBodyPreserved(t *testing.T) {
	var (
		capturedMethod string
		capturedBody   string
		capturedHeader string
	)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedMethod = r.Method
		b, _ := io.ReadAll(r.Body)
		capturedBody = string(b)
		capturedHeader = r.Header.Get("X-Custom-Header")
		w.WriteHeader(http.StatusCreated)
	}))
	defer upstream.Close()

	route := newTestRoute(t, "upstream", upstream.URL+"/v1/*")
	resolver := egressadapter.NewRouteResolver([]domainegress.Route{route})
	cfg := egressadapter.ProxyConfig{
		Listen:         "127.0.0.1:0",
		DefaultPolicy:  domainegress.PolicyDeny,
		DefaultTimeout: 5 * time.Second,
		Routes:         []domainegress.Route{route},
		AllowInsecure:  true, // test server is HTTP
	}
	proxy := egressadapter.NewProxy(cfg, resolver, upstream.Client(), nil)

	if err := proxy.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer proxy.Stop(context.Background()) //nolint:errcheck

	proxyURL := "http://" + proxy.Addr() + "/"
	proxyClient := &http.Client{Timeout: 5 * time.Second}

	bodyStr := `{"amount":100}`
	httpReq, err := http.NewRequest(http.MethodPost, proxyURL, strings.NewReader(bodyStr))
	if err != nil {
		t.Fatalf("http.NewRequest: %v", err)
	}
	httpReq.Header.Set("X-Egress-URL", upstream.URL+"/v1/charges")
	httpReq.Header.Set("X-Custom-Header", "my-value")

	resp, err := proxyClient.Do(httpReq)
	if err != nil {
		t.Fatalf("proxy request: %v", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusCreated {
		t.Errorf("StatusCode = %d, want %d", resp.StatusCode, http.StatusCreated)
	}
	if capturedMethod != http.MethodPost {
		t.Errorf("upstream method = %q, want POST", capturedMethod)
	}
	if capturedBody != bodyStr {
		t.Errorf("upstream body = %q, want %q", capturedBody, bodyStr)
	}
	if capturedHeader != "my-value" {
		t.Errorf("upstream X-Custom-Header = %q, want %q", capturedHeader, "my-value")
	}
}

// TestHTTPHandler_MissingTargetURL verifies that a request without a target URL
// returns 400 Bad Request.
func TestHTTPHandler_MissingTargetURL(t *testing.T) {
	resolver := egressadapter.NewRouteResolver(nil)
	cfg := egressadapter.ProxyConfig{
		Listen:         "127.0.0.1:0",
		DefaultPolicy:  domainegress.PolicyDeny,
		DefaultTimeout: 5 * time.Second,
	}
	proxy := egressadapter.NewProxy(cfg, resolver, nil, nil)

	if err := proxy.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer proxy.Stop(context.Background()) //nolint:errcheck

	proxyURL := "http://" + proxy.Addr() + "/"
	proxyClient := &http.Client{Timeout: 5 * time.Second}

	resp, err := proxyClient.Get(proxyURL)
	if err != nil {
		t.Fatalf("proxy request: %v", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("StatusCode = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
}

// TestHTTPHandler_GracefulShutdown verifies that Stop terminates the server
// without error when called after Start.
func TestHTTPHandler_GracefulShutdown(t *testing.T) {
	resolver := egressadapter.NewRouteResolver(nil)
	cfg := egressadapter.ProxyConfig{
		Listen:         "127.0.0.1:0",
		DefaultPolicy:  domainegress.PolicyDeny,
		DefaultTimeout: 5 * time.Second,
	}
	proxy := egressadapter.NewProxy(cfg, resolver, nil, nil)

	if err := proxy.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := proxy.Stop(ctx); err != nil {
		t.Errorf("Stop returned error: %v", err)
	}
}

// TestHTTPHandler_ResponseHeadersPreserved verifies that response headers from
// the upstream are forwarded to the caller.
func TestHTTPHandler_ResponseHeadersPreserved(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Rate-Limit-Remaining", "42")
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	route := newTestRoute(t, "upstream", upstream.URL+"/v1/*")
	resolver := egressadapter.NewRouteResolver([]domainegress.Route{route})
	cfg := egressadapter.ProxyConfig{
		Listen:         "127.0.0.1:0",
		DefaultPolicy:  domainegress.PolicyDeny,
		DefaultTimeout: 5 * time.Second,
		Routes:         []domainegress.Route{route},
		AllowInsecure:  true, // test server is HTTP
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

	if got := resp.Header.Get("X-Rate-Limit-Remaining"); got != "42" {
		t.Errorf("X-Rate-Limit-Remaining = %q, want %q", got, "42")
	}
}

// TestHandleRequest_SSRFGuard_BlocksPrivateIP verifies that HandleRequest returns
// an error wrapping SSRFBlockedError when the egress target resolves to a private
// IP address and block_private is true.
func TestHandleRequest_SSRFGuard_BlocksPrivateIP(t *testing.T) {
	guard, err := egressadapter.NewSSRFGuard(egressadapter.SSRFGuardConfig{
		BlockPrivate: true,
	})
	if err != nil {
		t.Fatalf("NewSSRFGuard: %v", err)
	}

	// Use a route that matches a target we can control.
	// We point it at 127.0.0.1, which the guard must block.
	resolver := egressadapter.NewRouteResolver(nil)
	cfg := egressadapter.ProxyConfig{
		Listen:         "127.0.0.1:0",
		DefaultPolicy:  domainegress.PolicyAllow, // allow so the route check passes
		DefaultTimeout: 5 * time.Second,
		SSRFGuard:      guard,
		AllowInsecure:  true, // TLS enforcement is not what this test exercises
	}
	proxy := egressadapter.NewProxy(cfg, resolver, nil, nil)

	req, err := domainegress.NewEgressRequest("GET", "http://127.0.0.1:9999/test", nil, nil)
	if err != nil {
		t.Fatalf("NewEgressRequest: %v", err)
	}

	_, handleErr := proxy.HandleRequest(context.Background(), req)
	if handleErr == nil {
		t.Fatal("HandleRequest should have returned an error for a private IP target")
	}

	var ssrfErr *egressadapter.SSRFBlockedError
	if !errors.As(handleErr, &ssrfErr) {
		t.Errorf("HandleRequest error = %v; want wrapped SSRFBlockedError", handleErr)
	}
}

// TestHTTPHandler_SSRFGuard_Returns403 verifies that the HTTP handler returns
// 403 Forbidden when SSRF protection blocks the request.
func TestHTTPHandler_SSRFGuard_Returns403(t *testing.T) {
	guard, err := egressadapter.NewSSRFGuard(egressadapter.SSRFGuardConfig{
		BlockPrivate: true,
	})
	if err != nil {
		t.Fatalf("NewSSRFGuard: %v", err)
	}

	resolver := egressadapter.NewRouteResolver(nil)
	cfg := egressadapter.ProxyConfig{
		Listen:         "127.0.0.1:0",
		DefaultPolicy:  domainegress.PolicyAllow,
		DefaultTimeout: 5 * time.Second,
		SSRFGuard:      guard,
		AllowInsecure:  true, // TLS enforcement is not what this test exercises
	}
	proxy := egressadapter.NewProxy(cfg, resolver, nil, nil)

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
	// Target a loopback address — the SSRF guard must block it.
	httpReq.Header.Set("X-Egress-URL", "http://127.0.0.1:9999/internal/api")

	resp, err := proxyClient.Do(httpReq)
	if err != nil {
		t.Fatalf("proxy request: %v", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("StatusCode = %d, want %d (SSRF should be blocked)", resp.StatusCode, http.StatusForbidden)
	}

	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "SSRF") {
		t.Errorf("response body %q should mention SSRF", string(body))
	}
}

// TestHTTPHandler_SSRFGuard_AllowedPrivateExemption verifies that a private IP
// in the allowed_private list is not blocked.
func TestHTTPHandler_SSRFGuard_AllowedPrivateExemption(t *testing.T) {
	// Start a test server on loopback (which would normally be blocked).
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("internal"))
	}))
	defer upstream.Close()

	// Extract the upstream host IP to add to allowed_private.
	// httptest.NewServer binds to 127.0.0.1.
	guard, err := egressadapter.NewSSRFGuard(egressadapter.SSRFGuardConfig{
		BlockPrivate:   true,
		AllowedPrivate: []string{"127.0.0.0/8"},
	})
	if err != nil {
		t.Fatalf("NewSSRFGuard: %v", err)
	}

	route := newTestRoute(t, "internal", upstream.URL+"/api/*")
	resolver := egressadapter.NewRouteResolver([]domainegress.Route{route})
	cfg := egressadapter.ProxyConfig{
		Listen:         "127.0.0.1:0",
		DefaultPolicy:  domainegress.PolicyDeny,
		DefaultTimeout: 5 * time.Second,
		Routes:         []domainegress.Route{route},
		SSRFGuard:      guard,
		AllowInsecure:  true, // TLS enforcement is not what this test exercises
	}
	proxy := egressadapter.NewProxy(cfg, resolver, nil, nil)

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
	httpReq.Header.Set("X-Egress-URL", upstream.URL+"/api/resource")

	resp, err := proxyClient.Do(httpReq)
	if err != nil {
		t.Fatalf("proxy request: %v", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		t.Errorf("StatusCode = %d, want %d (exempted private IP should be allowed)", resp.StatusCode, http.StatusOK)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "internal" {
		t.Errorf("body = %q, want %q", string(body), "internal")
	}
}
