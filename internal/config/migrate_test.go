package config_test

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"github.com/vibewarden/vibewarden/internal/config"
)

// captureLogger returns a logger that writes all output to a buffer so tests
// can assert on log messages without any output noise.
func captureLogger(buf *bytes.Buffer) *slog.Logger {
	return slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

func TestMigrateLegacyMetrics_DefaultMetrics_NoMigration(t *testing.T) {
	var logBuf bytes.Buffer
	logger := captureLogger(&logBuf)

	cfg := &config.Config{
		Metrics: config.MetricsConfig{
			Enabled:      true,
			PathPatterns: []string{},
		},
		Telemetry: config.TelemetryConfig{
			Enabled: true,
			Prometheus: config.PrometheusExporterConfig{
				Enabled: true,
			},
		},
	}

	config.MigrateLegacyMetrics(cfg, logger)

	// No deprecation warning should have been logged.
	if strings.Contains(logBuf.String(), "DEPRECATED") {
		t.Error("MigrateLegacyMetrics logged deprecation warning for default config, want silent")
	}
}

func TestMigrateLegacyMetrics_MetricsDisabled_MigratesAndWarns(t *testing.T) {
	var logBuf bytes.Buffer
	logger := captureLogger(&logBuf)

	cfg := &config.Config{
		Metrics: config.MetricsConfig{
			Enabled:      false,
			PathPatterns: []string{},
		},
	}

	config.MigrateLegacyMetrics(cfg, logger)

	if cfg.Telemetry.Enabled != false {
		t.Errorf("Telemetry.Enabled = %v, want false", cfg.Telemetry.Enabled)
	}
	if cfg.Telemetry.Prometheus.Enabled != false {
		t.Errorf("Telemetry.Prometheus.Enabled = %v, want false", cfg.Telemetry.Prometheus.Enabled)
	}
	if !strings.Contains(logBuf.String(), "DEPRECATED") {
		t.Error("MigrateLegacyMetrics should log a deprecation warning when metrics is disabled")
	}
}

func TestMigrateLegacyMetrics_WithPathPatterns_MigratesAndWarns(t *testing.T) {
	var logBuf bytes.Buffer
	logger := captureLogger(&logBuf)

	patterns := []string{"/users/:id", "/api/v1/items/:item_id"}
	cfg := &config.Config{
		Metrics: config.MetricsConfig{
			Enabled:      true,
			PathPatterns: patterns,
		},
	}

	config.MigrateLegacyMetrics(cfg, logger)

	if len(cfg.Telemetry.PathPatterns) != len(patterns) {
		t.Errorf("Telemetry.PathPatterns len = %d, want %d", len(cfg.Telemetry.PathPatterns), len(patterns))
	}
	for i, p := range patterns {
		if cfg.Telemetry.PathPatterns[i] != p {
			t.Errorf("PathPatterns[%d] = %q, want %q", i, cfg.Telemetry.PathPatterns[i], p)
		}
	}
	if cfg.Telemetry.Enabled != true {
		t.Errorf("Telemetry.Enabled = %v, want true", cfg.Telemetry.Enabled)
	}
	if cfg.Telemetry.Prometheus.Enabled != true {
		t.Errorf("Telemetry.Prometheus.Enabled = %v, want true", cfg.Telemetry.Prometheus.Enabled)
	}
	if !strings.Contains(logBuf.String(), "DEPRECATED") {
		t.Error("MigrateLegacyMetrics should log deprecation warning when path_patterns are set")
	}
}

func TestMigrateLegacyMetrics_Idempotent(t *testing.T) {
	var logBuf bytes.Buffer
	logger := captureLogger(&logBuf)

	cfg := &config.Config{
		Metrics: config.MetricsConfig{
			Enabled:      false,
			PathPatterns: []string{"/api/:id"},
		},
	}

	config.MigrateLegacyMetrics(cfg, logger)
	config.MigrateLegacyMetrics(cfg, logger)

	// Calling twice should produce the same result (two warnings in log, but same config state).
	if cfg.Telemetry.Enabled != false {
		t.Errorf("Telemetry.Enabled = %v after second call, want false", cfg.Telemetry.Enabled)
	}
}

func TestMigrateLegacyMetrics_TableDriven(t *testing.T) {
	tests := []struct {
		name                   string
		metrics                config.MetricsConfig
		wantDeprecationWarning bool
		wantTelemetryEnabled   bool
		wantPromEnabled        bool
		wantPathPatterns       []string
	}{
		{
			name: "default config (enabled, no patterns)",
			metrics: config.MetricsConfig{
				Enabled:      true,
				PathPatterns: nil,
			},
			wantDeprecationWarning: false,
			// Telemetry stays at zero value — no migration.
		},
		{
			name: "metrics disabled",
			metrics: config.MetricsConfig{
				Enabled:      false,
				PathPatterns: nil,
			},
			wantDeprecationWarning: true,
			wantTelemetryEnabled:   false,
			wantPromEnabled:        false,
		},
		{
			name: "metrics with path patterns",
			metrics: config.MetricsConfig{
				Enabled:      true,
				PathPatterns: []string{"/users/:id"},
			},
			wantDeprecationWarning: true,
			wantTelemetryEnabled:   true,
			wantPromEnabled:        true,
			wantPathPatterns:       []string{"/users/:id"},
		},
		{
			name: "metrics disabled with path patterns",
			metrics: config.MetricsConfig{
				Enabled:      false,
				PathPatterns: []string{"/api/:version/items"},
			},
			wantDeprecationWarning: true,
			wantTelemetryEnabled:   false,
			wantPromEnabled:        false,
			wantPathPatterns:       []string{"/api/:version/items"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var logBuf bytes.Buffer
			logger := captureLogger(&logBuf)

			cfg := &config.Config{Metrics: tt.metrics}
			config.MigrateLegacyMetrics(cfg, logger)

			hasWarning := strings.Contains(logBuf.String(), "DEPRECATED")
			if hasWarning != tt.wantDeprecationWarning {
				t.Errorf("deprecation warning present = %v, want %v (log: %s)", hasWarning, tt.wantDeprecationWarning, logBuf.String())
			}

			if !tt.wantDeprecationWarning {
				return // no migration expected, skip field checks
			}

			if cfg.Telemetry.Enabled != tt.wantTelemetryEnabled {
				t.Errorf("Telemetry.Enabled = %v, want %v", cfg.Telemetry.Enabled, tt.wantTelemetryEnabled)
			}
			if cfg.Telemetry.Prometheus.Enabled != tt.wantPromEnabled {
				t.Errorf("Telemetry.Prometheus.Enabled = %v, want %v", cfg.Telemetry.Prometheus.Enabled, tt.wantPromEnabled)
			}
			if len(cfg.Telemetry.PathPatterns) != len(tt.wantPathPatterns) {
				t.Errorf("PathPatterns len = %d, want %d", len(cfg.Telemetry.PathPatterns), len(tt.wantPathPatterns))
			}
		})
	}
}

// TestWarnInsecureDatabase verifies that WarnInsecureDatabase emits the expected
// advisory log messages and stays silent when the configuration is secure.
func TestWarnInsecureDatabase(t *testing.T) {
	tests := []struct {
		name        string
		cfg         config.DatabaseConfig
		wantWarning bool
		wantContain string
	}{
		{
			name:        "no external URL, no tls_mode — silent",
			cfg:         config.DatabaseConfig{},
			wantWarning: false,
		},
		{
			name: "tls_mode=require — silent",
			cfg: config.DatabaseConfig{
				TLSMode: "require",
			},
			wantWarning: false,
		},
		{
			name: "tls_mode=disable — warns",
			cfg: config.DatabaseConfig{
				TLSMode: "disable",
			},
			wantWarning: true,
			wantContain: "database.tls_mode",
		},
		{
			name: "external_url with sslmode=require — silent",
			cfg: config.DatabaseConfig{
				ExternalURL: "postgres://user:pass@db.example.com:5432/kratos?sslmode=require",
			},
			wantWarning: false,
		},
		{
			name: "external_url with sslmode=disable — warns",
			cfg: config.DatabaseConfig{
				ExternalURL: "postgres://user:pass@db.example.com:5432/kratos?sslmode=disable",
			},
			wantWarning: true,
			wantContain: "sslmode=disable",
		},
		{
			name: "external_url without sslmode param — silent",
			cfg: config.DatabaseConfig{
				ExternalURL: "postgres://user:pass@db.example.com:5432/kratos",
			},
			wantWarning: false,
		},
		{
			name: "tls_mode=disable AND external_url sslmode=disable — warns once per condition",
			cfg: config.DatabaseConfig{
				TLSMode:     "disable",
				ExternalURL: "postgres://user:pass@db.example.com:5432/kratos?sslmode=disable",
			},
			wantWarning: true,
			wantContain: "disable",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var logBuf bytes.Buffer
			logger := captureLogger(&logBuf)

			cfg := &config.Config{Database: tt.cfg}
			config.WarnInsecureDatabase(cfg, logger)

			hasWarning := logBuf.Len() > 0
			if hasWarning != tt.wantWarning {
				t.Errorf("warning emitted = %v, want %v (log: %q)", hasWarning, tt.wantWarning, logBuf.String())
			}
			if tt.wantWarning && tt.wantContain != "" {
				if !strings.Contains(logBuf.String(), tt.wantContain) {
					t.Errorf("log output %q does not contain %q", logBuf.String(), tt.wantContain)
				}
			}
		})
	}
}
