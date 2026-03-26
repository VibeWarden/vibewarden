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
	cfg    ports.TLSConfig
	logger *slog.Logger
}

// New creates a new TLS Plugin with the given configuration and logger.
func New(cfg ports.TLSConfig, logger *slog.Logger) *Plugin {
	return &Plugin{cfg: cfg, logger: logger}
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

// Start is a no-op for the TLS plugin. TLS lifecycle is managed by Caddy.
func (p *Plugin) Start(_ context.Context) error { return nil }

// Stop is a no-op for the TLS plugin. TLS lifecycle is managed by Caddy.
func (p *Plugin) Stop(_ context.Context) error { return nil }

// Health returns the current health status of the TLS plugin.
// When TLS is disabled, Health reports healthy with a "tls disabled" message.
// When TLS is enabled, Health reports healthy with the active provider.
func (p *Plugin) Health() ports.HealthStatus {
	if !p.cfg.Enabled {
		return ports.HealthStatus{
			Healthy: true,
			Message: "tls disabled",
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
func buildLetsEncryptTLSApp(cfg ports.TLSConfig) map[string]any {
	policy := map[string]any{
		"subjects": []string{cfg.Domain},
		"issuers": []map[string]any{
			{"module": "acme"},
		},
	}

	tlsApp := map[string]any{
		"automation": map[string]any{
			"policies": []map[string]any{policy},
		},
	}

	if cfg.StoragePath != "" {
		tlsApp["storage"] = map[string]any{
			"module": "file_system",
			"root":   cfg.StoragePath,
		}
	}

	return tlsApp
}

// buildSelfSignedTLSApp returns a Caddy TLS app configuration that instructs
// Caddy to generate an internal self-signed certificate.
// This is intended for local development and testing only.
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

	tlsApp := map[string]any{
		"automation": map[string]any{
			"policies": []map[string]any{policy},
		},
	}

	if cfg.StoragePath != "" {
		tlsApp["storage"] = map[string]any{
			"module": "file_system",
			"root":   cfg.StoragePath,
		}
	}

	return tlsApp
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
