package cmd

import (
	"github.com/spf13/cobra"
)

// NewAddCmd creates the `vibewarden add` subcommand group.
//
// The add command group contains subcommands that incrementally enable
// VibeWarden features in an existing project by modifying vibewarden.yaml.
func NewAddCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "add",
		Short: "Enable a VibeWarden feature in your project",
		Long: `Enable VibeWarden features incrementally by modifying vibewarden.yaml.

Each subcommand enables a specific feature and updates the configuration file.
Run 'vibewarden wrap' first if vibewarden.yaml does not exist.

Examples:
  vibewarden add auth
  vibewarden add rate-limiting
  vibewarden add tls --domain example.com
  vibewarden add admin
  vibewarden add metrics`,
		// Default: print help when no subcommand is given.
		Run: func(cmd *cobra.Command, _ []string) {
			cmd.Help() //nolint:errcheck
		},
	}

	cmd.AddCommand(newAddAuthCmd())
	cmd.AddCommand(newAddRateLimitCmd())
	cmd.AddCommand(newAddTLSCmd())
	cmd.AddCommand(newAddAdminCmd())
	cmd.AddCommand(newAddMetricsCmd())

	return cmd
}
