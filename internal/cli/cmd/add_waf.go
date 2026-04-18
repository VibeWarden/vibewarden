package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	domainscaffold "github.com/vibewarden/vibewarden/internal/domain/scaffold"
)

// newAddWAFCmd creates the `vibew add waf` subcommand.
//
// This command enables the Web Application Firewall in vibewarden.yaml
// with an optional --mode flag to select detect or block mode.
func newAddWAFCmd() *cobra.Command {
	var mode string

	cmd := &cobra.Command{
		Use:   "waf [directory]",
		Short: "Enable the Web Application Firewall",
		Long: `Enable the Web Application Firewall (WAF) in vibewarden.yaml.

Adds the waf configuration section with sensible defaults:
  - All rule categories enabled (SQLi, XSS, path traversal, command injection)
  - Mode defaults to "detect" (log-only)

Supported modes:
  detect   Log detections without blocking requests (default)
  block    Reject matching requests with 403 Forbidden

Next steps after enabling WAF:
  1. Restart VibeWarden
  2. Monitor logs for WAF detection events
  3. Switch to --mode block when you are confident in the rules

Run 'vibew wrap' first if vibewarden.yaml does not exist.

Examples:
  vibew add waf                # enables WAF in detect mode (default)
  vibew add waf --mode block   # enables WAF in block mode`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if mode != "detect" && mode != "block" {
				return fmt.Errorf("--mode must be %q or %q, got %q", "detect", "block", mode)
			}

			dir := ""
			if len(args) > 0 {
				dir = args[0]
			}

			opts := domainscaffold.FeatureOptions{
				WAFMode: mode,
			}
			return runAddFeature(cmd, dir, domainscaffold.FeatureWAF, opts)
		},
	}

	cmd.Flags().StringVar(&mode, "mode", "detect", `WAF mode: "detect" (log-only) or "block" (reject)`)

	return cmd
}
