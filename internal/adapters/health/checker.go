// Package health provides an HTTP-based active upstream health checker adapter.
package health

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/vibewarden/vibewarden/internal/domain/events"
	domainheal "github.com/vibewarden/vibewarden/internal/domain/health"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// HTTPChecker is an implementation of ports.UpstreamHealthChecker that probes
// the upstream application by issuing periodic HTTP GET requests to the
// configured health path. State transitions trigger structured events and
// metric updates.
//
// HTTPChecker is safe for concurrent use after Start returns.
type HTTPChecker struct {
	mu     sync.RWMutex
	entity *domainheal.UpstreamHealth

	upstreamBase string // e.g. "http://127.0.0.1:3000"
	client       *http.Client

	logger  *slog.Logger
	eventer ports.EventLogger
	metrics ports.MetricsCollectorWithUpstreamHealth

	stopCh chan struct{}
	doneCh chan struct{}
}

// Config is the set of runtime parameters for the HTTP health checker.
// All duration fields must already be parsed from config strings.
type Config struct {
	// UpstreamHost is the upstream host (e.g. "127.0.0.1").
	UpstreamHost string

	// UpstreamPort is the upstream port (e.g. 3000).
	UpstreamPort int

	// DomainConfig is the parsed domain-layer configuration.
	DomainConfig domainheal.Config
}

// NewHTTPChecker constructs an HTTPChecker. The metrics parameter is optional
// and may be nil when the metrics subsystem is disabled.
func NewHTTPChecker(
	cfg Config,
	logger *slog.Logger,
	eventer ports.EventLogger,
	metrics ports.MetricsCollectorWithUpstreamHealth,
) (*HTTPChecker, error) {
	entity, err := domainheal.NewUpstreamHealth(cfg.DomainConfig)
	if err != nil {
		return nil, fmt.Errorf("creating upstream health entity: %w", err)
	}

	upstreamBase := fmt.Sprintf("http://%s:%d", cfg.UpstreamHost, cfg.UpstreamPort)

	return &HTTPChecker{
		entity:       entity,
		upstreamBase: upstreamBase,
		client: &http.Client{
			Timeout: cfg.DomainConfig.Timeout,
		},
		logger:  logger,
		eventer: eventer,
		metrics: metrics,
		stopCh:  make(chan struct{}),
		doneCh:  make(chan struct{}),
	}, nil
}

// Start begins background health probing. Probes are issued at the configured
// interval until the provided context is cancelled or Stop is called. Start
// returns immediately; the probing loop runs in a goroutine.
func (c *HTTPChecker) Start(ctx context.Context) error {
	go c.loop(ctx)
	return nil
}

// Stop signals the background goroutine to exit and waits for it to finish,
// honouring the provided context deadline.
func (c *HTTPChecker) Stop(ctx context.Context) error {
	close(c.stopCh)
	select {
	case <-c.doneCh:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("upstream health checker stop: %w", ctx.Err())
	}
}

// CurrentStatus returns the most recently computed health status.
// It is safe for concurrent use and does not block.
func (c *HTTPChecker) CurrentStatus() domainheal.UpstreamStatus {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.entity.Status()
}

// Snapshot returns a point-in-time view of the current health state,
// suitable for inclusion in the /_vibewarden/health response.
func (c *HTTPChecker) Snapshot() ports.UpstreamHealthSnapshot {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return ports.UpstreamHealthSnapshot{
		Status:               c.entity.Status().String(),
		ConsecutiveSuccesses: c.entity.ConsecutiveSuccesses(),
		ConsecutiveFailures:  c.entity.ConsecutiveFailures(),
		LastError:            c.entity.LastError(),
	}
}

// loop is the background probing goroutine.
func (c *HTTPChecker) loop(ctx context.Context) {
	defer close(c.doneCh)

	ticker := time.NewTicker(c.entity.Config().Interval)
	defer ticker.Stop()

	// Run first probe immediately instead of waiting for the first tick.
	c.probe(ctx)

	for {
		select {
		case <-ticker.C:
			c.probe(ctx)
		case <-c.stopCh:
			return
		case <-ctx.Done():
			return
		}
	}
}

// probe performs a single HTTP GET to the upstream health path and updates the
// domain entity. If a state transition occurs, a structured event is emitted
// and the metrics gauge is updated.
func (c *HTTPChecker) probe(ctx context.Context) {
	url := c.upstreamBase + c.entity.Config().Path
	probeCtx, cancel := context.WithTimeout(ctx, c.entity.Config().Timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(probeCtx, http.MethodGet, url, nil)
	if err != nil {
		c.recordFailure(fmt.Sprintf("building probe request: %s", err.Error()))
		return
	}

	resp, err := c.client.Do(req)
	if err != nil {
		c.recordFailure(err.Error())
		return
	}
	_ = resp.Body.Close() //nolint:errcheck // body close error on health probe is not actionable

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		c.recordSuccess()
	} else {
		c.recordFailure(fmt.Sprintf("non-2xx status: %d", resp.StatusCode))
	}
}

// recordSuccess updates domain state for a successful probe and emits events /
// metrics on transition.
func (c *HTTPChecker) recordSuccess() {
	c.mu.Lock()
	previous, transitioned := c.entity.RecordSuccess(time.Now())
	current := c.entity.Status()
	successCount := c.entity.ConsecutiveSuccesses()
	c.mu.Unlock()

	if transitioned {
		c.emitHealthChangedEvent(previous, current, successCount, "")
		c.updateMetric(current)
	}
}

// recordFailure updates domain state for a failed probe and emits events /
// metrics on transition.
func (c *HTTPChecker) recordFailure(errMsg string) {
	c.mu.Lock()
	previous, transitioned := c.entity.RecordFailure(time.Now(), errMsg)
	current := c.entity.Status()
	failureCount := c.entity.ConsecutiveFailures()
	c.mu.Unlock()

	if transitioned {
		c.emitHealthChangedEvent(previous, current, failureCount, errMsg)
		c.updateMetric(current)
	}

	if c.logger != nil {
		c.logger.Debug("upstream health probe failed",
			slog.String("error", errMsg),
			slog.String("status", current.String()),
		)
	}
}

// emitHealthChangedEvent constructs and emits an upstream.health_changed event.
func (c *HTTPChecker) emitHealthChangedEvent(
	previous domainheal.UpstreamStatus,
	current domainheal.UpstreamStatus,
	consecutiveCount int,
	lastError string,
) {
	if c.eventer == nil {
		return
	}
	ev := events.NewUpstreamHealthChanged(events.UpstreamHealthChangedParams{
		PreviousStatus:   previous.String(),
		NewStatus:        current.String(),
		ConsecutiveCount: consecutiveCount,
		UpstreamURL:      c.upstreamBase + c.entity.Config().Path,
		LastError:        lastError,
	})
	if err := c.eventer.Log(context.Background(), ev); err != nil {
		if c.logger != nil {
			c.logger.Error("upstream health checker: failed to emit event",
				slog.String("event_type", ev.EventType),
				slog.String("error", err.Error()),
			)
		}
	}
}

// updateMetric pushes the vibewarden_upstream_healthy gauge update.
func (c *HTTPChecker) updateMetric(current domainheal.UpstreamStatus) {
	if c.metrics == nil {
		return
	}
	c.metrics.SetUpstreamHealthy(context.Background(), current == domainheal.StatusHealthy)
}

// NewHTTPCheckerFromURL constructs an HTTPChecker using a full URL as the
// upstream base (e.g. "http://127.0.0.1:3000"). This is primarily intended for
// use in tests where a httptest.Server URL is available directly.
func NewHTTPCheckerFromURL(
	upstreamBaseURL string,
	domainCfg domainheal.Config,
	logger *slog.Logger,
	eventer ports.EventLogger,
	metrics ports.MetricsCollectorWithUpstreamHealth,
) (*HTTPChecker, error) {
	entity, err := domainheal.NewUpstreamHealth(domainCfg)
	if err != nil {
		return nil, fmt.Errorf("creating upstream health entity: %w", err)
	}
	return &HTTPChecker{
		entity:       entity,
		upstreamBase: upstreamBaseURL,
		client: &http.Client{
			Timeout: domainCfg.Timeout,
		},
		logger:  logger,
		eventer: eventer,
		metrics: metrics,
		stopCh:  make(chan struct{}),
		doneCh:  make(chan struct{}),
	}, nil
}

// Compile-time assertion that HTTPChecker satisfies ports.UpstreamHealthChecker.
var _ ports.UpstreamHealthChecker = (*HTTPChecker)(nil)
