package cmd

import (
	"os"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	credentialsadapter "github.com/vibewarden/vibewarden/internal/adapters/credentials"
	opsadapter "github.com/vibewarden/vibewarden/internal/adapters/ops"
	templateadapter "github.com/vibewarden/vibewarden/internal/adapters/template"
	generateapp "github.com/vibewarden/vibewarden/internal/app/generate"
	opsapp "github.com/vibewarden/vibewarden/internal/app/ops"
	configtemplates "github.com/vibewarden/vibewarden/internal/config/templates"
)

// NewObsCmd creates the "vibew obs" parent subcommand with "up" and "down"
// child commands. The obs subcommand controls the Prometheus + Grafana
// observability stack that is shipped as a Docker Compose profile overlay on
// the same project as the main dev stack.
func NewObsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "obs",
		Short: "Manage the local observability stack",
		Long: `Manage the Prometheus + Grafana observability stack.

The observability stack runs as a Docker Compose profile overlay on the same
project as the main dev stack started by 'vibew dev'. Services share the same
Docker network, so Prometheus can scrape VibeWarden without any extra bridging.

Subcommands:
  up    Start Prometheus and Grafana
  down  Stop Prometheus and Grafana

Examples:
  vibew obs up
  vibew obs down
  vibew obs down -v --yes`,
		// Print help when the parent command is invoked without a subcommand.
		Run: func(cmd *cobra.Command, _ []string) {
			cmd.Help() //nolint:errcheck
		},
	}

	cmd.AddCommand(newObsUpCmd())
	cmd.AddCommand(newObsDownCmd())

	return cmd
}

// newObsUpCmd creates the "vibew obs up" subcommand.
func newObsUpCmd() *cobra.Command {
	var (
		configPath string
		verbose    bool
	)

	cmd := &cobra.Command{
		Use:   "up",
		Short: "Start the observability stack",
		Long: `Start the Prometheus + Grafana observability stack.

The observability services are started as a Docker Compose profile overlay on
the same project as the main dev stack. They share the same Docker network, so
Prometheus can scrape the VibeWarden sidecar's /_vibewarden/metrics endpoint
immediately.

If 'vibew dev' has not been run first, the observability services will still
start but Prometheus scrape targets will be unreachable until the sidecar is
running. A friendly advisory is printed in that case.

Examples:
  vibew obs up
  vibew obs up --config ./my-vibewarden.yaml
  vibew obs up --verbose`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := requireScaffolding(); err != nil {
				return err
			}
			if err := requireConfig(configPath); err != nil {
				return err
			}

			cfg, err := loadAndResolve(cmd.Context(), configPath)
			if err != nil {
				return err
			}

			compose := opsadapter.NewComposeAdapter()
			renderer := templateadapter.NewRenderer(configtemplates.FS)
			generator := generateapp.NewServiceWithCredentials(
				renderer,
				credentialsadapter.NewGenerator(),
				credentialsadapter.NewStore(),
			)
			svc := opsapp.NewObsService(compose, generator)

			opts := opsapp.ObsUpOptions{
				ConfigPath: configPath,
				Verbose:    verbose,
			}
			return svc.Up(cmd.Context(), cfg, opts, cmd.OutOrStdout())
		},
	}

	cmd.Flags().StringVar(&configPath, "config", "", "path to vibewarden.yaml (default: ./vibewarden.yaml)")
	cmd.Flags().BoolVar(&verbose, "verbose", false, "stream docker compose stderr during startup")

	return cmd
}

// newObsDownCmd creates the "vibew obs down" subcommand.
func newObsDownCmd() *cobra.Command {
	var (
		configPath    string
		volumes       bool
		removeOrphans bool
		yes           bool
	)

	cmd := &cobra.Command{
		Use:   "down",
		Short: "Stop the observability stack",
		Long: `Stop the Prometheus + Grafana observability stack.

Idempotent: running on an already-stopped observability stack is a no-op (exit 0).

Flags:
  -v, --volumes         also remove named volumes (Grafana state, Prometheus data,
                        etc.). Prompts for confirmation in a TTY unless --yes is
                        also set.
      --remove-orphans  remove containers for services no longer in the compose file.
      --yes             skip the confirmation prompt for --volumes.

Examples:
  vibew obs down
  vibew obs down -v --yes
  vibew obs down --remove-orphans`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			compose := opsadapter.NewComposeAdapter()
			svc := opsapp.NewObsService(compose, nil)

			isTTY := term.IsTerminal(int(os.Stdout.Fd())) //nolint:gosec // file descriptor fits in int on all supported platforms

			// Derive the compose project name from the config so that
			// volume removal constructs the correct "<project>_<volume>"
			// reference. Config load is best-effort: if it fails we proceed
			// with an empty project name (volumes will be skipped, not silently
			// wrong). See the ProjectName field on ObsDownOptions.
			var projectName string
			if cfg, err := loadAndResolve(cmd.Context(), configPath); err == nil {
				projectName = cfg.ComposeProjectName()
			}

			opts := opsapp.ObsDownOptions{
				Volumes:       volumes,
				RemoveOrphans: removeOrphans,
				Yes:           yes,
				In:            os.Stdin,
				IsTTY:         isTTY,
				ProjectName:   projectName,
			}
			return svc.Down(cmd.Context(), opts, cmd.OutOrStdout())
		},
	}

	cmd.Flags().StringVar(&configPath, "config", "", "path to vibewarden.yaml (default: ./vibewarden.yaml)")
	cmd.Flags().BoolVarP(&volumes, "volumes", "v", false, "also remove named volumes (destructive)")
	cmd.Flags().BoolVar(&removeOrphans, "remove-orphans", false, "remove containers for services no longer in the compose file")
	cmd.Flags().BoolVar(&yes, "yes", false, "skip confirmation prompt for --volumes")

	return cmd
}
