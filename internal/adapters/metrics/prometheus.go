// Package metrics provides metrics adapter implementations for VibeWarden.
package metrics

import (
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// PrometheusAdapter implements ports.MetricsCollector using prometheus/client_golang.
// It creates an isolated registry (not the global default) and registers all
// VibeWarden metrics plus Go runtime collectors.
type PrometheusAdapter struct {
	registry          *prometheus.Registry
	requestsTotal     *prometheus.CounterVec
	requestDuration   *prometheus.HistogramVec
	rateLimitHits     *prometheus.CounterVec
	authDecisions     *prometheus.CounterVec
	upstreamErrors    prometheus.Counter
	activeConnections prometheus.Gauge
	pathMatcher       *PathMatcher
}

// NewPrometheusAdapter creates a new Prometheus metrics adapter with all collectors
// registered. The pathPatterns parameter configures path normalization (e.g., "/users/:id")
// to prevent high-cardinality labels.
func NewPrometheusAdapter(pathPatterns []string) *PrometheusAdapter {
	reg := prometheus.NewRegistry()

	// Register Go runtime collectors.
	reg.MustRegister(collectors.NewGoCollector())
	reg.MustRegister(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))

	requestsTotal := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "vibewarden_requests_total",
			Help: "Total number of HTTP requests processed.",
		},
		[]string{"method", "status_code", "path_pattern"},
	)

	requestDuration := prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "vibewarden_request_duration_seconds",
			Help:    "HTTP request duration in seconds.",
			Buckets: []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
		},
		[]string{"method", "path_pattern"},
	)

	rateLimitHits := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "vibewarden_rate_limit_hits_total",
			Help: "Total number of rate limit hits.",
		},
		[]string{"limit_type"},
	)

	authDecisions := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "vibewarden_auth_decisions_total",
			Help: "Total number of authentication decisions.",
		},
		[]string{"decision"},
	)

	upstreamErrors := prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "vibewarden_upstream_errors_total",
			Help: "Total number of upstream connection errors.",
		},
	)

	activeConnections := prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "vibewarden_active_connections",
			Help: "Current number of active proxy connections.",
		},
	)

	reg.MustRegister(requestsTotal, requestDuration, rateLimitHits,
		authDecisions, upstreamErrors, activeConnections)

	return &PrometheusAdapter{
		registry:          reg,
		requestsTotal:     requestsTotal,
		requestDuration:   requestDuration,
		rateLimitHits:     rateLimitHits,
		authDecisions:     authDecisions,
		upstreamErrors:    upstreamErrors,
		activeConnections: activeConnections,
		pathMatcher:       NewPathMatcher(pathPatterns),
	}
}

// Handler returns an http.Handler that serves the Prometheus metrics endpoint
// in OpenMetrics format.
func (p *PrometheusAdapter) Handler() http.Handler {
	return promhttp.HandlerFor(p.registry, promhttp.HandlerOpts{
		EnableOpenMetrics: true,
	})
}

// NormalizePath returns the matching pattern for a path, or "other" if no pattern matches.
// This delegates to the underlying PathMatcher configured at construction time.
func (p *PrometheusAdapter) NormalizePath(path string) string {
	return p.pathMatcher.Match(path)
}

// IncRequestTotal implements ports.MetricsCollector.
func (p *PrometheusAdapter) IncRequestTotal(method, statusCode, pathPattern string) {
	p.requestsTotal.WithLabelValues(method, statusCode, pathPattern).Inc()
}

// ObserveRequestDuration implements ports.MetricsCollector.
func (p *PrometheusAdapter) ObserveRequestDuration(method, pathPattern string, duration time.Duration) {
	p.requestDuration.WithLabelValues(method, pathPattern).Observe(duration.Seconds())
}

// IncRateLimitHit implements ports.MetricsCollector.
func (p *PrometheusAdapter) IncRateLimitHit(limitType string) {
	p.rateLimitHits.WithLabelValues(limitType).Inc()
}

// IncAuthDecision implements ports.MetricsCollector.
func (p *PrometheusAdapter) IncAuthDecision(decision string) {
	p.authDecisions.WithLabelValues(decision).Inc()
}

// IncUpstreamError implements ports.MetricsCollector.
func (p *PrometheusAdapter) IncUpstreamError() {
	p.upstreamErrors.Inc()
}

// SetActiveConnections implements ports.MetricsCollector.
func (p *PrometheusAdapter) SetActiveConnections(n int) {
	p.activeConnections.Set(float64(n))
}
