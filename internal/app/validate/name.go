package validate

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/vibewarden/vibewarden/internal/app/ops"
)

// CheckName detects the name-collision case: when cfg.Name is empty and the
// directory name of projectRoot is the exact string "vibewarden", the image
// tag would be "vibewarden-app:latest" which collides with the sidecar binary
// name and causes confusion downstream.
//
// Per spec (issue #1144 AC §Check 1): FAIL only when cwd basename is exactly
// "vibewarden". Any other directory name (even "vibewarden-app", "myapp", etc.)
// results in a skip — no row emitted — because there is no collision in those
// cases.
func CheckName(_ context.Context, inputs CheckInputs) Result {
	// When name: is set, no collision is possible — the explicit name wins.
	if inputs.Cfg.Name != "" {
		return Result{Skip: true}
	}

	dir := inputs.ProjectRoot
	if dir == "" {
		return Result{Skip: true}
	}

	base := filepath.Base(dir)
	if base != "vibewarden" {
		return Result{Skip: true}
	}

	return Result{
		State: ops.StatusFAIL,
		Message: fmt.Sprintf(
			`name: unset and directory name %q collides with the sidecar binary name — set name: <project-slug> in vibewarden.yaml`,
			base,
		),
	}
}
