// Package ports defines the interfaces (ports) for VibeWarden's hexagonal architecture.
package ports

import (
	"context"
	"io"
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

// ComposeUpOptions carries optional arguments for ComposeRunner.Up. It is
// defined as a struct (rather than positional parameters) so that future flags
// can be added without breaking callers.
type ComposeUpOptions struct {
	// Stderr, when non-nil, receives a live copy of the docker compose stderr
	// stream while the command runs. Use this to surface build progress or
	// diagnostics to the user in real time (e.g. when --verbose is set).
	// When nil, stderr is captured into an internal buffer that is written to
	// the adapter's default stderr only on command failure.
	Stderr io.Writer
}

// ComposeDownOptions carries optional arguments for ComposeRunner.Down.
type ComposeDownOptions struct {
	// Volumes controls whether named volumes declared in the compose file are
	// also removed. When Services is empty this is equivalent to
	// `docker compose down --volumes` and removes ALL project volumes —
	// callers are responsible for confirming destructive intent with the user.
	// When Services is non-empty, Volumes is honoured via a follow-up
	// `docker volume rm` step against VolumeNames (best-effort; errors from
	// "in use" or "no such volume" are silently tolerated).
	Volumes bool
	// RemoveOrphans, when true, removes containers for services not defined in
	// the current compose file (equivalent to `--remove-orphans`). This flag
	// is ignored when Services is non-empty (orphan removal is a project-level
	// concept that does not apply to service-targeted teardown).
	RemoveOrphans bool

	// Services limits teardown to the named compose services.
	//
	// When non-empty, the adapter performs a service-targeted teardown by
	// running `docker compose stop <services...>` followed by
	// `docker compose rm -f <services...>` instead of `docker compose down`.
	// This is the correct way to tear down a subset of a compose project —
	// `docker compose down --profile <name>` does NOT scope teardown by
	// profile (compose's --profile is an activation flag for `up`, not a
	// scope limiter for `down`) and would remove all services in the project.
	// When empty, behaviour is unchanged: full project teardown via
	// `docker compose down`.
	Services []string

	// VolumeNames lists the named volumes to remove when Volumes is true and
	// Services is non-empty. Each name is relative (e.g. "prometheus-data");
	// the adapter resolves the full volume name by prepending ProjectName + "_".
	// When VolumeNames is empty and Volumes is true with a non-empty Services
	// list, no volumes are removed (the caller must supply the list explicitly).
	VolumeNames []string

	// ProjectName is the Docker Compose project name used to construct the
	// fully-qualified volume name "<ProjectName>_<VolumeName>" when removing
	// volumes in a service-targeted teardown (Volumes true, Services non-empty).
	// It must match the `name:` field in the compose file (or the value Docker
	// derives from it). When empty and Volumes is true, no volumes are removed.
	ProjectName string
}

// DownResult summarises what happened during a ComposeRunner.Down invocation.
// Zero values indicate a no-op (nothing was running).
type DownResult struct {
	// StoppedContainers is the number of containers that were stopped/removed
	// by the down invocation. Zero means nothing was running.
	StoppedContainers int
	// RemovedVolumes is the number of named volumes removed. Always zero when
	// ComposeDownOptions.Volumes is false.
	RemovedVolumes int
}

// ComposeRunner runs Docker Compose commands.
// Implementations shell out to the docker compose CLI.
type ComposeRunner interface {
	// Up starts services defined in the compose file.
	// composeFile is the path to the docker-compose.yml to use; when empty
	// the default file discovery behaviour of docker compose applies.
	// profiles is a list of compose profiles to activate (e.g. "observability").
	// opts controls stderr handling — see ComposeUpOptions. On failure the
	// adapter writes captured stderr to its default stderr before returning.
	Up(ctx context.Context, composeFile string, profiles []string, opts ComposeUpOptions) error

	// Restart rebuilds and recreates services in the compose project using
	// docker compose up -d --force-recreate --build. This ensures Dockerfile
	// changes are picked up. composeFile is the path to the docker-compose.yml;
	// when empty the default discovery logic applies. services is the optional
	// list of service names to restart; when empty all services are restarted.
	Restart(ctx context.Context, composeFile string, services []string) error

	// Down stops and removes containers for the compose project. composeFile
	// is the path to the docker-compose.yml; when empty the default discovery
	// logic applies. opts controls --volumes and --remove-orphans.
	// When opts.Services is non-empty, teardown is scoped to those services
	// via stop+rm rather than a full project down — docker compose --profile
	// does NOT scope teardown by profile (it is an activation flag for up,
	// not a scope limiter for down), so Services is the correct mechanism.
	// When nothing is running, Down returns a zero-valued DownResult and a
	// nil error (idempotent no-op).
	Down(ctx context.Context, composeFile string, opts ComposeDownOptions) (DownResult, error)

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

// DockerBuildOptions holds optional arguments for DockerBuilder.Build. Defined
// as a struct so future flags can be added without breaking callers, following
// the ComposeUpOptions precedent.
type DockerBuildOptions struct {
	// NoCache passes --no-cache to docker build when true.
	NoCache bool
	// Platform specifies the target platform for the Docker build (e.g.
	// "linux/amd64"). When empty, docker uses the local platform.
	Platform string
	// Labels is a map of Docker label keys to values, applied to the built
	// image via repeated `--label key=value` arguments. Callers must not
	// assume label ordering is preserved — the adapter sorts by key for
	// deterministic argument construction.
	Labels map[string]string
}

// DockerBuilder runs "docker build" commands.
// Implementations shell out to the docker CLI.
type DockerBuilder interface {
	// Build runs "docker build -t <tag> <contextDir>" with the given options.
	// Output from the command is streamed to stdout/stderr so the user sees
	// progress in real time.
	Build(ctx context.Context, tag string, contextDir string, opts DockerBuildOptions) error
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

// PortOwner identifies who is listening on a TCP port. It is a value object
// returned by PortOwnerProbe implementations and consumed by diagnostic
// services that need to distinguish the sidecar from foreign processes.
type PortOwner string

const (
	// OwnerUnknown means the port is free or the owner could not be
	// determined (e.g. probe error on a reachable host).
	OwnerUnknown PortOwner = "unknown"
	// OwnerVibeWarden means a VibeWarden sidecar is listening and
	// responded on /_vibewarden/health with the expected JSON signature.
	OwnerVibeWarden PortOwner = "vibewarden"
	// OwnerForeign means some other process is listening — either the TLS
	// handshake failed, the HTTP status was non-2xx, or the response body
	// did not start with the VibeWarden health signature.
	OwnerForeign PortOwner = "foreign"
)

// PortOwnerProbe identifies the owner of a listener on host:port by probing
// the /_vibewarden/health endpoint over TLS. Implementations must be safe to
// call against self-signed certificates because those are the norm in local
// development.
type PortOwnerProbe interface {
	// ProbeOwner returns the identity of the process bound to host:port.
	// It never returns an error: ambiguous results are reported as
	// OwnerForeign (safe default) or OwnerUnknown (port free).
	ProbeOwner(ctx context.Context, host string, port int) PortOwner
}
