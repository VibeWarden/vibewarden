package webhooksig

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	caddyadapter "github.com/vibewarden/vibewarden/internal/adapters/caddy"
	domainwebhook "github.com/vibewarden/vibewarden/internal/domain/webhook"
	"github.com/vibewarden/vibewarden/internal/middleware"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// Plugin is the VibeWarden inbound webhook signature verification plugin.
// It implements ports.Plugin and ports.CaddyContributor.
//
// When enabled, the plugin registers a Caddy handler at priority 35 — after
// admin auth (30) but before rate limiting (50) — that verifies HMAC
// signatures on inbound webhook requests. Unmatched paths are passed through
// unchanged.
type Plugin struct {
	cfg       Config
	logger    *slog.Logger
	rules     []middleware.WebhookSignatureRule
	healthy   bool
	healthMsg string
}

// New creates a new webhook signature verification Plugin.
func New(cfg Config, logger *slog.Logger) *Plugin {
	return &Plugin{
		cfg:       cfg,
		logger:    logger,
		healthMsg: "not initialised",
	}
}

// Name returns the canonical plugin identifier "webhook-signature".
func (p *Plugin) Name() string { return "webhook-signature" }

// Priority returns 35 — after admin auth at 30 and before rate limiting at 50.
func (p *Plugin) Priority() int { return 35 }

// Init validates the configuration and resolves secret env var values.
func (p *Plugin) Init(_ context.Context) error {
	if !p.cfg.Enabled {
		p.healthy = true
		p.healthMsg = "webhook-signature disabled"
		return nil
	}

	rules := make([]middleware.WebhookSignatureRule, 0, len(p.cfg.Paths))
	for i, r := range p.cfg.Paths {
		if r.Path == "" {
			return fmt.Errorf("webhook-signature rule[%d]: path is required", i)
		}
		provider := domainwebhook.Provider(r.Provider)
		if err := validateProvider(provider); err != nil {
			return fmt.Errorf("webhook-signature rule[%d]: %w", i, err)
		}
		if r.SecretEnvVar == "" {
			return fmt.Errorf("webhook-signature rule[%d]: secret_env_var is required", i)
		}
		secret := os.Getenv(r.SecretEnvVar)
		if secret == "" {
			p.logger.Warn("webhook-signature: secret env var is empty — requests may be rejected",
				slog.String("env_var", r.SecretEnvVar),
				slog.String("path", r.Path),
			)
		}
		rules = append(rules, middleware.WebhookSignatureRule{
			Path: r.Path,
			Config: domainwebhook.VerifyConfig{
				Provider: provider,
				Secret:   secret,
				Header:   r.Header,
			},
		})
	}

	p.rules = rules
	p.healthy = true
	p.healthMsg = fmt.Sprintf("active (%d rules)", len(rules))
	p.logger.Info("webhook-signature plugin initialised",
		slog.Int("rules", len(rules)),
	)
	return nil
}

// Start is a no-op. Signature verification is performed at request time by
// the Caddy handler contributed via ContributeCaddyHandlers.
func (p *Plugin) Start(_ context.Context) error { return nil }

// Stop is a no-op. The plugin has no background goroutines.
func (p *Plugin) Stop(_ context.Context) error {
	p.healthy = false
	p.healthMsg = "stopped"
	return nil
}

// Health returns the current health status of the webhook signature plugin.
func (p *Plugin) Health() ports.HealthStatus {
	return ports.HealthStatus{
		Healthy: p.healthy,
		Message: p.healthMsg,
	}
}

// ContributeCaddyRoutes returns nil.
// The webhook signature plugin does not add named routes; it contributes a
// catch-all middleware handler via ContributeCaddyHandlers.
func (p *Plugin) ContributeCaddyRoutes() []ports.CaddyRoute { return nil }

// ContributeCaddyHandlers returns the Caddy webhook signature handler when
// the plugin is enabled and at least one rule is configured.
func (p *Plugin) ContributeCaddyHandlers() []ports.CaddyHandler {
	if !p.cfg.Enabled || len(p.rules) == 0 {
		return nil
	}

	ruleCfgs := make([]caddyadapter.WebhookSignatureHandlerRuleConfig, 0, len(p.cfg.Paths))
	for _, r := range p.cfg.Paths {
		ruleCfgs = append(ruleCfgs, caddyadapter.WebhookSignatureHandlerRuleConfig{
			Path:         r.Path,
			Provider:     r.Provider,
			SecretEnvVar: r.SecretEnvVar,
			Header:       r.Header,
		})
	}

	handlerJSON, err := caddyadapter.BuildWebhookSignatureHandlerJSON(
		caddyadapter.WebhookSignatureHandlerConfig{Rules: ruleCfgs},
	)
	if err != nil {
		p.logger.Error("webhook-signature: failed to build Caddy handler JSON",
			slog.String("error", err.Error()),
		)
		return nil
	}

	return []ports.CaddyHandler{
		{Handler: handlerJSON, Priority: 35},
	}
}

// validateProvider returns an error when the provider string is not one of
// the known webhook provider identifiers.
func validateProvider(p domainwebhook.Provider) error {
	switch p {
	case domainwebhook.ProviderStripe,
		domainwebhook.ProviderGitHub,
		domainwebhook.ProviderSlack,
		domainwebhook.ProviderTwilio,
		domainwebhook.ProviderGeneric:
		return nil
	default:
		return fmt.Errorf(
			"unknown provider %q; valid values: stripe, github, slack, twilio, generic", p,
		)
	}
}

// Interface guards.
var (
	_ ports.Plugin           = (*Plugin)(nil)
	_ ports.CaddyContributor = (*Plugin)(nil)
)
