package ports

import (
	"context"
	"io"
)

// ComposeLogsStreamOptions holds the parameters for a streaming docker compose
// logs invocation.
type ComposeLogsStreamOptions struct {
	// ProjectName is the Docker Compose project name passed via -p.
	ProjectName string
	// ComposeFile is the path to the docker-compose.yml file passed via -f.
	ComposeFile string
	// Services limits the log output to the named compose services.
	// When empty, logs for all services are returned.
	Services []string
	// Tail is the number of lines to show from the end of the log for each
	// container. Use -1 to show all lines. 0 delegates to docker compose's
	// own default.
	Tail int
	// Follow streams log output continuously (--follow).
	Follow bool
	// Since shows logs since a relative duration or RFC3339 timestamp. The
	// value is passed verbatim to docker compose — no client-side parsing is
	// performed. An empty string omits the flag.
	Since string
	// Stdout is the writer that receives docker compose stdout. It should be
	// the CLI's inherited stdout so output streams directly to the terminal.
	Stdout io.Writer
	// Stderr is the writer that receives docker compose stderr. It should be
	// the CLI's inherited stderr; the adapter also tees stderr into a small
	// internal buffer to detect daemon-unavailable conditions.
	Stderr io.Writer
}

// ComposeLogsStreamer streams docker compose logs to the writers in opts.
// Implementations shell out to "docker compose logs" with the supplied flags.
type ComposeLogsStreamer interface {
	// Stream runs "docker compose -p <project> -f <file> logs [flags]
	// [services...]" and writes output directly to opts.Stdout and opts.Stderr.
	// It returns nil when the command completes successfully or when the
	// context is cancelled (Ctrl-C during --follow). It returns
	// ErrDockerUnavailable when the daemon cannot be reached.
	Stream(ctx context.Context, opts ComposeLogsStreamOptions) error
}
