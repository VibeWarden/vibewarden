package ops

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// ImageRemoveAdapter implements ports.DockerImageRemover by shelling out to
// the docker CLI. It follows the same shell-out pattern as ImageInspectAdapter.
type ImageRemoveAdapter struct{}

// NewImageRemoveAdapter creates a new ImageRemoveAdapter.
func NewImageRemoveAdapter() *ImageRemoveAdapter {
	return &ImageRemoveAdapter{}
}

// Remove runs "docker image rm <tag>". A missing image is treated as a
// successful no-op: when stderr contains "No such image" the adapter returns
// nil so callers on the rebuild path can always call Remove unconditionally
// without checking whether the image exists first.
//
// All other failures (e.g. image in use by a running container, daemon
// unreachable) are returned as wrapped errors.
func (a *ImageRemoveAdapter) Remove(ctx context.Context, tag string) error {
	args := buildImageRmArgs(tag)
	//nolint:gosec // "docker" is a hardcoded binary name; tag is operator-controlled image reference
	cmd := exec.CommandContext(ctx, "docker", args...)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err == nil {
		return nil
	}

	stderrStr := stderr.String()
	// "No such image" is idempotent success — image was already removed.
	if strings.Contains(stderrStr, "No such image") {
		return nil
	}

	return fmt.Errorf("docker image rm: %w\nstderr: %s", err, stderrStr)
}

// buildImageRmArgs constructs the argument slice for "docker image rm".
// Extracted as a pure function so tests can assert arg construction without
// shelling out to Docker (mirrors buildDockerArgs in build.go).
func buildImageRmArgs(tag string) []string {
	return []string{"image", "rm", tag}
}
