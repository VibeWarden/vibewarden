package ports

import (
	"context"

	"github.com/vibewarden/vibewarden/internal/config"
)

// ConfigGenerator generates VibeWarden runtime configuration files from a
// loaded Config. Implementations write files under the .vibewarden/generated/
// directory so that Docker Compose and Ory Kratos can pick them up.
type ConfigGenerator interface {
	// Generate creates or overwrites the generated configuration files for the
	// supplied cfg under outputDir. When outputDir is empty it defaults to
	// ".vibewarden/generated" relative to the current working directory.
	//
	// Generated files:
	//   <outputDir>/kratos/kratos.yml
	//   <outputDir>/kratos/identity.schema.json
	//   <outputDir>/docker-compose.yml
	Generate(ctx context.Context, cfg *config.Config, outputDir string) error
}
