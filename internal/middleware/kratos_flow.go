package middleware

import (
	"log/slog"
	"net/http"
	"time"
)

// kratosFlowPathPrefixes lists the URL path prefixes that are considered
// Kratos self-service flow paths. Requests matching any prefix are logged
// with the proxy.kratos_flow event before being forwarded.
var kratosFlowPathPrefixes = []string{
	"/self-service/",
	"/.ory/kratos/public/",
}

// KratosFlowLoggingMiddleware returns HTTP middleware that emits a structured
// proxy.kratos_flow log event for every request whose path falls under a known
// Kratos self-service flow prefix.
//
// This middleware is intended to wrap the Kratos reverse-proxy handler so that
// AI-readable logs capture when the sidecar is routing a browser flow to Kratos
// rather than to the upstream application.
func KratosFlowLoggingMiddleware(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if isKratosFlowPath(r.URL.Path) {
				logKratosFlowEvent(logger, r)
			}
			next.ServeHTTP(w, r)
		})
	}
}

// isKratosFlowPath reports whether the given request path targets a Kratos
// self-service flow or the Ory canonical API prefix.
func isKratosFlowPath(requestPath string) bool {
	for _, prefix := range kratosFlowPathPrefixes {
		if len(requestPath) >= len(prefix) && requestPath[:len(prefix)] == prefix {
			return true
		}
	}
	return false
}

// logKratosFlowEvent emits the proxy.kratos_flow structured log event.
func logKratosFlowEvent(logger *slog.Logger, r *http.Request) {
	logger.InfoContext(r.Context(), "proxy.kratos_flow",
		slog.String("schema_version", "v1"),
		slog.String("event_type", "proxy.kratos_flow"),
		slog.String("ai_summary", "request proxied to Kratos self-service API"),
		slog.Group("payload",
			slog.String("method", r.Method),
			slog.String("path", r.URL.Path),
			slog.String("timestamp", time.Now().UTC().Format(time.RFC3339)),
		),
	)
}
