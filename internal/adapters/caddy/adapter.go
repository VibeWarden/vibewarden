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
	"github.com/vibewarden/vibewarden/internal/ports"
)

// Adapter implements ports.ProxyServer using embedded Caddy.
type Adapter struct {
	config      *ports.ProxyConfig
	logger      *slog.Logger
	eventLogger ports.EventLogger
}

// NewAdapter creates a new Caddy adapter with the given configuration.
// The eventLogger parameter is optional: pass nil to disable structured event
// logging (the adapter will still emit plain slog lines).
func NewAdapter(cfg *ports.ProxyConfig, logger *slog.Logger, eventLogger ports.EventLogger) *Adapter {
	return &Adapter{
		config:      cfg,
		logger:      logger,
		eventLogger: eventLogger,
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
func (a *Adapter) buildConfigJSON() ([]byte, error) {
	cfg, err := BuildCaddyConfig(a.config)
	if err != nil {
		return nil, err
	}

	data, err := json.Marshal(cfg)
	if err != nil {
		return nil, fmt.Errorf("marshaling caddy config: %w", err)
	}

	return data, nil
}
