package ops

import (
	"context"
	"fmt"
	"os/exec"
)

// BuildAdapter implements ports.DockerBuilder by shelling out to the docker CLI.
type BuildAdapter struct{}

// NewBuildAdapter creates a new BuildAdapter.
func NewBuildAdapter() *BuildAdapter {
	return &BuildAdapter{}
}

// Build runs "docker build -t <tag> [--no-cache] <contextDir>".
// Output from the command is streamed directly to stdout/stderr so the user
// sees progress in real time.
func (b *BuildAdapter) Build(ctx context.Context, tag string, contextDir string, noCache bool) error {
	args := []string{"build", "-t", tag}
	if noCache {
		args = append(args, "--no-cache")
	}
	args = append(args, contextDir)

	cmd := exec.CommandContext(ctx, "docker", args...) //nolint:gosec // "docker" is a hardcoded binary name; args are constructed from operator-controlled tag and contextDir, not user input
	// Inherit the parent process's file descriptors for live output.
	cmd.Stdout = nil
	cmd.Stderr = nil

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker build: %w", err)
	}
	return nil
}
