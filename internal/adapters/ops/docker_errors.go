package ops

import (
	"strings"

	"github.com/vibewarden/vibewarden/internal/ports"
)

// ClassifyDockerError inspects the error message and stderr captured from a
// docker shell-out and returns a *ports.DockerUnavailableError wrapping the
// most specific sentinel when a known unavailability signature is detected, or
// originalErr unchanged when no signature matches (including empty stderr or
// nil originalErr).
//
// This is the single canonical place where Docker error detection lives.
// Do not add docker-binary-absent or daemon-unavailable string checks anywhere
// else in the codebase — extend this function instead.
//
// Recognised signatures (checked in precedence order):
//   - err.Error() contains "executable file not found" →
//     ErrDockerUnavailable (docker binary absent from PATH)
//   - stderr contains "permission denied while trying to connect to the docker api" →
//     ErrDockerSocketPermission
//   - stderr contains "unix:///" AND "permission denied" →
//     ErrDockerSocketPermission (macOS user-socket path variant)
//   - stderr contains "cannot connect to the docker daemon" →
//     ErrDockerDaemonNotRunning
//   - stderr contains "is the docker daemon running" →
//     ErrDockerDaemonNotRunning (variant phrasing emitted by some Docker versions)
//   - stderr contains "docker: command not found" →
//     ErrDockerDaemonNotRunning (snap-installed Docker on Ubuntu; binary missing
//     means the daemon is unreachable — same operator hint applies)
//
// Precedence rule when both permission and daemon-not-running signatures appear:
// ErrDockerSocketPermission wins. Rationale: a socket that exists but is
// locked is a more specific diagnosis than the generic "cannot connect" envelope.
//
// The returned *DockerUnavailableError satisfies errors.Is for both the
// specific sentinel and the ErrDockerUnavailable umbrella, so existing callers
// that check errors.Is(err, ports.ErrDockerUnavailable) continue to match.
func ClassifyDockerError(originalErr error, stderr string) error {
	if originalErr == nil {
		return nil
	}

	// Check the error message itself for binary-not-found before inspecting
	// stderr — exec.ErrNotFound / LookPath embeds this signature in the error.
	if strings.Contains(originalErr.Error(), "executable file not found") {
		return &ports.DockerUnavailableError{
			Sentinel: ports.ErrDockerUnavailable,
			Stderr:   stderr,
			Cause:    originalErr,
		}
	}

	lower := strings.ToLower(stderr)

	hasPermissionAPI := strings.Contains(lower, "permission denied while trying to connect to the docker api")
	hasUnixSocket := strings.Contains(lower, "unix:///") && strings.Contains(lower, "permission denied")
	hasDaemonNotRunning := strings.Contains(lower, "cannot connect to the docker daemon") ||
		strings.Contains(lower, "is the docker daemon running") ||
		strings.Contains(lower, "docker: command not found")

	var sentinel error
	switch {
	case hasPermissionAPI || hasUnixSocket:
		sentinel = ports.ErrDockerSocketPermission
	case hasDaemonNotRunning:
		sentinel = ports.ErrDockerDaemonNotRunning
	default:
		return originalErr
	}

	return &ports.DockerUnavailableError{
		Sentinel: sentinel,
		Stderr:   stderr,
		Cause:    originalErr,
	}
}
