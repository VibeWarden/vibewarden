package egress_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	egressadapter "github.com/vibewarden/vibewarden/internal/adapters/egress"
	domainegress "github.com/vibewarden/vibewarden/internal/domain/egress"
)

// TestHandleRequest_HeaderInjection verifies that headers configured via
// InjectHeaders are added to the outbound request before forwarding.
func TestHandleRequest_HeaderInjection(t *testing.T) {
	var capturedAPIKey string

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedAPIKey = r.Header.Get("X-Api-Key")
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	hdrCfg := domainegress.HeadersConfig{
		InjectHeaders: map[string]string{"X-Api-Key": "injected-key"},
	}
	route := newTestRoute(t, "api", upstream.URL+"/v1/*", domainegress.WithHeaders(hdrCfg))
	proxy := newTestProxy(t, []domainegress.Route{route}, upstream.Client(), domainegress.PolicyDeny)

	req, err := domainegress.NewEgressRequest("GET", upstream.URL+"/v1/resource", nil, nil)
	if err != nil {
		t.Fatalf("NewEgressRequest: %v", err)
	}

	if _, err := proxy.HandleRequest(context.Background(), req); err != nil {
		t.Fatalf("HandleRequest: %v", err)
	}

	if capturedAPIKey != "injected-key" {
		t.Errorf("upstream received X-Api-Key = %q, want %q", capturedAPIKey, "injected-key")
	}
}

// TestHandleRequest_RequestHeaderStripping verifies that headers in
// StripRequestHeaders are removed before the request is forwarded.
func TestHandleRequest_RequestHeaderStripping(t *testing.T) {
	var capturedCookie string

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedCookie = r.Header.Get("Cookie")
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	hdrCfg := domainegress.HeadersConfig{
		StripRequestHeaders: []string{"Cookie"},
	}
	route := newTestRoute(t, "api", upstream.URL+"/v1/*", domainegress.WithHeaders(hdrCfg))
	proxy := newTestProxy(t, []domainegress.Route{route}, upstream.Client(), domainegress.PolicyDeny)

	incoming := http.Header{"Cookie": []string{"session=abc123"}}
	req, err := domainegress.NewEgressRequest("GET", upstream.URL+"/v1/resource", incoming, nil)
	if err != nil {
		t.Fatalf("NewEgressRequest: %v", err)
	}

	if _, err := proxy.HandleRequest(context.Background(), req); err != nil {
		t.Fatalf("HandleRequest: %v", err)
	}

	if capturedCookie != "" {
		t.Errorf("upstream received Cookie = %q, want it stripped", capturedCookie)
	}
}

// TestHandleRequest_XInjectSecretAlwaysStripped verifies that X-Inject-Secret
// is never forwarded upstream, even when no explicit StripRequestHeaders is set.
// When X-Inject-Secret is present the proxy performs dynamic injection using the
// configured SecretInjector; the header itself is always stripped from the outbound
// request before forwarding.
func TestHandleRequest_XInjectSecretAlwaysStripped(t *testing.T) {
	var capturedSecret string

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedSecret = r.Header.Get("X-Inject-Secret")
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	// Provide an injector with the secret the dynamic header will request.
	store := newFakeSecretStore(map[string]map[string]string{
		"my-secret-name": {"value": "injected-value"},
	})
	injector := egressadapter.NewSecretInjector(store, egressadapter.SecretInjectorConfig{})

	// No explicit header config — X-Inject-Secret triggers dynamic injection.
	route := newTestRoute(t, "api", upstream.URL+"/v1/*")
	resolver := egressadapter.NewRouteResolver([]domainegress.Route{route})
	cfg := egressadapter.ProxyConfig{
		AllowInsecure:  true, // test server is HTTP
		Listen:         "127.0.0.1:0",
		DefaultPolicy:  domainegress.PolicyDeny,
		DefaultTimeout: 5 * time.Second,
		Routes:         []domainegress.Route{route},
		SecretInjector: injector,
	}
	proxy := egressadapter.NewProxy(cfg, resolver, upstream.Client(), nil)

	incoming := http.Header{"X-Inject-Secret": []string{"my-secret-name"}}
	req, err := domainegress.NewEgressRequest("GET", upstream.URL+"/v1/resource", incoming, nil)
	if err != nil {
		t.Fatalf("NewEgressRequest: %v", err)
	}

	if _, err := proxy.HandleRequest(context.Background(), req); err != nil {
		t.Fatalf("HandleRequest: %v", err)
	}

	if capturedSecret != "" {
		t.Errorf("upstream received X-Inject-Secret = %q, want it stripped", capturedSecret)
	}
}

// TestHandleRequest_XInjectSecretStrippedOnAllowPolicy verifies that when
// X-Inject-Secret is present on an allow-policy (unmatched) request, the proxy
// performs dynamic injection — resolving the secret and injecting it as the
// Authorization header — and always strips X-Inject-Secret before forwarding.
func TestHandleRequest_XInjectSecretStrippedOnAllowPolicy(t *testing.T) {
	var capturedSecret string

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedSecret = r.Header.Get("X-Inject-Secret")
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	store := newFakeSecretStore(map[string]map[string]string{
		"leaked-secret": {"value": "safe-injected-value"},
	})
	injector := egressadapter.NewSecretInjector(store, egressadapter.SecretInjectorConfig{})

	// No routes — request falls through to allow policy, but injection still happens.
	resolver := egressadapter.NewRouteResolver(nil)
	cfg := egressadapter.ProxyConfig{
		AllowInsecure:  true, // test server is HTTP
		Listen:         "127.0.0.1:0",
		DefaultPolicy:  domainegress.PolicyAllow,
		DefaultTimeout: 5 * time.Second,
		SecretInjector: injector,
	}
	proxy := egressadapter.NewProxy(cfg, resolver, upstream.Client(), nil)

	incoming := http.Header{"X-Inject-Secret": []string{"leaked-secret"}}
	req, err := domainegress.NewEgressRequest("GET", upstream.URL+"/anything", incoming, nil)
	if err != nil {
		t.Fatalf("NewEgressRequest: %v", err)
	}

	if _, err := proxy.HandleRequest(context.Background(), req); err != nil {
		t.Fatalf("HandleRequest: %v", err)
	}

	if capturedSecret != "" {
		t.Errorf("upstream received X-Inject-Secret = %q, want it stripped (allow policy)", capturedSecret)
	}
}

// TestHandleRequest_ResponseHeaderStripping verifies that headers in
// StripResponseHeaders are removed from the upstream response.
func TestHandleRequest_ResponseHeaderStripping(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Internal-Trace", "trace-id-abc")
		w.Header().Set("X-Rate-Limit", "100")
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	hdrCfg := domainegress.HeadersConfig{
		StripResponseHeaders: []string{"X-Internal-Trace"},
	}
	route := newTestRoute(t, "api", upstream.URL+"/v1/*", domainegress.WithHeaders(hdrCfg))
	proxy := newTestProxy(t, []domainegress.Route{route}, upstream.Client(), domainegress.PolicyDeny)

	req, err := domainegress.NewEgressRequest("GET", upstream.URL+"/v1/resource", nil, nil)
	if err != nil {
		t.Fatalf("NewEgressRequest: %v", err)
	}

	resp, err := proxy.HandleRequest(context.Background(), req)
	if err != nil {
		t.Fatalf("HandleRequest: %v", err)
	}

	if v := resp.Header.Get("X-Internal-Trace"); v != "" {
		t.Errorf("response Header X-Internal-Trace = %q, want stripped", v)
	}
	if v := resp.Header.Get("X-Rate-Limit"); v != "100" {
		t.Errorf("response Header X-Rate-Limit = %q, want %q", v, "100")
	}
}

// TestHandleRequest_DefaultSensitiveResponseHeadersStripped verifies that
// Server and X-Powered-By are stripped from every response, even with no
// per-route header config.
func TestHandleRequest_DefaultSensitiveResponseHeadersStripped(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Server", "Apache/2.4.51")
		w.Header().Set("X-Powered-By", "PHP/8.1")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	// No HeadersConfig set on route — default stripping must still apply.
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

	if v := resp.Header.Get("Server"); v != "" {
		t.Errorf("response Header Server = %q, want stripped", v)
	}
	if v := resp.Header.Get("X-Powered-By"); v != "" {
		t.Errorf("response Header X-Powered-By = %q, want stripped", v)
	}
	if v := resp.Header.Get("Content-Type"); v != "application/json" {
		t.Errorf("response Header Content-Type = %q, want %q", v, "application/json")
	}
}

// TestHandleRequest_DefaultSensitiveHeadersStrippedOnAllowPolicy verifies
// that Server and X-Powered-By are stripped even for unmatched allow-policy requests.
func TestHandleRequest_DefaultSensitiveHeadersStrippedOnAllowPolicy(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Server", "nginx/1.25")
		w.Header().Set("X-Powered-By", "Express")
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	// No routes — falls through to allow policy.
	proxy := newTestProxy(t, nil, upstream.Client(), domainegress.PolicyAllow)

	req, err := domainegress.NewEgressRequest("GET", upstream.URL+"/anything", nil, nil)
	if err != nil {
		t.Fatalf("NewEgressRequest: %v", err)
	}

	resp, err := proxy.HandleRequest(context.Background(), req)
	if err != nil {
		t.Fatalf("HandleRequest: %v", err)
	}

	if v := resp.Header.Get("Server"); v != "" {
		t.Errorf("response Header Server = %q, want stripped (allow policy)", v)
	}
	if v := resp.Header.Get("X-Powered-By"); v != "" {
		t.Errorf("response Header X-Powered-By = %q, want stripped (allow policy)", v)
	}
}

// TestHTTPHandler_InjectHeaderEndToEnd verifies header injection through the
// full HTTP handler path (Start + real HTTP client).
func TestHTTPHandler_InjectHeaderEndToEnd(t *testing.T) {
	var capturedAPIKey string

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedAPIKey = r.Header.Get("X-Api-Key")
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	hdrCfg := domainegress.HeadersConfig{
		InjectHeaders: map[string]string{"X-Api-Key": "e2e-secret"},
	}
	route := newTestRoute(t, "api", upstream.URL+"/v1/*", domainegress.WithHeaders(hdrCfg))
	resolver := egressadapter.NewRouteResolver([]domainegress.Route{route})
	cfg := egressadapter.ProxyConfig{
		AllowInsecure:  true, // test server is HTTP
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
	if capturedAPIKey != "e2e-secret" {
		t.Errorf("upstream received X-Api-Key = %q, want %q", capturedAPIKey, "e2e-secret")
	}
}

// TestHTTPHandler_SensitiveResponseHeadersStrippedEndToEnd verifies that
// Server and X-Powered-By are stripped from the response seen by the caller
// via the full HTTP handler path.
func TestHTTPHandler_SensitiveResponseHeadersStrippedEndToEnd(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Server", "nginx/1.25.3")
		w.Header().Set("X-Powered-By", "PHP/8.2")
		w.Header().Set("X-Keep", "kept")
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	route := newTestRoute(t, "api", upstream.URL+"/v1/*")
	resolver := egressadapter.NewRouteResolver([]domainegress.Route{route})
	cfg := egressadapter.ProxyConfig{
		AllowInsecure:  true, // test server is HTTP
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

	if v := resp.Header.Get("Server"); v != "" {
		t.Errorf("caller received Server = %q, want stripped", v)
	}
	if v := resp.Header.Get("X-Powered-By"); v != "" {
		t.Errorf("caller received X-Powered-By = %q, want stripped", v)
	}
	if v := resp.Header.Get("X-Keep"); v != "kept" {
		t.Errorf("caller received X-Keep = %q, want %q", v, "kept")
	}
}
