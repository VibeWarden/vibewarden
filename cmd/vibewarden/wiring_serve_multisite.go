package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	caddyadapter "github.com/vibewarden/vibewarden/internal/adapters/caddy"
	fsnotifyadapter "github.com/vibewarden/vibewarden/internal/adapters/fsnotify"
	logadapter "github.com/vibewarden/vibewarden/internal/adapters/log"
	"github.com/vibewarden/vibewarden/internal/app/proxy"
	reloadsvc "github.com/vibewarden/vibewarden/internal/app/reload"
	"github.com/vibewarden/vibewarden/internal/config/sites"
	"github.com/vibewarden/vibewarden/internal/domain/site"
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

	// Step 6: Create the multi-site Caddy adapter.
	// A minimal ProxyConfig is needed for backward-compatible fields (version).
	minimalProxyCfg := &ports.ProxyConfig{
		ListenAddr: fmt.Sprintf("%s:%d", globalCfg.ListenHost, globalCfg.ListenPort),
		Version:    ver,
	}
	adapter := caddyadapter.NewMultiSiteAdapter(minimalProxyCfg, registry, logger, eventLogger)

	// Step 7: Set up signal handling.
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

	// Step 8: Start the site watcher for hot-reload.
	siteWatcher := fsnotifyadapter.NewSiteWatcher(logger)
	watchCh, watchErr := siteWatcher.Watch(ctx, sitesDir)
	if watchErr != nil {
		logger.Warn("site watcher failed to start",
			slog.String("path", sitesDir),
			slog.String("error", watchErr.Error()),
		)
	} else {
		// Step 9: Create the MultiSiteService event loop.
		reloadFn := func(reloadCtx context.Context) error {
			return adapter.Reload(reloadCtx)
		}
		multiSiteSvc := reloadsvc.NewMultiSiteService(registry, eventLogger, logger, reloadFn)
		go multiSiteSvc.Run(ctx, watchCh)
	}

	// Step 10: Run the proxy until shutdown.
	svc := proxy.NewService(adapter, logger)
	if err := svc.Run(ctx); err != nil {
		return fmt.Errorf("proxy service: %w", err)
	}

	return nil
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
