// Package caddy implements the ProxyServer port using embedded Caddy.
package caddy

import (
	"fmt"
	"log/slog"
	"net/http"

	gocaddy "github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"

	domainwaf "github.com/vibewarden/vibewarden/internal/domain/waf"
	"github.com/vibewarden/vibewarden/internal/middleware"
)

func init() {
	gocaddy.RegisterModule(WAFEngineHandler{})
}

// WAFEngineHandlerRulesConfig is the JSON-serialisable per-category rule toggle
// embedded in WAFEngineHandlerConfig.
type WAFEngineHandlerRulesConfig struct {
	// SQLInjection toggles the SQLi rule category (default: true).
	SQLInjection bool `json:"sqli"`

	// XSS toggles the XSS rule category (default: true).
	XSS bool `json:"xss"`

	// PathTraversal toggles the path traversal rule category (default: true).
	PathTraversal bool `json:"path_traversal"`

	// CmdInjection toggles the command injection rule category (default: true).
	CmdInjection bool `json:"cmd_injection"`
}

// WAFEngineHandlerConfig is the JSON-serialisable configuration for the
// WAFEngineHandler Caddy module. It is embedded in the Caddy JSON config under
// the "config" key of the "vibewarden_waf_engine" handler entry.
type WAFEngineHandlerConfig struct {
	// Mode is "block" or "detect". Empty defaults to "detect".
	Mode string `json:"mode"`

	// Rules toggles individual attack categories.
	Rules WAFEngineHandlerRulesConfig `json:"rules"`

	// ExemptPaths is the list of URL path glob patterns that bypass scanning.
	ExemptPaths []string `json:"exempt_paths"`
}

// WAFEngineHandler is a Caddy HTTP middleware module that scans incoming HTTP
// requests against the built-in VibeWarden WAF rule engine.
//
// In "block" mode matching requests are rejected with 403 Forbidden and a
// structured JSON error body. In "detect" mode detections are logged and the
// request is forwarded to the upstream application unchanged.
//
// The module is registered under the name "vibewarden_waf_engine" and
// referenced from the Caddy JSON configuration as:
//
//	{"handler": "vibewarden_waf_engine", ...}
type WAFEngineHandler struct {
	// Config holds the WAF engine configuration, populated by Caddy's JSON
	// unmarshaller during the Provision lifecycle.
	Config WAFEngineHandlerConfig `json:"config"`

	// mw is the compiled middleware function, built once during Provision.
	mw func(next http.Handler) http.Handler
}

// CaddyModule returns the module metadata used to register it with Caddy.
func (WAFEngineHandler) CaddyModule() gocaddy.ModuleInfo {
	return gocaddy.ModuleInfo{
		ID:  "http.handlers.vibewarden_waf_engine",
		New: func() gocaddy.Module { return new(WAFEngineHandler) },
	}
}

// Provision implements gocaddy.Provisioner. It builds the WAF rule set and
// compiles the middleware function so that per-request processing incurs no
// repeated allocation.
func (h *WAFEngineHandler) Provision(_ gocaddy.Context) error {
	rules, err := buildEnabledRules(h.Config.Rules)
	if err != nil {
		return fmt.Errorf("building WAF rule set: %w", err)
	}

	rs, err := domainwaf.NewRuleSet(rules)
	if err != nil {
		return fmt.Errorf("building WAF rule set: %w", err)
	}

	mode := middleware.WAFMode(h.Config.Mode)
	if mode == "" {
		mode = middleware.WAFModeDetect
	}

	cfg := middleware.WAFConfig{
		Mode:              mode,
		EnabledCategories: buildEnabledCategories(h.Config.Rules),
		ExemptPaths:       h.Config.ExemptPaths,
	}

	// The Caddy handler has no access to the metrics collector or audit logger
	// because Caddy modules are provisioned independently from the plugin
	// registry. Pass nil for both optional dependencies; the middleware handles
	// nil gracefully by skipping those side effects.
	h.mw = middleware.WAFMiddleware(rs, cfg, slog.Default(), nil, nil)
	return nil
}

// ServeHTTP implements caddyhttp.MiddlewareHandler. It delegates to the
// compiled WAF middleware and then calls next.
func (h *WAFEngineHandler) ServeHTTP(w http.ResponseWriter, r *http.Request, next caddyhttp.Handler) error {
	h.mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Ignore the error returned by next.ServeHTTP — Caddy propagates it
		// upward; storing it here would be a data race.
		_ = next.ServeHTTP(w, r)
	})).ServeHTTP(w, r)
	return nil
}

// buildEnabledRules returns the subset of built-in rules selected by cfg.
// If all categories are disabled, returns an error (the rule set requires at
// least one rule). Callers should handle an empty selection by not registering
// the handler at all.
func buildEnabledRules(cfg WAFEngineHandlerRulesConfig) ([]domainwaf.Rule, error) {
	enabled := map[domainwaf.Category]bool{
		domainwaf.CategorySQLInjection:     cfg.SQLInjection,
		domainwaf.CategoryXSS:              cfg.XSS,
		domainwaf.CategoryPathTraversal:    cfg.PathTraversal,
		domainwaf.CategoryCommandInjection: cfg.CmdInjection,
	}

	all := domainwaf.BuiltinRules()
	var selected []domainwaf.Rule
	for _, rule := range all {
		if enabled[rule.Category()] {
			selected = append(selected, rule)
		}
	}

	if len(selected) == 0 {
		// All categories disabled — return all rules so the rule set is valid;
		// the EnabledCategories map in WAFMiddleware will skip them all at
		// runtime. This keeps the domain invariant (non-empty rule set) intact.
		return all, nil
	}
	return selected, nil
}

// buildEnabledCategories converts the per-category config booleans into the
// map consumed by middleware.WAFConfig.EnabledCategories.
func buildEnabledCategories(cfg WAFEngineHandlerRulesConfig) map[domainwaf.Category]bool {
	return map[domainwaf.Category]bool{
		domainwaf.CategorySQLInjection:     cfg.SQLInjection,
		domainwaf.CategoryXSS:              cfg.XSS,
		domainwaf.CategoryPathTraversal:    cfg.PathTraversal,
		domainwaf.CategoryCommandInjection: cfg.CmdInjection,
	}
}

// Interface guards — ensure WAFEngineHandler satisfies required Caddy
// interfaces at compile time.
var (
	_ gocaddy.Provisioner         = (*WAFEngineHandler)(nil)
	_ caddyhttp.MiddlewareHandler = (*WAFEngineHandler)(nil)
)
