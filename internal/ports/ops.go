// Package ports defines the interfaces (ports) for VibeWarden's hexagonal architecture.
package ports

import (
	"context"
	"time"
)

// ContainerInfo holds the status of a single container reported by docker compose ps.
type ContainerInfo struct {
	// Name is the container name.
	Name string
	// Service is the Compose service name.
	Service string
	// State is the raw container state string (e.g. "running", "exited").
	State string
	// Health is the Docker health-check status ("healthy", "unhealthy", "starting", or empty).
	Health string
	// Image is the Docker image reference used by this container (e.g. "myapp:latest").
	Image string
	// Project is the Compose project name that owns this container.
	Project string
	// CreatedAt is the timestamp when the container was created. Zero value means unknown.
	CreatedAt time.Time
}

// ComposeRunner runs Docker Compose commands.
// Implementations shell out to the docker compose CLI.
type ComposeRunner interface {
	// Up starts services defined in the compose file.
	// composeFile is the path to the docker-compose.yml to use; when empty
	// the default file discovery behaviour of docker compose applies.
	// profiles is a list of compose profiles to activate (e.g. "observability").
	// The output of the command is streamed to the caller via the returned channel.
	Up(ctx context.Context, composeFile string, profiles []string) error

	// Restart rebuilds and recreates services in the compose project using
	// docker compose up -d --force-recreate --build. This ensures Dockerfile
	// changes are picked up. composeFile is the path to the docker-compose.yml;
	// when empty the default discovery logic applies. services is the optional
	// list of service names to restart; when empty all services are restarted.
	Restart(ctx context.Context, composeFile string, services []string) error

	// Version returns the docker compose version string.
	// Returns an error when docker compose is not available.
	Version(ctx context.Context) (string, error)

	// Info returns the docker info output.
	// Returns an error when Docker is not running.
	Info(ctx context.Context) error

	// PS returns the list of containers for the compose project.
	// composeFile is the path to the docker-compose.yml; when empty the default
	// discovery logic applies.  Returns an empty slice when no containers exist.
	PS(ctx context.Context, composeFile string) ([]ContainerInfo, error)

	// Logs returns the last tailLines lines of logs for the given service.
	// composeFile is the path to the docker-compose.yml; when empty the default
	// discovery logic applies.
	Logs(ctx context.Context, composeFile string, service string, tailLines int) (string, error)
}

// DockerBuilder runs "docker build" commands.
// Implementations shell out to the docker CLI.
type DockerBuilder interface {
	// Build runs "docker build -t <tag> <contextDir>".
	// When noCache is true the --no-cache flag is passed to docker build.
	// When platform is non-empty it is passed as --platform (e.g. "linux/amd64").
	// Output from the command is streamed to stdout/stderr so the user sees
	// progress in real time.
	Build(ctx context.Context, tag string, contextDir string, noCache bool, platform string) error
}

// HealthChecker performs HTTP health checks against VibeWarden endpoints.
type HealthChecker interface {
	// CheckHealth performs a GET request to the given URL and returns true
	// when the response status is 2xx. The context controls the timeout.
	CheckHealth(ctx context.Context, url string) (ok bool, statusCode int, err error)
}

// PortChecker verifies whether a TCP port is available (not in use).
type PortChecker interface {
	// IsPortAvailable returns true when nothing is listening on the given
	// host:port address.
	IsPortAvailable(ctx context.Context, host string, port int) (bool, error)
}

// DockerImageChecker checks whether a Docker image exists in the local daemon.
// Implementations shell out to the docker CLI.
type DockerImageChecker interface {
	// ImageExists returns true when the named image is present in the local
	// Docker image store. name is a full image reference such as "myapp:latest".
	// Returns an error only for unexpected failures (e.g. docker daemon
	// unreachable); a missing image is not an error — it returns (false, nil).
	ImageExists(ctx context.Context, name string) (bool, error)
}

// DockerShellProber checks whether a Docker image contains /bin/sh.
// Implementations shell out to the docker CLI.
type DockerShellProber interface {
	// HasShell returns true when the image contains a working /bin/sh.
	// Returns an error only for unexpected failures (e.g. docker daemon
	// unreachable); an image without a shell is not an error — it returns
	// (false, nil).
	HasShell(ctx context.Context, image string) (bool, error)
}

// ComposeLogs fetches recent log lines from a Docker Compose service.
// Implementations shell out to the docker compose CLI.
type ComposeLogs interface {
	// Tail returns the last n lines of logs from the specified service in the
	// given compose file. Returns an empty string when no logs are available.
	Tail(ctx context.Context, composeFile string, service string, n int) (string, error)
}

// ImageExporter saves a Docker image from the local daemon to a tar archive.
// Implementations shell out to the docker CLI ("docker save").
type ImageExporter interface {
	// Save exports the named Docker image to the file at destPath using
	// "docker save -o <destPath> <imageName>". The image must exist in the
	// local Docker daemon. Returns an error when the image is not found or
	// the docker CLI fails.
	Save(ctx context.Context, imageName, destPath string) error
}
