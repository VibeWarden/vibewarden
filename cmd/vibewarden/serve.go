package main

import (
	"context"
	"path/filepath"

	"github.com/spf13/cobra"
)

// newServeCmd creates the serve subcommand.
//
// When a multi-site directory layout (sites/ subdirectory with at least one
// child) is detected in the config directory, the command delegates to
// runServeMultiSite. Otherwise it falls back to the standard single-site
// runServe path.
func newServeCmd() *cobra.Command {
	var configPath string

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Start the VibeWarden reverse proxy",
		Long: `Start the VibeWarden security sidecar reverse proxy.

Reads configuration from vibewarden.yaml (or the path specified with --config).
Listens for SIGINT/SIGTERM and performs a graceful shutdown.

In multi-site mode (detected by the presence of a sites/ directory with at
least one subdirectory), the sidecar loads global.yaml and per-site
vibewarden.yaml files, serves each site on its own domain, and watches for
configuration changes in real time.`,
		RunE: func(_ *cobra.Command, _ []string) error {
			// Determine the base directory for multi-site detection.
			// When --config points to a file, its parent directory is used.
			// When --config is empty, the current working directory is used.
			baseDir := "."
			if configPath != "" {
				baseDir = filepath.Dir(configPath)
			}

			if isMultiSiteDir(baseDir) {
				return runServeMultiSite(context.Background(), baseDir, version)
			}

			return runServe(context.Background(), serveOptions{
				configPath: configPath,
				version:    version,
			})
		},
	}

	cmd.Flags().StringVarP(&configPath, "config", "c", "", "path to vibewarden.yaml (default: ./vibewarden.yaml)")

	return cmd
}
