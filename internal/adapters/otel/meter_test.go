package otel_test

import (
	"context"
	"testing"

	oteladapter "github.com/vibewarden/vibewarden/internal/adapters/otel"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// newInitializedProvider creates and initializes a provider for testing
// with the Prometheus exporter enabled (legacy default behaviour).
// The caller must call Shutdown when done.
func newInitializedProvider(t *testing.T) *oteladapter.Provider {
	t.Helper()
	p := oteladapter.NewProvider()
	cfg := ports.TelemetryConfig{
		Prometheus: ports.PrometheusExporterConfig{Enabled: true},
	}
	if err := p.Init(context.Background(), "test", "0.0.0", cfg); err != nil {
		t.Fatalf("Init() failed: %v", err)
	}
	t.Cleanup(func() { _ = p.Shutdown(context.Background()) })
	return p
}

func TestMeter_Int64Counter_Created(t *testing.T) {
	p := newInitializedProvider(t)
	meter := p.Meter()

	c, err := meter.Int64Counter("test_counter_total")
	if err != nil {
		t.Fatalf("Int64Counter() returned error: %v", err)
	}
	if c == nil {
		t.Fatal("Int64Counter() returned nil counter")
	}
}

func TestMeter_Int64Counter_WithOptions(t *testing.T) {
	p := newInitializedProvider(t)
	meter := p.Meter()

	_, err := meter.Int64Counter("test_counter_opts_total",
		ports.WithDescription("A counter with options."),
		ports.WithUnit("1"),
	)
	if err != nil {
		t.Fatalf("Int64Counter() with options returned error: %v", err)
	}
}

func TestMeter_Float64Histogram_Created(t *testing.T) {
	p := newInitializedProvider(t)
	meter := p.Meter()

	h, err := meter.Float64Histogram("test_duration_seconds",
		ports.WithDescription("A test histogram."),
		ports.WithUnit("s"),
		ports.WithExplicitBuckets([]float64{0.01, 0.1, 1.0}),
	)
	if err != nil {
		t.Fatalf("Float64Histogram() returned error: %v", err)
	}
	if h == nil {
		t.Fatal("Float64Histogram() returned nil histogram")
	}
}

func TestMeter_Int64UpDownCounter_Created(t *testing.T) {
	p := newInitializedProvider(t)
	meter := p.Meter()

	c, err := meter.Int64UpDownCounter("test_gauge",
		ports.WithDescription("A test gauge."),
	)
	if err != nil {
		t.Fatalf("Int64UpDownCounter() returned error: %v", err)
	}
	if c == nil {
		t.Fatal("Int64UpDownCounter() returned nil counter")
	}
}

func TestMeter_Int64Counter_Add_DoesNotPanic(t *testing.T) {
	p := newInitializedProvider(t)
	meter := p.Meter()

	c, err := meter.Int64Counter("no_panic_counter_total")
	if err != nil {
		t.Fatalf("Int64Counter() failed: %v", err)
	}

	// Must not panic.
	c.Add(context.Background(), 1)
	c.Add(context.Background(), 5,
		ports.Attribute{Key: "method", Value: "GET"},
		ports.Attribute{Key: "status_code", Value: "200"},
	)
}

func TestMeter_Float64Histogram_Record_DoesNotPanic(t *testing.T) {
	p := newInitializedProvider(t)
	meter := p.Meter()

	h, err := meter.Float64Histogram("no_panic_hist_seconds")
	if err != nil {
		t.Fatalf("Float64Histogram() failed: %v", err)
	}

	// Must not panic.
	h.Record(context.Background(), 0.042)
	h.Record(context.Background(), 1.5,
		ports.Attribute{Key: "path_pattern", Value: "/users/:id"},
	)
}

func TestMeter_Int64UpDownCounter_Add_DoesNotPanic(t *testing.T) {
	p := newInitializedProvider(t)
	meter := p.Meter()

	c, err := meter.Int64UpDownCounter("no_panic_updown")
	if err != nil {
		t.Fatalf("Int64UpDownCounter() failed: %v", err)
	}

	// Must not panic.
	c.Add(context.Background(), 5)
	c.Add(context.Background(), -3)
}

func TestMeter_ImplementsPorts(t *testing.T) {
	p := newInitializedProvider(t)
	var _ ports.Meter = p.Meter()
}
