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
	"strconv"
	"time"

	gocaddy "github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"

	logadapter "github.com/vibewarden/vibewarden/internal/adapters/log"
	"github.com/vibewarden/vibewarden/internal/domain/events"
	"github.com/vibewarden/vibewarden/internal/middleware"
	"github.com/vibewarden/vibewarden/internal/ports"
)

func init() {
	gocaddy.RegisterModule(RetryHandler{})
}

// idempotentMethods is the set of HTTP methods that are safe to retry.
// POST is intentionally excluded to avoid duplicate side effects.
var idempotentMethods = map[string]bool{
	http.MethodGet:     true,
	http.MethodHead:    true,
	http.MethodOptions: true,
	http.MethodPut:     true,
	http.MethodDelete:  true,
}

// RetryHandlerConfig is the JSON-serialisable configuration for the
// RetryHandler Caddy module. It is embedded in the Caddy JSON config
// under the "config" key of the "vibewarden_retry" handler entry.
type RetryHandlerConfig struct {
	// MaxAttempts is the total number of attempts (including the initial request).
	MaxAttempts int `json:"max_attempts"`

	// InitialBackoffMs is the wait before the first retry in milliseconds.
	InitialBackoffMs float64 `json:"initial_backoff_ms"`

	// MaxBackoffMs is the upper bound on the computed backoff in milliseconds.
	MaxBackoffMs float64 `json:"max_backoff_ms"`

	// RetryOn is the set of HTTP status codes that should trigger a retry.
	RetryOn []int `json:"retry_on"`
}

// RetryHandler is a Caddy HTTP middleware module that retries upstream requests
// with exponential backoff when the upstream returns a configured error status.
//
// Behaviour:
//   - Only idempotent HTTP methods are retried (GET, HEAD, OPTIONS, PUT, DELETE).
//     POST (and other non-idempotent methods) are never retried.
//   - Retries stop immediately when the request context is cancelled (e.g. by the
//     timeout middleware upstream in the chain).
//   - Each retry attempt emits an upstream.retry structured event and increments
//     the vibewarden_upstream_retries_total Prometheus counter.
//   - The module is placed after the circuit breaker handler in the chain.
//     If the circuit breaker returns a non-2xx upstream response that response
//     passes through here unchanged (the circuit breaker already handled it by
//     writing 503 and returning, so this handler's next chain ends there).
//
// The module is registered under the name "vibewarden_retry" and referenced
// from the Caddy JSON configuration as:
//
//	{"handler": "vibewarden_retry", ...}
type RetryHandler struct {
	// Config holds the retry configuration, populated by Caddy's JSON
	// unmarshaller during the Provision lifecycle.
	Config RetryHandlerConfig `json:"config"`

	// logger is used to emit error messages when event logging fails.
	logger *slog.Logger

	// eventLogger emits structured upstream.retry events.
	eventLogger ports.EventLogger

	// metrics records retry counters.
	metrics ports.MetricsCollector
}

// CaddyModule returns the module metadata used to register it with Caddy.
func (RetryHandler) CaddyModule() gocaddy.ModuleInfo {
	return gocaddy.ModuleInfo{
		ID:  "http.handlers.vibewarden_retry",
		New: func() gocaddy.Module { return new(RetryHandler) },
	}
}

// Provision implements gocaddy.Provisioner. It initialises the logger and
// event logger used by this handler instance.
func (h *RetryHandler) Provision(_ gocaddy.Context) error {
	h.logger = slog.New(slog.NewJSONHandler(os.Stderr, nil))
	h.eventLogger = logadapter.NewSlogEventLogger(os.Stdout)
	return nil
}

// ServeHTTP implements caddyhttp.MiddlewareHandler. For idempotent methods it
// retries the upstream call up to MaxAttempts times with exponential backoff
// when the upstream returns one of the configured RetryOn status codes.
// Retries stop early if the request context is cancelled.
//
// Each attempt is buffered in a retryResponseWriter so that a failed attempt
// does not commit bytes to the real ResponseWriter. Only the final response
// (success or last attempt) is flushed to w.
func (h *RetryHandler) ServeHTTP(w http.ResponseWriter, r *http.Request, next caddyhttp.Handler) error {
	maxAttempts := h.Config.MaxAttempts
	if maxAttempts < 2 {
		maxAttempts = 2
	}

	// Non-idempotent methods are never retried; delegate immediately.
	if !idempotentMethods[r.Method] {
		return next.ServeHTTP(w, r)
	}

	initialBackoff := time.Duration(h.Config.InitialBackoffMs * float64(time.Millisecond))
	maxBackoff := time.Duration(h.Config.MaxBackoffMs * float64(time.Millisecond))
	if maxBackoff <= 0 {
		maxBackoff = 10 * time.Second
	}

	retryOn := h.retryOnSet()
	clientIP := middleware.ExtractClientIP(r, false)

	backoff := initialBackoff

	var lastBuf *retryResponseWriter

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		// Check context cancellation before every attempt (including the first).
		if errors.Is(r.Context().Err(), context.Canceled) ||
			errors.Is(r.Context().Err(), context.DeadlineExceeded) {
			return r.Context().Err()
		}

		if attempt > 1 {
			prevStatus := 0
			if lastBuf != nil {
				prevStatus = lastBuf.status
			}
			// Emit structured retry event and increment metric before sleeping.
			h.emitRetryEvent(r, attempt-1, prevStatus, clientIP)

			// Wait for backoff or context cancellation — whichever comes first.
			select {
			case <-r.Context().Done():
				return r.Context().Err()
			case <-time.After(backoff):
			}

			// Double the backoff, capped at maxBackoff.
			backoff *= 2
			if backoff > maxBackoff {
				backoff = maxBackoff
			}
		}

		// Buffer this attempt — do not write to the real ResponseWriter yet.
		buf := newRetryResponseWriter(w)
		err := next.ServeHTTP(buf, r)
		lastBuf = buf

		// If the handler returned an error it is a transport-level failure
		// (not an HTTP error response). Do not retry on transport errors.
		if err != nil {
			return err
		}

		// Success or a status code we don't retry on — flush and stop.
		if !retryOn[buf.status] {
			buf.flush()
			return nil
		}
	}

	// All attempts exhausted — add Retry-After hint and flush the last response.
	if lastBuf != nil {
		if lastBuf.status == http.StatusServiceUnavailable {
			backoffMs := h.Config.InitialBackoffMs * float64(int(1)<<uint(h.Config.MaxAttempts-1)) //nolint:gosec // MaxAttempts is validated to be small (≤10) during config parsing
			if backoffMs > h.Config.MaxBackoffMs {
				backoffMs = h.Config.MaxBackoffMs
			}
			retryAfter := int(backoffMs / 1000)
			if retryAfter < 1 {
				retryAfter = 1
			}
			lastBuf.Header().Set("Retry-After", strconv.Itoa(retryAfter))
		}
		lastBuf.flush()
	}
	return nil
}

// retryOnSet converts the RetryOn slice to a set for O(1) lookup.
func (h *RetryHandler) retryOnSet() map[int]bool {
	set := make(map[int]bool, len(h.Config.RetryOn))
	for _, code := range h.Config.RetryOn {
		set[code] = true
	}
	return set
}

// emitRetryEvent logs a structured upstream.retry event and increments the
// Prometheus counter. Both operations are best-effort.
func (h *RetryHandler) emitRetryEvent(r *http.Request, attempt, statusCode int, clientIP string) {
	if h.eventLogger != nil {
		ev := events.NewUpstreamRetry(events.UpstreamRetryParams{
			Method:     r.Method,
			Path:       r.URL.Path,
			Attempt:    attempt,
			StatusCode: statusCode,
			ClientIP:   clientIP,
		})
		if err := h.eventLogger.Log(r.Context(), ev); err != nil {
			h.logger.Error("retry: failed to emit upstream.retry event",
				slog.String("error", err.Error()))
		}
	}

	if h.metrics != nil {
		h.metrics.IncUpstreamRetry(r.Method)
	}
}

// retryResponseWriter buffers an HTTP response so that a failed upstream
// attempt can be discarded before committing to the real ResponseWriter.
// Headers and body bytes are held in memory; flush() copies them to the
// underlying writer exactly once.
type retryResponseWriter struct {
	underlying http.ResponseWriter
	header     http.Header
	status     int
	body       []byte
}

// newRetryResponseWriter allocates a new retryResponseWriter that buffers
// output destined for underlying.
func newRetryResponseWriter(underlying http.ResponseWriter) *retryResponseWriter {
	// Copy the existing header map so that callers cannot mutate the
	// underlying header directly through this writer.
	h := make(http.Header)
	for k, v := range underlying.Header() {
		h[k] = v
	}
	return &retryResponseWriter{
		underlying: underlying,
		header:     h,
		status:     http.StatusOK,
	}
}

// Header returns the buffered response header map.
func (rw *retryResponseWriter) Header() http.Header { return rw.header }

// WriteHeader buffers the status code.
func (rw *retryResponseWriter) WriteHeader(code int) { rw.status = code }

// Write appends bytes to the buffered body.
func (rw *retryResponseWriter) Write(b []byte) (int, error) {
	rw.body = append(rw.body, b...)
	return len(b), nil
}

// flush writes the buffered status, headers, and body to the underlying
// ResponseWriter. It must be called exactly once after a successful (or final)
// attempt.
func (rw *retryResponseWriter) flush() {
	dst := rw.underlying.Header()
	for k, v := range rw.header {
		dst[k] = v
	}
	rw.underlying.WriteHeader(rw.status)
	if len(rw.body) > 0 {
		_, _ = rw.underlying.Write(rw.body)
	}
}

// buildRetryHandlerJSON serialises a RetryConfig to the Caddy handler JSON
// fragment used in BuildCaddyConfig. Returns nil when retry is not enabled.
func buildRetryHandlerJSON(cfg ports.ResilienceConfig) (map[string]any, error) {
	rc := cfg.Retry
	if !rc.Enabled {
		return nil, nil
	}

	handlerCfg := RetryHandlerConfig{
		MaxAttempts:      rc.MaxAttempts,
		InitialBackoffMs: float64(rc.InitialBackoff) / float64(time.Millisecond),
		MaxBackoffMs:     float64(rc.MaxBackoff) / float64(time.Millisecond),
		RetryOn:          rc.RetryOn,
	}

	cfgBytes, err := json.Marshal(handlerCfg)
	if err != nil {
		return nil, fmt.Errorf("marshaling retry handler config: %w", err)
	}

	return map[string]any{
		"handler": "vibewarden_retry",
		"config":  json.RawMessage(cfgBytes),
	}, nil
}

// Interface guards — ensure RetryHandler satisfies required Caddy interfaces
// at compile time.
var (
	_ gocaddy.Provisioner         = (*RetryHandler)(nil)
	_ caddyhttp.MiddlewareHandler = (*RetryHandler)(nil)
)
