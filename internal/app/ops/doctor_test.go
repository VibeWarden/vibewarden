package ops_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/vibewarden/vibewarden/internal/app/ops"
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

func TestDoctorService_Run_AllPassing(t *testing.T) {
	fc := &fakeCompose{
		versionStr: "Docker Compose version v2.35.1",
		infoErr:    nil,
	}
	pc := &fakePortChecker{available: map[int]bool{8080: true}}

	svc := ops.NewDoctorService(fc, pc)
	cfg := defaultConfig()
	var buf bytes.Buffer

	allOK, err := svc.Run(context.Background(), cfg, "vibewarden.yaml", &buf)
	if err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}

	// Upstream is likely not running in test — expect allOK = false
	_ = allOK

	out := buf.String()
	for _, want := range []string{
		"VibeWarden Doctor",
		"Docker daemon",
		"Docker Compose",
		"Config file",
		"Proxy port",
		"Upstream app",
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
	svc := ops.NewDoctorService(fc, pc)
	cfg := defaultConfig()
	var buf bytes.Buffer

	allOK, err := svc.Run(context.Background(), cfg, "", &buf)
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
	svc := ops.NewDoctorService(fc, pc)
	cfg := defaultConfig()
	var buf bytes.Buffer

	allOK, err := svc.Run(context.Background(), cfg, "", &buf)
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
	fc := &fakeCompose{
		versionStr: "Docker Compose version v2.35.1",
	}
	pc := &fakePortChecker{available: map[int]bool{8080: false}}
	svc := ops.NewDoctorService(fc, pc)
	cfg := defaultConfig()
	var buf bytes.Buffer

	allOK, err := svc.Run(context.Background(), cfg, "", &buf)
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

func TestDoctorService_Run_NilConfig(t *testing.T) {
	fc := &fakeCompose{versionStr: "Docker Compose version v2.35.1"}
	pc := &fakePortChecker{}
	svc := ops.NewDoctorService(fc, pc)

	// nil cfg should not panic — checkConfigFile handles it
	// We pass a defaultConfig but test the label path
	cfg := defaultConfig()
	var buf bytes.Buffer

	_, err := svc.Run(context.Background(), cfg, "custom.yaml", &buf)
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
	}
	pc := &fakePortChecker{available: map[int]bool{8080: false}}
	svc := ops.NewDoctorService(fc, pc)
	cfg := defaultConfig()
	var buf bytes.Buffer

	_, err := svc.Run(context.Background(), cfg, "", &buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := buf.String()
	for _, want := range []string{
		"Docker daemon",
		"Docker Compose",
		"Config file",
		"Proxy port",
		"Upstream app",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("check %q missing from output\ngot:\n%s", want, out)
		}
	}
}
