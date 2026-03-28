package otel

import (
	"context"

	"github.com/vibewarden/vibewarden/internal/ports"
	"go.opentelemetry.io/otel/attribute"
	otelmetric "go.opentelemetry.io/otel/metric"
)

// meterAdapter wraps an OTel metric.Meter to implement ports.Meter.
type meterAdapter struct {
	m otelmetric.Meter
}

// Int64Counter creates an OTel Int64Counter and wraps it as ports.Int64Counter.
func (a *meterAdapter) Int64Counter(name string, opts ...ports.InstrumentOption) (ports.Int64Counter, error) {
	c, err := a.m.Int64Counter(name, toCounterOptions(opts)...)
	if err != nil {
		return nil, err
	}
	return &int64CounterAdapter{c: c}, nil
}

// Float64Histogram creates an OTel Float64Histogram and wraps it as ports.Float64Histogram.
func (a *meterAdapter) Float64Histogram(name string, opts ...ports.InstrumentOption) (ports.Float64Histogram, error) {
	h, err := a.m.Float64Histogram(name, toHistogramOptions(opts)...)
	if err != nil {
		return nil, err
	}
	return &float64HistogramAdapter{h: h}, nil
}

// Int64UpDownCounter creates an OTel Int64UpDownCounter and wraps it as ports.Int64UpDownCounter.
func (a *meterAdapter) Int64UpDownCounter(name string, opts ...ports.InstrumentOption) (ports.Int64UpDownCounter, error) {
	c, err := a.m.Int64UpDownCounter(name, toUpDownCounterOptions(opts)...)
	if err != nil {
		return nil, err
	}
	return &int64UpDownCounterAdapter{c: c}, nil
}

// toCounterOptions translates ports.InstrumentOption to otelmetric.Int64CounterOption.
func toCounterOptions(opts []ports.InstrumentOption) []otelmetric.Int64CounterOption {
	out := make([]otelmetric.Int64CounterOption, 0, len(opts))
	if desc := ports.DescriptionOf(opts); desc != "" {
		out = append(out, otelmetric.WithDescription(desc))
	}
	if unit := ports.UnitOf(opts); unit != "" {
		out = append(out, otelmetric.WithUnit(unit))
	}
	return out
}

// toHistogramOptions translates ports.InstrumentOption to otelmetric.Float64HistogramOption.
func toHistogramOptions(opts []ports.InstrumentOption) []otelmetric.Float64HistogramOption {
	out := make([]otelmetric.Float64HistogramOption, 0, len(opts)+1)
	if desc := ports.DescriptionOf(opts); desc != "" {
		out = append(out, otelmetric.WithDescription(desc))
	}
	if unit := ports.UnitOf(opts); unit != "" {
		out = append(out, otelmetric.WithUnit(unit))
	}
	if buckets := ports.BucketsOf(opts); len(buckets) > 0 {
		out = append(out, otelmetric.WithExplicitBucketBoundaries(buckets...))
	}
	return out
}

// toUpDownCounterOptions translates ports.InstrumentOption to otelmetric.Int64UpDownCounterOption.
func toUpDownCounterOptions(opts []ports.InstrumentOption) []otelmetric.Int64UpDownCounterOption {
	out := make([]otelmetric.Int64UpDownCounterOption, 0, len(opts))
	if desc := ports.DescriptionOf(opts); desc != "" {
		out = append(out, otelmetric.WithDescription(desc))
	}
	if unit := ports.UnitOf(opts); unit != "" {
		out = append(out, otelmetric.WithUnit(unit))
	}
	return out
}

// toOTelAttrs converts ports.Attribute slice to OTel attribute.KeyValue slice.
func toOTelAttrs(attrs []ports.Attribute) []attribute.KeyValue {
	kvs := make([]attribute.KeyValue, len(attrs))
	for i, a := range attrs {
		kvs[i] = attribute.String(a.Key, a.Value)
	}
	return kvs
}

// int64CounterAdapter wraps an OTel metric.Int64Counter to implement ports.Int64Counter.
type int64CounterAdapter struct {
	c otelmetric.Int64Counter
}

// Add increments the counter by incr with the given attributes.
func (a *int64CounterAdapter) Add(ctx context.Context, incr int64, attrs ...ports.Attribute) {
	a.c.Add(ctx, incr, otelmetric.WithAttributes(toOTelAttrs(attrs)...))
}

// float64HistogramAdapter wraps an OTel metric.Float64Histogram to implement ports.Float64Histogram.
type float64HistogramAdapter struct {
	h otelmetric.Float64Histogram
}

// Record records a single observation with the given attributes.
func (a *float64HistogramAdapter) Record(ctx context.Context, value float64, attrs ...ports.Attribute) {
	a.h.Record(ctx, value, otelmetric.WithAttributes(toOTelAttrs(attrs)...))
}

// int64UpDownCounterAdapter wraps an OTel metric.Int64UpDownCounter to implement ports.Int64UpDownCounter.
type int64UpDownCounterAdapter struct {
	c otelmetric.Int64UpDownCounter
}

// Add adds incr to the counter. incr may be negative.
func (a *int64UpDownCounterAdapter) Add(ctx context.Context, incr int64, attrs ...ports.Attribute) {
	a.c.Add(ctx, incr, otelmetric.WithAttributes(toOTelAttrs(attrs)...))
}
