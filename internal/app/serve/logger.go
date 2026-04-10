// Package serve provides the RunServe application service that orchestrates the
// VibeWarden startup sequence: config loading, plugin registration, Caddy wiring,
// and graceful shutdown. It is importable by both the OSS binary and the Pro binary.
package serve

import (
	"log/slog"
	"os"

	logadapter "github.com/vibewarden/vibewarden/internal/adapters/log"
	"github.com/vibewarden/vibewarden/internal/config"
	"github.com/vibewarden/vibewarden/internal/plugins"
	metricsplugin "github.com/vibewarden/vibewarden/internal/plugins/metrics"
	"github.com/vibewarden/vibewarden/internal/ports"
)

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

// buildEventLogger constructs the event logger used by the caddy adapter and
// other consumers. When the metrics plugin has an OTel log handler available,
// the logger fans out to both stdout JSON and OTel via a MultiHandler.
// Falls back to stdout-only when log export is disabled or unavailable.
//
// The ring buffer is always wired as an additional sink so that the admin
// events endpoint can serve recent structured events without a database.
func buildEventLogger(registry *plugins.Registry, logger *slog.Logger, ringBuf ports.EventLogger) ports.EventLogger {
	var slogLogger ports.EventLogger
	for _, p := range registry.Plugins() {
		if mp, ok := p.(*metricsplugin.Plugin); ok {
			if h := mp.LogHandler(); h != nil {
				logger.Info("event logger: OTel log export enabled")
				slogLogger = logadapter.NewSlogEventLogger(os.Stdout, h)
			}
			break
		}
	}
	if slogLogger == nil {
		slogLogger = logadapter.NewSlogEventLogger(os.Stdout)
	}
	return logadapter.NewMultiEventLogger(slogLogger, ringBuf)
}
