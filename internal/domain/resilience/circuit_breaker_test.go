package resilience_test

import (
	"testing"
	"time"

	"github.com/vibewarden/vibewarden/internal/domain/resilience"
)

func validConfig() resilience.CircuitBreakerConfig {
	return resilience.CircuitBreakerConfig{
		Threshold: 3,
		Timeout:   10 * time.Second,
	}
}

func TestCircuitBreakerConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     resilience.CircuitBreakerConfig
		wantErr bool
	}{
		{
			name:    "valid config",
			cfg:     resilience.CircuitBreakerConfig{Threshold: 1, Timeout: time.Second},
			wantErr: false,
		},
		{
			name:    "zero threshold",
			cfg:     resilience.CircuitBreakerConfig{Threshold: 0, Timeout: time.Second},
			wantErr: true,
		},
		{
			name:    "negative threshold",
			cfg:     resilience.CircuitBreakerConfig{Threshold: -1, Timeout: time.Second},
			wantErr: true,
		},
		{
			name:    "zero timeout",
			cfg:     resilience.CircuitBreakerConfig{Threshold: 1, Timeout: 0},
			wantErr: true,
		},
		{
			name:    "negative timeout",
			cfg:     resilience.CircuitBreakerConfig{Threshold: 1, Timeout: -time.Second},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestNewCircuitBreaker_InvalidConfig(t *testing.T) {
	_, err := resilience.NewCircuitBreaker(resilience.CircuitBreakerConfig{Threshold: 0, Timeout: time.Second})
	if err == nil {
		t.Error("expected error for zero threshold, got nil")
	}
}

func TestCircuitBreaker_InitialState(t *testing.T) {
	cb, err := resilience.NewCircuitBreaker(validConfig())
	if err != nil {
		t.Fatalf("NewCircuitBreaker: %v", err)
	}
	if got := cb.State(); got != resilience.StateClosed {
		t.Errorf("initial state = %v, want Closed", got)
	}
	if cb.IsOpen(time.Now()) {
		t.Error("expected IsOpen = false in initial closed state")
	}
}

func TestCircuitBreaker_Closed_FailuresAccumulate(t *testing.T) {
	cfg := resilience.CircuitBreakerConfig{Threshold: 3, Timeout: time.Minute}
	cb, _ := resilience.NewCircuitBreaker(cfg)
	now := time.Now()

	// First two failures should not trip.
	cb.RecordFailure(now)
	cb.RecordFailure(now)
	if cb.State() != resilience.StateClosed {
		t.Errorf("expected Closed after 2 failures, got %v", cb.State())
	}
	if cb.Failures() != 2 {
		t.Errorf("expected 2 failures, got %d", cb.Failures())
	}

	// Third failure reaches threshold → Open.
	_, transitioned := cb.RecordFailure(now)
	if !transitioned {
		t.Error("expected transition on threshold failure")
	}
	if cb.State() != resilience.StateOpen {
		t.Errorf("expected Open after threshold, got %v", cb.State())
	}
}

func TestCircuitBreaker_Open_BlocksRequests(t *testing.T) {
	cfg := resilience.CircuitBreakerConfig{Threshold: 1, Timeout: time.Minute}
	cb, _ := resilience.NewCircuitBreaker(cfg)
	now := time.Now()

	cb.RecordFailure(now)

	if !cb.IsOpen(now) {
		t.Error("expected IsOpen = true when Open and timeout not elapsed")
	}
}

func TestCircuitBreaker_Open_TransitionsToHalfOpenAfterTimeout(t *testing.T) {
	cfg := resilience.CircuitBreakerConfig{Threshold: 1, Timeout: time.Second}
	cb, _ := resilience.NewCircuitBreaker(cfg)
	opened := time.Now()

	cb.RecordFailure(opened)

	// Before timeout: still open.
	if !cb.IsOpen(opened.Add(500 * time.Millisecond)) {
		t.Error("expected still open before timeout")
	}

	// After timeout: transitions to HalfOpen and allows probe.
	if cb.IsOpen(opened.Add(2 * time.Second)) {
		t.Error("expected IsOpen = false after timeout (HalfOpen probe allowed)")
	}
	if cb.State() != resilience.StateHalfOpen {
		t.Errorf("expected HalfOpen after timeout, got %v", cb.State())
	}
}

func TestCircuitBreaker_HalfOpen_SuccessCloses(t *testing.T) {
	cfg := resilience.CircuitBreakerConfig{Threshold: 1, Timeout: time.Second}
	cb, _ := resilience.NewCircuitBreaker(cfg)
	opened := time.Now()

	cb.RecordFailure(opened)
	// Advance to HalfOpen.
	cb.IsOpen(opened.Add(2 * time.Second))

	// HalfOpen: first request after IsOpen advances state. Now record success.
	previous := cb.RecordSuccess()
	if previous != resilience.StateHalfOpen {
		t.Errorf("RecordSuccess previous state = %v, want HalfOpen", previous)
	}
	if cb.State() != resilience.StateClosed {
		t.Errorf("expected Closed after probe success, got %v", cb.State())
	}
	if cb.Failures() != 0 {
		t.Errorf("expected 0 failures after close, got %d", cb.Failures())
	}
}

func TestCircuitBreaker_HalfOpen_FailureReopens(t *testing.T) {
	cfg := resilience.CircuitBreakerConfig{Threshold: 1, Timeout: time.Second}
	cb, _ := resilience.NewCircuitBreaker(cfg)
	opened := time.Now()

	cb.RecordFailure(opened)
	// Advance to HalfOpen.
	cb.IsOpen(opened.Add(2 * time.Second))

	// HalfOpen: probe fails → back to Open.
	probeTime := opened.Add(2 * time.Second)
	_, transitioned := cb.RecordFailure(probeTime)
	if !transitioned {
		t.Error("expected transition on HalfOpen failure")
	}
	if cb.State() != resilience.StateOpen {
		t.Errorf("expected Open after probe failure, got %v", cb.State())
	}
}

func TestCircuitBreaker_HalfOpen_BlocksFurtherRequests(t *testing.T) {
	cfg := resilience.CircuitBreakerConfig{Threshold: 1, Timeout: time.Second}
	cb, _ := resilience.NewCircuitBreaker(cfg)
	opened := time.Now()

	cb.RecordFailure(opened)
	// Advance to HalfOpen — first IsOpen call allows probe through.
	cb.IsOpen(opened.Add(2 * time.Second))

	// A second IsOpen call while in HalfOpen should block further requests.
	if !cb.IsOpen(opened.Add(2 * time.Second)) {
		t.Error("expected HalfOpen to block concurrent requests (probe slot consumed)")
	}
}

func TestCircuitBreaker_SuccessResetFailures(t *testing.T) {
	cfg := resilience.CircuitBreakerConfig{Threshold: 5, Timeout: time.Minute}
	cb, _ := resilience.NewCircuitBreaker(cfg)
	now := time.Now()

	cb.RecordFailure(now)
	cb.RecordFailure(now)
	cb.RecordSuccess()

	if cb.Failures() != 0 {
		t.Errorf("expected failures reset to 0 after success, got %d", cb.Failures())
	}
	if cb.State() != resilience.StateClosed {
		t.Errorf("expected Closed after success, got %v", cb.State())
	}
}

func TestState_String(t *testing.T) {
	tests := []struct {
		state resilience.State
		want  string
	}{
		{resilience.StateClosed, "closed"},
		{resilience.StateOpen, "open"},
		{resilience.StateHalfOpen, "half_open"},
		{resilience.State(99), "unknown"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := tt.state.String(); got != tt.want {
				t.Errorf("State.String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCircuitBreaker_OpenedAt(t *testing.T) {
	cfg := resilience.CircuitBreakerConfig{Threshold: 1, Timeout: time.Minute}
	cb, _ := resilience.NewCircuitBreaker(cfg)

	// Before any failures, OpenedAt should be zero.
	if !cb.OpenedAt().IsZero() {
		t.Error("expected zero OpenedAt before any failure")
	}

	now := time.Now()
	cb.RecordFailure(now)
	if cb.OpenedAt().IsZero() {
		t.Error("expected non-zero OpenedAt after tripping")
	}
}

func TestCircuitBreaker_Config(t *testing.T) {
	cfg := validConfig()
	cb, _ := resilience.NewCircuitBreaker(cfg)
	got := cb.Config()
	if got.Threshold != cfg.Threshold || got.Timeout != cfg.Timeout {
		t.Errorf("Config() = %+v, want %+v", got, cfg)
	}
}
