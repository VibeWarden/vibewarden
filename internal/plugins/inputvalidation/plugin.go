package inputvalidation

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"path"

	"github.com/vibewarden/vibewarden/internal/middleware"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// Plugin is the input validation plugin for VibeWarden.
// It implements ports.Plugin and ports.CaddyContributor.
//
// The plugin enforces request input size limits (URL length, query string
// length, header count, per-header value size) at priority 18 — before the
// WAF (priority 25) — so that oversized inputs are rejected early, before
// any regex scanning begins.
//
// Start and Stop are no-ops; the plugin is fully stateless. Health reports
// whether the plugin is enabled.
type Plugin struct {
	cfg    Config
	logger *slog.Logger
}

// New creates a new input validation Plugin.
func New(cfg Config, logger *slog.Logger) *Plugin {
	return &Plugin{cfg: cfg, logger: logger}
}

// Name returns the canonical plugin identifier "input-validation".
func (p *Plugin) Name() string { return "input-validation" }

// Priority returns 18 — input validation runs after security headers (20) is
// incorrect; it must run before the WAF (25). We use 18 so it slots between
// CORS/IP-filter (10-15) and the WAF (25), with security headers at 20 also
// running first since headers are added to the response, not inspecting the
// request.
func (p *Plugin) Priority() int { return 18 }

// Init validates the plugin configuration. It verifies that all path override
// patterns are syntactically valid path.Match expressions.
func (p *Plugin) Init(_ context.Context) error {
	if !p.cfg.Enabled {
		return nil
	}

	for i, ov := range p.cfg.PathOverrides {
		if ov.Path == "" {
			return fmt.Errorf("input_validation: path_overrides[%d].path must not be empty", i)
		}
		if _, err := path.Match(ov.Path, ""); err != nil {
			return fmt.Errorf("input_validation: path_overrides[%d].path %q is not a valid path.Match pattern: %w",
				i, ov.Path, err)
		}
	}

	p.logger.Info("input validation plugin initialised",
		slog.Int("max_url_length", p.cfg.MaxURLLength),
		slog.Int("max_query_string_length", p.cfg.MaxQueryStringLength),
		slog.Int("max_header_count", p.cfg.MaxHeaderCount),
		slog.Int("max_header_size", p.cfg.MaxHeaderSize),
		slog.Int("path_overrides", len(p.cfg.PathOverrides)),
	)
	return nil
}

// Start is a no-op for the input validation plugin.
func (p *Plugin) Start(_ context.Context) error { return nil }

// Stop is a no-op for the input validation plugin.
func (p *Plugin) Stop(_ context.Context) error { return nil }

// Health returns the current health status of the input validation plugin.
func (p *Plugin) Health() ports.HealthStatus {
	if !p.cfg.Enabled {
		return ports.HealthStatus{
			Healthy: true,
			Message: "input-validation disabled",
		}
	}
	return ports.HealthStatus{
		Healthy: true,
		Message: fmt.Sprintf(
			"input-validation active (max_url=%d, max_qs=%d, max_headers=%d, max_header_size=%d, overrides=%d)",
			p.cfg.MaxURLLength,
			p.cfg.MaxQueryStringLength,
			p.cfg.MaxHeaderCount,
			p.cfg.MaxHeaderSize,
			len(p.cfg.PathOverrides),
		),
	}
}

// ContributeCaddyRoutes returns nil. The input validation plugin does not add
// named routes.
func (p *Plugin) ContributeCaddyRoutes() []ports.CaddyRoute { return nil }

// ContributeCaddyHandlers returns a single Caddy handler fragment for the
// input validation middleware at priority 18. Returns an empty slice when the
// plugin is disabled.
func (p *Plugin) ContributeCaddyHandlers() []ports.CaddyHandler {
	if !p.cfg.Enabled {
		return nil
	}

	handlerJSON, err := buildHandlerJSON(p.cfg)
	if err != nil {
		p.logger.Error("input validation plugin: building handler JSON",
			slog.String("err", err.Error()))
		return nil
	}

	return []ports.CaddyHandler{
		{
			Handler:  handlerJSON,
			Priority: 18,
		},
	}
}

// ---------------------------------------------------------------------------
// Internal builders — pure functions, no side effects.
// ---------------------------------------------------------------------------

// handlerConfig is the JSON-serialisable configuration sent to the
// vibewarden_input_validation Caddy module.
type handlerConfig struct {
	// MaxURLLength is the maximum allowed raw URI length.
	MaxURLLength int `json:"max_url_length"`

	// MaxQueryStringLength is the maximum allowed query string length.
	MaxQueryStringLength int `json:"max_query_string_length"`

	// MaxHeaderCount is the maximum number of request headers.
	MaxHeaderCount int `json:"max_header_count"`

	// MaxHeaderSize is the maximum per-header value byte size.
	MaxHeaderSize int `json:"max_header_size"`

	// PathOverrides defines per-path limit overrides.
	PathOverrides []pathOverrideHandlerConfig `json:"path_overrides,omitempty"`
}

// pathOverrideHandlerConfig is the JSON representation of a single per-path
// override sent to the Caddy module.
type pathOverrideHandlerConfig struct {
	Path                 string `json:"path"`
	MaxURLLength         int    `json:"max_url_length,omitempty"`
	MaxQueryStringLength int    `json:"max_query_string_length,omitempty"`
	MaxHeaderCount       int    `json:"max_header_count,omitempty"`
	MaxHeaderSize        int    `json:"max_header_size,omitempty"`
}

// buildHandlerJSON serialises cfg into the Caddy handler JSON map expected by
// the vibewarden_input_validation module.
func buildHandlerJSON(cfg Config) (map[string]any, error) {
	hcfg := handlerConfig{
		MaxURLLength:         cfg.MaxURLLength,
		MaxQueryStringLength: cfg.MaxQueryStringLength,
		MaxHeaderCount:       cfg.MaxHeaderCount,
		MaxHeaderSize:        cfg.MaxHeaderSize,
	}

	for _, ov := range cfg.PathOverrides {
		hcfg.PathOverrides = append(hcfg.PathOverrides, pathOverrideHandlerConfig(ov))
	}

	cfgBytes, err := json.Marshal(hcfg)
	if err != nil {
		return nil, fmt.Errorf("marshaling input validation handler config: %w", err)
	}

	return map[string]any{
		"handler": "vibewarden_input_validation",
		"config":  json.RawMessage(cfgBytes),
	}, nil
}

// ---------------------------------------------------------------------------
// Middleware accessor — used by the Caddy adapter to build the handler chain.
// ---------------------------------------------------------------------------

// Middleware returns the compiled net/http middleware function for the plugin.
// It is used by adapters that construct the middleware chain directly (e.g.
// the Caddy adapter) rather than via Caddy JSON config contribution.
func (p *Plugin) Middleware() func(http.Handler) http.Handler {
	ovs := make([]middleware.InputValidationPathOverride, 0, len(p.cfg.PathOverrides))
	for _, ov := range p.cfg.PathOverrides {
		ovs = append(ovs, middleware.InputValidationPathOverride{
			Path:                 ov.Path,
			MaxURLLength:         ov.MaxURLLength,
			MaxQueryStringLength: ov.MaxQueryStringLength,
			MaxHeaderCount:       ov.MaxHeaderCount,
			MaxHeaderSize:        ov.MaxHeaderSize,
		})
	}

	return middleware.InputValidation(middleware.InputValidationConfig{
		Enabled:              p.cfg.Enabled,
		MaxURLLength:         p.cfg.MaxURLLength,
		MaxQueryStringLength: p.cfg.MaxQueryStringLength,
		MaxHeaderCount:       p.cfg.MaxHeaderCount,
		MaxHeaderSize:        p.cfg.MaxHeaderSize,
		PathOverrides:        ovs,
	})
}

// ---------------------------------------------------------------------------
// Interface guards.
// ---------------------------------------------------------------------------

var (
	_ ports.Plugin           = (*Plugin)(nil)
	_ ports.CaddyContributor = (*Plugin)(nil)
)
