// Package otel provides the OpenTelemetry SDK adapter for VibeWarden.
//
// It initializes the MeterProvider with configured exporters (Prometheus, OTLP, or both)
// and implements ports.OTelProvider. The provider is the single source of truth for OTel
// SDK lifecycle management.
//
// # Prometheus fallback behavior
//
// When no explicit telemetry configuration is provided, Prometheus export is enabled by
// default (telemetry.prometheus.enabled = true) and OTLP is disabled. This guarantees
// that /_vibewarden/metrics always serves valid Prometheus text format out of the box,
// preserving backward compatibility with existing scrapers and dashboards.
//
// Users who want push-based export can enable OTLP alongside or instead of Prometheus.
// The only invalid configuration is disabling both exporters simultaneously, which causes
// Init to return an error ("at least one exporter must be enabled").
package otel

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	prometheusclient "github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/vibewarden/vibewarden/internal/ports"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	otelprom "go.opentelemetry.io/otel/exporters/prometheus"
	otelmetric "go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
)

// Provider implements ports.OTelProvider using the OTel Go SDK.
// It supports both Prometheus and OTLP exporters, configured via Init.
// All methods are safe for concurrent use after Init returns.
type Provider struct {
	mu             sync.RWMutex
	meterProvider  *sdkmetric.MeterProvider
	tracerProvider *sdktrace.TracerProvider
	meter          otelmetric.Meter
	tracer         trace.Tracer
	handler        http.Handler
	registry       *prometheusclient.Registry

	promEnabled  bool
	otlpEnabled  bool
	traceEnabled bool
}

// NewProvider creates an uninitialized Provider.
// Call Init before using any other methods.
func NewProvider() *Provider {
	return &Provider{}
}

// Init initializes the OTel SDK with configured exporters.
// serviceName and serviceVersion are recorded as OTel resource attributes.
// Returns an error if Init has already been called, if no exporters are enabled,
// if OTLP is enabled without an endpoint, if an unsupported protocol is requested,
// or if traces are enabled without the OTLP exporter.
func (p *Provider) Init(ctx context.Context, serviceName, serviceVersion string, cfg ports.TelemetryConfig) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.meterProvider != nil {
		return fmt.Errorf("otel provider already initialized")
	}

	// Validate config.
	if !cfg.Prometheus.Enabled && !cfg.OTLP.Enabled {
		return fmt.Errorf("at least one exporter must be enabled")
	}
	if cfg.OTLP.Enabled && cfg.OTLP.Endpoint == "" {
		return fmt.Errorf("OTLP endpoint required when OTLP exporter is enabled")
	}
	if cfg.OTLP.Enabled && cfg.OTLP.Protocol != "" && cfg.OTLP.Protocol != "http" {
		return fmt.Errorf("unsupported protocol: %s", cfg.OTLP.Protocol)
	}
	if cfg.Traces.Enabled && !cfg.OTLP.Enabled {
		return fmt.Errorf("traces require OTLP exporter to be enabled")
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

	// Collect options for each enabled exporter.
	var opts []sdkmetric.Option
	opts = append(opts, sdkmetric.WithResource(res))

	// Prometheus exporter (pull-based).
	if cfg.Prometheus.Enabled {
		p.registry = prometheusclient.NewRegistry()
		promExporter, err := otelprom.New(
			otelprom.WithRegisterer(p.registry),
			otelprom.WithoutScopeInfo(),
		)
		if err != nil {
			return fmt.Errorf("creating prometheus exporter: %w", err)
		}
		opts = append(opts, sdkmetric.WithReader(promExporter))
		p.handler = promhttp.HandlerFor(p.registry, promhttp.HandlerOpts{
			EnableOpenMetrics: true,
		})
		p.promEnabled = true
	}

	// OTLP HTTP exporter (push-based).
	if cfg.OTLP.Enabled {
		interval := cfg.OTLP.Interval
		if interval == 0 {
			interval = 30 * time.Second
		}

		// Build OTLP HTTP exporter options.
		otlpOpts := []otlpmetrichttp.Option{
			otlpmetrichttp.WithEndpointURL(cfg.OTLP.Endpoint),
		}
		if len(cfg.OTLP.Headers) > 0 {
			otlpOpts = append(otlpOpts, otlpmetrichttp.WithHeaders(cfg.OTLP.Headers))
		}

		otlpExporter, err := otlpmetrichttp.New(ctx, otlpOpts...)
		if err != nil {
			return fmt.Errorf("creating otlp exporter: %w", err)
		}

		// Periodic reader pushes metrics at the configured interval.
		periodicReader := sdkmetric.NewPeriodicReader(otlpExporter,
			sdkmetric.WithInterval(interval),
		)
		opts = append(opts, sdkmetric.WithReader(periodicReader))
		p.otlpEnabled = true
	}

	// Create MeterProvider with all configured readers.
	p.meterProvider = sdkmetric.NewMeterProvider(opts...)

	// Set as global provider for any code that uses otel.GetMeterProvider().
	otel.SetMeterProvider(p.meterProvider)

	// Create the application meter.
	p.meter = p.meterProvider.Meter("github.com/vibewarden/vibewarden")

	// TracerProvider — only initialized when traces and OTLP are both enabled.
	if cfg.Traces.Enabled {
		traceExporter, err := otlptracehttp.New(ctx,
			otlptracehttp.WithEndpointURL(cfg.OTLP.Endpoint),
		)
		if err != nil {
			return fmt.Errorf("creating otlp trace exporter: %w", err)
		}

		bsp := sdktrace.NewBatchSpanProcessor(traceExporter)
		p.tracerProvider = sdktrace.NewTracerProvider(
			sdktrace.WithResource(res),
			sdktrace.WithSpanProcessor(bsp),
		)

		// Set as global tracer provider.
		otel.SetTracerProvider(p.tracerProvider)

		// Create the application tracer.
		p.tracer = p.tracerProvider.Tracer("github.com/vibewarden/vibewarden")
		p.traceEnabled = true
	}

	return nil
}

// Shutdown gracefully shuts down the TracerProvider and MeterProvider, flushing
// any buffered data. TracerProvider is shut down first so that pending spans are
// flushed before metrics are finalized.
// It is safe to call Shutdown on an uninitialized provider; it returns nil.
func (p *Provider) Shutdown(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.meterProvider == nil {
		return nil
	}

	// Shut down TracerProvider first (flushes pending spans).
	if p.tracerProvider != nil {
		if err := p.tracerProvider.Shutdown(ctx); err != nil {
			slog.ErrorContext(ctx, "shutting down tracer provider", slog.Any("error", err))
		}
	}

	return p.meterProvider.Shutdown(ctx)
}

// Handler returns the Prometheus metrics HTTP handler for scraping.
// Returns nil if Prometheus exporter is disabled or Init has not been called.
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

// Tracer returns a ports.Tracer wrapping the OTel SDK tracer.
// Returns nil if tracing is disabled or Init has not been called.
func (p *Provider) Tracer() ports.Tracer {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.tracer == nil {
		return nil
	}
	return &tracerAdapter{t: p.tracer}
}

// PrometheusEnabled returns true if the Prometheus exporter is active.
func (p *Provider) PrometheusEnabled() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.promEnabled
}

// OTLPEnabled returns true if the OTLP exporter is active.
func (p *Provider) OTLPEnabled() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.otlpEnabled
}

// TracingEnabled returns true if the tracing exporter is active.
func (p *Provider) TracingEnabled() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.traceEnabled
}
