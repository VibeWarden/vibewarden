// Package cmd contains the cobra command tree for the VibeWarden CLI.
package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// NewRootCmd creates and returns the root cobra command for VibeWarden.
// version is printed by the --version flag.
func NewRootCmd(version string) *cobra.Command {
	root := &cobra.Command{
		Use:   "vibewarden",
		Short: "VibeWarden - Security sidecar for vibe-coded apps",
		Long: `VibeWarden is an open-source security sidecar that handles
TLS, authentication, rate limiting, and AI-readable structured logs.

Zero-to-secure in minutes.`,
		Version: version,
		// Default: print help when no subcommand is given.
		Run: func(cmd *cobra.Command, _ []string) {
			cmd.Help() //nolint:errcheck
		},
	}

	root.SetVersionTemplate("vibewarden {{.Version}}\n")

	// Register all subcommands.
	root.AddCommand(NewInitCmd())
	root.AddCommand(NewContextCmd())
	root.AddCommand(NewAddCmd())
	root.AddCommand(NewDevCmd())
	root.AddCommand(NewStatusCmd())
	root.AddCommand(NewDoctorCmd())
	root.AddCommand(NewLogsCmd())
	root.AddCommand(NewSecretCmd())
	root.AddCommand(NewValidateCmd())
	root.AddCommand(NewGenerateCmd())
	root.AddCommand(NewPluginsCmd())
	root.AddCommand(NewCertCmd())
	root.AddCommand(NewTokenCmd())

	return root
}

// Execute runs the root command and exits on error.
// It is called from main() and is not testable directly; use NewRootCmd in
// tests.
func Execute(version string) {
	if err := NewRootCmd(version).Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
