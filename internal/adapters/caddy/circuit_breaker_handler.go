// Package caddy implements the ProxyServer port using embedded Caddy.
package caddy

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	gocaddy "github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"

	logadapter "github.com/vibewarden/vibewarden/internal/adapters/log"
	resilienceadapter "github.com/vibewarden/vibewarden/internal/adapters/resilience"
	"github.com/vibewarden/vibewarden/internal/middleware"
	"github.com/vibewarden/vibewarden/internal/ports"
)

func init() {
	gocaddy.RegisterModule(CircuitBreakerHandler{})
}

// CircuitBreakerHandlerConfig is the JSON-serialisable configuration for the
// CircuitBreakerHandler Caddy module.
type CircuitBreakerHandlerConfig struct {
	// Threshold is the number of consecutive failures required to trip the
	// circuit from Closed to Open.
	Threshold int `json:"threshold"`

	// TimeoutSeconds is how long the circuit stays Open before transitioning
	// to HalfOpen to allow a probe request.
	TimeoutSeconds float64 `json:"timeout_seconds"`
}

// CircuitBreakerHandler is a Caddy HTTP middleware module that implements the
// three-state circuit breaker pattern (Closed/Open/HalfOpen). When the circuit
// is open it immediately returns 503 Service Unavailable without contacting the
// upstream.
//
// The following upstream responses count as failures:
//   - 502 Bad Gateway
//   - 503 Service Unavailable
//   - 504 Gateway Timeout
//
// The module is registered under the name "vibewarden_circuit_breaker" and
// referenced from the Caddy JSON configuration as:
//
//	{"handler": "vibewarden_circuit_breaker", ...}
type CircuitBreakerHandler struct {
	// Config holds the circuit breaker configuration, populated by Caddy's JSON
	// unmarshaller during the Provision lifecycle.
	Config CircuitBreakerHandlerConfig `json:"config"`

	// logger is used for internal error messages.
	logger *slog.Logger

	// eventLogger emits structured state-transition events.
	eventLogger ports.EventLogger

	// cb is the concurrency-safe circuit breaker adapter.
	cb ports.CircuitBreaker
}

// CaddyModule returns the module metadata used to register it with Caddy.
func (CircuitBreakerHandler) CaddyModule() gocaddy.ModuleInfo {
	return gocaddy.ModuleInfo{
		ID:  "http.handlers.vibewarden_circuit_breaker",
		New: func() gocaddy.Module { return new(CircuitBreakerHandler) },
	}
}

// Provision implements gocaddy.Provisioner. It initialises the circuit breaker,
// logger and event logger.
func (h *CircuitBreakerHandler) Provision(_ gocaddy.Context) error {
	h.logger = slog.New(slog.NewJSONHandler(os.Stderr, nil))
	h.eventLogger = logadapter.NewSlogEventLogger(os.Stdout)

	cfg := ports.CircuitBreakerConfig{
		Enabled:   true,
		Threshold: h.Config.Threshold,
		Timeout:   time.Duration(h.Config.TimeoutSeconds * float64(time.Second)),
	}

	cb, err := resilienceadapter.NewInMemoryCircuitBreaker(cfg, h.logger, h.eventLogger, nil)
	if err != nil {
		return fmt.Errorf("provisioning circuit breaker: %w", err)
	}
	h.cb = cb
	return nil
}

// ServeHTTP implements caddyhttp.MiddlewareHandler. When the circuit is open it
// writes a structured 503 response immediately. Otherwise it delegates to the
// next handler and classifies the upstream response.
func (h *CircuitBreakerHandler) ServeHTTP(w http.ResponseWriter, r *http.Request, next caddyhttp.Handler) error {
	if h.cb.IsOpen() {
		middleware.WriteErrorResponse(w, r, http.StatusServiceUnavailable,
			"circuit_breaker_open",
			"upstream is currently unavailable; try again later")
		return nil
	}

	// Wrap the ResponseWriter to capture the status code written by downstream
	// handlers so we can classify the response.
	crw := &captureResponseWriter{ResponseWriter: w}

	err := next.ServeHTTP(crw, r)

	if err != nil || isUpstreamFailureStatus(crw.status) {
		h.cb.RecordFailure()
	} else {
		h.cb.RecordSuccess()
	}

	return err
}

// isUpstreamFailureStatus returns true for HTTP status codes that indicate a
// failure in the upstream service (502, 503, 504). Client errors (4xx) are not
// counted as upstream failures.
func isUpstreamFailureStatus(status int) bool {
	switch status {
	case http.StatusBadGateway,
		http.StatusServiceUnavailable,
		http.StatusGatewayTimeout:
		return true
	}
	return false
}

// captureResponseWriter wraps http.ResponseWriter to capture the status code.
type captureResponseWriter struct {
	http.ResponseWriter
	status int
}

// WriteHeader captures the status code and delegates to the underlying writer.
func (c *captureResponseWriter) WriteHeader(status int) {
	c.status = status
	c.ResponseWriter.WriteHeader(status)
}

// buildCircuitBreakerHandlerJSON serialises a CircuitBreakerConfig to the Caddy
// handler JSON fragment used in BuildCaddyConfig. Returns nil when the circuit
// breaker is not enabled.
func buildCircuitBreakerHandlerJSON(cfg ports.ResilienceConfig) (map[string]any, error) {
	cbCfg := cfg.CircuitBreaker
	if !cbCfg.Enabled {
		return nil, nil
	}

	handlerCfg := CircuitBreakerHandlerConfig{
		Threshold:      cbCfg.Threshold,
		TimeoutSeconds: cbCfg.Timeout.Seconds(),
	}

	cfgBytes, err := json.Marshal(handlerCfg)
	if err != nil {
		return nil, fmt.Errorf("marshaling circuit breaker handler config: %w", err)
	}

	return map[string]any{
		"handler": "vibewarden_circuit_breaker",
		"config":  json.RawMessage(cfgBytes),
	}, nil
}

// Interface guards — ensure CircuitBreakerHandler satisfies required Caddy
// interfaces at compile time.
var (
	_ gocaddy.Provisioner         = (*CircuitBreakerHandler)(nil)
	_ caddyhttp.MiddlewareHandler = (*CircuitBreakerHandler)(nil)
)
