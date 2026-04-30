package ops

import (
	"strings"

	"github.com/vibewarden/vibewarden/internal/ports"
)

// ClassifyDockerError inspects stderr captured from a docker shell-out and
// returns a *ports.DockerUnavailableError wrapping the most specific sentinel
// when a known unavailability signature is detected, or originalErr unchanged
// when no signature matches (including empty stderr or nil originalErr).
//
// Recognised signatures:
//   - "permission denied while trying to connect to the docker API" →
//     ErrDockerSocketPermission
//   - "unix:///" AND "permission denied" →
//     ErrDockerSocketPermission (macOS user-socket path variant)
//   - "Cannot connect to the Docker daemon" →
//     ErrDockerDaemonNotRunning
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

	lower := strings.ToLower(stderr)

	hasPermissionAPI := strings.Contains(lower, "permission denied while trying to connect to the docker api")
	hasUnixSocket := strings.Contains(lower, "unix:///") && strings.Contains(lower, "permission denied")
	hasDaemonNotRunning := strings.Contains(lower, "cannot connect to the docker daemon")

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
