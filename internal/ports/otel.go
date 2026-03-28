// Package ports defines the interfaces (ports) for VibeWarden's hexagonal architecture.
package ports

import (
	"context"
	"log/slog"
	"net/http"
	"time"
)

// TelemetryConfig holds all telemetry export settings.
// It is passed to OTelProvider.Init to configure exporters.
type TelemetryConfig struct {
	// Prometheus enables the Prometheus pull-based exporter.
	// When enabled, metrics are available at /_vibewarden/metrics.
	Prometheus PrometheusExporterConfig

	// OTLP enables the OTLP push-based exporter.
	// When enabled, metrics are pushed to the configured endpoint.
	OTLP OTLPExporterConfig

	// Logs configures structured event log export settings.
	Logs LogExportConfig

	// Traces configures distributed tracing settings.
	Traces TraceExportConfig
}

// TraceExportConfig configures OTel tracing.
type TraceExportConfig struct {
	// Enabled toggles tracing (default: false).
	// When enabled, a span is created for each HTTP request and exported via OTLP.
	// Requires that the OTLP exporter endpoint is configured.
	Enabled bool
}

// LogExportConfig configures log export via OTLP.
type LogExportConfig struct {
	// OTLPEnabled toggles OTLP log export (default: false).
	// When enabled, structured events are exported to the same OTLP endpoint as metrics.
	// Requires that the OTLP exporter endpoint is configured.
	OTLPEnabled bool
}

// PrometheusExporterConfig configures the Prometheus pull-based exporter.
type PrometheusExporterConfig struct {
	// Enabled toggles the Prometheus exporter (default: true).
	Enabled bool
}

// OTLPExporterConfig configures the OTLP push-based exporter.
type OTLPExporterConfig struct {
	// Enabled toggles the OTLP exporter (default: false).
	Enabled bool

	// Endpoint is the OTLP HTTP endpoint URL (e.g., "http://localhost:4318").
	// Required when Enabled is true.
	Endpoint string

	// Headers are optional HTTP headers for authentication (e.g., API keys).
	// Keys are header names, values are header values.
	Headers map[string]string

	// Interval is the export interval (default: 30s).
	// Metrics are batched and pushed at this interval.
	Interval time.Duration

	// Protocol specifies the OTLP protocol: "http" or "grpc" (default: "http").
	// This version only implements "http"; "grpc" is reserved for future use.
	Protocol string
}

// OTelProvider manages the OpenTelemetry SDK lifecycle.
// It initializes the MeterProvider with configured exporters and exposes
// an HTTP handler for Prometheus scraping (when Prometheus exporter is enabled).
// Implementations must be safe for concurrent use after Init returns.
type OTelProvider interface {
	// Init initializes the OTel SDK with the given service identity and telemetry config.
	// It sets up the MeterProvider with the configured exporters (Prometheus, OTLP, or both).
	// Must be called once before any other methods.
	Init(ctx context.Context, serviceName, serviceVersion string, cfg TelemetryConfig) error

	// Shutdown gracefully shuts down the OTel SDK, flushing any buffered data.
	// For OTLP exporter, this flushes pending metrics to the endpoint.
	// Must honour the context deadline.
	Shutdown(ctx context.Context) error

	// Handler returns an http.Handler that serves Prometheus metrics.
	// Returns nil if Prometheus exporter is disabled or Init has not been called.
	Handler() http.Handler

	// Meter returns a named OTel Meter for creating instruments.
	// The scope name is "github.com/vibewarden/vibewarden".
	Meter() Meter

	// Tracer returns an OTel Tracer for creating spans.
	// Returns nil if tracing is disabled or Init has not been called.
	Tracer() Tracer

	// PrometheusEnabled returns true if the Prometheus exporter is active.
	PrometheusEnabled() bool

	// OTLPEnabled returns true if the OTLP exporter is active.
	OTLPEnabled() bool

	// TracingEnabled returns true if the tracing exporter is active.
	TracingEnabled() bool
}

// Tracer is a subset of the OTel trace.Tracer interface.
// It exposes only the span creation method VibeWarden needs, keeping application
// code decoupled from the full OTel API.
type Tracer interface {
	// Start creates a span and a context containing the newly-created span.
	// The span must be ended by calling span.End() when the operation completes.
	Start(ctx context.Context, spanName string, opts ...SpanStartOption) (context.Context, Span)
}

// Span represents a single operation within a trace.
type Span interface {
	// End marks the span as complete. Must be called exactly once.
	End()

	// SetStatus sets the span status.
	SetStatus(code SpanStatusCode, description string)

	// SetAttributes sets attributes on the span.
	SetAttributes(attrs ...Attribute)

	// RecordError records an error as a span event.
	RecordError(err error)
}

// SpanStartOption configures span creation.
type SpanStartOption interface {
	isSpanStartOption()
}

// spanKindOption carries the span kind for a SpanStartOption.
type spanKindOption struct{ kind SpanKind }

func (spanKindOption) isSpanStartOption() {}

// WithSpanKind returns a SpanStartOption that sets the span kind.
func WithSpanKind(kind SpanKind) SpanStartOption {
	return spanKindOption{kind: kind}
}

// KindOf extracts the SpanKind from a SpanStartOption slice, defaulting to SpanKindInternal.
// This is a helper for adapter implementations.
func KindOf(opts []SpanStartOption) SpanKind {
	for _, o := range opts {
		if k, ok := o.(spanKindOption); ok {
			return k.kind
		}
	}
	return SpanKindInternal
}

// SpanStatusCode represents the status of a span.
type SpanStatusCode int

const (
	// SpanStatusUnset is the default status, indicating no explicit status has been set.
	SpanStatusUnset SpanStatusCode = iota
	// SpanStatusOK indicates the span completed successfully.
	SpanStatusOK
	// SpanStatusError indicates the span completed with an error.
	SpanStatusError
)

// SpanKind is the type of span, describing the relationship between the span and its callers.
type SpanKind int

const (
	// SpanKindInternal is used for spans representing internal operations.
	SpanKindInternal SpanKind = iota
	// SpanKindServer is used for spans representing server-side handling of a request.
	SpanKindServer
	// SpanKindClient is used for spans representing client-side outgoing requests.
	SpanKindClient
)

// LoggerProvider manages the OTel Log SDK lifecycle.
// It creates a LoggerProvider that bridges slog events to OTel log records via OTLP.
// Implementations must be safe for concurrent use after Init returns.
type LoggerProvider interface {
	// Handler returns an slog.Handler that bridges log records to OTel.
	// The handler emits logs with the configured service identity and resource attributes.
	// Returns nil if log export is disabled or Init has not been called.
	Handler() slog.Handler

	// Shutdown gracefully shuts down the LoggerProvider, flushing any buffered logs.
	// Must honour the context deadline.
	Shutdown(ctx context.Context) error
}

// Meter is a subset of the OTel metric.Meter interface, exposing only the
// instrument creation methods VibeWarden needs. This keeps the port layer
// decoupled from the full OTel API.
type Meter interface {
	// Int64Counter creates a Counter instrument for incrementing metrics.
	Int64Counter(name string, options ...InstrumentOption) (Int64Counter, error)

	// Float64Histogram creates a Histogram instrument for recording distributions.
	Float64Histogram(name string, options ...InstrumentOption) (Float64Histogram, error)

	// Int64UpDownCounter creates an UpDownCounter for gauge-like values that can
	// increase or decrease.
	Int64UpDownCounter(name string, options ...InstrumentOption) (Int64UpDownCounter, error)
}

// InstrumentOption configures an OTel instrument (description, unit, explicit buckets, etc.).
// Use WithDescription, WithUnit, and WithExplicitBuckets to construct options.
type InstrumentOption interface {
	isInstrumentOption()
}

// descriptionOption carries the human-readable description for an instrument.
type descriptionOption struct{ v string }

func (descriptionOption) isInstrumentOption() {}

// WithDescription returns an InstrumentOption that sets the instrument description.
func WithDescription(desc string) InstrumentOption { return descriptionOption{v: desc} }

// unitOption carries the unit string for an instrument.
type unitOption struct{ v string }

func (unitOption) isInstrumentOption() {}

// WithUnit returns an InstrumentOption that sets the instrument unit (e.g. "s", "By").
func WithUnit(unit string) InstrumentOption { return unitOption{v: unit} }

// explicitBucketsOption carries histogram bucket boundaries.
type explicitBucketsOption struct{ v []float64 }

func (explicitBucketsOption) isInstrumentOption() {}

// WithExplicitBuckets returns an InstrumentOption that configures explicit histogram buckets.
func WithExplicitBuckets(boundaries []float64) InstrumentOption {
	return explicitBucketsOption{v: boundaries}
}

// DescriptionOf extracts the description from an InstrumentOption slice, or returns "".
// This is a helper for adapter implementations.
func DescriptionOf(opts []InstrumentOption) string {
	for _, o := range opts {
		if d, ok := o.(descriptionOption); ok {
			return d.v
		}
	}
	return ""
}

// UnitOf extracts the unit from an InstrumentOption slice, or returns "".
// This is a helper for adapter implementations.
func UnitOf(opts []InstrumentOption) string {
	for _, o := range opts {
		if u, ok := o.(unitOption); ok {
			return u.v
		}
	}
	return ""
}

// BucketsOf extracts explicit bucket boundaries from an InstrumentOption slice, or returns nil.
// This is a helper for adapter implementations.
func BucketsOf(opts []InstrumentOption) []float64 {
	for _, o := range opts {
		if b, ok := o.(explicitBucketsOption); ok {
			return b.v
		}
	}
	return nil
}

// Int64Counter is an OTel counter instrument for int64 increments.
type Int64Counter interface {
	// Add increments the counter by incr with the given attributes.
	Add(ctx context.Context, incr int64, attrs ...Attribute)
}

// Float64Histogram is an OTel histogram instrument for float64 observations.
type Float64Histogram interface {
	// Record records a single observation with the given attributes.
	Record(ctx context.Context, value float64, attrs ...Attribute)
}

// Int64UpDownCounter is an OTel up-down counter for gauge-like int64 values.
type Int64UpDownCounter interface {
	// Add adds incr to the counter. incr may be negative.
	Add(ctx context.Context, incr int64, attrs ...Attribute)
}

// Attribute is a key-value pair attached to metric observations.
type Attribute struct {
	// Key is the attribute name.
	Key string
	// Value is the attribute value.
	Value string
}
