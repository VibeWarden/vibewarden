// Package caddy implements the ProxyServer port using embedded Caddy.
package caddy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	gocaddy "github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"

	logadapter "github.com/vibewarden/vibewarden/internal/adapters/log"
	"github.com/vibewarden/vibewarden/internal/domain/events"
	"github.com/vibewarden/vibewarden/internal/middleware"
	"github.com/vibewarden/vibewarden/internal/ports"
)

func init() {
	gocaddy.RegisterModule(TimeoutHandler{})
}

// TimeoutHandlerConfig is the JSON-serialisable configuration for the
// TimeoutHandler Caddy module. It is embedded in the Caddy JSON config
// under the "config" key of the "vibewarden_timeout" handler entry.
type TimeoutHandlerConfig struct {
	// TimeoutSeconds is the maximum number of seconds to wait for the upstream
	// to respond before returning 504 Gateway Timeout.
	// A value of 0 disables the timeout.
	TimeoutSeconds float64 `json:"timeout_seconds"`
}

// TimeoutHandler is a Caddy HTTP middleware module that enforces a maximum
// upstream response time. When the timeout fires the handler:
//
//   - writes a structured JSON 504 Gateway Timeout response via WriteErrorResponse
//   - emits an upstream.timeout structured event
//   - increments the vibewarden_upstream_timeouts_total Prometheus counter
//
// The module is registered under the name "vibewarden_timeout" and referenced
// from the Caddy JSON configuration as:
//
//	{"handler": "vibewarden_timeout", ...}
type TimeoutHandler struct {
	// Config holds the timeout configuration, populated by Caddy's JSON
	// unmarshaller during the Provision lifecycle.
	Config TimeoutHandlerConfig `json:"config"`

	// logger is used to emit error messages when event logging fails.
	logger *slog.Logger

	// eventLogger emits structured upstream.timeout events.
	eventLogger ports.EventLogger

	// metrics records timeout counters.
	metrics ports.MetricsCollector
}

// CaddyModule returns the module metadata used to register it with Caddy.
func (TimeoutHandler) CaddyModule() gocaddy.ModuleInfo {
	return gocaddy.ModuleInfo{
		ID:  "http.handlers.vibewarden_timeout",
		New: func() gocaddy.Module { return new(TimeoutHandler) },
	}
}

// Provision implements gocaddy.Provisioner. It initialises the logger and
// event logger used by this handler instance.
func (h *TimeoutHandler) Provision(_ gocaddy.Context) error {
	h.logger = slog.New(slog.NewJSONHandler(os.Stderr, nil))
	h.eventLogger = logadapter.NewSlogEventLogger(os.Stdout)
	return nil
}

// ServeHTTP implements caddyhttp.MiddlewareHandler. It cancels the request
// context after the configured timeout and, if the deadline is exceeded,
// responds with 504 Gateway Timeout together with a structured event.
func (h *TimeoutHandler) ServeHTTP(w http.ResponseWriter, r *http.Request, next caddyhttp.Handler) error {
	timeout := time.Duration(h.Config.TimeoutSeconds * float64(time.Second))
	if timeout <= 0 {
		return next.ServeHTTP(w, r)
	}

	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()

	r = r.WithContext(ctx)

	// tw captures whether a response has been committed so we do not write
	// a second response body when the deadline fires after the upstream
	// already started writing.
	tw := &timeoutResponseWriter{ResponseWriter: w}

	err := next.ServeHTTP(tw, r)
	if err == nil && !errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return nil
	}

	if errors.Is(ctx.Err(), context.DeadlineExceeded) || isTimeoutError(err) {
		if !tw.written {
			middleware.WriteErrorResponse(w, r, http.StatusGatewayTimeout, "upstream_timeout",
				"the upstream application did not respond in time")
		}
		h.emitTimeoutEvent(r, timeout)
		// Returning nil prevents Caddy from writing a second error response.
		return nil
	}

	return err
}

// emitTimeoutEvent logs a structured upstream.timeout event and increments
// the Prometheus counter. Both operations are best-effort and do not affect
// the HTTP response.
func (h *TimeoutHandler) emitTimeoutEvent(r *http.Request, timeout time.Duration) {
	clientIP := middleware.ExtractClientIP(r, false)

	if h.eventLogger != nil {
		ev := events.NewUpstreamTimeout(events.UpstreamTimeoutParams{
			Method:         r.Method,
			Path:           r.URL.Path,
			TimeoutSeconds: timeout.Seconds(),
			ClientIP:       clientIP,
		})
		if err := h.eventLogger.Log(r.Context(), ev); err != nil {
			h.logger.Error("timeout: failed to emit upstream.timeout event", slog.String("error", err.Error()))
		}
	}

	if h.metrics != nil {
		h.metrics.IncUpstreamTimeout()
	}
}

// isTimeoutError returns true when err looks like a context deadline exceeded
// error coming from the upstream transport.
func isTimeoutError(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, context.DeadlineExceeded)
}

// timeoutResponseWriter wraps http.ResponseWriter to track whether any bytes
// have been written. This prevents writing a 504 body after the upstream has
// already committed a response.
type timeoutResponseWriter struct {
	http.ResponseWriter
	written bool
}

// WriteHeader marks the response as started and delegates to the underlying writer.
func (tw *timeoutResponseWriter) WriteHeader(code int) {
	tw.written = true
	tw.ResponseWriter.WriteHeader(code)
}

// Write marks the response as started and delegates to the underlying writer.
func (tw *timeoutResponseWriter) Write(b []byte) (int, error) {
	tw.written = true
	return tw.ResponseWriter.Write(b)
}

// buildTimeoutHandlerJSON serialises a ResilienceConfig to the Caddy handler
// JSON fragment used in BuildCaddyConfig. Returns nil when no timeout is
// configured (Timeout == 0), in which case the caller should skip this handler.
func buildTimeoutHandlerJSON(cfg ports.ResilienceConfig) (map[string]any, error) {
	if cfg.Timeout <= 0 {
		return nil, nil
	}

	handlerCfg := TimeoutHandlerConfig{
		TimeoutSeconds: cfg.Timeout.Seconds(),
	}

	cfgBytes, err := json.Marshal(handlerCfg)
	if err != nil {
		return nil, fmt.Errorf("marshaling timeout handler config: %w", err)
	}

	return map[string]any{
		"handler": "vibewarden_timeout",
		"config":  json.RawMessage(cfgBytes),
	}, nil
}

// Interface guards — ensure TimeoutHandler satisfies required Caddy interfaces
// at compile time.
var (
	_ gocaddy.Provisioner         = (*TimeoutHandler)(nil)
	_ caddyhttp.MiddlewareHandler = (*TimeoutHandler)(nil)
)
