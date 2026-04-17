// Package deploy provides the application service that deploys a VibeWarden
// project to a remote server over SSH.
package deploy

import (
	"context"
	"fmt"
	"strings"

	"github.com/vibewarden/vibewarden/internal/ports"
)

// Mode indicates whether the target VM already has a VibeWarden sidecar
// installed or whether a fresh bootstrap is needed.
type Mode int

const (
	// ModeFreshInstall means no existing sidecar was found on the
	// target VM. The deploy flow must create the full directory layout,
	// write global.yaml, start the sidecar, and deploy the first site.
	ModeFreshInstall Mode = iota

	// ModeAddSite means an existing sidecar was detected on the
	// target VM. The deploy flow must add the new site under
	// ~/vibewarden/sites/<project>/ and restart the sidecar to pick up
	// the new configuration.
	ModeAddSite
)

// String returns a human-readable name for the deploy mode.
func (m Mode) String() string {
	switch m {
	case ModeFreshInstall:
		return "fresh-install"
	case ModeAddSite:
		return "add-site"
	default:
		return "unknown"
	}
}

// sidecarMarkerPath is the path to the global.yaml marker file that indicates
// an existing VibeWarden sidecar installation on the remote host.
const sidecarMarkerPath = remoteBaseDir + "/.sidecar/global.yaml"

// Detect checks the remote host for an existing VibeWarden sidecar
// installation by testing for the presence of the marker file at
// ~/vibewarden/.sidecar/global.yaml. If the file exists, the returned mode
// is ModeAddSite; otherwise it is ModeFreshInstall.
//
// Any SSH error that is not a "file not found" condition is propagated to the
// caller as-is so that connection failures are surfaced immediately.
func Detect(ctx context.Context, executor ports.RemoteExecutor) (Mode, error) {
	cmd := fmt.Sprintf("test -f %s", sidecarMarkerPath)
	_, err := executor.Run(ctx, cmd)
	if err == nil {
		return ModeAddSite, nil
	}

	// `test -f` exits with status 1 when the file does not exist.
	// The SSH executor wraps non-zero exit codes in the error message.
	// We treat exit status 1 as "file not found" and anything else as a
	// real SSH/connection error.
	if strings.Contains(err.Error(), "exit status 1") {
		return ModeFreshInstall, nil
	}

	return 0, fmt.Errorf("detecting deploy mode via SSH: %w", err)
}

// IsMultiApp checks whether the remote host has a multi-app sidecar layout
// by testing for the existence of the sites/ directory under ~/vibewarden/.
// Returns true when at least one site subdirectory exists, false otherwise.
//
// Any SSH error that is not a "directory not found" condition is propagated
// to the caller.
func IsMultiApp(ctx context.Context, executor ports.RemoteExecutor) (bool, error) {
	cmd := fmt.Sprintf("test -d %s", sitesDir)
	_, err := executor.Run(ctx, cmd)
	if err == nil {
		return true, nil
	}

	if strings.Contains(err.Error(), "exit status 1") {
		return false, nil
	}

	return false, fmt.Errorf("checking multi-app layout via SSH: %w", err)
}
