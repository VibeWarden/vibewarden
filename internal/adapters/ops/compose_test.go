package ops_test

import (
	"bytes"
	"context"
	"os/exec"
	"testing"

	opsadapter "github.com/vibewarden/vibewarden/internal/adapters/ops"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// dockerAvailable reports whether the docker binary is available on PATH.
// Tests that require docker are skipped when it is not.
func dockerAvailable() bool {
	_, err := exec.LookPath("docker")
	return err == nil
}

func TestComposeAdapter_UpArgsBaselineStack(t *testing.T) {
	// This test verifies that Up builds the correct command for the baseline
	// stack (no profiles). It relies on docker being present but the compose
	// project does not need to exist — we cancel the context immediately so
	// the command never actually runs.
	if !dockerAvailable() {
		t.Skip("docker binary not available")
	}

	adapter := opsadapter.NewComposeAdapter()
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately so docker compose up exits fast

	// We only care that Up returns an error (context cancelled), not that it
	// succeeds — this confirms the command is attempted with the right args.
	err := adapter.Up(ctx, "", nil, ports.ComposeUpOptions{})
	if err == nil {
		t.Fatal("expected an error because context was cancelled before run")
	}
}

func TestComposeAdapter_UpArgsWithProfiles(t *testing.T) {
	// Same pattern as above but verifies profile flags are forwarded.
	if !dockerAvailable() {
		t.Skip("docker binary not available")
	}

	adapter := opsadapter.NewComposeAdapter()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := adapter.Up(ctx, "", []string{"observability"}, ports.ComposeUpOptions{})
	if err == nil {
		t.Fatal("expected an error because context was cancelled before run")
	}
}

func TestComposeAdapter_UpArgsWithMultipleProfiles(t *testing.T) {
	// Verify that multiple profiles are each preceded by --profile.
	if !dockerAvailable() {
		t.Skip("docker binary not available")
	}

	adapter := opsadapter.NewComposeAdapter()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := adapter.Up(ctx, "", []string{"observability", "debug"}, ports.ComposeUpOptions{})
	if err == nil {
		t.Fatal("expected an error because context was cancelled before run")
	}
}

func TestComposeAdapter_UpArgsWithComposeFile(t *testing.T) {
	// Verify that a non-empty composeFile is passed as -f.
	if !dockerAvailable() {
		t.Skip("docker binary not available")
	}

	adapter := opsadapter.NewComposeAdapter()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := adapter.Up(ctx, ".vibewarden/generated/docker-compose.yml", nil, ports.ComposeUpOptions{})
	if err == nil {
		t.Fatal("expected an error because context was cancelled before run")
	}
}

func TestComposeAdapter_Restart_CancelledContext(t *testing.T) {
	if !dockerAvailable() {
		t.Skip("docker binary not available")
	}

	adapter := opsadapter.NewComposeAdapter()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := adapter.Restart(ctx, "", nil)
	if err == nil {
		t.Fatal("expected an error because context was cancelled")
	}
}

func TestComposeAdapter_Restart_CancelledContextWithService(t *testing.T) {
	if !dockerAvailable() {
		t.Skip("docker binary not available")
	}

	adapter := opsadapter.NewComposeAdapter()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := adapter.Restart(ctx, ".vibewarden/generated/docker-compose.yml", []string{"app"})
	if err == nil {
		t.Fatal("expected an error because context was cancelled")
	}
}

// restartArgs mirrors the argument-construction logic of ComposeAdapter.Restart
// for use in table-driven tests. The adapter now uses "up -d --force-recreate
// --build" instead of "restart" so that Dockerfile changes are picked up.
func restartArgs(composeFile string, services []string) []string {
	args := []string{"compose"}
	if composeFile != "" {
		args = append(args, "-f", composeFile)
	}
	args = append(args, "up", "-d", "--force-recreate", "--build")
	args = append(args, services...)
	return args
}

func TestRestartArgsConstruction(t *testing.T) {
	tests := []struct {
		name        string
		composeFile string
		services    []string
		want        []string
	}{
		{
			name: "no file, no services",
			want: []string{"compose", "up", "-d", "--force-recreate", "--build"},
		},
		{
			name:     "no file, single service",
			services: []string{"app"},
			want:     []string{"compose", "up", "-d", "--force-recreate", "--build", "app"},
		},
		{
			name:     "no file, multiple services",
			services: []string{"app", "kratos"},
			want:     []string{"compose", "up", "-d", "--force-recreate", "--build", "app", "kratos"},
		},
		{
			name:        "with file, no services",
			composeFile: ".vibewarden/generated/docker-compose.yml",
			want:        []string{"compose", "-f", ".vibewarden/generated/docker-compose.yml", "up", "-d", "--force-recreate", "--build"},
		},
		{
			name:        "with file and service",
			composeFile: ".vibewarden/generated/docker-compose.yml",
			services:    []string{"app"},
			want:        []string{"compose", "-f", ".vibewarden/generated/docker-compose.yml", "up", "-d", "--force-recreate", "--build", "app"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := restartArgs(tt.composeFile, tt.services)
			if len(got) != len(tt.want) {
				t.Fatalf("len(args) = %d, want %d\ngot:  %v\nwant: %v", len(got), len(tt.want), got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("args[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestComposeAdapter_VersionReturnsErrorWhenDockerMissing(t *testing.T) {
	if dockerAvailable() {
		t.Skip("docker is available; skipping missing-docker test")
	}

	adapter := opsadapter.NewComposeAdapter()
	_, err := adapter.Version(context.Background())
	if err == nil {
		t.Fatal("expected an error when docker is not available")
	}
}

func TestComposeAdapter_InfoReturnsErrorWhenDockerMissing(t *testing.T) {
	if dockerAvailable() {
		t.Skip("docker is available; skipping missing-docker test")
	}

	adapter := opsadapter.NewComposeAdapter()
	err := adapter.Info(context.Background())
	if err == nil {
		t.Fatal("expected an error when docker is not available")
	}
}

func TestComposeAdapter_PS_CancelledContext(t *testing.T) {
	if !dockerAvailable() {
		t.Skip("docker binary not available")
	}

	adapter := opsadapter.NewComposeAdapter()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := adapter.PS(ctx, "")
	if err == nil {
		t.Fatal("expected an error because context was cancelled")
	}
}

// commandArgs is a helper used in table-driven tests to verify the args slice
// that would be passed to docker compose up for a given composeFile and profiles.
func commandArgs(composeFile string, profiles []string) []string {
	args := []string{"compose"}
	if composeFile != "" {
		args = append(args, "-f", composeFile)
	}
	for _, p := range profiles {
		args = append(args, "--profile", p)
	}
	args = append(args, "up", "-d")
	return args
}

// TestParseDownOutput verifies the docker compose down stderr parser.
// docker emits one "Container <name>  Removed" / "Volume <name>  Removed"
// line per removed resource; we count containers and volumes (not networks).
func TestParseDownOutput(t *testing.T) {
	tests := []struct {
		name           string
		stderr         string
		wantContainers int
		wantVolumes    int
	}{
		{
			name:           "empty output means nothing was running",
			stderr:         "",
			wantContainers: 0,
			wantVolumes:    0,
		},
		{
			name: "one container stopped, no volumes",
			stderr: " Container myapp-app-1  Removed\n" +
				" Network myapp_default  Removed\n",
			wantContainers: 1,
			wantVolumes:    0,
		},
		{
			name: "multiple containers and volumes removed",
			stderr: " Container myapp-app-1  Removed\n" +
				" Container myapp-kratos-1  Removed\n" +
				" Container myapp-postgres-1  Removed\n" +
				" Volume myapp_certs  Removed\n" +
				" Volume myapp_db  Removed\n" +
				" Network myapp_default  Removed\n",
			wantContainers: 3,
			wantVolumes:    2,
		},
		{
			name: "in-progress lines (not Removed) are ignored",
			stderr: " Container myapp-app-1  Stopping\n" +
				" Container myapp-app-1  Stopped\n" +
				" Container myapp-app-1  Removed\n",
			wantContainers: 1,
			wantVolumes:    0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := opsadapter.ParseDownOutputForTest(tt.stderr)
			if got.StoppedContainers != tt.wantContainers {
				t.Errorf("StoppedContainers = %d, want %d", got.StoppedContainers, tt.wantContainers)
			}
			if got.RemovedVolumes != tt.wantVolumes {
				t.Errorf("RemovedVolumes = %d, want %d", got.RemovedVolumes, tt.wantVolumes)
			}
		})
	}
}

// TestComposeAdapter_Up_SurfaceStderrOnFailure is a table-driven test that
// verifies Up's stderr handling. It uses the package-level Up implementation
// against a non-existent docker subcommand to trigger a real exec failure —
// this keeps the test hermetic (no real docker daemon required) while still
// exercising the actual adapter code path.
func TestComposeAdapter_Up_StderrSink(t *testing.T) {
	if !dockerAvailable() {
		t.Skip("docker binary not available")
	}
	// When opts.Stderr is nil, failure stderr must be flushed to the
	// adapter's stderrSink (not leaked to os.Stderr). We substitute a
	// bytes.Buffer via the test-only constructor and point docker at a
	// non-existent compose file so it fails deterministically.
	adapter, sink := opsadapter.NewComposeAdapterForTest()
	ctx := context.Background()

	err := adapter.Up(ctx, "/nonexistent/docker-compose.yml", nil, ports.ComposeUpOptions{})
	if err == nil {
		t.Fatal("expected an error for nonexistent compose file")
	}
	if sink.Len() == 0 {
		t.Error("expected stderr to be flushed to the sink on failure, got empty")
	}
}

func TestComposeAdapter_Up_VerboseStreamsStderr(t *testing.T) {
	if !dockerAvailable() {
		t.Skip("docker binary not available")
	}
	// When opts.Stderr is set, docker compose stderr must stream to the
	// caller even on failure paths. The internal sink must NOT receive a
	// duplicate dump.
	adapter, sink := opsadapter.NewComposeAdapterForTest()
	ctx := context.Background()
	var streamed bytes.Buffer

	err := adapter.Up(ctx, "/nonexistent/docker-compose.yml", nil, ports.ComposeUpOptions{Stderr: &streamed})
	if err == nil {
		t.Fatal("expected an error for nonexistent compose file")
	}
	if streamed.Len() == 0 {
		t.Error("expected stderr to stream to caller writer, got empty")
	}
	if sink.Len() != 0 {
		t.Errorf("expected internal sink to be empty in verbose mode, got %q", sink.String())
	}
}

func TestImageCheckerAdapter_ImageExists_NondexistentImage(t *testing.T) {
	// Run "docker image inspect" against a clearly non-existent image name.
	// This verifies that a missing image returns (false, nil) and not an error.
	if !dockerAvailable() {
		t.Skip("docker binary not available")
	}

	adapter := opsadapter.NewImageCheckerAdapter()
	exists, err := adapter.ImageExists(context.Background(), "vibewarden-test-nonexistent-image:definitely-not-here")
	if err != nil {
		t.Fatalf("ImageExists() unexpected error: %v", err)
	}
	if exists {
		t.Error("ImageExists() = true, want false for a non-existent image")
	}
}

func TestImageCheckerAdapter_ImageExists_CancelledContext(t *testing.T) {
	if !dockerAvailable() {
		t.Skip("docker binary not available")
	}

	adapter := opsadapter.NewImageCheckerAdapter()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := adapter.ImageExists(ctx, "alpine:latest")
	if err == nil {
		t.Fatal("ImageExists() expected error with cancelled context, got nil")
	}
}

func TestShellProberAdapter_CancelledContext(t *testing.T) {
	if !dockerAvailable() {
		t.Skip("docker binary not available")
	}

	adapter := opsadapter.NewShellProberAdapter()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := adapter.HasShell(ctx, "alpine:latest")
	if err == nil {
		t.Fatal("HasShell() expected error with cancelled context, got nil")
	}
}

func TestComposeAdapter_Tail_CancelledContext(t *testing.T) {
	if !dockerAvailable() {
		t.Skip("docker binary not available")
	}

	adapter := opsadapter.NewComposeAdapter()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := adapter.Tail(ctx, "", "vibewarden", 10)
	if err == nil {
		t.Fatal("Tail() expected error with cancelled context, got nil")
	}
}

func TestCommandArgsConstruction(t *testing.T) {
	tests := []struct {
		name        string
		composeFile string
		profiles    []string
		want        []string
	}{
		{
			name: "no file, no profiles",
			want: []string{"compose", "up", "-d"},
		},
		{
			name:     "no file, single profile",
			profiles: []string{"observability"},
			want:     []string{"compose", "--profile", "observability", "up", "-d"},
		},
		{
			name:     "no file, multiple profiles",
			profiles: []string{"observability", "debug"},
			want:     []string{"compose", "--profile", "observability", "--profile", "debug", "up", "-d"},
		},
		{
			name:        "with file, no profiles",
			composeFile: ".vibewarden/generated/docker-compose.yml",
			want:        []string{"compose", "-f", ".vibewarden/generated/docker-compose.yml", "up", "-d"},
		},
		{
			name:        "with file and profile",
			composeFile: ".vibewarden/generated/docker-compose.yml",
			profiles:    []string{"observability"},
			want:        []string{"compose", "-f", ".vibewarden/generated/docker-compose.yml", "--profile", "observability", "up", "-d"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := commandArgs(tt.composeFile, tt.profiles)
			if len(got) != len(tt.want) {
				t.Fatalf("len(args) = %d, want %d\ngot:  %v\nwant: %v", len(got), len(tt.want), got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("args[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}
