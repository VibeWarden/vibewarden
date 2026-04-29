package ops

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"

	"github.com/vibewarden/vibewarden/internal/config"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// Rebuild executes the stop → rmi → build → start sequence.
//
// It is the recovery path for the image-identity mismatch introduced by
// ADR-100 / #1219. The sequence:
//
//  1. Print "Stopping stack..." and call compose.Down on the generated
//     docker-compose.yml. Volumes are removed when opts.RebuildVolumes is true.
//     Failure aborts before any destructive image step.
//  2. If cfg.App.Image is user-managed (not vibew-derived) OR cfg.App.Build is
//     non-empty (compose builds the image itself), print the INFO skip line and
//     fall through to s.Run directly — no rmi or vibew-build step.
//  3. Otherwise: print "Removing image <tag>" and call s.imageRemover.Remove.
//     The adapter returns nil for "No such image" (idempotent), so the line is
//     always printed to document intent even when the image was already absent.
//     Any other Remove failure is logged at WARN and execution continues — the
//     build that follows will recreate the image regardless.
//  4. Print "Rebuilding image..." and call builder.Run with the resolved tag
//     (same tag as the rmi target). On build failure the stack is NOT started;
//     the error is returned to the caller.
//  5. Print "Starting stack..." and call s.Run. The freshly-built image carries
//     correct project-root labels so #1219's identity check passes naturally.
//
// Exact verbose stdout template (pinned by rebuild_test.go):
//
//	Stopping stack...
//	Removing image qr-code-blackhole-app:latest
//	Rebuilding image...
//	Starting stack...
//
// User-set escape hatch line (pinned by rebuild_test.go):
//
//	vibew dev --rebuild: app.image is user-managed; skipping rmi+build, starting stack normally.
func (s *DevService) Rebuild(
	ctx context.Context,
	cfg *config.Config,
	opts DevOptions,
	builder *BuildService,
	out io.Writer,
) error {
	composeFile := filepath.Join(generatedOutputDir, "docker-compose.yml")

	// Step 1 — stop the stack.
	fmt.Fprintln(out, "Stopping stack...")
	_, err := s.compose.Down(ctx, composeFile, ports.ComposeDownOptions{
		Volumes: opts.RebuildVolumes,
	})
	if err != nil {
		return fmt.Errorf("stopping stack: %w", err)
	}

	// Step 2 — skip path when the image is user-managed or compose builds it.
	if isUserSetImage(cfg) || cfg.App.Build != "" {
		fmt.Fprintln(out, "vibew dev --rebuild: app.image is user-managed; skipping rmi+build, starting stack normally.")
		return s.Run(ctx, cfg, opts, out)
	}

	// Resolve the image tag once; use the same value for both rmi and build so
	// they cannot drift.
	tag := cfg.ComposeProjectName() + "-app:latest"

	// Step 3 — remove the image (idempotent; "no such image" is nil from adapter).
	fmt.Fprintf(out, "Removing image %s\n", tag)
	if s.imageRemover != nil {
		if rmErr := s.imageRemover.Remove(ctx, tag); rmErr != nil {
			// Non-fatal: log and continue so the build still runs.
			slog.Warn("docker image rm reported an error; continuing to build",
				"tag", tag, "error", rmErr)
		}
	}

	// Step 4 — rebuild the image.
	fmt.Fprintln(out, "Rebuilding image...")
	if err := builder.Run(ctx, cfg, BuildOptions{ImageTag: tag}, out); err != nil {
		return fmt.Errorf("rebuilding image: %w", err)
	}

	// Step 5 — start the stack.
	fmt.Fprintln(out, "Starting stack...")
	return s.Run(ctx, cfg, opts, out)
}
