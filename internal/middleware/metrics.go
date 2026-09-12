package middleware

import (
	"net/http"
	"strconv"
	"time"

	"github.com/vibewarden/vibewarden/internal/ports"
)

// metricsResponseWriter wraps http.ResponseWriter to capture the HTTP status code
// written by the downstream handler.
type metricsResponseWriter struct {
	http.ResponseWriter
	statusCode int
	written    bool
}

func (rw *metricsResponseWriter) WriteHeader(code int) {
	if !rw.written {
		rw.statusCode = code
		rw.written = true
	}
	rw.ResponseWriter.WriteHeader(code)
}

func (rw *metricsResponseWriter) Write(b []byte) (int, error) {
	if !rw.written {
		rw.statusCode = http.StatusOK
		rw.written = true
	}
	return rw.ResponseWriter.Write(b)
}

// Flush forwards a flush to the underlying writer so streaming responses are
// not buffered by this wrapper. The first flush commits the response, so the
// captured status code is pinned at that point.
func (rw *metricsResponseWriter) Flush() {
	if !rw.written {
		rw.statusCode = http.StatusOK
		rw.written = true
	}
	FlushResponseWriter(rw.ResponseWriter)
}

// Unwrap returns the wrapped ResponseWriter so http.ResponseController can
// reach interfaces implemented further down the chain.
func (rw *metricsResponseWriter) Unwrap() http.ResponseWriter { return rw.ResponseWriter }

// MetricsMiddleware returns HTTP middleware that records request metrics.
// It must be placed early in the middleware chain (after security headers,
// before auth/rate-limit) to capture the full request lifecycle.
//
// The middleware records:
//   - vibewarden_requests_total (counter)
//   - vibewarden_request_duration_seconds (histogram)
//
// Requests to /_vibewarden/metrics are excluded to avoid self-referential noise.
//
// The normalizePathFn converts raw request paths to stable pattern strings
// for label cardinality control (e.g., "/users/123" -> "/users/:id").
func MetricsMiddleware(
	mc ports.MetricsCollector,
	normalizePathFn func(string) string,
) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Skip metrics for the metrics endpoint itself.
			if r.URL.Path == "/_vibewarden/metrics" {
				next.ServeHTTP(w, r)
				return
			}

			start := time.Now()
			rw := &metricsResponseWriter{ResponseWriter: w, statusCode: http.StatusOK}

			next.ServeHTTP(rw, r)

			duration := time.Since(start)
			pathPattern := normalizePathFn(r.URL.Path)
			statusCode := strconv.Itoa(rw.statusCode)

			mc.IncRequestTotal(r.Method, statusCode, pathPattern)
			mc.ObserveRequestDuration(r.Method, pathPattern, duration)
		})
	}
}
