// Package tls implements the VibeWarden TLS plugin.
//
// The TLS plugin is responsible for:
//   - Validating TLS configuration on Init.
//   - Contributing TLS connection policies to the main Caddy HTTPS server.
//   - Contributing the TLS automation/certificate configuration to the Caddy tls app.
//   - Contributing the HTTP→HTTPS redirect server when TLS is enabled.
//
// Start and Stop are no-ops because TLS is fully managed by the Caddy runtime.
// Health reports whether TLS is enabled and the configured provider.
//
// The plugin implements ports.Plugin and ports.CaddyContributor. The TLS-specific
// Caddy config (connection policies, TLS app, redirect server) is exposed via
// dedicated methods that the Caddy config builder will call during wiring (issue #164).
package tls

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/vibewarden/vibewarden/internal/ports"
)

// Plugin is the TLS plugin for VibeWarden.
// It implements ports.Plugin and ports.CaddyContributor.
// The TLS plugin has priority 10 so it is configured before other plugins.
type Plugin struct {
	cfg     ports.TLSConfig
	logger  *slog.Logger
	monitor *CertMonitor
}

// New creates a new TLS Plugin with the given configuration and logger.
// eventLog may be nil; when non-nil, certificate expiry events are emitted through it.
// The metrics collector may be set later with SetMetricsCollector before Start is called.
func New(cfg ports.TLSConfig, eventLog ports.EventLogger, logger *slog.Logger) *Plugin {
	p := &Plugin{cfg: cfg, logger: logger}
	if cfg.Enabled && cfg.CertMonitoring.Enabled {
		p.monitor = NewCertMonitor(cfg, eventLog, nil, logger)
	}
	return p
}

// SetMetricsCollector injects a metrics collector into the certificate expiry
// monitor. It must be called before Start. When called with nil, the monitor
// emits no metrics.
func (p *Plugin) SetMetricsCollector(mc ports.MetricsCollector) {
	if p.monitor != nil {
		p.monitor.metrics = mc
	}
}

// Name returns the canonical plugin identifier "tls".
// This must match the key used under plugins: in vibewarden.yaml.
func (p *Plugin) Name() string { return "tls" }

// Priority returns the plugin's initialization priority.
// TLS is assigned priority 10 so it is configured before other plugins.
func (p *Plugin) Priority() int { return 10 }

// Init validates the TLS configuration. It returns an error if the
// configuration is inconsistent (e.g. domain missing for letsencrypt).
// Init must be called before ContributeCaddyRoutes, ContributeCaddyHandlers,
// TLSConnectionPolicies, TLSApp, and RedirectServer.
func (p *Plugin) Init(_ context.Context) error {
	if !p.cfg.Enabled {
		return nil
	}
	if err := validateTLSConfig(p.cfg); err != nil {
		return fmt.Errorf("tls plugin init: %w", err)
	}
	p.logger.Info("tls plugin initialised",
		slog.String("provider", string(p.cfg.Provider)),
		slog.String("domain", p.cfg.Domain),
	)
	return nil
}

// Start launches the certificate expiry monitor when monitoring is enabled.
// TLS termination itself is managed by the Caddy runtime.
func (p *Plugin) Start(ctx context.Context) error {
	if p.monitor != nil {
		p.monitor.Start(ctx)
	}
	return nil
}

// Stop shuts down the certificate expiry monitor when monitoring is enabled.
// TLS termination itself is managed by the Caddy runtime.
func (p *Plugin) Stop(_ context.Context) error {
	if p.monitor != nil {
		p.monitor.Stop()
	}
	return nil
}

// Health returns the current health status of the TLS plugin.
// When TLS is disabled, Health reports healthy with a "tls disabled" message.
// When TLS is enabled, Health reports healthy with the active provider, unless
// the certificate expiry monitor has detected that the certificate is within
// the critical threshold, in which case Health reports degraded.
func (p *Plugin) Health() ports.HealthStatus {
	if !p.cfg.Enabled {
		return ports.HealthStatus{
			Healthy: true,
			Message: "tls disabled",
		}
	}

	if p.monitor != nil {
		if degraded, msg := p.monitor.Degraded(); degraded {
			return ports.HealthStatus{
				Healthy: false,
				Message: fmt.Sprintf("tls degraded: %s", msg),
			}
		}
	}

	return ports.HealthStatus{
		Healthy: true,
		Message: fmt.Sprintf("tls enabled, provider: %s", p.cfg.Provider),
	}
}

// ContributeCaddyRoutes returns an empty slice.
// The TLS plugin does not add any named routes to the Caddy server block;
// it contributes at the server and app level via TLSConnectionPolicies,
// TLSApp, and RedirectServer.
func (p *Plugin) ContributeCaddyRoutes() []ports.CaddyRoute { return nil }

// ContributeCaddyHandlers returns an empty slice.
// The TLS plugin does not inject middleware into the catch-all handler chain.
func (p *Plugin) ContributeCaddyHandlers() []ports.CaddyHandler { return nil }

// TLSConnectionPolicies returns the Caddy tls_connection_policies slice to be
// set on the main HTTPS server block. Returns nil when TLS is disabled.
//
// For the external provider the policy references the operator-supplied
// certificate by tag. For all other providers an empty default policy lets
// Caddy select the certificate automatically.
func (p *Plugin) TLSConnectionPolicies() []map[string]any {
	if !p.cfg.Enabled {
		return nil
	}
	return buildTLSConnectionPolicies(p.cfg)
}

// TLSApp builds the Caddy "tls" application configuration for the chosen
// provider. Returns nil when TLS is disabled or no TLS app section is needed
// for the provider.
//
// An error is returned only for unknown provider values, which should have
// already been caught by Init — this is a defensive guard.
func (p *Plugin) TLSApp() (map[string]any, error) {
	if !p.cfg.Enabled {
		return nil, nil
	}
	app, err := buildTLSApp(p.cfg)
	if err != nil {
		return nil, fmt.Errorf("tls plugin: building tls app: %w", err)
	}
	return app, nil
}

// RedirectServer returns the Caddy HTTP→HTTPS redirect server configuration
// to be added as a sibling server alongside the main HTTPS server.
// Returns nil when TLS is disabled.
func (p *Plugin) RedirectServer() map[string]any {
	if !p.cfg.Enabled {
		return nil
	}
	return buildHTTPRedirectServer()
}

// ---------------------------------------------------------------------------
// Internal builders — pure functions, no side effects.
// ---------------------------------------------------------------------------

// validateTLSConfig checks that the TLS configuration is self-consistent.
func validateTLSConfig(cfg ports.TLSConfig) error {
	switch cfg.Provider {
	case ports.TLSProviderLetsEncrypt:
		if cfg.Domain == "" {
			return fmt.Errorf("domain is required for provider %q", cfg.Provider)
		}
	case ports.TLSProviderExternal:
		if cfg.CertPath == "" {
			return fmt.Errorf("cert_path is required for provider %q", cfg.Provider)
		}
		if cfg.KeyPath == "" {
			return fmt.Errorf("key_path is required for provider %q", cfg.Provider)
		}
	case ports.TLSProviderSelfSigned, "":
		// No additional fields required.
	default:
		return fmt.Errorf("unknown tls provider %q; valid values: letsencrypt, self-signed, external", cfg.Provider)
	}
	return nil
}

// buildTLSConnectionPolicies creates the Caddy tls_connection_policies slice.
// For the external provider the policy selects the operator-supplied certificate
// by tag. For all other providers an empty default policy lets Caddy pick.
func buildTLSConnectionPolicies(cfg ports.TLSConfig) []map[string]any {
	if cfg.Provider == ports.TLSProviderExternal {
		return []map[string]any{
			{
				"certificate_selection": map[string]any{
					"any_tag": []string{"vibewarden_external"},
				},
			},
		}
	}
	return []map[string]any{{}}
}

// buildTLSApp returns the Caddy "tls" app configuration for the chosen provider.
// Returns nil when no TLS app section is required.
func buildTLSApp(cfg ports.TLSConfig) (map[string]any, error) {
	switch cfg.Provider {
	case ports.TLSProviderLetsEncrypt:
		return buildLetsEncryptTLSApp(cfg), nil
	case ports.TLSProviderSelfSigned, "":
		return buildSelfSignedTLSApp(cfg), nil
	case ports.TLSProviderExternal:
		return buildExternalTLSApp(cfg), nil
	default:
		// Should have been caught by validateTLSConfig — defensive fallback.
		return nil, fmt.Errorf("unknown tls provider: %q", cfg.Provider)
	}
}

// buildLetsEncryptTLSApp returns a Caddy TLS app configuration that provisions
// certificates automatically via ACME (Let's Encrypt).
//
// The ACME issuer is configured with explicit HTTP-01 challenge support. The
// HTTP-01 challenge listener is bound to the same :80 port as the redirect
// server — Caddy manages both from within the same embedded instance, so no
// port conflict occurs.
//
// Note: storage is intentionally NOT set here. Caddy's storage backend is a
// top-level field on the Config struct; placing it inside apps.tls causes Caddy
// to reject the config with "unknown field: storage". Storage is set at the
// top-level in BuildCaddyConfig when cfg.StoragePath is non-empty.
func buildLetsEncryptTLSApp(cfg ports.TLSConfig) map[string]any {
	policy := map[string]any{
		"subjects": []string{cfg.Domain},
		"issuers": []map[string]any{
			{
				"module": "acme",
				// Configure HTTP-01 challenge explicitly. Caddy's embedded HTTP
				// server on :80 serves the ACME challenge response automatically;
				// specifying the port here makes the intent unambiguous and avoids
				// relying on Caddy's automatic challenge port detection when running
				// behind Docker networking.
				"challenges": map[string]any{
					"http": map[string]any{
						"alternate_port": 80,
					},
				},
			},
		},
	}

	return map[string]any{
		"automation": map[string]any{
			"policies": []map[string]any{policy},
		},
	}
}

// buildSelfSignedTLSApp returns a Caddy TLS app configuration that instructs
// Caddy to generate an internal self-signed certificate.
// This is intended for local development and testing only.
//
// Note: storage is intentionally NOT set here. See buildLetsEncryptTLSApp for
// the explanation of why storage belongs at the top-level Caddy config.
func buildSelfSignedTLSApp(cfg ports.TLSConfig) map[string]any {
	policy := map[string]any{
		"issuers": []map[string]any{
			{"module": "internal"},
		},
	}

	// Scope the policy to the domain when one is provided.
	if cfg.Domain != "" {
		policy["subjects"] = []string{cfg.Domain}
	}

	return map[string]any{
		"automation": map[string]any{
			"policies": []map[string]any{policy},
		},
	}
}

// buildExternalTLSApp returns a Caddy TLS app configuration that loads
// certificates from the file paths supplied by the operator.
func buildExternalTLSApp(cfg ports.TLSConfig) map[string]any {
	return map[string]any{
		"certificates": map[string]any{
			"load_files": []map[string]any{
				{
					"certificate": cfg.CertPath,
					"key":         cfg.KeyPath,
					"tags":        []string{"vibewarden_external"},
				},
			},
		},
	}
}

// buildHTTPRedirectServer returns a Caddy server configuration that permanently
// (HTTP 301) redirects all plain HTTP requests to HTTPS.
func buildHTTPRedirectServer() map[string]any {
	return map[string]any{
		"listen": []string{":80"},
		"routes": []map[string]any{
			{
				"handle": []map[string]any{
					{
						"handler": "static_response",
						"headers": map[string][]string{
							"Location": {"https://{http.request.host}{http.request.uri}"},
						},
						"status_code": 301,
					},
				},
			},
		},
		"automatic_https": map[string]any{"disable": true},
	}
}
