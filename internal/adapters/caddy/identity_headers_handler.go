package caddy

import (
	"encoding/json"
	"net/http"

	gocaddy "github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
)

func init() {
	gocaddy.RegisterModule(IdentityHeadersHandler{})
}

// IdentityHeadersHandlerConfig is the JSON-serialisable configuration for the
// IdentityHeadersHandler Caddy module.
type IdentityHeadersHandlerConfig struct {
	// CookieName is the name of the Kratos session cookie.
	CookieName string `json:"cookie_name"`

	// KratosURL is the base URL of the Kratos public API (e.g. "http://kratos:4433").
	KratosURL string `json:"kratos_url"`
}

// IdentityHeadersHandler is a Caddy HTTP handler module that sets upstream
// identity headers (X-User-Id, X-User-Email, X-User-Verified) from a validated
// Kratos session.
//
// In the current implementation the upstream AuthHandler (http.handlers.vibewarden_authentication)
// already sets these headers after validating the session. This handler exists
// as a no-op pass-through so that the auth plugin can emit both handlers without
// Caddy rejecting the configuration for an unknown module. If the authentication
// handler is ever refactored to not set identity headers, this handler can be
// updated to call Kratos whoami and inject the headers itself.
//
// The module is registered under the name "vibewarden_identity_headers" and referenced from
// the Caddy JSON configuration as:
//
//	{"handler": "vibewarden_identity_headers", "cookie_name": "...", "kratos_url": "..."}
type IdentityHeadersHandler struct {
	Config IdentityHeadersHandlerConfig `json:"config"`
}

// CaddyModule returns the Caddy module information.
func (IdentityHeadersHandler) CaddyModule() gocaddy.ModuleInfo {
	return gocaddy.ModuleInfo{
		ID:  "http.handlers.vibewarden_identity_headers",
		New: func() gocaddy.Module { return new(IdentityHeadersHandler) },
	}
}

// Provision implements gocaddy.Provisioner.
func (h *IdentityHeadersHandler) Provision(_ gocaddy.Context) error {
	return nil
}

// UnmarshalJSON implements custom unmarshalling to support both nested
// {"config": {...}} and flat {"cookie_name": "...", ...} JSON layouts.
func (h *IdentityHeadersHandler) UnmarshalJSON(data []byte) error {
	// Try nested config first.
	var nested struct {
		Config IdentityHeadersHandlerConfig `json:"config"`
	}
	if err := json.Unmarshal(data, &nested); err == nil && nested.Config.CookieName != "" {
		h.Config = nested.Config
		return nil
	}
	// Try flat structure.
	return json.Unmarshal(data, &h.Config)
}

// ServeHTTP implements caddyhttp.MiddlewareHandler.
// Identity headers are already set by the upstream AuthHandler. This handler
// is a pass-through that delegates to the next handler in the chain.
func (h *IdentityHeadersHandler) ServeHTTP(w http.ResponseWriter, r *http.Request, next caddyhttp.Handler) error {
	return next.ServeHTTP(w, r)
}

// Interface guards.
var (
	_ caddyhttp.MiddlewareHandler = (*IdentityHeadersHandler)(nil)
	_ gocaddy.Module              = (*IdentityHeadersHandler)(nil)
	_ gocaddy.Provisioner         = (*IdentityHeadersHandler)(nil)
)
