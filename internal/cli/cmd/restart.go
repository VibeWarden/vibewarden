package cmd

import (
	"github.com/spf13/cobra"

	opsadapter "github.com/vibewarden/vibewarden/internal/adapters/ops"
	opsapp "github.com/vibewarden/vibewarden/internal/app/ops"
)

// NewRestartCmd creates the "vibew restart [service...]" subcommand.
//
// The command restarts the Docker Compose stack (or a subset of named services)
// without rebuilding or recreating containers.  It always uses the generated
// compose file at .vibewarden/generated/docker-compose.yml.
//
// Examples:
//
//	vibew restart              # restart all services
//	vibew restart app          # restart only the app service
//	vibew restart app kratos   # restart multiple services
func NewRestartCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "restart [service...]",
		Short: "Restart the stack without rebuilding",
		Long: `Restart the VibeWarden Docker Compose stack without rebuilding or recreating
containers.  This is faster than 'vibew dev' after a config-only change.

When called without arguments all services are restarted.  Pass one or more
service names to restart only those services.

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
