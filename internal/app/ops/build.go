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
type BuildService struct {
	builder ports.DockerBuilder
}

// NewBuildService creates a new BuildService.
func NewBuildService(builder ports.DockerBuilder) *BuildService {
	return &BuildService{builder: builder}
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
	return nil
}

// resolveImageTag returns the Docker image tag for the build.
// Priority:
//  1. cfg.App.Image when cfg is non-nil and non-empty.
//  2. Base name of workDir (directory name), normalised to lower-case with
//     ":latest" appended.
func resolveImageTag(cfg *config.Config, workDir string) (string, error) {
	if cfg != nil && cfg.App.Image != "" {
		return cfg.App.Image, nil
	}

	abs, err := filepath.Abs(workDir)
	if err != nil {
		return "", fmt.Errorf("resolving work directory: %w", err)
	}

	name := strings.ToLower(filepath.Base(abs))
	if name == "" || name == "." {
		return "", fmt.Errorf("cannot derive image name from directory %q", workDir)
	}

	return name + ":latest", nil
}
