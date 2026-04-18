// Package caddy implements the ProxyServer port using embedded Caddy.
package caddy

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/caddyserver/caddy/v2"

	// Import Caddy standard modules so they are registered with the Caddy module system.
	_ "github.com/caddyserver/caddy/v2/modules/standard"

	"github.com/vibewarden/vibewarden/internal/domain/events"
	"github.com/vibewarden/vibewarden/internal/domain/site"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// Adapter implements ports.ProxyServer using embedded Caddy.
//
// When registry is non-nil, the adapter operates in multi-site mode:
// buildConfigJSON delegates to BuildMultiSiteConfig, producing per-host
// routes from the registry's healthy sites. When registry is nil, the
// adapter falls back to single-site mode using BuildCaddyConfig.
type Adapter struct {
	config          *ports.ProxyConfig
	registry        *site.Registry
	perSiteHandlers map[string][]ports.CaddyHandler
	logger          *slog.Logger
	eventLogger     ports.EventLogger
}

// NewAdapter creates a new Caddy adapter in single-site mode.
// The eventLogger parameter is optional: pass nil to disable structured event
// logging (the adapter will still emit plain slog lines).
func NewAdapter(cfg *ports.ProxyConfig, logger *slog.Logger, eventLogger ports.EventLogger) *Adapter {
	return &Adapter{
		config:      cfg,
		logger:      logger,
		eventLogger: eventLogger,
	}
}

// NewMultiSiteAdapter creates a Caddy adapter in multi-site mode.
// The registry holds all managed sites and the global configuration.
// When buildConfigJSON is called, it reads the registry's healthy sites
// and global config to produce per-host Caddy routes.
//
// perSiteHandlers maps each site name to the Caddy handlers contributed by
// that site's plugin registry (e.g. WAF, auth, CORS). Pass nil to omit
// plugin-contributed handlers.
//
// The cfg parameter is still required for backward-compatible fields
// (version, event logging params) that are sidecar-global, not per-site.
func NewMultiSiteAdapter(cfg *ports.ProxyConfig, registry *site.Registry, perSiteHandlers map[string][]ports.CaddyHandler, logger *slog.Logger, eventLogger ports.EventLogger) *Adapter {
	return &Adapter{
		config:          cfg,
		registry:        registry,
		perSiteHandlers: perSiteHandlers,
		logger:          logger,
		eventLogger:     eventLogger,
	}
}

// Start implements ports.ProxyServer.Start.
// It builds the Caddy JSON configuration, loads it, and blocks until the context is cancelled.
func (a *Adapter) Start(ctx context.Context) error {
	cfgJSON, err := a.buildConfigJSON()
	if err != nil {
		return fmt.Errorf("building caddy config: %w", err)
	}

	if err := caddy.Load(cfgJSON, true); err != nil {
		return fmt.Errorf("loading caddy config: %w", err)
	}

	if a.eventLogger != nil {
		a.emitStartEvents(ctx)
	}

	// Block until context is cancelled.
	<-ctx.Done()

	return nil
}

// Stop implements ports.ProxyServer.Stop.
// It gracefully shuts down the Caddy instance.
func (a *Adapter) Stop(_ context.Context) error {
	a.logger.Info("stopping caddy proxy")
	if err := caddy.Stop(); err != nil {
		return fmt.Errorf("stopping caddy: %w", err)
	}
	return nil
}

// Reload implements ports.ProxyServer.Reload.
// It applies configuration changes without dropping connections.
func (a *Adapter) Reload(_ context.Context) error {
	cfgJSON, err := a.buildConfigJSON()
	if err != nil {
		return fmt.Errorf("building caddy config: %w", err)
	}

	a.logger.Info("reloading caddy configuration")

	if err := caddy.Load(cfgJSON, true); err != nil {
		return fmt.Errorf("reloading caddy config: %w", err)
	}
	return nil
}

// UpdateConfig replaces the adapter's ProxyConfig with the supplied value.
// It is called by the reload service immediately before Reload so that the
// next caddy.Load call uses the updated settings.
func (a *Adapter) UpdateConfig(cfg *ports.ProxyConfig) {
	a.config = cfg
}

// buildConfigJSON constructs and marshals the Caddy JSON configuration.
// When a registry is present, multi-site mode is used. Otherwise, the
// adapter falls back to single-site mode for backward compatibility.
func (a *Adapter) buildConfigJSON() ([]byte, error) {
	var (
		cfg map[string]any
		err error
	)

	if a.registry != nil {
		global := a.registry.Global()
		if global == nil {
			g := site.DefaultGlobalConfig()
			global = &g
		}
		cfg, err = BuildMultiSiteConfig(a.registry.HealthySites(), *global, a.perSiteHandlers, a.logger)
	} else {
		cfg, err = BuildCaddyConfig(a.config)
	}

	if err != nil {
		return nil, err
	}

	data, err := json.Marshal(cfg)
	if err != nil {
		return nil, fmt.Errorf("marshaling caddy config: %w", err)
	}

	return data, nil
}

// emitStartEvents emits proxy.started structured events after the Caddy
// server has loaded its configuration successfully.
//
// In single-site mode, a single event is emitted with the values from the
// adapter's ProxyConfig. In multi-site mode, one event is emitted per
// healthy site in the registry with that site's actual TLS, upstream, and
// security header settings — avoiding the misleading zero-value event that
// would result from the minimal multi-site ProxyConfig.
//
// If SkipStartEvent is set on the ProxyConfig, no events are emitted. This
// escape hatch exists for callers that wish to emit events themselves.
func (a *Adapter) emitStartEvents(ctx context.Context) {
	if a.config.SkipStartEvent {
		return
	}

	if a.registry != nil {
		a.emitMultiSiteStartEvents(ctx)
		return
	}

	ev := events.NewProxyStarted(events.ProxyStartedParams{
		ListenAddr:             a.config.ListenAddr,
		UpstreamAddr:           a.config.UpstreamAddr,
		TLSEnabled:             a.config.TLS.Enabled,
		TLSProvider:            string(a.config.TLS.Provider),
		SecurityHeadersEnabled: a.config.SecurityHeaders.Enabled,
		Version:                a.config.Version,
	})
	if logErr := a.eventLogger.Log(ctx, ev); logErr != nil {
		a.logger.Error("failed to emit proxy.started event", slog.String("error", logErr.Error()))
	}
}

// emitMultiSiteStartEvents emits one proxy.started event per healthy site in
// the registry, using each site's actual configuration values.
func (a *Adapter) emitMultiSiteStartEvents(ctx context.Context) {
	for _, s := range a.registry.HealthySites() {
		cfg := s.Config()
		if cfg == nil {
			continue
		}

		upstreamAddr := fmt.Sprintf("%s:%d", cfg.Upstream.Host, cfg.Upstream.Port)

		ev := events.NewProxyStarted(events.ProxyStartedParams{
			ListenAddr:             a.config.ListenAddr,
			UpstreamAddr:           upstreamAddr,
			TLSEnabled:             cfg.TLS.Enabled,
			TLSProvider:            cfg.TLS.Provider,
			SecurityHeadersEnabled: cfg.SecurityHeaders.Enabled,
			Version:                a.config.Version,
		})
		if logErr := a.eventLogger.Log(ctx, ev); logErr != nil {
			a.logger.Error("failed to emit proxy.started event",
				slog.String("site", s.Name()),
				slog.String("error", logErr.Error()),
			)
		}
	}
}
