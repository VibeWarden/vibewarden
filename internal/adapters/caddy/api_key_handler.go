package caddy

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"

	gocaddy "github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"

	"github.com/vibewarden/vibewarden/internal/domain/auth"
	"github.com/vibewarden/vibewarden/internal/middleware"
	"github.com/vibewarden/vibewarden/internal/ports"
)

func init() {
	gocaddy.RegisterModule(APIKeyHandler{})
}

// APIKeyHandlerConfig is the JSON-serialisable configuration for APIKeyHandler.
// It mirrors ports.APIKeyConfig but uses JSON struct tags so Caddy can
// unmarshal it from the handler JSON fragment contributed by the auth plugin.
type APIKeyHandlerConfig struct {
	// Header is the request header from which the API key is extracted.
	// Defaults to "X-API-Key" when empty.
	Header string `json:"header,omitempty"`

	// ScopeRules is the ordered list of path+method authorization rules.
	// Each rule is serialised from auth.APIKeyScopeRule via buildAPIKeyHandler.
	ScopeRules []APIKeyScopeRuleConfig `json:"scope_rules,omitempty"`
}

// APIKeyScopeRuleConfig is the JSON-serialisable form of a single scope rule.
type APIKeyScopeRuleConfig struct {
	// Path is a glob pattern matched against the request URL path.
	Path string `json:"path"`

	// Methods restricts which HTTP methods the rule applies to. Empty means all.
	Methods []string `json:"methods,omitempty"`

	// RequiredScopes is the set of scope strings the key must hold.
	RequiredScopes []string `json:"required_scopes"`
}

// APIKeyHandler is a Caddy HTTP handler module that enforces API key
// authentication on proxied requests.
//
// It is registered under the Caddy module ID "http.handlers.api_key_auth" and
// sits at priority 35 in the catch-all handler chain — between rate-limit (30)
// and the Kratos/JWT auth handlers (40+).
//
// The handler delegates all enforcement logic to middleware.APIKeyMiddleware,
// which reads the configured header, validates via the injected
// ports.APIKeyValidator, and applies scope rules. RuntimeServices.APIKeyValidator
// is the injection point; when nil at Provision time the handler fails closed
// (HTTP 500 with body {"error":"auth misconfigured"}) on every request.
type APIKeyHandler struct {
	// Config holds the JSON-unmarshalled configuration.
	Config APIKeyHandlerConfig `json:"config,omitempty"`

	// handler is the compiled Go middleware, created during Provision.
	handler func(http.Handler) http.Handler

	// logger is used for misconfiguration errors.
	logger *slog.Logger

	// nilValidator is true when the validator was absent during Provision. The
	// handler returns 500 for every request in this state (fail-closed).
	nilValidator bool
}

// CaddyModule returns the module metadata used to register APIKeyHandler with
// Caddy. The module ID is "http.handlers.api_key_auth".
func (APIKeyHandler) CaddyModule() gocaddy.ModuleInfo {
	return gocaddy.ModuleInfo{
		ID:  "http.handlers.api_key_auth",
		New: func() gocaddy.Module { return new(APIKeyHandler) },
	}
}

// Provision implements gocaddy.Provisioner. It reads services from the
// composition-root registry and forwards to ProvisionWith. Production code path.
func (h *APIKeyHandler) Provision(_ gocaddy.Context) error {
	return h.ProvisionWith(currentServices())
}

// ProvisionWith initialises the handler with explicit services. Tests call this
// directly with controlled services; production calls it via Provision.
//
// When services.APIKeyValidator is nil the handler is put into fail-closed mode:
// every request receives HTTP 500. This is intentional — a misconfigured api-key
// mode must not silently allow all traffic through.
func (h *APIKeyHandler) ProvisionWith(services RuntimeServices) error {
	h.logger = services.Logger
	if h.logger == nil {
		h.logger = slog.New(slog.NewJSONHandler(os.Stderr, nil))
	}

	if services.APIKeyValidator == nil {
		h.logger.Error("api_key_auth: APIKeyValidator is nil in RuntimeServices — all requests will be rejected with 500")
		h.nilValidator = true
		return nil
	}

	// Convert serialised scope rules to domain types.
	scopeRules := make([]auth.ScopeRule, len(h.Config.ScopeRules))
	for i, r := range h.Config.ScopeRules {
		scopeRules[i] = auth.ScopeRule{
			Path:           r.Path,
			Methods:        r.Methods,
			RequiredScopes: r.RequiredScopes,
		}
	}

	apiKeyCfg := ports.APIKeyConfig{
		Header:     h.Config.Header,
		ScopeRules: scopeRules,
	}

	h.handler = middleware.APIKeyMiddleware(
		services.APIKeyValidator,
		apiKeyCfg,
		services.EventLogger,
		services.AuditEventLogger,
		nil, // drop counter: nil is safe — middleware skips it when nil
	)

	return nil
}

// ServeHTTP implements caddyhttp.MiddlewareHandler.
//
// When the validator was absent at Provision time (nilValidator is true), every
// request is rejected with HTTP 500 and body {"error":"auth misconfigured"}.
// Otherwise, the request is forwarded to the compiled APIKeyMiddleware.
func (h *APIKeyHandler) ServeHTTP(w http.ResponseWriter, r *http.Request, next caddyhttp.Handler) error {
	if h.nilValidator {
		h.logger.Error("api_key_auth: rejecting request — APIKeyValidator was nil at provision time",
			slog.String("path", r.URL.Path),
			slog.String("method", r.Method),
		)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"auth misconfigured"}`)) //nolint:errcheck
		return nil
	}

	// Adapt the caddyhttp.Handler to a stdlib http.Handler for the Go middleware.
	stdNext := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		//nolint:errcheck // error propagates through Caddy's own error handling chain
		_ = next.ServeHTTP(w, r)
	})

	h.handler(stdNext).ServeHTTP(w, r)
	return nil
}

// UnmarshalJSON implements custom unmarshalling so the handler can be
// instantiated from both a flat and a nested Caddy JSON configuration.
func (h *APIKeyHandler) UnmarshalJSON(data []byte) error {
	// Try nested config first (consistent with how RateLimitHandler does it).
	var nested struct {
		Config APIKeyHandlerConfig `json:"config"`
	}
	if err := json.Unmarshal(data, &nested); err == nil && (nested.Config.Header != "" || len(nested.Config.ScopeRules) > 0) {
		h.Config = nested.Config
		return nil
	}
	// Fall back to flat structure matching the map[string]any produced by
	// buildAPIKeyHandler in internal/plugins/auth/plugin.go.
	var flat struct {
		Header     string                  `json:"header"`
		ScopeRules []APIKeyScopeRuleConfig `json:"scope_rules"`
	}
	if err := json.Unmarshal(data, &flat); err != nil {
		return fmt.Errorf("api_key_auth handler: unmarshalling config: %w", err)
	}
	h.Config.Header = flat.Header
	h.Config.ScopeRules = flat.ScopeRules
	return nil
}

// Interface guards — ensure APIKeyHandler satisfies the required Caddy
// and VibeWarden interfaces at compile time.
var (
	_ gocaddy.Provisioner         = (*APIKeyHandler)(nil)
	_ caddyhttp.MiddlewareHandler = (*APIKeyHandler)(nil)
	_ gocaddy.Module              = (*APIKeyHandler)(nil)
)
