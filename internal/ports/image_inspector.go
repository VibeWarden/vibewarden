package ports

import (
	"context"
	"errors"
	"time"
)

// ErrImageNotFound is returned by ImageInspector.Inspect when the named image
// is absent from the local Docker daemon. This is a user-correctable condition
// (build or pull the image) and is mapped to exit code 2 by the CLI layer.
var ErrImageNotFound = errors.New("docker image not found")

// ErrDockerUnavailable is returned by ImageInspector.Inspect when the Docker
// daemon is unreachable (not running, or docker CLI not installed). Mapped to
// exit code 3 by the CLI layer.
var ErrDockerUnavailable = errors.New("docker daemon unavailable")

// ImageInfo is the value object returned by ImageInspector.Inspect. It is a
// pure, serialisable view over `docker image inspect` output. All fields are
// populated on success; callers must treat a zero value as "missing" only when
// ErrImageNotFound was returned from Inspect.
type ImageInfo struct {
	// Tag is the fully qualified image reference, e.g. "qr-van-gogh-app:latest".
	Tag string
	// Digest is the content-addressable digest, e.g. "sha256:abc123…".
	Digest string
	// OS is the image OS, typically "linux".
	OS string
	// Architecture is the image CPU architecture, e.g. "amd64", "arm64".
	Architecture string
	// Created is the image creation timestamp in UTC.
	Created time.Time
	// SizeBytes is the uncompressed image size in bytes.
	SizeBytes int64
	// Labels is the map of OCI/Docker labels stamped on the image. Keys follow
	// reverse-DNS convention (e.g. "org.vibewarden.project-root-hash"). Always
	// a non-nil map after a successful Inspect — callers may iterate without a
	// nil guard. Empty when the image carries no labels.
	Labels map[string]string
}

// Platform returns the canonical "<os>/<arch>" platform string used by Docker
// and Docker Buildx (e.g. "linux/amd64", "linux/arm64").
func (i ImageInfo) Platform() string {
	if i.OS == "" && i.Architecture == "" {
		return ""
	}
	return i.OS + "/" + i.Architecture
}

// ImageInspector returns metadata from `docker image inspect <tag>` as a
// pure-Go value object. Implementations shell out to the docker CLI.
//
// Inspect returns ErrImageNotFound when the image is absent from the local
// daemon (a user-correctable condition, mapped to a distinct exit code by
// the CLI layer). It returns ErrDockerUnavailable when the daemon is
// unreachable. Any other error is wrapped and surfaced as a generic failure.
type ImageInspector interface {
	// Inspect retrieves metadata for the named image tag.
	Inspect(ctx context.Context, tag string) (ImageInfo, error)
}
