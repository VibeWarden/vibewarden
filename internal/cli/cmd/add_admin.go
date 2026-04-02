package cmd

import (
	"github.com/spf13/cobra"

	domainscaffold "github.com/vibewarden/vibewarden/internal/domain/scaffold"
)

// newAddAdminCmd creates the `vibewarden add admin` subcommand.
//
// This command enables the VibeWarden admin API in vibewarden.yaml.
func newAddAdminCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "admin [directory]",
		Short: "Enable the admin API",
		Long: `Enable the VibeWarden admin API in vibewarden.yaml.

The admin API provides endpoints for user management, metrics, and
configuration inspection. It is protected by a bearer token.

Next steps after enabling admin:
  1. Set the VIBEWARDEN_ADMIN_TOKEN environment variable
  2. Restart VibeWarden
  3. Access the admin API at http://localhost:8080/_vibewarden/admin/

Run 'vibewarden wrap' first if vibewarden.yaml does not exist.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := ""
			if len(args) > 0 {
				dir = args[0]
			}
			return runAddFeature(cmd, dir, domainscaffold.FeatureAdmin, domainscaffold.FeatureOptions{})
		},
	}
}
