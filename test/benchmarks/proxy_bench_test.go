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
	"strings"
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
func (noopMetrics) SetTLSCertExpirySeconds(_ string, _ float64)                  {}

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

// benchJSONBody is the request payload used by the "_WithBody" benchmarks. It
// mirrors BenchmarkScanRequest_Typical in internal/domain/waf so the proxy-level
// and domain-level body-scan numbers are directly comparable.
const benchJSONBody = `{"username":"alice","action":"login"}`

// newBenchRequestWithBody returns a fresh POST request carrying a small JSON
// body. Unlike newBenchRequest, this exercises the WAF body-scan path: the
// middleware reads up to 8 KB of the body, runs every rule against it, and
// restores the body for downstream handlers.
func newBenchRequestWithBody() *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/api/resource", strings.NewReader(benchJSONBody))
	r.Header.Set("Content-Type", "application/json")
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

// BenchmarkProxy_DirectPassthrough_WithBody is the no-middleware baseline for
// the "_WithBody" benchmarks. It exists so the WAF body-scan overhead can be
// read as a delta against a request of the same shape — a POST with a JSON body
// costs more to construct than the GET used by BenchmarkProxy_DirectPassthrough,
// and that construction cost must not be charged to the WAF.
func BenchmarkProxy_DirectPassthrough_WithBody(b *testing.B) {
	handler := upstreamHandler

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, newBenchRequestWithBody())
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
		ipLimiter, userLimiter, cfg, logger, nil, nil, nil,
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
// benign GET request with no body (no rules fire). It scans URL query
// parameters and selected headers; the body-scan path allocates its read buffer
// but has zero bytes to match rules against.
//
// This is the floor of WAF cost, not a representative number for a JSON API.
// Use BenchmarkProxy_WithWAF_WithBody for that.
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

// BenchmarkProxy_WithWAF_WithBody measures the latency added by WAFMiddleware
// against a benign POST carrying a small JSON body. Every WAF rule is evaluated
// against the body bytes in addition to query parameters and headers, which is
// the dominant cost for real API traffic. Compare against
// BenchmarkProxy_DirectPassthrough_WithBody, not the no-body baseline.
func BenchmarkProxy_WithWAF_WithBody(b *testing.B) {
	rs := waf.DefaultRuleSet()
	cfg := defaultWAFCfg()
	logger := discardLogger()

	handler := middleware.WAFMiddleware(rs, cfg, logger, noopMetrics{}, nil)(upstreamHandler)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, newBenchRequestWithBody())
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
	rateMW := middleware.RateLimitMiddleware(ipLimiter, userLimiter, defaultRateLimitCfg(), logger, nil, nil, nil)
	secMW := middleware.SecurityHeaders(defaultSecurityCfg())

	handler := secMW(rateMW(wafMW(upstreamHandler)))

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, newBenchRequest())
	}
}
