// Package middleware provides HTTP middleware for VibeWarden.
package middleware

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/vibewarden/vibewarden/internal/ports"
)

// depCacheTTL is how long dependency health check results are cached.
// This prevents hammering dependencies on every health endpoint request.
const depCacheTTL = 5 * time.Second

// depProbeTimeout is the per-dependency probe timeout.
const depProbeTimeout = 3 * time.Second

// HealthResponse is the JSON response from the health endpoint.
type HealthResponse struct {
	// Status is the overall health status: "ok", "degraded", or "unhealthy".
	Status string `json:"status"`

	// Version is the running VibeWarden binary version.
	Version string `json:"version"`

	// Dependencies maps dependency name to its current health status.
	// Only present when the health handler was constructed with checkers.
	Dependencies map[string]ports.DependencyStatus `json:"dependencies,omitempty"`
}

// cachedEntry holds a single cached dependency status with a fetch timestamp.
type cachedEntry struct {
	status    ports.DependencyStatus
	fetchedAt time.Time
}

// depStatusCache caches dependency health check results with a fixed TTL.
// It is safe for concurrent use.
type depStatusCache struct {
	mu      sync.Mutex
	entries map[string]cachedEntry
	ttl     time.Duration
}

func newDepStatusCache(ttl time.Duration) *depStatusCache {
	return &depStatusCache{
		entries: make(map[string]cachedEntry),
		ttl:     ttl,
	}
}

// get returns the cached DependencyStatus for name when it is still within
// the TTL, and false otherwise.
func (c *depStatusCache) get(name string) (ports.DependencyStatus, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[name]
	if !ok || time.Since(e.fetchedAt) > c.ttl {
		return ports.DependencyStatus{}, false
	}
	return e.status, true
}

// set stores a fresh DependencyStatus for name.
func (c *depStatusCache) set(name string, status ports.DependencyStatus) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[name] = cachedEntry{status: status, fetchedAt: time.Now()}
}

// HealthHandler returns an http.HandlerFunc for the health check endpoint.
// The health endpoint is served at /_vibewarden/health.
//
// When checkers are provided each dependency is probed on demand (with a
// 5-second cache to avoid hammering dependencies on every request) and the
// results are included in the JSON response.
//
// The overall "status" field is derived from dependency results:
//   - "ok"       — all dependencies healthy (or no checkers configured)
//   - "degraded" — one or more dependencies unhealthy; sidecar is still serving
func HealthHandler(version string, checkers ...ports.DependencyChecker) http.HandlerFunc {
	cache := newDepStatusCache(depCacheTTL)

	return func(w http.ResponseWriter, r *http.Request) {
		resp := HealthResponse{
			Status:  string(ports.HealthSummaryOK),
			Version: version,
		}

		if len(checkers) > 0 {
			deps := make(map[string]ports.DependencyStatus, len(checkers))
			allHealthy := true

			for _, checker := range checkers {
				name := checker.DependencyName()

				status, hit := cache.get(name)
				if !hit {
					// Probe with a bounded timeout so a slow dependency cannot
					// hold up the health endpoint indefinitely.
					probeCtx, cancel := context.WithTimeout(r.Context(), depProbeTimeout)
					status = checker.CheckDependency(probeCtx)
					cancel()
					cache.set(name, status)
				}

				deps[name] = status
				if status.Status != "healthy" {
					allHealthy = false
				}
			}

			resp.Dependencies = deps
			if !allHealthy {
				resp.Status = string(ports.HealthSummaryDegraded)
			}
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
// When dependency checkers are provided they are forwarded to HealthHandler
// for live dependency status reporting.
func HealthMiddleware(version string, checkers ...ports.DependencyChecker) func(next http.Handler) http.Handler {
	healthHandler := HealthHandler(version, checkers...)

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
