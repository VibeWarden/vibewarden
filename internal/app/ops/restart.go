package ops

import (
	"context"
	"fmt"
	"io"
	"path/filepath"

	"github.com/vibewarden/vibewarden/internal/ports"
)

// RestartService orchestrates the "vibew restart" use case.
// It restarts one or more services in the Docker Compose stack without
// rebuilding or recreating containers.
type RestartService struct {
	compose ports.ComposeRunner
}

// NewRestartService creates a new RestartService.
func NewRestartService(compose ports.ComposeRunner) *RestartService {
	return &RestartService{compose: compose}
}

// Run restarts the compose stack (or a subset of services) using the generated
// compose file under .vibewarden/generated/.
// When services is empty all services are restarted.
// When services is non-empty only those named services are restarted.
func (s *RestartService) Run(ctx context.Context, services []string, out io.Writer) error {
	composeFile := filepath.Join(generatedOutputDir, "docker-compose.yml")

	if len(services) == 0 {
		fmt.Fprintln(out, "Restarting all services...")
	} else {
		fmt.Fprintf(out, "Restarting service(s): %v...\n", services)
	}

	if err := s.compose.Restart(ctx, composeFile, services); err != nil {
		return fmt.Errorf("restarting services: %w", err)
	}

	fmt.Fprintln(out, "Done.")
	return nil
}
