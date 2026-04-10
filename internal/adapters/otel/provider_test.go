package otel_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	oteladapter "github.com/vibewarden/vibewarden/internal/adapters/otel"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// defaultPromCfg returns a TelemetryConfig with only Prometheus enabled —
// the same behaviour as the old Init(ctx, name, version) call.
func defaultPromCfg() ports.TelemetryConfig {
	return ports.TelemetryConfig{
		Prometheus: ports.PrometheusExporterConfig{Enabled: true},
	}
}

func TestNewProvider_ReturnsNonNil(t *testing.T) {
	p := oteladapter.NewProvider()
	if p == nil {
		t.Fatal("NewProvider() returned nil")
	}
}

func TestProvider_HandlerBeforeInit_ReturnsNil(t *testing.T) {
	p := oteladapter.NewProvider()
	if h := p.Handler(); h != nil {
		t.Error("Handler() before Init should return nil")
	}
}

func TestProvider_MeterBeforeInit_ReturnsNil(t *testing.T) {
	p := oteladapter.NewProvider()
	if m := p.Meter(); m != nil {
		t.Error("Meter() before Init should return nil")
	}
}

func TestProvider_ShutdownBeforeInit_IsNoop(t *testing.T) {
	p := oteladapter.NewProvider()
	if err := p.Shutdown(context.Background()); err != nil {
		t.Errorf("Shutdown() before Init returned error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Init — Prometheus only (regression tests for legacy behaviour)
// ---------------------------------------------------------------------------

func TestProvider_Init_PrometheusOnly_Success(t *testing.T) {
	p := oteladapter.NewProvider()
	err := p.Init(context.Background(), "vibewarden-test", "1.0.0", defaultPromCfg())
	if err != nil {
		t.Fatalf("Init() returned unexpected error: %v", err)
	}
	t.Cleanup(func() { _ = p.Shutdown(context.Background()) })
}

func TestProvider_Init_Twice_ReturnsError(t *testing.T) {
	p := oteladapter.NewProvider()
	if err := p.Init(context.Background(), "vibewarden-test", "1.0.0", defaultPromCfg()); err != nil {
		t.Fatalf("first Init() failed: %v", err)
	}
	t.Cleanup(func() { _ = p.Shutdown(context.Background()) })

	err := p.Init(context.Background(), "vibewarden-test", "1.0.0", defaultPromCfg())
	if err == nil {
		t.Error("second Init() should return an error")
	}
	if !strings.Contains(err.Error(), "already initialized") {
		t.Errorf("expected 'already initialized' error, got: %v", err)
	}
}

func TestProvider_HandlerAfterInit_PromEnabled_NotNil(t *testing.T) {
	p := oteladapter.NewProvider()
	if err := p.Init(context.Background(), "vibewarden-test", "1.0.0", defaultPromCfg()); err != nil {
		t.Fatalf("Init() failed: %v", err)
	}
	t.Cleanup(func() { _ = p.Shutdown(context.Background()) })

	if h := p.Handler(); h == nil {
		t.Error("Handler() after Init with Prometheus enabled should not return nil")
	}
}

func TestProvider_MeterAfterInit_NotNil(t *testing.T) {
	p := oteladapter.NewProvider()
	if err := p.Init(context.Background(), "vibewarden-test", "1.0.0", defaultPromCfg()); err != nil {
		t.Fatalf("Init() failed: %v", err)
	}
	t.Cleanup(func() { _ = p.Shutdown(context.Background()) })

	if m := p.Meter(); m == nil {
		t.Error("Meter() after Init should not return nil")
	}
}

func TestProvider_ImplementsOTelProvider(t *testing.T) {
	var _ ports.OTelProvider = (*oteladapter.Provider)(nil)
}

func TestProvider_Handler_ServesMetrics(t *testing.T) {
	p := oteladapter.NewProvider()
	if err := p.Init(context.Background(), "vibewarden-test", "1.0.0", defaultPromCfg()); err != nil {
		t.Fatalf("Init() failed: %v", err)
	}
	t.Cleanup(func() { _ = p.Shutdown(context.Background()) })

	handler := p.Handler()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("handler returned status %d, want 200", rr.Code)
	}
}

func TestProvider_Handler_ExposesOTelMetrics(t *testing.T) {
	p := oteladapter.NewProvider()
	if err := p.Init(context.Background(), "vibewarden-test", "1.0.0", defaultPromCfg()); err != nil {
		t.Fatalf("Init() failed: %v", err)
	}
	t.Cleanup(func() { _ = p.Shutdown(context.Background()) })

	// Create a counter and record a value.
	meter := p.Meter()
	counter, err := meter.Int64Counter("test_counter_total",
		ports.WithDescription("A test counter."),
	)
	if err != nil {
		t.Fatalf("creating counter: %v", err)
	}
	counter.Add(context.Background(), 42)

	// Scrape the metrics endpoint.
	handler := p.Handler()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	body, _ := io.ReadAll(rr.Body)
	bodyStr := string(body)

	if !strings.Contains(bodyStr, "test_counter_total") {
		t.Errorf("metrics output does not contain 'test_counter_total'\n\nFull output:\n%s", bodyStr)
	}
}

func TestProvider_Shutdown_Succeeds(t *testing.T) {
	p := oteladapter.NewProvider()
	if err := p.Init(context.Background(), "vibewarden-test", "1.0.0", defaultPromCfg()); err != nil {
		t.Fatalf("Init() failed: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := p.Shutdown(ctx); err != nil {
		t.Errorf("Shutdown() returned error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Init — validation error cases
// ---------------------------------------------------------------------------

func TestProvider_Init_NeitherExporterEnabled_ReturnsError(t *testing.T) {
	p := oteladapter.NewProvider()
	cfg := ports.TelemetryConfig{
		Prometheus: ports.PrometheusExporterConfig{Enabled: false},
		OTLP:       ports.OTLPExporterConfig{Enabled: false},
	}
	err := p.Init(context.Background(), "vibewarden-test", "1.0.0", cfg)
	if err == nil {
		t.Fatal("Init() with no exporters should return error")
	}
	if !strings.Contains(err.Error(), "at least one exporter") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestProvider_Init_OTLPEnabledNoEndpoint_ReturnsError(t *testing.T) {
	p := oteladapter.NewProvider()
	cfg := ports.TelemetryConfig{
		OTLP: ports.OTLPExporterConfig{Enabled: true, Endpoint: ""},
	}
	err := p.Init(context.Background(), "vibewarden-test", "1.0.0", cfg)
	if err == nil {
		t.Fatal("Init() with OTLP enabled and no endpoint should return error")
	}
	if !strings.Contains(err.Error(), "OTLP endpoint required") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestProvider_Init_UnsupportedProtocol_ReturnsError(t *testing.T) {
	p := oteladapter.NewProvider()
	cfg := ports.TelemetryConfig{
		OTLP: ports.OTLPExporterConfig{
			Enabled:  true,
			Endpoint: "http://localhost:4318",
			Protocol: "grpc",
		},
	}
	err := p.Init(context.Background(), "vibewarden-test", "1.0.0", cfg)
	if err == nil {
		t.Fatal("Init() with grpc protocol should return error")
	}
	if !strings.Contains(err.Error(), "unsupported protocol") {
		t.Errorf("unexpected error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// PrometheusEnabled / OTLPEnabled
// ---------------------------------------------------------------------------

func TestProvider_PrometheusEnabled_BeforeInit_False(t *testing.T) {
	p := oteladapter.NewProvider()
	if p.PrometheusEnabled() {
		t.Error("PrometheusEnabled() before Init should return false")
	}
}

func TestProvider_OTLPEnabled_BeforeInit_False(t *testing.T) {
	p := oteladapter.NewProvider()
	if p.OTLPEnabled() {
		t.Error("OTLPEnabled() before Init should return false")
	}
}

func TestProvider_PrometheusEnabled_AfterInit_PrometheusOnly(t *testing.T) {
	p := oteladapter.NewProvider()
	if err := p.Init(context.Background(), "vibewarden-test", "1.0.0", defaultPromCfg()); err != nil {
		t.Fatalf("Init() failed: %v", err)
	}
	t.Cleanup(func() { _ = p.Shutdown(context.Background()) })

	if !p.PrometheusEnabled() {
		t.Error("PrometheusEnabled() should return true after Init with Prometheus")
	}
	if p.OTLPEnabled() {
		t.Error("OTLPEnabled() should return false when only Prometheus configured")
	}
}
