package serve

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	caddyadapter "github.com/vibewarden/vibewarden/internal/adapters/caddy"
	logadapter "github.com/vibewarden/vibewarden/internal/adapters/log"
	"github.com/vibewarden/vibewarden/internal/app/proxy"
	"github.com/vibewarden/vibewarden/internal/config"
	"github.com/vibewarden/vibewarden/internal/plugins"
)

// Options configures the RunServe function behavior.
type Options struct {
	// ConfigPath is the path to vibewarden.yaml. An empty string causes the
	// default search paths to be used (current directory and /etc/vibewarden).
	ConfigPath string

	// Version is the binary version string, typically set via -ldflags at build
	// time. It is embedded in the ProxyConfig for observability.
	Version string
}

// RunServe loads config, builds the plugin registry, wires Caddy via plugin
// contributors, and runs until a shutdown signal is received or the context is
// cancelled.
//
// Additional plugins can be contributed by passing PluginRegistrar functions via
// extraPlugins. They are called after plugins.RegisterBuiltinPlugins, allowing
// Pro or custom binaries to extend the registry without forking the OSS code.
//
// Signal handling: RunServe installs SIGINT/SIGTERM handlers that cancel the
// context and trigger graceful shutdown.
func RunServe(ctx context.Context, opts Options, extraPlugins ...plugins.PluginRegistrar) error {
	cfg, err := config.Load(opts.ConfigPath)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	logger := buildLogger(cfg.Log)

	// Migrate legacy metrics: config to telemetry: if needed.
	config.MigrateLegacyMetrics(cfg, logger)

	logger.Info("VibeWarden starting",
		slog.String("version", opts.Version),
		slog.String("listen", fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)),
		slog.String("upstream", fmt.Sprintf("%s:%d", cfg.Upstream.Host, cfg.Upstream.Port)),
	)

	// Build the plugin registry and register all compiled-in plugins.
	// At registration time we use a stdout-only event logger. After InitAll,
	// we upgrade to a multi-handler logger (stdout + OTel) when log export is
	// enabled. The caddy adapter — the main consumer of the event logger —
	// is created after StartAll, so it always gets the upgraded logger.
	initialEventLogger := logadapter.NewSlogEventLogger(os.Stdout)

	registry := plugins.NewRegistry(logger)

	// Register built-in OSS plugins.
	plugins.RegisterBuiltinPlugins(registry, cfg, initialEventLogger, logger)

	// Register any additional plugins supplied by the caller (e.g. Pro plugins).
	for _, reg := range extraPlugins {
		reg(registry, cfg, initialEventLogger, logger)
	}

	// Set up OS signal handling before Init/Start so that a slow Init can
	// still be interrupted.
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	go func() {
		select {
		case sig := <-sigCh:
			logger.Info("received shutdown signal", slog.String("signal", sig.String()))
			cancel()
		case <-ctx.Done():
		}
	}()

	// Initialise all plugins.
	if err := registry.InitAll(ctx); err != nil {
		return fmt.Errorf("initialising plugins: %w", err)
	}

	// After InitAll, retrieve the OTel log handler from the metrics plugin
	// (if log export is enabled) and build the final event logger.
	eventLogger := buildEventLogger(registry, logger)

	// Wire the metrics collector into the TLS cert expiry monitor so that
	// the vibewarden_tls_cert_expiry_seconds gauge is populated. This must
	// happen after InitAll (metrics provider is ready) and before StartAll
	// (monitor background goroutine launches on Start).
	wireTLSMetricsCollector(registry)

	// Start all plugins (background servers, etc.).
	if err := registry.StartAll(ctx); err != nil {
		return fmt.Errorf("starting plugins: %w", err)
	}

	// Ensure StopAll runs on return (normal or error path).
	defer func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer stopCancel()
		if stopErr := registry.StopAll(stopCtx); stopErr != nil {
			logger.Error("stopping plugins", slog.String("error", stopErr.Error()))
		}
	}()

	// Build ProxyConfig — base fields that the Caddy adapter uses directly.
	// Plugin-specific wiring (security headers, rate limiting, auth, admin,
	// metrics) is now driven by each plugin's CaddyContributor implementation.
	proxyCfg := buildProxyConfig(cfg, registry, opts.Version)

	// Create Caddy adapter and proxy service.
	adapter := caddyadapter.NewAdapter(proxyCfg, logger, eventLogger)
	svc := proxy.NewService(adapter, logger)

	if err := svc.Run(ctx); err != nil {
		return fmt.Errorf("proxy service: %w", err)
	}

	return nil
}
