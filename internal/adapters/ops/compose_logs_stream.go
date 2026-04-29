package ops

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"

	"github.com/vibewarden/vibewarden/internal/ports"
)

// daemonUnavailableSignatures are substrings that appear in docker's stderr
// when the daemon is not running or docker is not installed.
var daemonUnavailableSignatures = []string{
	"cannot connect to the docker daemon",
	"docker: command not found",
	"is the docker daemon running",
}

// cmdRunner is the function signature for constructing and running an
// exec.Cmd. It is a package-level variable so tests can substitute a fake
// implementation without spawning real processes.
type cmdRunner func(ctx context.Context, name string, args ...string) *exec.Cmd

// defaultCmdRunner is the production cmdRunner that delegates to exec.CommandContext.
func defaultCmdRunner(ctx context.Context, name string, args ...string) *exec.Cmd {
	return exec.CommandContext(ctx, name, args...) //nolint:gosec // args are constructed from caller-supplied values, not shell input
}

// ComposeLogsStreamAdapter implements ports.ComposeLogsStreamer by shelling
// out to the docker compose CLI.
//
// Stdout from the subprocess is written directly to opts.Stdout — no
// buffering — so --follow streams in real time. Stderr is tee'd into a small
// internal buffer to detect the daemon-unavailable condition while still
// forwarding output to opts.Stderr.
type ComposeLogsStreamAdapter struct {
	runner cmdRunner
}

// NewComposeLogsStreamAdapter creates a ComposeLogsStreamAdapter that shells
// out to the real docker CLI.
func NewComposeLogsStreamAdapter() *ComposeLogsStreamAdapter {
	return &ComposeLogsStreamAdapter{runner: defaultCmdRunner}
}

// NewComposeLogsStreamAdapterWithRunner creates a ComposeLogsStreamAdapter
// that uses the provided cmdRunner. Intended for testing only — production
// code should use NewComposeLogsStreamAdapter.
func NewComposeLogsStreamAdapterWithRunner(r func(ctx context.Context, name string, args ...string) *exec.Cmd) *ComposeLogsStreamAdapter {
	return &ComposeLogsStreamAdapter{runner: r}
}

// Stream runs "docker compose -p <project> -f <composeFile> logs [--tail N]
// [--follow] [--since X] [services...]" and writes output to opts.Stdout and
// opts.Stderr. It returns nil when the context is cancelled (graceful Ctrl-C
// during --follow). It returns ports.ErrDockerUnavailable when the daemon
// cannot be reached.
func (a *ComposeLogsStreamAdapter) Stream(ctx context.Context, opts ports.ComposeLogsStreamOptions) error {
	args := BuildLogsArgs(opts)

	cmd := a.runner(ctx, "docker", args...)
	cmd.Stdout = opts.Stdout

	// Tee stderr: forward live to opts.Stderr AND capture into a small buffer
	// for daemon-unavailable detection. The daemon-unavailable message is
	// always in the first few hundred bytes; we do not need a large buffer.
	var stderrBuf bytes.Buffer
	if opts.Stderr != nil {
		cmd.Stderr = io.MultiWriter(opts.Stderr, &stderrBuf)
	} else {
		cmd.Stderr = &stderrBuf
	}

	if err := cmd.Run(); err != nil {
		// Context cancelled → Ctrl-C during --follow is a graceful exit.
		if ctx.Err() != nil {
			return nil
		}

		// Classify daemon-unavailable stderr.
		stderrLower := strings.ToLower(stderrBuf.String())
		for _, sig := range daemonUnavailableSignatures {
			if strings.Contains(stderrLower, sig) {
				return fmt.Errorf("docker compose logs: %w", ports.ErrDockerUnavailable)
			}
		}

		// Check for signal-related exit (SIGINT/SIGTERM when --follow is
		// interrupted by the OS rather than context cancellation).
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			if exitErr.ExitCode() == -1 {
				// Killed by signal — treat as clean exit.
				return nil
			}
		}

		return fmt.Errorf("docker compose logs: %w", err)
	}
	return nil
}

// BuildLogsArgs constructs the docker CLI argv for a logs invocation.
// It is a pure function exposed so tests can verify argument construction
// without running docker.
func BuildLogsArgs(opts ports.ComposeLogsStreamOptions) []string {
	args := []string{"compose"}
	if opts.ProjectName != "" {
		args = append(args, "-p", opts.ProjectName)
	}
	if opts.ComposeFile != "" {
		args = append(args, "-f", opts.ComposeFile)
	}
	args = append(args, "logs")
	if opts.Tail != 0 {
		tail := opts.Tail
		if tail == -1 {
			args = append(args, "--tail", "all")
		} else {
			args = append(args, "--tail", fmt.Sprintf("%d", tail))
		}
	}
	if opts.Follow {
		args = append(args, "--follow")
	}
	if opts.Since != "" {
		args = append(args, "--since", opts.Since)
	}
	args = append(args, opts.Services...)
	return args
}
