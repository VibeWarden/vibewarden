package probe_test

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vibewarden/vibewarden/internal/app/probe"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// updateGolden controls whether golden files are regenerated. Run with:
//
//	UPDATE_GOLDEN=1 go test ./internal/app/probe/...
var updateGolden = os.Getenv("UPDATE_GOLDEN") == "1"

func goldenPath(t *testing.T, name string) string {
	t.Helper()
	return filepath.Join("testdata", name+".golden")
}

func runGolden(t *testing.T, name string, result probe.Result, err error) {
	t.Helper()
	var buf bytes.Buffer
	probe.Render(&buf, result, err)
	got := buf.String()

	path := goldenPath(t, name)
	if updateGolden {
		if err2 := os.WriteFile(path, []byte(got), 0o644); err2 != nil {
			t.Fatalf("writing golden %s: %v", path, err2)
		}
		t.Logf("updated golden: %s", path)
		return
	}

	want, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatalf("reading golden %s: %v — run UPDATE_GOLDEN=1 go test to regenerate", path, readErr)
	}
	if got != string(want) {
		t.Errorf("golden mismatch for %q:\ngot:\n%s\nwant:\n%s", name, got, string(want))
	}
}

func TestRender_DevOK(t *testing.T) {
	result := probe.Result{
		URL:     "https://localhost:8443/_vibewarden/health",
		EnvName: "",
		Doc: ports.HealthDocument{
			Status:  "ok",
			Version: "0.18.4",
			Components: map[string]string{
				"sidecar":  "ok",
				"upstream": "ok",
			},
		},
	}
	runGolden(t, "dev_ok", result, nil)
}

func TestRender_EnvOK(t *testing.T) {
	result := probe.Result{
		URL:     "https://app.example.com/_vibewarden/health",
		EnvName: "production",
		Doc: ports.HealthDocument{
			Status:  "ok",
			Version: "0.18.4",
			Components: map[string]string{
				"sidecar":  "ok",
				"upstream": "ok",
			},
		},
	}
	runGolden(t, "env_ok", result, nil)
}

func TestRender_BootGapExhausted(t *testing.T) {
	result := probe.Result{
		URL:     "https://localhost:8443/_vibewarden/health",
		EnvName: "",
		Doc: ports.HealthDocument{
			Status:  "degraded",
			Version: "0.18.4",
			Components: map[string]string{
				"sidecar":  "ok",
				"upstream": "unknown",
			},
		},
	}
	runGolden(t, "boot_gap_exhausted", result, probe.ErrBootGapExhausted)
}

func TestRender_Refused(t *testing.T) {
	result := probe.Result{
		URL:     "https://localhost:8443/_vibewarden/health",
		EnvName: "",
	}
	runGolden(t, "refused", result, ports.ErrProbeRefused)
}

func TestRender_Non200(t *testing.T) {
	result := probe.Result{
		URL:     "https://app.example.com/_vibewarden/health",
		EnvName: "production",
	}
	err := &ports.ProbeNon200Error{StatusCode: 502, Body: "Bad Gateway"}
	runGolden(t, "non_200", result, err)
}

func TestRender_Malformed(t *testing.T) {
	result := probe.Result{
		URL:     "https://app.example.com/_vibewarden/health",
		EnvName: "production",
	}
	runGolden(t, "malformed", result, fmt.Errorf("%w: unexpected field", ports.ErrProbeMalformed))
}

// TestRender_FailingUpstream checks that a "failing" upstream renders the
// degraded block with the correct DEGRADED summary (not boot-gap message).
func TestRender_FailingUpstream(t *testing.T) {
	result := probe.Result{
		URL:     "https://localhost:8443/_vibewarden/health",
		EnvName: "",
		Doc: ports.HealthDocument{
			Status:  "degraded",
			Version: "0.18.4",
			Components: map[string]string{
				"sidecar":  "ok",
				"upstream": "failing",
			},
		},
	}
	var buf bytes.Buffer
	probe.Render(&buf, result, nil)
	got := buf.String()
	if !strings.Contains(got, "DEGRADED") {
		t.Errorf("expected DEGRADED in output, got: %q", got)
	}
	if strings.Contains(got, "boot gap") || strings.Contains(got, "converged") {
		t.Errorf("failing upstream should not show boot-gap message, got: %q", got)
	}
}

// TestRender_Non200_Unwrap verifies that errors.Is works for wrapped ErrProbeNon200.
func TestRender_Non200_Unwrap(t *testing.T) {
	ne := &ports.ProbeNon200Error{StatusCode: 404, Body: "not found"}
	if !errors.Is(ne, ports.ErrProbeNon200) {
		t.Error("ProbeNon200Error should satisfy errors.Is(err, ErrProbeNon200)")
	}
}

// TestRender_RefusedEnv checks the per-env connection-refused message listing
// production-specific causes (bundle not deployed, host down, DNS, sidecar).
func TestRender_RefusedEnv(t *testing.T) {
	result := probe.Result{
		URL:     "https://demo.example.com/_vibewarden/health",
		EnvName: "production",
	}
	runGolden(t, "refused_env", result, ports.ErrProbeRefused)
}

// TestRender_DNSFailureEnv checks the env-aware DNS-failure message pointing
// to tls.domain in the env YAML and the A/AAAA records.
func TestRender_DNSFailureEnv(t *testing.T) {
	result := probe.Result{
		URL:     "https://demo.example.com/_vibewarden/health",
		EnvName: "production",
	}
	runGolden(t, "dns_failure_env", result, ports.ErrDNSFailure)
}

// TestRender_DNSFailureDefault checks the default-mode DNS-failure message
// that flags an unexpected /etc/hosts problem (localhost always resolves).
func TestRender_DNSFailureDefault(t *testing.T) {
	result := probe.Result{
		URL:     "https://localhost:8443/_vibewarden/health",
		EnvName: "",
	}
	runGolden(t, "dns_failure_default", result, ports.ErrDNSFailure)
}

// TestRender_ErrDNSFailure_Sentinel is a smoke test that confirms ErrDNSFailure
// is a properly exported sentinel that satisfies errors.Is with itself.
func TestRender_ErrDNSFailure_Sentinel(t *testing.T) {
	if !errors.Is(ports.ErrDNSFailure, ports.ErrDNSFailure) {
		t.Error("ErrDNSFailure should satisfy errors.Is(err, ErrDNSFailure)")
	}
}

// TestRender_TLSRetryExhausted verifies the golden output for
// ErrTLSRetryExhausted with env "production" and the default 30s budget. The
// golden file is pinned at internal/app/probe/testdata/tls_retry_exhausted.golden.
func TestRender_TLSRetryExhausted(t *testing.T) {
	result := probe.Result{
		URL:            "https://demo.example.com/_vibewarden/health",
		EnvName:        "production",
		TLSRetryBudget: 30 * time.Second,
	}
	runGolden(t, "tls_retry_exhausted", result, probe.ErrTLSRetryExhausted)
}

// TestRender_TLSRetryExhausted_60s verifies that the rendered budget line
// substitutes the actual TLSRetryBudget (60s) rather than a hardcoded constant.
func TestRender_TLSRetryExhausted_60s(t *testing.T) {
	result := probe.Result{
		URL:            "https://demo.example.com/_vibewarden/health",
		EnvName:        "production",
		TLSRetryBudget: 60 * time.Second,
	}
	var buf bytes.Buffer
	probe.Render(&buf, result, probe.ErrTLSRetryExhausted)
	got := buf.String()

	if !strings.Contains(got, "60s") {
		t.Errorf("expected '60s' in output for 60s budget, got: %q", got)
	}
	if strings.Contains(got, "30s") {
		t.Errorf("expected no hardcoded '30s' in output for 60s budget, got: %q", got)
	}
}
