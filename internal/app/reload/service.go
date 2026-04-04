// Package reload provides the hot config reload application service.
// It orchestrates validation, state update, and proxy reconfiguration when
// the vibewarden.yaml file changes or an operator triggers a reload via the
// admin API.
package reload

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/vibewarden/vibewarden/internal/config"
	"github.com/vibewarden/vibewarden/internal/domain/events"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// Service orchestrates configuration hot reload.
// It is the single source of truth for the currently active configuration and
// coordinates reload across all dependent components.
type Service struct {
	mu         sync.RWMutex
	configPath string
	currentCfg *config.Config
	proxy      ports.ProxyServer
	eventLog   ports.EventLogger
	logger     *slog.Logger

	// rebuildProxyConfig rebuilds the ProxyConfig from the supplied Config.
	// Injected from serve.go so the service does not need to know about the
	// plugin registry.
	rebuildProxyConfig func(*config.Config) *ports.ProxyConfig
}

// NewService creates a Service. initialCfg is the configuration that was
// loaded at startup. rebuildFn must not be nil.
func NewService(
	configPath string,
	initialCfg *config.Config,
	proxy ports.ProxyServer,
	eventLog ports.EventLogger,
	logger *slog.Logger,
	rebuildFn func(*config.Config) *ports.ProxyConfig,
) *Service {
	return &Service{
		configPath:         configPath,
		currentCfg:         initialCfg,
		proxy:              proxy,
		eventLog:           eventLog,
		logger:             logger,
		rebuildProxyConfig: rebuildFn,
	}
}

// Reload implements ports.ConfigReloader.Reload.
//
// Steps:
//  1. Load config from disk.
//  2. Validate config.
//  3. If invalid: emit config.reload_failed, return error.
//  4. Acquire write lock.
//  5. Store new config as current.
//  6. Rebuild ProxyConfig.
//  7. Call proxyServer.Reload(ctx).
//  8. Release lock.
//  9. Emit config.reloaded.
//  10. Return nil.
func (s *Service) Reload(ctx context.Context, source string) error {
	start := time.Now()

	newCfg, err := config.Load(s.configPath)
	if err != nil {
		reason := err.Error()
		s.logger.Error("config reload failed",
			slog.String("source", source),
			slog.String("error", reason),
		)
		s.emitReloadFailed(ctx, source, reason, nil)
		return fmt.Errorf("loading config: %w", err)
	}

	s.mu.Lock()
	s.currentCfg = newCfg
	proxyCfg := s.rebuildProxyConfig(newCfg)
	s.mu.Unlock()

	// Update the proxy adapter's config before calling Reload.
	// The Caddy adapter's Reload() method uses its stored config field, so we
	// need to point it at the new ProxyConfig. We do this by checking whether
	// the proxy implements ConfigUpdater (an optional extension interface).
	if cu, ok := s.proxy.(ConfigUpdater); ok {
		cu.UpdateConfig(proxyCfg)
	}

	if err := s.proxy.Reload(ctx); err != nil {
		reason := err.Error()
		s.logger.Error("proxy reload failed",
			slog.String("source", source),
			slog.String("error", reason),
		)
		s.emitReloadFailed(ctx, source, "proxy reload failed: "+reason, nil)
		return fmt.Errorf("reloading proxy: %w", err)
	}

	durationMS := time.Since(start).Milliseconds()
	s.logger.Info("config reloaded",
		slog.String("source", source),
		slog.Int64("duration_ms", durationMS),
	)

	s.emitReloaded(ctx, source, durationMS)
	return nil
}

// CurrentConfig implements ports.ConfigReloader.CurrentConfig.
// Returns a redacted copy of the currently active configuration.
func (s *Service) CurrentConfig() ports.RedactedConfig {
	s.mu.RLock()
	cfg := s.currentCfg
	s.mu.RUnlock()
	return config.Redact(cfg)
}

// Config returns the currently active config.Config (unredacted, for internal
// use only). The returned pointer must not be modified by the caller.
func (s *Service) Config() *config.Config {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.currentCfg
}

// ------------------------------------------------------------------
// Private helpers
// ------------------------------------------------------------------

func (s *Service) emitReloaded(ctx context.Context, source string, durationMS int64) {
	if s.eventLog == nil {
		return
	}
	ev := events.NewConfigReloaded(events.ConfigReloadedParams{
		ConfigPath:    s.configPath,
		TriggerSource: source,
		DurationMS:    durationMS,
	})
	if err := s.eventLog.Log(ctx, ev); err != nil {
		s.logger.Error("failed to emit config.reloaded event", slog.String("error", err.Error()))
	}
}

func (s *Service) emitReloadFailed(ctx context.Context, source, reason string, validationErrs []string) {
	if s.eventLog == nil {
		return
	}
	ev := events.NewConfigReloadFailed(events.ConfigReloadFailedParams{
		ConfigPath:       s.configPath,
		TriggerSource:    source,
		Reason:           reason,
		ValidationErrors: validationErrs,
	})
	if err := s.eventLog.Log(ctx, ev); err != nil {
		s.logger.Error("failed to emit config.reload_failed event", slog.String("error", err.Error()))
	}
}

// ------------------------------------------------------------------
// Optional extension interface
// ------------------------------------------------------------------

// ConfigUpdater is an optional interface that a ProxyServer may implement to
// accept a new ProxyConfig before a Reload call. It allows the reload service
// to update the adapter's configuration without knowing the concrete type.
type ConfigUpdater interface {
	UpdateConfig(cfg *ports.ProxyConfig)
}
