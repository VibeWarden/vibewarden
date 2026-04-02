package cmd

import (
	"github.com/spf13/cobra"

	domainscaffold "github.com/vibewarden/vibewarden/internal/domain/scaffold"
)

// newAddAuthCmd creates the `vibewarden add auth` subcommand.
//
// This command enables Ory Kratos authentication in vibewarden.yaml by
// appending the kratos and auth configuration sections.
func newAddAuthCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "auth [directory]",
		Short: "Enable Ory Kratos authentication",
		Long: `Enable Ory Kratos authentication in vibewarden.yaml.

Adds the kratos and auth configuration sections with sensible defaults.
Run 'vibewarden wrap' first if vibewarden.yaml does not exist.

Next steps after enabling auth:
  1. Start Kratos: docker compose up kratos
  2. Configure your login/registration UI at the URLs in vibewarden.yaml
  3. Set KRATOS_DB_PASSWORD and KRATOS_SECRETS_* environment variables`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := ""
			if len(args) > 0 {
				dir = args[0]
			}
			return runAddFeature(cmd, dir, domainscaffold.FeatureAuth, domainscaffold.FeatureOptions{})
		},
	}
}
