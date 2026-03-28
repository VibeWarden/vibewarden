package middleware

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/vibewarden/vibewarden/internal/ports"
)

// fakeDependencyChecker implements ports.DependencyChecker for testing.
type fakeDependencyChecker struct {
	name   string
	status ports.DependencyStatus
}

func (f *fakeDependencyChecker) DependencyName() string { return f.name }

func (f *fakeDependencyChecker) CheckDependency(_ context.Context) ports.DependencyStatus {
	return f.status
}

// countingChecker counts how many times CheckDependency is called.
type countingChecker struct {
	name   string
	status ports.DependencyStatus
	calls  atomic.Int64
}

func (c *countingChecker) DependencyName() string { return c.name }

func (c *countingChecker) CheckDependency(_ context.Context) ports.DependencyStatus {
	c.calls.Add(1)
	return c.status
}

func TestHealthHandler_NoDependencies_ReturnsOK(t *testing.T) {
	handler := HealthHandler("v1.0.0", nil)

	req := httptest.NewRequest(http.MethodGet, "/_vibewarden/health", nil)
	w := httptest.NewRecorder()
	handler(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}

	var resp HealthResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if resp.Status != "ok" {
		t.Errorf("status = %q, want %q", resp.Status, "ok")
	}
	if len(resp.Dependencies) != 0 {
		t.Errorf("dependencies should be absent when no checkers provided, got %v", resp.Dependencies)
	}
}

func TestHealthHandler_AllDepsHealthy_ReturnsOK(t *testing.T) {
	checker := &fakeDependencyChecker{
		name:   "kratos",
		status: ports.DependencyStatus{Status: "healthy", LatencyMS: 5},
	}
	handler := HealthHandler("dev", nil, checker)

	req := httptest.NewRequest(http.MethodGet, "/_vibewarden/health", nil)
	w := httptest.NewRecorder()
	handler(w, req)

	var resp HealthResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if resp.Status != "ok" {
		t.Errorf("status = %q, want %q", resp.Status, "ok")
	}
	dep, ok := resp.Dependencies["kratos"]
	if !ok {
		t.Fatal("dependencies.kratos missing")
	}
	if dep.Status != "healthy" {
		t.Errorf("kratos status = %q, want %q", dep.Status, "healthy")
	}
	if dep.LatencyMS != 5 {
		t.Errorf("kratos latency_ms = %d, want 5", dep.LatencyMS)
	}
}

func TestHealthHandler_UnhealthyDep_ReturnsDegraded(t *testing.T) {
	checker := &fakeDependencyChecker{
		name: "postgres",
		status: ports.DependencyStatus{
			Status: "unhealthy",
			Error:  "connection refused",
		},
	}
	handler := HealthHandler("dev", nil, checker)

	req := httptest.NewRequest(http.MethodGet, "/_vibewarden/health", nil)
	w := httptest.NewRecorder()
	handler(w, req)

	var resp HealthResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if resp.Status != "degraded" {
		t.Errorf("status = %q, want %q", resp.Status, "degraded")
	}
	dep, ok := resp.Dependencies["postgres"]
	if !ok {
		t.Fatal("dependencies.postgres missing")
	}
	if dep.Status != "unhealthy" {
		t.Errorf("postgres status = %q, want %q", dep.Status, "unhealthy")
	}
	if dep.Error != "connection refused" {
		t.Errorf("postgres error = %q, want %q", dep.Error, "connection refused")
	}
}

func TestHealthHandler_MixedDeps_ReturnsDegraded(t *testing.T) {
	tests := []struct {
		name       string
		checkers   []ports.DependencyChecker
		wantStatus string
	}{
		{
			name: "all healthy",
			checkers: []ports.DependencyChecker{
				&fakeDependencyChecker{"kratos", ports.DependencyStatus{Status: "healthy"}},
				&fakeDependencyChecker{"postgres", ports.DependencyStatus{Status: "healthy"}},
			},
			wantStatus: "ok",
		},
		{
			name: "one unhealthy",
			checkers: []ports.DependencyChecker{
				&fakeDependencyChecker{"kratos", ports.DependencyStatus{Status: "healthy"}},
				&fakeDependencyChecker{"postgres", ports.DependencyStatus{Status: "unhealthy", Error: "timeout"}},
			},
			wantStatus: "degraded",
		},
		{
			name: "all unhealthy",
			checkers: []ports.DependencyChecker{
				&fakeDependencyChecker{"kratos", ports.DependencyStatus{Status: "unhealthy"}},
				&fakeDependencyChecker{"postgres", ports.DependencyStatus{Status: "unhealthy"}},
			},
			wantStatus: "degraded",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := HealthHandler("dev", nil, tt.checkers...)

			req := httptest.NewRequest(http.MethodGet, "/_vibewarden/health", nil)
			w := httptest.NewRecorder()
			handler(w, req)

			var resp HealthResponse
			if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
				t.Fatalf("decoding response: %v", err)
			}
			if resp.Status != tt.wantStatus {
				t.Errorf("status = %q, want %q", resp.Status, tt.wantStatus)
			}
		})
	}
}

func TestHealthHandler_CachesResults(t *testing.T) {
	checker := &countingChecker{
		name:   "kratos",
		status: ports.DependencyStatus{Status: "healthy"},
	}

	// Use a very short TTL is hard to control in a test, so we rely on the
	// real cache TTL (5s) and verify that two back-to-back requests only call
	// CheckDependency once.
	handler := HealthHandler("dev", nil, checker)

	for i := 0; i < 5; i++ {
		req := httptest.NewRequest(http.MethodGet, "/_vibewarden/health", nil)
		w := httptest.NewRecorder()
		handler(w, req)
	}

	if checker.calls.Load() != 1 {
		t.Errorf("CheckDependency called %d times, want 1 (cache should deduplicate)", checker.calls.Load())
	}
}

func TestHealthHandler_CacheExpires(t *testing.T) {
	// Create a handler with a custom (very short) cache by directly constructing
	// the cache and verifying it expires. We test the public API by verifying
	// the depStatusCache behavior separately.
	c := newDepStatusCache(10 * time.Millisecond)

	// Store a value.
	c.set("test", ports.DependencyStatus{Status: "healthy"})

	// Immediately retrievable.
	if _, ok := c.get("test"); !ok {
		t.Fatal("expected cache hit immediately after set")
	}

	// Wait for expiry.
	time.Sleep(20 * time.Millisecond)

	if _, ok := c.get("test"); ok {
		t.Error("expected cache miss after TTL expired")
	}
}

func TestHealthHandler_VersionInResponse(t *testing.T) {
	const wantVersion = "v2.3.4"
	handler := HealthHandler(wantVersion, nil)

	req := httptest.NewRequest(http.MethodGet, "/_vibewarden/health", nil)
	w := httptest.NewRecorder()
	handler(w, req)

	var resp HealthResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if resp.Version != wantVersion {
		t.Errorf("version = %q, want %q", resp.Version, wantVersion)
	}
}

func TestHealthMiddleware_WithDependencyCheckers(t *testing.T) {
	checker := &fakeDependencyChecker{
		name:   "kratos",
		status: ports.DependencyStatus{Status: "unhealthy", Error: "connection refused"},
	}
	mw := HealthMiddleware("v1.0.0", nil, checker)

	nextCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusTeapot)
	})

	handler := mw(next)

	req := httptest.NewRequest(http.MethodGet, "/_vibewarden/health", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if nextCalled {
		t.Error("next handler was called for health path — should not be")
	}
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}

	var resp HealthResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if resp.Status != "degraded" {
		t.Errorf("status = %q, want %q", resp.Status, "degraded")
	}
	if _, ok := resp.Dependencies["kratos"]; !ok {
		t.Error("dependencies.kratos missing from health response")
	}
}

func TestHealthHandler_DependencyErrorFieldOmittedWhenHealthy(t *testing.T) {
	checker := &fakeDependencyChecker{
		name:   "kratos",
		status: ports.DependencyStatus{Status: "healthy", LatencyMS: 3},
	}
	handler := HealthHandler("dev", nil, checker)

	req := httptest.NewRequest(http.MethodGet, "/_vibewarden/health", nil)
	w := httptest.NewRecorder()
	handler(w, req)

	// Parse raw JSON to check omitempty behavior.
	var raw map[string]any
	if err := json.NewDecoder(w.Body).Decode(&raw); err != nil {
		t.Fatalf("decoding raw response: %v", err)
	}
	deps, ok := raw["dependencies"].(map[string]any)
	if !ok {
		t.Fatal("dependencies missing")
	}
	kratos, ok := deps["kratos"].(map[string]any)
	if !ok {
		t.Fatal("kratos dep missing")
	}
	if _, hasError := kratos["error"]; hasError {
		t.Error("error field should be omitted for healthy dependency (omitempty)")
	}
}
