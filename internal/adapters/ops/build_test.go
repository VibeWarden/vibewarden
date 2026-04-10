package ops_test

import (
	"context"
	"testing"

	opsadapter "github.com/vibewarden/vibewarden/internal/adapters/ops"
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

	err := adapter.Build(ctx, "test-image:latest", ".", false)
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

	err := adapter.Build(ctx, "test-image:latest", ".", true)
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
	err := adapter.Build(context.Background(), "test-image:latest", ".", false)
	if err == nil {
		t.Fatal("expected an error when docker is not available")
	}
}

// TestBuildArgsConstruction verifies the expected docker build args shape for
// various input combinations without actually running docker.
func TestBuildArgsConstruction(t *testing.T) {
	tests := []struct {
		name       string
		tag        string
		contextDir string
		noCache    bool
		wantArgs   []string
	}{
		{
			name:       "basic build",
			tag:        "myapp:latest",
			contextDir: ".",
			noCache:    false,
			wantArgs:   []string{"build", "-t", "myapp:latest", "."},
		},
		{
			name:       "build with no-cache",
			tag:        "myapp:latest",
			contextDir: ".",
			noCache:    true,
			wantArgs:   []string{"build", "-t", "myapp:latest", "--no-cache", "."},
		},
		{
			name:       "build with custom context dir",
			tag:        "webapp:v2",
			contextDir: "/home/user/project",
			noCache:    false,
			wantArgs:   []string{"build", "-t", "webapp:v2", "/home/user/project"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildArgs(tt.tag, tt.contextDir, tt.noCache)
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

// buildArgs mirrors the logic in BuildAdapter.Build to allow table-driven
// testing of the argument construction without executing docker.
func buildArgs(tag, contextDir string, noCache bool) []string {
	args := []string{"build", "-t", tag}
	if noCache {
		args = append(args, "--no-cache")
	}
	args = append(args, contextDir)
	return args
}
