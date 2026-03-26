package cmd

import (
	"github.com/spf13/cobra"

	domainscaffold "github.com/vibewarden/vibewarden/internal/domain/scaffold"
)

// newAddMetricsCmd creates the `vibewarden add metrics` subcommand.
//
// This command enables Prometheus metrics in vibewarden.yaml.
func newAddMetricsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "metrics [directory]",
		Short: "Enable Prometheus metrics",
		Long: `Enable Prometheus metrics in vibewarden.yaml.

Adds the metrics configuration section. Metrics are exposed at /metrics by
default and can be scraped by Prometheus or any compatible collector.

Next steps after enabling metrics:
  1. Restart VibeWarden
  2. Scrape http://localhost:8080/metrics with Prometheus
  3. Optionally add a Grafana dashboard for visualisation

Run 'vibewarden init' first if vibewarden.yaml does not exist.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := ""
			if len(args) > 0 {
				dir = args[0]
			}
			return runAddFeature(cmd, dir, domainscaffold.FeatureMetrics, domainscaffold.FeatureOptions{})
		},
	}
}
