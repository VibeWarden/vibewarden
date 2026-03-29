// Package benchmarks provides Go benchmark tests for VibeWarden's middleware
// stack. Each benchmark measures per-request latency through a specific
// middleware configuration using httptest.Server and httptest.ResponseRecorder.
//
// Run all benchmarks:
//
//	go test -bench=. -benchmem -benchtime=5s ./test/benchmarks/
//
// Run a single benchmark and print memory allocation stats:
//
//	go test -bench=BenchmarkProxy_WithSecurityHeaders -benchmem ./test/benchmarks/
package benchmarks

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/vibewarden/vibewarden/internal/domain/resilience"
	"github.com/vibewarden/vibewarden/internal/domain/waf"
	"github.com/vibewarden/vibewarden/internal/middleware"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// ---------------------------------------------------------------------------
// Fakes / stubs required by middleware constructors
// ---------------------------------------------------------------------------

// noopRateLimiter always allows requests. It is the fastest possible
// implementation — ideal for benchmarks that do not test rate-limiting logic.
type noopRateLimiter struct{}

func (noopRateLimiter) Allow(_ context.Context, _ string) ports.RateLimitResult {
	return ports.RateLimitResult{Allowed: true, Remaining: 999, Limit: 1000, Burst: 1000}
}

func (noopRateLimiter) Close() error { return nil }

// noopMetrics is a no-op MetricsCollector used by benchmarks that need a
// non-nil collector without the overhead of a real Prometheus registry.
type noopMetrics struct{}

func (noopMetrics) IncRequestTotal(_, _, _ string)                               {}
func (noopMetrics) ObserveRequestDuration(_, _ string, _ time.Duration)          {}
func (noopMetrics) IncRateLimitHit(_ string)                                     {}
func (noopMetrics) IncAuthDecision(_ string)                                     {}
func (noopMetrics) IncUpstreamError()                                            {}
func (noopMetrics) IncUpstreamTimeout()                                          {}
func (noopMetrics) IncUpstreamRetry(_ string)                                    {}
func (noopMetrics) SetActiveConnections(_ int)                                   {}
func (noopMetrics) SetCircuitBreakerState(_ context.Context, _ resilience.State) {}
func (noopMetrics) IncWAFDetection(_, _ string)                                  {}
func (noopMetrics) IncEgressRequestTotal(_, _, _ string)                         {}
func (noopMetrics) ObserveEgressDuration(_, _ string, _ time.Duration)           {}
func (noopMetrics) IncEgressErrorTotal(_ string)                                 {}

// ---------------------------------------------------------------------------
// Shared helpers
// ---------------------------------------------------------------------------

// upstreamHandler is the minimal target handler that every benchmark proxies
// through. It writes a fixed "OK" body so all measurements include realistic
// response path work.
var upstreamHandler = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, `{"status":"ok"}`)
})

// newBenchRequest returns a fresh GET request pointed at the root path with a
// RemoteAddr that satisfies net.SplitHostPort (required by rate-limit middleware).
func newBenchRequest() *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/api/resource", nil)
	r.RemoteAddr = "10.0.0.1:12345"
	return r
}

// discardLogger returns a logger that discards all output, avoiding I/O cost
// inside benchmarks.
func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// defaultSecurityCfg returns a fully populated security headers config.
func defaultSecurityCfg() ports.SecurityHeadersConfig {
	return middleware.DefaultSecurityHeadersConfig()
}

// defaultRateLimitCfg returns a rate-limit config that allows all traffic and
// does not trust proxy headers.
func defaultRateLimitCfg() ports.RateLimitConfig {
	return ports.RateLimitConfig{
		Enabled:           true,
		TrustProxyHeaders: false,
	}
}

// defaultWAFCfg returns a WAF config in block mode with no exempt paths.
func defaultWAFCfg() middleware.WAFConfig {
	return middleware.WAFConfig{
		Mode: middleware.WAFModeBlock,
	}
}

// ---------------------------------------------------------------------------
// BenchmarkProxy_DirectPassthrough
// ---------------------------------------------------------------------------

// BenchmarkProxy_DirectPassthrough measures the baseline cost of serving a
// request with no middleware at all — the raw httptest overhead.
func BenchmarkProxy_DirectPassthrough(b *testing.B) {
	handler := upstreamHandler

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, newBenchRequest())
	}
}

// ---------------------------------------------------------------------------
// BenchmarkProxy_WithSecurityHeaders
// ---------------------------------------------------------------------------

// BenchmarkProxy_WithSecurityHeaders measures the latency added by the
// SecurityHeaders middleware alone. The middleware sets ~6 HTTP response
// headers on every request.
func BenchmarkProxy_WithSecurityHeaders(b *testing.B) {
	handler := middleware.SecurityHeaders(defaultSecurityCfg())(upstreamHandler)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, newBenchRequest())
	}
}

// ---------------------------------------------------------------------------
// BenchmarkProxy_WithRateLimiting
// ---------------------------------------------------------------------------

// BenchmarkProxy_WithRateLimiting measures the latency added by the
// RateLimitMiddleware when both per-IP and per-user limiters always allow the
// request. This exercises the middleware's IP extraction and Allow() call path
// without the cost of actual token-bucket accounting (which would require a
// real limiter).
func BenchmarkProxy_WithRateLimiting(b *testing.B) {
	ipLimiter := noopRateLimiter{}
	userLimiter := noopRateLimiter{}
	logger := discardLogger()
	cfg := defaultRateLimitCfg()

	handler := middleware.RateLimitMiddleware(
		ipLimiter, userLimiter, cfg, logger, nil, nil,
	)(upstreamHandler)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, newBenchRequest())
	}
}

// ---------------------------------------------------------------------------
// BenchmarkProxy_WithWAF
// ---------------------------------------------------------------------------

// BenchmarkProxy_WithWAF measures the latency added by WAFMiddleware against a
// benign request (no rules fire). This exercises the full scan path over URL
// query parameters, selected headers, and the (empty) request body.
func BenchmarkProxy_WithWAF(b *testing.B) {
	rs := waf.DefaultRuleSet()
	cfg := defaultWAFCfg()
	logger := discardLogger()

	handler := middleware.WAFMiddleware(rs, cfg, logger, noopMetrics{}, nil)(upstreamHandler)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, newBenchRequest())
	}
}

// ---------------------------------------------------------------------------
// BenchmarkProxy_AllMiddleware
// ---------------------------------------------------------------------------

// BenchmarkProxy_AllMiddleware measures the cumulative latency when all
// middleware layers are stacked in the order VibeWarden uses in production:
//
//  1. SecurityHeaders  — set response headers
//  2. RateLimiting     — check per-IP token bucket
//  3. WAF              — scan request for injection patterns
//
// The tracing and metrics middleware are omitted from this benchmark because
// their costs depend on the OTel SDK and Prometheus registry implementations
// respectively, which fall outside the pure middleware latency budget.
func BenchmarkProxy_AllMiddleware(b *testing.B) {
	rs := waf.DefaultRuleSet()
	ipLimiter := noopRateLimiter{}
	userLimiter := noopRateLimiter{}
	logger := discardLogger()

	// Build the chain from innermost to outermost.
	wafMW := middleware.WAFMiddleware(rs, defaultWAFCfg(), logger, noopMetrics{}, nil)
	rateMW := middleware.RateLimitMiddleware(ipLimiter, userLimiter, defaultRateLimitCfg(), logger, nil, nil)
	secMW := middleware.SecurityHeaders(defaultSecurityCfg())

	handler := secMW(rateMW(wafMW(upstreamHandler)))

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, newBenchRequest())
	}
}
