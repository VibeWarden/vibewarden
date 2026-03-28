// Package ports defines the interfaces (ports) for VibeWarden's hexagonal architecture.
package ports

import (
	"context"
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

	// PrometheusEnabled returns true if the Prometheus exporter is active.
	PrometheusEnabled() bool

	// OTLPEnabled returns true if the OTLP exporter is active.
	OTLPEnabled() bool
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
