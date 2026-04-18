package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"syscall"

	caddyadapter "github.com/vibewarden/vibewarden/internal/adapters/caddy"
	fsnotifyadapter "github.com/vibewarden/vibewarden/internal/adapters/fsnotify"
	logadapter "github.com/vibewarden/vibewarden/internal/adapters/log"
	"github.com/vibewarden/vibewarden/internal/app/proxy"
	reloadsvc "github.com/vibewarden/vibewarden/internal/app/reload"
	"github.com/vibewarden/vibewarden/internal/config/sites"
	"github.com/vibewarden/vibewarden/internal/domain/site"
	"github.com/vibewarden/vibewarden/internal/plugins"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// isMultiSiteDir reports whether the given directory contains a sites/
// subdirectory with at least one child directory, indicating that the
// sidecar should start in multi-site mode.
func isMultiSiteDir(dir string) bool {
	sitesDir := filepath.Join(dir, "sites")
	entries, err := os.ReadDir(sitesDir)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if entry.IsDir() {
			return true
		}
	}
	return false
}

// runServeMultiSite loads global config and per-site configs from the sites/
// directory, builds a site registry, validates domains, starts the site
// watcher, and runs the multi-site Caddy proxy until shutdown.
//
// The directory layout is expected to be:
//
//	<baseDir>/
//	  global.yaml           (optional, defaults applied if missing)
//	  sites/
//	    <site-name>/
//	      vibewarden.yaml   (per-site config)
//
// This function is called from runServe when a multi-site directory layout
// is detected.
func runServeMultiSite(ctx context.Context, baseDir string, ver string) error {
	// Step 1: Load global config.
	globalPath := filepath.Join(baseDir, "global.yaml")
	globalCfg, err := sites.LoadGlobal(globalPath)
	if err != nil {
		return fmt.Errorf("loading global config: %w", err)
	}

	logger := buildMultiSiteLogger(globalCfg.LogLevel)

	logger.Info("VibeWarden multi-site mode starting",
		slog.String("version", ver),
		slog.String("base_dir", baseDir),
		slog.String("listen", fmt.Sprintf("%s:%d", globalCfg.ListenHost, globalCfg.ListenPort)),
	)

	// Step 2: Load all per-site configs.
	sitesDir := filepath.Join(baseDir, "sites")
	loadedSites, loadErrs := sites.LoadSites(sitesDir)
	for _, loadErr := range loadErrs {
		logger.Warn("site load error", slog.String("error", loadErr.Error()))
	}

	// Step 3: Populate the registry.
	registry := site.NewRegistry()
	registry.SetGlobal(*globalCfg)

	for _, s := range loadedSites {
		registry.Add(s)
	}

	logger.Info("sites loaded",
		slog.Int("total", registry.Len()),
		slog.Int("healthy", len(registry.HealthySites())),
		slog.Int("error", len(registry.ErrorSites())),
	)

	// Step 4: Validate domains.
	if domErr := registry.ValidateDomains(); domErr != nil {
		return fmt.Errorf("domain validation failed: %w", domErr)
	}

	// Step 5: Create the event logger.
	eventLogger := logadapter.NewSlogEventLogger(os.Stdout)

	// Step 6: Build per-site plugin registries and collect Caddy handlers.
	//
	// Each healthy site gets its own plugin registry so that plugins like WAF,
	// rate limiting, auth, CORS, and IP filter contribute their handlers to the
	// correct site's middleware chain. Only Init is called (no Start) because
	// handler contribution requires initialised state but not running background
	// work. See wiring_serve_helpers.go for the single-site equivalent.
	perSiteHandlers, err := buildPerSitePluginHandlers(ctx, registry, eventLogger, logger)
	if err != nil {
		return fmt.Errorf("building per-site plugin handlers: %w", err)
	}

	// Step 7: Create the multi-site Caddy adapter.
	// A minimal ProxyConfig is needed for backward-compatible fields (version).
	minimalProxyCfg := &ports.ProxyConfig{
		ListenAddr: fmt.Sprintf("%s:%d", globalCfg.ListenHost, globalCfg.ListenPort),
		Version:    ver,
	}
	adapter := caddyadapter.NewMultiSiteAdapter(minimalProxyCfg, registry, perSiteHandlers, logger, eventLogger)

	// Step 8: Set up signal handling.
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

	// Step 9: Start the site watcher for hot-reload.
	siteWatcher := fsnotifyadapter.NewSiteWatcher(logger)
	watchCh, watchErr := siteWatcher.Watch(ctx, sitesDir)
	if watchErr != nil {
		logger.Warn("site watcher failed to start",
			slog.String("path", sitesDir),
			slog.String("error", watchErr.Error()),
		)
	} else {
		// Step 10: Create the MultiSiteService event loop.
		reloadFn := func(reloadCtx context.Context) error {
			return adapter.Reload(reloadCtx)
		}
		multiSiteSvc := reloadsvc.NewMultiSiteService(registry, eventLogger, logger, reloadFn)
		go multiSiteSvc.Run(ctx, watchCh)
	}

	// Step 11: Run the proxy until shutdown.
	svc := proxy.NewService(adapter, logger)
	if err := svc.Run(ctx); err != nil {
		return fmt.Errorf("proxy service: %w", err)
	}

	return nil
}

// buildPerSitePluginHandlers creates a per-site plugin registry for each
// healthy site, registers builtin plugins, calls InitAll, and collects the
// CaddyContributor handlers. The returned map is keyed by site name.
//
// Only Init is called — Start is not needed because CaddyContributors are
// queried after Init (matching the single-site lifecycle in wiring_serve.go
// and wiring_serve_helpers.go).
//
// Non-critical plugin init failures are logged and the plugin is marked
// degraded (it will not contribute handlers). Critical plugin init failures
// for a site cause that site's handlers to be skipped entirely — the site
// still serves with its config-driven handlers (security headers, rate
// limiting, compression) but without plugin-contributed handlers.
func buildPerSitePluginHandlers(
	ctx context.Context,
	siteRegistry *site.Registry,
	eventLogger ports.EventLogger,
	logger *slog.Logger,
) (map[string][]ports.CaddyHandler, error) {
	healthySites := siteRegistry.HealthySites()
	if len(healthySites) == 0 {
		return nil, nil
	}

	perSiteHandlers := make(map[string][]ports.CaddyHandler, len(healthySites))

	for _, s := range healthySites {
		cfg := s.Config()
		if cfg == nil {
			continue
		}

		siteLogger := logger.With(slog.String("site", s.Name()))

		// Create a per-site plugin registry with a site-scoped logger so that
		// plugin lifecycle log lines include the site name.
		reg := plugins.NewRegistry(siteLogger)
		plugins.RegisterBuiltinPlugins(reg, cfg, eventLogger, siteLogger)

		if err := reg.InitAll(ctx); err != nil {
			// Critical plugin failed — log the error but continue. The site
			// will serve without plugin-contributed handlers.
			siteLogger.Error("per-site plugin init failed — site will serve without plugin handlers",
				slog.String("error", err.Error()),
			)
			continue
		}

		// Collect handlers from CaddyContributor plugins.
		var handlers []ports.CaddyHandler
		for _, contrib := range reg.CaddyContributors() {
			handlers = append(handlers, contrib.ContributeCaddyHandlers()...)
		}

		if len(handlers) > 0 {
			// Sort by ascending priority for deterministic ordering.
			sort.Slice(handlers, func(i, j int) bool {
				return handlers[i].Priority < handlers[j].Priority
			})
			perSiteHandlers[s.Name()] = handlers

			siteLogger.Info("plugin handlers collected",
				slog.Int("count", len(handlers)),
			)
		}
	}

	return perSiteHandlers, nil
}

// buildMultiSiteLogger creates an slog.Logger from the global log level string.
func buildMultiSiteLogger(level string) *slog.Logger {
	var slogLevel slog.Level
	switch level {
	case "debug":
		slogLevel = slog.LevelDebug
	case "warn":
		slogLevel = slog.LevelWarn
	case "error":
		slogLevel = slog.LevelError
	default:
		slogLevel = slog.LevelInfo
	}
	opts := &slog.HandlerOptions{Level: slogLevel}
	return slog.New(slog.NewJSONHandler(os.Stderr, opts))
}
