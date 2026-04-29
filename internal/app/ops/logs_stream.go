package ops

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/vibewarden/vibewarden/internal/config"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// ErrStackNotRunning is returned by LogsStreamService.Run when the dev stack
// has not been started or the generated compose file does not exist. The
// recovery action is always "vibew dev".
var ErrStackNotRunning = errors.New("stack is not running")

// ErrUnknownService is returned by LogsStreamService.Run when a requested
// service name is not found in the generated compose file.
type ErrUnknownService struct {
	// Service is the unrecognised service name the caller requested.
	Service string
	// Known is the sorted list of valid service names from the compose file.
	Known []string
}

// Error returns the user-facing error message listing known services.
func (e *ErrUnknownService) Error() string {
	return fmt.Sprintf("unknown service %q. Known: %s", e.Service, strings.Join(e.Known, ", "))
}

// LogsStreamOptions holds the caller-supplied parameters for a streaming logs
// invocation.
type LogsStreamOptions struct {
	// Services is the list of compose service names to include. An empty
	// slice means all services.
	Services []string
	// Tail is the number of recent log lines to show per container.
	// -1 means all. 0 delegates to docker compose's own default.
	Tail int
	// Follow streams output continuously.
	Follow bool
	// Since shows logs since a relative duration or RFC3339 timestamp.
	// The value is passed verbatim to docker compose.
	Since string
}

// LogsStreamService orchestrates the "vibew logs" use case.
//
// It resolves the generated compose file path, validates requested service
// names against the compose file, detects whether the stack is running, and
// delegates to a ComposeLogsStreamer.
type LogsStreamService struct {
	compose  ports.ComposeRunner
	streamer ports.ComposeLogsStreamer
}

// NewLogsStreamService creates a LogsStreamService.
func NewLogsStreamService(compose ports.ComposeRunner, streamer ports.ComposeLogsStreamer) *LogsStreamService {
	return &LogsStreamService{compose: compose, streamer: streamer}
}

// Run executes the "vibew logs" flow:
//  1. Resolve the generated compose file path.
//  2. If the compose file does not exist → ErrStackNotRunning.
//  3. Parse known services from the compose file.
//  4. Validate every service in opts.Services against known services.
//  5. Check whether the stack is running via compose.PS.
//  6. Call streamer.Stream with the resolved options.
//
// stdout and stderr are the writers that receive docker compose output. In
// CLI usage these are the inherited terminal writers.
func (s *LogsStreamService) Run(ctx context.Context, cfg *config.Config, opts LogsStreamOptions, stdout, stderr io.Writer) error {
	composeFile := filepath.Join(generatedOutputDir, "docker-compose.yml")

	// Step 1: compose file must exist before we do anything else.
	knownServices, err := ServicesFromComposeFile(composeFile)
	if err != nil {
		// Missing compose file means the stack has never been started.
		return ErrStackNotRunning
	}

	// Step 2: validate every requested service against known services.
	for _, svc := range opts.Services {
		if !containsString(knownServices, svc) {
			return &ErrUnknownService{Service: svc, Known: knownServices}
		}
	}

	// Step 3: check that the stack is actually running.
	containers, err := s.compose.PS(ctx, composeFile)
	if err != nil {
		// PS failure most likely means docker is unavailable or the project
		// does not exist. Treat as not running.
		return ErrStackNotRunning
	}
	if len(containers) == 0 {
		return ErrStackNotRunning
	}

	// Step 4: stream logs.
	projectName := cfg.ComposeProjectName()
	streamOpts := ports.ComposeLogsStreamOptions{
		ProjectName: projectName,
		ComposeFile: composeFile,
		Services:    opts.Services,
		Tail:        opts.Tail,
		Follow:      opts.Follow,
		Since:       opts.Since,
		Stdout:      stdout,
		Stderr:      stderr,
	}
	return s.streamer.Stream(ctx, streamOpts)
}

// containsString reports whether s is present in slice.
func containsString(slice []string, s string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
}
