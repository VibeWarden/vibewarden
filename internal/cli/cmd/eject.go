package cmd

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	caddyadapter "github.com/vibewarden/vibewarden/internal/adapters/caddy"
	ejectapp "github.com/vibewarden/vibewarden/internal/app/eject"
	"github.com/vibewarden/vibewarden/internal/config"
)

// NewEjectCmd creates the "vibew eject" subcommand.
//
// The command reads vibewarden.yaml (or the path supplied via --config) and
// prints the equivalent raw proxy configuration to stdout. This allows
// operators to graduate past VibeWarden and run the underlying proxy (e.g.
// Caddy) directly with an equivalent configuration.
//
// Only --format caddy is supported in v1. Additional formats (nginx, traefik)
// are reserved for future releases.
func NewEjectCmd() *cobra.Command {
	var (
		configPath string
		format     string
	)

	cmd := &cobra.Command{
		Use:   "eject",
		Short: "Export the equivalent raw proxy config from vibewarden.yaml",
		Long: `Export the raw proxy configuration equivalent to the current vibewarden.yaml.

The generated configuration can be used to run the underlying proxy directly,
without VibeWarden. This is useful when you have outgrown VibeWarden and want
to manage the proxy configuration yourself.

Supported formats:
  caddy  — Caddy JSON config (default). Feed it to Caddy's /load API or use
            it as a config file: caddy run --config caddy.json --adapter json

Note: VibeWarden-internal endpoints (/_vibewarden/health, /_vibewarden/ready,
/_vibewarden/metrics, /_vibewarden/admin) are included as static stubs in the
generated config. Metrics and admin API routes are omitted because their
internal servers are managed by VibeWarden and have no equivalent outside it.

Examples:
  vibew eject
  vibew eject --config ./path/to/vibewarden.yaml
  vibew eject --format caddy > caddy.json`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			f := ejectapp.Format(format)
			if f != ejectapp.FormatCaddy {
				return ejectapp.ErrUnsupportedFormat{Format: f}
			}

			// Check file existence explicitly when a path is supplied so we can
			// surface a clear error message.
			if configPath != "" {
				if _, err := os.Stat(configPath); err != nil {
					if os.IsNotExist(err) {
						return fmt.Errorf("config file not found: %s", configPath)
					}
					return fmt.Errorf("accessing config file: %w", err)
				}
			}

			cfg, err := config.Load(configPath)
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}

			builder := caddyadapter.NewEjectBuilder()
			svc := ejectapp.NewService(builder)

			result, err := svc.Eject(cfg)
			if err != nil {
				return fmt.Errorf("ejecting config: %w", err)
			}

			enc := json.NewEncoder(cmd.OutOrStdout())
			enc.SetIndent("", "  ")
			if err := enc.Encode(result); err != nil {
				return fmt.Errorf("encoding config: %w", err)
			}

			return nil
		},
	}

	cmd.Flags().StringVarP(&configPath, "config", "c", "", "path to vibewarden.yaml (default: ./vibewarden.yaml)")
	cmd.Flags().StringVar(&format, "format", "caddy", "output format (supported: caddy)")

	if err := cmd.RegisterFlagCompletionFunc("config", func(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
		return []string{"yaml", "yml"}, cobra.ShellCompDirectiveFilterFileExt
	}); err != nil {
		fmt.Fprintln(os.Stderr, "warning: flag completion registration failed:", err)
	}

	if err := cmd.RegisterFlagCompletionFunc("format", func(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
		return []string{"caddy"}, cobra.ShellCompDirectiveNoFileComp
	}); err != nil {
		fmt.Fprintln(os.Stderr, "warning: flag completion registration failed:", err)
	}

	return cmd
}
