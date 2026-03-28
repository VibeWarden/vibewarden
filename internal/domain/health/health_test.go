package health_test

import (
	"testing"
	"time"

	"github.com/vibewarden/vibewarden/internal/domain/health"
)

func validConfig() health.Config {
	return health.Config{
		Enabled:            true,
		Path:               "/health",
		Interval:           10 * time.Second,
		Timeout:            5 * time.Second,
		UnhealthyThreshold: 3,
		HealthyThreshold:   2,
	}
}

func TestConfigValidate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     health.Config
		wantErr bool
	}{
		{
			name:    "disabled config always valid",
			cfg:     health.Config{Enabled: false},
			wantErr: false,
		},
		{
			name:    "valid enabled config",
			cfg:     validConfig(),
			wantErr: false,
		},
		{
			name: "empty path",
			cfg: func() health.Config {
				c := validConfig()
				c.Path = ""
				return c
			}(),
			wantErr: true,
		},
		{
			name: "zero interval",
			cfg: func() health.Config {
				c := validConfig()
				c.Interval = 0
				return c
			}(),
			wantErr: true,
		},
		{
			name: "zero timeout",
			cfg: func() health.Config {
				c := validConfig()
				c.Timeout = 0
				return c
			}(),
			wantErr: true,
		},
		{
			name: "zero unhealthy_threshold",
			cfg: func() health.Config {
				c := validConfig()
				c.UnhealthyThreshold = 0
				return c
			}(),
			wantErr: true,
		},
		{
			name: "zero healthy_threshold",
			cfg: func() health.Config {
				c := validConfig()
				c.HealthyThreshold = 0
				return c
			}(),
			wantErr: true,
		},
		{
			name: "negative unhealthy_threshold",
			cfg: func() health.Config {
				c := validConfig()
				c.UnhealthyThreshold = -1
				return c
			}(),
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

func TestUpstreamStatusString(t *testing.T) {
	tests := []struct {
		status health.UpstreamStatus
		want   string
	}{
		{health.StatusUnknown, "unknown"},
		{health.StatusHealthy, "healthy"},
		{health.StatusUnhealthy, "unhealthy"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := tt.status.String(); got != tt.want {
				t.Errorf("String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNewUpstreamHealth(t *testing.T) {
	t.Run("valid config", func(t *testing.T) {
		u, err := health.NewUpstreamHealth(validConfig())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if u.Status() != health.StatusUnknown {
			t.Errorf("initial status = %v, want Unknown", u.Status())
		}
	})

	t.Run("invalid config returns error", func(t *testing.T) {
		cfg := validConfig()
		cfg.Path = ""
		_, err := health.NewUpstreamHealth(cfg)
		if err == nil {
			t.Error("expected error for invalid config, got nil")
		}
	})
}

func TestRecordSuccess_TransitionsToHealthy(t *testing.T) {
	cfg := validConfig()
	cfg.HealthyThreshold = 2
	u, _ := health.NewUpstreamHealth(cfg)
	now := time.Now()

	// First success: no transition yet
	prev, transitioned := u.RecordSuccess(now)
	if transitioned {
		t.Error("expected no transition on first success")
	}
	if prev != health.StatusUnknown {
		t.Errorf("prev = %v, want Unknown", prev)
	}
	if u.Status() != health.StatusUnknown {
		t.Errorf("status = %v, want Unknown", u.Status())
	}
	if u.ConsecutiveSuccesses() != 1 {
		t.Errorf("successes = %d, want 1", u.ConsecutiveSuccesses())
	}

	// Second success: should transition to Healthy
	prev, transitioned = u.RecordSuccess(now.Add(time.Second))
	if !transitioned {
		t.Error("expected transition on second success")
	}
	if prev != health.StatusUnknown {
		t.Errorf("prev = %v, want Unknown", prev)
	}
	if u.Status() != health.StatusHealthy {
		t.Errorf("status = %v, want Healthy", u.Status())
	}
}

func TestRecordFailure_TransitionsToUnhealthy(t *testing.T) {
	cfg := validConfig()
	cfg.UnhealthyThreshold = 2
	cfg.HealthyThreshold = 1
	u, _ := health.NewUpstreamHealth(cfg)

	// Make healthy first
	now := time.Now()
	u.RecordSuccess(now)
	if u.Status() != health.StatusHealthy {
		t.Fatalf("setup: expected Healthy, got %v", u.Status())
	}

	// First failure: no transition
	prev, transitioned := u.RecordFailure(now.Add(time.Second), "connection refused")
	if transitioned {
		t.Error("expected no transition on first failure")
	}
	if prev != health.StatusHealthy {
		t.Errorf("prev = %v, want Healthy", prev)
	}
	if u.Status() != health.StatusHealthy {
		t.Errorf("status = %v, want Healthy after 1 failure (threshold=2)", u.Status())
	}
	if u.LastError() != "connection refused" {
		t.Errorf("LastError = %q, want 'connection refused'", u.LastError())
	}

	// Second failure: should transition to Unhealthy
	prev, transitioned = u.RecordFailure(now.Add(2*time.Second), "timeout")
	if !transitioned {
		t.Error("expected transition on second failure")
	}
	if prev != health.StatusHealthy {
		t.Errorf("prev = %v, want Healthy", prev)
	}
	if u.Status() != health.StatusUnhealthy {
		t.Errorf("status = %v, want Unhealthy", u.Status())
	}
}

func TestRecordSuccess_ClearsFailureCount(t *testing.T) {
	cfg := validConfig()
	cfg.UnhealthyThreshold = 3
	cfg.HealthyThreshold = 2
	u, _ := health.NewUpstreamHealth(cfg)
	now := time.Now()

	// Record 2 failures (below threshold)
	u.RecordFailure(now, "err")
	u.RecordFailure(now.Add(time.Second), "err")
	if u.ConsecutiveFailures() != 2 {
		t.Fatalf("expected 2 consecutive failures, got %d", u.ConsecutiveFailures())
	}

	// A success resets failure count
	u.RecordSuccess(now.Add(2 * time.Second))
	if u.ConsecutiveFailures() != 0 {
		t.Errorf("failures = %d, want 0 after success", u.ConsecutiveFailures())
	}
	if u.LastError() != "" {
		t.Errorf("LastError = %q, want empty after success", u.LastError())
	}
}

func TestRecordFailure_ClearsSuccessCount(t *testing.T) {
	cfg := validConfig()
	cfg.HealthyThreshold = 3
	cfg.UnhealthyThreshold = 2
	u, _ := health.NewUpstreamHealth(cfg)
	now := time.Now()

	// Record 2 successes (below threshold)
	u.RecordSuccess(now)
	u.RecordSuccess(now.Add(time.Second))
	if u.ConsecutiveSuccesses() != 2 {
		t.Fatalf("expected 2 consecutive successes, got %d", u.ConsecutiveSuccesses())
	}

	// A failure resets success count
	u.RecordFailure(now.Add(2*time.Second), "err")
	if u.ConsecutiveSuccesses() != 0 {
		t.Errorf("successes = %d, want 0 after failure", u.ConsecutiveSuccesses())
	}
}

func TestRecordSuccess_AlreadyHealthy_NoTransition(t *testing.T) {
	cfg := validConfig()
	cfg.HealthyThreshold = 1
	u, _ := health.NewUpstreamHealth(cfg)
	now := time.Now()

	// Become healthy
	_, transitioned := u.RecordSuccess(now)
	if !transitioned {
		t.Fatal("expected transition to Healthy")
	}

	// Another success: already Healthy, no transition
	_, transitioned = u.RecordSuccess(now.Add(time.Second))
	if transitioned {
		t.Error("expected no transition when already Healthy")
	}
}

func TestRecordFailure_AlreadyUnhealthy_NoTransition(t *testing.T) {
	cfg := validConfig()
	cfg.UnhealthyThreshold = 1
	u, _ := health.NewUpstreamHealth(cfg)
	now := time.Now()

	// Become unhealthy
	_, transitioned := u.RecordFailure(now, "err")
	if !transitioned {
		t.Fatal("expected transition to Unhealthy")
	}

	// Another failure: already Unhealthy, no transition
	_, transitioned = u.RecordFailure(now.Add(time.Second), "err2")
	if transitioned {
		t.Error("expected no transition when already Unhealthy")
	}
}

func TestRecordSuccess_UpdatesLastProbed(t *testing.T) {
	u, _ := health.NewUpstreamHealth(validConfig())
	now := time.Now()

	if !u.LastProbed().IsZero() {
		t.Error("expected zero LastProbed before any probe")
	}
	u.RecordSuccess(now)
	if !u.LastProbed().Equal(now) {
		t.Errorf("LastProbed = %v, want %v", u.LastProbed(), now)
	}
}

func TestRecordFailure_UpdatesLastProbed(t *testing.T) {
	u, _ := health.NewUpstreamHealth(validConfig())
	now := time.Now()

	u.RecordFailure(now, "err")
	if !u.LastProbed().Equal(now) {
		t.Errorf("LastProbed = %v, want %v", u.LastProbed(), now)
	}
}

func TestUnknownToUnhealthy_DirectTransition(t *testing.T) {
	cfg := validConfig()
	cfg.UnhealthyThreshold = 1
	u, _ := health.NewUpstreamHealth(cfg)
	now := time.Now()

	prev, transitioned := u.RecordFailure(now, "dial error")
	if !transitioned {
		t.Error("expected transition from Unknown to Unhealthy")
	}
	if prev != health.StatusUnknown {
		t.Errorf("prev = %v, want Unknown", prev)
	}
	if u.Status() != health.StatusUnhealthy {
		t.Errorf("status = %v, want Unhealthy", u.Status())
	}
}
