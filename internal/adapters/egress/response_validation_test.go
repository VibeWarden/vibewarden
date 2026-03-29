package egress_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	egressadapter "github.com/vibewarden/vibewarden/internal/adapters/egress"
	domainegress "github.com/vibewarden/vibewarden/internal/domain/egress"
	"github.com/vibewarden/vibewarden/internal/domain/events"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// newValidationProxy creates a Proxy with an EventLogger and MetricsCollector
// wired in so tests can assert on emitted events and error metrics.
func newValidationProxy(
	t *testing.T,
	routes []domainegress.Route,
	client *http.Client,
	logger ports.EventLogger,
	metrics ports.MetricsCollector,
) *egressadapter.Proxy {
	t.Helper()
	resolver := egressadapter.NewRouteResolver(routes)
	cfg := egressadapter.ProxyConfig{
		Listen:         "127.0.0.1:0",
		DefaultPolicy:  domainegress.PolicyDeny,
		DefaultTimeout: 5 * time.Second,
		Routes:         routes,
		AllowInsecure:  true,
		EventLogger:    logger,
		Metrics:        metrics,
	}
	return egressadapter.NewProxy(cfg, resolver, client, nil)
}

// TestValidateResponse_StatusCode_Pass verifies that a response whose status
// code is in the allowlist is forwarded normally.
func TestValidateResponse_StatusCode_Pass(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer upstream.Close()

	route := newTestRoute(t, "api", upstream.URL+"/v1/*",
		domainegress.WithValidateResponse(domainegress.ResponseValidationConfig{
			StatusCodes: []string{"2xx"},
		}),
	)
	proxy := newValidationProxy(t, []domainegress.Route{route}, upstream.Client(), nil, nil)

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
}

// TestValidateResponse_StatusCode_Fail verifies that a response whose status
// code is NOT in the allowlist causes ErrResponseValidationFailed to be returned
// and the upstream body to be dropped.
func TestValidateResponse_StatusCode_Fail(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"boom"}`))
	}))
	defer upstream.Close()

	route := newTestRoute(t, "api", upstream.URL+"/v1/*",
		domainegress.WithValidateResponse(domainegress.ResponseValidationConfig{
			StatusCodes: []string{"2xx"},
		}),
	)
	el := &fakeObsEventLogger{}
	mc := &fakeMetricsCollector{}
	proxy := newValidationProxy(t, []domainegress.Route{route}, upstream.Client(), el, mc)

	req, err := domainegress.NewEgressRequest("GET", upstream.URL+"/v1/resource", nil, nil)
	if err != nil {
		t.Fatalf("NewEgressRequest: %v", err)
	}
	_, err = proxy.HandleRequest(context.Background(), req)
	if err == nil {
		t.Fatal("HandleRequest should have returned an error")
	}
	if !errors.Is(err, egressadapter.ErrResponseValidationFailed) {
		t.Errorf("error = %v, want errors.Is(ErrResponseValidationFailed)", err)
	}

	// An egress.response_invalid event should have been emitted.
	evTypes := el.EventTypes()
	found := false
	for _, et := range evTypes {
		if et == events.EventTypeEgressResponseInvalid {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected egress.response_invalid event, got event types: %v", evTypes)
	}

	// The error counter should have been incremented.
	if len(mc.ErrorTotals()) == 0 {
		t.Error("expected IncEgressErrorTotal to be called")
	}
}

// TestValidateResponse_ContentType_Pass verifies that a response with a
// content type in the allowlist is forwarded normally.
func TestValidateResponse_ContentType_Pass(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer upstream.Close()

	route := newTestRoute(t, "api", upstream.URL+"/v1/*",
		domainegress.WithValidateResponse(domainegress.ResponseValidationConfig{
			ContentTypes: []string{"application/json"},
		}),
	)
	proxy := newValidationProxy(t, []domainegress.Route{route}, upstream.Client(), nil, nil)

	req, err := domainegress.NewEgressRequest("GET", upstream.URL+"/v1/data", nil, nil)
	if err != nil {
		t.Fatalf("NewEgressRequest: %v", err)
	}
	resp, err := proxy.HandleRequest(context.Background(), req)
	if err != nil {
		t.Fatalf("HandleRequest returned unexpected error: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("StatusCode = %d, want 200", resp.StatusCode)
	}
}

// TestValidateResponse_ContentType_Fail verifies that a response with a
// content type NOT in the allowlist causes ErrResponseValidationFailed.
func TestValidateResponse_ContentType_Fail(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`<html>nope</html>`))
	}))
	defer upstream.Close()

	route := newTestRoute(t, "api", upstream.URL+"/v1/*",
		domainegress.WithValidateResponse(domainegress.ResponseValidationConfig{
			ContentTypes: []string{"application/json"},
		}),
	)
	el := &fakeObsEventLogger{}
	proxy := newValidationProxy(t, []domainegress.Route{route}, upstream.Client(), el, nil)

	req, err := domainegress.NewEgressRequest("GET", upstream.URL+"/v1/data", nil, nil)
	if err != nil {
		t.Fatalf("NewEgressRequest: %v", err)
	}
	_, err = proxy.HandleRequest(context.Background(), req)
	if err == nil {
		t.Fatal("HandleRequest should have returned an error")
	}
	if !errors.Is(err, egressadapter.ErrResponseValidationFailed) {
		t.Errorf("error = %v, want errors.Is(ErrResponseValidationFailed)", err)
	}

	evTypes := el.EventTypes()
	found := false
	for _, et := range evTypes {
		if et == events.EventTypeEgressResponseInvalid {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected egress.response_invalid event, got: %v", evTypes)
	}
}

// TestValidateResponse_BothRules_Pass verifies that when both status code and
// content type rules are configured, a response matching both rules passes.
func TestValidateResponse_BothRules_Pass(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":1}`))
	}))
	defer upstream.Close()

	route := newTestRoute(t, "api", upstream.URL+"/v1/*",
		domainegress.WithValidateResponse(domainegress.ResponseValidationConfig{
			StatusCodes:  []string{"2xx"},
			ContentTypes: []string{"application/json"},
		}),
	)
	proxy := newValidationProxy(t, []domainegress.Route{route}, upstream.Client(), nil, nil)

	req, err := domainegress.NewEgressRequest("POST", upstream.URL+"/v1/items", nil, nil)
	if err != nil {
		t.Fatalf("NewEgressRequest: %v", err)
	}
	resp, err := proxy.HandleRequest(context.Background(), req)
	if err != nil {
		t.Fatalf("HandleRequest returned unexpected error: %v", err)
	}
	if resp.StatusCode != http.StatusCreated {
		t.Errorf("StatusCode = %d, want 201", resp.StatusCode)
	}
}

// TestValidateResponse_BothRules_StatusFails verifies that when both rules are
// configured, a bad status code triggers the error even when content type is
// acceptable.
func TestValidateResponse_BothRules_StatusFails(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"error":"down"}`))
	}))
	defer upstream.Close()

	route := newTestRoute(t, "api", upstream.URL+"/v1/*",
		domainegress.WithValidateResponse(domainegress.ResponseValidationConfig{
			StatusCodes:  []string{"2xx"},
			ContentTypes: []string{"application/json"},
		}),
	)
	proxy := newValidationProxy(t, []domainegress.Route{route}, upstream.Client(), nil, nil)

	req, err := domainegress.NewEgressRequest("GET", upstream.URL+"/v1/items", nil, nil)
	if err != nil {
		t.Fatalf("NewEgressRequest: %v", err)
	}
	_, err = proxy.HandleRequest(context.Background(), req)
	if !errors.Is(err, egressadapter.ErrResponseValidationFailed) {
		t.Errorf("error = %v, want ErrResponseValidationFailed", err)
	}
}

// TestValidateResponse_NoConfig_AllowsAll verifies that a route with no
// validate_response config does not block any upstream response.
func TestValidateResponse_NoConfig_AllowsAll(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("error page"))
	}))
	defer upstream.Close()

	route := newTestRoute(t, "api", upstream.URL+"/v1/*")
	proxy := newValidationProxy(t, []domainegress.Route{route}, upstream.Client(), nil, nil)

	req, err := domainegress.NewEgressRequest("GET", upstream.URL+"/v1/page", nil, nil)
	if err != nil {
		t.Fatalf("NewEgressRequest: %v", err)
	}
	resp, err := proxy.HandleRequest(context.Background(), req)
	if err != nil {
		t.Fatalf("HandleRequest returned unexpected error: %v", err)
	}
	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("StatusCode = %d, want 500", resp.StatusCode)
	}
}

// TestHTTPHandler_ValidationFail_Returns502 verifies that the HTTP handler
// writes 502 Bad Gateway when response validation fails.
func TestHTTPHandler_ValidationFail_Returns502(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("<html>unexpected</html>"))
	}))
	defer upstream.Close()

	route := newTestRoute(t, "api", upstream.URL+"/v1/*",
		domainegress.WithValidateResponse(domainegress.ResponseValidationConfig{
			ContentTypes: []string{"application/json"},
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
	httpReq, err := http.NewRequest(http.MethodGet, proxyURL, nil)
	if err != nil {
		t.Fatalf("http.NewRequest: %v", err)
	}
	httpReq.Header.Set("X-Egress-URL", upstream.URL+"/v1/resource")

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		t.Fatalf("proxy request: %v", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusBadGateway {
		body, _ := io.ReadAll(resp.Body)
		t.Errorf("StatusCode = %d, want 502; body: %q", resp.StatusCode, string(body))
	}
}

// TestValidateResponse_EventPayload verifies that the egress.response_invalid
// event contains the expected fields.
func TestValidateResponse_EventPayload(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusForbidden)
	}))
	defer upstream.Close()

	route := newTestRoute(t, "myroute", upstream.URL+"/v1/*",
		domainegress.WithValidateResponse(domainegress.ResponseValidationConfig{
			StatusCodes: []string{"2xx"},
		}),
	)
	el := &fakeObsEventLogger{}
	proxy := newValidationProxy(t, []domainegress.Route{route}, upstream.Client(), el, nil)

	req, err := domainegress.NewEgressRequest("GET", upstream.URL+"/v1/item", nil, nil)
	if err != nil {
		t.Fatalf("NewEgressRequest: %v", err)
	}
	_, _ = proxy.HandleRequest(context.Background(), req)

	evs := el.Snapshot()
	var invalidEv *events.Event
	for i := range evs {
		if evs[i].EventType == events.EventTypeEgressResponseInvalid {
			invalidEv = &evs[i]
			break
		}
	}
	if invalidEv == nil {
		t.Fatal("no egress.response_invalid event emitted")
	}

	payload := invalidEv.Payload
	assertPayloadString(t, payload, "route", "myroute")
	assertPayloadString(t, payload, "method", "GET")
	assertPayloadInt(t, payload, "status_code", http.StatusForbidden)
	assertPayloadString(t, payload, "content_type", "text/plain")

	if _, ok := payload["reason"]; !ok {
		t.Error("event payload missing 'reason' field")
	}
}

// assertPayloadString checks that payload[key] is a string equal to want.
func assertPayloadString(t *testing.T, payload map[string]any, key, want string) {
	t.Helper()
	v, ok := payload[key]
	if !ok {
		t.Errorf("payload missing key %q", key)
		return
	}
	s, ok := v.(string)
	if !ok {
		t.Errorf("payload[%q] = %T, want string", key, v)
		return
	}
	if s != want {
		t.Errorf("payload[%q] = %q, want %q", key, s, want)
	}
}

// assertPayloadInt checks that payload[key] is an int equal to want.
func assertPayloadInt(t *testing.T, payload map[string]any, key string, want int) {
	t.Helper()
	v, ok := payload[key]
	if !ok {
		t.Errorf("payload missing key %q", key)
		return
	}
	n, ok := v.(int)
	if !ok {
		t.Errorf("payload[%q] = %T(%v), want int", key, v, v)
		return
	}
	if n != want {
		t.Errorf("payload[%q] = %d, want %d", key, n, want)
	}
}
