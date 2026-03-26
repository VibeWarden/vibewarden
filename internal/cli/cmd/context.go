package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

// NewContextCmd creates the `vibewarden context` subcommand group.
//
// The context command group contains operations for managing AI agent context
// files. Currently only the `refresh` subcommand is registered.
func NewContextCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "context",
		Short: "Manage AI agent context files",
		Long: `Manage the AI agent context files that tell your coding assistant
about VibeWarden's security layer.

Run 'vibewarden context refresh' to regenerate context files from the current
vibewarden.yaml configuration.`,
		// Default: print help when no subcommand is given.
		Run: func(cmd *cobra.Command, _ []string) {
			cmd.Help() //nolint:errcheck
		},
	}

	cmd.AddCommand(newContextRefreshCmd())

	return cmd
}

// newContextRefreshCmd creates the `vibewarden context refresh` subcommand.
//
// This command regenerates AI agent context files to reflect the current
// vibewarden.yaml configuration. Full implementation is tracked in issue #73.
func newContextRefreshCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "refresh",
		Short: "Regenerate AI agent context files from vibewarden.yaml",
		Long: `Regenerate AI agent context files to reflect the current vibewarden.yaml
configuration.

This is useful after changing feature flags (auth, rate limiting, TLS) so that
your AI coding assistant receives up-to-date instructions about the security layer.

Note: Full implementation is coming in a future release. See issue #73.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			fmt.Fprintln(cmd.OutOrStdout(), "TODO: context refresh — full implementation coming in issue #73.")
			fmt.Fprintln(cmd.OutOrStdout(), "For now, re-run 'vibewarden init --force' to regenerate context files.")
			return nil
		},
	}
}
