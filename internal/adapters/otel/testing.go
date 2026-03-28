// Package otel provides the OpenTelemetry SDK adapter for VibeWarden.
package otel

import (
	"context"
	"net/http"
	"net/http/httptest"

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

// NewTestLogProvider creates a LogProvider backed by an in-process httptest.Server
// that acts as a fake OTLP endpoint. It initializes the provider so that Handler
// returns a valid slog.Handler. The second return value is a function to retrieve
// the raw request bodies received by the fake endpoint. The third return value is
// the httptest.Server closer — callers must call it when the test ends.
//
// Typical test usage:
//
//	p, bodies, closeFn := otel.NewTestLogProvider(ctx)
//	defer closeFn()
//	t.Cleanup(func() { _ = p.Shutdown(context.Background()) })
func NewTestLogProvider(ctx context.Context) (*LogProvider, func() [][]byte, func(), error) {
	var received [][]byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := func() ([]byte, error) {
			defer r.Body.Close()
			var buf []byte
			b := make([]byte, 4096)
			for {
				n, err := r.Body.Read(b)
				if n > 0 {
					buf = append(buf, b[:n]...)
				}
				if err != nil {
					break
				}
			}
			return buf, nil
		}()
		received = append(received, body)
		w.WriteHeader(http.StatusOK)
	}))

	p := NewLogProvider()
	cfg := ports.LogExportConfig{OTLPEnabled: true}
	if err := p.Init(ctx, "vibewarden-test", "0.0.0-test", srv.URL, cfg); err != nil {
		srv.Close()
		return nil, nil, nil, err
	}

	bodies := func() [][]byte { return received }
	closeFn := srv.Close
	return p, bodies, closeFn, nil
}
