package metrics

import (
	"context"
	"math"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/vibewarden/vibewarden/internal/domain/resilience"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// OTelAdapter implements ports.MetricsCollector using an OTel MeterProvider.
// It creates counters and histograms via ports.Meter and records observations.
// All methods are safe for concurrent use.
type OTelAdapter struct {
	requestsTotal             ports.Int64Counter
	requestDuration           ports.Float64Histogram
	rateLimitHits             ports.Int64Counter
	authDecisions             ports.Int64Counter
	upstreamErrors            ports.Int64Counter
	upstreamTimeouts          ports.Int64Counter
	upstreamRetries           ports.Int64Counter
	activeConnections         ports.Int64UpDownCounter
	circuitBreakerState       ports.Int64UpDownCounter
	upstreamHealthy           ports.Int64UpDownCounter
	wafDetections             ports.Int64Counter
	egressRequestsTotal       ports.Int64Counter
	egressDuration            ports.Float64Histogram
	egressErrorsTotal         ports.Int64Counter
	tlsCertExpirySeconds      ports.Int64UpDownCounter
	currentConns              atomic.Int64
	currentCBState            atomic.Int64
	currentUpstreamHealthy    atomic.Int64
	currentCertExpiryByDomain sync.Map // map[string]*atomic.Int64
	pathMatcher               *PathMatcher
	handler                   http.Handler
}

// NewOTelAdapter creates a new OTel-backed MetricsCollector.
// The provider must be initialized (Init called) before calling this function.
// pathPatterns configures path normalization (e.g., "/users/:id") to prevent
// high-cardinality labels.
func NewOTelAdapter(provider ports.OTelProvider, pathPatterns []string) (*OTelAdapter, error) {
	meter := provider.Meter()

	requestsTotal, err := meter.Int64Counter("vibewarden_requests_total",
		ports.WithDescription("Total number of HTTP requests processed."),
	)
	if err != nil {
		return nil, err
	}

	requestDuration, err := meter.Float64Histogram("vibewarden_request_duration_seconds",
		ports.WithDescription("HTTP request duration in seconds."),
		ports.WithUnit("s"),
		ports.WithExplicitBuckets([]float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10}),
	)
	if err != nil {
		return nil, err
	}

	rateLimitHits, err := meter.Int64Counter("vibewarden_rate_limit_hits_total",
		ports.WithDescription("Total number of rate limit hits."),
	)
	if err != nil {
		return nil, err
	}

	authDecisions, err := meter.Int64Counter("vibewarden_auth_decisions_total",
		ports.WithDescription("Total number of authentication decisions."),
	)
	if err != nil {
		return nil, err
	}

	upstreamErrors, err := meter.Int64Counter("vibewarden_upstream_errors_total",
		ports.WithDescription("Total number of upstream connection errors."),
	)
	if err != nil {
		return nil, err
	}

	upstreamTimeouts, err := meter.Int64Counter("vibewarden_upstream_timeouts_total",
		ports.WithDescription("Total number of upstream requests terminated due to timeout."),
	)
	if err != nil {
		return nil, err
	}

	upstreamRetries, err := meter.Int64Counter("vibewarden_upstream_retries_total",
		ports.WithDescription("Total number of upstream retry attempts."),
	)
	if err != nil {
		return nil, err
	}

	activeConnections, err := meter.Int64UpDownCounter("vibewarden_active_connections",
		ports.WithDescription("Current number of active proxy connections."),
	)
	if err != nil {
		return nil, err
	}

	circuitBreakerState, err := meter.Int64UpDownCounter("vibewarden_circuit_breaker_state",
		ports.WithDescription("Current circuit breaker state: 0=closed, 1=open, 2=half_open."),
	)
	if err != nil {
		return nil, err
	}

	upstreamHealthy, err := meter.Int64UpDownCounter("vibewarden_upstream_healthy",
		ports.WithDescription("Whether the upstream application is healthy: 1=healthy, 0=unhealthy or unknown."),
	)
	if err != nil {
		return nil, err
	}

	wafDetections, err := meter.Int64Counter("vibewarden_waf_detections_total",
		ports.WithDescription("Total number of WAF rule detections."),
	)
	if err != nil {
		return nil, err
	}

	egressRequestsTotal, err := meter.Int64Counter("vibewarden_egress_requests_total",
		ports.WithDescription("Total number of egress proxy requests."),
	)
	if err != nil {
		return nil, err
	}

	egressDuration, err := meter.Float64Histogram("vibewarden_egress_request_duration_seconds",
		ports.WithDescription("Egress proxy request duration in seconds."),
		ports.WithUnit("s"),
		ports.WithExplicitBuckets([]float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10}),
	)
	if err != nil {
		return nil, err
	}

	egressErrorsTotal, err := meter.Int64Counter("vibewarden_egress_errors_total",
		ports.WithDescription("Total number of egress proxy transport-level errors."),
	)
	if err != nil {
		return nil, err
	}

	tlsCertExpirySeconds, err := meter.Int64UpDownCounter("vibewarden_tls_cert_expiry_seconds",
		ports.WithDescription("Seconds until the monitored TLS certificate expires. Negative when already expired."),
		ports.WithUnit("s"),
	)
	if err != nil {
		return nil, err
	}

	return &OTelAdapter{
		requestsTotal:        requestsTotal,
		requestDuration:      requestDuration,
		rateLimitHits:        rateLimitHits,
		authDecisions:        authDecisions,
		upstreamErrors:       upstreamErrors,
		upstreamTimeouts:     upstreamTimeouts,
		upstreamRetries:      upstreamRetries,
		activeConnections:    activeConnections,
		circuitBreakerState:  circuitBreakerState,
		upstreamHealthy:      upstreamHealthy,
		wafDetections:        wafDetections,
		egressRequestsTotal:  egressRequestsTotal,
		egressDuration:       egressDuration,
		egressErrorsTotal:    egressErrorsTotal,
		tlsCertExpirySeconds: tlsCertExpirySeconds,
		pathMatcher:          NewPathMatcher(pathPatterns),
		handler:              provider.Handler(),
	}, nil
}

// Handler returns the Prometheus HTTP handler for scraping.
func (a *OTelAdapter) Handler() http.Handler { return a.handler }

// NormalizePath returns the matching pattern for a path, or "other" if no pattern matches.
func (a *OTelAdapter) NormalizePath(path string) string {
	return a.pathMatcher.Match(path)
}

// IncRequestTotal implements ports.MetricsCollector.
func (a *OTelAdapter) IncRequestTotal(method, statusCode, pathPattern string) {
	a.requestsTotal.Add(context.Background(), 1,
		ports.Attribute{Key: "method", Value: method},
		ports.Attribute{Key: "status_code", Value: statusCode},
		ports.Attribute{Key: "path_pattern", Value: pathPattern},
	)
}

// ObserveRequestDuration implements ports.MetricsCollector.
func (a *OTelAdapter) ObserveRequestDuration(method, pathPattern string, duration time.Duration) {
	a.requestDuration.Record(context.Background(), duration.Seconds(),
		ports.Attribute{Key: "method", Value: method},
		ports.Attribute{Key: "path_pattern", Value: pathPattern},
	)
}

// IncRateLimitHit implements ports.MetricsCollector.
func (a *OTelAdapter) IncRateLimitHit(limitType string) {
	a.rateLimitHits.Add(context.Background(), 1,
		ports.Attribute{Key: "limit_type", Value: limitType},
	)
}

// IncAuthDecision implements ports.MetricsCollector.
func (a *OTelAdapter) IncAuthDecision(decision string) {
	a.authDecisions.Add(context.Background(), 1,
		ports.Attribute{Key: "decision", Value: decision},
	)
}

// IncUpstreamError implements ports.MetricsCollector.
func (a *OTelAdapter) IncUpstreamError() {
	a.upstreamErrors.Add(context.Background(), 1)
}

// IncUpstreamTimeout implements ports.MetricsCollector.
func (a *OTelAdapter) IncUpstreamTimeout() {
	a.upstreamTimeouts.Add(context.Background(), 1)
}

// IncUpstreamRetry implements ports.MetricsCollector.
func (a *OTelAdapter) IncUpstreamRetry(method string) {
	a.upstreamRetries.Add(context.Background(), 1,
		ports.Attribute{Key: "method", Value: method},
	)
}

// SetActiveConnections implements ports.MetricsCollector.
// OTel's UpDownCounter only supports Add, not Set. This implementation tracks
// the previous value atomically and adds the delta on each call.
func (a *OTelAdapter) SetActiveConnections(n int) {
	next := int64(n)
	prev := a.currentConns.Swap(next)
	delta := next - prev
	if delta != 0 {
		a.activeConnections.Add(context.Background(), delta)
	}
}

// SetCircuitBreakerState implements ports.MetricsCollector.
// Records the current circuit breaker state as a gauge value:
// 0=closed, 1=open, 2=half_open.
// OTel's UpDownCounter only supports Add; this implementation tracks the
// previous value atomically and emits only the delta.
func (a *OTelAdapter) SetCircuitBreakerState(ctx context.Context, state resilience.State) {
	next := int64(state)
	prev := a.currentCBState.Swap(next)
	delta := next - prev
	if delta != 0 {
		a.circuitBreakerState.Add(ctx, delta)
	}
}

// SetUpstreamHealthy implements ports.MetricsCollectorWithUpstreamHealth.
// Sets the vibewarden_upstream_healthy gauge to 1 when healthy, 0 otherwise.
// OTel's UpDownCounter only supports Add; this implementation tracks the
// previous value atomically and emits only the delta.
func (a *OTelAdapter) SetUpstreamHealthy(ctx context.Context, healthy bool) {
	var next int64
	if healthy {
		next = 1
	}
	prev := a.currentUpstreamHealthy.Swap(next)
	delta := next - prev
	if delta != 0 {
		a.upstreamHealthy.Add(ctx, delta)
	}
}

// IncWAFDetection implements ports.MetricsCollector.
func (a *OTelAdapter) IncWAFDetection(rule, mode string) {
	a.wafDetections.Add(context.Background(), 1,
		ports.Attribute{Key: "rule", Value: rule},
		ports.Attribute{Key: "mode", Value: mode},
	)
}

// IncEgressRequestTotal implements ports.MetricsCollector.
func (a *OTelAdapter) IncEgressRequestTotal(route, method, statusCode string) {
	a.egressRequestsTotal.Add(context.Background(), 1,
		ports.Attribute{Key: "route", Value: route},
		ports.Attribute{Key: "method", Value: method},
		ports.Attribute{Key: "status_code", Value: statusCode},
	)
}

// ObserveEgressDuration implements ports.MetricsCollector.
func (a *OTelAdapter) ObserveEgressDuration(route, method string, duration time.Duration) {
	a.egressDuration.Record(context.Background(), duration.Seconds(),
		ports.Attribute{Key: "route", Value: route},
		ports.Attribute{Key: "method", Value: method},
	)
}

// IncEgressErrorTotal implements ports.MetricsCollector.
func (a *OTelAdapter) IncEgressErrorTotal(route string) {
	a.egressErrorsTotal.Add(context.Background(), 1,
		ports.Attribute{Key: "route", Value: route},
	)
}

// SetTLSCertExpirySeconds implements ports.MetricsCollector.
// Sets the vibewarden_tls_cert_expiry_seconds gauge for the given domain to the
// provided number of seconds until expiry. The value is stored per-domain using
// an atomic counter; the delta between the new and previous value is emitted.
// The seconds value is rounded to the nearest integer before recording.
func (a *OTelAdapter) SetTLSCertExpirySeconds(domain string, seconds float64) {
	next := int64(math.Round(seconds))
	prevPtr, _ := a.currentCertExpiryByDomain.LoadOrStore(domain, &atomic.Int64{})
	prev := prevPtr.(*atomic.Int64).Swap(next)
	delta := next - prev
	if delta != 0 {
		a.tlsCertExpirySeconds.Add(context.Background(), delta,
			ports.Attribute{Key: "domain", Value: domain},
		)
	}
}
