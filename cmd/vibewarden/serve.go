package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	caddyadapter "github.com/vibewarden/vibewarden/internal/adapters/caddy"
	"github.com/vibewarden/vibewarden/internal/app/proxy"
	"github.com/vibewarden/vibewarden/internal/config"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// newServeCmd creates the serve subcommand.
// This is a minimal implementation; the full CLI is handled in Epic #6.
func newServeCmd() *cobra.Command {
	var configPath string

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Start the VibeWarden reverse proxy",
		Long: `Start the VibeWarden security sidecar reverse proxy.

Reads configuration from vibewarden.yaml (or the path specified with --config).
Listens for SIGINT/SIGTERM and performs a graceful shutdown.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runServe(configPath)
		},
	}

	cmd.Flags().StringVarP(&configPath, "config", "c", "", "path to vibewarden.yaml (default: ./vibewarden.yaml)")

	return cmd
}

// runServe loads config, wires up the proxy, and runs until shutdown signal.
func runServe(configPath string) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	// Initialize structured logger.
	logger := buildLogger(cfg.Log)

	logger.Info("VibeWarden starting",
		slog.String("version", version),
		slog.String("listen", fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)),
		slog.String("upstream", fmt.Sprintf("%s:%d", cfg.Upstream.Host, cfg.Upstream.Port)),
	)

	// Build ProxyConfig from application config.
	proxyCfg := &ports.ProxyConfig{
		ListenAddr:   fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port),
		UpstreamAddr: fmt.Sprintf("%s:%d", cfg.Upstream.Host, cfg.Upstream.Port),
		Version:      version,
		TLS: ports.TLSConfig{
			Enabled:  cfg.TLS.Enabled,
			Provider: ports.TLSProvider(cfg.TLS.Provider),
			Domain:   cfg.TLS.Domain,
		},
		SecurityHeaders: ports.SecurityHeadersConfig{
			Enabled:               cfg.SecurityHeaders.Enabled,
			HSTSMaxAge:            cfg.SecurityHeaders.HSTSMaxAge,
			HSTSIncludeSubDomains: cfg.SecurityHeaders.HSTSIncludeSubDomains,
			HSTSPreload:           cfg.SecurityHeaders.HSTSPreload,
			ContentTypeNosniff:    cfg.SecurityHeaders.ContentTypeNosniff,
			FrameOption:           cfg.SecurityHeaders.FrameOption,
			ContentSecurityPolicy: cfg.SecurityHeaders.ContentSecurityPolicy,
			ReferrerPolicy:        cfg.SecurityHeaders.ReferrerPolicy,
			PermissionsPolicy:     cfg.SecurityHeaders.PermissionsPolicy,
		},
	}

	// Create Caddy adapter and proxy service.
	adapter := caddyadapter.NewAdapter(proxyCfg, logger)
	svc := proxy.NewService(adapter, logger)

	// Handle OS signals for graceful shutdown.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-sigCh
		logger.Info("received shutdown signal", slog.String("signal", sig.String()))
		cancel()
	}()

	if err := svc.Run(ctx); err != nil {
		// context.Canceled is the normal exit path after a signal.
		return fmt.Errorf("proxy service: %w", err)
	}

	return nil
}

// buildLogger creates an slog.Logger from the log configuration.
func buildLogger(cfg config.LogConfig) *slog.Logger {
	var level slog.Level
	switch cfg.Level {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}

	opts := &slog.HandlerOptions{Level: level}

	if cfg.Format == "text" {
		return slog.New(slog.NewTextHandler(os.Stderr, opts))
	}

	return slog.New(slog.NewJSONHandler(os.Stderr, opts))
}
