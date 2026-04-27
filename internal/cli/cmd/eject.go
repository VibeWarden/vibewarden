package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"

	"github.com/spf13/cobra"

	caddyadapter "github.com/vibewarden/vibewarden/internal/adapters/caddy"
	ejectapp "github.com/vibewarden/vibewarden/internal/app/eject"
	"github.com/vibewarden/vibewarden/internal/config"
	"github.com/vibewarden/vibewarden/internal/plugins"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// NewEjectCmd creates the "vibew eject" subcommand.
//
// Use this command when you run Caddy directly on the host (vanilla Caddy,
// k8s sidecar, or any non-Docker setup) and want VibeWarden to generate the
// Caddy config for you. If you deploy via Docker Compose, use vibew bundle
// instead — it produces a complete deploy package rather than just the raw
// proxy config.
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
		Short: "Export raw Caddy JSON for non-Docker deploys (most users want `vibew bundle`)",
		Long: `Export the raw Caddy JSON configuration equivalent to the current vibewarden.yaml.

Use this when you run Caddy directly (vanilla Caddy host, k8s sidecar, or any
non-Docker setup) and want VibeWarden to generate the Caddy config for you.

If you deploy via Docker Compose, use ` + "`vibew bundle`" + ` instead — it produces
a complete deploy package (compose file, image tar, merged config, .env,
README) rather than just the raw proxy config.

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

			extraHandlers, err := buildEjectHandlers(cmd.Context(), cfg)
			if err != nil {
				return fmt.Errorf("building plugin handlers: %w", err)
			}

			builder := caddyadapter.NewEjectBuilder()
			svc := ejectapp.NewService(builder)

			result, err := svc.Eject(cfg, extraHandlers)
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

// buildEjectHandlers creates a minimal plugin registry, initialises only the
// CaddyContributor plugins, and collects their handler fragments. This mirrors
// how the serve path builds ExtraHandlers via registry.CaddyContributors()
// (see cmd/vibewarden/wiring_serve_helpers.go) while keeping the eject
// application service free of plugin-specific imports.
func buildEjectHandlers(ctx context.Context, cfg *config.Config) ([]ports.CaddyHandler, error) {
	// Use a silent logger — eject is a config-generation tool; plugin lifecycle
	// messages (init, start) would be noise on stdout/stderr.
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	registry := plugins.NewRegistry(logger)
	plugins.RegisterBuiltinPlugins(registry, cfg, nil, logger)

	if err := registry.InitAll(ctx); err != nil {
		return nil, fmt.Errorf("initialising plugins: %w", err)
	}

	var handlers []ports.CaddyHandler
	for _, contrib := range registry.CaddyContributors() {
		handlers = append(handlers, contrib.ContributeCaddyHandlers()...)
	}

	return handlers, nil
}
