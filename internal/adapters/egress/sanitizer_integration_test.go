package egress_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	egressadapter "github.com/vibewarden/vibewarden/internal/adapters/egress"
	domainegress "github.com/vibewarden/vibewarden/internal/domain/egress"
)

// TestHandleRequest_Sanitize_QueryParamsStripped verifies that query parameters
// listed in sanitize.query_params are removed before the request reaches the
// upstream, and an egress.sanitized event is emitted.
func TestHandleRequest_Sanitize_QueryParamsStripped(t *testing.T) {
	var capturedURLQuery string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedURLQuery = r.URL.RawQuery
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	logger := &fakeEventLogger{}
	route := newTestRoute(t, "api", upstream.URL+"/v1/*",
		domainegress.WithSanitize(domainegress.SanitizeConfig{
			QueryParams: []string{"api_key"},
		}),
	)
	cfg := egressadapter.ProxyConfig{
		Listen:         "127.0.0.1:0",
		DefaultPolicy:  domainegress.PolicyDeny,
		DefaultTimeout: 5 * time.Second,
		Routes:         []domainegress.Route{route},
		AllowInsecure:  true,
		EventLogger:    logger,
	}
	resolver := egressadapter.NewRouteResolver([]domainegress.Route{route})
	proxy := egressadapter.NewProxy(cfg, resolver, upstream.Client(), nil)

	reqURL := upstream.URL + "/v1/resource?api_key=secret&amount=100"
	req, err := domainegress.NewEgressRequest("GET", reqURL, nil, nil)
	if err != nil {
		t.Fatalf("NewEgressRequest: %v", err)
	}

	resp, err := proxy.HandleRequest(context.Background(), req)
	if err != nil {
		t.Fatalf("HandleRequest: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("StatusCode = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	// The upstream must not have seen api_key.
	if strings.Contains(capturedURLQuery, "api_key") {
		t.Errorf("upstream received api_key in query: %s", capturedURLQuery)
	}
	// But amount must still be there.
	if !strings.Contains(capturedURLQuery, "amount=100") {
		t.Errorf("upstream query is missing amount=100: %s", capturedURLQuery)
	}

	// An egress.sanitized event must have been emitted.
	logged := logger.logged
	var sanitizedFound bool
	for _, ev := range logged {
		if ev.EventType == "egress.sanitized" {
			sanitizedFound = true
			if got := ev.Payload["stripped_query_params"]; got != 1 {
				t.Errorf("stripped_query_params = %v, want 1", got)
			}
		}
	}
	if !sanitizedFound {
		t.Fatal("no egress.sanitized event was emitted")
	}
}

// TestHandleRequest_Sanitize_JSONBodyFieldsRedacted verifies that body fields
// listed in sanitize.body_fields are replaced with "[REDACTED]" before forwarding.
func TestHandleRequest_Sanitize_JSONBodyFieldsRedacted(t *testing.T) {
	var capturedBody string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		capturedBody = string(b)
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	route := newTestRoute(t, "api", upstream.URL+"/v1/*",
		domainegress.WithSanitize(domainegress.SanitizeConfig{
			BodyFields: []string{"password", "ssn"},
		}),
	)
	cfg := egressadapter.ProxyConfig{
		Listen:         "127.0.0.1:0",
		DefaultPolicy:  domainegress.PolicyDeny,
		DefaultTimeout: 5 * time.Second,
		Routes:         []domainegress.Route{route},
		AllowInsecure:  true,
	}
	resolver := egressadapter.NewRouteResolver([]domainegress.Route{route})
	proxy := egressadapter.NewProxy(cfg, resolver, upstream.Client(), nil)

	bodyStr := `{"username":"alice","password":"hunter2","ssn":"123-45-6789"}`
	headers := http.Header{"Content-Type": []string{"application/json"}}
	req, err := domainegress.NewEgressRequest("POST", upstream.URL+"/v1/login", headers,
		io.NopCloser(strings.NewReader(bodyStr)),
	)
	if err != nil {
		t.Fatalf("NewEgressRequest: %v", err)
	}

	_, err = proxy.HandleRequest(context.Background(), req)
	if err != nil {
		t.Fatalf("HandleRequest: %v", err)
	}

	if strings.Contains(capturedBody, "hunter2") {
		t.Errorf("upstream received original password in body: %s", capturedBody)
	}
	if strings.Contains(capturedBody, "123-45-6789") {
		t.Errorf("upstream received original SSN in body: %s", capturedBody)
	}
	if !strings.Contains(capturedBody, "[REDACTED]") {
		t.Errorf("upstream body does not contain [REDACTED]: %s", capturedBody)
	}
	if !strings.Contains(capturedBody, "alice") {
		t.Errorf("upstream body should still contain username: %s", capturedBody)
	}
}

// TestHandleRequest_Sanitize_HeadersRedactedInEvent verifies that request
// headers listed in sanitize.headers appear as "[REDACTED]" in the
// egress.sanitized event, while the actual forwarded request preserves the
// original values.
func TestHandleRequest_Sanitize_HeadersRedactedInEvent(t *testing.T) {
	var capturedAuthHeader string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedAuthHeader = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	logger := &fakeEventLogger{}
	route := newTestRoute(t, "api", upstream.URL+"/v1/*",
		domainegress.WithSanitize(domainegress.SanitizeConfig{
			Headers: []string{"Authorization"},
		}),
	)
	cfg := egressadapter.ProxyConfig{
		Listen:         "127.0.0.1:0",
		DefaultPolicy:  domainegress.PolicyDeny,
		DefaultTimeout: 5 * time.Second,
		Routes:         []domainegress.Route{route},
		AllowInsecure:  true,
		EventLogger:    logger,
	}
	resolver := egressadapter.NewRouteResolver([]domainegress.Route{route})
	proxy := egressadapter.NewProxy(cfg, resolver, upstream.Client(), nil)

	headers := http.Header{"Authorization": []string{"Bearer secrettoken"}}
	req, err := domainegress.NewEgressRequest("GET", upstream.URL+"/v1/resource", headers, nil)
	if err != nil {
		t.Fatalf("NewEgressRequest: %v", err)
	}

	_, err = proxy.HandleRequest(context.Background(), req)
	if err != nil {
		t.Fatalf("HandleRequest: %v", err)
	}

	// Upstream must receive the real Authorization header (not redacted).
	if capturedAuthHeader != "Bearer secrettoken" {
		t.Errorf("upstream received Authorization = %q, want original value", capturedAuthHeader)
	}

	// The egress.sanitized event must report 1 redacted header.
	var sanitizedFound bool
	for _, ev := range logger.logged {
		if ev.EventType == "egress.sanitized" {
			sanitizedFound = true
			if got := ev.Payload["redacted_headers"]; got != 1 {
				t.Errorf("redacted_headers = %v, want 1", got)
			}
		}
	}
	if !sanitizedFound {
		t.Fatal("no egress.sanitized event was emitted")
	}
}

// TestHandleRequest_Sanitize_NoEventWhenNothingRedacted verifies that no
// egress.sanitized event is emitted when none of the configured rules matched.
func TestHandleRequest_Sanitize_NoEventWhenNothingRedacted(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	logger := &fakeEventLogger{}
	route := newTestRoute(t, "api", upstream.URL+"/v1/*",
		domainegress.WithSanitize(domainegress.SanitizeConfig{
			QueryParams: []string{"api_key"}, // request has no api_key
		}),
	)
	cfg := egressadapter.ProxyConfig{
		Listen:         "127.0.0.1:0",
		DefaultPolicy:  domainegress.PolicyDeny,
		DefaultTimeout: 5 * time.Second,
		Routes:         []domainegress.Route{route},
		AllowInsecure:  true,
		EventLogger:    logger,
	}
	resolver := egressadapter.NewRouteResolver([]domainegress.Route{route})
	proxy := egressadapter.NewProxy(cfg, resolver, upstream.Client(), nil)

	req, err := domainegress.NewEgressRequest("GET", upstream.URL+"/v1/resource", nil, nil)
	if err != nil {
		t.Fatalf("NewEgressRequest: %v", err)
	}

	_, err = proxy.HandleRequest(context.Background(), req)
	if err != nil {
		t.Fatalf("HandleRequest: %v", err)
	}

	for _, ev := range logger.logged {
		if ev.EventType == "egress.sanitized" {
			t.Errorf("unexpected egress.sanitized event emitted when nothing was redacted: %v", ev)
		}
	}
}

// TestHTTPHandler_Sanitize_EndToEnd exercises the full HTTP handler path with
// sanitization rules active, verifying that query params are stripped at the
// HTTP layer (transparent routing).
func TestHTTPHandler_Sanitize_EndToEnd(t *testing.T) {
	var capturedQuery string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedQuery = r.URL.RawQuery
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	route := newTestRoute(t, "api", upstream.URL+"/v1/*",
		domainegress.WithSanitize(domainegress.SanitizeConfig{
			QueryParams: []string{"token"},
		}),
	)
	resolver := egressadapter.NewRouteResolver([]domainegress.Route{route})
	cfg := egressadapter.ProxyConfig{
		Listen:         "127.0.0.1:0",
		DefaultPolicy:  domainegress.PolicyDeny,
		DefaultTimeout: 5 * time.Second,
		Routes:         []domainegress.Route{route},
		AllowInsecure:  true,
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
	httpReq.Header.Set("X-Egress-URL", upstream.URL+"/v1/resource?token=mysecret&page=2")

	resp, err := proxyClient.Do(httpReq)
	if err != nil {
		t.Fatalf("proxy request: %v", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		t.Errorf("StatusCode = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if strings.Contains(capturedQuery, "token=mysecret") {
		t.Errorf("upstream received token in URL query: %s", capturedQuery)
	}
	if !strings.Contains(capturedQuery, "page=2") {
		t.Errorf("upstream is missing page=2 in URL query: %s", capturedQuery)
	}
}
