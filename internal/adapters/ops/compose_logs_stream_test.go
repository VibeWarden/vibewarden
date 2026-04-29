package ops_test

import (
	"bytes"
	"context"
	"os/exec"
	"strings"
	"testing"

	"github.com/vibewarden/vibewarden/internal/adapters/ops"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// TestBuildLogsArgs verifies that buildLogsArgs produces the expected docker
// CLI argv for various ComposeLogsStreamOptions combinations.
func TestBuildLogsArgs(t *testing.T) {
	tests := []struct {
		name     string
		opts     ports.ComposeLogsStreamOptions
		wantArgs []string
	}{
		{
			name: "basic all services",
			opts: ports.ComposeLogsStreamOptions{
				ProjectName: "myproject",
				ComposeFile: "/path/to/docker-compose.yml",
				Tail:        100,
			},
			wantArgs: []string{"compose", "-p", "myproject", "-f", "/path/to/docker-compose.yml", "logs", "--tail", "100"},
		},
		{
			name: "single service",
			opts: ports.ComposeLogsStreamOptions{
				ProjectName: "myproject",
				ComposeFile: "/path/to/docker-compose.yml",
				Tail:        100,
				Services:    []string{"vibewarden"},
			},
			wantArgs: []string{"compose", "-p", "myproject", "-f", "/path/to/docker-compose.yml", "logs", "--tail", "100", "vibewarden"},
		},
		{
			name: "multi-service",
			opts: ports.ComposeLogsStreamOptions{
				ProjectName: "myproject",
				ComposeFile: "/path/to/docker-compose.yml",
				Tail:        100,
				Services:    []string{"vibewarden", "app"},
			},
			wantArgs: []string{"compose", "-p", "myproject", "-f", "/path/to/docker-compose.yml", "logs", "--tail", "100", "vibewarden", "app"},
		},
		{
			name: "--follow flag",
			opts: ports.ComposeLogsStreamOptions{
				ProjectName: "myproject",
				ComposeFile: "/path/to/docker-compose.yml",
				Tail:        100,
				Follow:      true,
			},
			wantArgs: []string{"compose", "-p", "myproject", "-f", "/path/to/docker-compose.yml", "logs", "--tail", "100", "--follow"},
		},
		{
			name: "--since flag",
			opts: ports.ComposeLogsStreamOptions{
				ProjectName: "myproject",
				ComposeFile: "/path/to/docker-compose.yml",
				Tail:        100,
				Since:       "5m",
			},
			wantArgs: []string{"compose", "-p", "myproject", "-f", "/path/to/docker-compose.yml", "logs", "--tail", "100", "--since", "5m"},
		},
		{
			name: "tail -1 means all",
			opts: ports.ComposeLogsStreamOptions{
				ProjectName: "myproject",
				ComposeFile: "/path/to/docker-compose.yml",
				Tail:        -1,
			},
			wantArgs: []string{"compose", "-p", "myproject", "-f", "/path/to/docker-compose.yml", "logs", "--tail", "all"},
		},
		{
			name: "tail 50",
			opts: ports.ComposeLogsStreamOptions{
				ProjectName: "myproject",
				ComposeFile: "/path/to/docker-compose.yml",
				Tail:        50,
			},
			wantArgs: []string{"compose", "-p", "myproject", "-f", "/path/to/docker-compose.yml", "logs", "--tail", "50"},
		},
		{
			name: "tail 0 omits --tail flag",
			opts: ports.ComposeLogsStreamOptions{
				ProjectName: "myproject",
				ComposeFile: "/path/to/docker-compose.yml",
				Tail:        0,
			},
			wantArgs: []string{"compose", "-p", "myproject", "-f", "/path/to/docker-compose.yml", "logs"},
		},
		{
			name: "project name in args",
			opts: ports.ComposeLogsStreamOptions{
				ProjectName: "testproject",
				ComposeFile: "/tmp/docker-compose.yml",
				Tail:        100,
			},
			wantArgs: []string{"compose", "-p", "testproject", "-f", "/tmp/docker-compose.yml", "logs", "--tail", "100"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ops.BuildLogsArgs(tt.opts)
			if len(got) != len(tt.wantArgs) {
				t.Fatalf("got args %v (len %d), want %v (len %d)", got, len(got), tt.wantArgs, len(tt.wantArgs))
			}
			for i, want := range tt.wantArgs {
				if got[i] != want {
					t.Errorf("arg[%d]: got %q, want %q\nfull args: %v", i, got[i], want, got)
				}
			}
		})
	}
}

// fakeRunner returns an exec.Cmd that immediately exits with the given code
// and writes msg to its stderr. It uses the os/exec test helper pattern by
// actually running the current test binary with a special env var.
//
// For simplicity in these tests we use a direct approach: build a Cmd
// that calls an echo-like program via shell. However, since we cannot rely
// on shell availability in all environments, we use a simpler approach:
// we inject a runner that returns a pre-built Cmd.

// TestComposeLogsStreamAdapter_DockerUnavailable verifies that the adapter
// translates a "Cannot connect to the Docker daemon" stderr signature into
// ports.ErrDockerUnavailable.
func TestComposeLogsStreamAdapter_DockerUnavailable(t *testing.T) {
	var capturedStderr bytes.Buffer
	adapter := ops.NewComposeLogsStreamAdapterWithRunner(func(ctx context.Context, name string, args ...string) *exec.Cmd {
		cmd := exec.CommandContext(ctx, "sh", "-c", "echo 'Cannot connect to the Docker daemon' >&2; exit 1") //nolint:gosec
		return cmd
	})

	err := adapter.Stream(context.Background(), ports.ComposeLogsStreamOptions{
		ProjectName: "test",
		ComposeFile: "/tmp/compose.yml",
		Stdout:      &bytes.Buffer{},
		Stderr:      &capturedStderr,
	})

	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "docker daemon unavailable") {
		t.Errorf("expected ErrDockerUnavailable, got: %v", err)
	}
}

// TestComposeLogsStreamAdapter_CtxCancel verifies that a cancelled context
// results in a nil error (graceful Ctrl-C).
func TestComposeLogsStreamAdapter_CtxCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately before even running

	adapter := ops.NewComposeLogsStreamAdapterWithRunner(func(ctx context.Context, name string, args ...string) *exec.Cmd {
		// This command would block, but ctx is already cancelled.
		return exec.CommandContext(ctx, "sleep", "100") //nolint:gosec
	})

	err := adapter.Stream(ctx, ports.ComposeLogsStreamOptions{
		ProjectName: "test",
		ComposeFile: "/tmp/compose.yml",
		Stdout:      &bytes.Buffer{},
		Stderr:      &bytes.Buffer{},
	})

	if err != nil {
		t.Errorf("Stream() with cancelled ctx expected nil error, got: %v", err)
	}
}

// TestComposeLogsStreamAdapter_ArgsContainProject verifies that the project
// flag and compose file appear in the argv passed to docker.
func TestComposeLogsStreamAdapter_ArgsContainProject(t *testing.T) {
	var capturedArgs []string

	adapter := ops.NewComposeLogsStreamAdapterWithRunner(func(ctx context.Context, name string, args ...string) *exec.Cmd {
		capturedArgs = append([]string{name}, args...)
		// Return a no-op command.
		return exec.CommandContext(ctx, "true") //nolint:gosec
	})

	_ = adapter.Stream(context.Background(), ports.ComposeLogsStreamOptions{
		ProjectName: "testproj",
		ComposeFile: "/tmp/test-compose.yml",
		Tail:        100,
		Stdout:      &bytes.Buffer{},
		Stderr:      &bytes.Buffer{},
	})

	assertContainsSequence(t, capturedArgs, []string{"-p", "testproj"})
	assertContainsSequence(t, capturedArgs, []string{"-f", "/tmp/test-compose.yml"})
	assertContains(t, capturedArgs, "logs")
}

// assertContainsSequence checks that slice contains the sub-sequence of
// consecutive elements.
func assertContainsSequence(t *testing.T, args, seq []string) {
	t.Helper()
	for i := 0; i <= len(args)-len(seq); i++ {
		match := true
		for j, s := range seq {
			if args[i+j] != s {
				match = false
				break
			}
		}
		if match {
			return
		}
	}
	t.Errorf("args %v do not contain sequence %v", args, seq)
}

// assertContains checks that slice contains the given element.
func assertContains(t *testing.T, args []string, want string) {
	t.Helper()
	for _, a := range args {
		if a == want {
			return
		}
	}
	t.Errorf("args %v do not contain %q", args, want)
}
