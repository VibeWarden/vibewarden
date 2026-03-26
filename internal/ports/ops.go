// Package ports defines the interfaces (ports) for VibeWarden's hexagonal architecture.
package ports

import "context"

// ComposeRunner runs Docker Compose commands.
// Implementations shell out to the docker compose CLI.
type ComposeRunner interface {
	// Up starts services defined in the compose file.
	// profiles is a list of compose profiles to activate (e.g. "observability").
	// The output of the command is streamed to the caller via the returned channel.
	Up(ctx context.Context, profiles []string) error

	// Version returns the docker compose version string.
	// Returns an error when docker compose is not available.
	Version(ctx context.Context) (string, error)

	// Info returns the docker info output.
	// Returns an error when Docker is not running.
	Info(ctx context.Context) error
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
