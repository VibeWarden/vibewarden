package ports

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// ErrImageNotFound is returned by ImageInspector.Inspect when the named image
// is absent from the local Docker daemon. This is a user-correctable condition
// (build or pull the image) and is mapped to exit code 2 by the CLI layer.
var ErrImageNotFound = errors.New("docker image not found")

// ErrDockerUnavailable is returned by ImageInspector.Inspect when the Docker
// daemon is unreachable (not running, or docker CLI not installed). Mapped to
// exit code 3 by the CLI layer.
//
// ErrDockerSocketPermission and ErrDockerDaemonNotRunning both wrap this
// sentinel so that existing errors.Is(err, ErrDockerUnavailable) callers
// continue to match without change.
var ErrDockerUnavailable = errors.New("docker daemon unavailable")

// ErrDockerSocketPermission is returned when docker stderr indicates the
// process lacks permission to access the docker socket (e.g. the user is not
// in the docker group on Linux, or Docker Desktop is not running on macOS).
var ErrDockerSocketPermission = fmt.Errorf("docker socket permission denied: %w", ErrDockerUnavailable)

// ErrDockerDaemonNotRunning is returned when docker stderr indicates the
// daemon is not reachable (not running, or no socket at all).
var ErrDockerDaemonNotRunning = fmt.Errorf("docker daemon not running: %w", ErrDockerUnavailable)

// DockerUnavailableError is returned by docker-shelling adapters when stderr
// matches a known unavailability signature. It carries the classified sentinel
// (ErrDockerSocketPermission or ErrDockerDaemonNotRunning), the raw docker
// stderr text (capped at 64 KiB) for display in the CLI renderer, and the
// original exec error.
//
// errors.Is(err, ErrDockerUnavailable), errors.Is(err, ErrDockerSocketPermission),
// and errors.Is(err, ErrDockerDaemonNotRunning) all work via the Unwrap chain.
// errors.As(err, &DockerUnavailableError{}) lets the CLI layer extract Stderr.
type DockerUnavailableError struct {
	// Sentinel is one of ErrDockerSocketPermission or ErrDockerDaemonNotRunning.
	Sentinel error
	// Stderr is the captured stderr text from the docker command (capped).
	Stderr string
	// Cause is the original exec error, preserved for callers that need it.
	Cause error
}

// Error implements the error interface. Returns the sentinel message so that
// log output remains human-readable without the stderr payload.
func (e *DockerUnavailableError) Error() string {
	return e.Sentinel.Error()
}

// Is reports whether e matches target. It delegates to errors.Is on the
// Sentinel so that errors.Is(err, ErrDockerUnavailable),
// errors.Is(err, ErrDockerSocketPermission), and
// errors.Is(err, ErrDockerDaemonNotRunning) all work as expected.
func (e *DockerUnavailableError) Is(target error) bool {
	return errors.Is(e.Sentinel, target)
}

// Unwrap returns the Sentinel, making the full ErrDockerUnavailable chain
// reachable via errors.Unwrap.
func (e *DockerUnavailableError) Unwrap() error {
	return e.Sentinel
}

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
