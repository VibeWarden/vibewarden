package ops

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/vibewarden/vibewarden/internal/ports"
)

// ImageInspectAdapter implements ports.ImageInspector by shelling out to the
// docker CLI. It matches the shell-out pattern established by ImageExportAdapter.
type ImageInspectAdapter struct{}

// NewImageInspectAdapter creates a new ImageInspectAdapter.
func NewImageInspectAdapter() *ImageInspectAdapter {
	return &ImageInspectAdapter{}
}

// dockerInspectOutput is the subset of `docker image inspect` JSON we consume.
// Docker outputs a JSON array; we parse element [0].
type dockerInspectOutput struct {
	ID           string   `json:"Id"`
	Architecture string   `json:"Architecture"`
	Os           string   `json:"Os"`
	Created      string   `json:"Created"`
	Size         int64    `json:"Size"`
	RepoDigests  []string `json:"RepoDigests"`
}

// Inspect runs `docker image inspect --format '{{json .}}' <tag>` and parses
// the result into a ports.ImageInfo value object.
//
// Error mapping:
//   - stderr contains "No such image" → ports.ErrImageNotFound
//   - docker command not found OR "Cannot connect to the Docker daemon" →
//     ports.ErrDockerUnavailable
//   - anything else → wrapped with fmt.Errorf("docker image inspect: %w", err)
func (a *ImageInspectAdapter) Inspect(ctx context.Context, tag string) (ports.ImageInfo, error) {
	//nolint:gosec // "docker" is a hardcoded binary; tag is operator-controlled image reference
	cmd := exec.CommandContext(ctx, "docker", "image", "inspect", "--format", "{{json .}}", tag)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		stderrStr := stderr.String()
		// Check for docker not being found (exec.ErrNotFound) or explicit
		// "executable file not found" before checking stderr content.
		if isDockerNotFound(err) || strings.Contains(stderrStr, "Cannot connect to the Docker daemon") {
			return ports.ImageInfo{}, ports.ErrDockerUnavailable
		}
		if strings.Contains(stderrStr, "No such image") {
			return ports.ImageInfo{}, ports.ErrImageNotFound
		}
		return ports.ImageInfo{}, fmt.Errorf("docker image inspect: %w\nstderr: %s", err, stderrStr)
	}

	var out dockerInspectOutput
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		return ports.ImageInfo{}, fmt.Errorf("docker image inspect: parsing JSON: %w", err)
	}

	created, err := time.Parse(time.RFC3339Nano, out.Created)
	if err != nil {
		// Attempt fallback formats docker may use.
		created, err = time.Parse("2006-01-02T15:04:05.999999999Z", out.Created)
		if err != nil {
			created = time.Time{}
		}
	}

	digest := ""
	if len(out.RepoDigests) > 0 {
		// RepoDigests entries look like "myapp@sha256:abc123". Extract just the digest.
		parts := strings.SplitN(out.RepoDigests[0], "@", 2)
		if len(parts) == 2 {
			digest = parts[1]
		} else {
			digest = out.RepoDigests[0]
		}
	}
	if digest == "" {
		// Fall back to the image ID when no repo digest is available (local
		// build that was never pushed).
		digest = out.ID
	}

	return ports.ImageInfo{
		Tag:          tag,
		Digest:       digest,
		OS:           out.Os,
		Architecture: out.Architecture,
		Created:      created.UTC(),
		SizeBytes:    out.Size,
	}, nil
}

// isDockerNotFound reports whether the error indicates the docker binary was
// not found on PATH.
func isDockerNotFound(err error) bool {
	if err == nil {
		return false
	}
	if strings.Contains(err.Error(), "executable file not found") {
		return true
	}
	// exec.ErrNotFound is set when LookPath fails.
	if strings.Contains(err.Error(), "exec: \"docker\": executable file not found in $PATH") {
		return true
	}
	return false
}
