package ops

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/vibewarden/vibewarden/internal/config"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// BuildService orchestrates the "vibew build" use case.
// It resolves the Docker image tag from vibewarden.yaml (app.image) or falls
// back to the current directory name, then delegates to a DockerBuilder.
// When a shell prober is wired, it probes the built image for /bin/sh and
// prints a warning when the image has no shell (distroless/scratch images).
type BuildService struct {
	builder     ports.DockerBuilder
	shellProber ports.DockerShellProber // optional; nil disables shell probe
}

// NewBuildService creates a new BuildService.
func NewBuildService(builder ports.DockerBuilder) *BuildService {
	return &BuildService{builder: builder}
}

// WithShellProber attaches a DockerShellProber to the BuildService.
// When set, Run probes the built image for /bin/sh after a successful build
// and prints a warning when no shell is found.
func (s *BuildService) WithShellProber(prober ports.DockerShellProber) *BuildService {
	s.shellProber = prober
	return s
}

// BuildOptions holds options for the build command.
type BuildOptions struct {
	// NoCache passes --no-cache to docker build when true.
	NoCache bool

	// ConfigPath is the path to vibewarden.yaml. Empty means the default
	// discovery logic (current directory) applies.
	ConfigPath string

	// WorkDir is the directory used both as the Docker build context and as
	// the fallback source of the image name. Defaults to "." when empty.
	WorkDir string
}

// Run executes the docker build command.
// It loads the config to resolve the image name, prints the resolved tag to
// out, then invokes the DockerBuilder. cfg may be nil when vibewarden.yaml is
// absent; in that case the directory name is used as the image name.
func (s *BuildService) Run(ctx context.Context, cfg *config.Config, opts BuildOptions, out io.Writer) error {
	workDir := opts.WorkDir
	if workDir == "" {
		workDir = "."
	}

	tag, err := resolveImageTag(cfg, workDir)
	if err != nil {
		return fmt.Errorf("resolving image tag: %w", err)
	}

	fmt.Fprintf(out, "Building Docker image: %s\n", tag)
	fmt.Fprintf(out, "Context: %s\n", workDir)
	if opts.NoCache {
		fmt.Fprintln(out, "Flags: --no-cache")
	}

	if err := s.builder.Build(ctx, tag, workDir, opts.NoCache); err != nil {
		return err
	}

	fmt.Fprintf(out, "Successfully built: %s\n", tag)

	// Post-build: probe the image for /bin/sh so we can warn about healthcheck
	// compatibility. Distroless and scratch images have no shell, which means
	// the generated docker-compose healthcheck (CMD-SHELL) will fail at runtime.
	s.probeShell(ctx, tag, out)

	return nil
}

// probeShell checks whether the built image contains /bin/sh and prints a
// warning when it does not. The probe is best-effort: errors from the prober
// are silently ignored so they never fail the build.
func (s *BuildService) probeShell(ctx context.Context, image string, out io.Writer) {
	if s.shellProber == nil {
		return
	}

	hasShell, err := s.shellProber.HasShell(ctx, image)
	if err != nil {
		// Probe failure is not fatal — just skip.
		return
	}
	if !hasShell {
		fmt.Fprintln(out, "")
		fmt.Fprintln(out, "Warning: your app image has no shell (/bin/sh). The generated healthcheck")
		fmt.Fprintln(out, "requires a shell. Switch to an Alpine-based image or add a custom healthcheck")
		fmt.Fprintln(out, "in your Dockerfile.")
	}
}

// resolveImageTag returns the Docker image tag for the build.
// The tag must match what docker-compose expects for the app service so that
// `vibew build` followed by `vibew dev` or `vibew deploy` finds the image.
//
// Docker Compose names built images as "<project>-<service>:latest". Since the
// app service is always called "app", the expected image name is
// "<ComposeProjectName>-app:latest". This function uses the same derivation
// logic as the deploy bundle to ensure consistency.
//
// Priority:
//  1. cfg.ComposeProjectName() + "-app:latest" when cfg is non-nil and has a
//     project name or image. This matches Docker Compose's naming convention.
//  2. Base name of workDir (directory name), normalised to lower-case with
//     "-app:latest" appended.
func resolveImageTag(cfg *config.Config, workDir string) (string, error) {
	if cfg != nil {
		name := cfg.ComposeProjectName()
		if name != "" && name != "vibewarden" {
			return name + "-app:latest", nil
		}
		// When ComposeProjectName() returns "vibewarden" (the fallback), it means
		// neither name nor image is set. Fall through to directory-based derivation
		// so the tag is project-specific rather than the generic fallback.
	}

	abs, err := filepath.Abs(workDir)
	if err != nil {
		return "", fmt.Errorf("resolving work directory: %w", err)
	}

	name := strings.ToLower(filepath.Base(abs))
	if name == "" || name == "." {
		return "", fmt.Errorf("cannot derive image name from directory %q", workDir)
	}

	return name + "-app:latest", nil
}
