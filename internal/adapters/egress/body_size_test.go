package egress_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	egressadapter "github.com/vibewarden/vibewarden/internal/adapters/egress"
	domainegress "github.com/vibewarden/vibewarden/internal/domain/egress"
)

// newTestProxyWithLimits creates a Proxy configured with the given routes,
// default body size limit, and default response size limit.
func newTestProxyWithLimits(
	t *testing.T,
	routes []domainegress.Route,
	client *http.Client,
	defaultBodyLimit int64,
	defaultRespLimit int64,
) *egressadapter.Proxy {
	t.Helper()
	resolver := egressadapter.NewRouteResolver(routes)
	cfg := egressadapter.ProxyConfig{
		Listen:                   "127.0.0.1:0",
		DefaultPolicy:            domainegress.PolicyDeny,
		DefaultTimeout:           5 * time.Second,
		Routes:                   routes,
		DefaultBodySizeLimit:     defaultBodyLimit,
		DefaultResponseSizeLimit: defaultRespLimit,
	}
	return egressadapter.NewProxy(cfg, resolver, client, nil)
}

// TestHandleRequest_RequestBodyLimit_ContentLengthExceeds verifies that a
// request with Content-Length exceeding the limit is rejected with
// ErrRequestBodyTooLarge before any upstream call is made.
func TestHandleRequest_RequestBodyLimit_ContentLengthExceeds(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("upstream must not be called when Content-Length exceeds body size limit")
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	route := newTestRoute(t, "api", upstream.URL+"/v1/*",
		domainegress.WithBodySizeLimit(100),
	)
	proxy := newTestProxyWithLimits(t, []domainegress.Route{route}, upstream.Client(), 0, 0)

	headers := make(http.Header)
	headers.Set("Content-Length", "200")
	req, err := domainegress.NewEgressRequest("POST", upstream.URL+"/v1/data", headers, strings.NewReader("x"))
	if err != nil {
		t.Fatalf("NewEgressRequest: %v", err)
	}

	_, gotErr := proxy.HandleRequest(context.Background(), req)
	if gotErr == nil {
		t.Fatal("HandleRequest should return ErrRequestBodyTooLarge")
	}
	if gotErr != egressadapter.ErrRequestBodyTooLarge {
		t.Errorf("error = %v, want ErrRequestBodyTooLarge", gotErr)
	}
}

// TestHandleRequest_RequestBodyLimit_BodyExceeds verifies that a request
// body that actually exceeds the limit during reading is rejected.
func TestHandleRequest_RequestBodyLimit_BodyExceeds(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The upstream may or may not be called depending on when the read happens.
		_, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	limit := int64(10)
	body := strings.NewReader(strings.Repeat("x", 100)) // 100 bytes > 10 limit

	route := newTestRoute(t, "api", upstream.URL+"/v1/*",
		domainegress.WithBodySizeLimit(limit),
	)
	proxy := newTestProxyWithLimits(t, []domainegress.Route{route}, upstream.Client(), 0, 0)

	req, err := domainegress.NewEgressRequest("POST", upstream.URL+"/v1/data", nil, body)
	if err != nil {
		t.Fatalf("NewEgressRequest: %v", err)
	}

	_, gotErr := proxy.HandleRequest(context.Background(), req)
	if gotErr == nil {
		t.Fatal("HandleRequest should return an error when body exceeds limit")
	}
	if gotErr != egressadapter.ErrRequestBodyTooLarge {
		t.Errorf("error = %v, want ErrRequestBodyTooLarge", gotErr)
	}
}

// TestHandleRequest_RequestBodyLimit_BelowLimit verifies that a request body
// within the limit is forwarded successfully.
func TestHandleRequest_RequestBodyLimit_BelowLimit(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		w.Header().Set("X-Received-Bytes", fmt.Sprintf("%d", len(b)))
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	body := strings.NewReader("short") // 5 bytes < 100 limit

	route := newTestRoute(t, "api", upstream.URL+"/v1/*",
		domainegress.WithBodySizeLimit(100),
	)
	proxy := newTestProxyWithLimits(t, []domainegress.Route{route}, upstream.Client(), 0, 0)

	req, err := domainegress.NewEgressRequest("POST", upstream.URL+"/v1/data", nil, body)
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
}

// TestHandleRequest_DefaultBodySizeLimit verifies that the proxy-level default
// body size limit applies when no per-route limit is configured.
func TestHandleRequest_DefaultBodySizeLimit(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("upstream must not be called when body exceeds default limit")
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	route := newTestRoute(t, "api", upstream.URL+"/v1/*") // no per-route limit
	// Default body limit = 10 bytes; body = 100 bytes.
	proxy := newTestProxyWithLimits(t, []domainegress.Route{route}, upstream.Client(), 10, 0)

	headers := make(http.Header)
	headers.Set("Content-Length", "100")
	req, err := domainegress.NewEgressRequest("POST", upstream.URL+"/v1/data", headers, strings.NewReader(""))
	if err != nil {
		t.Fatalf("NewEgressRequest: %v", err)
	}

	_, gotErr := proxy.HandleRequest(context.Background(), req)
	if gotErr != egressadapter.ErrRequestBodyTooLarge {
		t.Errorf("error = %v, want ErrRequestBodyTooLarge", gotErr)
	}
}

// TestHandleRequest_PerRouteLimitOverridesDefault verifies that a per-route
// body size limit takes precedence over the proxy default.
func TestHandleRequest_PerRouteLimitOverridesDefault(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	// Default = 5 bytes, route = 200 bytes. Body = 100 bytes.
	// The per-route limit (200) is higher, so the request should pass.
	route := newTestRoute(t, "api", upstream.URL+"/v1/*",
		domainegress.WithBodySizeLimit(200),
	)
	proxy := newTestProxyWithLimits(t, []domainegress.Route{route}, upstream.Client(), 5, 0)

	body := strings.NewReader(strings.Repeat("a", 100))
	req, err := domainegress.NewEgressRequest("POST", upstream.URL+"/v1/data", nil, body)
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
}

// TestHTTPHandler_RequestBodyLimit_Returns413 verifies that the HTTP handler
// returns 413 when the request body exceeds the size limit.
func TestHTTPHandler_RequestBodyLimit_Returns413(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("upstream must not be called")
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	route := newTestRoute(t, "api", upstream.URL+"/v1/*",
		domainegress.WithBodySizeLimit(10),
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

	body := strings.NewReader(strings.Repeat("x", 100)) // 100 bytes > 10 limit
	httpReq, err := http.NewRequest(http.MethodPost, proxyURL, body)
	if err != nil {
		t.Fatalf("http.NewRequest: %v", err)
	}
	httpReq.Header.Set("X-Egress-URL", upstream.URL+"/v1/data")
	httpReq.ContentLength = 100

	resp, err := proxyClient.Do(httpReq)
	if err != nil {
		t.Fatalf("proxy request: %v", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Errorf("StatusCode = %d, want %d", resp.StatusCode, http.StatusRequestEntityTooLarge)
	}
}

// TestHTTPHandler_ResponseSizeLimit_Truncates verifies that a response body
// larger than the limit is truncated and the X-Egress-Response-Truncated
// trailer is set.
func TestHTTPHandler_ResponseSizeLimit_Truncates(t *testing.T) {
	const limit = 10
	const fullBody = "hello world this is more than ten bytes"

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(fullBody))
	}))
	defer upstream.Close()

	route := newTestRoute(t, "api", upstream.URL+"/v1/*",
		domainegress.WithResponseSizeLimit(limit),
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
	httpReq.Header.Set("X-Egress-URL", upstream.URL+"/v1/data")

	resp, err := proxyClient.Do(httpReq)
	if err != nil {
		t.Fatalf("proxy request: %v", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		t.Errorf("StatusCode = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	bodyBytes, _ := io.ReadAll(resp.Body)
	if int64(len(bodyBytes)) > limit {
		t.Errorf("body length = %d, want <= %d (limit)", len(bodyBytes), limit)
	}
	if int64(len(bodyBytes)) != limit {
		t.Errorf("body length = %d, want exactly %d bytes (limit)", len(bodyBytes), limit)
	}

	// The truncation trailer should be present.
	truncated := resp.Trailer.Get("X-Egress-Response-Truncated")
	if truncated != "true" {
		t.Errorf("X-Egress-Response-Truncated trailer = %q, want %q", truncated, "true")
	}
}

// TestHTTPHandler_ResponseSizeLimit_NoTruncation verifies that a response body
// within the limit is passed through unmodified and no truncation header is set.
func TestHTTPHandler_ResponseSizeLimit_NoTruncation(t *testing.T) {
	const body = "short"

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(body))
	}))
	defer upstream.Close()

	route := newTestRoute(t, "api", upstream.URL+"/v1/*",
		domainegress.WithResponseSizeLimit(100),
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
	httpReq.Header.Set("X-Egress-URL", upstream.URL+"/v1/data")

	resp, err := proxyClient.Do(httpReq)
	if err != nil {
		t.Fatalf("proxy request: %v", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	bodyBytes, _ := io.ReadAll(resp.Body)
	if string(bodyBytes) != body {
		t.Errorf("body = %q, want %q", string(bodyBytes), body)
	}

	if resp.Trailer.Get("X-Egress-Response-Truncated") != "" {
		t.Error("X-Egress-Response-Truncated trailer should not be set when body is within limit")
	}
}

// TestHTTPHandler_DefaultResponseSizeLimit verifies that the proxy-level
// default response size limit is applied when no per-route limit is set.
func TestHTTPHandler_DefaultResponseSizeLimit(t *testing.T) {
	const fullBody = "this response body is quite long and will be truncated"

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(fullBody))
	}))
	defer upstream.Close()

	route := newTestRoute(t, "api", upstream.URL+"/v1/*") // no per-route limit
	resolver := egressadapter.NewRouteResolver([]domainegress.Route{route})
	cfg := egressadapter.ProxyConfig{
		Listen:                   "127.0.0.1:0",
		DefaultPolicy:            domainegress.PolicyDeny,
		DefaultTimeout:           5 * time.Second,
		Routes:                   []domainegress.Route{route},
		DefaultResponseSizeLimit: 10,
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
	httpReq.Header.Set("X-Egress-URL", upstream.URL+"/v1/data")

	resp, err := proxyClient.Do(httpReq)
	if err != nil {
		t.Fatalf("proxy request: %v", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	bodyBytes, _ := io.ReadAll(resp.Body)
	if int64(len(bodyBytes)) > 10 {
		t.Errorf("body length = %d, want <= 10 (default response limit)", len(bodyBytes))
	}

	truncated := resp.Trailer.Get("X-Egress-Response-Truncated")
	if truncated != "true" {
		t.Errorf("X-Egress-Response-Truncated trailer = %q, want %q", truncated, "true")
	}
}

// TestHTTPHandler_PerRouteResponseLimitOverridesDefault verifies that a
// per-route response size limit takes precedence over the proxy default.
func TestHTTPHandler_PerRouteResponseLimitOverridesDefault(t *testing.T) {
	const fullBody = "hello world extended body"

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(fullBody))
	}))
	defer upstream.Close()

	// Default = 5 bytes, route = 100 bytes. Body = 25 bytes.
	// Per-route limit (100) is higher than body size, so no truncation.
	route := newTestRoute(t, "api", upstream.URL+"/v1/*",
		domainegress.WithResponseSizeLimit(100),
	)
	resolver := egressadapter.NewRouteResolver([]domainegress.Route{route})
	cfg := egressadapter.ProxyConfig{
		Listen:                   "127.0.0.1:0",
		DefaultPolicy:            domainegress.PolicyDeny,
		DefaultTimeout:           5 * time.Second,
		Routes:                   []domainegress.Route{route},
		DefaultResponseSizeLimit: 5,
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
	httpReq.Header.Set("X-Egress-URL", upstream.URL+"/v1/data")

	resp, err := proxyClient.Do(httpReq)
	if err != nil {
		t.Fatalf("proxy request: %v", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	bodyBytes, _ := io.ReadAll(resp.Body)
	if string(bodyBytes) != fullBody {
		t.Errorf("body = %q, want %q (per-route limit should allow full body)", string(bodyBytes), fullBody)
	}

	if resp.Trailer.Get("X-Egress-Response-Truncated") != "" {
		t.Error("X-Egress-Response-Truncated should not be set when per-route limit allows full body")
	}
}

// TestHTTPHandler_NoSizeLimits verifies that requests and responses are
// unaffected when no size limits are configured.
func TestHTTPHandler_NoSizeLimits(t *testing.T) {
	const body = "response body content"

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(fmt.Sprintf("received %d bytes; response: %s", len(b), body)))
	}))
	defer upstream.Close()

	route := newTestRoute(t, "api", upstream.URL+"/v1/*") // no limits
	proxy := newTestProxyWithLimits(t, []domainegress.Route{route}, upstream.Client(), 0, 0)

	if err := proxy.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer proxy.Stop(context.Background()) //nolint:errcheck

	proxyURL := "http://" + proxy.Addr() + "/"
	proxyClient := &http.Client{Timeout: 5 * time.Second}

	reqBody := strings.NewReader(strings.Repeat("x", 1000))
	httpReq, err := http.NewRequest(http.MethodPost, proxyURL, reqBody)
	if err != nil {
		t.Fatalf("http.NewRequest: %v", err)
	}
	httpReq.Header.Set("X-Egress-URL", upstream.URL+"/v1/data")

	resp, err := proxyClient.Do(httpReq)
	if err != nil {
		t.Fatalf("proxy request: %v", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		t.Errorf("StatusCode = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	respBytes, _ := io.ReadAll(resp.Body)
	if !bytes.Contains(respBytes, []byte("1000 bytes")) {
		t.Errorf("response body %q should contain '1000 bytes'", string(respBytes))
	}
}
