package ops

import (
	"context"
	"fmt"
	"io"
	"path/filepath"

	"github.com/vibewarden/vibewarden/internal/ports"
)

// RestartService orchestrates the "vibew restart" use case.
// It rebuilds and recreates containers using "docker compose up -d
// --force-recreate --build" so that Dockerfile changes are picked up.
type RestartService struct {
	compose ports.ComposeRunner
}

// NewRestartService creates a new RestartService.
func NewRestartService(compose ports.ComposeRunner) *RestartService {
	return &RestartService{compose: compose}
}

// Run rebuilds and recreates the compose stack (or a subset of services) using
// the generated compose file under .vibewarden/generated/.
// When services is empty all services are rebuilt and recreated.
// When services is non-empty only those named services are affected.
func (s *RestartService) Run(ctx context.Context, services []string, out io.Writer) error {
	composeFile := filepath.Join(generatedOutputDir, "docker-compose.yml")

	if len(services) == 0 {
		fmt.Fprintln(out, "Rebuilding and restarting all services...")
	} else {
		fmt.Fprintf(out, "Rebuilding and restarting service(s): %v...\n", services)
	}

	if err := s.compose.Restart(ctx, composeFile, services); err != nil {
		fmt.Fprintln(out, "")
		fmt.Fprintln(out, "Restart failed. Run 'vibew logs' or 'vibew doctor' to diagnose.")
		return fmt.Errorf("restarting services: %w", err)
	}

	fmt.Fprintln(out, "Done.")
	return nil
}
