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
	fsnotifyadapter "github.com/vibewarden/vibewarden/internal/adapters/fsnotify"
	httpadapter "github.com/vibewarden/vibewarden/internal/adapters/http"
	logadapter "github.com/vibewarden/vibewarden/internal/adapters/log"
	pgadapter "github.com/vibewarden/vibewarden/internal/adapters/postgres"
	proposaladapter "github.com/vibewarden/vibewarden/internal/adapters/proposal"
	migratesvc "github.com/vibewarden/vibewarden/internal/app/migrate"
	proposalapp "github.com/vibewarden/vibewarden/internal/app/proposal"
	"github.com/vibewarden/vibewarden/internal/app/proxy"
	reloadsvc "github.com/vibewarden/vibewarden/internal/app/reload"
	"github.com/vibewarden/vibewarden/internal/config"
	"github.com/vibewarden/vibewarden/internal/plugins"
	usermgmtplugin "github.com/vibewarden/vibewarden/internal/plugins/usermgmt"
	"github.com/vibewarden/vibewarden/internal/ports"
	"github.com/vibewarden/vibewarden/migrations"
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

	// Run database migrations before plugin init when a database is configured.
	if dbURL := cfg.Database.ResolveURL(); dbURL != "" {
		runner, err := pgadapter.NewMigrationAdapter(dbURL, migrations.FS)
		if err != nil {
			return fmt.Errorf("creating migration runner: %w", err)
		}

		svc := migratesvc.NewService(runner, logger)
		defer func() {
			if closeErr := svc.Close(); closeErr != nil {
				logger.Error("closing migration runner", slog.String("error", closeErr.Error()))
			}
		}()

		if err := svc.ApplyAll(ctx); err != nil {
			return fmt.Errorf("running database migrations: %w", err)
		}
	} else {
		logger.Debug("no database configured, skipping migrations")
	}

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
	// The ring buffer is created here and wired both as an additional event
	// sink and into the admin API so that recent events can be queried.
	ringBuf := logadapter.NewRingBuffer(logadapter.DefaultRingBufferCapacity)
	eventLogger := buildEventLogger(registry, logger, ringBuf)

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

	// Build the reload service. The rebuildFn captures the registry and version
	// so that a reload can rebuild ProxyConfig without needing to re-initialise
	// plugins (which must not restart on hot reload).
	rebuildFn := func(newCfg *config.Config) *ports.ProxyConfig {
		return buildProxyConfig(newCfg, registry, opts.Version)
	}

	reloadService := reloadsvc.NewService(
		opts.ConfigPath,
		cfg,
		adapter,
		eventLogger,
		logger,
		rebuildFn,
	)

	// Build the in-memory proposal store and application service.
	// The applier needs the config path and the reload service, both of which
	// are available at this point.
	proposalStore := proposaladapter.NewStore()
	proposalApplier := proposaladapter.NewApplier(opts.ConfigPath, reloadService)
	proposalSvc := proposalapp.NewService(proposalStore, proposalApplier, eventLogger, logger)
	proposalHandlers := httpadapter.NewProposalHandlers(proposalSvc, logger)

	// Inject the reload service, ring buffer, and proposal handlers into the
	// user-management plugin's admin handlers so that config, events, and
	// proposal endpoints are available via the admin API.
	for _, p := range registry.Plugins() {
		if ump, ok := p.(*usermgmtplugin.Plugin); ok {
			ump.InjectReloader(reloadService)
			ump.InjectRingBuffer(ringBuf)
			ump.InjectProposalHandlers(proposalHandlers)
			break
		}
	}

	// Start file watcher goroutine when watch is enabled.
	if cfg.Watch.Enabled {
		debounce, err := time.ParseDuration(cfg.Watch.Debounce)
		if err != nil {
			logger.Warn("invalid watch.debounce, using default 500ms",
				slog.String("debounce", cfg.Watch.Debounce),
				slog.String("error", err.Error()),
			)
			debounce = 500 * time.Millisecond
		}

		watcher := fsnotifyadapter.NewWatcher(logger, ports.WithDebounce(debounce))
		watchPath := opts.ConfigPath
		if watchPath == "" {
			watchPath = "vibewarden.yaml"
		}

		ch, watchErr := watcher.Watch(ctx, watchPath)
		if watchErr != nil {
			// Non-fatal: log the error but continue without file watching.
			logger.Warn("config file watcher failed to start",
				slog.String("path", watchPath),
				slog.String("error", watchErr.Error()),
			)
		} else {
			go func() {
				for range ch {
					logger.Info("config file changed, triggering reload",
						slog.String("path", watchPath),
					)
					if reloadErr := reloadService.Reload(ctx, "file_watcher"); reloadErr != nil {
						logger.Error("hot reload failed",
							slog.String("error", reloadErr.Error()),
						)
					}
				}
			}()
		}
	}

	svc := proxy.NewService(adapter, logger)

	if err := svc.Run(ctx); err != nil {
		return fmt.Errorf("proxy service: %w", err)
	}

	return nil
}
