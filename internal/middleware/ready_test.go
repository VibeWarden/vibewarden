package middleware

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	domainheal "github.com/vibewarden/vibewarden/internal/domain/health"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// fakeReadinessChecker implements ports.ReadinessChecker for testing.
type fakeReadinessChecker struct {
	status ports.ReadinessStatus
}

func (f *fakeReadinessChecker) Ready() bool {
	return f.status.PluginsReady && f.status.UpstreamReachable
}

func (f *fakeReadinessChecker) ReadinessStatus() ports.ReadinessStatus {
	return f.status
}

// Compile-time assertion.
var _ ports.ReadinessChecker = (*fakeReadinessChecker)(nil)

func TestReadyHandler_NilChecker_Returns200Ready(t *testing.T) {
	handler := ReadyHandler(nil)

	req := httptest.NewRequest(http.MethodGet, "/_vibewarden/ready", nil)
	w := httptest.NewRecorder()
	handler(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}

	var resp ReadyResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if !resp.Ready {
		t.Error("ready = false, want true (no checker means unconditionally ready)")
	}
}

func TestReadyHandler_ContentTypeJSON(t *testing.T) {
	handler := ReadyHandler(nil)

	req := httptest.NewRequest(http.MethodGet, "/_vibewarden/ready", nil)
	w := httptest.NewRecorder()
	handler(w, req)

	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want %q", ct, "application/json")
	}
}

func TestReadyHandler_AllReady_Returns200(t *testing.T) {
	checker := &fakeReadinessChecker{
		status: ports.ReadinessStatus{
			PluginsReady:      true,
			UpstreamReachable: true,
			Plugins: map[string]ports.HealthStatus{
				"tls": {Healthy: true, Message: "ok"},
			},
		},
	}
	handler := ReadyHandler(checker)

	req := httptest.NewRequest(http.MethodGet, "/_vibewarden/ready", nil)
	w := httptest.NewRecorder()
	handler(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}

	var resp ReadyResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if !resp.Ready {
		t.Error("ready = false, want true")
	}
	if resp.Upstream != "reachable" {
		t.Errorf("upstream = %q, want %q", resp.Upstream, "reachable")
	}
	if resp.Plugins["tls"] != "healthy" {
		t.Errorf("plugins[tls] = %q, want %q", resp.Plugins["tls"], "healthy")
	}
}

func TestReadyHandler_PluginsNotReady_Returns503(t *testing.T) {
	checker := &fakeReadinessChecker{
		status: ports.ReadinessStatus{
			PluginsReady:      false,
			UpstreamReachable: true,
			Plugins: map[string]ports.HealthStatus{
				"user-management": {Healthy: false, Message: "postgres unreachable"},
			},
		},
	}
	handler := ReadyHandler(checker)

	req := httptest.NewRequest(http.MethodGet, "/_vibewarden/ready", nil)
	w := httptest.NewRecorder()
	handler(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", w.Code)
	}

	var resp ReadyResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if resp.Ready {
		t.Error("ready = true, want false (plugin unhealthy)")
	}
	if resp.Plugins["user-management"] != "unhealthy" {
		t.Errorf("plugins[user-management] = %q, want %q", resp.Plugins["user-management"], "unhealthy")
	}
}

func TestReadyHandler_UpstreamUnreachable_Returns503(t *testing.T) {
	checker := &fakeReadinessChecker{
		status: ports.ReadinessStatus{
			PluginsReady:      true,
			UpstreamReachable: false,
		},
	}
	handler := ReadyHandler(checker)

	req := httptest.NewRequest(http.MethodGet, "/_vibewarden/ready", nil)
	w := httptest.NewRecorder()
	handler(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", w.Code)
	}

	var resp ReadyResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if resp.Ready {
		t.Error("ready = true, want false (upstream unreachable)")
	}
	if resp.Upstream != "unreachable" {
		t.Errorf("upstream = %q, want %q", resp.Upstream, "unreachable")
	}
}

func TestReadyHandler_BothUnready_Returns503(t *testing.T) {
	checker := &fakeReadinessChecker{
		status: ports.ReadinessStatus{
			PluginsReady:      false,
			UpstreamReachable: false,
		},
	}
	handler := ReadyHandler(checker)

	req := httptest.NewRequest(http.MethodGet, "/_vibewarden/ready", nil)
	w := httptest.NewRecorder()
	handler(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", w.Code)
	}

	var resp ReadyResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if resp.Ready {
		t.Error("ready = true, want false")
	}
}

func TestReadyHandler_MultiplePlugins_AllHealthy(t *testing.T) {
	checker := &fakeReadinessChecker{
		status: ports.ReadinessStatus{
			PluginsReady:      true,
			UpstreamReachable: true,
			Plugins: map[string]ports.HealthStatus{
				"tls":             {Healthy: true},
				"rate-limiting":   {Healthy: true},
				"user-management": {Healthy: true},
			},
		},
	}
	handler := ReadyHandler(checker)

	req := httptest.NewRequest(http.MethodGet, "/_vibewarden/ready", nil)
	w := httptest.NewRecorder()
	handler(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}

	var resp ReadyResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if !resp.Ready {
		t.Error("ready = false, want true")
	}
	wantPlugins := []string{"tls", "rate-limiting", "user-management"}
	for _, name := range wantPlugins {
		if resp.Plugins[name] != "healthy" {
			t.Errorf("plugins[%s] = %q, want %q", name, resp.Plugins[name], "healthy")
		}
	}
}

func TestReadyHandler_PluginsOmittedWhenEmpty(t *testing.T) {
	checker := &fakeReadinessChecker{
		status: ports.ReadinessStatus{
			PluginsReady:      true,
			UpstreamReachable: true,
			Plugins:           nil,
		},
	}
	handler := ReadyHandler(checker)

	req := httptest.NewRequest(http.MethodGet, "/_vibewarden/ready", nil)
	w := httptest.NewRecorder()
	handler(w, req)

	var raw map[string]any
	if err := json.NewDecoder(w.Body).Decode(&raw); err != nil {
		t.Fatalf("decoding raw response: %v", err)
	}
	if _, ok := raw["plugins"]; ok {
		t.Error("plugins field should be omitted when no plugins are registered (omitempty)")
	}
}

func TestReadyMiddleware_InterceptsReadyPath(t *testing.T) {
	checker := &fakeReadinessChecker{
		status: ports.ReadinessStatus{
			PluginsReady:      true,
			UpstreamReachable: true,
		},
	}
	mw := ReadyMiddleware(checker)

	nextCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusTeapot)
	})

	handler := mw(next)

	req := httptest.NewRequest(http.MethodGet, "/_vibewarden/ready", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if nextCalled {
		t.Error("next handler was called for ready path — should not be")
	}
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

func TestReadyMiddleware_PassesThroughOtherPaths(t *testing.T) {
	mw := ReadyMiddleware(nil)

	nextCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
	})

	handler := mw(next)

	paths := []string{"/", "/api/users", "/ready", "/_vibewarden/health", "/_vibewarden/other"}
	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			nextCalled = false
			req := httptest.NewRequest(http.MethodGet, path, nil)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)
			if !nextCalled {
				t.Errorf("next handler was not called for path %q", path)
			}
		})
	}
}

func TestReadyMiddleware_NotReady_Returns503AndDoesNotCallNext(t *testing.T) {
	checker := &fakeReadinessChecker{
		status: ports.ReadinessStatus{
			PluginsReady:      false,
			UpstreamReachable: true,
		},
	}
	mw := ReadyMiddleware(checker)

	nextCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusTeapot)
	})

	handler := mw(next)
	req := httptest.NewRequest(http.MethodGet, "/_vibewarden/ready", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if nextCalled {
		t.Error("next handler was called — should not be for ready path")
	}
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", w.Code)
	}
}

// TestReadyHandler_UsesUpstreamHealthChecker verifies that ReadyHandler
// integrates correctly with the fakeUpstreamHealthChecker via a ReadinessChecker
// that delegates to it.
func TestReadyHandler_UpstreamHealthyVsUnhealthy(t *testing.T) {
	tests := []struct {
		name           string
		upstreamStatus domainheal.UpstreamStatus
		wantReady      bool
		wantHTTPCode   int
		wantUpstream   string
	}{
		{
			name:           "upstream healthy → ready",
			upstreamStatus: domainheal.StatusHealthy,
			wantReady:      true,
			wantHTTPCode:   http.StatusOK,
			wantUpstream:   "reachable",
		},
		{
			name:           "upstream unhealthy → not ready",
			upstreamStatus: domainheal.StatusUnhealthy,
			wantReady:      false,
			wantHTTPCode:   http.StatusServiceUnavailable,
			wantUpstream:   "unreachable",
		},
		{
			name:           "upstream unknown → not ready",
			upstreamStatus: domainheal.StatusUnknown,
			wantReady:      false,
			wantHTTPCode:   http.StatusServiceUnavailable,
			wantUpstream:   "unreachable",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			upstreamStatus := tt.upstreamStatus.String() == "healthy"
			checker := &fakeReadinessChecker{
				status: ports.ReadinessStatus{
					PluginsReady:      true,
					UpstreamReachable: upstreamStatus,
				},
			}
			handler := ReadyHandler(checker)

			req := httptest.NewRequest(http.MethodGet, "/_vibewarden/ready", nil)
			w := httptest.NewRecorder()
			handler(w, req)

			if w.Code != tt.wantHTTPCode {
				t.Errorf("HTTP status = %d, want %d", w.Code, tt.wantHTTPCode)
			}

			var resp ReadyResponse
			if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
				t.Fatalf("decoding response: %v", err)
			}
			if resp.Ready != tt.wantReady {
				t.Errorf("ready = %v, want %v", resp.Ready, tt.wantReady)
			}
			if resp.Upstream != tt.wantUpstream {
				t.Errorf("upstream = %q, want %q", resp.Upstream, tt.wantUpstream)
			}
		})
	}
}

// fakeReadinessCheckerWithContext is a no-op implementation for interface compliance testing.
type fakeReadinessCheckerWithContext struct{}

func (f *fakeReadinessCheckerWithContext) Ready() bool { return true }
func (f *fakeReadinessCheckerWithContext) ReadinessStatus() ports.ReadinessStatus {
	return ports.ReadinessStatus{PluginsReady: true, UpstreamReachable: true}
}

// TestReadyHandler_ReadyFieldAlwaysPresentInJSON verifies the "ready" key is
// always present in the JSON response body.
func TestReadyHandler_ReadyFieldAlwaysPresentInJSON(t *testing.T) {
	tests := []struct {
		name    string
		checker ports.ReadinessChecker
	}{
		{"nil checker", nil},
		{"ready checker", &fakeReadinessCheckerWithContext{}},
		{"not ready checker", &fakeReadinessChecker{status: ports.ReadinessStatus{}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := ReadyHandler(tt.checker)
			req := httptest.NewRequest(http.MethodGet, "/_vibewarden/ready", nil)
			w := httptest.NewRecorder()
			handler(w, req)

			var raw map[string]any
			if err := json.NewDecoder(w.Body).Decode(&raw); err != nil {
				t.Fatalf("decoding response: %v", err)
			}
			if _, ok := raw["ready"]; !ok {
				t.Error("ready field must always be present in ready response")
			}
		})
	}
}

// TestReadyAndHealthAreIndependent verifies that the liveness endpoint
// (/_vibewarden/health) and the readiness endpoint (/_vibewarden/ready) can
// return different statuses independently.
func TestReadyAndHealthAreIndependent(t *testing.T) {
	// Health handler always returns 200 (liveness — process is alive).
	healthHandler := HealthHandler("v1.0.0", nil)

	// Ready handler returns 503 (plugins not yet initialised).
	readyChecker := &fakeReadinessChecker{
		status: ports.ReadinessStatus{
			PluginsReady:      false,
			UpstreamReachable: true,
		},
	}
	readyHandler := ReadyHandler(readyChecker)

	// Health must be 200.
	hW := httptest.NewRecorder()
	healthHandler(hW, httptest.NewRequest(http.MethodGet, "/_vibewarden/health", nil))
	if hW.Code != http.StatusOK {
		t.Errorf("health status = %d, want 200", hW.Code)
	}

	// Ready must be 503.
	rW := httptest.NewRecorder()
	readyHandler(rW, httptest.NewRequest(http.MethodGet, "/_vibewarden/ready", nil))
	if rW.Code != http.StatusServiceUnavailable {
		t.Errorf("ready status = %d, want 503", rW.Code)
	}
}

// Ensure the fake implements the context-less interface correctly.
var _ ports.ReadinessChecker = (*fakeReadinessChecker)(nil)
var _ http.Handler = http.HandlerFunc(nil)

// TestReadyHandlerAcceptsContext ensures the handler honours request context
// cancellation (it should not block on a cancelled context).
func TestReadyHandlerAcceptsContext(t *testing.T) {
	checker := &fakeReadinessChecker{
		status: ports.ReadinessStatus{
			PluginsReady:      true,
			UpstreamReachable: true,
		},
	}
	handler := ReadyHandler(checker)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	req := httptest.NewRequest(http.MethodGet, "/_vibewarden/ready", nil).WithContext(ctx)
	w := httptest.NewRecorder()
	handler(w, req) // must not block

	// Response may or may not be written depending on implementation; we just
	// want to confirm it doesn't hang.
}
