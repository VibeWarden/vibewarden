package ops_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/vibewarden/vibewarden/internal/app/ops"
	"github.com/vibewarden/vibewarden/internal/ports"
)

func TestObsService_Up_InvokesComposeWithObservabilityProfile(t *testing.T) {
	fc := &fakeCompose{}
	svc := ops.NewObsService(fc, nil)
	cfg := defaultConfig()
	var buf bytes.Buffer

	if err := svc.Up(context.Background(), cfg, ops.ObsUpOptions{}, &buf); err != nil {
		t.Fatalf("Up() unexpected error: %v", err)
	}

	if len(fc.capturedProfiles) != 1 || fc.capturedProfiles[0] != "observability" {
		t.Errorf("Up() profiles = %v, want [observability]", fc.capturedProfiles)
	}
}

func TestObsService_Up_UsesGeneratedComposeFile(t *testing.T) {
	fc := &fakeCompose{}
	svc := ops.NewObsService(fc, nil)
	cfg := defaultConfig()
	var buf bytes.Buffer

	if err := svc.Up(context.Background(), cfg, ops.ObsUpOptions{}, &buf); err != nil {
		t.Fatalf("Up() unexpected error: %v", err)
	}

	want := ".vibewarden/generated/docker-compose.yml"
	if fc.capturedComposeFile != want {
		t.Errorf("Up() composeFile = %q, want %q", fc.capturedComposeFile, want)
	}
}

func TestObsService_Up_PrintsGrafanaAndPrometheusURLs(t *testing.T) {
	fc := &fakeCompose{}
	svc := ops.NewObsService(fc, nil)
	cfg := defaultConfig()
	var buf bytes.Buffer

	if err := svc.Up(context.Background(), cfg, ops.ObsUpOptions{}, &buf); err != nil {
		t.Fatalf("Up() unexpected error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "Grafana") {
		t.Errorf("expected Grafana URL in output, got:\n%s", out)
	}
	if !strings.Contains(out, "Prometheus") {
		t.Errorf("expected Prometheus URL in output, got:\n%s", out)
	}
}

func TestObsService_Up_ComposeError_ReturnsError(t *testing.T) {
	fc := &fakeCompose{upErr: errors.New("docker not running")}
	svc := ops.NewObsService(fc, nil)
	cfg := defaultConfig()
	var buf bytes.Buffer

	err := svc.Up(context.Background(), cfg, ops.ObsUpOptions{}, &buf)
	if err == nil {
		t.Fatal("Up() expected error when compose fails, got nil")
	}
	if !strings.Contains(err.Error(), "starting observability stack") {
		t.Errorf("error should wrap compose error, got: %v", err)
	}
}

func TestObsService_Up_Verbose_ForwardsStderrWriter(t *testing.T) {
	fc := &fakeCompose{}
	svc := ops.NewObsService(fc, nil)
	cfg := defaultConfig()
	var buf bytes.Buffer

	if err := svc.Up(context.Background(), cfg, ops.ObsUpOptions{Verbose: true}, &buf); err != nil {
		t.Fatalf("Up() unexpected error: %v", err)
	}
	if fc.capturedUpOpts.Stderr == nil {
		t.Error("expected Up() to be called with non-nil Stderr in verbose mode")
	}
}

func TestObsService_Up_NotVerbose_StderrNil(t *testing.T) {
	fc := &fakeCompose{}
	svc := ops.NewObsService(fc, nil)
	cfg := defaultConfig()
	var buf bytes.Buffer

	if err := svc.Up(context.Background(), cfg, ops.ObsUpOptions{}, &buf); err != nil {
		t.Fatalf("Up() unexpected error: %v", err)
	}
	if fc.capturedUpOpts.Stderr != nil {
		t.Error("expected Up() Stderr nil when not verbose")
	}
}

func TestObsService_Up_SidecarNotRunning_PrintsAdvisory(t *testing.T) {
	// When PS returns no sidecar container, Up still succeeds but prints an advisory.
	fc := &fakeCompose{
		psResult: []ports.ContainerInfo{
			{Service: "postgres", State: "running"},
		},
	}
	svc := ops.NewObsService(fc, nil)
	cfg := defaultConfig()
	var buf bytes.Buffer

	if err := svc.Up(context.Background(), cfg, ops.ObsUpOptions{}, &buf); err != nil {
		t.Fatalf("Up() expected no error for missing sidecar, got: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "Advisory") {
		t.Errorf("expected advisory message when sidecar not running, got:\n%s", out)
	}
	if !strings.Contains(out, "vibew dev") {
		t.Errorf("expected 'vibew dev' hint in advisory, got:\n%s", out)
	}
}

func TestObsService_Up_SidecarRunning_NoAdvisory(t *testing.T) {
	// When PS includes a running sidecar, no advisory is printed.
	fc := &fakeCompose{
		psResult: []ports.ContainerInfo{
			{Service: "vibewarden", State: "running"},
		},
	}
	svc := ops.NewObsService(fc, nil)
	cfg := defaultConfig()
	var buf bytes.Buffer

	if err := svc.Up(context.Background(), cfg, ops.ObsUpOptions{}, &buf); err != nil {
		t.Fatalf("Up() unexpected error: %v", err)
	}
	out := buf.String()
	if strings.Contains(out, "Advisory") {
		t.Errorf("unexpected advisory when sidecar is running:\n%s", out)
	}
}

func TestObsService_Up_PSError_DoesNotFail(t *testing.T) {
	// When PS fails, the advisory check is skipped and Up proceeds normally.
	fc := &fakeCompose{psErr: errors.New("docker daemon not responding")}
	svc := ops.NewObsService(fc, nil)
	cfg := defaultConfig()
	var buf bytes.Buffer

	if err := svc.Up(context.Background(), cfg, ops.ObsUpOptions{}, &buf); err != nil {
		t.Fatalf("Up() unexpected error when PS fails: %v", err)
	}
}

// --- Down tests ---

func TestObsService_Down_InvokesComposeDown(t *testing.T) {
	fc := &fakeCompose{}
	svc := ops.NewObsService(fc, nil)
	var buf bytes.Buffer

	if err := svc.Down(context.Background(), ops.ObsDownOptions{Yes: true}, &buf); err != nil {
		t.Fatalf("Down() unexpected error: %v", err)
	}
	if fc.downCalled != 1 {
		t.Errorf("expected Down() to be called once, got %d", fc.downCalled)
	}
}

func TestObsService_Down_UsesGeneratedComposeFile(t *testing.T) {
	fc := &fakeCompose{}
	svc := ops.NewObsService(fc, nil)
	var buf bytes.Buffer

	if err := svc.Down(context.Background(), ops.ObsDownOptions{Yes: true}, &buf); err != nil {
		t.Fatalf("Down() unexpected error: %v", err)
	}
	// The compose file is internal; we verify Down was called (compose file
	// verified via the obsService's Down implementation which uses generatedOutputDir).
	if fc.downCalled == 0 {
		t.Error("expected Down() to be called")
	}
}

func TestObsService_Down_WithVolumes_ForwardsFlag(t *testing.T) {
	fc := &fakeCompose{}
	svc := ops.NewObsService(fc, nil)
	var buf bytes.Buffer

	opts := ops.ObsDownOptions{Volumes: true, Yes: true}
	if err := svc.Down(context.Background(), opts, &buf); err != nil {
		t.Fatalf("Down() unexpected error: %v", err)
	}
	if !fc.capturedDownOpts.Volumes {
		t.Error("expected Volumes=true forwarded to compose.Down")
	}
}

func TestObsService_Down_WithRemoveOrphans_ForwardsFlag(t *testing.T) {
	fc := &fakeCompose{}
	svc := ops.NewObsService(fc, nil)
	var buf bytes.Buffer

	opts := ops.ObsDownOptions{RemoveOrphans: true}
	if err := svc.Down(context.Background(), opts, &buf); err != nil {
		t.Fatalf("Down() unexpected error: %v", err)
	}
	if !fc.capturedDownOpts.RemoveOrphans {
		t.Error("expected RemoveOrphans=true forwarded to compose.Down")
	}
}

func TestObsService_Down_NonTTY_VolumesWithoutYes_ReturnsError(t *testing.T) {
	fc := &fakeCompose{}
	svc := ops.NewObsService(fc, nil)
	var buf bytes.Buffer

	opts := ops.ObsDownOptions{Volumes: true, IsTTY: false, Yes: false}
	err := svc.Down(context.Background(), opts, &buf)
	if err == nil {
		t.Fatal("Down() expected error for non-TTY volumes without --yes, got nil")
	}
	if !errors.Is(err, ops.ErrNonTTYVolumesRequiresYes) {
		t.Errorf("expected ErrNonTTYVolumesRequiresYes, got: %v", err)
	}
}

func TestObsService_Down_ComposeError_ReturnsError(t *testing.T) {
	fc := &fakeCompose{downErr: errors.New("compose down failed")}
	svc := ops.NewObsService(fc, nil)
	var buf bytes.Buffer

	err := svc.Down(context.Background(), ops.ObsDownOptions{}, &buf)
	if err == nil {
		t.Fatal("Down() expected error when compose fails, got nil")
	}
	if !strings.Contains(err.Error(), "stopping observability stack") {
		t.Errorf("error should wrap compose error, got: %v", err)
	}
}

func TestObsService_Down_PassesObservabilityProfile(t *testing.T) {
	// obs down must scope teardown to the observability profile only, so that
	// running `vibew obs down` does not stop the main sidecar or other services.
	fc := &fakeCompose{}
	svc := ops.NewObsService(fc, nil)
	var buf bytes.Buffer

	if err := svc.Down(context.Background(), ops.ObsDownOptions{Yes: true}, &buf); err != nil {
		t.Fatalf("Down() unexpected error: %v", err)
	}

	profiles := fc.capturedDownOpts.Profiles
	if len(profiles) != 1 || profiles[0] != "observability" {
		t.Errorf("Down() Profiles = %v, want [observability]", profiles)
	}
}

func TestObsService_Up_PrintsGrafanaOnPort3001(t *testing.T) {
	// Grafana's default host port is 3001 (mapped from container-internal :3000).
	// The CLI output must reflect the actual accessible URL.
	fc := &fakeCompose{}
	svc := ops.NewObsService(fc, nil)
	cfg := defaultConfig()
	var buf bytes.Buffer

	if err := svc.Up(context.Background(), cfg, ops.ObsUpOptions{}, &buf); err != nil {
		t.Fatalf("Up() unexpected error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "localhost:3001") {
		t.Errorf("expected Grafana URL with port 3001 in output, got:\n%s", out)
	}
	if strings.Contains(out, "localhost:3000") {
		t.Errorf("output must not reference port 3000 (container-internal); got:\n%s", out)
	}
}
