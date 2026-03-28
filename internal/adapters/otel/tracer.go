// Package otel provides the OpenTelemetry SDK adapter for VibeWarden.
package otel

import (
	"context"

	"github.com/vibewarden/vibewarden/internal/ports"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

// tracerAdapter wraps an OTel trace.Tracer to implement ports.Tracer.
type tracerAdapter struct {
	t trace.Tracer
}

// Start creates a span and a context containing the newly-created span.
// SpanStartOptions are converted from ports types to OTel SDK types.
func (a *tracerAdapter) Start(ctx context.Context, spanName string, opts ...ports.SpanStartOption) (context.Context, ports.Span) {
	traceOpts := convertSpanStartOptions(opts)
	ctx, span := a.t.Start(ctx, spanName, traceOpts...)
	return ctx, &spanAdapter{s: span}
}

// convertSpanStartOptions converts ports.SpanStartOption slice to OTel trace.SpanStartOption slice.
func convertSpanStartOptions(opts []ports.SpanStartOption) []trace.SpanStartOption {
	if len(opts) == 0 {
		return nil
	}
	// Extract span kind if provided; KindOf defaults to SpanKindInternal when absent.
	kind := ports.KindOf(opts)
	return []trace.SpanStartOption{trace.WithSpanKind(convertSpanKind(kind))}
}

// spanAdapter wraps an OTel trace.Span to implement ports.Span.
type spanAdapter struct {
	s trace.Span
}

// End marks the span as complete. Must be called exactly once.
func (a *spanAdapter) End() {
	a.s.End()
}

// SetStatus sets the span status code and description.
func (a *spanAdapter) SetStatus(code ports.SpanStatusCode, description string) {
	a.s.SetStatus(convertStatusCode(code), description)
}

// SetAttributes sets key-value attributes on the span.
func (a *spanAdapter) SetAttributes(attrs ...ports.Attribute) {
	otelAttrs := make([]attribute.KeyValue, len(attrs))
	for i, attr := range attrs {
		otelAttrs[i] = attribute.String(attr.Key, attr.Value)
	}
	a.s.SetAttributes(otelAttrs...)
}

// RecordError records an error as a span event.
func (a *spanAdapter) RecordError(err error) {
	a.s.RecordError(err)
}

// convertSpanKind converts a ports.SpanKind to an OTel trace.SpanKind.
func convertSpanKind(kind ports.SpanKind) trace.SpanKind {
	switch kind {
	case ports.SpanKindServer:
		return trace.SpanKindServer
	case ports.SpanKindClient:
		return trace.SpanKindClient
	default:
		return trace.SpanKindInternal
	}
}

// convertStatusCode converts a ports.SpanStatusCode to an OTel codes.Code.
func convertStatusCode(code ports.SpanStatusCode) codes.Code {
	switch code {
	case ports.SpanStatusOK:
		return codes.Ok
	case ports.SpanStatusError:
		return codes.Error
	default:
		return codes.Unset
	}
}

// propagatorAdapter wraps propagation.TraceContext to implement ports.TextMapPropagator.
// It bridges the OTel SDK propagation API to VibeWarden's port layer.
type propagatorAdapter struct {
	p propagation.TraceContext
}

// Extract reads the W3C traceparent (and tracestate) headers from the carrier
// into the returned context, enabling the span started from that context to
// become a child of the upstream trace.
func (a *propagatorAdapter) Extract(ctx context.Context, carrier ports.TextMapCarrier) context.Context {
	return a.p.Extract(ctx, headerCarrierBridge{c: carrier})
}

// Inject writes the current span context from ctx into the carrier as W3C
// traceparent and tracestate headers, enabling downstream services to
// continue the trace.
func (a *propagatorAdapter) Inject(ctx context.Context, carrier ports.TextMapCarrier) {
	a.p.Inject(ctx, headerCarrierBridge{c: carrier})
}

// headerCarrierBridge adapts ports.TextMapCarrier to propagation.TextMapCarrier.
type headerCarrierBridge struct {
	c ports.TextMapCarrier
}

func (b headerCarrierBridge) Get(key string) string { return b.c.Get(key) }
func (b headerCarrierBridge) Set(key, value string) { b.c.Set(key, value) }
func (b headerCarrierBridge) Keys() []string        { return b.c.Keys() }
