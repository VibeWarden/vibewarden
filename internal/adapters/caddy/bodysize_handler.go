// Package caddy implements the ProxyServer port using embedded Caddy.
package caddy

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	gocaddy "github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"

	"github.com/vibewarden/vibewarden/internal/ports"
)

func init() {
	gocaddy.RegisterModule(BodySizeHandler{})
}

// BodySizeHandlerConfig is the JSON-serialisable configuration for the
// BodySizeHandler Caddy module. It is embedded in the Caddy JSON config
// under the "config" key of the "vibewarden_body_size" handler entry.
type BodySizeHandlerConfig struct {
	// MaxBytes is the global default maximum request body size in bytes.
	// A value of 0 or less disables the global limit.
	MaxBytes int64 `json:"max_bytes"`

	// Overrides defines per-path limits that take precedence over MaxBytes.
	Overrides []BodySizeOverrideHandlerConfig `json:"overrides,omitempty"`
}

// BodySizeOverrideHandlerConfig is the JSON-serialisable form of a single
// per-path body size override.
type BodySizeOverrideHandlerConfig struct {
	// Path is the URL path prefix to match (e.g. "/api/upload").
	Path string `json:"path"`

	// MaxBytes is the maximum body size for this path in bytes.
	// A value of 0 means no limit for this path.
	MaxBytes int64 `json:"max_bytes"`
}

// BodySizeHandler is a Caddy HTTP middleware module that enforces request body
// size limits with optional per-path overrides.
//
// It wraps net/http.MaxBytesReader to constrain how many bytes the downstream
// handler may read from the request body. When the limit is exceeded,
// net/http.MaxBytesReader causes the next handler to receive an error with
// HTTP status 413 Payload Too Large.
//
// Per-path overrides are evaluated by longest-prefix match before the global
// default is applied. Overrides with MaxBytes == 0 suppress the global limit
// for that path (no limit).
//
// The module is registered under the name "vibewarden_body_size" and referenced
// from the Caddy JSON configuration as:
//
//	{"handler": "vibewarden_body_size", ...}
type BodySizeHandler struct {
	// Config holds the body size configuration, populated by Caddy's JSON
	// unmarshaller during the Provision lifecycle.
	Config BodySizeHandlerConfig `json:"config"`
}

// CaddyModule returns the module metadata used to register it with Caddy.
func (BodySizeHandler) CaddyModule() gocaddy.ModuleInfo {
	return gocaddy.ModuleInfo{
		ID:  "http.handlers.vibewarden_body_size",
		New: func() gocaddy.Module { return new(BodySizeHandler) },
	}
}

// Provision implements gocaddy.Provisioner. It is a no-op for this handler
// because all configuration is applied at request time.
func (h *BodySizeHandler) Provision(_ gocaddy.Context) error {
	return nil
}

// ServeHTTP implements caddyhttp.MiddlewareHandler. It determines the
// applicable body size limit for the request path, wraps the request body
// with http.MaxBytesReader if a limit applies, and delegates to next.
func (h *BodySizeHandler) ServeHTTP(w http.ResponseWriter, r *http.Request, next caddyhttp.Handler) error {
	limit := h.resolveLimit(r.URL.Path)
	if limit > 0 && r.Body != nil {
		r.Body = http.MaxBytesReader(w, r.Body, limit)
	}
	return next.ServeHTTP(w, r)
}

// resolveLimit returns the effective body size limit in bytes for the given
// request path. Per-path overrides are evaluated by longest prefix match.
// A return value of 0 means no limit should be applied.
func (h *BodySizeHandler) resolveLimit(path string) int64 {
	// Find the best (longest) matching override.
	var bestLen int
	var bestOverride *BodySizeOverrideHandlerConfig

	for i := range h.Config.Overrides {
		ov := &h.Config.Overrides[i]
		if strings.HasPrefix(path, ov.Path) && len(ov.Path) > bestLen {
			bestLen = len(ov.Path)
			bestOverride = ov
		}
	}

	if bestOverride != nil {
		// An override matched. Its MaxBytes value takes precedence —
		// 0 means "no limit for this path", not "use the global default".
		return bestOverride.MaxBytes
	}

	return h.Config.MaxBytes
}

// buildBodySizeHandlerJSON serialises a BodySizeHandlerConfig to the Caddy
// handler JSON fragment used in BuildCaddyConfig.
func buildBodySizeHandlerJSON(cfg ports.BodySizeConfig) (map[string]any, error) {
	handlerCfg := BodySizeHandlerConfig{
		MaxBytes: cfg.MaxBytes,
	}

	for _, ov := range cfg.Overrides {
		handlerCfg.Overrides = append(handlerCfg.Overrides, BodySizeOverrideHandlerConfig{
			Path:     ov.Path,
			MaxBytes: ov.MaxBytes,
		})
	}

	cfgBytes, err := json.Marshal(handlerCfg)
	if err != nil {
		return nil, fmt.Errorf("marshaling body size handler config: %w", err)
	}

	return map[string]any{
		"handler": "vibewarden_body_size",
		"config":  json.RawMessage(cfgBytes),
	}, nil
}

// Interface guards — ensure BodySizeHandler satisfies required Caddy interfaces
// at compile time.
var (
	_ gocaddy.Provisioner         = (*BodySizeHandler)(nil)
	_ caddyhttp.MiddlewareHandler = (*BodySizeHandler)(nil)
)
