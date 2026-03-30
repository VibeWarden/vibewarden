package ops_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vibewarden/vibewarden/internal/app/ops"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// fakePortChecker is a test double for ports.PortChecker.
type fakePortChecker struct {
	// available maps port → available
	available map[int]bool
}

func (f *fakePortChecker) IsPortAvailable(_ context.Context, _ string, port int) (bool, error) {
	if v, ok := f.available[port]; ok {
		return v, nil
	}
	return true, nil
}

// reachableHealthChecker is a fakeHealthChecker that reports upstream as reachable.
func reachableHealthChecker() *fakeHealthChecker {
	return &fakeHealthChecker{
		responses: map[string]healthResponse{
			"http://127.0.0.1:3000": {ok: true, statusCode: 200},
		},
	}
}

// noContainersCompose returns a fakeCompose whose PS returns an empty slice.
func noContainersCompose() *fakeCompose {
	return &fakeCompose{
		versionStr: "Docker Compose version v2.35.1",
		psResult:   nil,
	}
}

// healthyContainersCompose returns a fakeCompose with one healthy running container.
func healthyContainersCompose() *fakeCompose {
	return &fakeCompose{
		versionStr: "Docker Compose version v2.35.1",
		psResult: []ports.ContainerInfo{
			{Name: "vibewarden-proxy-1", Service: "proxy", State: "running", Health: "healthy"},
		},
	}
}

// defaultOpts returns a DoctorOptions with a temp workDir so generated-file checks
// do not depend on the actual filesystem.
func defaultOpts(t *testing.T) ops.DoctorOptions {
	t.Helper()
	return ops.DoctorOptions{
		ConfigPath: "vibewarden.yaml",
		WorkDir:    t.TempDir(), // no generated files present by default
	}
}

// optsWithGeneratedFile creates a DoctorOptions whose workDir contains the
// expected generated docker-compose.yml so that check passes.
func optsWithGeneratedFile(t *testing.T) ops.DoctorOptions {
	t.Helper()
	dir := t.TempDir()
	genDir := filepath.Join(dir, ".vibewarden", "generated")
	if err := os.MkdirAll(genDir, 0o755); err != nil {
		t.Fatalf("create generated dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(genDir, "docker-compose.yml"), []byte("version: '3'"), 0o644); err != nil {
		t.Fatalf("create docker-compose.yml: %v", err)
	}
	return ops.DoctorOptions{
		ConfigPath: "vibewarden.yaml",
		WorkDir:    dir,
	}
}

func TestDoctorService_Run_AllPassing(t *testing.T) {
	fc := healthyContainersCompose()
	pc := &fakePortChecker{available: map[int]bool{8443: true}}
	hc := reachableHealthChecker()

	svc := ops.NewDoctorService(fc, pc, hc)
	cfg := defaultConfig()
	var buf bytes.Buffer

	opts := optsWithGeneratedFile(t)
	allOK, err := svc.Run(context.Background(), cfg, opts, &buf)
	if err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}
	if !allOK {
		t.Error("expected allOK = true when all checks pass")
	}

	out := buf.String()
	for _, want := range []string{
		"VibeWarden Doctor",
		"Docker daemon",
		"Docker Compose",
		"Config file",
		"Proxy port",
		"Generated files",
		"Container health",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\ngot:\n%s", want, out)
		}
	}
}

func TestDoctorService_Run_DockerNotRunning(t *testing.T) {
	fc := &fakeCompose{
		infoErr:    errors.New("docker daemon not running"),
		versionStr: "Docker Compose version v2.35.1",
	}
	pc := &fakePortChecker{}
	hc := reachableHealthChecker()
	svc := ops.NewDoctorService(fc, pc, hc)
	cfg := defaultConfig()
	var buf bytes.Buffer

	allOK, err := svc.Run(context.Background(), cfg, defaultOpts(t), &buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if allOK {
		t.Error("expected allOK = false when docker daemon is not running")
	}

	out := buf.String()
	if !strings.Contains(out, "not running") {
		t.Errorf("expected 'not running' in output, got:\n%s", out)
	}
}

func TestDoctorService_Run_DockerComposeNotAvailable(t *testing.T) {
	fc := &fakeCompose{
		infoErr:    nil,
		versionErr: errors.New("docker compose: command not found"),
	}
	pc := &fakePortChecker{}
	hc := reachableHealthChecker()
	svc := ops.NewDoctorService(fc, pc, hc)
	cfg := defaultConfig()
	var buf bytes.Buffer

	allOK, err := svc.Run(context.Background(), cfg, defaultOpts(t), &buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if allOK {
		t.Error("expected allOK = false when docker compose is not available")
	}

	out := buf.String()
	if !strings.Contains(out, "not available") {
		t.Errorf("expected 'not available' in output, got:\n%s", out)
	}
}

func TestDoctorService_Run_PortInUse(t *testing.T) {
	fc := noContainersCompose()
	pc := &fakePortChecker{available: map[int]bool{8443: false}}
	hc := reachableHealthChecker()
	svc := ops.NewDoctorService(fc, pc, hc)
	cfg := defaultConfig()
	var buf bytes.Buffer

	allOK, err := svc.Run(context.Background(), cfg, defaultOpts(t), &buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if allOK {
		t.Error("expected allOK = false when proxy port is in use")
	}

	out := buf.String()
	if !strings.Contains(out, "already in use") {
		t.Errorf("expected 'already in use' in output, got:\n%s", out)
	}
}

func TestDoctorService_Run_ConfigPathInOutput(t *testing.T) {
	fc := noContainersCompose()
	pc := &fakePortChecker{}
	hc := reachableHealthChecker()
	svc := ops.NewDoctorService(fc, pc, hc)
	cfg := defaultConfig()
	var buf bytes.Buffer

	opts := defaultOpts(t)
	opts.ConfigPath = "custom.yaml"
	_, err := svc.Run(context.Background(), cfg, opts, &buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "custom.yaml") {
		t.Errorf("expected config path in output, got:\n%s", out)
	}
}

func TestDoctorService_ChecksAreIndependent(t *testing.T) {
	// All checks fail — report should still contain all check names.
	fc := &fakeCompose{
		infoErr:    errors.New("docker not running"),
		versionErr: errors.New("compose not found"),
		psErr:      errors.New("ps failed"),
	}
	pc := &fakePortChecker{available: map[int]bool{8443: false}}
	hc := reachableHealthChecker()
	svc := ops.NewDoctorService(fc, pc, hc)
	cfg := defaultConfig()
	var buf bytes.Buffer

	_, err := svc.Run(context.Background(), cfg, defaultOpts(t), &buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := buf.String()
	for _, want := range []string{
		"Docker daemon",
		"Docker Compose",
		"Config file",
		"Proxy port",
		"Generated files",
		"Container health",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("check %q missing from output\ngot:\n%s", want, out)
		}
	}
}

func TestDoctorService_Run_GeneratedFileMissing_IsWarn(t *testing.T) {
	fc := noContainersCompose()
	pc := &fakePortChecker{available: map[int]bool{8443: true}}
	hc := reachableHealthChecker()
	svc := ops.NewDoctorService(fc, pc, hc)
	cfg := defaultConfig()
	var buf bytes.Buffer

	// defaultOpts uses an empty temp dir — no generated file present.
	allOK, err := svc.Run(context.Background(), cfg, defaultOpts(t), &buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// WARN does not cause allOK=false.
	if !allOK {
		t.Error("expected allOK = true because generated-file absence is WARN, not FAIL")
	}

	out := buf.String()
	if !strings.Contains(out, "[WARN]") {
		t.Errorf("expected [WARN] badge in output, got:\n%s", out)
	}
	if !strings.Contains(out, "Generated files") {
		t.Errorf("expected 'Generated files' check name in output, got:\n%s", out)
	}
}

func TestDoctorService_Run_UnhealthyContainer_IsFail(t *testing.T) {
	fc := &fakeCompose{
		versionStr: "Docker Compose version v2.35.1",
		psResult: []ports.ContainerInfo{
			{Name: "vibewarden-proxy-1", Service: "proxy", State: "running", Health: "unhealthy"},
		},
	}
	pc := &fakePortChecker{available: map[int]bool{8443: true}}
	hc := reachableHealthChecker()
	svc := ops.NewDoctorService(fc, pc, hc)
	cfg := defaultConfig()
	var buf bytes.Buffer

	allOK, err := svc.Run(context.Background(), cfg, optsWithGeneratedFile(t), &buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if allOK {
		t.Error("expected allOK = false when a container is unhealthy")
	}

	out := buf.String()
	if !strings.Contains(out, "unhealthy") {
		t.Errorf("expected 'unhealthy' in output, got:\n%s", out)
	}
}

func TestDoctorService_Run_JSONOutput(t *testing.T) {
	fc := noContainersCompose()
	pc := &fakePortChecker{available: map[int]bool{8443: true}}
	hc := reachableHealthChecker()
	svc := ops.NewDoctorService(fc, pc, hc)
	cfg := defaultConfig()
	var buf bytes.Buffer

	opts := optsWithGeneratedFile(t)
	opts.JSON = true
	_, err := svc.Run(context.Background(), cfg, opts, &buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var results []ops.CheckResult
	if err := json.Unmarshal(buf.Bytes(), &results); err != nil {
		t.Fatalf("output is not valid JSON: %v\ngot:\n%s", err, buf.String())
	}
	if len(results) == 0 {
		t.Error("expected at least one check result in JSON output")
	}
	for _, r := range results {
		if r.Name == "" {
			t.Errorf("check result has empty name: %+v", r)
		}
		if r.Severity == "" {
			t.Errorf("check result has empty severity: %+v", r)
		}
	}
}

func TestDoctorService_Run_OKFAILBadgesInOutput(t *testing.T) {
	// Docker daemon failure should produce a [FAIL] badge in human output.
	fc := &fakeCompose{
		infoErr:    errors.New("docker not running"),
		versionStr: "Docker Compose version v2.35.1",
	}
	pc := &fakePortChecker{available: map[int]bool{8443: true}}
	hc := reachableHealthChecker()
	svc := ops.NewDoctorService(fc, pc, hc)
	cfg := defaultConfig()
	var buf bytes.Buffer

	_, err := svc.Run(context.Background(), cfg, defaultOpts(t), &buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "[FAIL]") {
		t.Errorf("expected [FAIL] badge in output, got:\n%s", out)
	}
	if !strings.Contains(out, "[OK]") {
		t.Errorf("expected [OK] badge in output, got:\n%s", out)
	}
}

func TestDoctorService_Run_ContainersHealthy_AllOK(t *testing.T) {
	fc := healthyContainersCompose()
	pc := &fakePortChecker{available: map[int]bool{8443: true}}
	hc := reachableHealthChecker()
	svc := ops.NewDoctorService(fc, pc, hc)
	cfg := defaultConfig()
	var buf bytes.Buffer

	opts := optsWithGeneratedFile(t)
	allOK, err := svc.Run(context.Background(), cfg, opts, &buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !allOK {
		t.Errorf("expected allOK = true when containers are healthy\noutput:\n%s", buf.String())
	}

	out := buf.String()
	if !strings.Contains(out, "Container health") {
		t.Errorf("expected 'Container health' check in output, got:\n%s", out)
	}
	if !strings.Contains(out, "running") {
		t.Errorf("expected 'running' in container health detail, got:\n%s", out)
	}
}
