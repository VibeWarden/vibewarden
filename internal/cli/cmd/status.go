package cmd

import (
	"crypto/tls"
	"fmt"
	"net/http"
	"time"

	"github.com/spf13/cobra"

	caddyadapter "github.com/vibewarden/vibewarden/internal/adapters/caddy"
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

			httpClient := newStatusHTTPClient(cfg)
			checker := opsadapter.NewHTTPHealthChecker(httpClient)
			compose := opsadapter.NewComposeAdapter()

			proxyHost := cfg.Server.Host
			if proxyHost == "" {
				proxyHost = "127.0.0.1"
			}
			proxyPort := cfg.Server.Port
			if proxyPort == 0 {
				proxyPort = 8443
			}
			tlsResolver := opsapp.NewChainResolver(
				caddyadapter.NewInProcessResolver(cfg),
				caddyadapter.NewHandshakeResolver(cfg, proxyHost, proxyPort),
			)

			svc := opsapp.NewStatusService(checker).
				WithCompose(compose).
				WithLogs(compose).
				WithTLSStateResolver(tlsResolver)

			return svc.Run(cmd.Context(), cfg, cmd.OutOrStdout())
		},
	}

	cmd.Flags().StringVar(&configPath, "config", "", "path to vibewarden.yaml (default: ./vibewarden.yaml)")

	return cmd
}

// newStatusHTTPClient returns an HTTP client suitable for health checks.
// When the TLS provider is "self-signed", the client skips certificate
// verification so that status checks against localhost succeed without
// importing the CA into the system trust store.
func newStatusHTTPClient(cfg *config.Config) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if cfg.TLS.Enabled && cfg.TLS.Provider == "self-signed" {
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec // self-signed health-check only
	}
	return &http.Client{
		Timeout:   5 * time.Second,
		Transport: transport,
	}
}
