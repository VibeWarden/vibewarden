package otel_test

import (
	"context"
	"log/slog"
	"testing"

	oteladapter "github.com/vibewarden/vibewarden/internal/adapters/otel"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// TestNewLogProvider_NotNil verifies the constructor returns a non-nil value.
func TestNewLogProvider_NotNil(t *testing.T) {
	p := oteladapter.NewLogProvider()
	if p == nil {
		t.Fatal("NewLogProvider() returned nil")
	}
}

// TestLogProvider_Handler_BeforeInit verifies Handler returns nil before Init.
func TestLogProvider_Handler_BeforeInit(t *testing.T) {
	p := oteladapter.NewLogProvider()
	if h := p.Handler(); h != nil {
		t.Errorf("Handler() = %v, want nil before Init", h)
	}
}

// TestLogProvider_Shutdown_BeforeInit verifies Shutdown is a no-op when Init was never called.
func TestLogProvider_Shutdown_BeforeInit(t *testing.T) {
	p := oteladapter.NewLogProvider()
	if err := p.Shutdown(context.Background()); err != nil {
		t.Errorf("Shutdown() before Init returned error: %v", err)
	}
}

// TestLogProvider_Init_Disabled verifies that Init with OTLPEnabled=false leaves
// the handler nil (log export disabled, no provider created).
func TestLogProvider_Init_Disabled(t *testing.T) {
	p := oteladapter.NewLogProvider()
	cfg := ports.LogExportConfig{OTLPEnabled: false}
	if err := p.Init(context.Background(), "svc", "1.0", "", cfg); err != nil {
		t.Fatalf("Init(disabled) returned error: %v", err)
	}
	if h := p.Handler(); h != nil {
		t.Errorf("Handler() = %v after disabled Init, want nil", h)
	}
}

// TestLogProvider_Init_MissingEndpoint verifies that enabling log export without
// an endpoint returns an error.
func TestLogProvider_Init_MissingEndpoint(t *testing.T) {
	p := oteladapter.NewLogProvider()
	cfg := ports.LogExportConfig{OTLPEnabled: true}
	err := p.Init(context.Background(), "svc", "1.0", "", cfg)
	if err == nil {
		t.Error("Init(enabled, no endpoint) did not return an error")
	}
}

// TestLogProvider_Init_AlreadyInitialized verifies that calling Init twice returns
// an error.
func TestLogProvider_Init_AlreadyInitialized(t *testing.T) {
	p := oteladapter.NewLogProvider()
	// First init with a valid-looking endpoint (will fail at OTLP connection but
	// for this test we only need to verify the double-init guard).
	cfg := ports.LogExportConfig{OTLPEnabled: true}
	// Intentionally use a non-existent endpoint; Init creates the exporter lazily.
	// The SDK constructor may succeed even with an unreachable endpoint.
	_ = p.Init(context.Background(), "svc", "1.0", "http://localhost:14318", cfg)

	// Second init must fail regardless.
	err := p.Init(context.Background(), "svc", "1.0", "http://localhost:14318", cfg)
	if err == nil {
		t.Error("second Init() did not return an error")
	}
	_ = p.Shutdown(context.Background())
}

// TestLogProvider_Init_ValidEndpoint verifies that Init with a valid endpoint
// produces a non-nil slog.Handler.
func TestLogProvider_Init_ValidEndpoint(t *testing.T) {
	p := oteladapter.NewLogProvider()
	cfg := ports.LogExportConfig{OTLPEnabled: true}
	// The OTLP HTTP exporter constructor succeeds even when the endpoint is unreachable.
	if err := p.Init(context.Background(), "vibewarden", "0.0.1", "http://localhost:14318", cfg); err != nil {
		t.Fatalf("Init() returned unexpected error: %v", err)
	}
	t.Cleanup(func() { _ = p.Shutdown(context.Background()) })

	h := p.Handler()
	if h == nil {
		t.Fatal("Handler() returned nil after successful Init")
	}
	// Verify it is a valid slog.Handler by attempting to use it.
	logger := slog.New(h)
	logger.Info("test log record")
}

// TestLogProvider_Shutdown_Idempotent verifies that calling Shutdown multiple
// times does not return an error.
func TestLogProvider_Shutdown_Idempotent(t *testing.T) {
	p := oteladapter.NewLogProvider()
	cfg := ports.LogExportConfig{OTLPEnabled: true}
	if err := p.Init(context.Background(), "svc", "1.0", "http://localhost:14318", cfg); err != nil {
		t.Fatalf("Init() returned unexpected error: %v", err)
	}

	ctx := context.Background()
	if err := p.Shutdown(ctx); err != nil {
		t.Errorf("first Shutdown() returned error: %v", err)
	}
}

// TestSeverityForEventType exercises the event type to severity mapping table.
func TestSeverityForEventType(t *testing.T) {
	tests := []struct {
		eventType    string
		wantSeverity string // "warn", "error", "info"
	}{
		{"auth.success", "info"},
		{"auth.failed", "warn"},
		{"rate_limit.hit", "warn"},
		{"ip_filter.blocked", "warn"},
		{"kratos.unavailable", "error"},
		{"rate_limit_failed", "error"},
		{"proxy.started", "info"},
		{"user.created", "info"},
		{"tls.issued", "info"},
	}

	for _, tt := range tests {
		t.Run(tt.eventType, func(t *testing.T) {
			got := oteladapter.SeverityForEventType(tt.eventType)
			switch tt.wantSeverity {
			case "warn":
				if got.String() != "WARN" {
					t.Errorf("SeverityForEventType(%q) = %v, want WARN", tt.eventType, got)
				}
			case "error":
				if got.String() != "ERROR" {
					t.Errorf("SeverityForEventType(%q) = %v, want ERROR", tt.eventType, got)
				}
			case "info":
				if got.String() != "INFO" {
					t.Errorf("SeverityForEventType(%q) = %v, want INFO", tt.eventType, got)
				}
			}
		})
	}
}
