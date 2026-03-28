// Package otel provides the OpenTelemetry SDK adapter for VibeWarden.
package otel

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"go.opentelemetry.io/contrib/bridges/otelslog"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	otellog "go.opentelemetry.io/otel/log"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"

	"github.com/vibewarden/vibewarden/internal/ports"
)

// LogProvider implements ports.LoggerProvider using the OTel Log SDK.
// It initializes an OTel LoggerProvider with an OTLP HTTP exporter and exposes
// an slog.Handler via the otelslog bridge that forwards slog records to OTel.
// All methods are safe for concurrent use after Init returns.
type LogProvider struct {
	mu             sync.RWMutex
	loggerProvider *sdklog.LoggerProvider
	handler        slog.Handler
}

// NewLogProvider creates an uninitialized LogProvider.
// Call Init before using Handler or Shutdown.
func NewLogProvider() *LogProvider {
	return &LogProvider{}
}

// Init initializes the OTel LoggerProvider with an OTLP HTTP exporter.
// serviceName and serviceVersion are recorded as OTel resource attributes.
// otlpEndpoint is the OTLP HTTP endpoint (e.g. "http://localhost:4318").
// Returns an error if cfg.OTLPEnabled is true but otlpEndpoint is empty,
// if Init has already been called, or if SDK initialization fails.
func (p *LogProvider) Init(ctx context.Context, serviceName, serviceVersion, otlpEndpoint string, cfg ports.LogExportConfig) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.loggerProvider != nil {
		return fmt.Errorf("log provider already initialized")
	}
	if !cfg.OTLPEnabled {
		// Nothing to initialize when log export is disabled.
		return nil
	}
	if otlpEndpoint == "" {
		return fmt.Errorf("otlp endpoint required when log export is enabled")
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

	// Create OTLP HTTP log exporter.
	exporter, err := otlploghttp.New(ctx,
		otlploghttp.WithEndpointURL(otlpEndpoint),
	)
	if err != nil {
		return fmt.Errorf("creating otlp log exporter: %w", err)
	}

	// Create LoggerProvider with batch processor.
	p.loggerProvider = sdklog.NewLoggerProvider(
		sdklog.WithResource(res),
		sdklog.WithProcessor(sdklog.NewBatchProcessor(exporter)),
	)

	// Create the otelslog bridge handler.
	p.handler = otelslog.NewHandler(
		"github.com/vibewarden/vibewarden",
		otelslog.WithLoggerProvider(p.loggerProvider),
		otelslog.WithVersion(serviceVersion),
		otelslog.WithSchemaURL(semconv.SchemaURL),
	)

	return nil
}

// Handler returns the otelslog bridge slog.Handler.
// Returns nil if Init has not been called or log export is disabled.
func (p *LogProvider) Handler() slog.Handler {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.handler
}

// Shutdown gracefully shuts down the LoggerProvider, flushing pending log batches.
// It is safe to call Shutdown on an uninitialized or disabled provider; it returns nil.
func (p *LogProvider) Shutdown(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.loggerProvider == nil {
		return nil
	}
	return p.loggerProvider.Shutdown(ctx)
}

// SeverityForEventType maps a VibeWarden event type string to an OTel log severity.
// The mapping is based on the semantic meaning of the event type suffix:
//
//   - "*.failed", "*.blocked", "*.hit"            → WARN
//   - "*.unavailable", "*_failed"                  → ERROR
//   - default                                       → INFO
//
// This function is exported for use by a future custom slog handler that will
// enrich log records with OTel severity before passing them to the bridge.
func SeverityForEventType(eventType string) otellog.Severity {
	switch {
	case strings.HasSuffix(eventType, ".failed"),
		strings.HasSuffix(eventType, ".blocked"),
		strings.HasSuffix(eventType, ".hit"):
		return otellog.SeverityWarn
	case strings.HasSuffix(eventType, ".unavailable"),
		strings.HasSuffix(eventType, "_failed"):
		return otellog.SeverityError
	default:
		return otellog.SeverityInfo
	}
}
