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

	"github.com/vibewarden/vibewarden/internal/ports"
)

// Adapter implements ports.ProxyServer using embedded Caddy.
type Adapter struct {
	config *ports.ProxyConfig
	logger *slog.Logger
}

// NewAdapter creates a new Caddy adapter with the given configuration.
func NewAdapter(cfg *ports.ProxyConfig, logger *slog.Logger) *Adapter {
	return &Adapter{
		config: cfg,
		logger: logger,
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

	a.logger.Info("proxy started",
		slog.String("schema_version", "v1"),
		slog.String("event_type", "proxy.started"),
		slog.String("ai_summary", fmt.Sprintf("Reverse proxy listening on %s, forwarding to %s", a.config.ListenAddr, a.config.UpstreamAddr)),
		slog.Group("payload",
			slog.String("listen", a.config.ListenAddr),
			slog.String("upstream", a.config.UpstreamAddr),
			slog.Bool("tls_enabled", a.config.TLS.Enabled),
			slog.String("tls_provider", string(a.config.TLS.Provider)),
			slog.Bool("security_headers_enabled", a.config.SecurityHeaders.Enabled),
			slog.String("version", a.config.Version),
		),
	)

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
