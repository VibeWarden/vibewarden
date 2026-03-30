package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	credentialsadapter "github.com/vibewarden/vibewarden/internal/adapters/credentials"
	opsadapter "github.com/vibewarden/vibewarden/internal/adapters/ops"
	templateadapter "github.com/vibewarden/vibewarden/internal/adapters/template"
	generateapp "github.com/vibewarden/vibewarden/internal/app/generate"
	opsapp "github.com/vibewarden/vibewarden/internal/app/ops"
	"github.com/vibewarden/vibewarden/internal/config"
	configtemplates "github.com/vibewarden/vibewarden/internal/config/templates"
)

// NewDevCmd creates the "vibewarden dev" subcommand.
//
// The command generates runtime config files under .vibewarden/generated/,
// then starts the Docker Compose dev environment in detached mode and
// prints the running service URLs.  Pass --observability to also start the
// Prometheus + Grafana observability stack.  Pass --watch to watch
// vibewarden.yaml for changes and auto-regenerate + restart the stack.
func NewDevCmd() *cobra.Command {
	var (
		observability bool
		watch         bool
		configPath    string
	)

	cmd := &cobra.Command{
		Use:   "dev",
		Short: "Start the local dev environment",
		Long: `Start the VibeWarden Docker Compose dev environment in detached mode.

When vibewarden.yaml is present, VibeWarden generates runtime configuration
files under .vibewarden/generated/ before starting the stack.

The baseline stack includes:
  - VibeWarden proxy (port 8443, HTTPS with self-signed certificate)
  - Ory Kratos identity server (ports 4433, 4434)
  - PostgreSQL
  - Mailslurper (email sink)

Pass --observability to also start Prometheus and Grafana.
Pass --watch to watch vibewarden.yaml for changes and automatically
regenerate config files and restart the stack (blocks until Ctrl+C).

Examples:
  vibewarden dev
  vibewarden dev --observability
  vibewarden dev --watch
  vibewarden dev --config ./my-vibewarden.yaml`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := config.Load(configPath)
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}

			compose := opsadapter.NewComposeAdapter()
			renderer := templateadapter.NewRenderer(configtemplates.FS)
			generator := generateapp.NewServiceWithCredentials(
				renderer,
				credentialsadapter.NewGenerator(),
				credentialsadapter.NewStore(),
			)

			var svc *opsapp.DevService
			if watch {
				watcher := opsadapter.NewFsnotifyWatcher()
				svc = opsapp.NewDevServiceWithWatcher(compose, generator, watcher)
			} else {
				svc = opsapp.NewDevServiceWithGenerator(compose, generator)
			}

			opts := opsapp.DevOptions{
				Observability: observability,
				Watch:         watch,
				ConfigPath:    configPath,
			}

			return svc.Run(cmd.Context(), cfg, opts, cmd.OutOrStdout())
		},
	}

	cmd.Flags().BoolVar(&observability, "observability", false, "start Prometheus and Grafana alongside the core stack")
	cmd.Flags().BoolVar(&watch, "watch", false, "watch vibewarden.yaml for changes and auto-regenerate + restart")
	cmd.Flags().StringVar(&configPath, "config", "", "path to vibewarden.yaml (default: ./vibewarden.yaml)")

	if err := cmd.RegisterFlagCompletionFunc("config", func(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
		return []string{"yaml", "yml"}, cobra.ShellCompDirectiveFilterFileExt
	}); err != nil {
		// registration can only fail when called on a non-existent flag; safe to ignore
		fmt.Fprintln(os.Stderr, "warning: flag completion registration failed:", err)
	}

	return cmd
}
