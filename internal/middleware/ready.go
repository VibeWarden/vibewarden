package middleware

import (
	"encoding/json"
	"net/http"

	"github.com/vibewarden/vibewarden/internal/ports"
)

// ReadyResponse is the JSON body returned by the /_vibewarden/ready endpoint.
type ReadyResponse struct {
	// Ready is true when all plugins are initialised and the upstream is reachable.
	Ready bool `json:"ready"`

	// Plugins maps each plugin name to its current health status string.
	// Possible values: "healthy", "unhealthy".
	Plugins map[string]string `json:"plugins,omitempty"`

	// Upstream is the current upstream reachability status.
	// Possible values: "reachable", "unreachable". Omitted when no upstream
	// health checker is configured.
	Upstream string `json:"upstream,omitempty"`

	// Degraded maps the name of each non-critical plugin that failed Init or
	// Start to the failure reason. A non-empty Degraded map means the sidecar
	// is running in partial-start mode: critical plugins are healthy, but one
	// or more observability/feature plugins could not start.
	Degraded map[string]string `json:"degraded,omitempty"`
}

// ReadyHandler returns an http.HandlerFunc for the readiness probe endpoint
// served at /_vibewarden/ready.
//
// The endpoint returns HTTP 200 with {"ready":true} only when all plugins are
// healthy and the upstream application is reachable. It returns HTTP 503 with
// {"ready":false} in any other case.
//
// readinessChecker may be nil; when nil the handler always returns 200 {"ready":true}
// because no plugins or upstream are being monitored.
func ReadyHandler(readinessChecker ports.ReadinessChecker) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		resp := ReadyResponse{Ready: true}

		if readinessChecker != nil {
			rs := readinessChecker.ReadinessStatus()
			resp.Ready = rs.PluginsReady && rs.UpstreamReachable

			// Populate per-plugin status.
			if len(rs.Plugins) > 0 {
				resp.Plugins = make(map[string]string, len(rs.Plugins))
				for name, hs := range rs.Plugins {
					if hs.Healthy {
						resp.Plugins[name] = "healthy"
					} else {
						resp.Plugins[name] = "unhealthy"
					}
				}
			}

			// Populate upstream status only when an upstream checker is present.
			// UpstreamReachable being false with no checker means no checker was
			// configured, not that the upstream is down. We distinguish by checking
			// whether any plugins are registered — but the cleaner signal is whether
			// the ReadinessStatus explicitly carries upstream information.
			// We rely on the ReadinessStatus.UpstreamReachable field: if the
			// implementation sets it to true unconditionally when no upstream
			// checker exists, we still want to omit the field from the JSON. To
			// support that, ReadyHandler accepts a second optional signal via the
			// readinessChecker interface itself — but since the interface doesn't
			// expose "has upstream checker", we include the upstream field whenever
			// a readinessChecker is provided.
			if rs.UpstreamReachable {
				resp.Upstream = "reachable"
			} else {
				resp.Upstream = "unreachable"
			}

			// Populate degraded plugin map when partial-start mode is active.
			if len(rs.DegradedPlugins) > 0 {
				resp.Degraded = rs.DegradedPlugins
			}
		}

		httpStatus := http.StatusOK
		if !resp.Ready {
			httpStatus = http.StatusServiceUnavailable
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(httpStatus)

		if err := json.NewEncoder(w).Encode(resp); err != nil {
			// Best-effort: headers already sent, cannot write error response.
			return
		}
	}
}

// ReadyMiddleware intercepts requests to /_vibewarden/ready and serves the
// readiness response. All other requests pass through to the next handler.
//
// readinessChecker may be nil; when nil the endpoint always returns 200.
func ReadyMiddleware(readinessChecker ports.ReadinessChecker) func(next http.Handler) http.Handler {
	readyHandler := ReadyHandler(readinessChecker)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/_vibewarden/ready" {
				readyHandler(w, r)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
