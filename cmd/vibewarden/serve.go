package main

import (
	"context"

	"github.com/spf13/cobra"

	appserve "github.com/vibewarden/vibewarden/internal/app/serve"
)

// newServeCmd creates the serve subcommand.
func newServeCmd() *cobra.Command {
	var configPath string

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Start the VibeWarden reverse proxy",
		Long: `Start the VibeWarden security sidecar reverse proxy.

Reads configuration from vibewarden.yaml (or the path specified with --config).
Listens for SIGINT/SIGTERM and performs a graceful shutdown.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return appserve.RunServe(context.Background(), appserve.Options{
				ConfigPath: configPath,
				Version:    version,
			})
		},
	}

	cmd.Flags().StringVarP(&configPath, "config", "c", "", "path to vibewarden.yaml (default: ./vibewarden.yaml)")

	return cmd
}
