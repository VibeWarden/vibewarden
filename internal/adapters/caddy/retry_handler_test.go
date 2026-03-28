package caddy

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/vibewarden/vibewarden/internal/domain/resilience"
	"github.com/vibewarden/vibewarden/internal/ports"
)

var _ ports.MetricsCollector = (*fakeMetrics)(nil)

// fakeMetrics is a minimal ports.MetricsCollector fake for handler tests.
type fakeMetrics struct {
	retryIncrements []string
}

func (f *fakeMetrics) IncRequestTotal(_, _, _ string)                      {}
func (f *fakeMetrics) ObserveRequestDuration(_, _ string, _ time.Duration) {}
func (f *fakeMetrics) IncRateLimitHit(_ string)                            {}
func (f *fakeMetrics) IncAuthDecision(_ string)                            {}
func (f *fakeMetrics) IncUpstreamError()                                   {}
func (f *fakeMetrics) IncUpstreamTimeout()                                 {}
func (f *fakeMetrics) IncUpstreamRetry(method string) {
	f.retryIncrements = append(f.retryIncrements, method)
}
func (f *fakeMetrics) SetActiveConnections(_ int)                                   {}
func (f *fakeMetrics) SetCircuitBreakerState(_ context.Context, _ resilience.State) {}

// sequentialHandler calls each inner handler in sequence, one per ServeHTTP call.
type sequentialHandler struct {
	handlers []*fakeCaddyHandler
	calls    int
}

func (s *sequentialHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) error {
	idx := s.calls
	if idx >= len(s.handlers) {
		idx = len(s.handlers) - 1
	}
	s.calls++
	return s.handlers[idx].ServeHTTP(w, r)
}

func TestRetryHandler_NonIdempotentMethod_NoRetry(t *testing.T) {
	seq := &sequentialHandler{
		handlers: []*fakeCaddyHandler{
			{statusCode: http.StatusServiceUnavailable},
			{statusCode: http.StatusOK},
		},
	}
	h := &RetryHandler{
		Config: RetryHandlerConfig{
			MaxAttempts:      3,
			InitialBackoffMs: 0,
			MaxBackoffMs:     0,
			RetryOn:          []int{502, 503, 504},
		},
	}

	req := httptest.NewRequest(http.MethodPost, "/submit", nil)
	rec := httptest.NewRecorder()

	err := h.ServeHTTP(rec, req, seq)
	if err != nil {
		t.Fatalf("ServeHTTP returned error: %v", err)
	}
	// POST must not be retried — handler called exactly once, 503 returned.
	if seq.calls != 1 {
		t.Errorf("expected 1 call for POST, got %d", seq.calls)
	}
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", rec.Code)
	}
}

func TestRetryHandler_IdempotentMethod_RetriesOnConfiguredStatus(t *testing.T) {
	tests := []struct {
		name          string
		method        string
		responses     []int
		wantCalls     int
		wantFinalCode int
	}{
		{
			name:          "GET retries on 503, succeeds on second attempt",
			method:        http.MethodGet,
			responses:     []int{503, 200},
			wantCalls:     2,
			wantFinalCode: 200,
		},
		{
			name:          "HEAD retries on 502, succeeds on second attempt",
			method:        http.MethodHead,
			responses:     []int{502, 200},
			wantCalls:     2,
			wantFinalCode: 200,
		},
		{
			name:          "PUT retries twice, succeeds on third attempt",
			method:        http.MethodPut,
			responses:     []int{503, 503, 200},
			wantCalls:     3,
			wantFinalCode: 200,
		},
		{
			name:          "DELETE exhausts all attempts",
			method:        http.MethodDelete,
			responses:     []int{503, 503, 503},
			wantCalls:     3,
			wantFinalCode: 503,
		},
		{
			name:          "GET does not retry on 500",
			method:        http.MethodGet,
			responses:     []int{500, 200},
			wantCalls:     1,
			wantFinalCode: 500,
		},
		{
			name:          "OPTIONS retries on 504",
			method:        http.MethodOptions,
			responses:     []int{504, 200},
			wantCalls:     2,
			wantFinalCode: 200,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handlers := make([]*fakeCaddyHandler, len(tt.responses))
			for i, code := range tt.responses {
				handlers[i] = &fakeCaddyHandler{statusCode: code}
			}
			seq := &sequentialHandler{handlers: handlers}

			fm := &fakeMetrics{}
			h := &RetryHandler{
				Config: RetryHandlerConfig{
					MaxAttempts:      len(tt.responses),
					InitialBackoffMs: 0,
					MaxBackoffMs:     0,
					RetryOn:          []int{502, 503, 504},
				},
				metrics: fm,
			}

			req := httptest.NewRequest(tt.method, "/resource", nil)
			rec := httptest.NewRecorder()

			err := h.ServeHTTP(rec, req, seq)
			if err != nil {
				t.Fatalf("ServeHTTP returned error: %v", err)
			}

			if seq.calls != tt.wantCalls {
				t.Errorf("calls = %d, want %d", seq.calls, tt.wantCalls)
			}
			if rec.Code != tt.wantFinalCode {
				t.Errorf("status = %d, want %d", rec.Code, tt.wantFinalCode)
			}

			// Metric increments = number of retries = calls - 1 (no metric on initial attempt)
			wantRetryMetrics := tt.wantCalls - 1
			if len(fm.retryIncrements) != wantRetryMetrics {
				t.Errorf("retry metric increments = %d, want %d", len(fm.retryIncrements), wantRetryMetrics)
			}
		})
	}
}

func TestRetryHandler_ContextCancellation_StopsRetries(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	callCount := 0
	handler := caddyHandlerFunc(func(w http.ResponseWriter, r *http.Request) error {
		callCount++
		w.WriteHeader(http.StatusServiceUnavailable)
		// Cancel context after first call.
		cancel()
		return nil
	})

	h := &RetryHandler{
		Config: RetryHandlerConfig{
			MaxAttempts:      5,
			InitialBackoffMs: 0,
			MaxBackoffMs:     0,
			RetryOn:          []int{503},
		},
	}

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	err := h.ServeHTTP(rec, req, handler)
	// After cancel the handler should return context.Canceled — not nil.
	if err == nil {
		t.Error("expected non-nil error after context cancellation, got nil")
	}
	if callCount > 2 {
		t.Errorf("expected at most 2 calls before context cancellation, got %d", callCount)
	}
}

func TestRetryHandler_ExponentialBackoff(t *testing.T) {
	// Verify that the handler computes increasing backoffs without exceeding MaxBackoffMs.
	// We test this by checking the time elapsed across two retries.
	// With InitialBackoffMs=10 and MaxBackoffMs=15:
	//   attempt 1: sleep 10ms  → backoff doubles to 20ms, capped to 15ms
	//   attempt 2: sleep 15ms
	// Total minimum elapsed: ~25ms.

	seq := &sequentialHandler{
		handlers: []*fakeCaddyHandler{
			{statusCode: 503},
			{statusCode: 503},
			{statusCode: 200},
		},
	}
	h := &RetryHandler{
		Config: RetryHandlerConfig{
			MaxAttempts:      3,
			InitialBackoffMs: 10,
			MaxBackoffMs:     15,
			RetryOn:          []int{503},
		},
	}

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()

	start := time.Now()
	if err := h.ServeHTTP(rec, req, seq); err != nil {
		t.Fatalf("ServeHTTP returned error: %v", err)
	}
	elapsed := time.Since(start)

	if elapsed < 20*time.Millisecond {
		t.Errorf("elapsed = %v; expected at least 20ms for two backoff sleeps", elapsed)
	}
	if rec.Code != http.StatusOK {
		t.Errorf("final status = %d, want 200", rec.Code)
	}
}

func TestBuildRetryHandlerJSON_Disabled(t *testing.T) {
	cfg := ports.ResilienceConfig{
		Retry: ports.RetryConfig{Enabled: false},
	}
	result, err := buildRetryHandlerJSON(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil {
		t.Errorf("expected nil when disabled, got %v", result)
	}
}

func TestBuildRetryHandlerJSON_Enabled(t *testing.T) {
	cfg := ports.ResilienceConfig{
		Retry: ports.RetryConfig{
			Enabled:        true,
			MaxAttempts:    3,
			InitialBackoff: 100 * time.Millisecond,
			MaxBackoff:     10 * time.Second,
			RetryOn:        []int{502, 503, 504},
		},
	}
	result, err := buildRetryHandlerJSON(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result when enabled")
	}
	if result["handler"] != "vibewarden_retry" {
		t.Errorf("handler = %v, want vibewarden_retry", result["handler"])
	}
	if result["config"] == nil {
		t.Error("expected config key in result")
	}
}

func TestBuildCaddyConfig_IncludesRetry(t *testing.T) {
	cfg := &ports.ProxyConfig{
		ListenAddr:   "127.0.0.1:8080",
		UpstreamAddr: "127.0.0.1:3000",
		Resilience: ports.ResilienceConfig{
			Retry: ports.RetryConfig{
				Enabled:        true,
				MaxAttempts:    3,
				InitialBackoff: 100 * time.Millisecond,
				MaxBackoff:     10 * time.Second,
				RetryOn:        []int{502, 503, 504},
			},
		},
	}

	result, err := BuildCaddyConfig(cfg)
	if err != nil {
		t.Fatalf("BuildCaddyConfig: %v", err)
	}

	handlers := extractHandlers(t, result)
	found := false
	for _, h := range handlers {
		if h["handler"] == "vibewarden_retry" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("retry handler not found in handler chain: %v", handlers)
	}
}

func TestBuildCaddyConfig_RetryAfterCircuitBreakerBeforeTimeout(t *testing.T) {
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
			Retry: ports.RetryConfig{
				Enabled:        true,
				MaxAttempts:    3,
				InitialBackoff: 100 * time.Millisecond,
				MaxBackoff:     10 * time.Second,
				RetryOn:        []int{502, 503, 504},
			},
		},
	}

	result, err := BuildCaddyConfig(cfg)
	if err != nil {
		t.Fatalf("BuildCaddyConfig: %v", err)
	}

	handlers := extractHandlers(t, result)
	cbIdx, retryIdx, toIdx := -1, -1, -1
	for i, h := range handlers {
		switch h["handler"] {
		case "vibewarden_circuit_breaker":
			cbIdx = i
		case "vibewarden_retry":
			retryIdx = i
		case "vibewarden_timeout":
			toIdx = i
		}
	}

	if cbIdx == -1 {
		t.Fatal("circuit breaker handler not found")
	}
	if retryIdx == -1 {
		t.Fatal("retry handler not found")
	}
	if toIdx == -1 {
		t.Fatal("timeout handler not found")
	}
	if cbIdx >= retryIdx {
		t.Errorf("circuit breaker (index %d) must come before retry (index %d)", cbIdx, retryIdx)
	}
	if retryIdx >= toIdx {
		t.Errorf("retry (index %d) must come before timeout (index %d)", retryIdx, toIdx)
	}
}

// caddyHandlerFunc allows using a function as a caddyhttp.Handler in tests.
type caddyHandlerFunc func(w http.ResponseWriter, r *http.Request) error

func (f caddyHandlerFunc) ServeHTTP(w http.ResponseWriter, r *http.Request) error {
	return f(w, r)
}
