package middleware

import (
	"log/slog"
	"net/http"

	"github.com/vibewarden/vibewarden/internal/domain/events"
	"github.com/vibewarden/vibewarden/internal/ports"
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
//
// The eventLogger receives structured events following the VibeWarden schema.
// If eventLogger is nil, event logging is skipped silently.
func KratosFlowLoggingMiddleware(logger *slog.Logger, eventLogger ports.EventLogger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if isKratosFlowPath(r.URL.Path) {
				emitKratosFlowEvent(r, eventLogger)
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

// emitKratosFlowEvent emits the proxy.kratos_flow structured event via the
// EventLogger port. If eventLogger is nil the call is a no-op.
func emitKratosFlowEvent(r *http.Request, eventLogger ports.EventLogger) {
	if eventLogger == nil {
		return
	}
	ev := events.NewProxyKratosFlow(events.ProxyKratosFlowParams{
		Method: r.Method,
		Path:   r.URL.Path,
	})
	// Best-effort: ignore logging errors so request processing is never blocked.
	_ = eventLogger.Log(r.Context(), ev)
}
