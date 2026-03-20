// Package main is the entrypoint for the VibeWarden security sidecar.
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// version is set at build time via -ldflags.
var version = "dev"

func main() {
	rootCmd := &cobra.Command{
		Use:   "vibewarden",
		Short: "VibeWarden - Security sidecar for vibe-coded apps",
		Long: `VibeWarden is an open-source security sidecar that handles
TLS, authentication, rate limiting, and AI-readable structured logs.

Zero-to-secure in minutes.`,
		Version: version,
		Run: func(cmd *cobra.Command, args []string) {
			// Default behavior: print help
			cmd.Help() //nolint:errcheck
		},
	}

	// Configure version template
	rootCmd.SetVersionTemplate("vibewarden {{.Version}}\n")

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
