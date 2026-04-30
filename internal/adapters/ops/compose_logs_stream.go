package ops

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"

	"github.com/vibewarden/vibewarden/internal/ports"
)

// stderrCapCap is the maximum number of bytes captured from docker's stderr for
// daemon-unavailable detection. The relevant message is always in the first few
// hundred bytes; capping at 64 KiB prevents unbounded growth during long
// --follow sessions.
const stderrCapCap = 64 * 1024

// limitedWriter is an io.Writer that discards bytes once the underlying buffer
// reaches cap. It always reports the full len(p) as written so that
// io.MultiWriter does not short-circuit the other writers in the chain.
type limitedWriter struct {
	buf *bytes.Buffer
	cap int
}

func (w *limitedWriter) Write(p []byte) (int, error) {
	n := len(p)
	remaining := w.cap - w.buf.Len()
	if remaining > 0 {
		if len(p) > remaining {
			p = p[:remaining]
		}
		w.buf.Write(p) //nolint:errcheck // bytes.Buffer.Write never returns an error
	}
	return n, nil
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

	// Tee stderr: forward live to opts.Stderr AND capture into a capped buffer
	// for daemon-unavailable detection. The cap prevents unbounded growth
	// during long --follow sessions (container lifecycle messages accumulate).
	// The daemon-unavailable message is always in the first few hundred bytes
	// so stderrCapCap (64 KiB) is more than sufficient for detection.
	var stderrBuf bytes.Buffer
	capturer := &limitedWriter{buf: &stderrBuf, cap: stderrCapCap}
	if opts.Stderr != nil {
		cmd.Stderr = io.MultiWriter(opts.Stderr, capturer)
	} else {
		cmd.Stderr = capturer
	}

	if err := cmd.Run(); err != nil {
		// Context cancelled → Ctrl-C during --follow is a graceful exit.
		if ctx.Err() != nil {
			return nil
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

		// Classify daemon-unavailable stderr via the shared helper.
		// Recognised signatures: "cannot connect to the docker daemon",
		// "is the docker daemon running", "docker: command not found",
		// and the two permission-denied variants. If no signature matches,
		// originalErr is returned unchanged.
		classified := ClassifyDockerError(err, stderrBuf.String())
		return fmt.Errorf("docker compose logs: %w", classified)
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
