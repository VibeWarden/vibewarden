package config_test

import (
	"os"
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
		{"health.expose_version", cfg.Health.ExposeVersion, true},
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

// TestReferenceYAML_PassesStrictLoader loads vibewarden.reference.yaml through
// LoadStrict — the same path `vibew validate` and `vibew bundle` use — and
// requires it to be accepted.
//
// TestReferenceYAML_UnmarshalsCleanly above uses LoadRaw, which tolerates
// unknown keys (ADR-065 keeps ErrorUnused=false on the runtime path). That let
// the reference file document `kratos.social_providers`, a key that does not
// exist on any struct — the field lives on AuthConfig as auth.social_providers.
// Anyone who copied that block out of the "documents EVERY supported option"
// file got a config that `vibew validate` rejected as an unknown key (#1439).
func TestReferenceYAML_PassesStrictLoader(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed — cannot determine repository root")
	}
	repoRoot := filepath.Join(filepath.Dir(thisFile), "..", "..")
	refYAML := filepath.Join(repoRoot, "vibewarden.reference.yaml")

	if _, err := config.LoadStrict(refYAML, ""); err != nil {
		t.Fatalf("LoadStrict(%q) returned error: %v\n\n"+
			"Every key in vibewarden.reference.yaml must map to a Config struct field — "+
			"the file is advertised as documenting every supported option, so a key the "+
			"strict loader rejects is a config an operator cannot actually use.", refYAML, err)
	}
}

// TestSetDefaults_EmptyYAML verifies that setDefaults() registers values that
// a user with an empty vibewarden.yaml (no relevant section at all) would
// receive. This catches "default-lie" bugs where the reference YAML documents
// a non-zero default but setDefaults() never registers it — causing users who
// omit the section to get the Go zero-value instead.
//
// Previously, audit.enabled and all resilience.circuit_breaker.* /
// resilience.retry.* fields were undeclared in setDefaults(), meaning a user
// without an audit: or resilience: block got audit disabled and numeric
// thresholds of 0. This test pins the fix.
func TestSetDefaults_EmptyYAML(t *testing.T) {
	dir := t.TempDir()
	emptyYAML := filepath.Join(dir, "vibewarden.yaml")
	if err := os.WriteFile(emptyYAML, []byte("# empty\n"), 0600); err != nil {
		t.Fatalf("writing empty yaml: %v", err)
	}

	cfg, err := config.LoadRaw(emptyYAML)
	if err != nil {
		t.Fatalf("LoadRaw(empty yaml) error: %v", err)
	}

	tests := []struct {
		field string
		got   any
		want  any
	}{
		// audit defaults — safety-critical: audit must be ON by default.
		{"audit.enabled", cfg.Audit.Enabled, true},
		{"audit.output", cfg.Audit.Output, "stdout"},

		// resilience.circuit_breaker defaults.
		{"resilience.circuit_breaker.enabled", cfg.Resilience.CircuitBreaker.Enabled, false},
		{"resilience.circuit_breaker.threshold", cfg.Resilience.CircuitBreaker.Threshold, 5},
		{"resilience.circuit_breaker.timeout", cfg.Resilience.CircuitBreaker.Timeout, "60s"},

		// resilience.retry defaults.
		{"resilience.retry.enabled", cfg.Resilience.Retry.Enabled, false},
		{"resilience.retry.max_attempts", cfg.Resilience.Retry.MaxAttempts, 3},
		{"resilience.retry.backoff", cfg.Resilience.Retry.InitialBackoff, "100ms"},
		{"resilience.retry.max_backoff", cfg.Resilience.Retry.MaxBackoff, "10s"},

		// health.expose_version default — must be true so existing deployments
		// continue to expose the version (backward-compatible default).
		{"health.expose_version", cfg.Health.ExposeVersion, true},

		// telemetry.traces default — must be false so tracing is opt-in.
		{"telemetry.traces.enabled", cfg.Telemetry.Traces.Enabled, false},
	}

	for _, tt := range tests {
		t.Run(tt.field, func(t *testing.T) {
			if tt.got != tt.want {
				t.Errorf("empty-yaml cfg.%s = %v, want %v (setDefaults() not registering this key?)",
					tt.field, tt.got, tt.want)
			}
		})
	}
}
