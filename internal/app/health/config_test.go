package health

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/vibewarden/vibewarden/internal/config"
)

func newTestLogger(buf *bytes.Buffer) *slog.Logger {
	return slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

func TestBuildDomainConfig(t *testing.T) {
	tests := []struct {
		name          string
		cfg           config.UpstreamHealthConfig
		wantEnabled   bool
		wantPath      string
		wantInterval  time.Duration
		wantTimeout   time.Duration
		wantUnhealthy int
		wantHealthy   int
		wantWarn      string // substring expected in log output when non-empty
	}{
		{
			name:        "disabled → (zero, false) with warn log",
			cfg:         config.UpstreamHealthConfig{Enabled: false},
			wantEnabled: false,
			wantWarn:    "upstream health probe disabled",
		},
		{
			name: "enabled with all defaults",
			cfg: config.UpstreamHealthConfig{
				Enabled: true,
			},
			wantEnabled:   true,
			wantPath:      "/health",
			wantInterval:  5 * time.Second,
			wantTimeout:   2 * time.Second,
			wantUnhealthy: 3,
			wantHealthy:   2,
		},
		{
			name: "enabled with explicit values",
			cfg: config.UpstreamHealthConfig{
				Enabled:            true,
				Path:               "/ping",
				Interval:           "15s",
				Timeout:            "3s",
				UnhealthyThreshold: 5,
				HealthyThreshold:   4,
			},
			wantEnabled:   true,
			wantPath:      "/ping",
			wantInterval:  15 * time.Second,
			wantTimeout:   3 * time.Second,
			wantUnhealthy: 5,
			wantHealthy:   4,
		},
		{
			name: "enabled with malformed interval falls back to 5s and warns",
			cfg: config.UpstreamHealthConfig{
				Enabled:  true,
				Interval: "not-a-duration",
			},
			wantEnabled:   true,
			wantPath:      "/health",
			wantInterval:  5 * time.Second,
			wantTimeout:   2 * time.Second,
			wantUnhealthy: 3,
			wantHealthy:   2,
			wantWarn:      "invalid duration in config",
		},
		{
			name: "enabled with malformed timeout falls back to 2s and warns",
			cfg: config.UpstreamHealthConfig{
				Enabled: true,
				Timeout: "bad",
			},
			wantEnabled:   true,
			wantPath:      "/health",
			wantInterval:  5 * time.Second,
			wantTimeout:   2 * time.Second,
			wantUnhealthy: 3,
			wantHealthy:   2,
			wantWarn:      "invalid duration in config",
		},
		{
			name: "enabled with empty path defaults to /health",
			cfg: config.UpstreamHealthConfig{
				Enabled: true,
				Path:    "",
			},
			wantEnabled:   true,
			wantPath:      "/health",
			wantInterval:  5 * time.Second,
			wantTimeout:   2 * time.Second,
			wantUnhealthy: 3,
			wantHealthy:   2,
		},
		{
			name: "enabled with zero thresholds use defaults",
			cfg: config.UpstreamHealthConfig{
				Enabled:            true,
				UnhealthyThreshold: 0,
				HealthyThreshold:   0,
			},
			wantEnabled:   true,
			wantPath:      "/health",
			wantInterval:  5 * time.Second,
			wantTimeout:   2 * time.Second,
			wantUnhealthy: 3,
			wantHealthy:   2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			logger := newTestLogger(&buf)

			domainCfg, enabled := BuildDomainConfig(tt.cfg, logger)

			if enabled != tt.wantEnabled {
				t.Errorf("enabled = %v, want %v", enabled, tt.wantEnabled)
			}

			logOutput := buf.String()

			if tt.wantWarn != "" && !strings.Contains(logOutput, tt.wantWarn) {
				t.Errorf("expected log to contain %q, got: %s", tt.wantWarn, logOutput)
			}

			if !tt.wantEnabled {
				// Zero Config returned when disabled.
				if domainCfg.Enabled {
					t.Error("domain Config.Enabled should be false when probe is disabled")
				}
				return
			}

			if domainCfg.Path != tt.wantPath {
				t.Errorf("Path = %q, want %q", domainCfg.Path, tt.wantPath)
			}
			if domainCfg.Interval != tt.wantInterval {
				t.Errorf("Interval = %v, want %v", domainCfg.Interval, tt.wantInterval)
			}
			if domainCfg.Timeout != tt.wantTimeout {
				t.Errorf("Timeout = %v, want %v", domainCfg.Timeout, tt.wantTimeout)
			}
			if domainCfg.UnhealthyThreshold != tt.wantUnhealthy {
				t.Errorf("UnhealthyThreshold = %d, want %d", domainCfg.UnhealthyThreshold, tt.wantUnhealthy)
			}
			if domainCfg.HealthyThreshold != tt.wantHealthy {
				t.Errorf("HealthyThreshold = %d, want %d", domainCfg.HealthyThreshold, tt.wantHealthy)
			}
		})
	}
}

func TestBuildDomainConfig_NilLogger(t *testing.T) {
	// Must not panic when logger is nil.
	cfg := config.UpstreamHealthConfig{Enabled: false}
	_, enabled := BuildDomainConfig(cfg, nil)
	if enabled {
		t.Error("expected enabled=false for disabled config")
	}

	cfg2 := config.UpstreamHealthConfig{Enabled: true, Interval: "bad"}
	domainCfg, enabled2 := BuildDomainConfig(cfg2, nil)
	if !enabled2 {
		t.Error("expected enabled=true for enabled config")
	}
	if domainCfg.Interval != 5*time.Second {
		t.Errorf("Interval = %v, want 5s", domainCfg.Interval)
	}
}
