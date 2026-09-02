package ops

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/vibewarden/vibewarden/internal/ports"
)

// dockerInspectTimeout bounds a single `docker image inspect` shell-out. A
// healthy local daemon answers in well under 200ms; the generous ceiling exists
// only so that an unresponsive daemon (common while Docker Desktop restarts)
// cannot hang `vibew dev` indefinitely (#1309).
const dockerInspectTimeout = 10 * time.Second

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
	Config       struct {
		Labels map[string]string `json:"Labels"`
	} `json:"Config"`
}

// Inspect runs `docker image inspect --format '{{json .}}' <tag>` and parses
// the result into a ports.ImageInfo value object.
//
// The call is bounded by dockerInspectTimeout unless the caller already set a
// deadline, so an unresponsive daemon fails fast instead of blocking forever.
//
// Error mapping:
//   - stderr contains "No such image" → ports.ErrImageNotFound
//   - the inspect deadline expired → ports.ErrDockerDaemonNotRunning
//     (and therefore ports.ErrDockerUnavailable)
//   - docker command not found OR "Cannot connect to the Docker daemon" →
//     ports.ErrDockerUnavailable
//   - anything else → wrapped with fmt.Errorf("docker image inspect: %w", err)
func (a *ImageInspectAdapter) Inspect(ctx context.Context, tag string) (ports.ImageInfo, error) {
	inspectCtx, cancel := inspectContext(ctx)
	defer cancel()

	//nolint:gosec // "docker" is a hardcoded binary; tag is operator-controlled image reference
	cmd := exec.CommandContext(inspectCtx, "docker", "image", "inspect", "--format", "{{json .}}", tag)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		stderrStr := stderr.String()
		if strings.Contains(stderrStr, "No such image") {
			return ports.ImageInfo{}, ports.ErrImageNotFound
		}
		// A blown deadline means the daemon never answered. Report it as
		// "daemon not running" so callers that already degrade gracefully on
		// ports.ErrDockerUnavailable (preflight WARN, dev identity check skip)
		// treat a frozen daemon the same way as an absent one.
		if errors.Is(inspectCtx.Err(), context.DeadlineExceeded) {
			return ports.ImageInfo{}, &ports.DockerUnavailableError{
				Sentinel: ports.ErrDockerDaemonNotRunning,
				Stderr:   stderrStr,
				Cause:    fmt.Errorf("docker image inspect timed out: %w", context.DeadlineExceeded),
			}
		}
		// ClassifyDockerError is the single canonical seam for all docker error
		// classification — binary-not-found (via err.Error()), daemon-unavailable,
		// and socket-permission checks are all handled there.
		classified := ClassifyDockerError(err, stderrStr)
		if classified != err {
			return ports.ImageInfo{}, classified
		}
		return ports.ImageInfo{}, fmt.Errorf("docker image inspect: %w\nstderr: %s", err, stderrStr)
	}

	info, err := parseInspectJSON(stdout.Bytes())
	if err != nil {
		return ports.ImageInfo{}, fmt.Errorf("docker image inspect: %w", err)
	}
	info.Tag = tag
	return info, nil
}

// inspectContext derives the context used for the docker shell-out. When the
// caller already imposed a deadline it is honoured unchanged; otherwise
// dockerInspectTimeout is applied. The returned cancel func is always non-nil
// and must be called by the caller.
func inspectContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if _, hasDeadline := ctx.Deadline(); hasDeadline {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, dockerInspectTimeout)
}

// parseInspectJSON parses the raw JSON blob produced by
// `docker image inspect --format '{{json .}}'` into a ports.ImageInfo.
// The tag field is NOT set here — callers must assign it after calling.
// This helper is separated from Inspect so that export_test.go can expose it
// for unit tests that exercise the JSON→ImageInfo path without shelling out to
// Docker (ADR-100 test-strategy requirement).
func parseInspectJSON(jsonBlob []byte) (ports.ImageInfo, error) {
	var out dockerInspectOutput
	if err := json.Unmarshal(jsonBlob, &out); err != nil {
		return ports.ImageInfo{}, fmt.Errorf("parsing JSON: %w", err)
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

	labels := out.Config.Labels
	if labels == nil {
		labels = make(map[string]string)
	}

	return ports.ImageInfo{
		Digest:       digest,
		OS:           out.Os,
		Architecture: out.Architecture,
		Created:      created.UTC(),
		SizeBytes:    out.Size,
		Labels:       labels,
	}, nil
}
