// Package main is the entrypoint for the VibeWarden security sidecar.
package main

import (
	"fmt"
	"os"

	cliCmd "github.com/vibewarden/vibewarden/internal/cli/cmd"
)

// version is set at build time via -ldflags.
var version = "dev"

func main() {
	rootCmd := cliCmd.NewRootCmd(version)

	// serve is defined in this package because it references the version
	// variable set at build time and wires concrete adapters that live outside
	// internal/cli/cmd.
	rootCmd.AddCommand(newServeCmd())

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
