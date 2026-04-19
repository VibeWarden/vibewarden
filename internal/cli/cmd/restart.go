package cmd

import (
	"github.com/spf13/cobra"

	opsadapter "github.com/vibewarden/vibewarden/internal/adapters/ops"
	opsapp "github.com/vibewarden/vibewarden/internal/app/ops"
)

// NewRestartCmd creates the "vibew restart [service...]" subcommand.
//
// The command rebuilds and recreates containers in the Docker Compose stack
// (or a subset of named services), picking up any Dockerfile or config changes.
// It always uses the generated compose file at .vibewarden/generated/docker-compose.yml.
//
// Examples:
//
//	vibew restart              # rebuild and restart all services
//	vibew restart app          # rebuild and restart only the app service
//	vibew restart app kratos   # rebuild and restart multiple services
func NewRestartCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "restart [service...]",
		Short: "Rebuild and restart the stack",
		Long: `Rebuild and restart the VibeWarden Docker Compose stack, picking up
any Dockerfile or configuration changes.

Internally runs 'docker compose up -d --force-recreate --build' so that
images are rebuilt and containers are recreated.

When called without arguments all services are rebuilt and restarted.  Pass
one or more service names to rebuild and restart only those services.

The generated compose file at .vibewarden/generated/docker-compose.yml is used.
Run 'vibew generate' first if that file does not yet exist.

Examples:
  vibew restart
  vibew restart app
  vibew restart app kratos`,
		RunE: func(cmd *cobra.Command, args []string) error {
			compose := opsadapter.NewComposeAdapter()
			svc := opsapp.NewRestartService(compose)
			return svc.Run(cmd.Context(), args, cmd.OutOrStdout())
		},
	}
}
