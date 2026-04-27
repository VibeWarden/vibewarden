package cmd

import (
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"

	multisiteapp "github.com/vibewarden/vibewarden/internal/app/multisite"
)

// NewAddCmd creates the `vibew add` subcommand group.
//
// The add command group contains subcommands that incrementally enable
// VibeWarden features in an existing project by modifying vibewarden.yaml.
// All subcommands refuse on multi-site project roots because multi-site
// support is post-v1 (#1169).
func NewAddCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "add",
		Short: "Enable a VibeWarden feature in your project",
		Long: `Enable VibeWarden features incrementally by modifying vibewarden.yaml.

Each subcommand enables a specific feature and updates the configuration file.
Run 'vibew wrap' first if vibewarden.yaml does not exist.

Examples:
  vibew add auth
  vibew add rate-limiting
  vibew add tls --domain example.com
  vibew add admin
  vibew add metrics
  vibew add waf --mode block`,
		// Default: print help when no subcommand is given.
		Run: func(cmd *cobra.Command, _ []string) {
			cmd.Help() //nolint:errcheck
		},
		// PersistentPreRunE runs before every subcommand in this group.
		// It rejects multi-site project roots because the add commands only
		// know how to modify a single vibewarden.yaml; multi-site support is
		// post-v1 and tracked at #1169.
		PersistentPreRunE: func(_ *cobra.Command, args []string) error {
			dir := "."
			if len(args) > 0 {
				dir = args[0]
			}
			configPath, err := filepath.Abs(filepath.Join(dir, "vibewarden.yaml"))
			if err != nil {
				configPath = filepath.Join(dir, "vibewarden.yaml")
			}
			if multisiteapp.IsProject(configPath) {
				return fmt.Errorf("multi-site projects are post-v1 (see #1169) — use a single-site project and revisit when #1169 lands")
			}
			return nil
		},
	}

	cmd.AddCommand(newAddAuthCmd())
	cmd.AddCommand(newAddRateLimitCmd())
	cmd.AddCommand(newAddTLSCmd())
	cmd.AddCommand(newAddAdminCmd())
	cmd.AddCommand(newAddMetricsCmd())
	cmd.AddCommand(newAddWAFCmd())

	return cmd
}
