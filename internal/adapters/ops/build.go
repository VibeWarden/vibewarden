package ops

import (
	"context"
	"fmt"
	"os/exec"
	"sort"

	"github.com/vibewarden/vibewarden/internal/ports"
)

// BuildAdapter implements ports.DockerBuilder by shelling out to the docker CLI.
type BuildAdapter struct{}

// NewBuildAdapter creates a new BuildAdapter.
func NewBuildAdapter() *BuildAdapter {
	return &BuildAdapter{}
}

// Build runs "docker build -t <tag> [--platform <platform>] [--no-cache]
// [--label key=value ...] <contextDir>".
// Labels are emitted in alphabetical key order for deterministic argument
// construction. Output from the command is streamed directly to stdout/stderr
// so the user sees progress in real time.
func (b *BuildAdapter) Build(ctx context.Context, tag string, contextDir string, opts ports.DockerBuildOptions) error {
	args := buildDockerArgs(tag, contextDir, opts)

	cmd := exec.CommandContext(ctx, "docker", args...) //nolint:gosec // "docker" is a hardcoded binary name; args are constructed from operator-controlled tag and contextDir, not user input
	// Inherit the parent process's file descriptors for live output.
	cmd.Stdout = nil
	cmd.Stderr = nil

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker build: %w", err)
	}
	return nil
}

// buildDockerArgs constructs the argument slice for "docker build". Extracted
// as a pure function so that tests can assert arg construction without shelling
// out to Docker.
func buildDockerArgs(tag, contextDir string, opts ports.DockerBuildOptions) []string {
	args := []string{"build"}
	if opts.Platform != "" {
		args = append(args, "--platform", opts.Platform)
	}
	args = append(args, "-t", tag)
	if opts.NoCache {
		args = append(args, "--no-cache")
	}

	// Emit labels in alphabetical key order for deterministic test assertions.
	keys := make([]string, 0, len(opts.Labels))
	for k := range opts.Labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		args = append(args, "--label", k+"="+opts.Labels[k])
	}

	args = append(args, contextDir)
	return args
}
