package middleware

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/vibewarden/vibewarden/internal/ports"
)

// tracingResponseWriter wraps http.ResponseWriter to capture the HTTP status code
// written by the downstream handler.
type tracingResponseWriter struct {
	http.ResponseWriter
	statusCode int
	written    bool
}

func (rw *tracingResponseWriter) WriteHeader(code int) {
	if !rw.written {
		rw.statusCode = code
		rw.written = true
	}
	rw.ResponseWriter.WriteHeader(code)
}

func (rw *tracingResponseWriter) Write(b []byte) (int, error) {
	if !rw.written {
		rw.statusCode = http.StatusOK
		rw.written = true
	}
	return rw.ResponseWriter.Write(b)
}

// httpHeaderCarrier adapts http.Header to implement ports.TextMapCarrier.
type httpHeaderCarrier http.Header

func (c httpHeaderCarrier) Get(key string) string { return http.Header(c).Get(key) }
func (c httpHeaderCarrier) Set(key, value string) { http.Header(c).Set(key, value) }
func (c httpHeaderCarrier) Keys() []string {
	keys := make([]string, 0, len(c))
	for k := range c {
		keys = append(keys, k)
	}
	return keys
}

// TracingMiddleware returns HTTP middleware that creates an OTel span for each request.
// It must be the outermost middleware (first in, last out) to capture the full
// request lifecycle including auth, rate limiting, and proxy latency.
//
// When a non-nil propagator is provided, it extracts the W3C traceparent header
// from the incoming request, making the new span a child of any upstream trace.
// Pass nil for propagator to disable context extraction (no-op, root span only).
//
// The middleware sets standard HTTP span attributes:
//   - http.request.method
//   - url.path
//   - http.response.status_code
//   - http.route (normalized path pattern)
//
// The span context is stored in the request context for downstream use
// (log correlation, error responses).
//
// Requests to /_vibewarden/* paths are NOT traced to avoid self-referential noise.
func TracingMiddleware(
	tracer ports.Tracer,
	normalizePathFn func(string) string,
	propagator ports.TextMapPropagator,
) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Skip tracing for internal endpoints.
			if strings.HasPrefix(r.URL.Path, "/_vibewarden/") {
				next.ServeHTTP(w, r)
				return
			}

			// Extract incoming trace context (W3C traceparent) when a propagator
			// is configured. This makes the new span a child of any upstream trace.
			ctx := r.Context()
			if propagator != nil {
				ctx = propagator.Extract(ctx, httpHeaderCarrier(r.Header))
			}

			// Create span with server kind.
			ctx, span := tracer.Start(ctx, "HTTP "+r.Method,
				ports.WithSpanKind(ports.SpanKindServer))
			defer span.End()

			// Set initial attributes.
			route := normalizePathFn(r.URL.Path)
			span.SetAttributes(
				ports.Attribute{Key: "http.request.method", Value: r.Method},
				ports.Attribute{Key: "url.path", Value: r.URL.Path},
				ports.Attribute{Key: "http.route", Value: route},
			)

			// Wrap response writer to capture status code.
			rw := &tracingResponseWriter{ResponseWriter: w, statusCode: http.StatusOK}

			// Serve with span context.
			next.ServeHTTP(rw, r.WithContext(ctx))

			// Set final attributes.
			span.SetAttributes(
				ports.Attribute{Key: "http.response.status_code", Value: strconv.Itoa(rw.statusCode)},
			)

			// Set span status based on HTTP status.
			if rw.statusCode >= 500 {
				span.SetStatus(ports.SpanStatusError, http.StatusText(rw.statusCode))
			} else {
				span.SetStatus(ports.SpanStatusOK, "")
			}
		})
	}
}
