package cmd

import (
	"fmt"
	"net/http"
	"time"

	"github.com/spf13/cobra"

	opsadapter "github.com/vibewarden/vibewarden/internal/adapters/ops"
	opsapp "github.com/vibewarden/vibewarden/internal/app/ops"
	"github.com/vibewarden/vibewarden/internal/config"
)

// NewStatusCmd creates the "vibew status" subcommand.
//
// The command queries each VibeWarden component and renders a terminal
// health dashboard. Color output indicates healthy (green) or unhealthy (red).
func NewStatusCmd() *cobra.Command {
	var configPath string

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show the health of all VibeWarden components",
		Long: `Query VibeWarden components and render a health dashboard.

Checked components:
  - Proxy        /_vibewarden/health
  - Auth (Kratos) admin health endpoint
  - Rate limit   from config
  - Metrics      /_vibewarden/metrics
  - TLS          provider and domain from config

Examples:
  vibew status
  vibew status --config ./my-vibewarden.yaml`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := config.Load(configPath)
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}

			httpClient := &http.Client{Timeout: 5 * time.Second}
			checker := opsadapter.NewHTTPHealthChecker(httpClient)
			svc := opsapp.NewStatusService(checker)

			return svc.Run(cmd.Context(), cfg, cmd.OutOrStdout())
		},
	}

	cmd.Flags().StringVar(&configPath, "config", "", "path to vibewarden.yaml (default: ./vibewarden.yaml)")

	return cmd
}
