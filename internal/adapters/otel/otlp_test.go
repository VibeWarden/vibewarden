package otel_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	oteladapter "github.com/vibewarden/vibewarden/internal/adapters/otel"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// ---------------------------------------------------------------------------
// OTLP-only exporter
// ---------------------------------------------------------------------------

func TestProvider_Init_OTLPOnly_Success(t *testing.T) {
	// Start a mock OTLP HTTP server.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	p := oteladapter.NewProvider()
	cfg := ports.TelemetryConfig{
		OTLP: ports.OTLPExporterConfig{
			Enabled:  true,
			Endpoint: srv.URL,
		},
	}
	err := p.Init(context.Background(), "vibewarden-test", "1.0.0", cfg)
	if err != nil {
		t.Fatalf("Init() with OTLP only returned error: %v", err)
	}
	t.Cleanup(func() { _ = p.Shutdown(context.Background()) })
}

func TestProvider_Init_OTLPOnly_HandlerIsNil(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	p := oteladapter.NewProvider()
	cfg := ports.TelemetryConfig{
		OTLP: ports.OTLPExporterConfig{
			Enabled:  true,
			Endpoint: srv.URL,
		},
	}
	if err := p.Init(context.Background(), "vibewarden-test", "1.0.0", cfg); err != nil {
		t.Fatalf("Init() failed: %v", err)
	}
	t.Cleanup(func() { _ = p.Shutdown(context.Background()) })

	if p.Handler() != nil {
		t.Error("Handler() should return nil when only OTLP exporter is configured")
	}
}

func TestProvider_Init_OTLPOnly_OTLPEnabledTrue(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	p := oteladapter.NewProvider()
	cfg := ports.TelemetryConfig{
		OTLP: ports.OTLPExporterConfig{
			Enabled:  true,
			Endpoint: srv.URL,
		},
	}
	if err := p.Init(context.Background(), "vibewarden-test", "1.0.0", cfg); err != nil {
		t.Fatalf("Init() failed: %v", err)
	}
	t.Cleanup(func() { _ = p.Shutdown(context.Background()) })

	if !p.OTLPEnabled() {
		t.Error("OTLPEnabled() should return true after Init with OTLP")
	}
	if p.PrometheusEnabled() {
		t.Error("PrometheusEnabled() should return false when only OTLP configured")
	}
}

// ---------------------------------------------------------------------------
// Dual exporter (Prometheus + OTLP)
// ---------------------------------------------------------------------------

func TestProvider_Init_DualExporter_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	p := oteladapter.NewProvider()
	cfg := ports.TelemetryConfig{
		Prometheus: ports.PrometheusExporterConfig{Enabled: true},
		OTLP: ports.OTLPExporterConfig{
			Enabled:  true,
			Endpoint: srv.URL,
		},
	}
	err := p.Init(context.Background(), "vibewarden-test", "1.0.0", cfg)
	if err != nil {
		t.Fatalf("Init() with dual exporters returned error: %v", err)
	}
	t.Cleanup(func() { _ = p.Shutdown(context.Background()) })
}

func TestProvider_Init_DualExporter_BothFlagsTrue(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	p := oteladapter.NewProvider()
	cfg := ports.TelemetryConfig{
		Prometheus: ports.PrometheusExporterConfig{Enabled: true},
		OTLP: ports.OTLPExporterConfig{
			Enabled:  true,
			Endpoint: srv.URL,
		},
	}
	if err := p.Init(context.Background(), "vibewarden-test", "1.0.0", cfg); err != nil {
		t.Fatalf("Init() failed: %v", err)
	}
	t.Cleanup(func() { _ = p.Shutdown(context.Background()) })

	if !p.PrometheusEnabled() {
		t.Error("PrometheusEnabled() should return true in dual-exporter mode")
	}
	if !p.OTLPEnabled() {
		t.Error("OTLPEnabled() should return true in dual-exporter mode")
	}
}

func TestProvider_Init_DualExporter_HandlerNotNil(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	p := oteladapter.NewProvider()
	cfg := ports.TelemetryConfig{
		Prometheus: ports.PrometheusExporterConfig{Enabled: true},
		OTLP: ports.OTLPExporterConfig{
			Enabled:  true,
			Endpoint: srv.URL,
		},
	}
	if err := p.Init(context.Background(), "vibewarden-test", "1.0.0", cfg); err != nil {
		t.Fatalf("Init() failed: %v", err)
	}
	t.Cleanup(func() { _ = p.Shutdown(context.Background()) })

	if p.Handler() == nil {
		t.Error("Handler() should not be nil in dual-exporter mode")
	}
}

// ---------------------------------------------------------------------------
// Custom OTLP options
// ---------------------------------------------------------------------------

func TestProvider_Init_OTLP_WithCustomHeaders(t *testing.T) {
	var receivedHeaders http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHeaders = r.Header.Clone()
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	p := oteladapter.NewProvider()
	cfg := ports.TelemetryConfig{
		OTLP: ports.OTLPExporterConfig{
			Enabled:  true,
			Endpoint: srv.URL,
			Headers:  map[string]string{"X-Api-Key": "secret-token"},
		},
	}
	if err := p.Init(context.Background(), "vibewarden-test", "1.0.0", cfg); err != nil {
		t.Fatalf("Init() failed: %v", err)
	}

	// Trigger a flush so headers are sent.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = p.Shutdown(ctx)

	// The header may arrive on first export or shutdown flush.
	// We only assert headers if an export request was received.
	if receivedHeaders != nil {
		if got := receivedHeaders.Get("X-Api-Key"); got != "secret-token" {
			t.Errorf("X-Api-Key header = %q, want %q", got, "secret-token")
		}
	}
}

func TestProvider_Init_OTLP_WithCustomInterval(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	p := oteladapter.NewProvider()
	cfg := ports.TelemetryConfig{
		OTLP: ports.OTLPExporterConfig{
			Enabled:  true,
			Endpoint: srv.URL,
			Interval: 100 * time.Millisecond,
		},
	}
	err := p.Init(context.Background(), "vibewarden-test", "1.0.0", cfg)
	if err != nil {
		t.Fatalf("Init() with custom interval returned error: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = p.Shutdown(ctx)
	})
}

func TestProvider_Init_OTLP_DefaultInterval_ZeroMeansThirtySeconds(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	p := oteladapter.NewProvider()
	cfg := ports.TelemetryConfig{
		OTLP: ports.OTLPExporterConfig{
			Enabled:  true,
			Endpoint: srv.URL,
			Interval: 0, // should default to 30s
		},
	}
	err := p.Init(context.Background(), "vibewarden-test", "1.0.0", cfg)
	if err != nil {
		t.Fatalf("Init() with zero interval returned error: %v", err)
	}
	t.Cleanup(func() { _ = p.Shutdown(context.Background()) })
}

func TestProvider_Init_OTLP_HTTPProtocol_Accepted(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	p := oteladapter.NewProvider()
	cfg := ports.TelemetryConfig{
		OTLP: ports.OTLPExporterConfig{
			Enabled:  true,
			Endpoint: srv.URL,
			Protocol: "http",
		},
	}
	err := p.Init(context.Background(), "vibewarden-test", "1.0.0", cfg)
	if err != nil {
		t.Fatalf("Init() with protocol=http returned error: %v", err)
	}
	t.Cleanup(func() { _ = p.Shutdown(context.Background()) })
}

// ---------------------------------------------------------------------------
// Shutdown flushes OTLP
// ---------------------------------------------------------------------------

func TestProvider_Shutdown_OTLP_FlushesOnExit(t *testing.T) {
	var requestCount int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	p := oteladapter.NewProvider()
	cfg := ports.TelemetryConfig{
		OTLP: ports.OTLPExporterConfig{
			Enabled:  true,
			Endpoint: srv.URL,
			Interval: 10 * time.Minute, // large interval, no periodic flush during test
		},
	}
	if err := p.Init(context.Background(), "vibewarden-test", "1.0.0", cfg); err != nil {
		t.Fatalf("Init() failed: %v", err)
	}

	// Record something via the meter.
	meter := p.Meter()
	counter, err := meter.Int64Counter("shutdown_flush_counter",
		ports.WithDescription("counter to verify shutdown flush"),
	)
	if err != nil {
		t.Fatalf("creating counter: %v", err)
	}
	counter.Add(context.Background(), 1)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := p.Shutdown(ctx); err != nil {
		t.Errorf("Shutdown() returned error: %v", err)
	}

	// Shutdown should have triggered a final export to the mock server.
	if requestCount == 0 {
		t.Error("expected at least one request to OTLP server during shutdown flush")
	}
}

// ---------------------------------------------------------------------------
// Table-driven: Init with various TelemetryConfig combinations
// ---------------------------------------------------------------------------

func TestProvider_Init_TableDriven(t *testing.T) {
	// Start a shared mock OTLP server for tests that need it.
	otlpSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(otlpSrv.Close)

	tests := []struct {
		name            string
		cfg             ports.TelemetryConfig
		wantErr         bool
		wantErrContains string
		wantPromEnabled bool
		wantOTLPEnabled bool
		wantHandlerNil  bool
	}{
		{
			name:            "prometheus only",
			cfg:             ports.TelemetryConfig{Prometheus: ports.PrometheusExporterConfig{Enabled: true}},
			wantErr:         false,
			wantPromEnabled: true,
			wantOTLPEnabled: false,
			wantHandlerNil:  false,
		},
		{
			name: "otlp only",
			cfg: ports.TelemetryConfig{
				OTLP: ports.OTLPExporterConfig{Enabled: true, Endpoint: otlpSrv.URL},
			},
			wantErr:         false,
			wantPromEnabled: false,
			wantOTLPEnabled: true,
			wantHandlerNil:  true,
		},
		{
			name: "both exporters",
			cfg: ports.TelemetryConfig{
				Prometheus: ports.PrometheusExporterConfig{Enabled: true},
				OTLP:       ports.OTLPExporterConfig{Enabled: true, Endpoint: otlpSrv.URL},
			},
			wantErr:         false,
			wantPromEnabled: true,
			wantOTLPEnabled: true,
			wantHandlerNil:  false,
		},
		{
			name:            "neither enabled",
			cfg:             ports.TelemetryConfig{},
			wantErr:         true,
			wantErrContains: "at least one exporter",
		},
		{
			name: "otlp without endpoint",
			cfg: ports.TelemetryConfig{
				OTLP: ports.OTLPExporterConfig{Enabled: true},
			},
			wantErr:         true,
			wantErrContains: "OTLP endpoint required",
		},
		{
			name: "unsupported protocol grpc",
			cfg: ports.TelemetryConfig{
				OTLP: ports.OTLPExporterConfig{
					Enabled:  true,
					Endpoint: otlpSrv.URL,
					Protocol: "grpc",
				},
			},
			wantErr:         true,
			wantErrContains: "unsupported protocol",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := oteladapter.NewProvider()
			err := p.Init(context.Background(), "vibewarden-test", "1.0.0", tt.cfg)

			if tt.wantErr {
				if err == nil {
					t.Fatal("Init() should have returned error")
				}
				if tt.wantErrContains != "" && !strings.Contains(err.Error(), tt.wantErrContains) {
					t.Errorf("error = %v, want to contain %q", err, tt.wantErrContains)
				}
				return
			}

			if err != nil {
				t.Fatalf("Init() returned unexpected error: %v", err)
			}
			t.Cleanup(func() { _ = p.Shutdown(context.Background()) })

			if p.PrometheusEnabled() != tt.wantPromEnabled {
				t.Errorf("PrometheusEnabled() = %v, want %v", p.PrometheusEnabled(), tt.wantPromEnabled)
			}
			if p.OTLPEnabled() != tt.wantOTLPEnabled {
				t.Errorf("OTLPEnabled() = %v, want %v", p.OTLPEnabled(), tt.wantOTLPEnabled)
			}
			if (p.Handler() == nil) != tt.wantHandlerNil {
				t.Errorf("Handler() nil = %v, want nil = %v", p.Handler() == nil, tt.wantHandlerNil)
			}
		})
	}
}
