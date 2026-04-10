// Package caddy implements the ProxyServer port using embedded Caddy.
package caddy

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"

	gocaddy "github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"

	auditadapter "github.com/vibewarden/vibewarden/internal/adapters/audit"
	"github.com/vibewarden/vibewarden/internal/middleware"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// init registers AdminAuthHandler with the Caddy module system so it can be
// referenced by name ("http.handlers.vibewarden_admin_auth") in Caddy's JSON
// configuration. This function is called automatically by the Go runtime before
// main() runs.
func init() {
	gocaddy.RegisterModule(AdminAuthHandler{})
}

// AdminAuthHandlerConfig holds the JSON-serialisable configuration for the
// AdminAuthHandler Caddy module. It mirrors ports.AdminAuthConfig but uses
// JSON struct tags so it can be embedded in a Caddy JSON config.
type AdminAuthHandlerConfig struct {
	// Enabled toggles the admin API.
	Enabled bool `json:"enabled"`

	// Token is the bearer token clients must supply in X-Admin-Key.
	Token string `json:"token"`

	// ConfigPath is an additional path prefix to protect.
	// When set, requests starting with this prefix are also authenticated.
	ConfigPath string `json:"config_path,omitempty"`
}

// AdminAuthHandler is a Caddy HTTP handler module that enforces bearer-token
// authentication on /_vibewarden/admin/* endpoints.
//
// The module is registered under the name "vibewarden_admin_auth" and
// referenced from the Caddy JSON configuration as:
//
//	{"handler": "vibewarden_admin_auth", ...}
type AdminAuthHandler struct {
	// Config holds the admin auth configuration (populated by Caddy's JSON
	// unmarshaller via the Provision lifecycle).
	Config AdminAuthHandlerConfig `json:"config"`

	// handler is the compiled Go middleware, created during Provision.
	handler func(http.Handler) http.Handler
}

// CaddyModule returns the module metadata used to register it with Caddy.
func (AdminAuthHandler) CaddyModule() gocaddy.ModuleInfo {
	return gocaddy.ModuleInfo{
		ID:  "http.handlers.vibewarden_admin_auth",
		New: func() gocaddy.Module { return new(AdminAuthHandler) },
	}
}

// Provision implements gocaddy.Provisioner.
// It is called once after the module is loaded from JSON and creates the
// compiled middleware handler from the configuration.
func (h *AdminAuthHandler) Provision(_ gocaddy.Context) error {
	cfg := ports.AdminAuthConfig{
		Enabled:    h.Config.Enabled,
		Token:      h.Config.Token,
		ConfigPath: h.Config.ConfigPath,
	}
	auditLogger := auditadapter.NewJSONWriter(os.Stdout)
	h.handler = middleware.AdminAuthMiddleware(cfg, auditLogger)
	return nil
}

// ServeHTTP implements caddyhttp.MiddlewareHandler.
// It delegates to the compiled Go middleware handler.
func (h *AdminAuthHandler) ServeHTTP(w http.ResponseWriter, r *http.Request, next caddyhttp.Handler) error {
	// Adapt the caddyhttp.Handler to a stdlib http.Handler for the Go middleware.
	stdNext := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Ignore the error from the Caddy next handler — it propagates through
		// Caddy's own error handling chain. Errors cannot be surfaced here
		// because the stdlib http.Handler interface does not return an error.
		//nolint:errcheck
		_ = next.ServeHTTP(w, r)
	})

	h.handler(stdNext).ServeHTTP(w, r)
	return nil
}

// buildAdminAuthHandlerJSON serialises an AdminAuthHandlerConfig to the Caddy
// handler JSON fragment used in BuildCaddyConfig.
func buildAdminAuthHandlerJSON(cfg ports.AdminAuthConfig) (map[string]any, error) {
	handlerCfg := AdminAuthHandlerConfig{
		Enabled: cfg.Enabled,
		Token:   cfg.Token,
	}

	cfgBytes, err := json.Marshal(handlerCfg)
	if err != nil {
		return nil, fmt.Errorf("marshaling admin auth handler config: %w", err)
	}

	var cfgRaw json.RawMessage = cfgBytes

	return map[string]any{
		"handler": "vibewarden_admin_auth",
		"config":  cfgRaw,
	}, nil
}

// Interface guards — ensure AdminAuthHandler satisfies the required Caddy
// and VibeWarden interfaces at compile time.
var (
	_ gocaddy.Provisioner         = (*AdminAuthHandler)(nil)
	_ caddyhttp.MiddlewareHandler = (*AdminAuthHandler)(nil)
)
