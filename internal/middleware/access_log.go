package middleware

import (
	"log/slog"
	"net/http"
	"time"
)

// accessLogResponseWriter wraps http.ResponseWriter to capture the HTTP status
// code and the number of bytes written by the downstream handler.
type accessLogResponseWriter struct {
	http.ResponseWriter
	statusCode   int
	bytesWritten int
	written      bool
}

func (rw *accessLogResponseWriter) WriteHeader(code int) {
	if !rw.written {
		rw.statusCode = code
		rw.written = true
	}
	rw.ResponseWriter.WriteHeader(code)
}

func (rw *accessLogResponseWriter) Write(b []byte) (int, error) {
	if !rw.written {
		rw.statusCode = http.StatusOK
		rw.written = true
	}
	n, err := rw.ResponseWriter.Write(b)
	rw.bytesWritten += n
	return n, err
}

// AccessLogMiddleware returns HTTP middleware that logs a structured INFO record
// for every completed request after the response has been written.
//
// Each log record contains:
//   - method       — HTTP method (GET, POST, …)
//   - path         — raw request URI path
//   - status       — HTTP response status code
//   - duration_ms  — request duration in milliseconds (float64)
//   - client_ip    — client IP address (X-Forwarded-For when trustProxy is true)
//   - request_id   — correlation ID from the request context
//   - user_agent   — value of the User-Agent header
//   - bytes        — number of bytes written to the response body
//
// When enabled is false the middleware is a transparent pass-through and the
// logger argument may be nil.
//
// The logger argument must not be nil when enabled is true.
func AccessLogMiddleware(logger *slog.Logger, enabled bool, trustProxy bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !enabled {
				next.ServeHTTP(w, r)
				return
			}

			start := time.Now()
			rw := &accessLogResponseWriter{
				ResponseWriter: w,
				statusCode:     http.StatusOK,
			}

			next.ServeHTTP(rw, r)

			duration := time.Since(start)

			logger.InfoContext(r.Context(), "access",
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.Int("status", rw.statusCode),
				slog.Float64("duration_ms", float64(duration.Nanoseconds())/1e6),
				slog.String("client_ip", ExtractClientIP(r, trustProxy)),
				slog.String("request_id", CorrelationID(r.Context())),
				slog.String("user_agent", r.UserAgent()),
				slog.Int("bytes", rw.bytesWritten),
			)
		})
	}
}
