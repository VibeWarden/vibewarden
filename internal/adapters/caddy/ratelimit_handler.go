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

	ratelimitadapter "github.com/vibewarden/vibewarden/internal/adapters/ratelimit"
	"github.com/vibewarden/vibewarden/internal/middleware"
	"github.com/vibewarden/vibewarden/internal/ports"
)

func init() {
	gocaddy.RegisterModule(RateLimitHandler{})
}

// RateLimitHandlerConfig holds the JSON-serialisable configuration for the
// RateLimitHandler Caddy module. It mirrors ports.RateLimitConfig but uses
// JSON struct tags so it can be embedded in a Caddy JSON config.
type RateLimitHandlerConfig struct {
	// Enabled toggles rate limiting.
	Enabled bool `json:"enabled"`

	// PerIP configures per-IP rate limits.
	PerIP RateLimitRuleHandlerConfig `json:"per_ip"`

	// PerUser configures per-user rate limits.
	PerUser RateLimitRuleHandlerConfig `json:"per_user"`

	// TrustProxyHeaders enables X-Forwarded-For reading.
	TrustProxyHeaders bool `json:"trust_proxy_headers"`

	// ExemptPaths is a list of glob patterns that bypass rate limiting.
	ExemptPaths []string `json:"exempt_paths,omitempty"`
}

// RateLimitRuleHandlerConfig holds rate and burst config for one limit direction.
type RateLimitRuleHandlerConfig struct {
	// RequestsPerSecond is the sustained request rate.
	RequestsPerSecond float64 `json:"requests_per_second"`

	// Burst is the maximum burst size.
	Burst int `json:"burst"`
}

// RateLimitHandler is a Caddy HTTP handler module that enforces rate limits.
// It wraps the VibeWarden rate limit middleware so that it participates in
// Caddy's handler chain.
//
// The module is registered under the name "vibewarden_rate_limit" and
// referenced from the Caddy JSON configuration as:
//
//	{"handler": "vibewarden_rate_limit", ...}
type RateLimitHandler struct {
	// Config holds the rate limit configuration (populated by Caddy's JSON
	// unmarshaller via the Provision lifecycle).
	Config RateLimitHandlerConfig `json:"config"`

	// handler is the compiled Go middleware, created during Provision.
	handler func(http.Handler) http.Handler

	// ipLimiter and userLimiter are the live rate limiter instances.
	// They are closed during Cleanup to stop background goroutines.
	ipLimiter   ports.RateLimiter
	userLimiter ports.RateLimiter
}

// CaddyModule returns the module metadata used to register it with Caddy.
func (RateLimitHandler) CaddyModule() gocaddy.ModuleInfo {
	return gocaddy.ModuleInfo{
		ID:  "http.handlers.vibewarden_rate_limit",
		New: func() gocaddy.Module { return new(RateLimitHandler) },
	}
}

// Provision implements gocaddy.Provisioner.
// It is called once after the module is loaded from JSON, and creates the
// in-memory rate limiter stores and the compiled middleware handler.
func (h *RateLimitHandler) Provision(_ gocaddy.Context) error {
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))

	factory := ratelimitadapter.NewDefaultMemoryFactory()

	h.ipLimiter = factory.NewLimiter(ports.RateLimitRule{
		RequestsPerSecond: h.Config.PerIP.RequestsPerSecond,
		Burst:             h.Config.PerIP.Burst,
	})

	h.userLimiter = factory.NewLimiter(ports.RateLimitRule{
		RequestsPerSecond: h.Config.PerUser.RequestsPerSecond,
		Burst:             h.Config.PerUser.Burst,
	})

	cfg := ports.RateLimitConfig{
		Enabled:           h.Config.Enabled,
		TrustProxyHeaders: h.Config.TrustProxyHeaders,
		ExemptPaths:       h.Config.ExemptPaths,
		PerIP: ports.RateLimitRule{
			RequestsPerSecond: h.Config.PerIP.RequestsPerSecond,
			Burst:             h.Config.PerIP.Burst,
		},
		PerUser: ports.RateLimitRule{
			RequestsPerSecond: h.Config.PerUser.RequestsPerSecond,
			Burst:             h.Config.PerUser.Burst,
		},
	}

	h.handler = middleware.RateLimitMiddleware(h.ipLimiter, h.userLimiter, cfg, logger)

	return nil
}

// Cleanup implements gocaddy.CleanerUpper.
// It closes the rate limiter stores to stop background cleanup goroutines.
func (h *RateLimitHandler) Cleanup() error {
	if h.ipLimiter != nil {
		if err := h.ipLimiter.Close(); err != nil {
			return fmt.Errorf("closing IP rate limiter: %w", err)
		}
	}
	if h.userLimiter != nil {
		if err := h.userLimiter.Close(); err != nil {
			return fmt.Errorf("closing user rate limiter: %w", err)
		}
	}
	return nil
}

// ServeHTTP implements caddyhttp.MiddlewareHandler.
// It delegates to the compiled Go middleware handler.
func (h *RateLimitHandler) ServeHTTP(w http.ResponseWriter, r *http.Request, next caddyhttp.Handler) error {
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

// buildRateLimitHandlerJSON serialises a RateLimitHandlerConfig to the Caddy
// handler JSON fragment used in BuildCaddyConfig.
func buildRateLimitHandlerJSON(cfg ports.RateLimitConfig) (map[string]any, error) {
	handlerCfg := RateLimitHandlerConfig{
		Enabled:           cfg.Enabled,
		TrustProxyHeaders: cfg.TrustProxyHeaders,
		ExemptPaths:       cfg.ExemptPaths,
		PerIP: RateLimitRuleHandlerConfig{
			RequestsPerSecond: cfg.PerIP.RequestsPerSecond,
			Burst:             cfg.PerIP.Burst,
		},
		PerUser: RateLimitRuleHandlerConfig{
			RequestsPerSecond: cfg.PerUser.RequestsPerSecond,
			Burst:             cfg.PerUser.Burst,
		},
	}

	cfgBytes, err := json.Marshal(handlerCfg)
	if err != nil {
		return nil, fmt.Errorf("marshaling rate limit handler config: %w", err)
	}

	var cfgRaw json.RawMessage = cfgBytes

	return map[string]any{
		"handler": "vibewarden_rate_limit",
		"config":  cfgRaw,
	}, nil
}

// Interface guards — ensure RateLimitHandler satisfies the required Caddy
// and VibeWarden interfaces at compile time.
var (
	_ gocaddy.Provisioner         = (*RateLimitHandler)(nil)
	_ gocaddy.CleanerUpper        = (*RateLimitHandler)(nil)
	_ caddyhttp.MiddlewareHandler = (*RateLimitHandler)(nil)
)
