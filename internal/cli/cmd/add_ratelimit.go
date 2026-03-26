package cmd

import (
	"github.com/spf13/cobra"

	domainscaffold "github.com/vibewarden/vibewarden/internal/domain/scaffold"
)

// newAddRateLimitCmd creates the `vibewarden add rate-limiting` subcommand.
//
// This command enables per-IP and per-user rate limiting in vibewarden.yaml.
func newAddRateLimitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "rate-limiting [directory]",
		Short: "Enable rate limiting",
		Long: `Enable per-IP and per-user rate limiting in vibewarden.yaml.

Adds the rate_limit configuration section with sensible defaults:
  - 10 requests/second per IP (burst: 20)
  - 50 requests/second per user (burst: 100)
  - /health and /ready paths are exempt

Run 'vibewarden init' first if vibewarden.yaml does not exist.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := ""
			if len(args) > 0 {
				dir = args[0]
			}
			return runAddFeature(cmd, dir, domainscaffold.FeatureRateLimit, domainscaffold.FeatureOptions{})
		},
	}
}
