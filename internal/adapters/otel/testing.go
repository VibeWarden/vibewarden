// Package otel provides the OpenTelemetry SDK adapter for VibeWarden.
package otel

import (
	"context"

	"github.com/vibewarden/vibewarden/internal/ports"
)

// NewTestProvider creates an OTelProvider with only the Prometheus exporter enabled,
// ready for use in tests. It initializes the provider and returns it ready for use.
//
// This helper exists so that tests do not need to repeat the boilerplate of
// constructing a TelemetryConfig and calling Init. The returned provider uses
// isolated Prometheus registries (not the global default) so parallel tests do
// not interfere with each other.
//
// Callers are responsible for calling Shutdown on the returned provider when
// the test completes:
//
//	provider, err := otel.NewTestProvider(ctx)
//	if err != nil { ... }
//	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
func NewTestProvider(ctx context.Context) (*Provider, error) {
	p := NewProvider()
	cfg := ports.TelemetryConfig{
		Prometheus: ports.PrometheusExporterConfig{Enabled: true},
		OTLP:       ports.OTLPExporterConfig{Enabled: false},
	}
	if err := p.Init(ctx, "vibewarden-test", "0.0.0-test", cfg); err != nil {
		return nil, err
	}
	return p, nil
}
