package cmd

import (
	"github.com/spf13/cobra"
)

// NewSecretCmd creates the `vibew secret` subcommand group.
//
// The secret command group contains utilities for generating cryptographically
// secure tokens and keys used by VibeWarden and its integrations.
func NewSecretCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "secret",
		Short: "Generate and manage secrets for VibeWarden",
		Long: `Utilities for generating and retrieving cryptographically secure secrets.

Generated secrets are written to stdout and can be piped directly into
environment files or shell variables. Retrieval commands read from OpenBao
(when running) or the .credentials file as a fallback.

Examples:
  vibew secret generate
  vibew secret generate --length 64
  vibew secret generate >> .env
  vibew secret get postgres
  vibew secret get kratos --json
  vibew secret list`,
		// Default: print help when no subcommand is given.
		Run: func(cmd *cobra.Command, _ []string) {
			cmd.Help() //nolint:errcheck
		},
	}

	cmd.AddCommand(newSecretGenerateCmd())
	cmd.AddCommand(newSecretGetCmd())
	cmd.AddCommand(newSecretListCmd())

	return cmd
}
