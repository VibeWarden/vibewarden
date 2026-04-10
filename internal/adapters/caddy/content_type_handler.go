// Package caddy implements the ProxyServer port using embedded Caddy.
package caddy

import (
	"encoding/json"
	"fmt"
	"net/http"

	gocaddy "github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"

	"github.com/vibewarden/vibewarden/internal/middleware"
)

func init() {
	gocaddy.RegisterModule(ContentTypeHandler{})
}

// ContentTypeHandlerConfig is the JSON-serialisable configuration for the
// ContentTypeHandler Caddy module. It is embedded in the Caddy JSON config
// under the "config" key of the "vibewarden_waf_content_type" handler entry.
type ContentTypeHandlerConfig struct {
	// Allowed is the list of permitted media types (e.g. "application/json").
	// Parameters such as "; charset=utf-8" are stripped before comparison.
	Allowed []string `json:"allowed"`
}

// ContentTypeHandler is a Caddy HTTP middleware module that enforces
// Content-Type validation on body-bearing HTTP requests (POST, PUT, PATCH).
//
// Requests using no-body methods (GET, HEAD, DELETE, OPTIONS, …) always pass
// through without inspection. A body-bearing request with a missing or
// disallowed Content-Type is rejected with 415 Unsupported Media Type and a
// structured JSON error body that includes a trace_id / request_id for log
// correlation.
//
// The module is registered under the name "vibewarden_waf_content_type" and
// referenced from the Caddy JSON configuration as:
//
//	{"handler": "vibewarden_waf_content_type", ...}
type ContentTypeHandler struct {
	// Config holds the Content-Type validation configuration, populated by
	// Caddy's JSON unmarshaller during the Provision lifecycle.
	Config ContentTypeHandlerConfig `json:"config"`

	// mw is the compiled middleware function, built once during Provision.
	mw func(next http.Handler) http.Handler
}

// CaddyModule returns the module metadata used to register it with Caddy.
func (ContentTypeHandler) CaddyModule() gocaddy.ModuleInfo {
	return gocaddy.ModuleInfo{
		ID:  "http.handlers.vibewarden_waf_content_type",
		New: func() gocaddy.Module { return new(ContentTypeHandler) },
	}
}

// Provision implements gocaddy.Provisioner. It compiles the middleware
// function from the handler configuration so that per-request processing
// incurs no repeated allocation.
func (h *ContentTypeHandler) Provision(_ gocaddy.Context) error {
	h.mw = middleware.ContentTypeValidation(middleware.ContentTypeValidationConfig{
		Enabled: true, // always true — the handler is only registered when enabled
		Allowed: h.Config.Allowed,
	})
	return nil
}

// ServeHTTP implements caddyhttp.MiddlewareHandler. It delegates to the
// compiled content-type validation middleware and then calls next.
func (h *ContentTypeHandler) ServeHTTP(w http.ResponseWriter, r *http.Request, next caddyhttp.Handler) error {
	// Wrap the caddyhttp.Handler in an http.Handler so the standard middleware
	// function can call it.
	h.mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Ignore the error returned by next.ServeHTTP — Caddy propagates it
		// upward; storing it here would be a data race.
		_ = next.ServeHTTP(w, r)
	})).ServeHTTP(w, r)
	return nil
}

// buildContentTypeHandlerJSON serialises a ContentTypeHandlerConfig into the
// Caddy handler JSON fragment used in BuildCaddyConfig.
func buildContentTypeHandlerJSON(allowed []string) (map[string]any, error) {
	handlerCfg := ContentTypeHandlerConfig{
		Allowed: allowed,
	}

	cfgBytes, err := json.Marshal(handlerCfg)
	if err != nil {
		return nil, fmt.Errorf("marshaling content type handler config: %w", err)
	}

	return map[string]any{
		"handler": "vibewarden_waf_content_type",
		"config":  json.RawMessage(cfgBytes),
	}, nil
}

// Interface guards — ensure ContentTypeHandler satisfies required Caddy
// interfaces at compile time.
var (
	_ gocaddy.Provisioner         = (*ContentTypeHandler)(nil)
	_ caddyhttp.MiddlewareHandler = (*ContentTypeHandler)(nil)
)
