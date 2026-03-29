package egress_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	egressadapter "github.com/vibewarden/vibewarden/internal/adapters/egress"
	"github.com/vibewarden/vibewarden/internal/adapters/otel"
	domainegress "github.com/vibewarden/vibewarden/internal/domain/egress"
	"github.com/vibewarden/vibewarden/internal/domain/events"
	"github.com/vibewarden/vibewarden/internal/domain/resilience"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// ---------------------------------------------------------------------------
// Test fakes
// ---------------------------------------------------------------------------

// fakeObsEventLogger records emitted events for assertion.
type fakeObsEventLogger struct {
	mu     sync.Mutex
	logged []events.Event
}

// Log records the event.
func (f *fakeObsEventLogger) Log(_ context.Context, ev events.Event) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.logged = append(f.logged, ev)
	return nil
}

// Snapshot returns a copy of all logged events.
func (f *fakeObsEventLogger) Snapshot() []events.Event {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]events.Event, len(f.logged))
	copy(out, f.logged)
	return out
}

// EventTypes returns the EventType field of all logged events.
func (f *fakeObsEventLogger) EventTypes() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	types := make([]string, len(f.logged))
	for i, e := range f.logged {
		types[i] = e.EventType
	}
	return types
}

// fakeMetricsCollector records calls to the egress metric methods.
type fakeMetricsCollector struct {
	mu            sync.Mutex
	requestTotals []egressRequestTotalCall
	durations     []egressDurationCall
	errorTotals   []string
}

type egressRequestTotalCall struct {
	route      string
	method     string
	statusCode string
}

type egressDurationCall struct {
	route    string
	method   string
	duration time.Duration
}

// Implement ports.MetricsCollector (non-egress methods are no-ops).
func (f *fakeMetricsCollector) IncRequestTotal(_, _, _ string)                               {}
func (f *fakeMetricsCollector) ObserveRequestDuration(_, _ string, _ time.Duration)          {}
func (f *fakeMetricsCollector) IncRateLimitHit(_ string)                                     {}
func (f *fakeMetricsCollector) IncAuthDecision(_ string)                                     {}
func (f *fakeMetricsCollector) IncUpstreamError()                                            {}
func (f *fakeMetricsCollector) IncUpstreamTimeout()                                          {}
func (f *fakeMetricsCollector) IncUpstreamRetry(_ string)                                    {}
func (f *fakeMetricsCollector) SetActiveConnections(_ int)                                   {}
func (f *fakeMetricsCollector) SetCircuitBreakerState(_ context.Context, _ resilience.State) {}
func (f *fakeMetricsCollector) IncWAFDetection(_, _ string)                                  {}

// Compile-time assertion.
var _ ports.MetricsCollector = (*fakeMetricsCollector)(nil)

func (f *fakeMetricsCollector) IncEgressRequestTotal(route, method, statusCode string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requestTotals = append(f.requestTotals, egressRequestTotalCall{route, method, statusCode})
}

func (f *fakeMetricsCollector) ObserveEgressDuration(route, method string, d time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.durations = append(f.durations, egressDurationCall{route, method, d})
}

func (f *fakeMetricsCollector) IncEgressErrorTotal(route string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.errorTotals = append(f.errorTotals, route)
}

// Snapshot methods for assertions.
func (f *fakeMetricsCollector) RequestTotals() []egressRequestTotalCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]egressRequestTotalCall, len(f.requestTotals))
	copy(out, f.requestTotals)
	return out
}

func (f *fakeMetricsCollector) Durations() []egressDurationCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]egressDurationCall, len(f.durations))
	copy(out, f.durations)
	return out
}

func (f *fakeMetricsCollector) ErrorTotals() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.errorTotals))
	copy(out, f.errorTotals)
	return out
}

// fakePropagator records Inject calls to verify trace context propagation.
type fakePropagator struct {
	mu       sync.Mutex
	injected []map[string]string
}

// Extract is a no-op for this fake.
func (f *fakePropagator) Extract(ctx context.Context, _ ports.TextMapCarrier) context.Context {
	return ctx
}

// Inject records the carrier state at injection time.
func (f *fakePropagator) Inject(_ context.Context, carrier ports.TextMapCarrier) {
	f.mu.Lock()
	defer f.mu.Unlock()
	snapshot := make(map[string]string)
	for _, k := range carrier.Keys() {
		snapshot[k] = carrier.Get(k)
	}
	f.injected = append(f.injected, snapshot)
}

// InjectCount returns the number of times Inject was called.
func (f *fakePropagator) InjectCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.injected)
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// newObservableProxy creates a Proxy wired with the given observability
// components. It does NOT start listening; tests drive HandleRequest directly.
func newObservableProxy(
	t *testing.T,
	routes []domainegress.Route,
	client *http.Client,
	policy domainegress.Policy,
	metrics ports.MetricsCollector,
	logger ports.EventLogger,
	tracer ports.Tracer,
	propagator ports.TextMapPropagator,
) *egressadapter.Proxy {
	t.Helper()
	resolver := egressadapter.NewRouteResolver(routes)
	cfg := egressadapter.ProxyConfig{
		Listen:         "127.0.0.1:0",
		DefaultPolicy:  policy,
		DefaultTimeout: 5 * time.Second,
		Routes:         routes,
		AllowInsecure:  true,
		Metrics:        metrics,
		EventLogger:    logger,
		Tracer:         tracer,
		Propagator:     propagator,
	}
	return egressadapter.NewProxy(cfg, resolver, client, nil)
}

// ---------------------------------------------------------------------------
// Metrics tests
// ---------------------------------------------------------------------------

// TestObservability_Metrics_SuccessfulRequest verifies that a successful egress
// request increments the request total with the correct status code and records
// a duration observation.
func TestObservability_Metrics_SuccessfulRequest(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	route := newTestRoute(t, "external-api", upstream.URL+"/*")
	mc := &fakeMetricsCollector{}

	// Manually implement SetCircuitBreakerState properly. The fakeMetricsCollector
	// above has a wrong signature placeholder — use the NoOp from the adapter below.
	proxy := newObservableProxy(t, []domainegress.Route{route}, upstream.Client(),
		domainegress.PolicyDeny, mc, nil, nil, nil)

	req, err := domainegress.NewEgressRequest("GET", upstream.URL+"/resource", nil, nil)
	if err != nil {
		t.Fatalf("NewEgressRequest: %v", err)
	}

	_, err = proxy.HandleRequest(context.Background(), req)
	if err != nil {
		t.Fatalf("HandleRequest returned unexpected error: %v", err)
	}

	totals := mc.RequestTotals()
	if len(totals) != 1 {
		t.Fatalf("expected 1 IncEgressRequestTotal call, got %d", len(totals))
	}
	if totals[0].route != "external-api" {
		t.Errorf("route = %q, want %q", totals[0].route, "external-api")
	}
	if totals[0].method != "GET" {
		t.Errorf("method = %q, want %q", totals[0].method, "GET")
	}
	if totals[0].statusCode != "200" {
		t.Errorf("statusCode = %q, want %q", totals[0].statusCode, "200")
	}

	durs := mc.Durations()
	if len(durs) != 1 {
		t.Fatalf("expected 1 ObserveEgressDuration call, got %d", len(durs))
	}
	if durs[0].route != "external-api" {
		t.Errorf("duration route = %q, want %q", durs[0].route, "external-api")
	}
	if durs[0].duration <= 0 {
		t.Error("duration should be > 0")
	}
}

// TestObservability_Metrics_BlockedByPolicy verifies that a request blocked by
// the deny policy increments the error total and does NOT record a duration or
// request total (because no upstream contact was made).
func TestObservability_Metrics_BlockedByPolicy(t *testing.T) {
	mc := &fakeMetricsCollector{}
	proxy := newObservableProxy(t, nil, nil, domainegress.PolicyDeny, mc, nil, nil, nil)

	req, err := domainegress.NewEgressRequest("GET", "https://api.unknown.example.com/", nil, nil)
	if err != nil {
		t.Fatalf("NewEgressRequest: %v", err)
	}

	_, err = proxy.HandleRequest(context.Background(), req)
	if err == nil {
		t.Fatal("expected error for blocked request")
	}

	if len(mc.RequestTotals()) != 0 {
		t.Errorf("expected 0 IncEgressRequestTotal calls for blocked request, got %d", len(mc.RequestTotals()))
	}
	if len(mc.Durations()) != 0 {
		t.Errorf("expected 0 ObserveEgressDuration calls for blocked request, got %d", len(mc.Durations()))
	}
	errs := mc.ErrorTotals()
	if len(errs) != 1 {
		t.Fatalf("expected 1 IncEgressErrorTotal call, got %d", len(errs))
	}
	if errs[0] != "unmatched" {
		t.Errorf("error route = %q, want %q", errs[0], "unmatched")
	}
}

// TestObservability_Metrics_TransportError verifies that a transport-level
// failure (connection refused) increments the error total and records both
// the duration and request total with status "error".
func TestObservability_Metrics_TransportError(t *testing.T) {
	// Use a server that is immediately closed so all connections are refused.
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	upstreamURL := upstream.URL
	upstream.Close() // close immediately to provoke connection refused

	route := newTestRoute(t, "broken-api", upstreamURL+"/*")
	mc := &fakeMetricsCollector{}

	proxy := newObservableProxy(t, []domainegress.Route{route}, nil,
		domainegress.PolicyDeny, mc, nil, nil, nil)

	req, err := domainegress.NewEgressRequest("GET", upstreamURL+"/resource", nil, nil)
	if err != nil {
		t.Fatalf("NewEgressRequest: %v", err)
	}

	_, err = proxy.HandleRequest(context.Background(), req)
	if err == nil {
		t.Fatal("expected error for refused connection")
	}

	totals := mc.RequestTotals()
	if len(totals) != 1 {
		t.Fatalf("expected 1 IncEgressRequestTotal call, got %d", len(totals))
	}
	if totals[0].statusCode != "error" {
		t.Errorf("statusCode = %q, want %q", totals[0].statusCode, "error")
	}

	durs := mc.Durations()
	if len(durs) != 1 {
		t.Fatalf("expected 1 ObserveEgressDuration call, got %d", len(durs))
	}
	errs := mc.ErrorTotals()
	if len(errs) != 1 {
		t.Fatalf("expected 1 IncEgressErrorTotal call, got %d", len(errs))
	}
	if errs[0] != "broken-api" {
		t.Errorf("error route = %q, want %q", errs[0], "broken-api")
	}
}

// ---------------------------------------------------------------------------
// Structured log event tests
// ---------------------------------------------------------------------------

// TestObservability_Events_SuccessfulRequest verifies that a successful request
// emits an egress.request event followed by an egress.response event.
func TestObservability_Events_SuccessfulRequest(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
	}))
	defer upstream.Close()

	route := newTestRoute(t, "target-api", upstream.URL+"/*")
	logger := &fakeObsEventLogger{}

	proxy := newObservableProxy(t, []domainegress.Route{route}, upstream.Client(),
		domainegress.PolicyDeny, nil, logger, nil, nil)

	req, err := domainegress.NewEgressRequest("POST", upstream.URL+"/items", nil, nil)
	if err != nil {
		t.Fatalf("NewEgressRequest: %v", err)
	}

	_, err = proxy.HandleRequest(context.Background(), req)
	if err != nil {
		t.Fatalf("HandleRequest returned unexpected error: %v", err)
	}

	types := logger.EventTypes()
	if len(types) != 2 {
		t.Fatalf("expected 2 events, got %d: %v", len(types), types)
	}
	if types[0] != events.EventTypeEgressRequest {
		t.Errorf("event[0] type = %q, want %q", types[0], events.EventTypeEgressRequest)
	}
	if types[1] != events.EventTypeEgressResponse {
		t.Errorf("event[1] type = %q, want %q", types[1], events.EventTypeEgressResponse)
	}

	// Verify payload fields on the response event.
	logged := logger.Snapshot()
	respEv := logged[1]
	if respEv.Payload["route"] != "target-api" {
		t.Errorf("response event route = %v, want %q", respEv.Payload["route"], "target-api")
	}
	if respEv.Payload["method"] != "POST" {
		t.Errorf("response event method = %v, want %q", respEv.Payload["method"], "POST")
	}
	if respEv.Payload["status_code"] != 201 {
		t.Errorf("response event status_code = %v, want 201", respEv.Payload["status_code"])
	}
	if respEv.Payload["attempts"] != 1 {
		t.Errorf("response event attempts = %v, want 1", respEv.Payload["attempts"])
	}
}

// TestObservability_Events_BlockedByPolicy verifies that a request blocked by
// the default deny policy emits exactly one egress.blocked event.
func TestObservability_Events_BlockedByPolicy(t *testing.T) {
	logger := &fakeObsEventLogger{}
	proxy := newObservableProxy(t, nil, nil, domainegress.PolicyDeny, nil, logger, nil, nil)

	req, err := domainegress.NewEgressRequest("DELETE", "https://api.example.com/resource", nil, nil)
	if err != nil {
		t.Fatalf("NewEgressRequest: %v", err)
	}

	_, err = proxy.HandleRequest(context.Background(), req)
	if err == nil {
		t.Fatal("expected error for blocked request")
	}

	types := logger.EventTypes()
	if len(types) != 1 {
		t.Fatalf("expected 1 event, got %d: %v", len(types), types)
	}
	if types[0] != events.EventTypeEgressBlocked {
		t.Errorf("event type = %q, want %q", types[0], events.EventTypeEgressBlocked)
	}

	logged := logger.Snapshot()
	ev := logged[0]
	if ev.Payload["reason"] != "no route matched default deny policy" {
		t.Errorf("reason = %v, want %q", ev.Payload["reason"], "no route matched default deny policy")
	}
	if ev.Payload["route"] != "unmatched" {
		t.Errorf("route = %v, want %q", ev.Payload["route"], "unmatched")
	}
}

// TestObservability_Events_TransportError verifies that a transport-level
// failure emits an egress.request event followed by an egress.error event.
func TestObservability_Events_TransportError(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	upstreamURL := upstream.URL
	upstream.Close()

	route := newTestRoute(t, "broken-api", upstreamURL+"/*")
	logger := &fakeObsEventLogger{}

	proxy := newObservableProxy(t, []domainegress.Route{route}, nil,
		domainegress.PolicyDeny, nil, logger, nil, nil)

	req, err := domainegress.NewEgressRequest("GET", upstreamURL+"/data", nil, nil)
	if err != nil {
		t.Fatalf("NewEgressRequest: %v", err)
	}

	_, err = proxy.HandleRequest(context.Background(), req)
	if err == nil {
		t.Fatal("expected error for transport failure")
	}

	types := logger.EventTypes()
	if len(types) != 2 {
		t.Fatalf("expected 2 events, got %d: %v", len(types), types)
	}
	if types[0] != events.EventTypeEgressRequest {
		t.Errorf("event[0] type = %q, want %q", types[0], events.EventTypeEgressRequest)
	}
	if types[1] != events.EventTypeEgressError {
		t.Errorf("event[1] type = %q, want %q", types[1], events.EventTypeEgressError)
	}
}

// ---------------------------------------------------------------------------
// Tracing tests
// ---------------------------------------------------------------------------

// TestObservability_Tracing_SpanCreatedForRequest verifies that a client span
// is started for each egress request and ended when the request completes.
func TestObservability_Tracing_SpanCreatedForRequest(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	route := newTestRoute(t, "traced-api", upstream.URL+"/*")
	tracer := &otel.MockTracer{}

	proxy := newObservableProxy(t, []domainegress.Route{route}, upstream.Client(),
		domainegress.PolicyDeny, nil, nil, tracer, nil)

	req, err := domainegress.NewEgressRequest("GET", upstream.URL+"/ping", nil, nil)
	if err != nil {
		t.Fatalf("NewEgressRequest: %v", err)
	}

	_, err = proxy.HandleRequest(context.Background(), req)
	if err != nil {
		t.Fatalf("HandleRequest returned unexpected error: %v", err)
	}

	if len(tracer.StartCalls) != 1 {
		t.Fatalf("expected 1 span Start call, got %d", len(tracer.StartCalls))
	}

	call := tracer.StartCalls[0]
	if !strings.HasPrefix(call.Name, "egress ") {
		t.Errorf("span name = %q, want prefix %q", call.Name, "egress ")
	}

	// Verify span kind is Client.
	kind := ports.KindOf(call.Opts)
	if kind != ports.SpanKindClient {
		t.Errorf("span kind = %v, want SpanKindClient", kind)
	}

	// Verify the span was ended.
	if tracer.SpanToReturn != nil && !tracer.SpanToReturn.Ended {
		t.Error("span was not ended")
	}
}

// TestObservability_Tracing_PropagatorInjectsCalled verifies that the
// TextMapPropagator.Inject is called for each upstream HTTP attempt so that
// the W3C traceparent header is propagated to the external service.
func TestObservability_Tracing_PropagatorInjectsCalled(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	route := newTestRoute(t, "traced-api", upstream.URL+"/*")
	tracer := &otel.MockTracer{}
	propagator := &fakePropagator{}

	proxy := newObservableProxy(t, []domainegress.Route{route}, upstream.Client(),
		domainegress.PolicyDeny, nil, nil, tracer, propagator)

	req, err := domainegress.NewEgressRequest("GET", upstream.URL+"/ping", nil, nil)
	if err != nil {
		t.Fatalf("NewEgressRequest: %v", err)
	}

	_, err = proxy.HandleRequest(context.Background(), req)
	if err != nil {
		t.Fatalf("HandleRequest returned unexpected error: %v", err)
	}

	if propagator.InjectCount() != 1 {
		t.Errorf("Propagator.Inject called %d times, want 1", propagator.InjectCount())
	}
}

// TestObservability_Tracing_NoTracerNoPanic verifies that the proxy operates
// correctly when no Tracer or Propagator is configured (nil-safe path).
func TestObservability_Tracing_NoTracerNoPanic(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	route := newTestRoute(t, "api", upstream.URL+"/*")
	// No tracer, no propagator, no metrics, no logger.
	proxy := newObservableProxy(t, []domainegress.Route{route}, upstream.Client(),
		domainegress.PolicyDeny, nil, nil, nil, nil)

	req, err := domainegress.NewEgressRequest("GET", upstream.URL+"/ping", nil, nil)
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

// ---------------------------------------------------------------------------
// Combined observability (metrics + events + tracing) test
// ---------------------------------------------------------------------------

// TestObservability_AllComponents_Success verifies that all three observability
// components (metrics, events, tracing) fire correctly for a single successful
// egress request.
func TestObservability_AllComponents_Success(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	}))
	defer upstream.Close()

	route := newTestRoute(t, "full-stack-api", upstream.URL+"/v1/*")
	mc := &fakeMetricsCollector{}
	logger := &fakeObsEventLogger{}
	tracer := &otel.MockTracer{}
	propagator := &fakePropagator{}

	proxy := newObservableProxy(t, []domainegress.Route{route}, upstream.Client(),
		domainegress.PolicyDeny, mc, logger, tracer, propagator)

	req, err := domainegress.NewEgressRequest("PUT", upstream.URL+"/v1/items", nil, nil)
	if err != nil {
		t.Fatalf("NewEgressRequest: %v", err)
	}

	_, err = proxy.HandleRequest(context.Background(), req)
	if err != nil {
		t.Fatalf("HandleRequest returned unexpected error: %v", err)
	}

	// Metrics.
	if len(mc.RequestTotals()) != 1 {
		t.Errorf("expected 1 request total, got %d", len(mc.RequestTotals()))
	}
	if mc.RequestTotals()[0].statusCode != "202" {
		t.Errorf("statusCode = %q, want %q", mc.RequestTotals()[0].statusCode, "202")
	}

	// Events.
	types := logger.EventTypes()
	if len(types) != 2 {
		t.Fatalf("expected 2 events, got %d: %v", len(types), types)
	}
	if types[0] != events.EventTypeEgressRequest {
		t.Errorf("event[0] = %q, want %q", types[0], events.EventTypeEgressRequest)
	}
	if types[1] != events.EventTypeEgressResponse {
		t.Errorf("event[1] = %q, want %q", types[1], events.EventTypeEgressResponse)
	}

	// Tracing.
	if len(tracer.StartCalls) != 1 {
		t.Errorf("expected 1 span, got %d", len(tracer.StartCalls))
	}
	if propagator.InjectCount() != 1 {
		t.Errorf("Inject called %d times, want 1", propagator.InjectCount())
	}
}
