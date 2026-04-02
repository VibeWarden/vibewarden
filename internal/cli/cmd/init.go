package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

// NewInitCmd creates a placeholder `vibewarden init` subcommand.
//
// The init command is reserved for full project scaffolding (creating a new
// project from scratch). Until that feature is implemented, this placeholder
// directs users to `vibewarden wrap` for adding VibeWarden to existing projects.
func NewInitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Create a new VibeWarden project (coming soon)",
		Long: `Create a new project with VibeWarden pre-configured.

This command is currently under development. To add VibeWarden to an
existing project, use:

  vibewarden wrap [directory]

See 'vibewarden wrap --help' for options.`,
		// DisableFlagParsing allows the placeholder to accept any flags without
		// erroring, so users who migrate from 'vibew init --upstream 3000' see
		// the informational message rather than an "unknown flag" error.
		DisableFlagParsing: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			fmt.Fprintln(cmd.OutOrStdout(), `The 'init' command is reserved for full project scaffolding (coming soon).

To add VibeWarden to an existing project, use:

  vibewarden wrap [directory]

Run 'vibewarden wrap --help' for available options.`)
			return nil
		},
	}
}
