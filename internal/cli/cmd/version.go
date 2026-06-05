package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

// NewVersionCmd creates the "vibew version" subcommand.
//
// It prints the same version string as "vibew --version", derived from the
// root command's Version field so there is a single source of truth.
// Both invocations produce identical output: "vibew <version>".
func NewVersionCmd(version string) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the vibew version and exit",
		Long:  `Print the VibeWarden CLI version string and exit. Produces the same output as "vibew --version".`,
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, _ []string) {
			fmt.Fprintf(cmd.OutOrStdout(), "vibew %s\n", version)
		},
	}
}
