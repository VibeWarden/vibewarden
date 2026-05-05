package config_test

import (
	"path/filepath"
	"runtime"
	"testing"

	"github.com/vibewarden/vibewarden/internal/config"
)

// TestReferenceYAML_UnmarshalsCleanly loads vibewarden.reference.yaml via the
// same code path used at runtime (LoadRaw) and verifies that:
//
//  1. The file is valid YAML — no parse errors.
//  2. It unmarshals cleanly into Config — no unknown-key or type-mismatch errors.
//  3. The resulting Config is non-zero — the loader actually read meaningful fields.
//
// This is a regression test for https://github.com/vibewarden/vibewarden/issues/1270.
// It catches malformed YAML and fields in the reference file that no longer map to
// a Config struct field (mapstructure tag mismatch).
func TestReferenceYAML_UnmarshalsCleanly(t *testing.T) {
	// Locate the repository root from this file's path.
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed — cannot determine repository root")
	}
	// internal/config/reference_yaml_test.go → ../../vibewarden.reference.yaml
	repoRoot := filepath.Join(filepath.Dir(thisFile), "..", "..")
	refYAML := filepath.Join(repoRoot, "vibewarden.reference.yaml")

	cfg, err := config.LoadRaw(refYAML)
	if err != nil {
		t.Fatalf("LoadRaw(%q) returned error: %v", refYAML, err)
	}

	// Verify the parsed config has expected sentinel values from the file.
	// These are fields that are uncommented (live, not commented-out) in the
	// reference YAML.  If LoadRaw silently returned a zero-value Config (e.g.
	// because no file was found), these checks would catch it.
	tests := []struct {
		field string
		got   any
		want  any
	}{
		{"server.host", cfg.Server.Host, "127.0.0.1"},
		{"server.port", cfg.Server.Port, 8443},
		{"upstream.host", cfg.Upstream.Host, "127.0.0.1"},
		{"upstream.port", cfg.Upstream.Port, 3000},
		{"tls.enabled", cfg.TLS.Enabled, true},
		{"tls.provider", cfg.TLS.Provider, "self-signed"},
		{"tls.cert_monitoring.enabled", cfg.TLS.CertMonitoring.Enabled, true},
		{"tls.cert_monitoring.check_interval", cfg.TLS.CertMonitoring.CheckInterval, "6h"},
		{"tls.cert_monitoring.warning_threshold", cfg.TLS.CertMonitoring.WarningThreshold, "720h"},
		{"tls.cert_monitoring.critical_threshold", cfg.TLS.CertMonitoring.CriticalThreshold, "168h"},
		{"log.level", cfg.Log.Level, "info"},
		{"log.format", cfg.Log.Format, "json"},
		{"log.access_log", cfg.Log.AccessLog, true},
		{"rate_limit.enabled", cfg.RateLimit.Enabled, true},
		{"waf.enabled", cfg.WAF.Enabled, true},
		{"waf.mode", cfg.WAF.Mode, "detect"},
		{"security_headers.enabled", cfg.SecurityHeaders.Enabled, true},
		{"body_size.max", cfg.BodySize.Max, "1MB"},
		{"resilience.timeout", cfg.Resilience.Timeout, "30s"},
		{"resilience.circuit_breaker.enabled", cfg.Resilience.CircuitBreaker.Enabled, false},
		{"resilience.retry.enabled", cfg.Resilience.Retry.Enabled, false},
		{"observability.enabled", cfg.Observability.Enabled, false},
		{"observability.grafana_port", cfg.Observability.GrafanaPort, 3001},
		{"audit.enabled", cfg.Audit.Enabled, true},
		{"audit.output", cfg.Audit.Output, "stdout"},
		{"input_validation.enabled", cfg.InputValidation.Enabled, false},
		{"input_validation.max_url_length", cfg.InputValidation.MaxURLLength, 2048},
		{"error_pages.enabled", cfg.ErrorPages.Enabled, false},
		{"maintenance.enabled", cfg.Maintenance.Enabled, false},
		{"compression.enabled", cfg.Compression.Enabled, true},
		{"watch.enabled", cfg.Watch.Enabled, true},
		{"watch.debounce", cfg.Watch.Debounce, "500ms"},
		{"egress.enabled", cfg.Egress.Enabled, false},
		{"cors.enabled", cfg.CORS.Enabled, false},
		{"telemetry.enabled", cfg.Telemetry.Enabled, true},
		{"secrets.enabled", cfg.Secrets.Enabled, false},
		{"deploy.target_platform", cfg.Deploy.TargetPlatform, "linux/amd64"},
	}

	for _, tt := range tests {
		t.Run(tt.field, func(t *testing.T) {
			if tt.got != tt.want {
				t.Errorf("cfg.%s = %v, want %v", tt.field, tt.got, tt.want)
			}
		})
	}
}
