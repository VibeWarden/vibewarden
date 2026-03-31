package middleware

import (
	"net/http"
	"strings"

	"github.com/vibewarden/vibewarden/internal/domain/events"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// maintenanceVibewardenPrefix is the path prefix that is always exempt from
// maintenance mode so that health checks and internal endpoints remain reachable.
const maintenanceVibewardenPrefix = "/_vibewarden/"

// MaintenanceConfig holds the configuration for MaintenanceMiddleware.
type MaintenanceConfig struct {
	// Enabled toggles maintenance mode. When false the middleware is a no-op.
	Enabled bool

	// Message is the human-readable message returned to clients in the 503 body.
	// Defaults to "Service is under maintenance" when empty.
	Message string
}

// MaintenanceMiddleware returns HTTP middleware that blocks all requests with
// 503 Service Unavailable when maintenance mode is enabled. Requests to paths
// under /_vibewarden/ (health, ready, metrics) are always passed through so
// that infrastructure health checks continue to work during maintenance.
//
// When enabled a maintenance.request_blocked structured event is emitted for
// every blocked request. If eventLogger is nil, event logging is skipped.
//
// The JSON response body is:
//
//	{"error":"maintenance","status":503,"message":"<configured message>"}
func MaintenanceMiddleware(cfg MaintenanceConfig, eventLogger ports.EventLogger) func(next http.Handler) http.Handler {
	message := cfg.Message
	if message == "" {
		message = "Service is under maintenance"
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !cfg.Enabled {
				next.ServeHTTP(w, r)
				return
			}

			// Always let /_vibewarden/* through so health/ready/metrics remain
			// reachable by load balancers and operators during maintenance.
			if strings.HasPrefix(r.URL.Path, maintenanceVibewardenPrefix) {
				next.ServeHTTP(w, r)
				return
			}

			emitMaintenanceBlocked(r, eventLogger, message)
			WriteErrorResponse(w, r, http.StatusServiceUnavailable, "maintenance", message)
		})
	}
}

// emitMaintenanceBlocked emits a maintenance.request_blocked structured event.
// If eventLogger is nil the call is a no-op. Errors are discarded so that
// logging failures never affect request handling.
func emitMaintenanceBlocked(r *http.Request, eventLogger ports.EventLogger, message string) {
	if eventLogger == nil {
		return
	}
	ev := events.NewMaintenanceRequestBlocked(events.MaintenanceRequestBlockedParams{
		Path:    r.URL.Path,
		Method:  r.Method,
		Message: message,
	})
	_ = eventLogger.Log(r.Context(), ev)
}
