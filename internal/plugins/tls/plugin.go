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

	"github.com/vibewarden/vibewarden/internal/domain/events"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// Plugin is the TLS plugin for VibeWarden.
// It implements ports.Plugin and ports.CaddyContributor.
// The TLS plugin has priority 10 so it is configured before other plugins.
type Plugin struct {
	cfg      ports.TLSConfig
	logger   *slog.Logger
	monitor  *CertMonitor
	eventLog ports.EventLogger
}

// New creates a new TLS Plugin with the given configuration and logger.
// eventLog may be nil; when non-nil, certificate expiry and ACME chain
// configuration events are emitted through it.
// The metrics collector may be set later with SetMetricsCollector before Start is called.
func New(cfg ports.TLSConfig, eventLog ports.EventLogger, logger *slog.Logger) *Plugin {
	p := &Plugin{cfg: cfg, logger: logger, eventLog: eventLog}
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

// Init validates the TLS configuration and emits ACME chain observability
// events. It returns an error if the configuration is inconsistent (e.g.
// domain missing for letsencrypt). Init must be called before
// ContributeCaddyRoutes, ContributeCaddyHandlers, TLSConnectionPolicies,
// TLSApp, and RedirectServer.
//
// ACME observability (per ADR-083):
//   - One tls.acme.chain_skipped event is emitted per issuer evaluated for
//     the default chain but excluded (e.g. ZeroSSL when tls.email is empty).
//   - A tls.acme.provider_deprecated event is emitted when provider=buypass
//     is explicitly selected (Buypass's ACME directory currently returns 403).
//   - A tls.acme.chain_configured event is always emitted for ACME providers
//     with the resolved chain, so operators have a single grepable signal.
//
// tls.acme.chain_fallback (runtime issuer transitions) is reserved in the
// schema but not emitted here: Caddy/certmagic/acmez does not expose a
// stable hook for issuer transitions at the time of writing. See ADR-083 §3b.
//
// Init does not panic when p.eventLog is nil; event emission is skipped and
// the structured logger captures the same information.
func (p *Plugin) Init(ctx context.Context) error {
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

	// Emit ACME chain events only for ACME providers. For self-signed and
	// external providers the chain concept does not apply.
	if isACMEProvider(p.cfg.Provider) {
		p.emitACMEChainEvents(ctx)
	}
	return nil
}

// emitACMEChainEvents resolves the ACME fallback chain for the plugin's
// configuration and emits the three observability events described in
// ADR-083 §3 (chain_skipped, provider_deprecated, chain_configured).
//
// Nil p.eventLog is safe: event emission is skipped and the slog logger
// captures a Warn record with the same classification so the operator is
// still notified at debug-level log channels.
func (p *Plugin) emitACMEChainEvents(ctx context.Context) {
	issuers, skipped := buildACMEIssuers(p.cfg)

	// Emit one chain_skipped event per excluded issuer.
	for _, sk := range skipped {
		evt := events.NewTLSACMEChainSkipped(events.TLSACMEChainSkippedParams{
			Provider:        sk.Provider,
			Reason:          sk.Reason,
			PrimaryProvider: string(p.cfg.Provider),
		})
		p.logEvent(ctx, evt, "acme issuer skipped from fallback chain",
			slog.String("provider", sk.Provider),
			slog.String("reason", sk.Reason),
		)
	}

	// Warn operators when they explicitly opted in to a deprecated provider.
	if p.cfg.Provider == ports.TLSProviderBuypass {
		evt := events.NewTLSACMEProviderDeprecated(events.TLSACMEProviderDeprecatedParams{
			Provider: string(ports.TLSProviderBuypass),
			Reason:   "directory_returns_403",
			Guidance: "consider provider: letsencrypt with tls.email",
		})
		p.logEvent(ctx, evt, "acme provider deprecated",
			slog.String("provider", string(ports.TLSProviderBuypass)),
		)
	}

	// Always publish the resolved chain. This is the grepable single-
	// source-of-truth signal for operators and log aggregators.
	evt := events.NewTLSACMEChainConfigured(events.TLSACMEChainConfiguredParams{
		PrimaryProvider: string(p.cfg.Provider),
		ResolvedChain:   resolvedChainProviders(p.cfg, issuers),
		Domain:          p.cfg.Domain,
	})
	p.logEvent(ctx, evt, "acme fallback chain configured",
		slog.String("primary_provider", string(p.cfg.Provider)),
		slog.String("domain", p.cfg.Domain),
	)
}

// logEvent forwards an event to the EventLogger when configured, falling
// back to a slog.Warn with the same structured fields when no logger is
// attached. Emission failures are logged but do not bubble up — event
// emission is best-effort per ports.EventLogger's contract.
func (p *Plugin) logEvent(ctx context.Context, evt events.Event, fallbackMsg string, fallbackAttrs ...slog.Attr) {
	if p.eventLog == nil {
		args := make([]any, 0, len(fallbackAttrs))
		for _, a := range fallbackAttrs {
			args = append(args, a)
		}
		p.logger.Warn(fallbackMsg, args...)
		return
	}
	if err := p.eventLog.Log(ctx, evt); err != nil {
		p.logger.Warn("failed to emit acme chain event",
			slog.String("event_type", evt.EventType),
			slog.String("error", err.Error()),
		)
	}
}

// resolvedChainProviders maps the Caddy-shaped issuer list back to short
// provider identifiers (e.g. "letsencrypt", "zerossl") for the
// tls.acme.chain_configured payload. For provider=letsencrypt with an
// acme_ca override, a single "custom" entry is used to avoid leaking the
// raw URL into structured logs.
func resolvedChainProviders(cfg ports.TLSConfig, issuers []map[string]any) []string {
	out := make([]string, 0, len(issuers))
	for _, iss := range issuers {
		caStr, _ := iss["ca"].(string)
		out = append(out, providerFromCA(cfg, caStr))
	}
	return out
}

// providerFromCA returns the short provider identifier corresponding to a
// Caddy ACME issuer's ca URL. An acme_ca override collapses to "custom"
// so the operator's private directory URL does not end up in log events.
func providerFromCA(cfg ports.TLSConfig, ca string) string {
	if cfg.ACMECA != "" && ca == cfg.ACMECA {
		return "custom"
	}
	switch ca {
	case acmeCALetsEncrypt:
		return string(ports.TLSProviderLetsEncrypt)
	case acmeCALetsEncryptStaging:
		return string(ports.TLSProviderLetsEncryptStaging)
	case acmeCAZeroSSL:
		return string(ports.TLSProviderZeroSSL)
	case acmeCABuypass:
		return string(ports.TLSProviderBuypass)
	default:
		return "custom"
	}
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
	case ports.TLSProviderLetsEncrypt,
		ports.TLSProviderBuypass,
		ports.TLSProviderLetsEncryptStaging:
		if cfg.Domain == "" {
			return fmt.Errorf("domain is required for provider %q", cfg.Provider)
		}
	case ports.TLSProviderZeroSSL:
		if cfg.Domain == "" {
			return fmt.Errorf("domain is required for provider %q", cfg.Provider)
		}
		if cfg.Email == "" {
			return fmt.Errorf("email is required for provider %q (ZeroSSL needs it for automatic EAB registration)", cfg.Provider)
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
		return fmt.Errorf("unknown tls provider %q; valid values: letsencrypt, zerossl, buypass, letsencrypt-staging, self-signed, external", cfg.Provider)
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

// isACMEProvider returns true when the TLS provider uses ACME for certificate
// provisioning.
func isACMEProvider(provider ports.TLSProvider) bool {
	switch provider {
	case ports.TLSProviderLetsEncrypt,
		ports.TLSProviderZeroSSL,
		ports.TLSProviderBuypass,
		ports.TLSProviderLetsEncryptStaging:
		return true
	default:
		return false
	}
}

// buildTLSApp returns the Caddy "tls" app configuration for the chosen provider.
// Returns nil when no TLS app section is required.
func buildTLSApp(cfg ports.TLSConfig) (map[string]any, error) {
	if isACMEProvider(cfg.Provider) {
		return buildACMETLSApp(cfg), nil
	}
	switch cfg.Provider {
	case ports.TLSProviderSelfSigned, "":
		return buildSelfSignedTLSApp(cfg), nil
	case ports.TLSProviderExternal:
		return buildExternalTLSApp(cfg), nil
	default:
		// Should have been caught by validateTLSConfig — defensive fallback.
		return nil, fmt.Errorf("unknown tls provider: %q", cfg.Provider)
	}
}

// MUST MIRROR: internal/adapters/caddy/acme_issuers.go
//
// The constants, SkippedIssuer struct, and buildACMEIssuers function below
// are intentionally duplicated from the caddy adapter because the adapter
// package and the plugin package cannot import each other without breaking
// the hexagonal boundary. Any change here must be mirrored byte-for-byte in
// internal/adapters/caddy/acme_issuers.go (the primary source of truth).
// De-duplication is tracked as a follow-up per ADR-083 §4.

// ACME directory URLs for supported certificate authorities.
const (
	acmeCALetsEncrypt        = "https://acme-v02.api.letsencrypt.org/directory"
	acmeCALetsEncryptStaging = "https://acme-staging-v02.api.letsencrypt.org/directory"
	acmeCAZeroSSL            = "https://acme.zerossl.com/v2/DV90"
	// acmeCABuypass — Buypass's ACME directory currently returns 403
	// Forbidden in production (#1055) and has therefore been removed from
	// the default fallback chain. It remains selectable as an explicit
	// provider opt-in.
	acmeCABuypass = "https://api.buypass.com/acme/directory"
)

// Machine-readable reason codes for skipped issuers. The v1 schema freezes
// the set of allowed values; see ADR-083 §3a.
const (
	skipReasonEmailNotConfigured = "email_not_configured"
)

// SkippedIssuer records an issuer that was evaluated for the default ACME
// fallback chain but excluded (e.g. ZeroSSL skipped because tls.email is
// empty). Callers emit one tls.acme.chain_skipped event per entry.
type SkippedIssuer struct {
	// Provider is the short identifier of the skipped issuer (e.g. "zerossl").
	Provider string

	// Reason is a machine-readable skip reason (e.g. "email_not_configured").
	Reason string
}

// buildACMETLSApp returns a Caddy TLS app configuration that provisions
// certificates automatically via ACME. For `provider: letsencrypt` without
// an acme_ca override the chain is [letsencrypt] (empty tls.email) or
// [letsencrypt, zerossl] (with tls.email); for specific providers
// (zerossl, buypass, letsencrypt-staging) a single issuer targeting that CA
// is used. See ADR-083 for the rationale behind the Buypass removal and
// ZeroSSL email-preflight.
//
// Note: storage is intentionally NOT set here. Caddy's storage backend is a
// top-level field on the Config struct; placing it inside apps.tls causes Caddy
// to reject the config with "unknown field: storage". Storage is set at the
// top-level in BuildCaddyConfig when cfg.StoragePath is non-empty.
func buildACMETLSApp(cfg ports.TLSConfig) map[string]any {
	issuers, _ := buildACMEIssuers(cfg)
	policy := map[string]any{
		"subjects": []string{cfg.Domain},
		"issuers":  issuers,
	}

	return map[string]any{
		"automation": map[string]any{
			"policies": []map[string]any{policy},
		},
	}
}

// buildACMEIssuers constructs the ordered list of ACME issuer configurations
// and reports any issuers that were evaluated but excluded from the default
// chain. See the primary copy in internal/adapters/caddy/acme_issuers.go for
// full behavioural documentation.
func buildACMEIssuers(cfg ports.TLSConfig) (issuers []map[string]any, skipped []SkippedIssuer) {
	email := cfg.Email

	switch cfg.Provider {
	case ports.TLSProviderLetsEncrypt:
		if cfg.ACMECA != "" {
			return []map[string]any{buildSingleACMEIssuer(cfg.ACMECA, email)}, nil
		}
		chain := []map[string]any{
			buildSingleACMEIssuer(acmeCALetsEncrypt, email),
		}
		if email == "" {
			return chain, []SkippedIssuer{
				{Provider: string(ports.TLSProviderZeroSSL), Reason: skipReasonEmailNotConfigured},
			}
		}
		chain = append(chain, buildSingleACMEIssuer(acmeCAZeroSSL, email))
		return chain, nil
	case ports.TLSProviderZeroSSL:
		return []map[string]any{buildSingleACMEIssuer(acmeCAZeroSSL, email)}, nil
	case ports.TLSProviderBuypass:
		return []map[string]any{buildSingleACMEIssuer(acmeCABuypass, email)}, nil
	case ports.TLSProviderLetsEncryptStaging:
		return []map[string]any{buildSingleACMEIssuer(acmeCALetsEncryptStaging, email)}, nil
	default:
		return []map[string]any{buildSingleACMEIssuer(acmeCALetsEncrypt, email)}, nil
	}
}

// buildSingleACMEIssuer returns an ACME issuer map targeting the given CA.
func buildSingleACMEIssuer(ca, email string) map[string]any {
	issuer := map[string]any{
		"module": "acme",
		"ca":     ca,
	}
	if email != "" {
		issuer["email"] = email
	}
	return issuer
}

// buildSelfSignedTLSApp returns a Caddy TLS app configuration that instructs
// Caddy to generate an internal self-signed certificate.
// This is intended for local development and testing only.
//
// Note: storage is intentionally NOT set here. See buildACMETLSApp for
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
