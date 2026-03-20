// Package middleware provides HTTP middleware for VibeWarden.
package middleware

import (
	"encoding/json"
	"net/http"
)

// HealthResponse is the JSON response from the health endpoint.
type HealthResponse struct {
	Status  string `json:"status"`
	Version string `json:"version"`
}

// HealthHandler returns an http.HandlerFunc for the health check endpoint.
// The health endpoint is served at /_vibewarden/health.
func HealthHandler(version string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		resp := HealthResponse{
			Status:  "ok",
			Version: version,
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)

		if err := json.NewEncoder(w).Encode(resp); err != nil {
			// Best-effort: headers already sent, cannot write error response.
			return
		}
	}
}

// HealthMiddleware intercepts requests to /_vibewarden/health and serves
// the health response. All other requests pass through to the next handler.
func HealthMiddleware(version string) func(next http.Handler) http.Handler {
	healthHandler := HealthHandler(version)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/_vibewarden/health" {
				healthHandler(w, r)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
