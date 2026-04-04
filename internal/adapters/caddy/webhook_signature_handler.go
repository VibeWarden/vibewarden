// Package caddy implements the ProxyServer port using embedded Caddy.
package caddy

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"

	gocaddy "github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"

	logadapter "github.com/vibewarden/vibewarden/internal/adapters/log"
	domainwebhook "github.com/vibewarden/vibewarden/internal/domain/webhook"
	"github.com/vibewarden/vibewarden/internal/middleware"
)

// init registers WebhookSignatureHandler with the Caddy module system so it
// can be referenced by name ("http.handlers.vibewarden_webhook_signature") in
// Caddy's JSON configuration.
func init() {
	gocaddy.RegisterModule(WebhookSignatureHandler{})
}

// WebhookSignatureHandlerRuleConfig is the JSON-serialisable configuration for
// a single webhook path rule.
type WebhookSignatureHandlerRuleConfig struct {
	// Path is the URL path this rule applies to (exact match).
	Path string `json:"path"`

	// Provider selects the signature format: "stripe", "github", "slack",
	// "twilio", or "generic".
	Provider string `json:"provider"`

	// SecretEnvVar is the name of the environment variable that holds the
	// shared HMAC secret. The value is read at Provision time, not at request
	// time, so secret rotation requires a config reload.
	SecretEnvVar string `json:"secret_env_var"`

	// Header is the custom HTTP header name used when Provider is "generic".
	// Ignored for all other providers.
	Header string `json:"header,omitempty"`
}

// WebhookSignatureHandlerConfig is the JSON-serialisable configuration for the
// WebhookSignatureHandler Caddy module.
type WebhookSignatureHandlerConfig struct {
	// Rules is the ordered list of webhook path verification rules.
	Rules []WebhookSignatureHandlerRuleConfig `json:"rules"`
}

// WebhookSignatureHandler is a Caddy HTTP handler module that verifies inbound
// webhook request signatures. It wraps the VibeWarden webhook signature
// middleware so that it participates in Caddy's handler chain.
//
// The module is registered under the name "vibewarden_webhook_signature" and
// referenced from the Caddy JSON configuration as:
//
//	{"handler": "vibewarden_webhook_signature", ...}
type WebhookSignatureHandler struct {
	// Config holds the handler configuration (populated by Caddy's JSON
	// unmarshaller via the Provision lifecycle).
	Config WebhookSignatureHandlerConfig `json:"config"`

	// handler is the compiled Go middleware, created during Provision.
	handler func(http.Handler) http.Handler
}

// CaddyModule returns the module metadata used to register it with Caddy.
func (WebhookSignatureHandler) CaddyModule() gocaddy.ModuleInfo {
	return gocaddy.ModuleInfo{
		ID:  "http.handlers.vibewarden_webhook_signature",
		New: func() gocaddy.Module { return new(WebhookSignatureHandler) },
	}
}

// Provision implements gocaddy.Provisioner.
// It resolves secret env var references and constructs the middleware handler.
func (h *WebhookSignatureHandler) Provision(_ gocaddy.Context) error {
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	eventLogger := logadapter.NewSlogEventLogger(os.Stdout)

	rules := make([]middleware.WebhookSignatureRule, 0, len(h.Config.Rules))
	for i, r := range h.Config.Rules {
		if r.Path == "" {
			return fmt.Errorf("webhook signature rule[%d]: path is required", i)
		}
		provider := domainwebhook.Provider(r.Provider)
		if err := validateProvider(provider); err != nil {
			return fmt.Errorf("webhook signature rule[%d]: %w", i, err)
		}

		secret := os.Getenv(r.SecretEnvVar)
		if secret == "" {
			logger.Warn("webhook signature: secret env var is empty",
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

	h.handler = middleware.WebhookSignatureMiddleware(rules, eventLogger)
	return nil
}

// ServeHTTP implements caddyhttp.MiddlewareHandler.
// It delegates to the compiled Go middleware handler.
func (h *WebhookSignatureHandler) ServeHTTP(w http.ResponseWriter, r *http.Request, next caddyhttp.Handler) error {
	var nextErr error
	stdNext := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextErr = next.ServeHTTP(w, r)
	})
	h.handler(stdNext).ServeHTTP(w, r)
	return nextErr
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

// BuildWebhookSignatureHandlerJSON serialises a WebhookSignatureHandlerConfig
// to the Caddy handler JSON fragment. It is used by the webhooksig plugin to
// build the handler definition contributed to the Caddy configuration.
func BuildWebhookSignatureHandlerJSON(cfg WebhookSignatureHandlerConfig) (map[string]any, error) {
	cfgBytes, err := json.Marshal(cfg)
	if err != nil {
		return nil, fmt.Errorf("marshaling webhook signature handler config: %w", err)
	}
	var cfgRaw json.RawMessage = cfgBytes
	return map[string]any{
		"handler": "vibewarden_webhook_signature",
		"config":  cfgRaw,
	}, nil
}

// Interface guards — ensure WebhookSignatureHandler satisfies the required
// Caddy and VibeWarden interfaces at compile time.
var (
	_ gocaddy.Provisioner         = (*WebhookSignatureHandler)(nil)
	_ caddyhttp.MiddlewareHandler = (*WebhookSignatureHandler)(nil)
)
