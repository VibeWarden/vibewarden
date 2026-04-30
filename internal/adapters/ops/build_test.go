package ops_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	opsadapter "github.com/vibewarden/vibewarden/internal/adapters/ops"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// TestBuildAdapter_CancelledContextReturnsError verifies that Build returns an
// error when the context is cancelled before docker starts. This confirms the
// command is attempted without requiring docker to succeed.
func TestBuildAdapter_CancelledContextReturnsError(t *testing.T) {
	if !dockerAvailable() {
		t.Skip("docker binary not available")
	}

	adapter := opsadapter.NewBuildAdapter()
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately so docker exits fast

	err := adapter.Build(ctx, "test-image:latest", ".", ports.DockerBuildOptions{})
	if err == nil {
		t.Fatal("expected an error because context was cancelled before run")
	}
}

// TestBuildAdapter_CancelledContextNoCacheReturnsError verifies that the
// --no-cache path also respects context cancellation.
func TestBuildAdapter_CancelledContextNoCacheReturnsError(t *testing.T) {
	if !dockerAvailable() {
		t.Skip("docker binary not available")
	}

	adapter := opsadapter.NewBuildAdapter()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := adapter.Build(ctx, "test-image:latest", ".", ports.DockerBuildOptions{NoCache: true})
	if err == nil {
		t.Fatal("expected an error because context was cancelled before run")
	}
}

// TestBuildAdapter_ReturnsErrorWhenDockerMissing verifies that Build returns an
// error when docker is not installed.
func TestBuildAdapter_ReturnsErrorWhenDockerMissing(t *testing.T) {
	if dockerAvailable() {
		t.Skip("docker is available; skipping missing-docker test")
	}

	adapter := opsadapter.NewBuildAdapter()
	err := adapter.Build(context.Background(), "test-image:latest", ".", ports.DockerBuildOptions{})
	if err == nil {
		t.Fatal("expected an error when docker is not available")
	}
}

// TestBuildArgsConstruction verifies the expected docker build args shape for
// various input combinations without actually running docker. Tests use the
// exported BuildDockerArgsForTest helper which mirrors the adapter's internal
// buildDockerArgs function.
func TestBuildArgsConstruction(t *testing.T) {
	tests := []struct {
		name       string
		tag        string
		contextDir string
		opts       ports.DockerBuildOptions
		wantArgs   []string
	}{
		{
			name:       "basic build",
			tag:        "myapp:latest",
			contextDir: ".",
			opts:       ports.DockerBuildOptions{},
			wantArgs:   []string{"build", "-t", "myapp:latest", "."},
		},
		{
			name:       "build with no-cache",
			tag:        "myapp:latest",
			contextDir: ".",
			opts:       ports.DockerBuildOptions{NoCache: true},
			wantArgs:   []string{"build", "-t", "myapp:latest", "--no-cache", "."},
		},
		{
			name:       "build with custom context dir",
			tag:        "webapp:v2",
			contextDir: "/home/user/project",
			opts:       ports.DockerBuildOptions{},
			wantArgs:   []string{"build", "-t", "webapp:v2", "/home/user/project"},
		},
		{
			name:       "build with platform",
			tag:        "myapp:latest",
			contextDir: ".",
			opts:       ports.DockerBuildOptions{Platform: "linux/amd64"},
			wantArgs:   []string{"build", "--platform", "linux/amd64", "-t", "myapp:latest", "."},
		},
		{
			name:       "build with platform and no-cache",
			tag:        "myapp:latest",
			contextDir: ".",
			opts:       ports.DockerBuildOptions{Platform: "linux/arm64", NoCache: true},
			wantArgs:   []string{"build", "--platform", "linux/arm64", "-t", "myapp:latest", "--no-cache", "."},
		},
		{
			// Labels are emitted in alphabetical key order — deterministic for testing.
			name:       "build with labels in alpha order",
			tag:        "myapp:latest",
			contextDir: ".",
			opts: ports.DockerBuildOptions{
				Labels: map[string]string{
					"org.vibewarden.project-root":      "/Users/foo/myapp",
					"org.vibewarden.project-root-hash": "sha256:abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890ab",
				},
			},
			wantArgs: []string{
				"build", "-t", "myapp:latest",
				"--label", "org.vibewarden.project-root=/Users/foo/myapp",
				"--label", "org.vibewarden.project-root-hash=sha256:abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890ab",
				".",
			},
		},
		{
			name:       "build with single label",
			tag:        "myapp:latest",
			contextDir: ".",
			opts: ports.DockerBuildOptions{
				Labels: map[string]string{"mykey": "myval"},
			},
			wantArgs: []string{"build", "-t", "myapp:latest", "--label", "mykey=myval", "."},
		},
		{
			name:       "nil labels — no label flags emitted",
			tag:        "myapp:latest",
			contextDir: ".",
			opts:       ports.DockerBuildOptions{Labels: nil},
			wantArgs:   []string{"build", "-t", "myapp:latest", "."},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := opsadapter.BuildDockerArgsForTest(tt.tag, tt.contextDir, tt.opts)
			if len(got) != len(tt.wantArgs) {
				t.Fatalf("len(args) = %d, want %d\ngot:  %v\nwant: %v",
					len(got), len(tt.wantArgs), got, tt.wantArgs)
			}
			for i := range got {
				if got[i] != tt.wantArgs[i] {
					t.Errorf("args[%d] = %q, want %q", i, got[i], tt.wantArgs[i])
				}
			}
		})
	}
}

// writeFakeDockerScript writes a shell script named "docker" into dir that
// prints msg to stderr and exits with the given code.
func writeFakeDockerScript(t *testing.T, dir, msg string, exitCode int) {
	t.Helper()
	script := fmt.Sprintf("#!/bin/sh\necho '%s' >&2\nexit %d\n", msg, exitCode)
	path := filepath.Join(dir, "docker")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("writing fake docker script: %v", err)
	}
}

// TestBuildAdapter_Build_DaemonNotRunning verifies that Build returns a
// *ports.DockerUnavailableError with ErrDockerDaemonNotRunning when the docker
// binary emits the canonical "Cannot connect to the Docker daemon" message.
// The test injects a fake docker binary via a modified PATH.
func TestBuildAdapter_Build_DaemonNotRunning(t *testing.T) {
	dir := t.TempDir()
	writeFakeDockerScript(t, dir, "Cannot connect to the Docker daemon at unix:///var/run/docker.sock. Is the docker daemon running?", 1)
	t.Setenv("PATH", dir)

	adapter := opsadapter.NewBuildAdapter()
	err := adapter.Build(context.Background(), "test-image:latest", ".", ports.DockerBuildOptions{})

	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ports.ErrDockerDaemonNotRunning) {
		t.Errorf("errors.Is(err, ErrDockerDaemonNotRunning) = false; got %v", err)
	}
	if !errors.Is(err, ports.ErrDockerUnavailable) {
		t.Errorf("errors.Is(err, ErrDockerUnavailable) = false; got %v", err)
	}
	var de *ports.DockerUnavailableError
	if !errors.As(err, &de) {
		t.Error("errors.As(*DockerUnavailableError) = false")
	}
}

// TestBuildAdapter_Build_PermissionDenied verifies that Build returns a
// *ports.DockerUnavailableError with ErrDockerSocketPermission when the docker
// binary emits the canonical "permission denied while trying to connect to the
// Docker API" message. The test injects a fake docker binary via a modified PATH.
func TestBuildAdapter_Build_PermissionDenied(t *testing.T) {
	dir := t.TempDir()
	writeFakeDockerScript(t, dir, "permission denied while trying to connect to the Docker API socket at unix:///var/run/docker.sock", 1)
	t.Setenv("PATH", dir)

	adapter := opsadapter.NewBuildAdapter()
	err := adapter.Build(context.Background(), "test-image:latest", ".", ports.DockerBuildOptions{})

	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ports.ErrDockerSocketPermission) {
		t.Errorf("errors.Is(err, ErrDockerSocketPermission) = false; got %v", err)
	}
	if !errors.Is(err, ports.ErrDockerUnavailable) {
		t.Errorf("errors.Is(err, ErrDockerUnavailable) = false; got %v", err)
	}
	var de *ports.DockerUnavailableError
	if !errors.As(err, &de) {
		t.Error("errors.As(*DockerUnavailableError) = false")
	}
}
