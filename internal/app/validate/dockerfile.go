package validate

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/vibewarden/vibewarden/internal/app/dockerfile"
	"github.com/vibewarden/vibewarden/internal/app/ops"
)

// CheckDockerfile parses the EXPOSE directives in <projectRoot>/Dockerfile and
// compares the last valid port against cfg.Upstream.Port. It delegates parsing
// to the shared internal/app/dockerfile package.
//
// Skip conditions (no row emitted):
//   - Dockerfile is absent.
//   - No valid EXPOSE directive is found.
//   - The ports match.
//
// Multi-line continuation (\) is not supported; such lines are treated as
// malformed and skipped, matching the architect's directive.
func CheckDockerfile(_ context.Context, inputs CheckInputs) Result {
	dockerfilePath := filepath.Join(inputs.ProjectRoot, "Dockerfile")
	f, err := os.Open(dockerfilePath) //nolint:gosec // ProjectRoot is the project root provided by the caller
	if err != nil {
		if os.IsNotExist(err) {
			return Result{Skip: true}
		}
		// Unreadable Dockerfile: skip silently — validate is a best-effort check.
		return Result{Skip: true}
	}
	defer func() { _ = f.Close() }()

	parsed, err := dockerfile.Parse(f)
	if err != nil {
		return Result{Skip: true}
	}

	if len(parsed.Exposes) == 0 {
		// No valid EXPOSE found — skip silently.
		return Result{Skip: true}
	}

	lastPort := parsed.Exposes[len(parsed.Exposes)-1].Port

	if lastPort == inputs.Cfg.Upstream.Port {
		// Ports match — no row needed.
		return Result{Skip: true}
	}

	return Result{
		State: ops.StatusFAIL,
		Message: fmt.Sprintf(
			"Dockerfile EXPOSE %d does not match upstream.port %d — update one to match",
			lastPort,
			inputs.Cfg.Upstream.Port,
		),
	}
}
