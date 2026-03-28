package caddy

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	domainresilience "github.com/vibewarden/vibewarden/internal/domain/resilience"
	"github.com/vibewarden/vibewarden/internal/ports"
)

var _ ports.CircuitBreaker = (*fakeCB)(nil)

// fakeCB is a simple ports.CircuitBreaker fake for handler tests.
type fakeCB struct {
	IsOpenResult bool
	FailureCalls int
	SuccessCalls int
}

func (f *fakeCB) IsOpen() bool                  { return f.IsOpenResult }
func (f *fakeCB) RecordFailure()                { f.FailureCalls++ }
func (f *fakeCB) RecordSuccess()                { f.SuccessCalls++ }
func (f *fakeCB) State() domainresilience.State { return domainresilience.StateClosed }

// fakeCaddyHandler is a minimal caddyhttp.Handler that writes a fixed status code.
type fakeCaddyHandler struct {
	statusCode int
}

func (f *fakeCaddyHandler) ServeHTTP(w http.ResponseWriter, _ *http.Request) error {
	w.WriteHeader(f.statusCode)
	return nil
}

func TestCircuitBreakerHandler_OpenCircuit_Returns503(t *testing.T) {
	fake := &fakeCB{IsOpenResult: true}
	h := &CircuitBreakerHandler{
		cb: fake,
	}

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()

	err := h.ServeHTTP(rec, req, &fakeCaddyHandler{statusCode: http.StatusOK})
	if err != nil {
		t.Fatalf("ServeHTTP returned error: %v", err)
	}
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", rec.Code)
	}
	if fake.SuccessCalls != 0 || fake.FailureCalls != 0 {
		t.Errorf("open circuit should not record anything; success=%d, failure=%d",
			fake.SuccessCalls, fake.FailureCalls)
	}
}

func TestCircuitBreakerHandler_ClosedCircuit_SuccessRecorded(t *testing.T) {
	fake := &fakeCB{IsOpenResult: false}
	h := &CircuitBreakerHandler{cb: fake}

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()

	err := h.ServeHTTP(rec, req, &fakeCaddyHandler{statusCode: http.StatusOK})
	if err != nil {
		t.Fatalf("ServeHTTP returned error: %v", err)
	}
	if fake.SuccessCalls != 1 {
		t.Errorf("expected 1 RecordSuccess call, got %d", fake.SuccessCalls)
	}
	if fake.FailureCalls != 0 {
		t.Errorf("expected 0 RecordFailure calls, got %d", fake.FailureCalls)
	}
}

func TestCircuitBreakerHandler_UpstreamFailureStatuses(t *testing.T) {
	tests := []struct {
		name         string
		upstreamCode int
		wantFailure  bool
	}{
		{"502 bad gateway", http.StatusBadGateway, true},
		{"503 service unavailable", http.StatusServiceUnavailable, true},
		{"504 gateway timeout", http.StatusGatewayTimeout, true},
		{"200 ok", http.StatusOK, false},
		{"201 created", http.StatusCreated, false},
		{"404 not found", http.StatusNotFound, false},
		{"400 bad request", http.StatusBadRequest, false},
		{"500 internal server error", http.StatusInternalServerError, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := &fakeCB{IsOpenResult: false}
			h := &CircuitBreakerHandler{cb: fake}

			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			rec := httptest.NewRecorder()

			_ = h.ServeHTTP(rec, req, &fakeCaddyHandler{statusCode: tt.upstreamCode})

			if tt.wantFailure && fake.FailureCalls != 1 {
				t.Errorf("status %d: expected RecordFailure, got %d calls", tt.upstreamCode, fake.FailureCalls)
			}
			if !tt.wantFailure && fake.SuccessCalls != 1 {
				t.Errorf("status %d: expected RecordSuccess, got %d calls", tt.upstreamCode, fake.SuccessCalls)
			}
		})
	}
}

func TestBuildCircuitBreakerHandlerJSON_Disabled(t *testing.T) {
	cfg := ports.ResilienceConfig{
		CircuitBreaker: ports.CircuitBreakerConfig{Enabled: false},
	}
	result, err := buildCircuitBreakerHandlerJSON(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil {
		t.Errorf("expected nil when disabled, got %v", result)
	}
}

func TestBuildCircuitBreakerHandlerJSON_Enabled(t *testing.T) {
	cfg := ports.ResilienceConfig{
		CircuitBreaker: ports.CircuitBreakerConfig{
			Enabled:   true,
			Threshold: 5,
			Timeout:   60 * time.Second,
		},
	}
	result, err := buildCircuitBreakerHandlerJSON(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result when enabled")
	}
	if result["handler"] != "vibewarden_circuit_breaker" {
		t.Errorf("handler = %v, want vibewarden_circuit_breaker", result["handler"])
	}
	if result["config"] == nil {
		t.Error("expected config key in result")
	}
}

func TestBuildCaddyConfig_IncludesCircuitBreaker(t *testing.T) {
	cfg := &ports.ProxyConfig{
		ListenAddr:   "127.0.0.1:8080",
		UpstreamAddr: "127.0.0.1:3000",
		Resilience: ports.ResilienceConfig{
			CircuitBreaker: ports.CircuitBreakerConfig{
				Enabled:   true,
				Threshold: 5,
				Timeout:   60 * time.Second,
			},
		},
	}

	result, err := BuildCaddyConfig(cfg)
	if err != nil {
		t.Fatalf("BuildCaddyConfig: %v", err)
	}

	// Walk the handler chain and find the circuit breaker.
	handlers := extractHandlers(t, result)
	found := false
	for _, h := range handlers {
		if h["handler"] == "vibewarden_circuit_breaker" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("circuit breaker handler not found in handler chain: %v", handlers)
	}
}

func TestBuildCaddyConfig_CircuitBreakerBeforeTimeout(t *testing.T) {
	cfg := &ports.ProxyConfig{
		ListenAddr:   "127.0.0.1:8080",
		UpstreamAddr: "127.0.0.1:3000",
		Resilience: ports.ResilienceConfig{
			Timeout: 30 * time.Second,
			CircuitBreaker: ports.CircuitBreakerConfig{
				Enabled:   true,
				Threshold: 5,
				Timeout:   60 * time.Second,
			},
		},
	}

	result, err := BuildCaddyConfig(cfg)
	if err != nil {
		t.Fatalf("BuildCaddyConfig: %v", err)
	}

	handlers := extractHandlers(t, result)
	cbIdx, toIdx := -1, -1
	for i, h := range handlers {
		switch h["handler"] {
		case "vibewarden_circuit_breaker":
			cbIdx = i
		case "vibewarden_timeout":
			toIdx = i
		}
	}

	if cbIdx == -1 {
		t.Fatal("circuit breaker handler not found")
	}
	if toIdx == -1 {
		t.Fatal("timeout handler not found")
	}
	if cbIdx >= toIdx {
		t.Errorf("circuit breaker (index %d) must come before timeout (index %d)", cbIdx, toIdx)
	}
}

// extractHandlers walks the Caddy JSON config tree and returns the handler slice
// from the catch-all route.
func extractHandlers(t *testing.T, config map[string]any) []map[string]any {
	t.Helper()
	apps, _ := config["apps"].(map[string]any)
	httpApp, _ := apps["http"].(map[string]any)
	servers, _ := httpApp["servers"].(map[string]any)
	vw, _ := servers["vibewarden"].(map[string]any)
	routes, _ := vw["routes"].([]map[string]any)

	// The catch-all route is the last one.
	if len(routes) == 0 {
		t.Fatal("no routes found")
	}
	last := routes[len(routes)-1]
	handlers, _ := last["handle"].([]map[string]any)
	return handlers
}
