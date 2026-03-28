package otel_test

import (
	"context"
	"errors"
	"testing"

	oteladapter "github.com/vibewarden/vibewarden/internal/adapters/otel"
	"github.com/vibewarden/vibewarden/internal/ports"
	"net/http"
	"net/http/httptest"
)

// newTracerProvider creates a Provider with a fake OTLP endpoint for tracing tests.
func newTracerProvider(t *testing.T) (*oteladapter.Provider, func()) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	p := oteladapter.NewProvider()
	cfg := ports.TelemetryConfig{
		Prometheus: ports.PrometheusExporterConfig{Enabled: true},
		OTLP: ports.OTLPExporterConfig{
			Enabled:  true,
			Endpoint: srv.URL,
		},
		Traces: ports.TraceExportConfig{Enabled: true},
	}
	if err := p.Init(context.Background(), "test", "0.0.1", cfg); err != nil {
		srv.Close()
		t.Fatalf("Init() failed: %v", err)
	}
	cleanup := func() {
		_ = p.Shutdown(context.Background())
		srv.Close()
	}
	return p, cleanup
}

func TestProvider_TracerBeforeInit_ReturnsNil(t *testing.T) {
	p := oteladapter.NewProvider()
	if tr := p.Tracer(); tr != nil {
		t.Error("Tracer() before Init should return nil")
	}
}

func TestProvider_TracingEnabled_BeforeInit_False(t *testing.T) {
	p := oteladapter.NewProvider()
	if p.TracingEnabled() {
		t.Error("TracingEnabled() before Init should return false")
	}
}

func TestProvider_TracerProvider_Init(t *testing.T) {
	p, cleanup := newTracerProvider(t)
	defer cleanup()

	if !p.TracingEnabled() {
		t.Error("TracingEnabled() should return true after Init with traces enabled")
	}
	if tr := p.Tracer(); tr == nil {
		t.Error("Tracer() should return non-nil after Init with traces enabled")
	}
}

func TestProvider_TracerProvider_RequiresOTLP(t *testing.T) {
	p := oteladapter.NewProvider()
	cfg := ports.TelemetryConfig{
		Prometheus: ports.PrometheusExporterConfig{Enabled: true},
		Traces:     ports.TraceExportConfig{Enabled: true},
	}
	err := p.Init(context.Background(), "test", "0.0.1", cfg)
	if err == nil {
		t.Fatal("Init() with traces enabled but OTLP disabled should return error")
	}
	if err.Error() != "traces require OTLP exporter to be enabled" {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestProvider_Shutdown_TracerProvider(t *testing.T) {
	_, cleanup := newTracerProvider(t)
	// Cleanup calls Shutdown; just make sure it doesn't panic or error.
	cleanup()
}

func TestTracerAdapter_Start(t *testing.T) {
	p, cleanup := newTracerProvider(t)
	defer cleanup()

	tr := p.Tracer()
	ctx := context.Background()
	childCtx, span := tr.Start(ctx, "test-span", ports.WithSpanKind(ports.SpanKindServer))

	if span == nil {
		t.Fatal("Start() returned nil span")
	}
	if childCtx == nil {
		t.Fatal("Start() returned nil context")
	}
	span.End()
}

func TestTracerAdapter_Start_InternalKind(t *testing.T) {
	p, cleanup := newTracerProvider(t)
	defer cleanup()

	tr := p.Tracer()
	ctx := context.Background()
	_, span := tr.Start(ctx, "internal-span")
	if span == nil {
		t.Fatal("Start() returned nil span")
	}
	span.End()
}

func TestSpanAdapter_SetAttributes(t *testing.T) {
	p, cleanup := newTracerProvider(t)
	defer cleanup()

	tr := p.Tracer()
	_, span := tr.Start(context.Background(), "attr-test")
	defer span.End()

	// Should not panic.
	span.SetAttributes(
		ports.Attribute{Key: "http.method", Value: "GET"},
		ports.Attribute{Key: "http.path", Value: "/foo"},
	)
}

func TestSpanAdapter_SetStatus(t *testing.T) {
	tests := []struct {
		name string
		code ports.SpanStatusCode
		desc string
	}{
		{"unset", ports.SpanStatusUnset, ""},
		{"ok", ports.SpanStatusOK, ""},
		{"error", ports.SpanStatusError, "internal server error"},
	}

	p, cleanup := newTracerProvider(t)
	defer cleanup()

	tr := p.Tracer()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, span := tr.Start(context.Background(), "status-test")
			defer span.End()
			// Should not panic.
			span.SetStatus(tt.code, tt.desc)
		})
	}
}

func TestSpanAdapter_RecordError(t *testing.T) {
	p, cleanup := newTracerProvider(t)
	defer cleanup()

	tr := p.Tracer()
	_, span := tr.Start(context.Background(), "error-test")
	defer span.End()

	// Should not panic.
	span.RecordError(errors.New("something went wrong"))
}

func TestMockTracer_Start(t *testing.T) {
	mock := &oteladapter.MockTracer{}
	ctx := context.Background()
	_, span := mock.Start(ctx, "my-span", ports.WithSpanKind(ports.SpanKindServer))

	if len(mock.StartCalls) != 1 {
		t.Fatalf("expected 1 StartCall, got %d", len(mock.StartCalls))
	}
	if mock.StartCalls[0].Name != "my-span" {
		t.Errorf("expected span name 'my-span', got %q", mock.StartCalls[0].Name)
	}
	if span == nil {
		t.Error("Start() returned nil span")
	}
}

func TestMockSpan_RecordsState(t *testing.T) {
	span := &oteladapter.MockSpan{}

	span.SetAttributes(ports.Attribute{Key: "k", Value: "v"})
	span.SetStatus(ports.SpanStatusError, "boom")
	span.RecordError(errors.New("err"))
	span.End()

	if !span.Ended {
		t.Error("Ended should be true after End()")
	}
	if span.StatusCode != ports.SpanStatusError {
		t.Errorf("StatusCode = %v, want SpanStatusError", span.StatusCode)
	}
	if span.StatusDesc != "boom" {
		t.Errorf("StatusDesc = %q, want %q", span.StatusDesc, "boom")
	}
	if len(span.Attrs) != 1 || span.Attrs[0].Key != "k" {
		t.Errorf("Attrs = %v, want [{k v}]", span.Attrs)
	}
	if len(span.Errors) != 1 {
		t.Errorf("Errors = %v, want 1 error", span.Errors)
	}
}

// ---------------------------------------------------------------------------
// Propagator
// ---------------------------------------------------------------------------

func TestProvider_PropagatorBeforeInit_ReturnsNil(t *testing.T) {
	p := oteladapter.NewProvider()
	if prop := p.Propagator(); prop != nil {
		t.Error("Propagator() before Init should return nil")
	}
}

func TestProvider_PropagatorWithoutTracing_ReturnsNil(t *testing.T) {
	p := oteladapter.NewProvider()
	cfg := ports.TelemetryConfig{
		Prometheus: ports.PrometheusExporterConfig{Enabled: true},
	}
	if err := p.Init(context.Background(), "test", "0.0.1", cfg); err != nil {
		t.Fatalf("Init() failed: %v", err)
	}
	t.Cleanup(func() { _ = p.Shutdown(context.Background()) })

	if prop := p.Propagator(); prop != nil {
		t.Error("Propagator() should return nil when tracing is disabled")
	}
}

func TestProvider_PropagatorWithTracing_ReturnsNonNil(t *testing.T) {
	p, cleanup := newTracerProvider(t)
	defer cleanup()

	prop := p.Propagator()
	if prop == nil {
		t.Fatal("Propagator() should return non-nil when tracing is enabled")
	}
}

func TestProvider_Propagator_ImplementsPort(t *testing.T) {
	p, cleanup := newTracerProvider(t)
	defer cleanup()

	var _ ports.TextMapPropagator = p.Propagator()
}

func TestProvider_Propagator_Extract_ReturnsContext(t *testing.T) {
	p, cleanup := newTracerProvider(t)
	defer cleanup()

	prop := p.Propagator()
	carrier := &mapCarrier{m: map[string]string{
		"traceparent": "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01",
	}}
	ctx := prop.Extract(context.Background(), carrier)
	if ctx == nil {
		t.Error("Extract() should return a non-nil context")
	}
}

func TestProvider_Propagator_Inject_WritesHeader(t *testing.T) {
	p, cleanup := newTracerProvider(t)
	defer cleanup()

	prop := p.Propagator()
	// First extract to get a valid span context in ctx.
	carrier := &mapCarrier{m: map[string]string{
		"traceparent": "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01",
	}}
	ctx := prop.Extract(context.Background(), carrier)

	// Inject into a new carrier.
	out := &mapCarrier{m: map[string]string{}}
	prop.Inject(ctx, out)

	if tp := out.m["traceparent"]; tp == "" {
		t.Error("Inject() should write a traceparent header")
	}
}

func TestProvider_Propagator_Extract_InvalidTraceparent_NoError(t *testing.T) {
	p, cleanup := newTracerProvider(t)
	defer cleanup()

	prop := p.Propagator()
	// An invalid traceparent should not cause a panic; it simply results in
	// no parent context being set.
	carrier := &mapCarrier{m: map[string]string{
		"traceparent": "not-a-valid-traceparent",
	}}
	ctx := prop.Extract(context.Background(), carrier)
	if ctx == nil {
		t.Error("Extract() with invalid traceparent should still return a non-nil context")
	}
}

// mapCarrier is a simple ports.TextMapCarrier backed by a map, used in tests.
type mapCarrier struct{ m map[string]string }

func (c *mapCarrier) Get(key string) string { return c.m[key] }
func (c *mapCarrier) Set(key, value string) { c.m[key] = value }
func (c *mapCarrier) Keys() []string {
	keys := make([]string, 0, len(c.m))
	for k := range c.m {
		keys = append(keys, k)
	}
	return keys
}
