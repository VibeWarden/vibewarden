package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	opsadapter "github.com/vibewarden/vibewarden/internal/adapters/ops"
	opsapp "github.com/vibewarden/vibewarden/internal/app/ops"
	"github.com/vibewarden/vibewarden/internal/config"
)

// NewBuildCmd creates the "vibew build" subcommand.
//
// The command resolves the Docker image tag from vibewarden.yaml (app.image)
// or falls back to the current directory name, then runs "docker build -t
// <tag> .". Pass --no-cache to force a full rebuild without layer caching.
func NewBuildCmd() *cobra.Command {
	var (
		noCache    bool
		configPath string
	)

	cmd := &cobra.Command{
		Use:   "build",
		Short: "Build the app Docker image",
		Long: `Build the application Docker image using the project name as the tag.

The image tag is resolved from vibewarden.yaml (app.image field). When
vibewarden.yaml is absent or app.image is not set, the current directory
name is used with the ":latest" suffix.

Examples:
  vibew build
  vibew build --no-cache
  vibew build --config ./my-vibewarden.yaml`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := config.Load(configPath)
			if err != nil {
				// Config is optional for this command — proceed without it.
				cfg = nil
			}

			builder := opsadapter.NewBuildAdapter()
			svc := opsapp.NewBuildService(builder)

			opts := opsapp.BuildOptions{
				NoCache:    noCache,
				ConfigPath: configPath,
			}

			if err := svc.Run(cmd.Context(), cfg, opts, cmd.OutOrStdout()); err != nil {
				return fmt.Errorf("build: %w", err)
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&noCache, "no-cache", false, "do not use Docker layer cache (passes --no-cache to docker build)")
	cmd.Flags().StringVar(&configPath, "config", "", "path to vibewarden.yaml (default: ./vibewarden.yaml)")

	return cmd
}
