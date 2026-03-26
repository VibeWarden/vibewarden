package cmd

import (
	"github.com/spf13/cobra"
)

// NewSecretCmd creates the `vibewarden secret` subcommand group.
//
// The secret command group contains utilities for generating cryptographically
// secure tokens and keys used by VibeWarden and its integrations.
func NewSecretCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "secret",
		Short: "Generate and manage secrets for VibeWarden",
		Long: `Utilities for generating cryptographically secure secrets.

Generated secrets are written to stdout and can be piped directly into
environment files or shell variables.

Examples:
  vibewarden secret generate
  vibewarden secret generate --length 64
  vibewarden secret generate >> .env`,
		// Default: print help when no subcommand is given.
		Run: func(cmd *cobra.Command, _ []string) {
			cmd.Help() //nolint:errcheck
		},
	}

	cmd.AddCommand(newSecretGenerateCmd())

	return cmd
}
