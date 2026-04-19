package ops

import (
	"context"
	"fmt"
	"os/exec"
)

// ImageExportAdapter implements ports.ImageExporter by shelling out to the
// docker CLI.
type ImageExportAdapter struct{}

// NewImageExportAdapter creates a new ImageExportAdapter.
func NewImageExportAdapter() *ImageExportAdapter {
	return &ImageExportAdapter{}
}

// Save exports the named Docker image to destPath using "docker save -o".
func (a *ImageExportAdapter) Save(ctx context.Context, imageName, destPath string) error {
	//nolint:gosec // "docker" is a hardcoded binary; args are operator-controlled
	cmd := exec.CommandContext(ctx, "docker", "save", "-o", destPath, imageName)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker save %s: %w\noutput: %s", imageName, err, string(output))
	}
	return nil
}
