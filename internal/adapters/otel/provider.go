// Package otel provides the OpenTelemetry SDK adapter for VibeWarden.
//
// It initializes the MeterProvider with a Prometheus exporter and implements
// ports.OTelProvider. The provider is the single source of truth for OTel
// SDK lifecycle management.
package otel

import (
	"context"
	"fmt"
	"net/http"
	"sync"

	prometheusclient "github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/vibewarden/vibewarden/internal/ports"
	"go.opentelemetry.io/otel"
	otelprom "go.opentelemetry.io/otel/exporters/prometheus"
	otelmetric "go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

// Provider implements ports.OTelProvider using the OTel Go SDK.
// It must be created with NewProvider and initialized with Init before use.
// All methods are safe for concurrent use after Init returns.
type Provider struct {
	mu            sync.RWMutex
	meterProvider *sdkmetric.MeterProvider
	meter         otelmetric.Meter
	handler       http.Handler
	registry      *prometheusclient.Registry
}

// NewProvider creates an uninitialized Provider.
// Call Init before using any other methods.
func NewProvider() *Provider {
	return &Provider{}
}

// Init initializes the OTel SDK with a Prometheus exporter.
// serviceName and serviceVersion are recorded as OTel resource attributes.
// Returns an error if Init has already been called or if initialization fails.
func (p *Provider) Init(ctx context.Context, serviceName, serviceVersion string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.meterProvider != nil {
		return fmt.Errorf("otel provider already initialized")
	}

	// Create a dedicated Prometheus registry (not the global default).
	p.registry = prometheusclient.NewRegistry()

	// Create Prometheus exporter with the isolated registry.
	exporter, err := otelprom.New(
		otelprom.WithRegisterer(p.registry),
		otelprom.WithoutScopeInfo(),
	)
	if err != nil {
		return fmt.Errorf("creating prometheus exporter: %w", err)
	}

	// Build resource with service identity.
	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceName(serviceName),
			semconv.ServiceVersion(serviceVersion),
		),
	)
	if err != nil {
		return fmt.Errorf("creating otel resource: %w", err)
	}

	// Create MeterProvider with the Prometheus exporter.
	p.meterProvider = sdkmetric.NewMeterProvider(
		sdkmetric.WithResource(res),
		sdkmetric.WithReader(exporter),
	)

	// Set as global provider for any code that uses otel.GetMeterProvider().
	otel.SetMeterProvider(p.meterProvider)

	// Create the application meter.
	p.meter = p.meterProvider.Meter("github.com/vibewarden/vibewarden")

	// Create the HTTP handler for Prometheus scraping.
	p.handler = promhttp.HandlerFor(p.registry, promhttp.HandlerOpts{
		EnableOpenMetrics: true,
	})

	return nil
}

// Shutdown gracefully shuts down the MeterProvider, flushing any buffered data.
// It is safe to call Shutdown on an uninitialized provider; it returns nil.
func (p *Provider) Shutdown(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.meterProvider == nil {
		return nil
	}
	return p.meterProvider.Shutdown(ctx)
}

// Handler returns the Prometheus metrics HTTP handler for scraping.
// Returns nil if Init has not been called.
func (p *Provider) Handler() http.Handler {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.handler
}

// Meter returns a ports.Meter wrapping the OTel SDK meter.
// Returns nil if Init has not been called.
func (p *Provider) Meter() ports.Meter {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.meter == nil {
		return nil
	}
	return &meterAdapter{m: p.meter}
}
