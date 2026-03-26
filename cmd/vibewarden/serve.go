package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	caddyadapter "github.com/vibewarden/vibewarden/internal/adapters/caddy"
	httpadapter "github.com/vibewarden/vibewarden/internal/adapters/http"
	kratosadapter "github.com/vibewarden/vibewarden/internal/adapters/kratos"
	logadapter "github.com/vibewarden/vibewarden/internal/adapters/log"
	metricsadapter "github.com/vibewarden/vibewarden/internal/adapters/metrics"
	postgresadapter "github.com/vibewarden/vibewarden/internal/adapters/postgres"
	adminapp "github.com/vibewarden/vibewarden/internal/app/admin"
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

	// Initialize the EventLogger — writes structured JSON events to stdout.
	eventLogger := logadapter.NewSlogEventLogger(os.Stdout)

	// Initialize Prometheus metrics adapter and start the internal metrics server.
	// The internal server binds a random localhost port; Caddy reverse-proxies
	// /_vibewarden/metrics to it when metrics are enabled.
	var metricsCfg ports.MetricsProxyConfig
	if cfg.Metrics.Enabled {
		pa := metricsadapter.NewPrometheusAdapter(cfg.Metrics.PathPatterns)
		metricsSrv := metricsadapter.NewServer(pa.Handler(), logger)
		if err := metricsSrv.Start(); err != nil {
			return fmt.Errorf("starting metrics server: %w", err)
		}
		defer func() {
			stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer stopCancel()
			if stopErr := metricsSrv.Stop(stopCtx); stopErr != nil {
				logger.Error("stopping metrics server", slog.String("error", stopErr.Error()))
			}
		}()
		metricsCfg = ports.MetricsProxyConfig{
			Enabled:      true,
			InternalAddr: metricsSrv.Addr(),
		}
		logger.Info("metrics enabled", slog.String("internal_addr", metricsSrv.Addr()))
	}

	// Initialize admin API when enabled in config.
	var adminCfg ports.AdminProxyConfig
	if cfg.Admin.Enabled {
		kratosAdmin := kratosadapter.NewAdminAdapter(cfg.Kratos.AdminURL, 0, logger)

		// AuditLogger is optional — only wired when a database URL is configured.
		var auditLogger ports.AuditLogger
		if cfg.Database.URL != "" {
			auditAdapter, err := postgresadapter.NewAuditAdapter(cfg.Database.URL)
			if err != nil {
				return fmt.Errorf("connecting to audit database: %w", err)
			}
			defer func() {
				if closeErr := auditAdapter.Close(); closeErr != nil {
					logger.Error("closing audit database", slog.String("error", closeErr.Error()))
				}
			}()
			auditLogger = auditAdapter
		}

		adminSvc := adminapp.NewService(kratosAdmin, eventLogger, auditLogger)
		adminHandlers := httpadapter.NewAdminHandlers(adminSvc, logger)
		adminSrv := httpadapter.NewAdminServer(adminHandlers, logger)
		if err := adminSrv.Start(); err != nil {
			return fmt.Errorf("starting admin server: %w", err)
		}
		defer func() {
			stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer stopCancel()
			if stopErr := adminSrv.Stop(stopCtx); stopErr != nil {
				logger.Error("stopping admin server", slog.String("error", stopErr.Error()))
			}
		}()
		adminCfg = ports.AdminProxyConfig{
			Enabled:      true,
			InternalAddr: adminSrv.Addr(),
		}
		logger.Info("admin API enabled", slog.String("internal_addr", adminSrv.Addr()))
	}

	// Build ProxyConfig from application config.
	proxyCfg := &ports.ProxyConfig{
		ListenAddr:   fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port),
		UpstreamAddr: fmt.Sprintf("%s:%d", cfg.Upstream.Host, cfg.Upstream.Port),
		Version:      version,
		TLS: ports.TLSConfig{
			Enabled:     cfg.TLS.Enabled,
			Provider:    ports.TLSProvider(cfg.TLS.Provider),
			Domain:      cfg.TLS.Domain,
			CertPath:    cfg.TLS.CertPath,
			KeyPath:     cfg.TLS.KeyPath,
			StoragePath: cfg.TLS.StoragePath,
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
		Auth: ports.AuthConfig{
			// Auth is enabled when a Kratos public URL is configured.
			// The KratosPublicURL drives Kratos flow proxy route insertion.
			Enabled:           cfg.Kratos.PublicURL != "",
			KratosPublicURL:   cfg.Kratos.PublicURL,
			KratosAdminURL:    cfg.Kratos.AdminURL,
			PublicPaths:       cfg.Auth.PublicPaths,
			SessionCookieName: cfg.Auth.SessionCookieName,
			LoginURL:          cfg.Auth.LoginURL,
		},
		RateLimit: ports.RateLimitConfig{
			Enabled:           cfg.RateLimit.Enabled,
			TrustProxyHeaders: cfg.RateLimit.TrustProxyHeaders,
			ExemptPaths:       cfg.RateLimit.ExemptPaths,
			PerIP: ports.RateLimitRule{
				RequestsPerSecond: cfg.RateLimit.PerIP.RequestsPerSecond,
				Burst:             cfg.RateLimit.PerIP.Burst,
			},
			PerUser: ports.RateLimitRule{
				RequestsPerSecond: cfg.RateLimit.PerUser.RequestsPerSecond,
				Burst:             cfg.RateLimit.PerUser.Burst,
			},
		},
		Metrics: metricsCfg,
		AdminAuth: ports.AdminAuthConfig{
			Enabled: cfg.Admin.Enabled,
			Token:   cfg.Admin.Token,
		},
		Admin: adminCfg,
	}

	// Create Caddy adapter and proxy service.
	adapter := caddyadapter.NewAdapter(proxyCfg, logger, eventLogger)
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
