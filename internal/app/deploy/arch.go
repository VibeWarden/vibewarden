package deploy

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"strings"

	"github.com/vibewarden/vibewarden/internal/archutil"
)

// ErrArchMismatch is a sentinel error returned when the local build architecture
// does not match the remote server architecture and a locally-built image is
// being transferred. Use errors.Is to check for this condition.
var ErrArchMismatch = errors.New("architecture mismatch")

// ArchMismatchError provides detailed context about an architecture mismatch
// between the local build environment and the remote deploy target. It includes
// a fix-it suggestion directing the user to rebuild with the correct platform.
type ArchMismatchError struct {
	// LocalArch is the normalized architecture of the local build environment.
	LocalArch string
	// RemoteArch is the normalized architecture of the remote deploy target.
	RemoteArch string
}

// Error returns a human-readable message with a fix-it suggestion.
func (e *ArchMismatchError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "architecture mismatch: local build is %s but remote server is %s\n", e.LocalArch, e.RemoteArch)
	b.WriteString("\n  Fix: rebuild with the correct platform:\n")
	fmt.Fprintf(&b, "    vibew build --platform linux/%s\n", e.RemoteArch)
	return b.String()
}

// Unwrap returns ErrArchMismatch so that errors.Is works.
func (e *ArchMismatchError) Unwrap() error {
	return ErrArchMismatch
}

// checkArchCompatibility compares the local architecture with the remote host's
// architecture (via "uname -m"). It is called during the prerequisites check
// only when a locally-built image will be transferred to the remote. When the
// architectures do not match, an ArchMismatchError is returned with a fix-it
// suggestion.
//
// The check is skipped (returns nil) when:
//   - The remote architecture cannot be determined (logs a warning but continues)
//   - Either architecture normalizes to an empty or unrecognized value
func (s *Service) checkArchCompatibility(ctx context.Context) error {
	output, err := s.executor.Run(ctx, "uname -m")
	if err != nil {
		// Cannot determine remote arch -- skip the check rather than blocking deploy.
		return nil
	}

	remoteArch := archutil.Normalize(output)
	localArch := s.localArch
	if localArch == "" {
		localArch = runtime.GOARCH
	}

	// Skip if either value is empty or unrecognized (no known mapping).
	if localArch == "" || remoteArch == "" {
		return nil
	}

	if localArch != remoteArch {
		return &ArchMismatchError{
			LocalArch:  localArch,
			RemoteArch: remoteArch,
		}
	}

	return nil
}
